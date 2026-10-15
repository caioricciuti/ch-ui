// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/caioricciuti/ch-ui/internal/accesshistory"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/rbac"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
)

// rbacMaxOps is the most ops one plan takes (see rbac.Compose).
const rbacMaxOps = 200

// historyRoutes adds access history under /history. Reads stay open in the
// grace window; so does the revert plan, which changes nothing. Taking a
// snapshot writes, so it needs an active license.
func (h *RBACHandler) historyRoutes(r chi.Router) {
	r.With(middleware.RequirePro(h.Config)).Get("/history", h.History)
	r.With(middleware.RequirePro(h.Config)).Post("/history/snapshot", h.TakeSnapshot)
	r.With(middleware.RequirePro(h.Config)).Get("/history/{id}", h.HistoryEntry)
	r.With(middleware.RequirePro(h.Config)).Get("/history/{id}/diff", h.HistoryDiff)
	r.With(middleware.RequireProRead(h.Config)).Post("/history/{id}/revert-plan", h.HistoryRevertPlan)
}

// recordHistory stores c for the session's connection and user. Failures
// are logged; history never fails the request that captured it.
func (h *RBACHandler) recordHistory(session *middleware.SessionInfo, c accesshistory.Capture) (*database.RBACSnapshot, bool) {
	if c.Snapshot == nil {
		return nil, false
	}
	c.ConnectionID, c.CHUser = session.ConnectionID, session.ClickhouseUser
	if c.Actor == "" {
		c.Actor = middleware.Actor(session)
	}
	row, created, err := accesshistory.Record(h.DB, c)
	if err != nil {
		slog.Warn("Access history record failed", "connection", session.ConnectionID, "source", c.Source, "error", err)
		return nil, false
	}
	return row, created
}

// checkRevertOf refuses a revert_of that is not a history row of this
// connection.
func (h *RBACHandler) checkRevertOf(w http.ResponseWriter, session *middleware.SessionInfo, id string) bool {
	if id == "" {
		return true
	}
	if _, _, err := h.DB.GetRBACSnapshot(session.ConnectionID, id); err != nil {
		writeError(w, http.StatusBadRequest, "revert_of is not a snapshot of this connection")
		return false
	}
	return true
}

// recordApply reads the state after an apply and stores it as an apply or
// revert row, compared with the state the plan was made on.
func (h *RBACHandler) recordApply(ctx context.Context, session *middleware.SessionInfo, ex sessionExecutor, req rbacPlanRequest, plan *rbac.Plan, pre *rbac.Snapshot, results []rbac.Result) {
	post, err := rbac.ReadSnapshot(ctx, ex)
	if err != nil {
		slog.Warn("Access history: snapshot after apply failed", "connection", session.ConnectionID, "error", err)
		return
	}
	source := accesshistory.SourceApply
	if req.RevertOf != "" {
		source = accesshistory.SourceRevert
	}
	h.recordHistory(session, accesshistory.Capture{
		// History never holds a password hash or salt.
		Source: source, Snapshot: post, Pre: pre, Applied: rbac.RedactOps(rbac.AppliedOps(req.Ops, results)), PlanHash: plan.PlanHash, RevertOf: req.RevertOf,
	})
}

func (h *RBACHandler) auditHistory(r *http.Request, session *middleware.SessionInfo, action string, details any) {
	b, _ := json.Marshal(details)
	if err := h.DB.CreateAuditLog(database.AuditLogParams{
		Action: action, Username: strPtr(middleware.Actor(session)), ClickhouseUser: strPtr(session.ClickhouseUser),
		ConnectionID: strPtr(session.ConnectionID), Details: strPtr(string(b)), IPAddress: strPtr(getClientIP(r)),
	}); err != nil {
		slog.Warn("Failed to write RBAC audit log", "action", action, "error", err)
	}
}

// historyRow is a history row with its summary as a JSON object.
type historyRow struct {
	database.RBACSnapshot
	Summary json.RawMessage `json:"summary"`
}

func toHistoryRow(s database.RBACSnapshot) historyRow {
	sum := json.RawMessage(s.Summary)
	if !json.Valid(sum) {
		sum = json.RawMessage("{}")
	}
	return historyRow{RBACSnapshot: s, Summary: sum}
}

// History handles GET /history: the connection's history, newest first.
func (h *RBACHandler) History(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	rows, next, err := h.DB.ListRBACSnapshots(database.RBACHistoryFilter{
		ConnectionID: session.ConnectionID, DriftOnly: q.Get("drift") == "only", Limit: limit, Cursor: q.Get("cursor"),
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	outside, err := h.DB.CountRBACDrift(session.ConnectionID, time.Now().AddDate(0, 0, -30))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to count drift")
		return
	}
	out := make([]historyRow, 0, len(rows))
	for _, s := range rows {
		out = append(out, toHistoryRow(s))
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "snapshots": out, "next_cursor": next, "outside_last_30d": outside})
}

// loadHistory returns a row of the session's connection with its state.
func (h *RBACHandler) loadHistory(w http.ResponseWriter, session *middleware.SessionInfo, id string) (*database.RBACSnapshot, *rbac.State, bool) {
	row, body, err := h.DB.GetRBACSnapshot(session.ConnectionID, id)
	if errors.Is(err, database.ErrRBACSnapshotNotFound) {
		writeError(w, http.StatusNotFound, "Snapshot not found")
		return nil, nil, false
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to load snapshot")
		return nil, nil, false
	}
	st, err := accesshistory.DecodeState(body)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to read snapshot")
		return nil, nil, false
	}
	return row, st, true
}

