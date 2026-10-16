// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/caioricciuti/ch-ui/internal/accesshistory"
	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/governance"
	"github.com/caioricciuti/ch-ui/internal/rbac"
	"github.com/caioricciuti/ch-ui/internal/safe"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

// RBACHandler edits ClickHouse access control: roles, grants, row policies,
// quotas and settings profiles. Every statement runs as the signed-in
// session's ClickHouse user and is built server-side from structured ops.
type RBACHandler struct {
	DB      *database.DB
	Gateway *tunnel.Gateway
	Config  *config.Config
	Syncer  *governance.Syncer
}

const rbacQueryTimeout = 30 * time.Second

// Routes are admin only. Reads and the plan (which changes nothing) stay
// open in the read-only grace window; apply needs an active license.
func (h *RBACHandler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Use(middleware.RequireAdmin(h.DB))
	r.With(middleware.RequirePro(h.Config)).Get("/snapshot", h.Snapshot)
	r.With(middleware.RequirePro(h.Config)).Get("/entity/{kind}/{name}", h.Entity)
	r.With(middleware.RequirePro(h.Config)).Get("/effective", h.Effective)
	r.With(middleware.RequirePro(h.Config)).Get("/who-can", h.WhoCan)
	r.With(middleware.RequireProRead(h.Config)).Post("/plan", h.Plan)
	r.With(middleware.RequirePro(h.Config)).Post("/apply", h.Apply)
	h.historyRoutes(r)
	r.With(middleware.RequirePro(h.Config)).Get("/export", h.Export)
	r.With(middleware.RequireProRead(h.Config)).Post("/import/plan", h.ImportPlan)
	return r
}

// sessionExecutor runs SQL as the session's ClickHouse user.
type sessionExecutor struct {
	gw         *tunnel.Gateway
	connID     string
	user, pass string
}

func (e sessionExecutor) Query(ctx context.Context, sql string, settings map[string]string) ([]map[string]any, error) {
	res, err := e.gw.ExecuteQueryWithSettingsCtx(ctx, e.connID, sql, e.user, e.pass, settings, rbacQueryTimeout)
	if err != nil {
		return nil, err
	}
	if res == nil {
		return nil, nil
	}
	return decodeRows(res.Data), nil
}

func (h *RBACHandler) executor(w http.ResponseWriter, r *http.Request) (*middleware.SessionInfo, sessionExecutor, bool) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return nil, sessionExecutor{}, false
	}
	if !h.Gateway.IsTunnelOnline(session.ConnectionID) {
		writeError(w, http.StatusServiceUnavailable, "The connection is offline")
		return nil, sessionExecutor{}, false
	}
	password, err := crypto.Decrypt(session.EncryptedPassword, h.Config.AppSecretKey)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to decrypt credentials")
		return nil, sessionExecutor{}, false
	}
	return session, sessionExecutor{gw: h.Gateway, connID: session.ConnectionID, user: session.ClickhouseUser, pass: password}, true
}

// Snapshot handles GET /snapshot.
func (h *RBACHandler) Snapshot(w http.ResponseWriter, r *http.Request) {
	session, ex, ok := h.executor(w, r)
	if !ok {
		return
	}
	snap, err := rbac.ReadSnapshot(r.Context(), ex)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	h.recordHistory(session, accesshistory.Capture{Source: accesshistory.SourceView, Snapshot: snap})
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "snapshot": snap})
}

func explainQueryFrom(r *http.Request) rbac.ExplainQuery {
	q := r.URL.Query()
	return rbac.ExplainQuery{
		User: q.Get("user"), Database: q.Get("database"), Table: q.Get("table"),
		Column: q.Get("column"), Privilege: q.Get("privilege"),
	}
}

func writeExplainError(w http.ResponseWriter, err error) {
	var verr *rbac.ValidationError
	switch {
	case errors.As(err, &verr):
		writeError(w, http.StatusBadRequest, verr.Error())
	case errors.Is(err, rbac.ErrUnknownUser):
		writeError(w, http.StatusNotFound, "User not found")
	default:
		writeError(w, http.StatusBadGateway, err.Error())
	}
}

// Effective handles GET /effective: what one user can do on an object and
// why. Reads only, so nothing is audited.
func (h *RBACHandler) Effective(w http.ResponseWriter, r *http.Request) {
	q := explainQueryFrom(r)
	if strings.TrimSpace(q.User) == "" {
		writeError(w, http.StatusBadRequest, "user is required")
		return
	}
	_, ex, ok := h.executor(w, r)
	if !ok {
		return
	}
	e, err := rbac.ExplainLive(r.Context(), ex, q)
	if err != nil {
		writeExplainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Success bool `json:"success"`
		*rbac.Explanation
	}{true, e})
}

