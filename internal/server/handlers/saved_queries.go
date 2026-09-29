package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

// SavedQueriesHandler handles saved query CRUD operations.
type SavedQueriesHandler struct {
	DB      *database.DB
	Gateway *tunnel.Gateway
	Config  *config.Config
}

// Routes registers saved query routes on the given router.
func (h *SavedQueriesHandler) Routes(r chi.Router) {
	// Writes require admin/analyst; viewers are read-only (they can still list,
	// view, and run saved queries).
	writer := middleware.RequireWriter()

	r.Get("/", h.List)
	r.Get("/{id}", h.Get)
	r.With(writer).Post("/", h.Create)
	r.With(writer).Put("/{id}", h.Update)
	r.With(writer).Delete("/{id}", h.Delete)
	r.With(writer).Post("/{id}/duplicate", h.Duplicate)
	// Executing a saved query with bind parameters is a Pro feature.
	if h.Config != nil {
		r.With(middleware.RequirePro(h.Config)).Post("/{id}/run", h.Run)
	}
}

// marshalParams serialises a {name: value} bind-parameter map to a JSON string
// for storage. Returns "" for an empty map so it stores SQL NULL.
func marshalParams(p map[string]string) string {
	if len(p) == 0 {
		return ""
	}
	b, err := json.Marshal(p)
	if err != nil {
		return ""
	}
	return string(b)
}

// List returns all saved queries.
func (h *SavedQueriesHandler) List(w http.ResponseWriter, r *http.Request) {
	queries, err := h.DB.GetSavedQueries()
	if err != nil {
		slog.Error("Failed to list saved queries", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to fetch saved queries")
		return
	}

	if queries == nil {
		queries = []database.SavedQuery{}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"saved_queries": queries})
}

// Get returns a single saved query by ID.
func (h *SavedQueriesHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "Query ID is required")
		return
	}

	query, err := h.DB.GetSavedQueryByID(id)
	if err != nil {
		slog.Error("Failed to get saved query", "error", err, "id", id)
		writeError(w, http.StatusInternalServerError, "Failed to fetch saved query")
		return
	}
	if query == nil {
		writeError(w, http.StatusNotFound, "Saved query not found")
		return
	}

	writeJSON(w, http.StatusOK, query)
}

// Create creates a new saved query.
func (h *SavedQueriesHandler) Create(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}

	var body struct {
		Name         string            `json:"name"`
		Description  string            `json:"description"`
		Query        string            `json:"query"`
		Parameters   map[string]string `json:"parameters"`
		ConnectionID string            `json:"connection_id"`
		Verified     bool              `json:"verified"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	name := strings.TrimSpace(body.Name)
	sqlQuery := strings.TrimSpace(body.Query)
	if name == "" {
		writeError(w, http.StatusBadRequest, "Name is required")
		return
	}
	if sqlQuery == "" {
		writeError(w, http.StatusBadRequest, "Query is required")
		return
	}

	connectionID := strings.TrimSpace(body.ConnectionID)
	if connectionID == "" {
		connectionID = session.ConnectionID
	}

	id, err := h.DB.CreateSavedQuery(database.CreateSavedQueryParams{
		Name:         name,
		Description:  strings.TrimSpace(body.Description),
		Query:        sqlQuery,
		Parameters:   marshalParams(body.Parameters),
		ConnectionID: connectionID,
		CreatedBy:    middleware.Actor(session),
		Verified:     body.Verified,
	})
	if err != nil {
		slog.Error("Failed to create saved query", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to create saved query")
		return
	}

	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:         "saved_query.created",
		Username:       strPtr(middleware.Actor(session)),
		ClickhouseUser: &session.ClickhouseUser,
		Details:        strPtr(name),
	})

	created, err := h.DB.GetSavedQueryByID(id)
	if err != nil || created == nil {
		writeJSON(w, http.StatusCreated, map[string]string{"id": id})
		return
	}

	writeJSON(w, http.StatusCreated, created)
}

// Update updates an existing saved query.
func (h *SavedQueriesHandler) Update(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}

	id := chi.URLParam(r, "id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "Query ID is required")
		return
	}

	existing, err := h.DB.GetSavedQueryByID(id)
	if err != nil {
		slog.Error("Failed to get saved query for update", "error", err, "id", id)
		writeError(w, http.StatusInternalServerError, "Failed to fetch saved query")
		return
	}
	if existing == nil {
		writeError(w, http.StatusNotFound, "Saved query not found")
		return
	}

	var body struct {
		Name         *string           `json:"name"`
		Description  *string           `json:"description"`
		Query        *string           `json:"query"`
		Parameters   map[string]string `json:"parameters"`
		ConnectionID *string           `json:"connection_id"`
		Verified     *bool             `json:"verified"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	params := database.UpdateSavedQueryParams{
		Name:       existing.Name,
		Query:      existing.Query,
		Parameters: deref(existing.Parameters),
		Verified:   existing.Verified,
	}
	if existing.Description != nil {
		params.Description = *existing.Description
	}
	if existing.ConnectionID != nil {
		params.ConnectionID = *existing.ConnectionID
	}

	changed := false
	if body.Name != nil {
		n := strings.TrimSpace(*body.Name)
		if n == "" {
			writeError(w, http.StatusBadRequest, "Name cannot be empty")
			return
		}
		params.Name = n
		changed = true
	}
	if body.Description != nil {
		params.Description = strings.TrimSpace(*body.Description)
		changed = true
	}
	if body.Query != nil {
		q := strings.TrimSpace(*body.Query)
		if q == "" {
			writeError(w, http.StatusBadRequest, "Query cannot be empty")
			return
		}
		params.Query = q
		changed = true
	}
	if body.Parameters != nil {
		params.Parameters = marshalParams(body.Parameters)
		changed = true
	}
	if body.ConnectionID != nil {
		params.ConnectionID = strings.TrimSpace(*body.ConnectionID)
		changed = true
	}
	if body.Verified != nil {
		params.Verified = *body.Verified
		changed = true
	}

	if !changed {
		writeError(w, http.StatusBadRequest, "No valid fields to update")
		return
	}

	if err := h.DB.UpdateSavedQuery(id, params); err != nil {
		slog.Error("Failed to update saved query", "error", err, "id", id)
		writeError(w, http.StatusInternalServerError, "Failed to update saved query")
		return
	}

	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:         "saved_query.updated",
		Username:       strPtr(middleware.Actor(session)),
		ClickhouseUser: &session.ClickhouseUser,
		Details:        strPtr(params.Name),
	})

	updated, err := h.DB.GetSavedQueryByID(id)
	if err != nil || updated == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
		return
	}

	writeJSON(w, http.StatusOK, updated)
}

