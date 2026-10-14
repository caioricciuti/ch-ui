package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/database/dbtest"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

const leakToken = "cht_0123456789abcdef0123456789abcdef"

type tokenFixture struct {
	db     *database.DB
	gw     *tunnel.Gateway
	router chi.Router
	connID string
	wsURL  string
}

func newTokenFixture(t *testing.T) *tokenFixture {
	t.Helper()
	db, err := dbtest.Open(t, "conn-token.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	connID, err := db.CreateConnection(database.CreateConnectionParams{Name: "prod", TunnelToken: leakToken, Type: database.ConnectionTypeTunnel})
	if err != nil {
		t.Fatal(err)
	}
	gw := tunnel.NewGateway(db)
	t.Cleanup(gw.Stop)
	srv := httptest.NewServer(http.HandlerFunc(gw.HandleWebSocket))
	t.Cleanup(srv.Close)

	h := &ConnectionsHandler{DB: db, Gateway: gw, Config: &config.Config{}}
	r := chi.NewRouter()
	r.Get("/", h.List)
	r.Get("/{id}", h.Get)
	r.Post("/", h.Create)
	r.Put("/{id}", h.Update)
	r.Delete("/{id}", h.Delete)
	r.Get("/{id}/token", h.GetToken)
	r.Post("/{id}/regenerate-token", h.RegenerateToken)
	return &tokenFixture{db: db, gw: gw, router: r, connID: connID, wsURL: "ws" + strings.TrimPrefix(srv.URL, "http")}
}

func (f *tokenFixture) call(t *testing.T, role, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req = req.WithContext(middleware.SetSession(req.Context(), &middleware.SessionInfo{UserRole: role, ClickhouseUser: role}))
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)
	return rec
}

// dialAgent authenticates a protocol-level agent and returns its socket and
// the gateway's reply type.
func (f *tokenFixture) dialAgent(t *testing.T, token string) (*websocket.Conn, string) {
	t.Helper()
	ws, _, err := websocket.DefaultDialer.Dial(f.wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ws.Close() })
	if err := ws.WriteJSON(tunnel.AgentMessage{Type: "auth", Token: token}); err != nil {
		t.Fatal(err)
	}
	_ = ws.SetReadDeadline(time.Now().Add(5 * time.Second))
	var reply tunnel.GatewayMessage
	if err := ws.ReadJSON(&reply); err != nil {
		t.Fatalf("auth reply: %v", err)
	}
	return ws, reply.Type
}

// expectClosed waits for the gateway to close an agent socket.
func expectClosed(t *testing.T, ws *websocket.Conn) {
	t.Helper()
	_ = ws.SetReadDeadline(time.Now().Add(5 * time.Second))
	for {
		_, _, err := ws.ReadMessage()
		if err == nil {
			continue // a ping or other frame queued before the close
		}
		if !websocket.IsCloseError(err, websocket.ClosePolicyViolation) {
			t.Fatalf("expected the gateway to close the agent, got %v", err)
		}
		return
	}
}

// hasKey reports whether key appears as an object key anywhere in v.
func hasKey(v any, key string) bool {
	switch x := v.(type) {
	case map[string]any:
		for k, child := range x {
			if k == key || hasKey(child, key) {
				return true
			}
		}
	case []any:
		for _, child := range x {
			if hasKey(child, key) {
				return true
			}
		}
	}
	return false
}

func decodeJSONBody(t *testing.T, rec *httptest.ResponseRecorder) any {
	t.Helper()
	var v any
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
	return v
}