// WhoCan handles GET /who-can: every user that can use a privilege on an
// object. Reads only, so nothing is audited.
func (h *RBACHandler) WhoCan(w http.ResponseWriter, r *http.Request) {
	q := explainQueryFrom(r)
	_, ex, ok := h.executor(w, r)
	if !ok {
		return
	}
	res, err := rbac.WhoCanLive(r.Context(), ex, q)
	if err != nil {
		writeExplainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Success bool `json:"success"`
		*rbac.WhoCanResult
	}{true, res})
}

// Entity handles GET /entity/{kind}/{name}: the SHOW CREATE text and, for
// roles and users, SHOW GRANTS.
func (h *RBACHandler) Entity(w http.ResponseWriter, r *http.Request) {
	kind := chi.URLParam(r, "kind")
	// chi routes on RawPath when the request needed escaping (a name with
	// "/" or "%"), and then hands back the escaped segment.
	name := chi.URLParam(r, "name")
	var err error
	if r.URL.RawPath != "" {
		name, err = url.PathUnescape(name)
	}
	if err != nil || rbac.ValidateName("entity", name) != nil {
		writeError(w, http.StatusBadRequest, "Invalid entity name")
		return
	}
	_, ex, ok := h.executor(w, r)
	if !ok {
		return
	}
	var show string
	switch kind {
	case rbac.KindRole:
		show = "SHOW CREATE ROLE " + safe.Identifier(name)
	case rbac.KindUser:
		show = "SHOW CREATE USER " + safe.Identifier(name)
	case rbac.KindQuota:
		show = "SHOW CREATE QUOTA " + safe.Identifier(name)
	case rbac.KindSettingsProfile:
		show = "SHOW CREATE SETTINGS PROFILE " + safe.Identifier(name)
	case rbac.KindRowPolicy:
		snap, err := rbac.ReadSnapshot(r.Context(), ex)
		if err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		p := snap.RowPolicyByFullName(name)
		if p == nil {
			writeError(w, http.StatusNotFound, "Row policy not found")
			return
		}
		show = "SHOW CREATE ROW POLICY " + safe.Identifier(p.Name) + " ON " + safe.Identifier(p.Database) + "." + safe.Identifier(p.Table)
	default:
		writeError(w, http.StatusBadRequest, "Unknown entity kind")
		return
	}
	ctx := r.Context()
	rows, err := ex.Query(ctx, show, nil)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	resp := map[string]any{"success": true, "create": strings.Join(rowValues(rows), "\n"), "grants": ""}
	if kind == rbac.KindRole || kind == rbac.KindUser {
		if g, err := ex.Query(ctx, "SHOW GRANTS FOR "+safe.Identifier(name), nil); err == nil {
			resp["grants"] = strings.Join(rowValues(g), "\n")
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

func rowValues(rows []map[string]any) []string {
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		for _, v := range row {
			out = append(out, fmt.Sprint(v))
			break
		}
	}
	return out
}

type rbacPlanRequest struct {
	Ops          []rbac.Op `json:"ops"`
	Cluster      string    `json:"cluster"`
	PlanHash     string    `json:"plan_hash"`
	Acknowledged []string  `json:"acknowledged"`
	// RevertOf names the access history row an apply reverts to.
	RevertOf string `json:"revert_of,omitempty"`
}

func decodeRBACRequest(w http.ResponseWriter, r *http.Request) (rbacPlanRequest, bool) {
	var req rbacPlanRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return req, false
	}
	return req, true
}

// serviceAccounts lists the ClickHouse users CH-UI runs unattended on this
// connection, so changing their access needs an acknowledgement.
func (h *RBACHandler) serviceAccounts(connID string) map[string]string {
	out := map[string]string{}
	for _, worker := range database.BackgroundWorkers() {
		c, err := h.DB.GetBackgroundCredential(connID, worker)
		if err == nil && c.Mode == "service_account" && c.Username != "" {
			addReason(out, c.Username, "background "+worker)
		}
	}
	if keys, err := h.DB.ListMCPKeys(); err == nil {
		for _, k := range keys {
			if k.ConnectionID == connID && k.RevokedAt == nil && k.CHUser != "" {
				addReason(out, k.CHUser, "MCP key "+k.Name)
			}
		}
	}
	if user, _, err := h.DB.GetConnectionSSOAccount(connID); err == nil && user != "" {
		label := "SSO service account"
		if cfg, err := h.DB.GetSSOIdentityConfig(connID); err == nil && cfg.PerPerson() {
			label = "SSO provisioner"
		}
		addReason(out, user, label)
	}
	// Users CH-UI provisioned for SSO people stay listed even after the
	// connection goes back to shared mode: the provisioner still owns them.
	if users, err := h.DB.ListSSOClickHouseUsers(connID); err == nil {
		for _, u := range users {
			addReason(out, u.CHUser, "SSO user "+u.Email)
		}
	}
	return out
}

func addReason(m map[string]string, user, reason string) {
	if prev, ok := m[user]; ok {
		m[user] = prev + ", " + reason
		return
	}
	m[user] = reason
}

func (h *RBACHandler) plan(w http.ResponseWriter, r *http.Request, ex sessionExecutor, connID string, req rbacPlanRequest) (*rbac.Plan, bool) {
	plan, _, ok := h.planWithSnapshot(w, r, ex, connID, req)
	return plan, ok
}

// planWithSnapshot is plan that also returns the snapshot it planned on.
func (h *RBACHandler) planWithSnapshot(w http.ResponseWriter, r *http.Request, ex sessionExecutor, connID string, req rbacPlanRequest) (*rbac.Plan, *rbac.Snapshot, bool) {
	plan, snap, err := rbac.PlanLive(r.Context(), ex, req.Ops, req.Cluster, rbac.GuardContext{ServiceAccounts: h.serviceAccounts(connID)})
	if err != nil {
		var verr *rbac.ValidationError
		if errors.As(err, &verr) {
			writeError(w, http.StatusBadRequest, verr.Error())
		} else {
			writeError(w, http.StatusBadGateway, err.Error())
		}
		return nil, nil, false
	}
	return plan, snap, true
}

// Plan handles POST /plan. It reads and validates only; nothing changes.
func (h *RBACHandler) Plan(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeRBACRequest(w, r)
	if !ok {
		return
	}
	session, ex, ok := h.executor(w, r)
	if !ok {
		return
	}
	plan, ok := h.plan(w, r, ex, session.ConnectionID, req)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "plan": plan})
}

