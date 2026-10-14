// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package handlers

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/database/dbtest"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
)

func TestReportsHandlerValidationAndRoles(t *testing.T) {
	db, err := dbtest.Open(t, "reports.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	conn, err := db.CreateConnection(database.CreateConnectionParams{Name: "c", TunnelToken: "t"})
	if err != nil {
		t.Fatal(err)
	}
	noDefault, _ := db.CreateSavedQuery(database.CreateSavedQueryParams{Name: "p", Query: "SELECT {day:Date}", ConnectionID: conn})
	plain, _ := db.CreateSavedQuery(database.CreateSavedQueryParams{Name: "q", Query: "SELECT 1", ConnectionID: conn})
	channel, _ := db.CreateAlertChannel("mail", "smtp", "enc", true, "admin")
	h := (&ReportsHandler{DB: db, Config: &config.Config{AppSecretKey: "s"}}).Routes()

	do := func(method, path, user, role, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req = req.WithContext(middleware.SetSession(req.Context(), &middleware.SessionInfo{ConnectionID: conn, ClickhouseUser: user, UserRole: role}))
		res := httptest.NewRecorder()
		h.ServeHTTP(res, req)
		return res
	}
	body := func(query, recipient string) string {
		return fmt.Sprintf(`{"name":"Daily","source_type":"saved_query","saved_query_id":%q,"connection_id":%q,"cron":"0 8 * * *","targets":[{"channel_id":%q,"recipients":[%q]}]}`, query, conn, channel, recipient)
	}

	if res := do("POST", "/", "v", "viewer", body(plain, "a@example.com")); res.Code != 403 {
		t.Fatalf("viewer create: %d", res.Code)
	}
	if res := do("POST", "/", "a", "analyst", body(noDefault, "a@example.com")); res.Code != 400 || !strings.Contains(res.Body.String(), "no default") {
		t.Fatalf("parameter without default accepted: %d %s", res.Code, res.Body.String())
	}
	if err := db.SetSetting("report_recipient_domains", "example.com"); err != nil {
		t.Fatal(err)
	}
	if res := do("POST", "/", "a", "analyst", body(plain, "x@elsewhere.org")); res.Code != 400 || !strings.Contains(res.Body.String(), "outside") {
		t.Fatalf("analyst sent outside the domains: %d %s", res.Code, res.Body.String())
	}
	res := do("POST", "/", "a", "analyst", body(plain, "a@example.com"))
	if res.Code != 201 {
		t.Fatalf("create: %d %s", res.Code, res.Body.String())
	}
	var created struct {
		Report database.ReportSubscription `json:"report"`
	}
	_ = json.Unmarshal(res.Body.Bytes(), &created)
	if created.Report.NextRunAt == nil || created.Report.Format != "csv" || created.Report.RowLimit != 1000 {
		t.Fatalf("defaults not applied: %+v", created.Report)
	}

	// Only the creator and admins see a report.
	if res := do("GET", "/", "v", "viewer", ""); res.Code != 200 || strings.Contains(res.Body.String(), "Daily") {
		t.Fatalf("viewer list: %d %s", res.Code, res.Body.String())
	}
	if res := do("GET", "/", "a", "analyst", ""); res.Code != 200 || !strings.Contains(res.Body.String(), "Daily") {
		t.Fatalf("creator list: %d", res.Code)
	}
	// Another analyst may not change it (and cannot tell it exists); an admin may.
	if res := do("DELETE", "/"+created.Report.ID, "b", "analyst", ""); res.Code != 404 {
		t.Fatalf("other analyst delete: %d", res.Code)
	}
	if res := do("DELETE", "/"+created.Report.ID, "root", "admin", ""); res.Code != 200 {
		t.Fatalf("admin delete: %d %s", res.Code, res.Body.String())
	}
	var n int
	_ = db.Conn().QueryRow(`SELECT count(*) FROM audit_logs WHERE action IN ('report.created', 'report.deleted')`).Scan(&n)
	if n != 2 {
		t.Fatalf("audits: %d", n)
	}
	if res := do("PUT", "/settings", "a", "analyst", `{"recipient_domains":[]}`); res.Code != 403 {
		t.Fatalf("analyst changed settings: %d", res.Code)
	}
}

// A report's output comes from the connection's report account, so its
// deliveries, CSV files and previews are the creator's and admins' only.
// Anyone else gets 404, as if the report did not exist.
func TestReportOutputIsCreatorOrAdminOnly(t *testing.T) {
	db, err := dbtest.Open(t, "reports-access.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	conn, err := db.CreateConnection(database.CreateConnectionParams{Name: "c", TunnelToken: "t"})
	if err != nil {
		t.Fatal(err)
	}
	owner := "alice"
	id, err := db.CreateReportSubscription(database.ReportSubscription{
		Name: "Revenue", SourceType: "saved_query", ConnectionID: conn, Cron: "0 8 * * *", Timezone: "UTC",
		Format: "csv", RowLimit: 10, InlineRows: 5, MaxBytes: 1 << 20, TimeoutSeconds: 30, CreatedBy: &owner,
	})
	if err != nil {
		t.Fatal(err)
	}
	delivery, err := db.CreateReportDelivery(id, "manual", "report", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	snap, _ := json.Marshal(map[string]interface{}{"csv": []byte("secret,1\n"), "csv_name": "r.csv"})
	if err := db.FinishReportDelivery(delivery, database.ReportDeliveryResult{Status: "delivered", Snapshot: snap, Finished: true}, time.Now()); err != nil {
		t.Fatal(err)
	}
	h := (&ReportsHandler{DB: db, Config: &config.Config{AppSecretKey: "s"}}).Routes()
	do := func(method, path, user, role string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, nil)
		req = req.WithContext(middleware.SetSession(req.Context(), &middleware.SessionInfo{ConnectionID: conn, ClickhouseUser: user, UserRole: role}))
		res := httptest.NewRecorder()
		h.ServeHTTP(res, req)
		return res
	}

	// Without a runner, preview and send stop at 503 once access passes.
	routes := []struct {
		method, path string
		ok           int
	}{
		{"GET", "/deliveries/" + delivery + "/download", 200},
		{"GET", "/" + id + "/deliveries", 200},
		{"POST", "/" + id + "/preview", 503},
		{"POST", "/" + id + "/send", 503},
	}
	for _, rt := range routes {
		for _, who := range []struct{ user, role string }{{"bob", "analyst"}, {"bob", "admin"}, {"alice", "analyst"}, {"carol", "viewer"}} {
			res := do(rt.method, rt.path, who.user, who.role)
			want := rt.ok
			switch {
			case who.role == "viewer" && rt.method == "POST":
				want = 403 // writer gate comes first
			case who.user != owner && who.role != "admin":
				want = 404
			}
			if res.Code != want {
				t.Fatalf("%s %s as %s/%s: got %d want %d: %s", rt.method, rt.path, who.user, who.role, res.Code, want, res.Body.String())
			}
			if want == 404 && strings.Contains(res.Body.String(), "secret") {
				t.Fatalf("%s %s leaked output to %s", rt.method, rt.path, who.user)
			}
		}
	}
	if res := do("GET", "/deliveries/"+delivery+"/download", owner, "analyst"); res.Body.String() != "secret,1\n" {
		t.Fatalf("owner download: %q", res.Body.String())
	}
}

// A report runs with the report account, so it may only be built on a saved
// query the caller can open.
func TestReportRefusesAnotherUsersPrivateSavedQuery(t *testing.T) {
	db, err := dbtest.Open(t, "reports-sq.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	conn, err := db.CreateConnection(database.CreateConnectionParams{Name: "c", TunnelToken: "t"})
	if err != nil {
		t.Fatal(err)
	}
	private, err := db.CreateSavedQuery(database.CreateSavedQueryParams{Name: "mine", Query: "SELECT 1", ConnectionID: conn, CreatedBy: "alice", Visibility: database.SavedQueryPrivate})
	if err != nil {
		t.Fatal(err)
	}
	channel, _ := db.CreateAlertChannel("mail", "smtp", "enc", true, "admin")
	h := (&ReportsHandler{DB: db, Config: &config.Config{AppSecretKey: "s"}}).Routes()
	create := func(user, role string) *httptest.ResponseRecorder {
		body := fmt.Sprintf(`{"name":"R","source_type":"saved_query","saved_query_id":%q,"connection_id":%q,"cron":"0 8 * * *","targets":[{"channel_id":%q,"recipients":["a@example.com"]}]}`, private, conn, channel)
		req := httptest.NewRequest("POST", "/", strings.NewReader(body))
		req = req.WithContext(middleware.SetSession(req.Context(), &middleware.SessionInfo{ConnectionID: conn, ClickhouseUser: user, UserRole: role}))
		res := httptest.NewRecorder()
		h.ServeHTTP(res, req)
		return res
	}
	if res := create("bob", "analyst"); res.Code != 400 || !strings.Contains(res.Body.String(), "Saved query not found") {
		t.Fatalf("other analyst built a report on a private query: %d %s", res.Code, res.Body.String())
	}
	if res := create("alice", "analyst"); res.Code != 201 {
		t.Fatalf("owner create: %d %s", res.Code, res.Body.String())
	}
}
