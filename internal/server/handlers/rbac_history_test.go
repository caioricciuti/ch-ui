// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

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
	"github.com/caioricciuti/ch-ui/internal/database/dbtest"
	"github.com/caioricciuti/ch-ui/internal/rbac"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
	"github.com/caioricciuti/ch-ui/internal/testutil"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

// historyFake is fakeAccess that also carries out CREATE ROLE and DROP
// ROLE, and can sneak in a role of its own when one is created, as an
// outside change landing during an apply would.
type historyFake struct {
	*fakeAccess
	sneakOn string
}

func backtickName(sql, prefix string) string {
	rest := strings.TrimPrefix(sql, prefix)
	if i := strings.IndexByte(rest, '`'); i >= 0 {
		return rest[:i]
	}
	return rest
}

func (f *historyFake) respond(msg tunnel.GatewayMessage) *tunnel.AgentMessage {
	sql := msg.SQL
	if sql == "" {
		sql = msg.Query
	}
	f.mu.Lock()
	switch {
	case strings.HasPrefix(sql, "CREATE ROLE `"):
		name := backtickName(sql, "CREATE ROLE `")
		f.roles = append(f.roles, name)
		if name == f.sneakOn {
			f.roles = append(f.roles, "sneaky")
		}
	case strings.HasPrefix(sql, "DROP ROLE IF EXISTS `"):
		name := backtickName(sql, "DROP ROLE IF EXISTS `")
		kept := f.roles[:0]
		for _, r := range f.roles {
			if r != name {
				kept = append(kept, r)
			}
		}
		f.roles = kept
	}
	f.mu.Unlock()
	return f.fakeAccess.respond(msg)
}

