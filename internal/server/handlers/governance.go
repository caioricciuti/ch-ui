package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/governance"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
	"github.com/go-chi/chi/v5"
)

// GovernanceHandler handles all governance-related HTTP endpoints.
type GovernanceHandler struct {
	DB      *database.DB
	Gateway *tunnel.Gateway
	Config  *config.Config
	Store   *governance.Store
	Syncer  *governance.Syncer
}

// Routes returns a chi.Router with all governance routes mounted.
func (h *GovernanceHandler) Routes() chi.Router {
	r := chi.NewRouter()

	// Overview & Sync
	r.Get("/overview", h.GetOverview)
	r.Post("/sync", h.TriggerSync)
	r.Post("/sync/{type}", h.TriggerSingleSync)
	r.Get("/sync/status", h.GetSyncStatus)

	// Metadata
	r.Get("/databases", h.ListDatabases)
	r.Get("/tables", h.ListTables)
	r.Get("/tables/{db}/{table}", h.GetTableDetail)
	r.Get("/tables/{db}/{table}/notes", h.ListTableNotes)
	r.Get("/tables/{db}/{table}/columns/{column}/notes", h.ListColumnNotes)
	r.With(middleware.RequireAdmin(h.DB)).Post("/tables/{db}/{table}/notes", h.CreateTableNote)
	r.With(middleware.RequireAdmin(h.DB)).Post("/tables/{db}/{table}/columns/{column}/notes", h.CreateColumnNote)
	r.With(middleware.RequireAdmin(h.DB)).Delete("/notes/{id}", h.DeleteObjectNote)
	r.With(middleware.RequireAdmin(h.DB)).Put("/tables/{db}/{table}/comment", h.UpdateTableComment)
	r.With(middleware.RequireAdmin(h.DB)).Put("/tables/{db}/{table}/columns/{column}/comment", h.UpdateColumnComment)
	r.Get("/schema-changes", h.ListSchemaChanges)

	// Query Log
	r.Get("/query-log", h.ListQueryLog)
	r.Get("/query-log/top", h.TopQueries)

	// Lineage
	r.Get("/lineage", h.GetLineage)
	r.Get("/lineage/graph", h.GetLineageGraph)

	// Tags
	r.Get("/tags", h.ListTags)
	r.Post("/tags", h.CreateTag)
	r.Delete("/tags/{id}", h.DeleteTag)

	// Access
	r.Route("/access", func(ar chi.Router) {
		ar.Get("/users", h.ListChUsers)
		ar.With(middleware.RequireAdmin(h.DB)).Post("/users", h.CreateChUser)
		ar.With(middleware.RequireAdmin(h.DB)).Delete("/users/{name}", h.DeleteChUser)
		ar.Get("/roles", h.ListChRoles)
		ar.Get("/matrix", h.GetAccessMatrix)
		ar.Get("/over-permissions", h.GetOverPermissions)
	})

	// Policies
	r.Route("/policies", func(pr chi.Router) {
		pr.With(middleware.RequireAdmin(h.DB)).Get("/", h.ListPolicies)
		pr.With(middleware.RequireAdmin(h.DB)).Post("/", h.CreatePolicy)
		pr.With(middleware.RequireAdmin(h.DB)).Get("/{id}", h.GetPolicy)
		pr.With(middleware.RequireAdmin(h.DB)).Put("/{id}", h.UpdatePolicy)
		pr.With(middleware.RequireAdmin(h.DB)).Delete("/{id}", h.DeletePolicy)
	})

	// Violations
	r.Get("/violations", h.ListViolations)
	r.With(middleware.RequireAdmin(h.DB)).Post("/violations/{id}/incident", h.CreateIncidentFromViolation)

	// Incidents
	r.Get("/incidents", h.ListIncidents)
	r.Get("/incidents/{id}", h.GetIncident)
	r.With(middleware.RequireAdmin(h.DB)).Post("/incidents", h.CreateIncident)
	r.With(middleware.RequireAdmin(h.DB)).Put("/incidents/{id}", h.UpdateIncident)
	r.Get("/incidents/{id}/comments", h.ListIncidentComments)
	r.With(middleware.RequireAdmin(h.DB)).Post("/incidents/{id}/comments", h.CreateIncidentComment)

	// Audit logs
	r.Get("/audit-logs", h.GetAuditLogs)

	// ClickHouse query log
	r.Get("/clickhouse-query-log", h.GetClickHouseQueryLog)

	// Alerts management
	r.Route("/alerts", func(ar chi.Router) {
		ar.Get("/channels", h.ListAlertChannels)
		ar.Post("/channels", h.CreateAlertChannel)
		ar.Put("/channels/{id}", h.UpdateAlertChannel)
		ar.Delete("/channels/{id}", h.DeleteAlertChannel)
		ar.Post("/channels/{id}/test", h.TestAlertChannel)
		ar.Get("/rules", h.ListAlertRules)
		ar.Post("/rules", h.CreateAlertRule)
		ar.Put("/rules/{id}", h.UpdateAlertRule)
		ar.Delete("/rules/{id}", h.DeleteAlertRule)
		ar.Get("/events", h.ListAlertEvents)
	})

	return r
}

// ── Helpers ──────────────────────────────────────────────────────────────────

func (h *GovernanceHandler) getCredentials(r *http.Request) (*governance.CHCredentials, error) {
	session := middleware.GetSession(r)
	if session == nil {
		return nil, fmt.Errorf("not authenticated")
	}
	password, err := crypto.Decrypt(session.EncryptedPassword, h.Config.AppSecretKey)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt credentials: %w", err)
	}
	return &governance.CHCredentials{
		ConnectionID: session.ConnectionID,
		User:         session.ClickhouseUser,
		Password:     password,
	}, nil
}

