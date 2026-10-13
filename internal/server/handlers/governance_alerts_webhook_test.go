// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package handlers

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestWebhookAlertChannelLifecycle(t *testing.T) {
	_, _, connA, _, router := newGovernanceRoleRouter(t)
	var hits atomic.Int32
	var lastKey atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		lastKey.Store(r.Header.Get("X-Api-Key"))
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// Loopback without the explicit opt-in is refused.
	bad := `{"name":"hook","channel_type":"webhook","config":{"url":"` + srv.URL + `","allow_http":true}}`
	if rec := serveAs(router, "admin", connA, http.MethodPost, "/alerts/channels", bad); rec.Code != http.StatusBadRequest {
		t.Fatalf("private webhook without opt-in: %d %s", rec.Code, rec.Body.String())
	}

	create := `{"name":"hook","channel_type":"webhook","config":{"url":"` + srv.URL + `","allow_http":true,"allow_private_network":true,"format":"slack","secret_header_name":"X-Api-Key","secret_header_value":"k3y"}}`
	rec := serveAs(router, "admin", connA, http.MethodPost, "/alerts/channels", create)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &created)

	rec = serveAs(router, "viewer", connA, http.MethodGet, "/alerts/channels", "")
	if strings.Contains(rec.Body.String(), "k3y") || !strings.Contains(rec.Body.String(), `"has_secret":true`) {
		t.Fatalf("secret must be redacted: %s", rec.Body.String())
	}

	// Editing without resending the secret keeps the stored one.
	update := `{"config":{"url":"` + srv.URL + `","allow_http":true,"allow_private_network":true,"format":"generic","secret_header_name":"X-Api-Key","secret_header_value":""}}`
	if rec := serveAs(router, "admin", connA, http.MethodPut, "/alerts/channels/"+created.ID, update); rec.Code != http.StatusOK {
		t.Fatalf("update: %d %s", rec.Code, rec.Body.String())
	}

	// The test send needs no recipients for a webhook.
	if rec := serveAs(router, "admin", connA, http.MethodPost, "/alerts/channels/"+created.ID+"/test", `{}`); rec.Code != http.StatusOK {
		t.Fatalf("test send: %d %s", rec.Code, rec.Body.String())
	}
	if hits.Load() != 1 || lastKey.Load() != "k3y" {
		t.Fatalf("webhook hits %d, key %v", hits.Load(), lastKey.Load())
	}

	// A rule can bind the webhook without recipients.
	rule := `{"name":"r","event_type":"*","severity_min":"info","enabled":true,"cooldown_seconds":0,"max_attempts":3,"channels":[{"channel_id":"` + created.ID + `","recipients":[]}]}`
	if rec := serveAs(router, "admin", connA, http.MethodPost, "/alerts/rules", rule); rec.Code != http.StatusCreated {
		t.Fatalf("rule: %d %s", rec.Code, rec.Body.String())
	}
}
