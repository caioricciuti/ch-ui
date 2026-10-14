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
	"time"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/rbac"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
)

type importPlanResponse struct {
	Ops     []rbac.Op      `json:"ops"`
	Skipped []rbac.Skip    `json:"skipped"`
	Summary map[string]int `json:"summary"`
	OverCap bool           `json:"over_cap"`
}

// One handler for every case: the handlers package is close to the CI
// time limit, so this test opens a single database.
func TestRBACAccessFile(t *testing.T) {
	h, db, fake, call := newRBACTestHandler(t)
	fake.mu.Lock()
	fake.users = []string{"admin", "alice"}
	fake.mu.Unlock()

	// Export in every format, with no authentication data.
	for _, format := range []string{"yaml", "json", "sql"} {
		rec := call("GET", "/export?format="+format, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("export %s: %d %s", format, rec.Code, rec.Body.String())
		}
		want := `attachment; filename="chui-access-test-` + time.Now().UTC().Format("2006-01-02") + "." + format + `"`
		if got := rec.Header().Get("Content-Disposition"); got != want {
			t.Fatalf("export %s disposition = %q", format, got)
		}
		body := rec.Body.String()
		if !strings.Contains(body, "existing") {
			t.Fatalf("export %s lacks the role:\n%s", format, body)
		}
		low := strings.ToLower(body)
		for _, secret := range []string{"identified", "sha256", "password:", `"password"`, "auth_type", "rbac-secret"} {
			if strings.Contains(low, secret) {
				t.Fatalf("export %s contains %s:\n%s", format, secret, body)
			}
		}
	}
	if rec := call("GET", "/export", ""); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "chui_access: 1") {
		t.Fatalf("default export: %d %s", rec.Code, rec.Body.String())
	}
	if rec := call("GET", "/export?format=xml", ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad format: %d", rec.Code)
	}

	// Import plan: a new role with a global grant; a grant on a database
	// the target does not have is skipped; a missing user is skipped.
	file := "chui_access: 1\nsource: {connection: prod}\nroles:\n  existing: {}\n  chui_new: {}\nusers:\n  ghost: {default_roles: none}\ngrants:\n  chui_new:\n    - {access: [SELECT], on: \"*.*\"}\n    - {access: [INSERT], on: \"nodb.*\"}\n"
	body, _ := json.Marshal(map[string]any{"file": file, "prune": map[string]bool{"roles": false}, "rename": map[string]any{"principals": map[string]string{}}})
	rec := call("POST", "/import/plan", string(body))
	if rec.Code != http.StatusOK {
		t.Fatalf("import plan: %d %s", rec.Code, rec.Body.String())
	}
	var ip importPlanResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &ip); err != nil {
		t.Fatal(err)
	}
	if len(ip.Ops) != 2 || ip.Ops[0].Kind != rbac.OpCreateRole || ip.Ops[1].Kind != rbac.OpGrantPrivilege || ip.Summary["roles"] != 1 || ip.Summary["grants"] != 1 || ip.OverCap {
		t.Fatalf("import plan = %+v", ip)
	}
	reasons := []string{}
	for _, s := range ip.Skipped {
		reasons = append(reasons, s.Kind+" "+s.Name+": "+s.Reason)
	}
	if got := strings.Join(reasons, "\n"); !strings.Contains(got, `database "nodb"`) || !strings.Contains(got, "user ghost: user not found") {
		t.Fatalf("skipped = %s", got)
	}
	// The ops go through the existing plan endpoint unchanged.
	opsJSON, _ := json.Marshal(ip.Ops)
	if rec := call("POST", "/plan", `{"ops":`+string(opsJSON)+`}`); rec.Code != http.StatusOK {
		t.Fatalf("plan of imported ops: %d %s", rec.Code, rec.Body.String())
	}
	if len(fake.ddl) != 0 {
		t.Fatalf("import plan ran DDL: %v", fake.ddl)
	}
	logs, err := db.GetAuditLogs(20)
	if err != nil {
		t.Fatal(err)
	}
	planned := 0
	for _, l := range logs {
		if l.Action != "rbac.import.planned" {
			continue
		}
		planned++
		if l.Details == nil || !strings.Contains(*l.Details, `"source_connection":"prod"`) || !strings.Contains(*l.Details, `"ops":2`) || strings.Contains(*l.Details, "chui_new") {
			t.Fatalf("audit details = %v", l.Details)
		}
	}
	if planned != 1 {
		t.Fatalf("rbac.import.planned rows = %d", planned)
	}

	// Client errors: unknown fields, a file with an unknown key (the path
	// is named), an unknown privilege (the entry is named), a bad kind and
	// an oversized body.
	for _, c := range []struct{ body, want string }{
		{`{"file":"chui_access: 1\n","sql":"DROP TABLE x"}`, "unknown field"},
		{`{"file":"chui_access: 1\n","prune":{"users":true}}`, "unknown field"},
		{`{"file":"chui_access: 1\nroles:\n  r: {pasword: x}\n"}`, "roles.r.pasword: unknown key"},
		{`{"file":"chui_access: 2\n"}`, "chui_access: version 2 is newer"},
		{`{"file":"chui_access: 1\nroles: {r: {}}\ngrants:\n  r:\n    - {access: [TRUNCATE], on: \"*.*\"}\n"}`, "grants of role r on *.*"},
		{`{"file":"chui_access: 1\n","kinds":["passwords"]}`, "unknown kind"},
		{`{"file":"` + strings.Repeat("x", 1<<20) + `"}`, "Invalid request body"},
	} {
		rec := call("POST", "/import/plan", c.body)
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), c.want) {
			t.Errorf("%.60s: %d %s", c.body, rec.Code, rec.Body.String())
		}
	}

	// Gating: viewers get 403; in grace both work; without a license 402.
	router := h.Routes()
	serve := func(role, method, path, body string) int {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req = req.WithContext(middleware.SetSession(req.Context(), &middleware.SessionInfo{UserRole: role, ClickhouseUser: "admin"}))
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec.Code
	}
	minimal := `{"file":"chui_access: 1\n"}`
	if code := serve("viewer", "GET", "/export", ""); code != http.StatusForbidden {
		t.Fatalf("viewer export: %d", code)
	}
	if code := serve("viewer", "POST", "/import/plan", minimal); code != http.StatusForbidden {
		t.Fatalf("viewer import: %d", code)
	}
	h.Config.SetProAccessForTest(func() config.ProAccess { return config.ProGrace })
	if rec := call("GET", "/export", ""); rec.Code != http.StatusOK {
		t.Fatalf("grace export: %d", rec.Code)
	}
	if rec := call("POST", "/import/plan", minimal); rec.Code != http.StatusOK {
		t.Fatalf("grace import plan: %d %s", rec.Code, rec.Body.String())
	}
	h.Config.SetProAccessForTest(func() config.ProAccess { return config.ProNone })
	if code := serve("admin", "GET", "/export", ""); code != http.StatusPaymentRequired {
		t.Fatalf("no license export: %d", code)
	}
	if code := serve("admin", "POST", "/import/plan", minimal); code != http.StatusPaymentRequired {
		t.Fatalf("no license import plan: %d", code)
	}
}

func TestAccessFilename(t *testing.T) {
	now := time.Date(2026, 10, 14, 23, 0, 0, 0, time.UTC)
	for in, want := range map[string]string{
		"prod":           "chui-access-prod-2026-10-14.yaml",
		`a "b"/c\d`:      "chui-access-a-b-c-d-2026-10-14.yaml",
		"":               "chui-access-connection-2026-10-14.yaml",
		"..":             "chui-access-connection-2026-10-14.yaml",
		"Prod EU (main)": "chui-access-Prod-EU-main-2026-10-14.yaml",
	} {
		if got := accessFilename(in, "yaml", now); got != want {
			t.Errorf("%q: %s", in, got)
		}
	}
}
