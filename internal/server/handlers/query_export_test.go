package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
	"github.com/caioricciuti/ch-ui/internal/testutil"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
	"github.com/gorilla/websocket"
)

const exportTestSecret = "export-test-secret"

// exportFixture is a gateway with one connection, and an HTTP server that
// runs ExportQuery as a logged-in user of that connection.
type exportFixture struct {
	db      *database.DB
	conn    string
	gateway *tunnel.Gateway
	gwURL   string
	handler *QueryHandler
	api     *httptest.Server
}

func newExportFixture(t *testing.T, cfg *config.Config, user, password string) *exportFixture {
	t.Helper()
	db, conn := testutil.WorkerDB(t)
	gw := tunnel.NewGateway(db)
	t.Cleanup(gw.Stop)
	gwSrv := httptest.NewServer(http.HandlerFunc(gw.HandleWebSocket))
	t.Cleanup(gwSrv.Close)

	if cfg == nil {
		cfg = &config.Config{}
	}
	cfg.AppSecretKey = exportTestSecret
	h := &QueryHandler{DB: db, Gateway: gw, Config: cfg}

	enc, err := crypto.Encrypt(password, exportTestSecret)
	if err != nil {
		t.Fatal(err)
	}
	session := &middleware.SessionInfo{ConnectionID: conn, ClickhouseUser: user, EncryptedPassword: enc}
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.ExportQuery(w, r.WithContext(middleware.SetSession(r.Context(), session)))
	}))
	// Runs after api.Close and before the database's temp dir is removed.
	t.Cleanup(h.waitBackground)
	t.Cleanup(api.Close)

	return &exportFixture{db: db, conn: conn, gateway: gw, gwURL: "ws" + strings.TrimPrefix(gwSrv.URL, "http"), handler: h, api: api}
}

// post sends an export request. The body is read fully; readErr is set when
// the server aborted the download.
func (f *exportFixture) post(t *testing.T, body string) (resp *http.Response, data []byte, readErr error) {
	t.Helper()
	resp, err := http.Post(f.api.URL, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, readErr = io.ReadAll(resp.Body)
	return resp, data, readErr
}

// fakeExportAgent authenticates with the given capabilities and answers each
// query_export with reply. It records every message it receives.
type fakeExportAgent struct {
	mu   sync.Mutex
	seen []tunnel.GatewayMessage
}

func (a *fakeExportAgent) messages() []tunnel.GatewayMessage {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]tunnel.GatewayMessage(nil), a.seen...)
}

func startFakeExportAgent(t *testing.T, f *exportFixture, capabilities []string, reply func(msg tunnel.GatewayMessage) []tunnel.AgentMessage) *fakeExportAgent {
	t.Helper()
	ws, _, err := websocket.DefaultDialer.Dial(f.gwURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ws.Close() })
	if err := ws.WriteJSON(tunnel.AgentMessage{Type: "auth", Token: "worker-token", Capabilities: capabilities}); err != nil {
		t.Fatal(err)
	}
	var ack tunnel.GatewayMessage
	if err := ws.ReadJSON(&ack); err != nil || ack.Type != "auth_ok" {
		t.Fatalf("auth: %+v %v", ack, err)
	}
	a := &fakeExportAgent{}
	go func() {
		for {
			var msg tunnel.GatewayMessage
			if err := ws.ReadJSON(&msg); err != nil {
				return
			}
			a.mu.Lock()
			a.seen = append(a.seen, msg)
			a.mu.Unlock()
			if msg.Type != "query_export" {
				continue
			}
			for _, out := range reply(msg) {
				out.QueryID = msg.QueryID
				if err := ws.WriteJSON(out); err != nil {
					return
				}
			}
		}
	}()
	return a
}

func exportChunk(b []byte, seq int) tunnel.AgentMessage {
	data, _ := json.Marshal(b)
	return tunnel.AgentMessage{Type: "query_export_chunk", Data: data, Seq: seq}
}