func (h *GovernanceHandler) executeClickHouseSQL(creds *governance.CHCredentials, sql string) error {
	_, err := h.Gateway.ExecuteQuery(creds.ConnectionID, sql, creds.User, creds.Password, 30*time.Second)
	if err != nil {
		return fmt.Errorf("execute clickhouse query: %w", err)
	}
	return nil
}

func (h *GovernanceHandler) triggerSyncAsync(creds governance.CHCredentials, syncType governance.SyncType) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if err := h.Syncer.SyncSingle(ctx, creds, syncType); err != nil {
			slog.Warn("Governance async sync failed", "connection", creds.ConnectionID, "type", syncType, "error", err)
		}
	}()
}

func (h *GovernanceHandler) connectionID(r *http.Request) string {
	session := middleware.GetSession(r)
	if session == nil {
		return ""
	}
	return session.ConnectionID
}

func queryInt(r *http.Request, key string, defaultVal int) int {
	v := r.URL.Query().Get(key)
	if v == "" {
		return defaultVal
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return defaultVal
	}
	return n
}

func queryIntBounded(r *http.Request, key string, defaultVal, minVal, maxVal int) int {
	n := queryInt(r, key, defaultVal)
	if n < minVal {
		return minVal
	}
	if n > maxVal {
		return maxVal
	}
	return n
}

// ── Overview & Sync ──────────────────────────────────────────────────────────

func (h *GovernanceHandler) GetOverview(w http.ResponseWriter, r *http.Request) {
	connID := h.connectionID(r)
	if connID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Not authenticated"})
		return
	}

	overview, err := h.Store.GetOverview(connID)
	if err != nil {
		slog.Error("Failed to get governance overview", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to get overview"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"overview": overview})
}

func (h *GovernanceHandler) TriggerSync(w http.ResponseWriter, r *http.Request) {
	creds, err := h.getCredentials(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
		return
	}

	result, err := h.Syncer.SyncConnection(r.Context(), *creds)
	if err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}

	session := middleware.GetSession(r)
	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:       "governance.sync",
		Username:     strPtr(session.ClickhouseUser),
		ConnectionID: strPtr(session.ConnectionID),
		Details:      strPtr("full sync triggered"),
	})

	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "result": result})
}

func (h *GovernanceHandler) TriggerSingleSync(w http.ResponseWriter, r *http.Request) {
	syncType := governance.SyncType(chi.URLParam(r, "type"))
	if syncType != governance.SyncMetadata && syncType != governance.SyncQueryLog && syncType != governance.SyncAccess {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid sync type. Use: metadata, query_log, access"})
		return
	}

	creds, err := h.getCredentials(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
		return
	}

	if err := h.Syncer.SyncSingle(r.Context(), *creds, syncType); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (h *GovernanceHandler) GetSyncStatus(w http.ResponseWriter, r *http.Request) {
	connID := h.connectionID(r)
	if connID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Not authenticated"})
		return
	}

	states, err := h.Store.GetSyncStates(connID)
	if err != nil {
		slog.Error("Failed to get sync status", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to get sync status"})
		return
	}
	if states == nil {
		states = []governance.SyncState{}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"sync_states": states})
}

// ── Metadata ─────────────────────────────────────────────────────────────────

func (h *GovernanceHandler) ListDatabases(w http.ResponseWriter, r *http.Request) {
	connID := h.connectionID(r)
	if connID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Not authenticated"})
		return
	}

	databases, err := h.Store.GetDatabases(connID)
	if err != nil {
		slog.Error("Failed to list governance databases", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to list databases"})
		return
	}
	if databases == nil {
		databases = []governance.GovDatabase{}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"databases": databases})
}

func (h *GovernanceHandler) ListTables(w http.ResponseWriter, r *http.Request) {
	connID := h.connectionID(r)
	if connID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Not authenticated"})
		return
	}

	dbFilter := r.URL.Query().Get("database")
	var tables []governance.GovTable
	var err error

	if dbFilter != "" {
		tables, err = h.Store.GetTablesByDatabase(connID, dbFilter)
	} else {
		tables, err = h.Store.GetTables(connID)
	}
	if err != nil {
		slog.Error("Failed to list governance tables", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to list tables"})
		return
	}
	if tables == nil {
		tables = []governance.GovTable{}
	}

	// Enrich with tags
	for i := range tables {
		tags, _ := h.Store.GetTagsForTable(connID, tables[i].DatabaseName, tables[i].TableName)
		tagNames := make([]string, 0)
		for _, t := range tags {
			if t.ObjectType == "table" {
				tagNames = append(tagNames, t.Tag)
			}
		}
		tables[i].Tags = tagNames
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"tables": tables, "total": len(tables)})
}

func (h *GovernanceHandler) GetTableDetail(w http.ResponseWriter, r *http.Request) {
	connID := h.connectionID(r)
	if connID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Not authenticated"})
		return
	}

	dbName := chi.URLParam(r, "db")
	tableName := chi.URLParam(r, "table")

	table, err := h.Store.GetTableByName(connID, dbName, tableName)
	if err != nil {
		slog.Error("Failed to get table detail", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to get table"})
		return
	}
	if table == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Table not found"})
		return
	}

	// Get columns
	columns, _ := h.Store.GetColumns(connID, dbName, tableName)
	if columns == nil {
		columns = []governance.GovColumn{}
	}

	// Enrich columns with tags
	for i := range columns {
		colTags, _ := h.Store.GetTagsForColumn(connID, dbName, tableName, columns[i].ColumnName)
		tagNames := make([]string, 0)
		for _, t := range colTags {
			tagNames = append(tagNames, t.Tag)
		}
		columns[i].Tags = tagNames
	}

	// Get table tags
	tableTags, _ := h.Store.GetTagsForTable(connID, dbName, tableName)
	tagNames := make([]string, 0)
	for _, t := range tableTags {
		if t.ObjectType == "table" {
			tagNames = append(tagNames, t.Tag)
		}
	}
	table.Tags = tagNames

	// Get recent queries
	queries, _, _ := h.Store.GetQueryLog(connID, 20, 0, "", dbName+"."+tableName)

	// Get lineage
	upstream, downstream, _ := h.Store.GetLineageForTable(connID, dbName, tableName)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"table":          table,
		"columns":        columns,
		"tags":           tableTags,
		"queries":        queries,
		"recent_queries": queries,
		"upstream":       upstream,
		"downstream":     downstream,
	})
}

