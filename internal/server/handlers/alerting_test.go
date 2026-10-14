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
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/database/dbtest"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
)

func serveAsUser(router http.Handler, role, user, connectionID, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(middleware.SetSession(req.Context(), &middleware.SessionInfo{UserRole: role, ClickhouseUser: user, ConnectionID: connectionID}))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestAlertingRoles(t *testing.T) {
	db, err := dbtest.Open(t, "alerting.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	conn, err := db.CreateConnection(database.CreateConnectionParams{Name: "c", TunnelToken: "c"})
	if err != nil {
		t.Fatal(err)
	}
	router := (&AlertingHandler{DB: db, Config: &config.Config{}}).Routes()

	// Health thresholds: everyone reads, only admins write.
	if rec := serveAs(router, "viewer", conn, http.MethodGet, "/health/settings", ""); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"check_kind":"replication_lag"`) {
		t.Fatalf("viewer GET /health/settings: %d %s", rec.Code, rec.Body.String())
	}
	body := `{"settings":[{"check_kind":"disk","enabled":true,"warn_threshold":70,"crit_threshold":85,"for_evaluations":3,"resolve_evaluations":2}]}`
	if rec := serveAs(router, "analyst", conn, http.MethodPut, "/health/settings", body); rec.Code != http.StatusForbidden {
		t.Fatalf("analyst PUT /health/settings: %d", rec.Code)
	}
	if rec := serveAs(router, "admin", conn, http.MethodPut, "/health/settings", `{"settings":[{"check_kind":"disk","enabled":true,"warn_threshold":95,"crit_threshold":85,"for_evaluations":1,"resolve_evaluations":1}]}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("warn above crit accepted: %d", rec.Code)
	}
	rec := serveAs(router, "admin", conn, http.MethodPut, "/health/settings", body)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"crit_threshold":85`) {
		t.Fatalf("admin PUT /health/settings: %d %s", rec.Code, rec.Body.String())
	}
	if rec := serveAs(router, "viewer", conn, http.MethodGet, "/health/state", ""); rec.Code != http.StatusOK {
		t.Fatalf("viewer GET /health/state: %d", rec.Code)
	}

	// SQL alerts: writers create, only the creator or an admin changes.
	alert := `{"name":"errors","sql":"SELECT count() FROM system.errors","mode":"value","comparator":"gt","threshold":0,"severity":"warn","interval_seconds":300,"timeout_seconds":30,"enabled":true}`
	if rec := serveAs(router, "viewer", conn, http.MethodPost, "/sql", alert); rec.Code != http.StatusForbidden {
		t.Fatalf("viewer POST /sql: %d", rec.Code)
	}
	for _, bad := range []string{
		strings.Replace(alert, "SELECT count() FROM system.errors", "DROP TABLE t", 1),
		strings.Replace(alert, "SELECT count() FROM system.errors", "SELECT 1; SELECT 2", 1),
		strings.Replace(alert, "system.errors", "system.errors SETTINGS max_threads = 99", 1),
		strings.Replace(alert, "system.errors", "system.errors FORMAT CSV", 1),
		strings.Replace(alert, `"interval_seconds":300`, `"interval_seconds":10`, 1),
	} {
		if rec := serveAsUser(router, "analyst", "ann", conn, http.MethodPost, "/sql", bad); rec.Code != http.StatusBadRequest {
			t.Errorf("invalid alert accepted (%d): %s", rec.Code, bad)
		}
	}
	rec = serveAsUser(router, "analyst", "ann", conn, http.MethodPost, "/sql", alert)
	if rec.Code != http.StatusCreated {
		t.Fatalf("analyst POST /sql: %d %s", rec.Code, rec.Body.String())
	}
	var created struct {
		Alert database.SQLAlert `json:"alert"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil || created.Alert.ID == "" || created.Alert.CreatedBy == nil || *created.Alert.CreatedBy != "ann" {
		t.Fatalf("created alert %+v %v", created.Alert, err)
	}
	path := "/sql/" + created.Alert.ID
	if rec := serveAsUser(router, "analyst", "bob", conn, http.MethodPut, path, alert); rec.Code != http.StatusForbidden {
		t.Fatalf("other analyst PUT: %d", rec.Code)
	}
	if rec := serveAsUser(router, "analyst", "bob", conn, http.MethodDelete, path, ""); rec.Code != http.StatusForbidden {
		t.Fatalf("other analyst DELETE: %d", rec.Code)
	}
	if rec := serveAsUser(router, "admin", "root", conn, http.MethodPut, path, strings.Replace(alert, `"threshold":0`, `"threshold":5`, 1)); rec.Code != http.StatusOK {
		t.Fatalf("admin PUT: %d %s", rec.Code, rec.Body.String())
	}
	rec = serveAs(router, "viewer", conn, http.MethodGet, "/sql", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"threshold":5`) || !strings.Contains(rec.Body.String(), `"worker_mode":"session"`) {
		t.Fatalf("viewer GET /sql: %d %s", rec.Code, rec.Body.String())
	}
	if rec := serveAs(router, "viewer", "other-connection", http.MethodGet, "/sql", ""); !strings.Contains(rec.Body.String(), `"alerts":[]`) {
		t.Fatalf("alerts leak across connections: %s", rec.Body.String())
	}
	if rec := serveAs(router, "viewer", conn, http.MethodGet, "/evaluations?source=bad", ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad source: %d", rec.Code)
	}
	if rec := serveAsUser(router, "analyst", "ann", conn, http.MethodDelete, path, ""); rec.Code != http.StatusOK {
		t.Fatalf("creator DELETE: %d", rec.Code)
	}
	logs, err := db.GetAuditLogs(20)
	if err != nil {
		t.Fatal(err)
	}
	actions := map[string]bool{}
	for _, l := range logs {
		actions[l.Action] = true
	}
	for _, a := range []string{"health_alerts.settings_updated", "sql_alert.created", "sql_alert.updated", "sql_alert.deleted"} {
		if !actions[a] {
			t.Errorf("missing audit action %s in %v", a, actions)
		}
	}
}

// TestAlertingDisableResolvesIncidents checks that turning off a firing
// health check or SQL alert raises a resolved event instead of dropping the
// incident silently.
func TestAlertingDisableResolvesIncidents(t *testing.T) {
	db, err := dbtest.Open(t, "alerting.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	conn, err := db.CreateConnection(database.CreateConnectionParams{Name: "c", TunnelToken: "c"})
	if err != nil {
		t.Fatal(err)
	}
	router := (&AlertingHandler{DB: db, Config: &config.Config{}}).Routes()
	resolvedEvents := func(eventType string) int {
		t.Helper()
		evs, err := db.ListAlertEvents(50, eventType, "")
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, ev := range evs {
			if strings.HasPrefix(ev.Title, "[RESOLVED] ") {
				n++
			}
		}
		return n
	}

	crit := "critical"
	since := "2026-10-13T10:00:00Z"
	firedID := "fired-event"
	if err := db.SaveHealthAlertStates([]database.HealthAlertState{{ConnectionID: conn, CheckKind: "disk", Node: "ch-1", State: "firing", Severity: &crit, Since: &since, LastEventID: &firedID}}); err != nil {
		t.Fatal(err)
	}
	body := `{"settings":[{"check_kind":"disk","enabled":false,"warn_threshold":80,"crit_threshold":90,"for_evaluations":2,"resolve_evaluations":2}]}`
	if rec := serveAs(router, "admin", conn, http.MethodPut, "/health/settings", body); rec.Code != http.StatusOK {
		t.Fatalf("disable disk check: %d %s", rec.Code, rec.Body.String())
	}
	if n := resolvedEvents("health.disk"); n != 1 {
		t.Fatalf("want 1 resolved health event, got %d", n)
	}
	if states, _ := db.ListHealthAlertStates(conn, false); len(states) != 0 {
		t.Fatalf("disabled check kept state: %+v", states)
	}

	alert := `{"name":"errors","sql":"SELECT count() FROM system.errors","mode":"value","comparator":"gt","threshold":0,"severity":"warn","interval_seconds":300,"timeout_seconds":30,"enabled":true}`
	rec := serveAsUser(router, "admin", "root", conn, http.MethodPost, "/sql", alert)
	var created struct {
		Alert database.SQLAlert `json:"alert"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil || created.Alert.ID == "" {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	if _, err := db.Conn().Exec(`UPDATE sql_alerts SET state = 'firing' WHERE id = ?`, created.Alert.ID); err != nil {
		t.Fatal(err)
	}
	path := "/sql/" + created.Alert.ID
	if rec := serveAsUser(router, "admin", "root", conn, http.MethodPut, path, strings.Replace(alert, `"enabled":true`, `"enabled":false`, 1)); rec.Code != http.StatusOK {
		t.Fatalf("disable SQL alert: %d %s", rec.Code, rec.Body.String())
	}
	if n := resolvedEvents("sql.alert"); n != 1 {
		t.Fatalf("want 1 resolved SQL event, got %d", n)
	}
	if a, _ := db.GetSQLAlert(created.Alert.ID); a == nil || a.State != "ok" || a.Enabled {
		t.Fatalf("disabled alert state: %+v", a)
	}
}