// Apply handles POST /apply: re-plan, compare hashes, check warnings, then
// run the statements in order and stop at the first failure.
func (h *RBACHandler) Apply(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeRBACRequest(w, r)
	if !ok {
		return
	}
	session, ex, ok := h.executor(w, r)
	if !ok {
		return
	}
	if !h.checkRevertOf(w, session, req.RevertOf) {
		return
	}
	plan, pre, ok := h.planWithSnapshot(w, r, ex, session.ConnectionID, req)
	if !ok {
		return
	}
	if req.PlanHash == "" || req.PlanHash != plan.PlanHash {
		writeJSON(w, http.StatusConflict, map[string]any{"success": false, "error": "Access control changed since the review, or the change set differs. Review the plan again.", "plan": plan})
		return
	}
	if plan.Blocked() {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": "The plan has blocking warnings and cannot be applied.", "plan": plan})
		return
	}
	if missing := plan.Unacknowledged(req.Acknowledged); len(missing) > 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": "Acknowledge every warning before applying: " + strings.Join(missing, ", "), "plan": plan})
		return
	}

	actor := middleware.Actor(session)
	ip := getClientIP(r)
	audit := func(action, details string) {
		if err := h.DB.CreateAuditLog(database.AuditLogParams{
			Action: action, Username: strPtr(actor), ClickhouseUser: strPtr(session.ClickhouseUser),
			ConnectionID: strPtr(session.ConnectionID), Details: strPtr(details), IPAddress: strPtr(ip),
		}); err != nil {
			slog.Warn("Failed to write RBAC audit log", "action", action, "error", err)
		}
	}
	summary, _ := json.Marshal(map[string]any{"ops": len(req.Ops), "statements": len(plan.Statements), "cluster": plan.Cluster, "plan_hash": plan.PlanHash})
	audit("rbac.apply", string(summary))
	h.recordHistory(session, accesshistory.Capture{Source: accesshistory.SourceView, Snapshot: pre, Note: "read before an apply"})

	results := rbac.Apply(r.Context(), ex, plan, func(res rbac.Result) {
		details := map[string]any{"seq": res.Seq, "sql": truncateAudit(res.SQL, 2048), "plan_hash": plan.PlanHash}
		action := "rbac.statement.applied"
		if res.Status == rbac.StatusFailed {
			action = "rbac.statement.failed"
			details["error"] = truncateAudit(res.Error, 2048)
		}
		b, _ := json.Marshal(details)
		audit(action, string(b))
	})
	h.recordApply(r.Context(), session, ex, req, plan, pre, results)

	if h.Syncer != nil {
		gov := &GovernanceHandler{DB: h.DB, Syncer: h.Syncer}
		gov.triggerSyncAsync(governance.CHCredentials{ConnectionID: session.ConnectionID, User: ex.user, Password: ex.pass}, governance.SyncAccess)
	}

	failed := 0
	for _, res := range results {
		if res.Status == rbac.StatusFailed {
			failed++
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success":      true,
		"results":      results,
		"rollback_ops": rbac.RollbackOps(plan, results),
		"failed":       failed,
		"plan_hash":    plan.PlanHash,
	})
}

func truncateAudit(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "... (" + strconv.Itoa(len(s)-max) + " more bytes)"
}