func (h *GovernanceHandler) UpdateTableComment(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Not authenticated"})
		return
	}

	creds, err := h.getCredentials(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
		return
	}

	dbName := strings.TrimSpace(chi.URLParam(r, "db"))
	tableName := strings.TrimSpace(chi.URLParam(r, "table"))
	if dbName == "" || tableName == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Database and table are required"})
		return
	}

	var body struct {
		Comment string `json:"comment"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
		return
	}

	sql := fmt.Sprintf(
		"ALTER TABLE %s.%s MODIFY COMMENT '%s'",
		escapeIdentifier(dbName),
		escapeIdentifier(tableName),
		escapeLiteral(body.Comment),
	)
	if err := h.executeClickHouseSQL(creds, sql); err != nil {
		slog.Error("Failed to update table comment", "connection", session.ConnectionID, "db", dbName, "table", tableName, "error", err)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}

	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:       "governance.table.comment.updated",
		Username:     strPtr(session.ClickhouseUser),
		ConnectionID: strPtr(session.ConnectionID),
		Details:      strPtr(fmt.Sprintf("%s.%s", dbName, tableName)),
	})

	h.triggerSyncAsync(*creds, governance.SyncMetadata)
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (h *GovernanceHandler) UpdateColumnComment(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Not authenticated"})
		return
	}

	creds, err := h.getCredentials(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
		return
	}

	dbName := strings.TrimSpace(chi.URLParam(r, "db"))
	tableName := strings.TrimSpace(chi.URLParam(r, "table"))
	columnName := strings.TrimSpace(chi.URLParam(r, "column"))
	if dbName == "" || tableName == "" || columnName == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Database, table, and column are required"})
		return
	}

	var body struct {
		Comment string `json:"comment"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
		return
	}

	sql := fmt.Sprintf(
		"ALTER TABLE %s.%s COMMENT COLUMN %s '%s'",
		escapeIdentifier(dbName),
		escapeIdentifier(tableName),
		escapeIdentifier(columnName),
		escapeLiteral(body.Comment),
	)
	if err := h.executeClickHouseSQL(creds, sql); err != nil {
		slog.Error("Failed to update column comment", "connection", session.ConnectionID, "db", dbName, "table", tableName, "column", columnName, "error", err)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}

	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:       "governance.column.comment.updated",
		Username:     strPtr(session.ClickhouseUser),
		ConnectionID: strPtr(session.ConnectionID),
		Details:      strPtr(fmt.Sprintf("%s.%s.%s", dbName, tableName, columnName)),
	})

	h.triggerSyncAsync(*creds, governance.SyncMetadata)
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (h *GovernanceHandler) ListTableNotes(w http.ResponseWriter, r *http.Request) {
	connID := h.connectionID(r)
	if connID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Not authenticated"})
		return
	}
	dbName := strings.TrimSpace(chi.URLParam(r, "db"))
	tableName := strings.TrimSpace(chi.URLParam(r, "table"))
	if dbName == "" || tableName == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Database and table are required"})
		return
	}
	notes, err := h.Store.ListObjectComments(connID, "table", dbName, tableName, "", 200)
	if err != nil {
		slog.Error("Failed to list table notes", "connection", connID, "db", dbName, "table", tableName, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to list table notes"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"notes": notes})
}

func (h *GovernanceHandler) ListColumnNotes(w http.ResponseWriter, r *http.Request) {
	connID := h.connectionID(r)
	if connID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Not authenticated"})
		return
	}
	dbName := strings.TrimSpace(chi.URLParam(r, "db"))
	tableName := strings.TrimSpace(chi.URLParam(r, "table"))
	columnName := strings.TrimSpace(chi.URLParam(r, "column"))
	if dbName == "" || tableName == "" || columnName == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Database, table and column are required"})
		return
	}
	notes, err := h.Store.ListObjectComments(connID, "column", dbName, tableName, columnName, 200)
	if err != nil {
		slog.Error("Failed to list column notes", "connection", connID, "db", dbName, "table", tableName, "column", columnName, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to list column notes"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"notes": notes})
}

func (h *GovernanceHandler) CreateTableNote(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Not authenticated"})
		return
	}
	dbName := strings.TrimSpace(chi.URLParam(r, "db"))
	tableName := strings.TrimSpace(chi.URLParam(r, "table"))
	if dbName == "" || tableName == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Database and table are required"})
		return
	}
	var body struct {
		CommentText string `json:"comment_text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
		return
	}
	commentText := strings.TrimSpace(body.CommentText)
	if commentText == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "comment_text is required"})
		return
	}
	if len(commentText) > 4000 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "comment_text must be <= 4000 characters"})
		return
	}
	id, err := h.Store.CreateObjectComment(session.ConnectionID, "table", dbName, tableName, "", commentText, session.ClickhouseUser)
	if err != nil {
		slog.Error("Failed to create table note", "connection", session.ConnectionID, "db", dbName, "table", tableName, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to create table note"})
		return
	}
	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:       "governance.table.note.created",
		Username:     strPtr(session.ClickhouseUser),
		ConnectionID: strPtr(session.ConnectionID),
		Details:      strPtr(fmt.Sprintf("%s.%s", dbName, tableName)),
	})
	writeJSON(w, http.StatusCreated, map[string]interface{}{"id": id, "success": true})
}

func (h *GovernanceHandler) CreateColumnNote(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Not authenticated"})
		return
	}
	dbName := strings.TrimSpace(chi.URLParam(r, "db"))
	tableName := strings.TrimSpace(chi.URLParam(r, "table"))
	columnName := strings.TrimSpace(chi.URLParam(r, "column"))
	if dbName == "" || tableName == "" || columnName == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Database, table and column are required"})
		return
	}
	var body struct {
		CommentText string `json:"comment_text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
		return
	}
	commentText := strings.TrimSpace(body.CommentText)
	if commentText == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "comment_text is required"})
		return
	}
	if len(commentText) > 4000 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "comment_text must be <= 4000 characters"})
		return
	}
	id, err := h.Store.CreateObjectComment(session.ConnectionID, "column", dbName, tableName, columnName, commentText, session.ClickhouseUser)
	if err != nil {
		slog.Error("Failed to create column note", "connection", session.ConnectionID, "db", dbName, "table", tableName, "column", columnName, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to create column note"})
		return
	}
	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:       "governance.column.note.created",
		Username:     strPtr(session.ClickhouseUser),
		ConnectionID: strPtr(session.ConnectionID),
		Details:      strPtr(fmt.Sprintf("%s.%s.%s", dbName, tableName, columnName)),
	})
	writeJSON(w, http.StatusCreated, map[string]interface{}{"id": id, "success": true})
}

