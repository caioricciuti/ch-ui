// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/rbac"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
)

// Access as code: export the access-control state of a connection as a
// file, and plan the import of one. Import only plans; the client stages
// the ops and runs them through /plan and /apply like any other change.

func (h *RBACHandler) auditAccess(r *http.Request, session *middleware.SessionInfo, action string, details map[string]any) {
	b, _ := json.Marshal(details)
	if err := h.DB.CreateAuditLog(database.AuditLogParams{
		Action: action, Username: strPtr(middleware.Actor(session)), ClickhouseUser: strPtr(session.ClickhouseUser),
		ConnectionID: strPtr(session.ConnectionID), Details: strPtr(string(b)), IPAddress: strPtr(getClientIP(r)),
	}); err != nil {
		slog.Warn("Failed to write RBAC audit log", "action", action, "error", err)
	}
}

func (h *RBACHandler) connectionName(connID string) string {
	if c, err := h.DB.GetConnectionByID(connID); err == nil && c != nil {
		return c.Name
	}
	return ""
}

var unsafeFilenameRe = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func accessFilename(connection, ext string, now time.Time) string {
	name := strings.Trim(unsafeFilenameRe.ReplaceAllString(connection, "-"), "-.")
	if name == "" {
		name = "connection"
	}
	return "chui-access-" + name + "-" + now.UTC().Format("2006-01-02") + "." + ext
}

// Export handles GET /export?format=yaml|json|sql: the live access state as
// a file. Entities in users.xml and all authentication data are left out.
func (h *RBACHandler) Export(w http.ResponseWriter, r *http.Request) {
	format := r.URL.Query().Get("format")
	if format == "" {
		format = "yaml"
	}
	if format != "yaml" && format != "json" && format != "sql" {
		writeError(w, http.StatusBadRequest, "format must be yaml, json or sql")
		return
	}
	session, ex, ok := h.executor(w, r)
	if !ok {
		return
	}
	snap, err := rbac.ReadSnapshot(r.Context(), ex)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	now := time.Now()
	st, excluded := rbac.ExportState(snap)
	src := rbac.FileSource{Connection: h.connectionName(session.ConnectionID), Host: snap.Host, ExportedAt: now.UTC().Format(time.RFC3339), Incomplete: snap.Errors}
	header := rbac.ExportHeader(src, excluded)
	var body []byte
	contentType := "application/yaml; charset=utf-8"
	switch format {
	case "yaml":
		body, err = rbac.MarshalFileYAML(st, src, header)
	case "json":
		body, err = rbac.MarshalFileJSON(st, src)
		contentType = "application/json; charset=utf-8"
	case "sql":
		body = []byte(rbac.ExportSQL(snap, st, header))
		contentType = "application/sql; charset=utf-8"
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to write the export: "+err.Error())
		return
	}
	h.auditAccess(r, session, "rbac.exported", map[string]any{
		"format": format, "roles": len(st.Roles), "users": len(st.Users), "row_policies": len(st.RowPolicies),
		"quotas": len(st.Quotas), "settings_profiles": len(st.SettingsProfiles), "excluded": len(excluded),
	})
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", `attachment; filename="`+accessFilename(src.Connection, format, now)+`"`)
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

type rbacImportRequest struct {
	File   string      `json:"file"`
	Prune  rbac.Prune  `json:"prune"`
	Rename rbac.Rename `json:"rename"`
	Kinds  []string    `json:"kinds"`
}

// ImportPlan handles POST /import/plan: parse an access file and return the
// ops that would make this connection match it. Nothing changes.
func (h *RBACHandler) ImportPlan(w http.ResponseWriter, r *http.Request) {
	var req rbacImportRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}
	file, err := rbac.ParseFile([]byte(req.File))
	if err != nil {
		writeError(w, http.StatusBadRequest, "The file is not valid: "+err.Error())
		return
	}
	session, ex, ok := h.executor(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	snap, err := rbac.ReadSnapshot(ctx, ex)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	var dbs map[string]bool
	var notes []rbac.Skip
	for _, e := range snap.Errors {
		notes = append(notes, rbac.Skip{Kind: "snapshot", Reason: "could not read " + e + "; the plan may try to create what already exists"})
	}
	if rows, err := ex.Query(ctx, "SELECT name FROM system.databases", nil); err == nil {
		dbs = map[string]bool{}
		for _, row := range rows {
			if name, ok := row["name"].(string); ok {
				dbs[name] = true
			}
		}
	} else {
		notes = append(notes, rbac.Skip{Kind: "database", Reason: "system.databases could not be read, so databases were not checked"})
	}
	plan, err := rbac.PlanImport(snap, file.State, rbac.ImportOpts{Prune: req.Prune, Rename: req.Rename, Kinds: req.Kinds, Databases: dbs})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	plan.Skipped = append(notes, plan.Skipped...)
	if len(plan.Ops) > 0 {
		cat, err := rbac.LoadCatalog(ctx, ex, plan.Ops)
		if err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		if err := rbac.ValidateImportOps(plan.Ops, cat); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	h.auditAccess(r, session, "rbac.import.planned", map[string]any{
		"source_connection": file.Source.Connection, "ops": len(plan.Ops), "skipped": len(plan.Skipped),
		"over_cap": plan.OverCap, "summary": plan.Summary, "kinds": req.Kinds, "prune": req.Prune,
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true, "ops": plan.Ops, "skipped": plan.Skipped, "summary": plan.Summary,
		"over_cap": plan.OverCap, "message": plan.Message, "source": file.Source,
	})
}
