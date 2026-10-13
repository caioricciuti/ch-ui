package handlers

import (
	"bytes"
	"context"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/caioricciuti/ch-ui/connector"
	connconfig "github.com/caioricciuti/ch-ui/connector/config"
	"github.com/caioricciuti/ch-ui/connector/ui"
	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/governance"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

// startRealAgent runs the actual connector (the same code as ch-ui-agent and
// the embedded direct connection) against the fixture's gateway.
func startRealAgent(t *testing.T, f *exportFixture, clickhouseURL string) {
	t.Helper()
	agent := connector.New(&connconfig.Config{
		TunnelURL:         f.gwURL,
		Token:             "worker-token",
		ClickHouseURL:     clickhouseURL,
		HeartbeatInterval: 30 * time.Second,
		ReconnectDelay:    time.Second,
		MaxReconnectDelay: 2 * time.Second,
	}, ui.New(true, true, false, false))
	go func() { _ = agent.Run() }()
	t.Cleanup(agent.Shutdown)

	deadline := time.Now().Add(10 * time.Second)
	for !f.gateway.IsTunnelOnline(f.conn) {
		if time.Now().After(deadline) {
			t.Fatal("agent did not connect")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestExportLive(t *testing.T) {
	endpoint := os.Getenv("CHUI_TEST_CLICKHOUSE_URL")
	if endpoint == "" {
		t.Skip("set CHUI_TEST_CLICKHOUSE_URL to a disposable test instance")
	}
	user, password := os.Getenv("CHUI_TEST_CLICKHOUSE_USER"), os.Getenv("CHUI_TEST_CLICKHOUSE_PASSWORD")
	if user == "" {
		user = "default"
	}

	f := newExportFixture(t, &config.Config{ExportMaxRows: 200_000}, user, password)
	startRealAgent(t, f, endpoint)

	t.Run("csv with names", func(t *testing.T) {
		resp, data, err := f.post(t, `{"sql":"SELECT number AS n, toString(number) AS s FROM numbers(100000) -- trailing comment","format":"CSVWithNames"}`)
		if err != nil || resp.StatusCode != http.StatusOK {
			t.Fatalf("status %d, err %v: %.300s", resp.StatusCode, err, data)
		}
		lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
		if len(lines) != 100_001 || lines[0] != `"n","s"` || lines[100_000] != `99999,"99999"` {
			t.Fatalf("got %d lines, first %q, last %q", len(lines), lines[0], lines[len(lines)-1])
		}
	})

	t.Run("parquet", func(t *testing.T) {
		resp, data, err := f.post(t, `{"sql":"SELECT number, toString(number) AS s FROM numbers({n:UInt32})","format":"Parquet","params":{"n":"50000"}}`)
		if err != nil || resp.StatusCode != http.StatusOK {
			t.Fatalf("status %d, err %v: %.300s", resp.StatusCode, err, data)
		}
		if len(data) < 8 || !bytes.HasPrefix(data, []byte("PAR1")) || !bytes.HasSuffix(data, []byte("PAR1")) {
			t.Fatalf("not a Parquet file: %d bytes", len(data))
		}
		if !strings.HasSuffix(resp.Header.Get("Content-Disposition"), `.parquet"`) {
			t.Fatalf("Content-Disposition %q", resp.Header.Get("Content-Disposition"))
		}
	})

	t.Run("row limit", func(t *testing.T) {
		// ClickHouse may hit the limit before or after the first bytes are
		// out: either a JSON error or a cut download, never a whole file.
		resp, data, err := f.post(t, `{"sql":"SELECT number FROM numbers(1000000)","format":"CSVWithNames"}`)
		if resp.StatusCode == http.StatusOK && err == nil {
			t.Fatalf("an export over the row limit completed with %d bytes", len(data))
		}
		if resp.StatusCode != http.StatusOK && !strings.Contains(string(data), "export limit") {
			t.Fatalf("status %d: %.300s", resp.StatusCode, data)
		}
	})

	t.Run("error after the first bytes aborts the download", func(t *testing.T) {
		resp, data, err := f.post(t, `{"sql":"SELECT number, throwIf(number = 150000) FROM numbers(190000)","format":"CSVWithNames"}`)
		if err == nil && resp.StatusCode == http.StatusOK {
			t.Fatalf("a failed export must not complete, got %d bytes", len(data))
		}
		if bytes.Contains(data, []byte("__exception__")) {
			t.Fatal("the ClickHouse exception block reached the file")
		}
	})

	t.Run("writes are refused", func(t *testing.T) {
		table := "default.chui_export_must_not_exist"
		client := connector.NewCHClient(endpoint, false)
		t.Cleanup(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_, _ = client.Execute(ctx, "DROP TABLE IF EXISTS "+table, user, password, nil)
		})
		resp, data, _ := f.post(t, `{"sql":"CREATE TABLE `+table+` (x UInt8) ENGINE = Memory","format":"CSVWithNames"}`)
		if resp.StatusCode == http.StatusOK {
			t.Fatalf("a write must be refused under readonly=1: %.300s", data)
		}
	})

	t.Run("guardrails refuse a blocked export", func(t *testing.T) {
		store := governance.NewStore(f.db)
		if err := store.UpsertSyncState(f.conn, string(governance.SyncAccess), "idle", nil, nil, 0); err != nil {
			t.Fatal(err)
		}
		policy, err := store.CreatePolicy(f.conn, "Block secret", "", "table", "chui_export_db", "secret", "", "analyst", "warn", "block", "admin")
		if err != nil {
			t.Fatal(err)
		}
		// Guardrails are a Pro feature.
		f.handler.Config.SetProAccessForTest(func() config.ProAccess { return config.ProActive })
		f.handler.Guardrails = governance.NewGuardrailService(store, f.db)
		t.Cleanup(func() {
			f.handler.Guardrails = nil
			f.handler.Config.SetProAccessForTest(nil)
			_ = store.DeletePolicy(policy)
		})
		resp, data, _ := f.post(t, `{"sql":"SELECT * FROM chui_export_db.secret","format":"CSVWithNames"}`)
		if resp.StatusCode != http.StatusForbidden || !strings.Contains(string(data), "policy_blocked") {
			t.Fatalf("status %d: %s", resp.StatusCode, data)
		}
	})
}

// An agent that predates export (no capabilities in its auth message) gets a
// clear request to update instead of a hanging download.
func TestExportLiveOldAgent(t *testing.T) {
	if os.Getenv("CHUI_TEST_CLICKHOUSE_URL") == "" {
		t.Skip("set CHUI_TEST_CLICKHOUSE_URL to a disposable test instance")
	}
	f := newExportFixture(t, nil, "default", "")
	startFakeExportAgent(t, f, nil, func(tunnel.GatewayMessage) []tunnel.AgentMessage { return nil })
	resp, data, _ := f.post(t, `{"sql":"SELECT 1","format":"CSVWithNames"}`)
	if resp.StatusCode != http.StatusConflict || !strings.Contains(string(data), "update the agent") {
		t.Fatalf("status %d: %s", resp.StatusCode, data)
	}
}
