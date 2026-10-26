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

// A report runs with its connection's report account, so it is built on the
// caller's connection and one on another connection is not found, even for
// its creator or an admin.
func TestReportsStayOnTheSessionConnection(t *testing.T) {
	db, err := dbtest.Open(t, "reports-scope.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	connA, err := db.CreateConnection(database.CreateConnectionParams{Name: "a", TunnelToken: "a"})
	if err != nil {
		t.Fatal(err)
	}
	connB, err := db.CreateConnection(database.CreateConnectionParams{Name: "b", TunnelToken: "b"})
	if err != nil {
		t.Fatal(err)
	}
	query, err := db.CreateSavedQuery(database.CreateSavedQueryParams{Name: "q", Query: "SELECT 1", ConnectionID: connB})
	if err != nil {
		t.Fatal(err)
	}
	channel, err := db.CreateAlertChannel("mail", "smtp", "enc", true, "admin")
	if err != nil {
		t.Fatal(err)
	}
	owner := "alice"
	theirs, err := db.CreateReportSubscription(database.ReportSubscription{
		Name: "On B", SourceType: "saved_query", SavedQueryID: &query, ConnectionID: connB, Cron: "0 8 * * *", Timezone: "UTC",
		Format: "csv", RowLimit: 10, InlineRows: 5, MaxBytes: 1 << 20, TimeoutSeconds: 30, CreatedBy: &owner,
	})
	if err != nil {
		t.Fatal(err)
	}
	delivery, err := db.CreateReportDelivery(theirs, "manual", "report", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	snap, _ := json.Marshal(map[string]interface{}{"csv": []byte("secret,1\n"), "csv_name": "r.csv"})
	if err := db.FinishReportDelivery(delivery, database.ReportDeliveryResult{Status: "delivered", Snapshot: snap, Finished: true}, time.Now()); err != nil {
		t.Fatal(err)
	}

	h := (&ReportsHandler{DB: db, Config: &config.Config{AppSecretKey: "s"}}).Routes()
	do := func(method, path, role, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req = req.WithContext(middleware.SetSession(req.Context(), &middleware.SessionInfo{ConnectionID: connA, ClickhouseUser: owner, UserRole: role}))
		res := httptest.NewRecorder()
		h.ServeHTTP(res, req)
		return res
	}
	body := func(conn string) string {
		return fmt.Sprintf(`{"name":"Daily","source_type":"saved_query","saved_query_id":%q,"connection_id":%q,"cron":"0 8 * * *","targets":[{"channel_id":%q,"recipients":["a@example.com"]}]}`, query, conn, channel)
	}

	for _, role := range []string{"analyst", "admin"} {
		res := do("GET", "/", role, "")
		if res.Code != 200 || strings.Contains(res.Body.String(), theirs) {
			t.Fatalf("%s list shows another connection's report: %d %s", role, res.Code, res.Body.String())
		}
		for _, rt := range []struct{ method, path, body string }{
			{"PUT", "/" + theirs, body(connB)},
			{"PUT", "/" + theirs, body("")},
			{"DELETE", "/" + theirs, ""},
			{"POST", "/" + theirs + "/preview", ""},
			{"POST", "/" + theirs + "/send", ""},
			{"GET", "/" + theirs + "/deliveries", ""},
			{"GET", "/deliveries/" + delivery + "/download", ""},
		} {
			res := do(rt.method, rt.path, role, rt.body)
			if res.Code != 404 || strings.Contains(res.Body.String(), "secret") {
				t.Fatalf("%s %s %s: %d %s, want 404", role, rt.method, rt.path, res.Code, res.Body.String())
			}
		}
	}
	if s, _ := db.GetReportSubscription(theirs); s == nil || s.ConnectionID != connB || s.Name != "On B" {
		t.Fatalf("report on another connection changed: %+v", s)
	}

	if res := do("POST", "/", "analyst", body(connB)); res.Code != 400 || !strings.Contains(res.Body.String(), "Switch to the other connection") {
		t.Fatalf("create on another connection: %d %s, want 400", res.Code, res.Body.String())
	}
	// An empty connection is the caller's.
	res := do("POST", "/", "analyst", body(""))
	if res.Code != 201 {
		t.Fatalf("create: %d %s", res.Code, res.Body.String())
	}
	var created struct {
		Report database.ReportSubscription `json:"report"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Report.ConnectionID != connA {
		t.Fatalf("new report on %q, want the session connection %q", created.Report.ConnectionID, connA)
	}
	if res := do("PUT", "/"+created.Report.ID, "analyst", body(connB)); res.Code != 400 {
		t.Fatalf("move to another connection: %d %s, want 400", res.Code, res.Body.String())
	}
	if s, _ := db.GetReportSubscription(created.Report.ID); s == nil || s.ConnectionID != connA {
		t.Fatalf("refused update moved the report: %+v", s)
	}
}
