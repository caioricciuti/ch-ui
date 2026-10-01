package handlers

import (
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
)

// QueryHistoryHandler serves the per-person query execution history (keyed by
// middleware.Actor, so SSO people sharing a ClickHouse account stay apart).
type QueryHistoryHandler struct {
	DB     *database.DB
	Config *config.Config
}

// Routes registers query history routes on the given router.
func (h *QueryHistoryHandler) Routes(r chi.Router) {
	r.Get("/", h.List)
	r.Delete("/{id}", h.DeleteEntry)
	r.Delete("/", h.Clear)
}

// List returns the current person's history on the current connection,
// most recent first. Supports ?search=, ?status=, ?limit=, ?offset=.
func (h *QueryHistoryHandler) List(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}

	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))

	entries, err := h.DB.GetQueryHistory(
		middleware.Actor(session),
		session.ConnectionID,
		q.Get("search"),
		q.Get("status"),
		limit,
		offset,
	)
	if err != nil {
		slog.Error("Failed to list query history", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to fetch query history")
		return
	}
	if entries == nil {
		entries = []database.QueryHistoryEntry{}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"entries": entries})
}

// DeleteEntry deletes a single history entry owned by the current person.
func (h *QueryHistoryHandler) DeleteEntry(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}

	id := chi.URLParam(r, "id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "Entry ID is required")
		return
	}

	if err := h.DB.DeleteQueryHistoryEntry(id, middleware.Actor(session), session.ConnectionID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "History entry not found")
			return
		}
		slog.Error("Failed to delete query history entry", "error", err, "id", id)
		writeError(w, http.StatusInternalServerError, "Failed to delete history entry")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

// Clear deletes all of the current person's history on the current connection.
func (h *QueryHistoryHandler) Clear(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}

	if err := h.DB.ClearQueryHistory(middleware.Actor(session), session.ConnectionID); err != nil {
		slog.Error("Failed to clear query history", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to clear query history")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}