// Delete removes a saved query.
func (h *SavedQueriesHandler) Delete(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}

	id := chi.URLParam(r, "id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "Query ID is required")
		return
	}

	existing, err := h.DB.GetSavedQueryByID(id)
	if err != nil {
		slog.Error("Failed to get saved query for delete", "error", err, "id", id)
		writeError(w, http.StatusInternalServerError, "Failed to fetch saved query")
		return
	}
	if existing == nil {
		writeError(w, http.StatusNotFound, "Saved query not found")
		return
	}

	if err := h.DB.DeleteSavedQuery(id); err != nil {
		slog.Error("Failed to delete saved query", "error", err, "id", id)
		writeError(w, http.StatusInternalServerError, "Failed to delete saved query")
		return
	}

	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:         "saved_query.deleted",
		Username:       strPtr(middleware.Actor(session)),
		ClickhouseUser: &session.ClickhouseUser,
		Details:        strPtr(existing.Name),
	})

	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

// Duplicate creates a copy of an existing saved query. An optional "name" in the
// body sets the copy's name; otherwise it falls back to "<original> (copy)".
func (h *SavedQueriesHandler) Duplicate(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}

	id := chi.URLParam(r, "id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "Query ID is required")
		return
	}

	original, err := h.DB.GetSavedQueryByID(id)
	if err != nil {
		slog.Error("Failed to get saved query for duplicate", "error", err, "id", id)
		writeError(w, http.StatusInternalServerError, "Failed to fetch saved query")
		return
	}
	if original == nil {
		writeError(w, http.StatusNotFound, "Saved query not found")
		return
	}

	// Optional custom name for the duplicate.
	var body struct {
		Name string `json:"name"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	newName := strings.TrimSpace(body.Name)
	if newName == "" {
		newName = strings.TrimSpace(original.Name + " (copy)")
	}

	description := ""
	if original.Description != nil {
		description = *original.Description
	}
	connectionID := ""
	if original.ConnectionID != nil {
		connectionID = *original.ConnectionID
	}

	newID, err := h.DB.CreateSavedQuery(database.CreateSavedQueryParams{
		Name:         newName,
		Description:  description,
		Query:        original.Query,
		Parameters:   deref(original.Parameters),
		ConnectionID: connectionID,
		CreatedBy:    middleware.Actor(session),
	})
	if err != nil {
		slog.Error("Failed to duplicate saved query", "error", err, "id", id)
		writeError(w, http.StatusInternalServerError, "Failed to duplicate saved query")
		return
	}

	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:         "saved_query.duplicated",
		Username:       strPtr(middleware.Actor(session)),
		ClickhouseUser: &session.ClickhouseUser,
		Details:        strPtr("Duplicated from " + id),
	})

	duplicated, err := h.DB.GetSavedQueryByID(newID)
	if err != nil || duplicated == nil {
		writeJSON(w, http.StatusCreated, map[string]string{"id": newID})
		return
	}

	writeJSON(w, http.StatusCreated, duplicated)
}

// deref returns the string a pointer points to, or "" when nil.
func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
