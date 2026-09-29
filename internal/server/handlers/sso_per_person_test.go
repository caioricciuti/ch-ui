// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package handlers

import (
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
	"github.com/go-chi/chi/v5"
)

// Two SSO people (alice, bob) share one ClickHouse service account, svc_sso,
// and a password user logs in as svc_sso itself. Per-person data (history,
// Brain chats and approvals, dashboard stars) must stay apart between all
// three, and audit rows must name the person with the account in ch_user.

const ssoServiceAccount = "svc_sso"

type ssoPeople struct {
	alice, bob, password *middleware.SessionInfo
}

func ssoFixture(t *testing.T) (*database.DB, ssoPeople) {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "sso.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	connection, err := db.CreateConnection(database.CreateConnectionParams{Name: "sso", TunnelToken: "sso"})
	if err != nil {
		t.Fatal(err)
	}
	person := func(subject string) *middleware.SessionInfo {
		return &middleware.SessionInfo{ConnectionID: connection, UserRole: "analyst", ClickhouseUser: ssoServiceAccount, AuthSubject: subject}
	}
	return db, ssoPeople{alice: person("alice@example.com"), bob: person("bob@example.com"), password: person("")}
}

func ssoRequest(r chi.Router, session *middleware.SessionInfo, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if session != nil {
		req = req.WithContext(middleware.SetSession(req.Context(), session))
	}
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	return rr
}