func (h *GovernanceHandler) DeleteObjectNote(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Not authenticated"})
		return
	}
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id is required"})
		return
	}
	if err := h.Store.DeleteObjectComment(session.ConnectionID, id); err != nil {
		if err == sql.ErrNoRows {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "Note not found"})
			return
		}
		slog.Error("Failed to delete object note", "connection", session.ConnectionID, "id", id, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to delete note"})
		return
	}
	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:       "governance.object.note.deleted",
		Username:     strPtr(session.ClickhouseUser),
		ConnectionID: strPtr(session.ConnectionID),
		Details:      strPtr(id),
	})
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (h *GovernanceHandler) ListSchemaChanges(w http.ResponseWriter, r *http.Request) {
	connID := h.connectionID(r)
	if connID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Not authenticated"})
		return
	}

	limit := queryIntBounded(r, "limit", 50, 1, 500)
	changes, err := h.Store.GetSchemaChanges(connID, limit)
	if err != nil {
		slog.Error("Failed to list schema changes", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to list schema changes"})
		return
	}
	if changes == nil {
		changes = []governance.SchemaChange{}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"changes": changes})
}

// ── Query Log ────────────────────────────────────────────────────────────────

func (h *GovernanceHandler) ListQueryLog(w http.ResponseWriter, r *http.Request) {
	connID := h.connectionID(r)
	if connID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Not authenticated"})
		return
	}

	limit := queryIntBounded(r, "limit", 100, 1, 5000)
	offset := queryIntBounded(r, "offset", 0, 0, 1000000)
	user := r.URL.Query().Get("user")
	table := r.URL.Query().Get("table")

	entries, total, err := h.Store.GetQueryLog(connID, limit, offset, user, table)
	if err != nil {
		slog.Error("Failed to list query log", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to list query log"})
		return
	}
	if entries == nil {
		entries = []governance.QueryLogEntry{}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"entries": entries, "total": total})
}

func (h *GovernanceHandler) TopQueries(w http.ResponseWriter, r *http.Request) {
	connID := h.connectionID(r)
	if connID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Not authenticated"})
		return
	}

	limit := queryIntBounded(r, "limit", 20, 1, 200)
	top, err := h.Store.GetTopQueries(connID, limit)
	if err != nil {
		slog.Error("Failed to get top queries", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to get top queries"})
		return
	}
	if top == nil {
		top = []map[string]interface{}{}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"queries": top, "top_queries": top})
}

// ── Lineage ──────────────────────────────────────────────────────────────────

func (h *GovernanceHandler) GetLineage(w http.ResponseWriter, r *http.Request) {
	connID := h.connectionID(r)
	if connID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Not authenticated"})
		return
	}

	dbName := r.URL.Query().Get("database")
	tableName := r.URL.Query().Get("table")
	if dbName == "" || tableName == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "database and table query params required"})
		return
	}

	upstream, downstream, err := h.Store.GetLineageForTable(connID, dbName, tableName)
	if err != nil {
		slog.Error("Failed to get lineage", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to get lineage"})
		return
	}
	if upstream == nil {
		upstream = []governance.LineageEdge{}
	}
	if downstream == nil {
		downstream = []governance.LineageEdge{}
	}

	// Build graph representation
	nodeMap := make(map[string]governance.LineageNode)
	currentKey := dbName + "." + tableName
	nodeMap[currentKey] = governance.LineageNode{
		ID: currentKey, Database: dbName, Table: tableName, Type: "current",
	}

	for _, e := range upstream {
		key := e.SourceDatabase + "." + e.SourceTable
		if _, ok := nodeMap[key]; !ok {
			nodeMap[key] = governance.LineageNode{
				ID: key, Database: e.SourceDatabase, Table: e.SourceTable, Type: "source",
			}
		}
	}
	for _, e := range downstream {
		key := e.TargetDatabase + "." + e.TargetTable
		if _, ok := nodeMap[key]; !ok {
			nodeMap[key] = governance.LineageNode{
				ID: key, Database: e.TargetDatabase, Table: e.TargetTable, Type: "target",
			}
		}
	}

	nodes := make([]governance.LineageNode, 0, len(nodeMap))
	for _, n := range nodeMap {
		nodes = append(nodes, n)
	}

	allEdges := append(upstream, downstream...)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"graph": governance.LineageGraph{Nodes: nodes, Edges: allEdges},
	})
}

