// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package handlers

import (
	"net/http"
	"strings"
	"testing"

	"github.com/caioricciuti/ch-ui/internal/crypto"
)

// Resend and Brevo channels left from before their removal still list, with
// the API key redacted, but cannot be created, turned on, edited in place or
// tested. They can be recreated as SMTP in place, or deleted.
func TestRemovedProviderChannels(t *testing.T) {
	db, _, connA, _, router := newGovernanceRoleRouter(t)
	legacy := func(name, kind string) string {
		enc, err := crypto.Encrypt(`{"api_key":"legacy-key-123","from_email":"alerts@example.com"}`, "test-secret")
		if err != nil {
			t.Fatal(err)
		}
		id, err := db.CreateAlertChannel(name, kind, enc, false, "admin")
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	resendID := legacy("old-resend", "resend")
	brevoID := legacy("old-brevo", "brevo")

	expect400 := func(method, path, body, want string) {
		t.Helper()
		rec := serveAs(router, "admin", connA, method, path, body)
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), want) {
			t.Fatalf("%s %s %s: %d %s, want 400 %q", method, path, body, rec.Code, rec.Body.String(), want)
		}
	}
	const removed = "Resend and Brevo were removed. Recreate this channel as SMTP or webhook."

	expect400(http.MethodPost, "/alerts/channels", `{"name":"new","channel_type":"brevo","config":{"api_key":"k","from_email":"a@example.com"}}`, removed)
	expect400(http.MethodPost, "/alerts/channels", `{"name":"new","channel_type":"pigeon","config":{}}`, "channel_type must be smtp or webhook")

	rec := serveAs(router, "viewer", connA, http.MethodGet, "/alerts/channels", "")
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "legacy-key-123") {
		t.Fatalf("list must keep the row and redact its key: %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"channel_type":"resend"`) || !strings.Contains(rec.Body.String(), `"has_secret":true`) {
		t.Fatalf("legacy row missing from list: %s", rec.Body.String())
	}

	expect400(http.MethodPut, "/alerts/channels/"+resendID, `{"is_active":true}`, removed)
	expect400(http.MethodPut, "/alerts/channels/"+resendID, `{"name":"renamed"}`, removed)
	expect400(http.MethodPost, "/alerts/channels/"+resendID+"/test", `{"recipients":["ops@example.com"]}`, removed)
	if ch, _ := db.GetAlertChannelByID(resendID); ch == nil || ch.IsActive || ch.Name != "old-resend" {
		t.Fatalf("rejected edits changed the row: %+v", ch)
	}

	// Switching the type needs a full config for the new type.
	expect400(http.MethodPut, "/alerts/channels/"+resendID, `{"channel_type":"smtp","is_active":true}`, "config is required")
	rec = serveAs(router, "admin", connA, http.MethodPut, "/alerts/channels/"+resendID,
		`{"channel_type":"smtp","is_active":true,"config":{"host":"smtp.example.com","from_email":"alerts@example.com"}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("recreate as smtp: %d %s", rec.Code, rec.Body.String())
	}
	ch, _ := db.GetAlertChannelByID(resendID)
	if ch == nil || ch.ChannelType != "smtp" || !ch.IsActive {
		t.Fatalf("recreated channel: %+v", ch)
	}
	if raw, err := crypto.Decrypt(ch.ConfigEncrypted, "test-secret"); err != nil || strings.Contains(raw, "legacy-key-123") {
		t.Fatalf("old API key kept after recreating as smtp: %q %v", raw, err)
	}

	if rec := serveAs(router, "admin", connA, http.MethodDelete, "/alerts/channels/"+brevoID, ""); rec.Code != http.StatusOK {
		t.Fatalf("delete legacy: %d %s", rec.Code, rec.Body.String())
	}
	if ch, _ := db.GetAlertChannelByID(brevoID); ch != nil {
		t.Fatal("legacy channel not deleted")
	}
}
