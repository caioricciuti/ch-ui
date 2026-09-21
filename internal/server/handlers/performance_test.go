package handlers

import (
	"encoding/json"
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

func TestPerformanceRoutesAccessAndIdentity(t *testing.T) {
	db, conn := testutil.WorkerDB(t)
	data := json.RawMessage(`[{"hash":"42","database":"analytics","sample_query":"SELECT ?","baseline_runs":10,"current_runs":20,"baseline_p95_ms":100,"current_p95_ms":400}]`)
	agent := testutil.NewAgent(t, db, "worker-token", func(msg tunnel.GatewayMessage) *tunnel.AgentMessage {
		return &tunnel.AgentMessage{Type: "query_result", Data: data}
	})
	enc, err := crypto.Encrypt("own-password", "test-secret")
	if err != nil {
		t.Fatal(err)
	}
	h := &PerformanceHandler{DB: db, Gateway: agent.Gateway, Config: &config.Config{AppSecretKey: "test-secret"}}
	r := h.Routes()
	request := func(method, path, body, role, connection string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if role != "" {
			req = req.WithContext(middleware.SetSession(req.Context(), &middleware.SessionInfo{ConnectionID: connection, ClickhouseUser: "shared-clickhouse-login", AuthSubject: "human@example.test", UserRole: role, EncryptedPassword: enc}))
		}
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)
		return rr
	}
	for _, path := range []string{"/investigations", "/investigations/missing"} {
		if got := request("GET", path, "", "", conn); got.Code != 401 {
			t.Fatalf("unauthenticated %s: %d", path, got.Code)
		}
	}
	for _, role := range []string{"", "viewer", "analyst"} {
		if got := request("GET", "/monitor", "", role, conn); got.Code != 403 {
			t.Fatalf("%s read delegated background report: %d", role, got.Code)
		}
	}
	for _, route := range []struct{ method, path, body string }{{"POST", "/investigations", `{}`}, {"PUT", "/investigations/missing", `{}`}, {"POST", "/investigations/missing/compare", `{}`}, {"PUT", "/monitor", `{"enabled":true}`}} {
		if got := request(route.method, route.path, route.body, "viewer", conn); got.Code != 403 {
			t.Fatalf("viewer mutation %s: %d", route.path, got.Code)
		}
	}
	body := `{"title":"Investigate query","owner":"owner@example.test","note":"Before change","hash":"42","database":"analytics","range":"1h"}`
	created := request("POST", "/investigations", body, "analyst", conn)
	if created.Code != 200 {
		t.Fatalf("create: %d %s", created.Code, created.Body.String())
	}
	var detail struct {
		Investigation database.PerformanceInvestigation `json:"investigation"`
		Events        []database.PerformanceEvent       `json:"events"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &detail); err != nil {
		t.Fatal(err)
	}
	if detail.Investigation.CreatedBy != "human@example.test" || detail.Events[0].Actor != "human@example.test" {
		t.Fatal("SSO human attribution lost")
	}
	for _, msg := range agent.Messages() {
		if msg.User != "shared-clickhouse-login" || msg.Password != "own-password" || msg.Settings["readonly"] != "1" {
			t.Fatal("interactive capture used another account or unrestricted query")
		}
	}
	path := "/investigations/" + detail.Investigation.ID
	if got := request("GET", path, "", "viewer", conn); got.Code != 200 {
		t.Fatalf("viewer read: %d", got.Code)
	}
	if got := request("GET", path, "", "admin", "other-connection"); got.Code != 404 {
		t.Fatalf("cross-connection detail: %d", got.Code)
	}
	if got := request("POST", path+"/compare", `{}`, "admin", "other-connection"); got.Code != 404 {
		t.Fatalf("cross-connection compare: %d", got.Code)
	}
	before := len(agent.Messages())
	if got := request("POST", path+"/compare", `{}`, "analyst", conn); got.Code != 409 {
		t.Fatalf("overlapping comparison: %d %s", got.Code, got.Body.String())
	}
	if len(agent.Messages()) != before {
		t.Fatal("overlap rejection unnecessarily queried ClickHouse")
	}
	if got := request("PUT", "/monitor", `{"enabled":true}`, "analyst", conn); got.Code != 403 {
		t.Fatal("analyst enabled background worker")
	}
	if got := request("PUT", "/monitor", `{"enabled":true}`, "admin", conn); got.Code != 409 {
		t.Fatalf("monitor enabled without dedicated account: %d", got.Code)
	}
	if err := db.SetBackgroundCredential(conn, database.BackgroundCredential{Worker: "performance", Mode: "service_account", Username: "reader", EncryptedPassword: enc}); err != nil {
		t.Fatal(err)
	}
	if got := request("PUT", "/monitor", `{"enabled":true}`, "admin", conn); got.Code != 200 {
		t.Fatalf("admin could not opt in: %d %s", got.Code, got.Body.String())
	}
}