func TestConnectionReadsNeverIncludeTunnelToken(t *testing.T) {
	f := newTokenFixture(t)
	for _, role := range []string{"viewer", "analyst", "admin"} {
		for _, path := range []string{"/", "/" + f.connID} {
			rec := f.call(t, role, http.MethodGet, path, "")
			if rec.Code != http.StatusOK {
				t.Fatalf("%s GET %s: %d %s", role, path, rec.Code, rec.Body.String())
			}
			body := rec.Body.String()
			if strings.Contains(body, "tunnel_token") || strings.Contains(body, leakToken) {
				t.Fatalf("%s GET %s leaked the tunnel token: %s", role, path, body)
			}
			if !strings.Contains(body, `"name":"prod"`) || !strings.Contains(body, `"online":false`) {
				t.Fatalf("%s GET %s lost connection fields: %s", role, path, body)
			}
		}
	}

	// The admin token endpoint is the place the token is read from.
	rec := f.call(t, "admin", http.MethodGet, "/"+f.connID+"/token", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"tunnel_token":"`+leakToken+`"`) {
		t.Fatalf("admin token endpoint: %d %s", rec.Code, rec.Body.String())
	}
}

// Create returns the new token once, at the top level; the connection object
// in every admin response stays token free.
func TestConnectionWritesKeepTokenOutOfConnectionObject(t *testing.T) {
	f := newTokenFixture(t)

	rec := f.call(t, "admin", http.MethodPost, "/", `{"name":"edge"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create tunnel: %d %s", rec.Code, rec.Body.String())
	}
	created := decodeJSONBody(t, rec).(map[string]any)
	token, _ := created["tunnel_token"].(string)
	if !strings.HasPrefix(token, "cht_") {
		t.Fatalf("create must return the new token to the admin: %v", created)
	}
	if hasKey(created["connection"], "tunnel_token") || strings.Contains(mustJSON(t, created["connection"]), token) {
		t.Fatalf("created connection object carries the token: %v", created["connection"])
	}

	rec = f.call(t, "admin", http.MethodPost, "/", `{"name":"direct","type":"direct","clickhouse_url":"http://ch.example:8123"}`)
	if rec.Code != http.StatusCreated || hasKey(decodeJSONBody(t, rec), "tunnel_token") {
		t.Fatalf("create direct: %d %s", rec.Code, rec.Body.String())
	}

	rec = f.call(t, "admin", http.MethodPut, "/"+f.connID, `{"name":"prod-renamed"}`)
	if rec.Code != http.StatusOK || hasKey(decodeJSONBody(t, rec), "tunnel_token") || strings.Contains(rec.Body.String(), leakToken) {
		t.Fatalf("update: %d %s", rec.Code, rec.Body.String())
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func lastAudit(t *testing.T, db *database.DB, action string) string {
	t.Helper()
	logs, err := db.GetAuditLogs(50)
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range logs {
		if l.Action == action && l.Details != nil {
			return *l.Details
		}
	}
	t.Fatalf("no %s audit entry", action)
	return ""
}

func TestRegenerateTokenDisconnectsLiveAgent(t *testing.T) {
	f := newTokenFixture(t)
	agent, reply := f.dialAgent(t, leakToken)
	if reply != "auth_ok" || !f.gw.IsTunnelOnline(f.connID) {
		t.Fatalf("agent did not attach: %s", reply)
	}

	rec := f.call(t, "admin", http.MethodPost, "/"+f.connID+"/regenerate-token", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("regenerate: %d %s", rec.Code, rec.Body.String())
	}
	var body struct {
		TunnelToken string `json:"tunnel_token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.TunnelToken == "" || body.TunnelToken == leakToken {
		t.Fatalf("regenerate response: %s", rec.Body.String())
	}
	if f.gw.IsTunnelOnline(f.connID) {
		t.Fatal("agent still published after regenerate")
	}
	expectClosed(t, agent)
	if d := lastAudit(t, f.db, "connection.token_regenerated"); !strings.Contains(d, "live agent disconnected") {
		t.Fatalf("audit details: %q", d)
	}

	if _, reply := f.dialAgent(t, leakToken); reply != "auth_error" {
		t.Fatalf("old token reconnect: %s, want auth_error", reply)
	}
	if _, reply := f.dialAgent(t, body.TunnelToken); reply != "auth_ok" || !f.gw.IsTunnelOnline(f.connID) {
		t.Fatalf("new token reconnect: %s, want auth_ok", reply)
	}
}

func TestRegenerateTokenWithoutAgentSaysNothingAboutDisconnect(t *testing.T) {
	f := newTokenFixture(t)
	if rec := f.call(t, "admin", http.MethodPost, "/"+f.connID+"/regenerate-token", ""); rec.Code != http.StatusOK {
		t.Fatalf("regenerate: %d %s", rec.Code, rec.Body.String())
	}
	if d := lastAudit(t, f.db, "connection.token_regenerated"); strings.Contains(d, "disconnected") {
		t.Fatalf("audit details: %q", d)
	}
}

func TestDeleteConnectionDisconnectsLiveAgent(t *testing.T) {
	f := newTokenFixture(t)
	agent, reply := f.dialAgent(t, leakToken)
	if reply != "auth_ok" {
		t.Fatalf("agent did not attach: %s", reply)
	}

	if rec := f.call(t, "admin", http.MethodDelete, "/"+f.connID, ""); rec.Code != http.StatusOK {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body.String())
	}
	if f.gw.IsTunnelOnline(f.connID) {
		t.Fatal("agent still published after delete")
	}
	expectClosed(t, agent)
	if d := lastAudit(t, f.db, "connection.deleted"); !strings.Contains(d, "live agent disconnected") {
		t.Fatalf("audit details: %q", d)
	}
	if _, reply := f.dialAgent(t, leakToken); reply != "auth_error" {
		t.Fatalf("reconnect after delete: %s, want auth_error", reply)
	}
}