func (h *GovernanceHandler) GetLineageGraph(w http.ResponseWriter, r *http.Request) {
	connID := h.connectionID(r)
	if connID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Not authenticated"})
		return
	}

	edges, err := h.Store.GetFullLineageGraph(connID)
	if err != nil {
		slog.Error("Failed to get lineage graph", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to get lineage graph"})
		return
	}
	if edges == nil {
		edges = []governance.LineageEdge{}
	}

	// Build nodes from edges
	nodeMap := make(map[string]governance.LineageNode)
	for _, e := range edges {
		srcKey := e.SourceDatabase + "." + e.SourceTable
		if _, ok := nodeMap[srcKey]; !ok {
			nodeMap[srcKey] = governance.LineageNode{
				ID: srcKey, Database: e.SourceDatabase, Table: e.SourceTable, Type: "source",
			}
		}
		tgtKey := e.TargetDatabase + "." + e.TargetTable
		if _, ok := nodeMap[tgtKey]; !ok {
			nodeMap[tgtKey] = governance.LineageNode{
				ID: tgtKey, Database: e.TargetDatabase, Table: e.TargetTable, Type: "target",
			}
		}
	}

	nodes := make([]governance.LineageNode, 0, len(nodeMap))
	for _, n := range nodeMap {
		nodes = append(nodes, n)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"graph": governance.LineageGraph{Nodes: nodes, Edges: edges},
	})
}

// ── Tags ─────────────────────────────────────────────────────────────────────

func (h *GovernanceHandler) ListTags(w http.ResponseWriter, r *http.Request) {
	connID := h.connectionID(r)
	if connID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Not authenticated"})
		return
	}

	dbName := r.URL.Query().Get("database")
	tableName := r.URL.Query().Get("table")

	var tags []governance.TagEntry
	var err error

	if dbName != "" && tableName != "" {
		tags, err = h.Store.GetTagsForTable(connID, dbName, tableName)
	} else {
		tags, err = h.Store.GetTags(connID)
	}
	if err != nil {
		slog.Error("Failed to list tags", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to list tags"})
		return
	}
	if tags == nil {
		tags = []governance.TagEntry{}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"tags": tags})
}

func (h *GovernanceHandler) CreateTag(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Not authenticated"})
		return
	}

	var body struct {
		ObjectType   string `json:"object_type"`
		DatabaseName string `json:"database_name"`
		TableName    string `json:"table_name"`
		ColumnName   string `json:"column_name"`
		Tag          string `json:"tag"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
		return
	}

	tag := governance.SensitivityTag(strings.ToUpper(body.Tag))
	if !governance.ValidTags[tag] {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid tag. Valid: PII, FINANCIAL, INTERNAL, PUBLIC, CRITICAL"})
		return
	}

	if body.ObjectType != "table" && body.ObjectType != "column" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "object_type must be 'table' or 'column'"})
		return
	}

	if body.DatabaseName == "" || body.TableName == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "database_name and table_name are required"})
		return
	}

	id, err := h.Store.CreateTag(
		session.ConnectionID, body.ObjectType, body.DatabaseName, body.TableName,
		body.ColumnName, tag, session.ClickhouseUser,
	)
	if err != nil {
		slog.Error("Failed to create tag", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to create tag"})
		return
	}

	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:       "governance.tag.created",
		Username:     strPtr(session.ClickhouseUser),
		ConnectionID: strPtr(session.ConnectionID),
		Details:      strPtr(fmt.Sprintf("%s on %s.%s", tag, body.DatabaseName, body.TableName)),
	})

	writeJSON(w, http.StatusCreated, map[string]interface{}{"id": id})
}

func (h *GovernanceHandler) DeleteTag(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Not authenticated"})
		return
	}

	id := chi.URLParam(r, "id")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Tag ID required"})
		return
	}

	if err := h.Store.DeleteTag(id); err != nil {
		slog.Error("Failed to delete tag", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to delete tag"})
		return
	}

	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:       "governance.tag.deleted",
		Username:     strPtr(session.ClickhouseUser),
		ConnectionID: strPtr(session.ConnectionID),
		Details:      strPtr(fmt.Sprintf("tag %s deleted", id)),
	})

	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

// ── Access ───────────────────────────────────────────────────────────────────

func (h *GovernanceHandler) ListChUsers(w http.ResponseWriter, r *http.Request) {
	connID := h.connectionID(r)
	if connID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Not authenticated"})
		return
	}

	users, err := h.Store.GetChUsers(connID)
	if err != nil {
		slog.Error("Failed to list CH users", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to list users"})
		return
	}
	if users == nil {
		users = []governance.ChUser{}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"users": users})
}

func (h *GovernanceHandler) CreateChUser(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Not authenticated"})
		return
	}

	creds, err := h.getCredentials(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
		return
	}

	var body struct {
		Name         string   `json:"name"`
		Password     string   `json:"password"`
		AuthType     string   `json:"auth_type"`
		DefaultRoles []string `json:"default_roles"`
		IfNotExists  *bool    `json:"if_not_exists"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
		return
	}

	name := strings.TrimSpace(body.Name)
	if name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}

	authType := strings.TrimSpace(strings.ToLower(body.AuthType))
	if authType == "" {
		if strings.TrimSpace(body.Password) == "" {
			authType = "no_password"
		} else {
			authType = "plaintext_password"
		}
	}

	switch authType {
	case "no_password", "plaintext_password", "sha256_password", "double_sha1_password":
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "auth_type must be one of: no_password, plaintext_password, sha256_password, double_sha1_password"})
		return
	}
	if authType != "no_password" && strings.TrimSpace(body.Password) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "password is required for selected auth_type"})
		return
	}

	allRoles, roleNames, parseErr := parseDefaultRolesInput(body.DefaultRoles)
	if parseErr != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": parseErr.Error()})
		return
	}

	var createSQL strings.Builder
	createSQL.WriteString("CREATE USER ")
	if body.IfNotExists == nil || *body.IfNotExists {
		createSQL.WriteString("IF NOT EXISTS ")
	}
	createSQL.WriteString(escapeIdentifier(name))
	createSQL.WriteString(buildClickHouseCreateAuthClause(authType, body.Password))

	if err := h.executeClickHouseSQL(creds, createSQL.String()); err != nil {
		slog.Error("Failed to create ClickHouse user", "connection", session.ConnectionID, "name", name, "error", err)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}

	escapedRoles := make([]string, 0, len(roleNames))
	for _, role := range roleNames {
		escapedRoles = append(escapedRoles, escapeIdentifier(role))
	}
	if len(escapedRoles) > 0 {
		grantSQL := "GRANT " + strings.Join(escapedRoles, ", ") + " TO " + escapeIdentifier(name)
		if err := h.executeClickHouseSQL(creds, grantSQL); err != nil {
			slog.Error("ClickHouse user created but role grant failed", "connection", session.ConnectionID, "name", name, "error", err)
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": fmt.Sprintf("user created but failed to grant roles: %v", err)})
			return
		}
	}
	if allRoles || len(escapedRoles) > 0 {
		defaultRoleClause := "ALL"
		if !allRoles {
			defaultRoleClause = strings.Join(escapedRoles, ", ")
		}
		alterSQL := "ALTER USER " + escapeIdentifier(name) + " DEFAULT ROLE " + defaultRoleClause
		if err := h.executeClickHouseSQL(creds, alterSQL); err != nil {
			slog.Error("ClickHouse user created but default role assignment failed", "connection", session.ConnectionID, "name", name, "error", err)
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": fmt.Sprintf("user created but failed to set default role: %v", err)})
			return
		}
	}

	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:       "governance.access.user.created",
		Username:     strPtr(session.ClickhouseUser),
		ConnectionID: strPtr(session.ConnectionID),
		Details:      strPtr(name),
	})

	h.triggerSyncAsync(*creds, governance.SyncAccess)
	writeJSON(w, http.StatusCreated, map[string]interface{}{"success": true, "name": name})
}

