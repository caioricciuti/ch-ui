package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/caioricciuti/ch-ui/connector"
	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/ingest"
	"github.com/caioricciuti/ch-ui/internal/ingest/spool"
	"github.com/caioricciuti/ch-ui/internal/pipelines"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
	"github.com/caioricciuti/ch-ui/internal/testutil"
)

// End to end against a live ClickHouse (CHUI_TEST_CLICKHOUSE_URL,
// passwordless default user): the receive setup, one OTLP JSON log sent
// with the key it returned, the row in the table and the source listed.
func TestLiveReceiveLogs(t *testing.T) {
	endpoint := os.Getenv("CHUI_TEST_CLICKHOUSE_URL")
	if endpoint == "" {
		t.Skip("set CHUI_TEST_CLICKHOUSE_URL to a disposable test instance")
	}
	if u := os.Getenv("CHUI_TEST_CLICKHOUSE_USER"); u != "" && u != "default" || os.Getenv("CHUI_TEST_CLICKHOUSE_PASSWORD") != "" {
		t.Skip("the live receive test needs the passwordless default user")
	}
	client := connector.NewCHClient(endpoint, false)
	scalar := func(sql string) string {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		res, err := client.Execute(ctx, sql, "default", "", nil)
		if err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
		if len(res.Data) == 0 {
			return ""
		}
		for _, v := range res.Data[0] {
			return fmt.Sprint(v)
		}
		return ""
	}
	dbName := fmt.Sprintf("chui_receive_%d", time.Now().UnixNano())
	scalar("CREATE DATABASE " + dbName)
	t.Cleanup(func() { scalar("DROP DATABASE IF EXISTS " + dbName) })

	db, conn := testutil.WorkerDB(t)
	agent := testutil.NewWorkerAgent(t, db, nil)
	cfg := &config.Config{AppSecretKey: "test-secret", DatabasePath: filepath.Join(t.TempDir(), "ch-ui.db")}
	enc, err := crypto.Encrypt("", cfg.AppSecretKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetBackgroundCredential(conn, database.BackgroundCredential{Worker: "pipeline", Mode: "service_account", Username: "default", EncryptedPassword: enc}); err != nil {
		t.Fatal(err)
	}
	sp, err := spool.Open(t.TempDir(), spool.Options{SegmentMaxAge: 50 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	hub := pipelines.NewHub(sp)
	runner := pipelines.NewRunner(db, agent.Gateway, cfg)
	runner.SetHub(hub)
	t.Cleanup(func() { runner.Stop(); hub.Close() })

	router := (&TelemetryHandler{DB: db, Gateway: agent.Gateway, Config: cfg, Runner: runner}).Routes()
	call := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req = req.WithContext(middleware.SetSession(req.Context(), &middleware.SessionInfo{
			UserRole: "analyst", ClickhouseUser: "default", ConnectionID: conn, EncryptedPassword: enc,
		}))
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	rec := call(http.MethodPost, "/receive", `{"signal":"logs","database":"`+dbName+`","ttl_days":3}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("receive: %d %s", rec.Code, rec.Body)
	}
	var out struct {
		Pipeline     receivePipeline `json:"pipeline"`
		Secret       string          `json:"secret"`
		TableCreated bool            `json:"table_created"`
		Source       struct {
			ID string `json:"id"`
		} `json:"source"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || !out.TableCreated || out.Pipeline.Status != "running" {
		t.Fatalf("receive answer: %s (%v)", rec.Body, err)
	}
	if got := scalar("SELECT count() FROM system.tables WHERE database = '" + dbName + "' AND name = 'otel_logs'"); got != "1" {
		t.Fatalf("otel_logs not created: %s", got)
	}
	deadline := time.Now().Add(10 * time.Second)
	for hub.Endpoint(out.Pipeline.ID) == nil {
		if time.Now().After(deadline) {
			t.Fatal("ingest endpoint not registered")
		}
		time.Sleep(20 * time.Millisecond)
	}

	ir := chi.NewRouter()
	ir.Route("/ingest", ingest.NewReceiver(db, hub).Routes)
	body := fmt.Sprintf(`{"resourceLogs":[{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"checkout"}}]},"scopeLogs":[{"scope":{"name":"s"},"logRecords":[{"timeUnixNano":"%d","severityText":"INFO","body":{"stringValue":"receive works"}}]}]}]}`, time.Now().UnixNano())
	req := httptest.NewRequest(http.MethodPost, "/ingest/v1/logs", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+out.Secret)
	irec := httptest.NewRecorder()
	ir.ServeHTTP(irec, req)
	if irec.Code != http.StatusOK {
		t.Fatalf("ingest: %d %s", irec.Code, irec.Body)
	}

	deadline = time.Now().Add(30 * time.Second)
	for scalar("SELECT count() FROM "+dbName+".otel_logs WHERE Body = 'receive works' AND ServiceName = 'checkout'") != "1" {
		if time.Now().After(deadline) {
			p, _ := db.GetPipelineByID(out.Pipeline.ID)
			t.Fatalf("the log never reached the table (pipeline %+v)", p)
		}
		time.Sleep(200 * time.Millisecond)
	}

	list := call(http.MethodGet, "/sources", "")
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), out.Source.ID) || !strings.Contains(list.Body.String(), dbName) {
		t.Fatalf("sources: %d %s", list.Code, list.Body)
	}
	if got := scalar("SELECT engine_full LIKE '%toIntervalDay(3)%' FROM system.tables WHERE database = '" + dbName + "' AND name = 'otel_logs'"); got != "1" && got != "true" {
		t.Fatalf("otel_logs has no 3 day TTL: %s", got)
	}
}