func newHistoryTestHandler(t *testing.T) (*RBACHandler, *historyFake, func(method, path, body string) *httptest.ResponseRecorder) {
	t.Helper()
	db, err := dbtest.Open(t, "rbac_history.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	conn, err := db.CreateConnection(database.CreateConnectionParams{Name: "test", TunnelToken: "hist-token"})
	if err != nil {
		t.Fatal(err)
	}
	fake := &historyFake{fakeAccess: &fakeAccess{roles: []string{"existing"}, users: []string{"admin"}}}
	agent := testutil.NewAgent(t, db, "hist-token", fake.respond)
	cfg := &config.Config{AppSecretKey: "hist-secret"}
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
	return h, fake, call
}

type historyRowJSON struct {
	ID        string `json:"id"`
	Source    string `json:"source"`
	Drift     string `json:"drift"`
	PlanHash  string `json:"plan_hash"`
	SeenCount int    `json:"seen_count"`
	Actor     string `json:"actor"`
	Summary   struct {
		Added       map[string]int `json:"added"`
		Unexplained []string       `json:"unexplained"`
		RevertOf    string         `json:"revert_of"`
	} `json:"summary"`
}

func listHistory(t *testing.T, call func(method, path, body string) *httptest.ResponseRecorder, query string) ([]historyRowJSON, int) {
	t.Helper()
	rec := call("GET", "/history"+query, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("history: %d %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Snapshots []historyRowJSON `json:"snapshots"`
		Outside   int              `json:"outside_last_30d"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out.Snapshots, out.Outside
}

// applyOps plans and applies ops, acknowledging every warning.
func applyOps(t *testing.T, call func(method, path, body string) *httptest.ResponseRecorder, ops []rbac.Op, revertOf string) {
	t.Helper()
	opsJSON, _ := json.Marshal(ops)
	rec := call("POST", "/plan", `{"ops":`+string(opsJSON)+`}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("plan: %d %s", rec.Code, rec.Body.String())
	}
	var pr planResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &pr)
	acks := []string{}
	for _, w := range pr.Plan.Warnings {
		acks = append(acks, w.Code)
	}
	body, _ := json.Marshal(map[string]any{"ops": ops, "plan_hash": pr.Plan.PlanHash, "acknowledged": acks, "revert_of": revertOf})
	if rec := call("POST", "/apply", string(body)); rec.Code != http.StatusOK {
		t.Fatalf("apply: %d %s", rec.Code, rec.Body.String())
	}
}

func TestRBACHistoryCaptureAndRevert(t *testing.T) {
	h, fake, call := newHistoryTestHandler(t)

	// Viewing stores a row only when the state changed.
	for i := 0; i < 2; i++ {
		if rec := call("GET", "/snapshot", ""); rec.Code != http.StatusOK {
			t.Fatalf("snapshot: %d", rec.Code)
		}
	}
	rows, _ := listHistory(t, call, "")
	if len(rows) != 1 || rows[0].Source != "view" || rows[0].SeenCount != 2 || rows[0].Drift != "none" {
		t.Fatalf("after two views: %+v", rows)
	}
	baseline := rows[0].ID

	// A role created outside CH-UI is drift.
	fake.mu.Lock()
	fake.roles = append(fake.roles, "outside_role")
	fake.mu.Unlock()
	call("GET", "/snapshot", "")
	rows, outside := listHistory(t, call, "")
	if len(rows) != 2 || rows[0].Drift != "outside" || rows[0].Summary.Added["entity"] != 1 || outside != 1 {
		t.Fatalf("outside change: %+v outside=%d", rows, outside)
	}

	// An apply stores the state before (unchanged here, so the newest row
	// is bumped) and after, with the plan hash.
	applyOps(t, call, []rbac.Op{{Kind: rbac.OpCreateRole, Name: "chui_new"}}, "")
	rows, _ = listHistory(t, call, "")
	if len(rows) != 3 || rows[0].Source != "apply" || rows[0].Drift != "none" || rows[0].PlanHash == "" || rows[0].Actor != "ops@example.test" {
		t.Fatalf("after apply: %+v", rows)
	}
	if rows[1].SeenCount != 2 {
		t.Fatalf("the pre-apply read should bump the newest row: %+v", rows[1])
	}

	// A role appearing during the apply that the plan did not create makes
	// the apply row mixed.
	fake.mu.Lock()
	fake.sneakOn = "mix_me"
	fake.mu.Unlock()
	applyOps(t, call, []rbac.Op{{Kind: rbac.OpCreateRole, Name: "mix_me"}}, "")
	rows, _ = listHistory(t, call, "")
	if rows[0].Drift != "mixed" || len(rows[0].Summary.Unexplained) != 1 || rows[0].Summary.Unexplained[0] != "role:sneaky" {
		t.Fatalf("mixed apply: %+v", rows[0])
	}
	if drift, _ := listHistory(t, call, "?drift=only"); len(drift) != 2 {
		t.Fatalf("drift only: %+v", drift)
	}

	// The diff against previous names the change.
	rec := call("GET", "/history/"+rows[0].ID+"/diff", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"grantee":"mix_me"`) {
		t.Fatalf("diff: %d %s", rec.Code, rec.Body.String())
	}

	// Revert to the baseline: drop the four roles added since.
	rec = call("POST", "/history/"+baseline+"/revert-plan", `{}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("revert-plan: %d %s", rec.Code, rec.Body.String())
	}
	var rp struct {
		Ops          []rbac.Op         `json:"ops"`
		Unrevertable []json.RawMessage `json:"unrevertable"`
		OverCap      bool              `json:"over_cap"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &rp)
	dropped := []string{}
	for _, op := range rp.Ops {
		if op.Kind == rbac.OpDropRole {
			dropped = append(dropped, op.Name)
		}
	}
	if strings.Join(dropped, ",") != "chui_new,mix_me,outside_role,sneaky" || len(rp.Unrevertable) != 0 || rp.OverCap {
		t.Fatalf("revert plan = %s", rec.Body.String())
	}
	// Limited to one entity.
	rec = call("POST", "/history/"+baseline+"/revert-plan", `{"only":["sneaky"]}`)
	_ = json.Unmarshal(rec.Body.Bytes(), &rp)
	if len(rp.Ops) != 1 || rp.Ops[0].Name != "sneaky" {
		t.Fatalf("only sneaky: %s", rec.Body.String())
	}
	if rec := call("POST", "/history/"+baseline+"/revert-plan", `{"sql":"x"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown field: %d", rec.Code)
	}
	if rec := call("POST", "/history/nope/revert-plan", `{}`); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown id: %d", rec.Code)
	}

	revertOps := []rbac.Op{}
	for _, n := range dropped {
		revertOps = append(revertOps, rbac.Op{Kind: rbac.OpDropRole, Name: n})
	}
	applyOps(t, call, revertOps, baseline)
	rows, _ = listHistory(t, call, "")
	if rows[0].Source != "revert" || rows[0].Summary.RevertOf != baseline || rows[0].Drift != "none" {
		t.Fatalf("revert row: %+v", rows[0])
	}
	rec = call("GET", "/history/"+baseline+"/diff?against=live", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"changes":[]`) {
		t.Fatalf("baseline vs live after revert: %s", rec.Body.String())
	}
	if rec := call("POST", "/apply", `{"ops":[],"revert_of":"nope"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown revert_of: %d", rec.Code)
	}

	logs, _ := h.DB.GetAuditLogs(50)
	prepared := 0
	for _, l := range logs {
		if l.Action == "rbac.history.revert_prepared" {
			prepared++
		}
	}
	if prepared != 2 {
		t.Fatalf("revert_prepared audit rows = %d", prepared)
	}
}

func TestRBACHistoryGrace(t *testing.T) {
	h, _, call := newHistoryTestHandler(t)
	rec := call("POST", "/history/snapshot", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"created":true`) || !strings.Contains(rec.Body.String(), `"source":"manual"`) {
		t.Fatalf("take snapshot: %d %s", rec.Code, rec.Body.String())
	}
	var taken struct {
		Snapshot historyRowJSON `json:"snapshot"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &taken)

	h.Config.SetProAccessForTest(func() config.ProAccess { return config.ProGrace })
	for _, path := range []string{"/history", "/history/" + taken.Snapshot.ID, "/history/" + taken.Snapshot.ID + "/diff"} {
		if rec := call("GET", path, ""); rec.Code != http.StatusOK {
			t.Fatalf("grace GET %s: %d %s", path, rec.Code, rec.Body.String())
		}
	}
	if rec := call("POST", "/history/snapshot", ""); rec.Code != http.StatusPaymentRequired {
		t.Fatalf("grace take snapshot: %d", rec.Code)
	}
	if rec := call("POST", "/history/"+taken.Snapshot.ID+"/revert-plan", `{}`); rec.Code != http.StatusOK {
		t.Fatalf("grace revert-plan: %d %s", rec.Code, rec.Body.String())
	}
}
