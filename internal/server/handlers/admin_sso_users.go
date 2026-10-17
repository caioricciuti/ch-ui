// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package handlers

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
)

// ssoClickHouseUserSummary is one per-person ClickHouse user as the admin
// users screen shows it under the person.
type ssoClickHouseUserSummary struct {
	ConnectionID   string   `json:"connection_id"`
	ConnectionName string   `json:"connection_name"`
	CHUser         string   `json:"ch_user"`
	Roles          []string `json:"roles"`
	LastSyncAt     string   `json:"last_sync_at"`
	LastSeenAt     string   `json:"last_seen_at"`
	Locked         bool     `json:"locked"`
}

// connectionNames maps every connection id to its name.
func (h *AdminHandler) connectionNames() (map[string]string, error) {
	conns, err := h.DB.GetConnections()
	if err != nil {
		return nil, err
	}
	names := make(map[string]string, len(conns))
	for _, c := range conns {
		names[c.ID] = c.Name
	}
	return names, nil
}

// connectionName is the connection's name, or its id when it is gone.
func connectionName(names map[string]string, id string) string {
	if name, ok := names[id]; ok && name != "" {
		return name
	}
	return id
}

// ssoClickHouseUsersByEmail groups every per-person ClickHouse user CH-UI
// created by the (lower-cased) email of the person behind it.
func (h *AdminHandler) ssoClickHouseUsersByEmail() (map[string][]ssoClickHouseUserSummary, error) {
	users, err := h.DB.ListSSOClickHouseUsers("")
	if err != nil {
		return nil, err
	}
	out := map[string][]ssoClickHouseUserSummary{}
	if len(users) == 0 {
		return out, nil
	}
	names, err := h.connectionNames()
	if err != nil {
		return nil, err
	}
	for _, u := range users {
		email := strings.ToLower(strings.TrimSpace(u.Email))
		roles := u.Roles
		if roles == nil {
			roles = []string{}
		}
		out[email] = append(out[email], ssoClickHouseUserSummary{
			ConnectionID:   u.ConnectionID,
			ConnectionName: connectionName(names, u.ConnectionID),
			CHUser:         u.CHUser,
			Roles:          roles,
			LastSyncAt:     u.LastSyncAt,
			LastSeenAt:     u.LastSeenAt,
			Locked:         u.Locked(),
		})
	}
	return out, nil
}

// ssoAccessFailure is one connection whose per-person user could not be
// dropped; its row is kept so the sweep can retry.
type ssoAccessFailure struct {
	ConnectionID   string `json:"connection_id"`
	ConnectionName string `json:"connection_name"`
	Error          string `json:"error"`
}

// RemoveSSOAccess ends an SSO person's access now: every ClickHouse user
// CH-UI created for them is dropped on its connection, and all their
// sessions are signed out. A connection that is offline or refuses the drop
// is reported in "failed" and keeps its row for a later retry. Admin only.
// DELETE /users/sso/{email}/access
func (h *AdminHandler) RemoveSSOAccess(w http.ResponseWriter, r *http.Request) {
	email, err := url.PathUnescape(chi.URLParam(r, "email"))
	email = strings.ToLower(strings.TrimSpace(email))
	if err != nil || email == "" {
		writeError(w, http.StatusBadRequest, "Email is required")
		return
	}
	session := middleware.GetSession(r)
	actor := middleware.Actor(session)
	if actor == "" {
		actor = "admin"
	}

	all, err := h.DB.ListSSOClickHouseUsers("")
	if err != nil {
		slog.Error("Failed to list per-person SSO users", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to retrieve the person's ClickHouse users")
		return
	}
	names, err := h.connectionNames()
	if err != nil {
		slog.Error("Failed to list connections", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to retrieve connections")
		return
	}

	dropped := 0
	failed := []ssoAccessFailure{}
	for _, u := range all {
		if strings.ToLower(strings.TrimSpace(u.Email)) != email {
			continue
		}
		fail := func(msg string) {
			failed = append(failed, ssoAccessFailure{ConnectionID: u.ConnectionID, ConnectionName: connectionName(names, u.ConnectionID), Error: msg})
		}
		if h.Gateway == nil || !h.Gateway.IsTunnelOnline(u.ConnectionID) {
			fail("connection is offline")
			continue
		}
		if h.Provisioner == nil {
			fail("provisioning is not available")
			continue
		}
		if err := h.Provisioner.Drop(r.Context(), u, actor); err != nil {
			slog.Warn("Failed to drop per-person SSO user", "error", err, "email", email, "connection", u.ConnectionID, "ch_user", u.CHUser)
			fail(err.Error())
			continue
		}
		dropped++
	}

	ended, err := h.DB.DeleteSessionsForRoleKey(database.SSORoleKeyPrefix+email, "")
	if err != nil {
		slog.Warn("Failed to end sessions after SSO access removal", "error", err, "email", email)
	}

	var actorName, chUser *string
	if session != nil {
		actorName, chUser = strPtr(actor), strPtr(session.ClickhouseUser)
	}
	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:         "sso.access_removed",
		Username:       actorName,
		ClickhouseUser: chUser,
		Details:        strPtr(fmt.Sprintf("Removed SSO access for %s: %d ClickHouse user(s) dropped, %d failed, %d session(s) signed out", email, dropped, len(failed), ended)),
		IPAddress:      strPtr(getClientIP(r)),
	})

	writeJSON(w, http.StatusOK, map[string]any{
		"dropped":        dropped,
		"failed":         failed,
		"sessions_ended": ended,
	})
}
