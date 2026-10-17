package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
	"github.com/caioricciuti/ch-ui/internal/testutil"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

// A share created by an SSO person with a per-person ClickHouse user stores
// a reference to that user, not a copy of its password: the link runs with
// whatever the password is now, and answers 410 once the user is locked or
// dropped. A password login keeps copying its password.
func TestShareOfPerPersonUserFollowsLiveCredentials(t *testing.T) {
	db, conn := testutil.WorkerDB(t)
	agent := testutil.NewAgent(t, db, "worker-token", func(tunnel.GatewayMessage) *tunnel.AgentMessage {
		return &tunnel.AgentMessage{Type: "query_result", Data: json.RawMessage(`[{"x":1}]`), Meta: json.RawMessage(`[{"name":"x","type":"UInt8"}]`)}
	})
	const secret = "test-secret"
	encrypt := func(pw string) string {
		enc, err := crypto.Encrypt(pw, secret)
		if err != nil {
			t.Fatal(err)
		}
		return enc
	}
	ident, err := db.UpsertSSOIdentity("https://idp.example", "sub-ana", "ana@x.com", "Ana", []string{"analysts"})
	if err != nil {
		t.Fatal(err)
	}
	saveUser := func(pw string) {
		if err := db.SaveSSOClickHouseUser(database.SSOClickHouseUser{
			IdentityID: ident.ID, ConnectionID: conn, CHUser: "sso:ana@x.com", EncryptedPassword: encrypt(pw), Roles: []string{"analyst"},
		}); err != nil {
			t.Fatal(err)
		}
	}
	saveUser("pw1")

	dash, err := db.CreateDashboard("Shared", "", "ana@x.com")
	if err != nil {
		t.Fatal(err)
	}
	// One panel per request: a 200 result is cached per panel for a while.
	var panels []string
	for _, q := range []string{"SELECT 1", "SELECT 2", "SELECT 3", "SELECT 4"} {
		id, err := db.CreatePanel(dash, q, "", "table", q, "", "{}", 0, 0, 4, 4)
		if err != nil {
			t.Fatal(err)
		}
		panels = append(panels, id)
	}
	if err := db.SetDashboardVariables(dash, `[{"name":"host","type":"query","query":"SELECT host FROM hosts"}]`); err != nil {
		t.Fatal(err)
	}

	h := &DashboardsHandler{DB: db, Gateway: agent.Gateway, Config: &config.Config{AppSecretKey: secret}}
	private, public := h.Routes(), h.PublicRoutes()

	createShare := func(session *middleware.SessionInfo) *database.DashboardShare {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/"+dash+"/shares", strings.NewReader(`{"visibility":"public"}`))
		rec := httptest.NewRecorder()
		private.ServeHTTP(rec, req.WithContext(middleware.SetSession(req.Context(), session)))
		if rec.Code != http.StatusCreated {
			t.Fatalf("create share: %d %s", rec.Code, rec.Body.String())
		}
		var resp struct {
			Share database.DashboardShare `json:"share"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		share, err := db.GetDashboardShareByToken(resp.Share.Token)
		if err != nil || share == nil {
			t.Fatalf("stored share: %v %v", share, err)
		}
		return share
	}

	// The session carries a password copy that is already stale: the share
	// must not store it.
	share := createShare(&middleware.SessionInfo{
		ConnectionID: conn, ClickhouseUser: "sso:ana@x.com", EncryptedPassword: encrypt("stale"),
		UserRole: "analyst", AuthSubject: "ana@x.com", SSOIdentityID: ident.ID,
	})
	if share.SSOIdentityID != ident.ID || share.EncryptedPassword != "" || share.ClickhouseUser != "sso:ana@x.com" {
		t.Fatalf("share of a per-person user: %+v", share)
	}
	plain := createShare(&middleware.SessionInfo{ConnectionID: conn, ClickhouseUser: "alice", EncryptedPassword: encrypt("alice-pw"), UserRole: "analyst"})
	if plain.SSOIdentityID != "" || plain.EncryptedPassword == "" {
		t.Fatalf("share of a password login must copy the password: %+v", plain)
	}
	// An SSO session on a connection in shared mode (no per-person row)
	// copies the service account password as before.
	other, err := db.UpsertSSOIdentity("https://idp.example", "sub-bob", "bob@x.com", "Bob", nil)
	if err != nil {
		t.Fatal(err)
	}
	shared := createShare(&middleware.SessionInfo{ConnectionID: conn, ClickhouseUser: "svc_sso", EncryptedPassword: encrypt("svc"), UserRole: "analyst", AuthSubject: "bob@x.com", SSOIdentityID: other.ID})
	if shared.SSOIdentityID != "" || shared.EncryptedPassword == "" || shared.ClickhouseUser != "svc_sso" {
		t.Fatalf("share of a shared-mode SSO session: %+v", shared)
	}

	query := func(panel string) *httptest.ResponseRecorder {
		t.Helper()
		rec := httptest.NewRecorder()
		public.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/"+share.Token+"/query", strings.NewReader(`{"panel_id":"`+panel+`"}`)))
		return rec
	}
	lastQuery := func() tunnel.GatewayMessage {
		t.Helper()
		msgs := agent.Messages()
		for i := len(msgs) - 1; i >= 0; i-- {
			if msgs[i].Type == "query" {
				return msgs[i]
			}
		}
		t.Fatal("no query reached the agent")
		return tunnel.GatewayMessage{}
	}

	if rec := query(panels[0]); rec.Code != http.StatusOK {
		t.Fatalf("panel with pw1: %d %s", rec.Code, rec.Body.String())
	}
	if m := lastQuery(); m.User != "sso:ana@x.com" || m.Password != "pw1" {
		t.Fatalf("ran as %s/%s, want sso:ana@x.com/pw1", m.User, m.Password)
	}

	// The person signs in again: the password rotates, the link follows.
	saveUser("pw2")
	if rec := query(panels[1]); rec.Code != http.StatusOK {
		t.Fatalf("panel with pw2: %d %s", rec.Code, rec.Body.String())
	}
	if m := lastQuery(); m.Password != "pw2" {
		t.Fatalf("ran with password %q, want the rotated pw2", m.Password)
	}

	// Locked: the link is gone, for panels and variable options alike.
	if err := db.SetSSOClickHouseUserLocked(ident.ID, conn, true); err != nil {
		t.Fatal(err)
	}
	before := len(agent.Messages())
	if rec := query(panels[2]); rec.Code != http.StatusGone || !strings.Contains(rec.Body.String(), shareOwnerGone) {
		t.Fatalf("locked owner: %d %s", rec.Code, rec.Body.String())
	}
	rec := httptest.NewRecorder()
	public.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/"+share.Token+"/variables/host/options", strings.NewReader(`{}`)))
	if rec.Code != http.StatusGone || !strings.Contains(rec.Body.String(), shareOwnerGone) {
		t.Fatalf("locked owner options: %d %s", rec.Code, rec.Body.String())
	}
	if n := len(agent.Messages()) - before; n != 0 {
		t.Fatalf("%d statements reached ClickHouse for a locked owner", n)
	}

	// Dropped: still gone. Shares of other people are unaffected.
	if err := db.DeleteSSOClickHouseUser(ident.ID, conn); err != nil {
		t.Fatal(err)
	}
	if rec := query(panels[3]); rec.Code != http.StatusGone || !strings.Contains(rec.Body.String(), shareOwnerGone) {
		t.Fatalf("dropped owner: %d %s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	public.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/"+plain.Token+"/query", strings.NewReader(`{"panel_id":"`+panels[3]+`"}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("password login's share: %d %s", rec.Code, rec.Body.String())
	}
	if m := lastQuery(); m.User != "alice" || m.Password != "alice-pw" {
		t.Fatalf("ran as %s/%s, want alice/alice-pw", m.User, m.Password)
	}
}
