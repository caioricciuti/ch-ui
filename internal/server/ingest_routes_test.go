package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/database/dbtest"
)

// The receivers sit outside the session middleware on the main port (and on
// the ingest listener, also under /v1); with ingest_only_separate the main
// port stops serving them. The spool lives in <data dir>/ingest-spool.
func TestIngestRoutes(t *testing.T) {
	dir := t.TempDir()
	db, err := dbtest.OpenIn(t, dir, "ch-ui.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	conn, err := db.CreateConnection(database.CreateConnectionParams{Name: "test", TunnelToken: "test-only"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateSession(database.CreateSessionParams{ConnectionID: conn, ClickhouseUser: "viewer", UserRole: "viewer", Token: "test-session", ExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339)}); err != nil {
		t.Fatal(err)
	}
	newServer := func(onlySeparate bool) *Server {
		s := New(&config.Config{DevMode: true, AppSecretKey: "test-only", AppURL: "https://ch.example.com", DatabasePath: filepath.Join(dir, "ch-ui.db"), IngestAddr: ":4318", IngestOnlySeparate: onlySeparate}, db, nil, nil)
		t.Cleanup(func() { s.gateway.Stop(); s.ingestHub.Close() })
		return s
	}
	post := func(h http.Handler, path string) int {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader("{}"))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}

	s := newServer(false)
	if !s.ingestHub.Available() {
		t.Fatal("spool not opened")
	}
	if _, err := os.Stat(filepath.Join(dir, "ingest-spool")); err != nil {
		t.Fatalf("spool dir: %v", err)
	}
	for _, sig := range []string{"logs", "traces", "events"} {
		if got := post(s.router, "/ingest/v1/"+sig); got != http.StatusUnauthorized {
			t.Errorf("main /ingest/v1/%s: %d, want 401", sig, got)
		}
		for _, p := range []string{"/ingest/v1/" + sig, "/v1/" + sig} {
			if got := post(s.ingestHTTP.Handler, p); got != http.StatusUnauthorized {
				t.Errorf("listener %s: %d, want 401", p, got)
			}
		}
	}
	// The Elasticsearch bulk API and Splunk HEC, on both ports.
	for _, p := range []string{"/ingest/es/_bulk", "/ingest/es/logs/_bulk", "/ingest/services/collector", "/ingest/services/collector/event", "/ingest/services/collector/raw"} {
		if got := post(s.router, p); got != http.StatusUnauthorized {
			t.Errorf("main %s: %d, want 401", p, got)
		}
		for _, lp := range []string{p, strings.TrimPrefix(p, "/ingest")} {
			if got := post(s.ingestHTTP.Handler, lp); got != http.StatusUnauthorized {
				t.Errorf("listener %s: %d, want 401", lp, got)
			}
		}
	}
	for _, h := range []http.Handler{s.router, s.ingestHTTP.Handler} {
		req := httptest.NewRequest(http.MethodGet, "/ingest/es/", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized || rec.Header().Get("X-Elastic-Product") != "Elasticsearch" {
			t.Errorf("GET /ingest/es/: %d %v", rec.Code, rec.Header())
		}
		req = httptest.NewRequest(http.MethodGet, "/ingest/services/collector/health", nil)
		rec = httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("HEC health: %d", rec.Code)
		}
	}
	if got := post(s.ingestHTTP.Handler, "/api/auth/login"); got != http.StatusNotFound {
		t.Errorf("the ingest listener serves the API: %d", got)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/ingest/info", nil)
	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("info without a session: %d", rec.Code)
	}
	req.AddCookie(&http.Cookie{Name: "chui_session", Value: "test-session"})
	rec = httptest.NewRecorder()
	s.router.ServeHTTP(rec, req)
	var info struct {
		BaseURL          string   `json:"base_url"`
		Signals          []string `json:"signals"`
		SeparateListener bool     `json:"separate_listener"`
		ESURL            string   `json:"es_url"`
		HECURL           string   `json:"hec_url"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &info); err != nil || rec.Code != 200 {
		t.Fatalf("info: %d %s", rec.Code, rec.Body)
	}
	if info.BaseURL != "https://ch.example.com:4318/ingest" || len(info.Signals) != 3 || !info.SeparateListener || info.ESURL != "https://ch.example.com:4318/ingest/es" || info.HECURL != "https://ch.example.com:4318/ingest/services/collector" {
		t.Fatalf("info: %+v", info)
	}
	s.ingestHub.Close()

	only := newServer(true)
	if got := post(only.router, "/ingest/v1/logs"); got != http.StatusNotFound {
		t.Errorf("main port with ingest_only_separate: %d, want 404", got)
	}
	if got := post(only.ingestHTTP.Handler, "/v1/logs"); got != http.StatusUnauthorized {
		t.Errorf("listener with ingest_only_separate: %d", got)
	}
}
