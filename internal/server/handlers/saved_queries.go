package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
)

// SavedQueriesHandler handles saved query CRUD operations.
type SavedQueriesHandler struct {
	DB *database.DB
}

// Routes registers saved query routes on the given router.
func (h *SavedQueriesHandler) Routes(r chi.Router) {
	r.Get("/", h.List)
	r.Get("/{id}", h.Get)
	r.Post("/", h.Create)
	r.Put("/{id}", h.Update)
	r.Delete("/{id}", h.Delete)
	r.Post("/{id}/duplicate", h.Duplicate)
}

// List returns all saved queries.
func (h *SavedQueriesHandler) List(w http.ResponseWriter, r *http.Request) {
	queries, err := h.DB.GetSavedQueries()
	if err != nil {
		slog.Error("Failed to list saved queries", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to fetch saved queries"})
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
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Query ID is required"})
		return
	}

	query, err := h.DB.GetSavedQueryByID(id)
	if err != nil {
		slog.Error("Failed to get saved query", "error", err, "id", id)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to fetch saved query"})
		return
	}
	if query == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Saved query not found"})
		return
	}

	writeJSON(w, http.StatusOK, query)
}

// Create creates a new saved query.
func (h *SavedQueriesHandler) Create(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Not authenticated"})
		return
	}

	var body struct {
		Name         string `json:"name"`
		Description  string `json:"description"`
		Query        string `json:"query"`
		ConnectionID string `json:"connection_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
		return
	}

	name := strings.TrimSpace(body.Name)
	sqlQuery := strings.TrimSpace(body.Query)
	if name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Name is required"})
		return
	}
	if sqlQuery == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Query is required"})
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
		ConnectionID: connectionID,
		CreatedBy:    session.ClickhouseUser,
	})
	if err != nil {
		slog.Error("Failed to create saved query", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to create saved query"})
		return
	}

	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:   "saved_query.created",
		Username: strPtr(session.ClickhouseUser),
		Details:  strPtr(name),
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
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Not authenticated"})
		return
	}

	id := chi.URLParam(r, "id")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Query ID is required"})
		return
	}

	existing, err := h.DB.GetSavedQueryByID(id)
	if err != nil {
		slog.Error("Failed to get saved query for update", "error", err, "id", id)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to fetch saved query"})
		return
	}
	if existing == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Saved query not found"})
		return
	}

	var body struct {
		Name         *string `json:"name"`
		Description  *string `json:"description"`
		Query        *string `json:"query"`
		ConnectionID *string `json:"connection_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
		return
	}

	name := existing.Name
	description := ""
	if existing.Description != nil {
		description = *existing.Description
	}
	query := existing.Query
	connectionID := ""
	if existing.ConnectionID != nil {
		connectionID = *existing.ConnectionID
	}

	changed := false
	if body.Name != nil {
		n := strings.TrimSpace(*body.Name)
		if n == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Name cannot be empty"})
			return
		}
		name = n
		changed = true
	}
	if body.Description != nil {
		description = strings.TrimSpace(*body.Description)
		changed = true
	}
	if body.Query != nil {
		q := strings.TrimSpace(*body.Query)
		if q == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Query cannot be empty"})
			return
		}
		query = q
		changed = true
	}
	if body.ConnectionID != nil {
		connectionID = strings.TrimSpace(*body.ConnectionID)
		changed = true
	}

	if !changed {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "No valid fields to update"})
		return
	}

	if err := h.DB.UpdateSavedQuery(id, name, description, query, connectionID); err != nil {
		slog.Error("Failed to update saved query", "error", err, "id", id)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to update saved query"})
		return
	}

	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:   "saved_query.updated",
		Username: strPtr(session.ClickhouseUser),
		Details:  strPtr(name),
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
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Not authenticated"})
		return
	}

	id := chi.URLParam(r, "id")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Query ID is required"})
		return
	}

	existing, err := h.DB.GetSavedQueryByID(id)
	if err != nil {
		slog.Error("Failed to get saved query for delete", "error", err, "id", id)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to fetch saved query"})
		return
	}
	if existing == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Saved query not found"})
		return
	}

	if err := h.DB.DeleteSavedQuery(id); err != nil {
		slog.Error("Failed to delete saved query", "error", err, "id", id)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to delete saved query"})
		return
	}

	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:   "saved_query.deleted",
		Username: strPtr(session.ClickhouseUser),
		Details:  strPtr(existing.Name),
	})

	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

// Duplicate creates a copy of an existing saved query.
func (h *SavedQueriesHandler) Duplicate(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Not authenticated"})
		return
	}

	id := chi.URLParam(r, "id")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Query ID is required"})
		return
	}

	original, err := h.DB.GetSavedQueryByID(id)
	if err != nil {
		slog.Error("Failed to get saved query for duplicate", "error", err, "id", id)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to fetch saved query"})
		return
	}
	if original == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Saved query not found"})
		return
	}

	newName := strings.TrimSpace(original.Name + " (copy)")
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
		ConnectionID: connectionID,
		CreatedBy:    session.ClickhouseUser,
	})
	if err != nil {
		slog.Error("Failed to duplicate saved query", "error", err, "id", id)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to duplicate saved query"})
		return
	}

	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:   "saved_query.duplicated",
		Username: strPtr(session.ClickhouseUser),
		Details:  strPtr("Duplicated from " + id),
	})

	duplicated, err := h.DB.GetSavedQueryByID(newID)
	if err != nil || duplicated == nil {
		writeJSON(w, http.StatusCreated, map[string]string{"id": newID})
		return
	}

	writeJSON(w, http.StatusCreated, duplicated)
}