func listHistoryQueries(t *testing.T, r chi.Router, session *middleware.SessionInfo) []string {
	t.Helper()
	res := ssoRequest(r, session, "GET", "/history/", "")
	if res.Code != 200 {
		t.Fatalf("list history: %d %s", res.Code, res.Body)
	}
	var out struct {
		Entries []database.QueryHistoryEntry `json:"entries"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	queries := make([]string, 0, len(out.Entries))
	for _, e := range out.Entries {
		queries = append(queries, e.QueryText)
	}
	return queries
}

func TestQueryHistorySSOPeopleIsolated(t *testing.T) {
	db, p := ssoFixture(t)
	q := &QueryHandler{DB: db}
	// recordQueryHistory is what the editor stream calls; it writes async.
	q.recordQueryHistory(p.alice.ConnectionID, p.alice.ClickhouseUser, middleware.Actor(p.alice), "SELECT 'alice'", "success", "", 1, 1)
	q.recordQueryHistory(p.bob.ConnectionID, p.bob.ClickhouseUser, middleware.Actor(p.bob), "SELECT 'bob'", "success", "", 1, 1)
	q.recordQueryHistory(p.password.ConnectionID, p.password.ClickhouseUser, middleware.Actor(p.password), "SELECT 'password'", "success", "", 1, 1)
	if _, err := db.Conn().Exec(`INSERT INTO query_history (id, connection_id, clickhouse_user, actor, query_text, status) VALUES ('shared', ?, ?, ?, 'SELECT ''shared''', 'success')`,
		p.alice.ConnectionID, ssoServiceAccount, database.SharedSSOHistoryActor); err != nil {
		t.Fatal(err)
	}
	waitForRows(t, db, "SELECT COUNT(*) FROM query_history", 4)

	h := &QueryHistoryHandler{DB: db}
	r := chi.NewRouter()
	r.Route("/history", h.Routes)

	for _, c := range []struct {
		who  *middleware.SessionInfo
		want string
	}{{p.alice, "SELECT 'alice'"}, {p.bob, "SELECT 'bob'"}, {p.password, "SELECT 'password'"}} {
		if got := listHistoryQueries(t, r, c.who); len(got) != 1 || got[0] != c.want {
			t.Fatalf("%s sees %v, want only %q", middleware.Actor(c.who), got, c.want)
		}
	}

	var aliceID string
	if err := db.Conn().QueryRow(`SELECT id FROM query_history WHERE actor = 'alice@example.com'`).Scan(&aliceID); err != nil {
		t.Fatal(err)
	}
	// Bob deletes alice's row by id and clears his own: alice, the password
	// user and the shared row all survive.
	if res := ssoRequest(r, p.bob, "DELETE", "/history/"+aliceID, ""); res.Code != 200 {
		t.Fatalf("delete: %d %s", res.Code, res.Body)
	}
	if res := ssoRequest(r, p.bob, "DELETE", "/history/", ""); res.Code != 200 {
		t.Fatalf("clear: %d %s", res.Code, res.Body)
	}
	if got := listHistoryQueries(t, r, p.alice); len(got) != 1 {
		t.Fatalf("bob's delete/clear removed alice's history: %v", got)
	}
	if got := listHistoryQueries(t, r, p.password); len(got) != 1 {
		t.Fatalf("bob's clear removed the password user's history: %v", got)
	}
	if got := listHistoryQueries(t, r, p.bob); len(got) != 0 {
		t.Fatalf("bob's clear left %v", got)
	}
	waitForRows(t, db, "SELECT COUNT(*) FROM query_history WHERE id = 'shared'", 1)
}

func waitForRows(t *testing.T, db *database.DB, query string, want int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		var n int
		if err := db.Conn().QueryRow(query).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s: got %d rows, want %d", query, n, want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestBrainChatSSOPeopleIsolated(t *testing.T) {
	db, p := ssoFixture(t)
	h := &BrainHandler{DB: db}
	r := chi.NewRouter()
	r.Route("/brain", h.Routes)

	res := ssoRequest(r, p.alice, "POST", "/brain/chats", `{"title":"alice plans"}`)
	if res.Code != 201 {
		t.Fatalf("create chat: %d %s", res.Code, res.Body)
	}
	var created struct {
		Chat database.BrainChat `json:"chat"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	chatID := created.Chat.ID
	if created.Chat.Username != "alice@example.com" {
		t.Fatalf("chat owner %q, want alice@example.com", created.Chat.Username)
	}

	for _, who := range []*middleware.SessionInfo{p.bob, p.password} {
		for _, c := range []struct{ method, path, body string }{
			{"GET", "/brain/chats/" + chatID, ""},
			{"PUT", "/brain/chats/" + chatID, `{"title":"hijacked"}`},
			{"GET", "/brain/chats/" + chatID + "/messages", ""},
			{"GET", "/brain/chats/" + chatID + "/artifacts", ""},
			{"DELETE", "/brain/chats/" + chatID, ""},
		} {
			if res := ssoRequest(r, who, c.method, c.path, c.body); res.Code != 404 {
				t.Fatalf("%s %s %s: got %d, want 404", middleware.Actor(who), c.method, c.path, res.Code)
			}
		}
		res := ssoRequest(r, who, "GET", "/brain/chats", "")
		var list struct {
			Chats []database.BrainChat `json:"chats"`
		}
		if err := json.Unmarshal(res.Body.Bytes(), &list); err != nil {
			t.Fatal(err)
		}
		if len(list.Chats) != 0 {
			t.Fatalf("%s lists alice's chats: %+v", middleware.Actor(who), list.Chats)
		}
	}

	res = ssoRequest(r, p.alice, "GET", "/brain/chats/"+chatID, "")
	if res.Code != 200 || !strings.Contains(res.Body.String(), "alice plans") {
		t.Fatalf("alice lost her chat or it was renamed: %d %s", res.Code, res.Body)
	}
}

func TestBrainApprovalOnlyRequesterDecides(t *testing.T) {
	db, p := ssoFixture(t)
	h := &BrainHandler{DB: db}
	r := chi.NewRouter()
	r.Route("/brain", h.Routes)

	if err := db.CreateBrainApproval("appr-1", "chat-1", "msg-1", "tc-1", "create_dashboard", "{}", middleware.Actor(p.alice)); err != nil {
		t.Fatal(err)
	}
	for _, who := range []*middleware.SessionInfo{p.bob, p.password} {
		for _, verb := range []string{"approve", "decline"} {
			if res := ssoRequest(r, who, "POST", "/brain/approvals/appr-1/"+verb, ""); res.Code != 404 {
				t.Fatalf("%s %s: got %d, want 404", middleware.Actor(who), verb, res.Code)
			}
		}
	}
	a, err := db.GetBrainApprovalByID("appr-1")
	if err != nil || a == nil || a.Status != "pending" {
		t.Fatalf("approval changed by someone else: %+v %v", a, err)
	}
	if res := ssoRequest(r, p.alice, "POST", "/brain/approvals/appr-1/approve", ""); res.Code != 200 {
		t.Fatalf("alice approve: %d %s", res.Code, res.Body)
	}
	a, _ = db.GetBrainApprovalByID("appr-1")
	if a.Status != "approved" || a.DecidedBy == nil || *a.DecidedBy != "alice@example.com" {
		t.Fatalf("decision not attributed to alice: %+v", a)
	}
	if res := ssoRequest(r, p.alice, "POST", "/brain/approvals/appr-1/approve", ""); res.Code != 409 {
		t.Fatalf("second decision: got %d, want 409", res.Code)
	}
}

func dashboardStarred(t *testing.T, r chi.Router, who *middleware.SessionInfo, id string) bool {
	t.Helper()
	res := ssoRequest(r, who, "GET", "/"+id+"/", "")
	if res.Code != 200 {
		t.Fatalf("get dashboard: %d %s", res.Code, res.Body)
	}
	var out struct {
		Dashboard database.Dashboard `json:"dashboard"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out.Dashboard.Starred
}

func TestDashboardStarsAndAuthorSSOPeople(t *testing.T) {
	db, p := ssoFixture(t)
	h := &DashboardsHandler{DB: db}
	r := h.Routes()

	res := ssoRequest(r, p.alice, "POST", "/", `{"name":"Alice board"}`)
	if res.Code != 201 {
		t.Fatalf("create dashboard: %d %s", res.Code, res.Body)
	}
	var created struct {
		Dashboard database.Dashboard `json:"dashboard"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	id := created.Dashboard.ID
	if created.Dashboard.CreatedBy == nil || *created.Dashboard.CreatedBy != "alice@example.com" {
		t.Fatalf("created_by = %v, want alice@example.com", created.Dashboard.CreatedBy)
	}

	logs, err := db.GetAuditLogs(50)
	if err != nil {
		t.Fatal(err)
	}
	var audited bool
	for _, l := range logs {
		if l.Action == "dashboard.created" {
			audited = true
			if l.Username == nil || *l.Username != "alice@example.com" || l.ChUser == nil || *l.ChUser != ssoServiceAccount {
				t.Fatalf("audit username=%v ch_user=%v, want alice@example.com / %s", l.Username, l.ChUser, ssoServiceAccount)
			}
		}
	}
	if !audited {
		t.Fatal("no dashboard.created audit row")
	}

	if res := ssoRequest(r, p.alice, "POST", "/"+id+"/star", ""); res.Code != 200 {
		t.Fatalf("star: %d %s", res.Code, res.Body)
	}
	if !dashboardStarred(t, r, p.alice, id) {
		t.Fatal("alice's star not visible to alice")
	}
	if dashboardStarred(t, r, p.bob, id) || dashboardStarred(t, r, p.password, id) {
		t.Fatal("alice's star leaked to someone sharing her ClickHouse account")
	}

	// Bob unstarring does not remove alice's star.
	if res := ssoRequest(r, p.bob, "DELETE", "/"+id+"/star", ""); res.Code != 200 {
		t.Fatalf("unstar: %d %s", res.Code, res.Body)
	}
	if !dashboardStarred(t, r, p.alice, id) {
		t.Fatal("bob's unstar removed alice's star")
	}
}

// The Brain audit log lists every person's approvals, so only admins may read it.
func TestBrainAuditIsAdminOnly(t *testing.T) {
	db, p := ssoFixture(t)
	h := &BrainHandler{DB: db}
	r := chi.NewRouter()
	r.Route("/brain", h.Routes)

	if err := db.CreateBrainApproval("appr-audit", "chat-1", "msg-1", "tc-1", "create_dashboard", "{}", middleware.Actor(p.alice)); err != nil {
		t.Fatal(err)
	}
	for _, role := range []string{"viewer", "analyst"} {
		s := *p.bob
		s.UserRole = role
		if res := ssoRequest(r, &s, "GET", "/brain/audit", ""); res.Code != 403 {
			t.Fatalf("%s: got %d, want 403", role, res.Code)
		}
	}
	admin := *p.bob
	admin.UserRole = "admin"
	res := ssoRequest(r, &admin, "GET", "/brain/audit", "")
	if res.Code != 200 || !strings.Contains(res.Body.String(), "appr-audit") {
		t.Fatalf("admin: got %d %s", res.Code, res.Body.String())
	}
}