// HistoryEntry handles GET /history/{id}: one row and its stored state.
func (h *RBACHandler) HistoryEntry(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}
	row, st, ok := h.loadHistory(w, session, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "snapshot": toHistoryRow(*row), "state": st})
}

// historySide names one side of a diff: a history row or the live state.
type historySide struct {
	ID    string `json:"id,omitempty"`
	Live  bool   `json:"live,omitempty"`
	At    string `json:"at,omitempty"`
	Label string `json:"label"`
}

func sideOf(s *database.RBACSnapshot) historySide {
	return historySide{ID: s.ID, At: s.FirstSeenAt, Label: s.Source + " " + s.FirstSeenAt}
}

// HistoryDiff handles GET /history/{id}/diff?against=previous|live|<id>.
// previous compares the row before it (same ClickHouse user) with it; live
// compares it with the server now; another id compares the older of the
// two with the newer.
func (h *RBACHandler) HistoryDiff(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}
	row, st, ok := h.loadHistory(w, session, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	against := r.URL.Query().Get("against")
	var fromSide, toSide historySide
	var from, to *rbac.State
	switch against {
	case "", "previous":
		prev, body, err := h.DB.PreviousRBACSnapshot(row)
		switch {
		case errors.Is(err, database.ErrRBACSnapshotNotFound):
			fromSide = historySide{Label: "nothing recorded before"}
		case err != nil:
			writeError(w, http.StatusInternalServerError, "Failed to load the previous snapshot")
			return
		default:
			if from, err = accesshistory.DecodeState(body); err != nil {
				writeError(w, http.StatusInternalServerError, "Failed to read snapshot")
				return
			}
			fromSide = sideOf(prev)
		}
		to, toSide = st, sideOf(row)
	case "live":
		_, ex, ok := h.executor(w, r)
		if !ok {
			return
		}
		live, err := rbac.ReadSnapshot(r.Context(), ex)
		if err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		from, fromSide = st, sideOf(row)
		to, toSide = rbac.FromSnapshot(live), historySide{Live: true, Label: "live"}
	default:
		other, ost, ok := h.loadHistory(w, session, against)
		if !ok {
			return
		}
		from, fromSide, to, toSide = ost, sideOf(other), st, sideOf(row)
		if other.FirstSeenAt > row.FirstSeenAt {
			from, fromSide, to, toSide = to, toSide, from, fromSide
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "from": fromSide, "to": toSide, "diff": rbac.DiffStates(from, to)})
}

// TakeSnapshot handles POST /history/snapshot: record the live state now.
// When nothing changed since the newest row, that row is bumped instead.
func (h *RBACHandler) TakeSnapshot(w http.ResponseWriter, r *http.Request) {
	session, ex, ok := h.executor(w, r)
	if !ok {
		return
	}
	snap, err := rbac.ReadSnapshot(r.Context(), ex)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	row, created := h.recordHistory(session, accesshistory.Capture{Source: accesshistory.SourceManual, Snapshot: snap})
	if row == nil {
		writeError(w, http.StatusInternalServerError, "Failed to record the snapshot")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "snapshot": toHistoryRow(*row), "created": created})
}

type revertPlanRequest struct {
	// Only limits the revert to these entity names.
	Only []string `json:"only"`
}

// unrevertable is a difference a revert plan leaves in place.
type unrevertable struct {
	Kind   string `json:"kind"`
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

// revertEntity counts the ops of a revert per entity, for picking a subset
// when the revert is over the plan cap.
type revertEntity struct {
	Name string `json:"name"`
	Ops  int    `json:"ops"`
}

// HistoryRevertPlan handles POST /history/{id}/revert-plan: the ops that
// make the server match the stored state, what they cannot change, and
// whether they fit in one plan. Nothing runs; the ops go through the normal
// plan and apply.
func (h *RBACHandler) HistoryRevertPlan(w http.ResponseWriter, r *http.Request) {
	var req revertPlanRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}
	session, ex, ok := h.executor(w, r)
	if !ok {
		return
	}
	row, target, ok := h.loadHistory(w, session, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	live, err := rbac.ReadSnapshot(r.Context(), ex)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	var only map[string]bool
	for _, n := range req.Only {
		if n = strings.TrimSpace(n); n != "" {
			if only == nil {
				only = map[string]bool{}
			}
			only[n] = true
		}
	}
	plan := rbac.PlanRevert(live, target, only)

	unrev := []unrevertable{}
	for _, s := range plan.Skipped {
		unrev = append(unrev, unrevertable{Kind: s.Kind, Name: s.Name, Reason: s.Reason})
	}
	for _, c := range plan.Remaining {
		unrev = append(unrev, unrevertable{Kind: c.EntityKind, Name: c.Grantee, Reason: "still differs after the revert: " + c.Change + " " + c.Detail})
	}
	counts := map[string]int{}
	for _, op := range plan.Ops {
		for _, n := range rbac.OpEntities(op) {
			counts[n]++
		}
	}
	entities := make([]revertEntity, 0, len(counts))
	for n, c := range counts {
		entities = append(entities, revertEntity{Name: n, Ops: c})
	}
	sort.Slice(entities, func(i, j int) bool { return entities[i].Name < entities[j].Name })

	h.auditHistory(r, session, "rbac.history.revert_prepared", map[string]any{
		"snapshot_id": row.ID, "hash": row.Hash, "ops": len(plan.Ops), "unrevertable": len(unrev), "only": req.Only,
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"success":      true,
		"snapshot_id":  row.ID,
		"ops":          plan.Ops,
		"unrevertable": unrev,
		"over_cap":     len(plan.Ops) > rbacMaxOps,
		"max_ops":      rbacMaxOps,
		"entities":     entities,
	})
}
