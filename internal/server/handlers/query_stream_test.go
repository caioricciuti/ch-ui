package handlers

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
	"github.com/caioricciuti/ch-ui/internal/testutil"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
	"github.com/gorilla/websocket"
)

// streamFixture runs StreamQuery as a logged-in user of a connection whose
// agent answers each query_stream with reply.
type streamFixture struct {
	api  *httptest.Server
	mu   sync.Mutex
	seen []tunnel.GatewayMessage
}

func newStreamFixture(t *testing.T, user, password string, reply func(msg tunnel.GatewayMessage) []tunnel.AgentMessage) *streamFixture {
	t.Helper()
	db, conn := testutil.WorkerDB(t)
	gw := tunnel.NewGateway(db)
	t.Cleanup(gw.Stop)
	gwSrv := httptest.NewServer(http.HandlerFunc(gw.HandleWebSocket))
	t.Cleanup(gwSrv.Close)

	h := &QueryHandler{DB: db, Gateway: gw, Config: &config.Config{AppSecretKey: exportTestSecret}}
	enc, err := crypto.Encrypt(password, exportTestSecret)
	if err != nil {
		t.Fatal(err)
	}
	session := &middleware.SessionInfo{ConnectionID: conn, ClickhouseUser: user, EncryptedPassword: enc}
	f := &streamFixture{api: httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.StreamQuery(w, r.WithContext(middleware.SetSession(r.Context(), session)))
	}))}
	t.Cleanup(h.waitBackground)
	t.Cleanup(f.api.Close)

	ws, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(gwSrv.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ws.Close() })
	if err := ws.WriteJSON(tunnel.AgentMessage{Type: "auth", Token: "worker-token"}); err != nil {
		t.Fatal(err)
	}
	var ack tunnel.GatewayMessage
	if err := ws.ReadJSON(&ack); err != nil || ack.Type != "auth_ok" {
		t.Fatalf("auth: %+v %v", ack, err)
	}
	go func() {
		for {
			var msg tunnel.GatewayMessage
			if err := ws.ReadJSON(&msg); err != nil {
				return
			}
			f.mu.Lock()
			f.seen = append(f.seen, msg)
			f.mu.Unlock()
			if msg.Type != "query_stream" {
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
	return f
}

func (f *streamFixture) streams() []tunnel.GatewayMessage {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []tunnel.GatewayMessage
	for _, m := range f.seen {
		if m.Type == "query_stream" {
			out = append(out, m)
		}
	}
	return out
}

type streamLine struct {
	Type      string            `json:"type"`
	Meta      json.RawMessage   `json:"meta"`
	Data      []json.RawMessage `json:"data"`
	Error     string            `json:"error"`
	TotalRows int64             `json:"total_rows"`
}

// post runs a /stream request and returns its NDJSON lines.
func (f *streamFixture) post(t *testing.T, body string) []streamLine {
	t.Helper()
	resp, err := http.Post(f.api.URL, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var lines []streamLine
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		var l streamLine
		if err := json.Unmarshal(sc.Bytes(), &l); err != nil {
			t.Fatalf("bad line %q: %v", sc.Text(), err)
		}
		if l.Type != "progress" {
			lines = append(lines, l)
		}
	}
	return lines
}

func refusedSetting(name string) tunnel.AgentMessage {
	return tunnel.AgentMessage{Type: "query_stream_error", Error: "metadata query failed: ClickHouse error: Code: 164. DB::Exception: Cannot modify '" + name + "' setting in readonly mode. (READONLY)"}
}

func rowsChunk(seq int, rows string) tunnel.AgentMessage {
	return tunnel.AgentMessage{Type: "query_stream_chunk", Seq: seq, Data: json.RawMessage(rows)}
}

func TestStreamQueryRetriesRefusedSettingsAndCapsRows(t *testing.T) {
	f := newStreamFixture(t, "ro", "pw", func(msg tunnel.GatewayMessage) []tunnel.AgentMessage {
		// ClickHouse names one refused setting at a time.
		for _, name := range []string{"max_result_rows", "result_overflow_mode"} {
			if _, ok := msg.Settings[name]; ok {
				return []tunnel.AgentMessage{refusedSetting(name)}
			}
		}
		return []tunnel.AgentMessage{
			{Type: "query_stream_start", Meta: json.RawMessage(`[{"name":"n","type":"UInt64"}]`)},
			rowsChunk(0, `[[1],[2],[3]]`),
			rowsChunk(1, `[[4],[5],[6]]`),
			{Type: "query_stream_end", TotalRows: 6},
		}
	})
	lines := f.post(t, `{"query":"SELECT n FROM t WHERE n > {x:UInt64}","maxResultRows":4,"params":{"x":"0"}}`)

	streams := f.streams()
	if len(streams) != 2 {
		t.Fatalf("want one retry, got %d dispatches", len(streams))
	}
	last := streams[1].Settings
	if _, ok := last["max_result_rows"]; ok {
		t.Errorf("retry still sends max_result_rows: %v", last)
	}
	if _, ok := last["result_overflow_mode"]; ok {
		t.Errorf("retry still sends result_overflow_mode: %v", last)
	}
	if last["param_x"] != "0" {
		t.Errorf("retry dropped the bind parameter: %v", last)
	}

	var types []string
	rows := 0
	var done streamLine
	for _, l := range lines {
		types = append(types, l.Type)
		rows += len(l.Data)
		if l.Type == "done" {
			done = l
		}
	}
	if types[0] != "meta" || types[len(types)-1] != "done" || strings.Count(strings.Join(types, ","), "meta") != 1 {
		t.Fatalf("unexpected stream %v", types)
	}
	if rows != 4 || done.TotalRows != 4 {
		t.Fatalf("want the 4 row cap enforced, got %d rows, total_rows %d", rows, done.TotalRows)
	}
}

func TestStreamQueryRetryAfterEmptyWriteMeta(t *testing.T) {
	f := newStreamFixture(t, "ro", "pw", func(msg tunnel.GatewayMessage) []tunnel.AgentMessage {
		meta := tunnel.AgentMessage{Type: "query_stream_start", Meta: json.RawMessage(`[]`)}
		if _, ok := msg.Settings["max_result_rows"]; ok {
			return []tunnel.AgentMessage{meta, refusedSetting("max_result_rows")}
		}
		return []tunnel.AgentMessage{meta, {Type: "query_stream_end"}}
	})
	lines := f.post(t, `{"query":"USE db"}`)
	var types []string
	for _, l := range lines {
		types = append(types, l.Type)
	}
	if strings.Join(types, ",") != "meta,done" {
		t.Fatalf("want one meta then done, got %v", types)
	}
}

func TestStreamQueryKeepsSettingTheStatementNames(t *testing.T) {
	f := newStreamFixture(t, "ro", "pw", func(msg tunnel.GatewayMessage) []tunnel.AgentMessage {
		return []tunnel.AgentMessage{refusedSetting("max_result_rows")}
	})
	lines := f.post(t, `{"query":"SELECT 1 SETTINGS max_result_rows = 5"}`)
	if len(f.streams()) != 1 {
		t.Fatalf("a setting the statement names must not be retried, got %d dispatches", len(f.streams()))
	}
	if len(lines) != 1 || lines[0].Type != "error" || !strings.Contains(lines[0].Error, "Cannot modify") {
		t.Fatalf("want the ClickHouse error, got %+v", lines)
	}
}

func TestSampleQueryWithTrailingComment(t *testing.T) {
	db, conn := testutil.WorkerDB(t)
	var mu sync.Mutex
	var sent []string
	agent := testutil.NewAgent(t, db, "worker-token", func(msg tunnel.GatewayMessage) *tunnel.AgentMessage {
		mu.Lock()
		sent = append(sent, msg.SQL)
		mu.Unlock()
		return &tunnel.AgentMessage{Type: "query_result", Data: json.RawMessage(`[]`), Meta: json.RawMessage(`[]`)}
	})
	h := &QueryHandler{DB: db, Gateway: agent.Gateway, Config: &config.Config{AppSecretKey: exportTestSecret}}
	enc, err := crypto.Encrypt("pw", exportTestSecret)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/query/sample", strings.NewReader(`{"query":"SELECT 1 -- note"}`))
	req = req.WithContext(middleware.SetSession(req.Context(), &middleware.SessionInfo{ConnectionID: conn, ClickhouseUser: "u", EncryptedPassword: enc}))
	rr := httptest.NewRecorder()
	h.SampleQuery(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
	}
	mu.Lock()
	defer mu.Unlock()
	if len(sent) != 1 || sent[0] != "SELECT * FROM (\nSELECT 1\n) AS __ch_ui_sample LIMIT 25 BY `_shard_num`" {
		t.Fatalf("sample SQL = %q", sent)
	}
}
