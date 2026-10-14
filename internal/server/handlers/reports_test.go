// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package handlers

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
)

func TestReportsHandlerValidationAndRoles(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "reports.db"))
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

	if res := do("GET", "/", "v", "viewer", ""); res.Code != 200 || !strings.Contains(res.Body.String(), "Daily") {
		t.Fatalf("viewer list: %d", res.Code)
	}
	// Another analyst may not change it; an admin may.
	if res := do("DELETE", "/"+created.Report.ID, "b", "analyst", ""); res.Code != 403 {
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