func (h *GovernanceHandler) DeleteChUser(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Not authenticated"})
		return
	}

	creds, err := h.getCredentials(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
		return
	}

	name := strings.TrimSpace(chi.URLParam(r, "name"))
	if name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}

	ifExists := true
	if raw := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("if_exists"))); raw == "false" || raw == "0" {
		ifExists = false
	}

	sql := "DROP USER "
	if ifExists {
		sql += "IF EXISTS "
	}
	sql += escapeIdentifier(name)

	if err := h.executeClickHouseSQL(creds, sql); err != nil {
		slog.Error("Failed to delete ClickHouse user", "connection", session.ConnectionID, "name", name, "error", err)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}

	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:       "governance.access.user.deleted",
		Username:     strPtr(session.ClickhouseUser),
		ConnectionID: strPtr(session.ConnectionID),
		Details:      strPtr(name),
	})

	h.triggerSyncAsync(*creds, governance.SyncAccess)
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (h *GovernanceHandler) ListChRoles(w http.ResponseWriter, r *http.Request) {
	connID := h.connectionID(r)
	if connID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Not authenticated"})
		return
	}

	roles, err := h.Store.GetChRoles(connID)
	if err != nil {
		slog.Error("Failed to list CH roles", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to list roles"})
		return
	}
	if roles == nil {
		roles = []governance.ChRole{}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"roles": roles})
}

func (h *GovernanceHandler) GetAccessMatrix(w http.ResponseWriter, r *http.Request) {
	connID := h.connectionID(r)
	if connID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Not authenticated"})
		return
	}

	user := r.URL.Query().Get("user")
	var matrix []governance.AccessMatrixEntry
	var err error

	if user != "" {
		matrix, err = h.Store.GetAccessMatrixForUser(connID, user)
	} else {
		matrix, err = h.Store.GetAccessMatrix(connID)
	}
	if err != nil {
		slog.Error("Failed to get access matrix", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to get access matrix"})
		return
	}
	if matrix == nil {
		matrix = []governance.AccessMatrixEntry{}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"matrix": matrix})
}

func (h *GovernanceHandler) GetOverPermissions(w http.ResponseWriter, r *http.Request) {
	connID := h.connectionID(r)
	if connID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Not authenticated"})
		return
	}

	days := queryIntBounded(r, "days", 30, 1, 3650)
	perms, err := h.Store.GetOverPermissionsWithDays(connID, days)
	if err != nil {
		slog.Error("Failed to get over-permissions", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to get over-permissions"})
		return
	}
	if perms == nil {
		perms = []governance.OverPermission{}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"over_permissions": perms})
}

// ── Policies ─────────────────────────────────────────────────────────────────

func (h *GovernanceHandler) ListPolicies(w http.ResponseWriter, r *http.Request) {
	connID := h.connectionID(r)
	if connID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Not authenticated"})
		return
	}

	policies, err := h.Store.GetPolicies(connID)
	if err != nil {
		slog.Error("Failed to list policies", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to list policies"})
		return
	}
	if policies == nil {
		policies = []governance.Policy{}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"policies": policies})
}