func TestExportStreamsAgentBytes(t *testing.T) {
	f := newExportFixture(t, &config.Config{ExportMaxRows: 500, ExportMaxBytes: 1 << 20}, "alice", "pw")
	parts := [][]byte{[]byte("a,b\n"), []byte("1,2\n"), {0x00, 0xff, '\n'}}
	agent := startFakeExportAgent(t, f, []string{tunnel.CapabilityExport}, func(tunnel.GatewayMessage) []tunnel.AgentMessage {
		out := make([]tunnel.AgentMessage, 0, len(parts)+1)
		for i, p := range parts {
			out = append(out, exportChunk(p, i))
		}
		return append(out, tunnel.AgentMessage{Type: "query_export_end", Bytes: 11})
	})

	resp, data, err := f.post(t, `{"sql":"SELECT a, b FROM t WHERE x = {x:UInt8};","format":"CSVWithNames","params":{"x":"1"}}`)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", resp.StatusCode, data)
	}
	if want := bytes.Join(parts, nil); !bytes.Equal(data, want) {
		t.Fatalf("body %q, want %q", data, want)
	}
	cd := resp.Header.Get("Content-Disposition")
	if !strings.HasPrefix(cd, `attachment; filename="ch-ui-export-`) || !strings.HasSuffix(cd, `.csv"`) {
		t.Fatalf("Content-Disposition %q", cd)
	}

	msgs := agent.messages()
	if len(msgs) != 1 {
		t.Fatalf("want one export request, got %d", len(msgs))
	}
	m := msgs[0]
	if m.Format != "CSVWithNames" || m.Query != "SELECT a, b FROM t WHERE x = {x:UInt8}" || m.User != "alice" || m.Password != "pw" {
		t.Fatalf("unexpected request %+v", m)
	}
	want := map[string]string{"readonly": "1", "max_result_rows": "500", "max_result_bytes": "1048576", "result_overflow_mode": "throw", "param_x": "1"}
	for k, v := range want {
		if m.Settings[k] != v {
			t.Fatalf("setting %s = %q, want %q (all: %v)", k, m.Settings[k], v, m.Settings)
		}
	}

	// History and audit are written in the background.
	deadline := time.Now().Add(3 * time.Second)
	for {
		entries, _ := f.db.GetQueryHistory("alice", f.conn, "", "", 10, 0)
		logs, _ := f.db.GetAuditLogsFiltered(10, "", "query.export", "", "")
		if len(entries) == 1 && entries[0].Status == "success" && len(logs) == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("history %+v, audit %+v", entries, logs)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestExportOldAgentGetsUpdateError(t *testing.T) {
	f := newExportFixture(t, nil, "alice", "")
	agent := startFakeExportAgent(t, f, nil, func(tunnel.GatewayMessage) []tunnel.AgentMessage { return nil })

	resp, data, _ := f.post(t, `{"sql":"SELECT 1","format":"Parquet"}`)
	if resp.StatusCode != http.StatusConflict || !strings.Contains(string(data), "update the agent") {
		t.Fatalf("status %d: %s", resp.StatusCode, data)
	}
	if n := len(agent.messages()); n != 0 {
		t.Fatalf("an old agent must not be sent the export, got %d messages", n)
	}
}

func TestExportErrorAfterFirstBytesAbortsDownload(t *testing.T) {
	f := newExportFixture(t, nil, "alice", "")
	startFakeExportAgent(t, f, []string{tunnel.CapabilityExport}, func(tunnel.GatewayMessage) []tunnel.AgentMessage {
		return []tunnel.AgentMessage{exportChunk([]byte("a\n1\n"), 0), {Type: "query_export_error", Error: "Code: 241. DB::Exception: Memory limit exceeded"}}
	})
	resp, _, err := f.post(t, `{"sql":"SELECT 1","format":"CSVWithNames"}`)
	if resp.StatusCode != http.StatusOK || err == nil {
		t.Fatalf("want a started download that is cut off, got status %d err %v", resp.StatusCode, err)
	}
}

func TestExportErrorBeforeFirstBytesIsJSON(t *testing.T) {
	f := newExportFixture(t, &config.Config{ExportMaxRows: 10}, "alice", "")
	startFakeExportAgent(t, f, []string{tunnel.CapabilityExport}, func(tunnel.GatewayMessage) []tunnel.AgentMessage {
		return []tunnel.AgentMessage{{Type: "query_export_error", Error: "Code: 396. DB::Exception: Limit for result exceeded, max rows: 10.00. (TOO_MANY_ROWS_OR_BYTES)"}}
	})
	resp, data, _ := f.post(t, `{"sql":"SELECT 1","format":"CSVWithNames"}`)
	if resp.StatusCode != http.StatusBadGateway || !strings.Contains(string(data), "export limit (10 rows") {
		t.Fatalf("status %d: %s", resp.StatusCode, data)
	}
}

func TestExportByteCapAbortsDownload(t *testing.T) {
	f := newExportFixture(t, &config.Config{ExportMaxBytes: 6}, "alice", "")
	startFakeExportAgent(t, f, []string{tunnel.CapabilityExport}, func(tunnel.GatewayMessage) []tunnel.AgentMessage {
		return []tunnel.AgentMessage{exportChunk([]byte("1234"), 0), exportChunk([]byte("5678"), 1), {Type: "query_export_end"}}
	})
	_, data, err := f.post(t, `{"sql":"SELECT 1","format":"TSVWithNames"}`)
	if err == nil || len(data) > 6 {
		t.Fatalf("want the download cut at the cap, got %d bytes, err %v", len(data), err)
	}
}

func TestExportEmptyResult(t *testing.T) {
	f := newExportFixture(t, nil, "alice", "")
	startFakeExportAgent(t, f, []string{tunnel.CapabilityExport}, func(tunnel.GatewayMessage) []tunnel.AgentMessage {
		return []tunnel.AgentMessage{{Type: "query_export_end"}}
	})
	resp, data, err := f.post(t, `{"sql":"SELECT 1 WHERE 0","format":"JSONEachRow"}`)
	if err != nil || resp.StatusCode != http.StatusOK || len(data) != 0 {
		t.Fatalf("status %d, %d bytes, err %v", resp.StatusCode, len(data), err)
	}
}

func TestExportReadonlyProfileDropsRefusedSetting(t *testing.T) {
	f := newExportFixture(t, nil, "ro", "")
	agent := startFakeExportAgent(t, f, []string{tunnel.CapabilityExport}, func(msg tunnel.GatewayMessage) []tunnel.AgentMessage {
		for _, name := range []string{"max_result_rows", "max_result_bytes", "result_overflow_mode"} {
			if _, ok := msg.Settings[name]; ok {
				return []tunnel.AgentMessage{{Type: "query_export_error", Error: fmt.Sprintf("Code: 164. DB::Exception: Cannot modify '%s' setting in readonly mode. (READONLY)", name)}}
			}
		}
		return []tunnel.AgentMessage{exportChunk([]byte("ok\n"), 0), {Type: "query_export_end"}}
	})
	resp, data, err := f.post(t, `{"sql":"SELECT 1","format":"CSVWithNames"}`)
	if err != nil || resp.StatusCode != http.StatusOK || string(data) != "ok\n" {
		t.Fatalf("status %d: %q %v", resp.StatusCode, data, err)
	}
	msgs := agent.messages()
	last := msgs[len(msgs)-1].Settings
	if last["readonly"] != "1" || len(msgs) != 4 {
		t.Fatalf("want three retries that keep readonly=1, got %d requests, last %v", len(msgs), last)
	}
}

func TestExportRejectsBadRequests(t *testing.T) {
	f := newExportFixture(t, nil, "alice", "")
	for _, body := range []string{
		`{"sql":"","format":"CSVWithNames"}`,
		`{"sql":"SELECT 1","format":"XLSX"}`,
		`{"sql":"SELECT 1","format":"CSV; DROP TABLE x"}`,
		`{"sql":"SELECT 1 FORMAT JSON","format":"CSVWithNames"}`,
	} {
		resp, data, _ := f.post(t, body)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("%s: status %d: %s", body, resp.StatusCode, data)
		}
	}
}
