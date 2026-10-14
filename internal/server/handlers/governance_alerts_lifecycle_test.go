// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package handlers

import (
	"net/http"
	"strings"
	"testing"

	"github.com/caioricciuti/ch-ui/internal/alerts"
	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/database"
)

// A rule edit with a bad channel is refused before anything is written.
func TestUpdateAlertRuleBadChannelChangesNothing(t *testing.T) {
	db, _, connA, _, router := newGovernanceRoleRouter(t)
	ch, err := db.CreateAlertChannel("hook", alerts.ChannelTypeWebhook, "enc", true, "admin")
	if err != nil {
		t.Fatal(err)
	}
	rule, err := db.CreateAlertRule("before", "*", alerts.SeverityWarn, true, 300, 3, "", "", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.ReplaceAlertRuleChannels(rule, []database.AlertRuleChannel{{ChannelID: ch, IsActive: true}}); err != nil {
		t.Fatal(err)
	}

	body := `{"name":"after","enabled":false,"connection_ids":["` + connA + `"],"channels":[{"channel_id":"missing"}]}`
	if rec := serveAs(router, "admin", connA, http.MethodPut, "/alerts/rules/"+rule, body); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad channel: %d %s", rec.Code, rec.Body.String())
	}
	got, err := db.GetAlertRuleByID(rule)
	if err != nil || got == nil {
		t.Fatal(err)
	}
	if got.Name != "before" || !got.Enabled || len(got.ConnectionIDs) != 0 {
		t.Fatalf("refused edit was partly saved: %+v", got)
	}
	if bindings, _ := db.ListAlertRuleChannels(rule); len(bindings) != 1 || bindings[0].ChannelID != ch {
		t.Fatalf("refused edit changed the channels: %+v", bindings)
	}

	body = `{"name":"after","connection_ids":["` + connA + `"],"channels":[{"channel_id":"` + ch + `","is_active":false}]}`
	if rec := serveAs(router, "admin", connA, http.MethodPut, "/alerts/rules/"+rule, body); rec.Code != http.StatusOK {
		t.Fatalf("valid edit: %d %s", rec.Code, rec.Body.String())
	}
	got, _ = db.GetAlertRuleByID(rule)
	if got.Name != "after" || len(got.ConnectionIDs) != 1 || got.ConnectionIDs[0] != connA {
		t.Fatalf("valid edit not saved: %+v", got)
	}
	if bindings, _ := db.ListAlertRuleChannels(rule); len(bindings) != 1 || bindings[0].IsActive {
		t.Fatalf("valid edit channels: %+v", bindings)
	}
}

// Policy violations and access drift stay out of the event list for
// everyone but admins, like /violations and /incidents.
func TestAlertEventsHideAdminOnlyTypes(t *testing.T) {
	db, _, connA, _, router := newGovernanceRoleRouter(t)
	for _, typ := range []string{alerts.EventTypePolicyViolation, alerts.EventTypeAccessDrift, alerts.EventTypeScheduleFailed} {
		if _, err := db.CreateAlertEvent(&connA, typ, alerts.SeverityWarn, "title "+typ, "secret detail", nil, "fp-"+typ, ""); err != nil {
			t.Fatal(err)
		}
	}
	for _, role := range []string{"viewer", "analyst"} {
		rec := serveAs(router, role, connA, http.MethodGet, "/alerts/events", "")
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), alerts.EventTypeScheduleFailed) ||
			strings.Contains(rec.Body.String(), alerts.EventTypePolicyViolation) || strings.Contains(rec.Body.String(), alerts.EventTypeAccessDrift) {
			t.Fatalf("%s events: %d %s", role, rec.Code, rec.Body.String())
		}
		rec = serveAs(router, role, connA, http.MethodGet, "/alerts/events?event_type="+alerts.EventTypePolicyViolation, "")
		if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "secret detail") {
			t.Fatalf("%s filtered events: %d %s", role, rec.Code, rec.Body.String())
		}
	}
	rec := serveAs(router, "admin", connA, http.MethodGet, "/alerts/events", "")
	if !strings.Contains(rec.Body.String(), alerts.EventTypePolicyViolation) || !strings.Contains(rec.Body.String(), alerts.EventTypeAccessDrift) {
		t.Fatalf("admin events: %s", rec.Body.String())
	}
}

// The access API refuses to drop the admin's own login, and a login CH-UI
// runs unattended work with unless force is sent.
func TestDeleteChUserGuards(t *testing.T) {
	db, _, connA, _, router := newGovernanceRoleRouter(t)
	enc, err := crypto.Encrypt("pw", "test-secret")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetBackgroundCredential(connA, database.BackgroundCredential{Worker: "sql_alert", Mode: "service_account", Username: "svc_alerts", EncryptedPassword: enc}); err != nil {
		t.Fatal(err)
	}

	// serveAs signs in as ClickHouse user "admin".
	if rec := serveAs(router, "admin", connA, http.MethodDelete, "/access/users/admin", ""); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "current session user") {
		t.Fatalf("own login: %d %s", rec.Code, rec.Body.String())
	}
	rec := serveAs(router, "admin", connA, http.MethodDelete, "/access/users/svc_alerts", "")
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "background sql_alert") {
		t.Fatalf("service account: %d %s", rec.Code, rec.Body.String())
	}
	// With force the guard steps aside; this test session has no
	// ClickHouse password, so the request stops at the credentials.
	if rec := serveAs(router, "admin", connA, http.MethodDelete, "/access/users/svc_alerts?force=true", ""); rec.Code == http.StatusConflict {
		t.Fatalf("force ignored: %d %s", rec.Code, rec.Body.String())
	}
	if rec := serveAs(router, "viewer", connA, http.MethodDelete, "/access/users/svc_alerts?force=true", ""); rec.Code != http.StatusForbidden {
		t.Fatalf("viewer delete: %d", rec.Code)
	}
}
