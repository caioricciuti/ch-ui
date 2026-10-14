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

	"github.com/caioricciuti/ch-ui/internal/alerts"
	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
	"github.com/caioricciuti/ch-ui/internal/testutil"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

// Run now records only for the creator or an admin, and only on an enabled
// alert. A disabled alert or another writer gets a dry run that changes no
// state and raises no event.
func TestRunSQLAlertRecordsOnlyForOwnerOnEnabled(t *testing.T) {
	db, conn := testutil.WorkerDB(t)
	agent := testutil.NewAgent(t, db, "worker-token", func(msg tunnel.GatewayMessage) *tunnel.AgentMessage {
		if msg.Type != "query" {
			return nil
		}
		return &tunnel.AgentMessage{Type: "query_result", Data: json.RawMessage(`[{"v":"5"}]`)}
	})
	cfg := &config.Config{AppSecretKey: "test-secret"}
	enc, err := crypto.Encrypt("pw", cfg.AppSecretKey)
	if err != nil {
		t.Fatal(err)
	}
	router := (&AlertingHandler{DB: db, Gateway: agent.Gateway, Config: cfg}).Routes()
	run := func(role, user, id string) (int, map[string]interface{}) {
		req := httptest.NewRequest(http.MethodPost, "/sql/"+id+"/run", nil)
		req = req.WithContext(middleware.SetSession(req.Context(), &middleware.SessionInfo{
			UserRole: role, ClickhouseUser: user, ConnectionID: conn, EncryptedPassword: enc,
		}))
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		var body map[string]interface{}
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		return rec.Code, body
	}
	events := func() int {
		evs, err := db.ListAlertEvents(50, alerts.EventTypeSQLAlert, "")
		if err != nil {
			t.Fatal(err)
		}
		return len(evs)
	}

	ann := "ann"
	id, err := db.CreateSQLAlert(&database.SQLAlert{
		ConnectionID: conn, Name: "errors", SQL: "SELECT 5 AS v", Mode: "value", Comparator: "gt", Threshold: 0,
		Severity: "warn", IntervalSeconds: 300, TimeoutSeconds: 30, ForEvaluations: 1, ResolveEvaluations: 1,
		Enabled: true, CreatedBy: &ann,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Another analyst: dry run.
	code, body := run("analyst", "bob", id)
	if code != http.StatusOK || body["recorded"] != false || body["firing"] != true || !strings.Contains(body["reason"].(string), "creator") {
		t.Fatalf("other analyst run: %d %v", code, body)
	}
	if a, _ := db.GetSQLAlert(id); a.State != alerts.StateOK || a.LastRunAt != nil || events() != 0 {
		t.Fatalf("dry run changed the alert: %+v, %d events", a, events())
	}

	// Disabled: the creator gets a dry run too.
	if _, err := db.Conn().Exec(`UPDATE sql_alerts SET enabled = 0 WHERE id = ?`, id); err != nil {
		t.Fatal(err)
	}
	code, body = run("analyst", "ann", id)
	if code != http.StatusOK || body["recorded"] != false || !strings.Contains(body["reason"].(string), "turned off") {
		t.Fatalf("disabled run: %d %v", code, body)
	}
	if a, _ := db.GetSQLAlert(id); a.State != alerts.StateOK || a.LastRunAt != nil || events() != 0 {
		t.Fatalf("run of a disabled alert changed it: %+v, %d events", a, events())
	}

	// Enabled, creator: recorded, fires.
	if _, err := db.Conn().Exec(`UPDATE sql_alerts SET enabled = 1 WHERE id = ?`, id); err != nil {
		t.Fatal(err)
	}
	code, body = run("analyst", "ann", id)
	if code != http.StatusOK || body["recorded"] != true || body["firing"] != true {
		t.Fatalf("creator run: %d %v", code, body)
	}
	if a, _ := db.GetSQLAlert(id); a.State != alerts.StateFiring || events() != 1 {
		t.Fatalf("creator run not recorded: %+v, %d events", a, events())
	}

	// A viewer cannot run at all.
	if code, _ := run("viewer", "vic", id); code != http.StatusForbidden {
		t.Fatalf("viewer run: %d", code)
	}
}