func (h *GovernanceHandler) CreatePolicy(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Not authenticated"})
		return
	}

	var body struct {
		Name            string `json:"name"`
		Description     string `json:"description"`
		ObjectType      string `json:"object_type"`
		ObjectDatabase  string `json:"object_database"`
		ObjectTable     string `json:"object_table"`
		ObjectColumn    string `json:"object_column"`
		RequiredRole    string `json:"required_role"`
		Severity        string `json:"severity"`
		EnforcementMode string `json:"enforcement_mode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
		return
	}

	if strings.TrimSpace(body.Name) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Policy name is required"})
		return
	}
	if body.ObjectType != "database" && body.ObjectType != "table" && body.ObjectType != "column" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "object_type must be database, table, or column"})
		return
	}
	if body.Severity == "" {
		body.Severity = "warn"
	}
	enforcementMode, err := normalizePolicyEnforcementMode(body.EnforcementMode)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	id, err := h.Store.CreatePolicy(
		session.ConnectionID, body.Name, body.Description, body.ObjectType,
		body.ObjectDatabase, body.ObjectTable, body.ObjectColumn,
		body.RequiredRole, body.Severity, enforcementMode, session.ClickhouseUser,
	)
	if err != nil {
		slog.Error("Failed to create policy", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to create policy"})
		return
	}

	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:       "governance.policy.created",
		Username:     strPtr(session.ClickhouseUser),
		ConnectionID: strPtr(session.ConnectionID),
		Details:      strPtr(body.Name),
	})

	policy, _ := h.Store.GetPolicyByID(id)
	writeJSON(w, http.StatusCreated, map[string]interface{}{"policy": policy})
}

func (h *GovernanceHandler) GetPolicy(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	policy, err := h.Store.GetPolicyByID(id)
	if err != nil {
		slog.Error("Failed to get policy", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to get policy"})
		return
	}
	if policy == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Policy not found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"policy": policy})
}

func (h *GovernanceHandler) UpdatePolicy(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Not authenticated"})
		return
	}

	id := chi.URLParam(r, "id")

	var body struct {
		Name            string `json:"name"`
		Description     string `json:"description"`
		RequiredRole    string `json:"required_role"`
		Severity        string `json:"severity"`
		EnforcementMode string `json:"enforcement_mode"`
		Enabled         *bool  `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
		return
	}

	enforcementMode, err := normalizePolicyEnforcementMode(body.EnforcementMode)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	enabled := true
	if body.Enabled != nil {
		enabled = *body.Enabled
	}

	if err := h.Store.UpdatePolicy(id, body.Name, body.Description, body.RequiredRole, body.Severity, enforcementMode, enabled); err != nil {
		slog.Error("Failed to update policy", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to update policy"})
		return
	}

	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:       "governance.policy.updated",
		Username:     strPtr(session.ClickhouseUser),
		ConnectionID: strPtr(session.ConnectionID),
		Details:      strPtr(id),
	})

	policy, _ := h.Store.GetPolicyByID(id)
	writeJSON(w, http.StatusOK, map[string]interface{}{"policy": policy})
}

func (h *GovernanceHandler) DeletePolicy(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Not authenticated"})
		return
	}

	id := chi.URLParam(r, "id")
	if err := h.Store.DeletePolicy(id); err != nil {
		slog.Error("Failed to delete policy", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to delete policy"})
		return
	}

	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:       "governance.policy.deleted",
		Username:     strPtr(session.ClickhouseUser),
		ConnectionID: strPtr(session.ConnectionID),
		Details:      strPtr(id),
	})

	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

// ── Violations ───────────────────────────────────────────────────────────────

func (h *GovernanceHandler) ListViolations(w http.ResponseWriter, r *http.Request) {
	connID := h.connectionID(r)
	if connID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Not authenticated"})
		return
	}

	limit := queryIntBounded(r, "limit", 50, 1, 500)
	policyID := r.URL.Query().Get("policy_id")

	violations, err := h.Store.GetViolations(connID, limit, policyID)
	if err != nil {
		slog.Error("Failed to list violations", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to list violations"})
		return
	}
	if violations == nil {
		violations = []governance.PolicyViolation{}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"violations": violations})
}

func (h *GovernanceHandler) CreateIncidentFromViolation(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Not authenticated"})
		return
	}

	violationID := strings.TrimSpace(chi.URLParam(r, "id"))
	if violationID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "violation id is required"})
		return
	}

	violation, err := h.Store.GetViolationByID(violationID)
	if err != nil {
		slog.Error("Failed to load violation", "id", violationID, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to load violation"})
		return
	}
	if violation == nil || violation.ConnectionID != session.ConnectionID {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Violation not found"})
		return
	}

	incidentID, created, err := h.Store.UpsertIncidentFromViolation(
		session.ConnectionID,
		violation.ID,
		violation.PolicyName,
		violation.User,
		normalizeIncidentSeverity(violation.Severity),
		violation.ViolationDetail,
	)
	if err != nil {
		slog.Error("Failed to upsert incident from violation", "violation", violation.ID, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to create incident"})
		return
	}

	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:       "governance.incident.from_violation",
		Username:     strPtr(session.ClickhouseUser),
		ConnectionID: strPtr(session.ConnectionID),
		Details:      strPtr(fmt.Sprintf("violation=%s incident=%s created=%t", violation.ID, incidentID, created)),
	})

	writeJSON(w, http.StatusCreated, map[string]interface{}{"incident_id": incidentID, "created": created, "success": true})
}

func (h *GovernanceHandler) ListIncidents(w http.ResponseWriter, r *http.Request) {
	connID := h.connectionID(r)
	if connID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Not authenticated"})
		return
	}

	status := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("status")))
	severity := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("severity")))
	limit := queryIntBounded(r, "limit", 100, 1, 1000)

	incidents, err := h.Store.ListIncidents(connID, status, severity, limit)
	if err != nil {
		slog.Error("Failed to list incidents", "connection", connID, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to list incidents"})
		return
	}
	if incidents == nil {
		incidents = []governance.Incident{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"incidents": incidents})
}

func (h *GovernanceHandler) GetIncident(w http.ResponseWriter, r *http.Request) {
	connID := h.connectionID(r)
	if connID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Not authenticated"})
		return
	}
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "incident id is required"})
		return
	}
	incident, err := h.Store.GetIncidentByID(id)
	if err != nil {
		slog.Error("Failed to load incident", "id", id, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to load incident"})
		return
	}
	if incident == nil || incident.ConnectionID != connID {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Incident not found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"incident": incident})
}

