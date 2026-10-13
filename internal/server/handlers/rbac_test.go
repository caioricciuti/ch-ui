// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/rbac"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
	"github.com/caioricciuti/ch-ui/internal/testutil"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

// fakeAccess answers the snapshot queries from a small in-memory state and
// records the DDL it receives.
type fakeAccess struct {
	mu    sync.Mutex
	roles []string
	ddl   []string
}

func (f *fakeAccess) respond(msg tunnel.GatewayMessage) *tunnel.AgentMessage {
	sql := msg.SQL
	if sql == "" {
		sql = msg.Query
	}
	rows := func(v any) *tunnel.AgentMessage {
		b, _ := json.Marshal(v)
		return &tunnel.AgentMessage{Type: "query_result", Data: b, Meta: json.RawMessage("[]")}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case strings.Contains(sql, "FROM system.roles"):
		out := []map[string]any{}
		for _, r := range f.roles {
			out = append(out, map[string]any{"name": r, "storage": "local_directory"})
		}
		return rows(out)
	case strings.Contains(sql, "currentUser()"):
		return rows([]map[string]any{{"u": "admin"}})
	case strings.HasPrefix(sql, "SHOW GRANTS"):
		return rows([]map[string]any{{"GRANTS": "GRANT ALL ON *.* TO admin WITH GRANT OPTION"}})
	case strings.Contains(sql, "system.privileges"):
		return rows([]map[string]any{{"p": "SELECT"}, {"p": "INSERT"}})
	case strings.HasPrefix(sql, "SELECT"), strings.HasPrefix(sql, "EXPLAIN"):
		return rows([]map[string]any{})
	case strings.Contains(sql, "fail_me"):
		f.ddl = append(f.ddl, sql)
		return &tunnel.AgentMessage{Type: "query_error", Error: "Code: 62. DB::Exception: Syntax error (version 25.8)"}
	default:
		f.ddl = append(f.ddl, sql)
		return rows([]map[string]any{})
	}
}

func newRBACTestHandler(t *testing.T) (*RBACHandler, *database.DB, *fakeAccess, func(method, path, body string) *httptest.ResponseRecorder) {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "rbac.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	conn, err := db.CreateConnection(database.CreateConnectionParams{Name: "test", TunnelToken: "rbac-token"})
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeAccess{roles: []string{"existing"}}
	agent := testutil.NewAgent(t, db, "rbac-token", fake.respond)
	cfg := &config.Config{AppSecretKey: "rbac-secret"}
	cfg.SetProAccessForTest(func() config.ProAccess { return config.ProActive })
	enc, err := crypto.Encrypt("pw", cfg.AppSecretKey)
	if err != nil {
		t.Fatal(err)
	}
	h := &RBACHandler{DB: db, Gateway: agent.Gateway, Config: cfg}
	router := h.Routes()
	call := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req = req.WithContext(middleware.SetSession(req.Context(), &middleware.SessionInfo{
			UserRole: "admin", ClickhouseUser: "admin", ConnectionID: conn, EncryptedPassword: enc, AuthSubject: "ops@example.test",
		}))
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}
	return h, db, fake, call
}

type planResponse struct {
	Plan rbac.Plan `json:"plan"`
}