func (h *GovernanceHandler) CreateIncident(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Not authenticated"})
		return
	}

	var body struct {
		SourceType string `json:"source_type"`
		SourceRef  string `json:"source_ref"`
		Title      string `json:"title"`
		Severity   string `json:"severity"`
		Status     string `json:"status"`
		Assignee   string `json:"assignee"`
		Details    string `json:"details"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
		return
	}
	title := strings.TrimSpace(body.Title)
	if title == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "title is required"})
		return
	}
	severity := normalizeIncidentSeverity(body.Severity)
	status := normalizeIncidentStatus(body.Status)
	sourceType := strings.TrimSpace(strings.ToLower(body.SourceType))
	if sourceType == "" {
		sourceType = "manual"
	}
	if sourceType != "manual" && sourceType != "violation" && sourceType != "over_permission" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "source_type must be manual, violation, or over_permission"})
		return
	}

	id, err := h.Store.CreateIncident(
		session.ConnectionID,
		sourceType,
		body.SourceRef,
		"",
		title,
		severity,
		status,
		body.Assignee,
		body.Details,
		session.ClickhouseUser,
	)
	if err != nil {
		slog.Error("Failed to create incident", "connection", session.ConnectionID, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to create incident"})
		return
	}
	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:       "governance.incident.created",
		Username:     strPtr(session.ClickhouseUser),
		ConnectionID: strPtr(session.ConnectionID),
		Details:      strPtr(id),
	})
	writeJSON(w, http.StatusCreated, map[string]interface{}{"id": id, "success": true})
}

func (h *GovernanceHandler) UpdateIncident(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Not authenticated"})
		return
	}
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "incident id is required"})
		return
	}
	existing, err := h.Store.GetIncidentByID(id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to load incident"})
		return
	}
	if existing == nil || existing.ConnectionID != session.ConnectionID {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Incident not found"})
		return
	}

	var body struct {
		Title          *string `json:"title"`
		Severity       *string `json:"severity"`
		Status         *string `json:"status"`
		Assignee       *string `json:"assignee"`
		Details        *string `json:"details"`
		ResolutionNote *string `json:"resolution_note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
		return
	}

	title := existing.Title
	if body.Title != nil {
		title = strings.TrimSpace(*body.Title)
	}
	if title == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "title is required"})
		return
	}

	severity := existing.Severity
	if body.Severity != nil {
		severity = normalizeIncidentSeverity(*body.Severity)
	}

	status := existing.Status
	if body.Status != nil {
		status = normalizeIncidentStatus(*body.Status)
	}

	assignee := derefString(existing.Assignee)
	if body.Assignee != nil {
		assignee = strings.TrimSpace(*body.Assignee)
	}
	details := derefString(existing.Details)
	if body.Details != nil {
		details = strings.TrimSpace(*body.Details)
	}
	resolution := derefString(existing.ResolutionNote)
	if body.ResolutionNote != nil {
		resolution = strings.TrimSpace(*body.ResolutionNote)
	}

	if err := h.Store.UpdateIncident(id, title, severity, status, assignee, details, resolution); err != nil {
		slog.Error("Failed to update incident", "id", id, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to update incident"})
		return
	}
	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:       "governance.incident.updated",
		Username:     strPtr(session.ClickhouseUser),
		ConnectionID: strPtr(session.ConnectionID),
		Details:      strPtr(id),
	})
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (h *GovernanceHandler) ListIncidentComments(w http.ResponseWriter, r *http.Request) {
	connID := h.connectionID(r)
	if connID == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Not authenticated"})
		return
	}
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "incident id is required"})
		return
	}
	incident, err := h.Store.GetIncidentByID(id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to load incident"})
		return
	}
	if incident == nil || incident.ConnectionID != connID {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Incident not found"})
		return
	}
	comments, err := h.Store.ListIncidentComments(id, 500)
	if err != nil {
		slog.Error("Failed to list incident comments", "id", id, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to list incident comments"})
		return
	}
	if comments == nil {
		comments = []governance.IncidentComment{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"comments": comments})
}

func (h *GovernanceHandler) CreateIncidentComment(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Not authenticated"})
		return
	}
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "incident id is required"})
		return
	}
	incident, err := h.Store.GetIncidentByID(id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to load incident"})
		return
	}
	if incident == nil || incident.ConnectionID != session.ConnectionID {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Incident not found"})
		return
	}
	var body struct {
		CommentText string `json:"comment_text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
		return
	}
	comment := strings.TrimSpace(body.CommentText)
	if comment == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "comment_text is required"})
		return
	}
	if len(comment) > 4000 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "comment_text must be <= 4000 characters"})
		return
	}
	commentID, err := h.Store.CreateIncidentComment(id, comment, session.ClickhouseUser)
	if err != nil {
		slog.Error("Failed to create incident comment", "id", id, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to create incident comment"})
		return
	}
	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:       "governance.incident.comment.created",
		Username:     strPtr(session.ClickhouseUser),
		ConnectionID: strPtr(session.ConnectionID),
		Details:      strPtr(id),
	})
	writeJSON(w, http.StatusCreated, map[string]interface{}{"id": commentID, "success": true})
}

func normalizeIncidentSeverity(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "info", "warn", "error", "critical":
		return strings.ToLower(strings.TrimSpace(v))
	default:
		return "warn"
	}
}

func normalizePolicyEnforcementMode(v string) (string, error) {
	mode := strings.ToLower(strings.TrimSpace(v))
	switch mode {
	case "", "warn":
		return "warn", nil
	case "block":
		return "block", nil
	default:
		return "", fmt.Errorf("enforcement_mode must be warn or block")
	}
}

func normalizeIncidentStatus(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "open", "triaged", "in_progress", "resolved", "dismissed":
		return strings.ToLower(strings.TrimSpace(v))
	default:
		return "open"
	}
}

func derefString(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}