func TestRBACPlanApplyFlow(t *testing.T) {
	_, db, fake, call := newRBACTestHandler(t)

	ops := `[{"kind":"create_role","name":"chui_new"},{"kind":"grant_privilege","target":{"type":"role","name":"chui_new"},"access":["SELECT"],"database":"d"}]`
	rec := call("POST", "/plan", `{"ops":`+ops+`}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("plan: %d %s", rec.Code, rec.Body.String())
	}
	var pr planResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &pr); err != nil {
		t.Fatal(err)
	}
	if len(pr.Plan.Statements) != 2 || pr.Plan.Statements[0].SQL != "CREATE ROLE `chui_new`" || pr.Plan.PlanHash == "" {
		t.Fatalf("plan = %+v", pr.Plan)
	}
	if len(fake.ddl) != 0 {
		t.Fatalf("plan executed DDL: %v", fake.ddl)
	}

	// Unknown privilege and unknown fields are client errors.
	if rec := call("POST", "/plan", `{"ops":[{"kind":"grant_privilege","target":{"type":"role","name":"r"},"access":["TRUNCATE"]}]}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown privilege: %d", rec.Code)
	}
	if rec := call("POST", "/plan", `{"ops":[],"sql":"DROP TABLE x"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("raw sql field: %d", rec.Code)
	}

	// A stale hash is refused with 409 and nothing runs.
	if rec := call("POST", "/apply", `{"ops":`+ops+`,"plan_hash":"stale"}`); rec.Code != http.StatusConflict {
		t.Fatalf("stale hash: %d %s", rec.Code, rec.Body.String())
	}
	fake.mu.Lock()
	fake.roles = append(fake.roles, "someone_else")
	fake.mu.Unlock()
	if rec := call("POST", "/apply", `{"ops":`+ops+`,"plan_hash":"`+pr.Plan.PlanHash+`"}`); rec.Code != http.StatusConflict {
		t.Fatalf("changed snapshot: %d", rec.Code)
	}
	if len(fake.ddl) != 0 {
		t.Fatalf("conflict executed DDL: %v", fake.ddl)
	}

	// Re-plan and apply.
	rec = call("POST", "/plan", `{"ops":`+ops+`}`)
	_ = json.Unmarshal(rec.Body.Bytes(), &pr)
	rec = call("POST", "/apply", `{"ops":`+ops+`,"plan_hash":"`+pr.Plan.PlanHash+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("apply: %d %s", rec.Code, rec.Body.String())
	}
	var ar struct {
		Results     []rbac.Result `json:"results"`
		RollbackOps []rbac.Op     `json:"rollback_ops"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &ar)
	if len(ar.Results) != 2 || ar.Results[0].Status != rbac.StatusApplied || ar.Results[1].Status != rbac.StatusApplied {
		t.Fatalf("results = %+v", ar.Results)
	}
	if len(ar.RollbackOps) != 2 || ar.RollbackOps[0].Kind != rbac.OpRevokePrivilege || ar.RollbackOps[1].Kind != rbac.OpDropRole {
		t.Fatalf("rollback = %+v", ar.RollbackOps)
	}

	logs, err := db.GetAuditLogs(20)
	if err != nil {
		t.Fatal(err)
	}
	actions := map[string]int{}
	for _, l := range logs {
		if !strings.HasPrefix(l.Action, "rbac.") {
			continue
		}
		actions[l.Action]++
		if l.Username == nil || *l.Username != "ops@example.test" || l.ChUser == nil || *l.ChUser != "admin" {
			t.Fatalf("audit attribution: %+v", l)
		}
	}
	if actions["rbac.apply"] != 1 || actions["rbac.statement.applied"] != 2 {
		t.Fatalf("audit actions = %v", actions)
	}
}

func TestRBACApplyPartialFailure(t *testing.T) {
	_, db, fake, call := newRBACTestHandler(t)
	ops := `[{"kind":"create_role","name":"a_role"},{"kind":"create_role","name":"fail_me"},{"kind":"create_role","name":"z_role"}]`
	rec := call("POST", "/plan", `{"ops":`+ops+`}`)
	var pr planResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &pr)
	rec = call("POST", "/apply", `{"ops":`+ops+`,"plan_hash":"`+pr.Plan.PlanHash+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("apply: %d %s", rec.Code, rec.Body.String())
	}
	var ar struct {
		Results     []rbac.Result `json:"results"`
		RollbackOps []rbac.Op     `json:"rollback_ops"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &ar)
	got := []string{}
	for _, r := range ar.Results {
		got = append(got, r.Status)
	}
	if strings.Join(got, ",") != "applied,failed,skipped" {
		t.Fatalf("statuses = %v", got)
	}
	if len(fake.ddl) != 2 {
		t.Fatalf("ran after failure: %v", fake.ddl)
	}
	if len(ar.RollbackOps) != 1 || ar.RollbackOps[0].Name != "a_role" {
		t.Fatalf("rollback = %+v", ar.RollbackOps)
	}
	logs, _ := db.GetAuditLogs(20)
	failed := 0
	for _, l := range logs {
		if l.Action == "rbac.statement.failed" {
			failed++
		}
	}
	if failed != 1 {
		t.Fatalf("failed audit rows = %d", failed)
	}
}

func TestRBACApplyRefusesWarnings(t *testing.T) {
	_, _, fake, call := newRBACTestHandler(t)
	// Revoking ACCESS MANAGEMENT from the session user is a block.
	block := `[{"kind":"revoke_privilege","target":{"type":"user","name":"admin"},"access":["SELECT"]},{"kind":"create_row_policy","row_policy":{"name":"p","database":"d","table":"t","using":"1","restrictive":true,"to":{"all":true}}}]`
	rec := call("POST", "/plan", `{"ops":`+block+`}`)
	var pr planResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &pr)
	if !pr.Plan.Blocked() {
		t.Fatalf("expected a block: %+v", pr.Plan.Warnings)
	}
	if rec := call("POST", "/apply", `{"ops":`+block+`,"plan_hash":"`+pr.Plan.PlanHash+`"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("blocked apply: %d", rec.Code)
	}

	ack := `[{"kind":"create_row_policy","row_policy":{"name":"p","database":"d","table":"t","using":"1","to":{"names":["x"]}}}]`
	rec = call("POST", "/plan", `{"ops":`+ack+`}`)
	_ = json.Unmarshal(rec.Body.Bytes(), &pr)
	if pr.Plan.Blocked() || len(pr.Plan.Warnings) == 0 {
		t.Fatalf("expected ack warnings only: %+v", pr.Plan.Warnings)
	}
	if rec := call("POST", "/apply", `{"ops":`+ack+`,"plan_hash":"`+pr.Plan.PlanHash+`"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("unacknowledged apply: %d", rec.Code)
	}
	if len(fake.ddl) != 0 {
		t.Fatalf("refused apply ran DDL: %v", fake.ddl)
	}
	if rec := call("POST", "/apply", `{"ops":`+ack+`,"plan_hash":"`+pr.Plan.PlanHash+`","acknowledged":["row_policy_scope"]}`); rec.Code != http.StatusOK {
		t.Fatalf("acknowledged apply: %d %s", rec.Code, rec.Body.String())
	}
}
