package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"sync"

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
	// view, and run the saved queries they can see). Owner checks happen in
	// the handlers: only the creator or an admin may change a query.
	writer := middleware.RequireWriter()

	r.Get("/", h.List)
	r.With(writer).Post("/", h.Create)

	// Folders are global and nest; any writer may manage them.
	r.Get("/folders", h.ListFolders)
	r.With(writer).Post("/folders", h.CreateFolder)
	r.With(writer).Put("/folders/{folderId}", h.UpdateFolder)
	r.With(writer).Delete("/folders/{folderId}", h.DeleteFolder)

	r.Get("/{id}", h.Get)
	r.With(writer).Put("/{id}", h.Update)
	r.With(writer).Delete("/{id}", h.Delete)
	r.With(writer).Post("/{id}/duplicate", h.Duplicate)
	r.Get("/{id}/versions", h.ListVersions)
	r.Get("/{id}/versions/{version}", h.GetVersion)
	r.With(writer).Post("/{id}/versions/{version}/restore", h.RestoreVersion)
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

// List returns the saved queries the caller can see. Query parameters:
// scope=all|mine|shared|verified, folder=<id> (that folder and its
// subfolders) or folder=root, owners=all (admins: include other people's
// private queries).
func (h *SavedQueriesHandler) List(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}
	viewer := listViewer(session, r.URL.Query().Get("owners") == "all")
	queries, err := h.DB.ListSavedQueriesVisibleTo(viewer)
	if err != nil {
		slog.Error("Failed to list saved queries", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to fetch saved queries")
		return
	}

	scope := strings.TrimSpace(r.URL.Query().Get("scope"))
	folder := strings.TrimSpace(r.URL.Query().Get("folder"))
	var inFolder map[string]bool
	if folder != "" && folder != "root" {
		inFolder, err = h.folderSubtree(folder)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "Failed to load folders")
			return
		}
	}

	out := make([]database.SavedQuery, 0, len(queries))
	for _, q := range queries {
		switch scope {
		case "mine":
			if q.Owner != viewer.Actor {
				continue
			}
		case "shared":
			if q.Visibility != database.SavedQueryShared {
				continue
			}
		case "verified":
			if !q.Verified {
				continue
			}
		}
		switch {
		case folder == "root":
			if q.FolderID != nil {
				continue
			}
		case inFolder != nil:
			if q.FolderID == nil || !inFolder[*q.FolderID] {
				continue
			}
		}
		q.CanEdit = database.CanEditSavedQuery(&q, viewer)
		out = append(out, q)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"saved_queries": out})
}

// Get returns a single saved query by ID.
func (h *SavedQueriesHandler) Get(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}
	q := h.loadSavedQuery(w, r, session, false)
	if q == nil {
		return
	}
	writeJSON(w, http.StatusOK, q)
}

// savedQueryBody is the JSON body of create and update.
type savedQueryBody struct {
	Name         *string           `json:"name"`
	Description  *string           `json:"description"`
	Query        *string           `json:"query"`
	Parameters   map[string]string `json:"parameters"`
	ConnectionID *string           `json:"connection_id"`
	Verified     *bool             `json:"verified"`
	FolderID     *string           `json:"folder_id"`
	Tags         *[]string         `json:"tags"`
	Visibility   *string           `json:"visibility"`
	// IfVersion is the version the editor loaded. When set, the update is
	// refused with 409 if the query has been saved or restored since.
	IfVersion *int `json:"if_version"`
}

// savedQueryWrites serialises content writes (update, restore), so the
// version check and the write it guards cannot interleave with another save.
var savedQueryWrites sync.Mutex

// Create creates a new saved query owned by the caller and records it as
// version 1.
func (h *SavedQueriesHandler) Create(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}

	var body savedQueryBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	name := strings.TrimSpace(deref(body.Name))
	sqlQuery := strings.TrimSpace(deref(body.Query))
	if name == "" {
		writeError(w, http.StatusBadRequest, "Name is required")
		return
	}
	if sqlQuery == "" {
		writeError(w, http.StatusBadRequest, "Query is required")
		return
	}

	connectionID := strings.TrimSpace(deref(body.ConnectionID))
	if connectionID == "" {
		connectionID = session.ConnectionID
	}
	visibility := database.SavedQueryShared
	if body.Visibility != nil && strings.TrimSpace(*body.Visibility) != "" {
		visibility = strings.TrimSpace(*body.Visibility)
	}
	if !h.checkVisibility(w, session, visibility) {
		return
	}
	folderID := strings.TrimSpace(deref(body.FolderID))
	if !h.checkFolder(w, folderID) {
		return
	}
	var tags []string
	if body.Tags != nil {
		if !checkSavedQueryTags(w, *body.Tags) {
			return
		}
		tags = database.NormalizeSavedQueryTags(*body.Tags)
	}

	actor := middleware.Actor(session)
	id, err := h.DB.CreateSavedQuery(database.CreateSavedQueryParams{
		Name:         name,
		Description:  strings.TrimSpace(deref(body.Description)),
		Query:        sqlQuery,
		Parameters:   marshalParams(body.Parameters),
		ConnectionID: connectionID,
		CreatedBy:    actor,
		Verified:     body.Verified != nil && *body.Verified,
		FolderID:     folderID,
		Tags:         tags,
		Visibility:   visibility,
	})
	if err != nil {
		slog.Error("Failed to create saved query", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to create saved query")
		return
	}
	h.recordVersion(id, actor, "Created")

	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:         "saved_query.created",
		Username:       strPtr(actor),
		ClickhouseUser: &session.ClickhouseUser,
		Details:        strPtr(name),
	})

	created, err := h.DB.GetSavedQueryByID(id)
	if err != nil || created == nil {
		writeJSON(w, http.StatusCreated, map[string]string{"id": id})
		return
	}
	created.CanEdit = true
	writeJSON(w, http.StatusCreated, created)
}

// Update changes a saved query. Content changes (name, description, SQL,
// parameters, connection) are recorded as a new version; folder, tags and
// visibility are not part of the history.
func (h *SavedQueriesHandler) Update(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}
	savedQueryWrites.Lock()
	defer savedQueryWrites.Unlock()
	existing := h.loadSavedQuery(w, r, session, true)
	if existing == nil {
		return
	}
	id := existing.ID
	viewer := byIDViewer(session)
	actor := viewer.Actor

	var body savedQueryBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if body.IfVersion != nil && *body.IfVersion != existing.Version {
		// Someone saved or restored it since the editor loaded it. The
		// current state goes back so the editor can offer what to do.
		writeJSON(w, http.StatusConflict, map[string]interface{}{
			"error":       "This saved query changed since you opened it",
			"code":        "saved_query_conflict",
			"saved_query": existing,
		})
		return
	}

	params := database.UpdateSavedQueryParams{
		Name:         existing.Name,
		Description:  deref(existing.Description),
		Query:        existing.Query,
		Parameters:   deref(existing.Parameters),
		ConnectionID: deref(existing.ConnectionID),
		Verified:     existing.Verified,
	}

	contentChanged := false
	if body.Name != nil {
		n := strings.TrimSpace(*body.Name)
		if n == "" {
			writeError(w, http.StatusBadRequest, "Name cannot be empty")
			return
		}
		contentChanged = contentChanged || n != params.Name
		params.Name = n
	}
	if body.Description != nil {
		d := strings.TrimSpace(*body.Description)
		contentChanged = contentChanged || d != params.Description
		params.Description = d
	}
	if body.Query != nil {
		q := strings.TrimSpace(*body.Query)
		if q == "" {
			writeError(w, http.StatusBadRequest, "Query cannot be empty")
			return
		}
		contentChanged = contentChanged || q != params.Query
		params.Query = q
	}
	if body.Parameters != nil {
		p := marshalParams(body.Parameters)
		contentChanged = contentChanged || p != params.Parameters
		params.Parameters = p
	}
	if body.ConnectionID != nil {
		c := strings.TrimSpace(*body.ConnectionID)
		contentChanged = contentChanged || c != params.ConnectionID
		params.ConnectionID = c
	}
	verifiedChanged := body.Verified != nil && *body.Verified != existing.Verified
	if verifiedChanged {
		if !database.CanVerifySavedQuery(existing, viewer) {
			writeError(w, http.StatusForbidden, "Only the owner or an admin can change the verified mark")
			return
		}
		params.Verified = *body.Verified
	}

	folderID := deref(existing.FolderID)
	folderChanged := false
	if body.FolderID != nil {
		f := strings.TrimSpace(*body.FolderID)
		if f != folderID {
			if !h.checkFolder(w, f) {
				return
			}
			folderID, folderChanged = f, true
		}
	}
	var tags []string
	tagsChanged := false
	if body.Tags != nil {
		if !checkSavedQueryTags(w, *body.Tags) {
			return
		}
		tags = database.NormalizeSavedQueryTags(*body.Tags)
		tagsChanged = strings.Join(tags, "\x00") != strings.Join(existing.Tags, "\x00")
	}
	visibility := existing.Visibility
	visibilityChanged := false
	if body.Visibility != nil {
		v := strings.TrimSpace(*body.Visibility)
		if v != existing.Visibility {
			if !h.checkVisibility(w, session, v) {
				return
			}
			visibility, visibilityChanged = v, true
		}
	}

	if !contentChanged && !verifiedChanged && !folderChanged && !tagsChanged && !visibilityChanged {
		existing.CanEdit = true
		writeJSON(w, http.StatusOK, existing)
		return
	}

	if contentChanged || verifiedChanged {
		if contentChanged {
			if err := h.DB.EnsureSavedQueryBaseline(id); err != nil {
				slog.Warn("Failed to record baseline saved query version", "error", err, "id", id)
			}
		}
		if err := h.DB.UpdateSavedQuery(id, params); err != nil {
			slog.Error("Failed to update saved query", "error", err, "id", id)
			writeError(w, http.StatusInternalServerError, "Failed to update saved query")
			return
		}
		if contentChanged {
			h.recordVersion(id, actor, "")
		}
		h.audit(session, "saved_query.updated", params.Name)
	}
	if folderChanged {
		if err := h.DB.SetSavedQueryFolder(id, folderID); err != nil {
			slog.Error("Failed to move saved query", "error", err, "id", id)
			writeError(w, http.StatusInternalServerError, "Failed to move saved query")
			return
		}
		dest := "root"
		if folderID != "" {
			if f, _ := h.DB.GetSavedQueryFolder(folderID); f != nil {
				dest = f.Name
			}
		}
		h.audit(session, "saved_query.moved", params.Name+" to "+dest)
	}
	if tagsChanged {
		if err := h.DB.SetSavedQueryTags(id, tags); err != nil {
			slog.Error("Failed to set saved query tags", "error", err, "id", id)
			writeError(w, http.StatusInternalServerError, "Failed to set tags")
			return
		}
	}
	if visibilityChanged {
		if err := h.DB.SetSavedQueryVisibility(id, visibility, actor); err != nil {
			slog.Error("Failed to set saved query visibility", "error", err, "id", id)
			writeError(w, http.StatusInternalServerError, "Failed to change visibility")
			return
		}
		h.audit(session, "saved_query.visibility_changed", params.Name+": "+existing.Visibility+" to "+visibility)
	}

	updated, err := h.DB.GetSavedQueryByID(id)
	if err != nil || updated == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
		return
	}
	updated.CanEdit = database.CanEditSavedQuery(updated, viewer)
	writeJSON(w, http.StatusOK, updated)
}

// Delete removes a saved query.
func (h *SavedQueriesHandler) Delete(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}
	existing := h.loadSavedQuery(w, r, session, true)
	if existing == nil {
		return
	}

	if err := h.DB.DeleteSavedQuery(existing.ID); err != nil {
		slog.Error("Failed to delete saved query", "error", err, "id", existing.ID)
		writeError(w, http.StatusInternalServerError, "Failed to delete saved query")
		return
	}
	h.audit(session, "saved_query.deleted", existing.Name)
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

// Duplicate copies a saved query the caller can see. The copy belongs to
// the caller, is private, and keeps the folder and tags. An optional "name"
// in the body sets the copy's name; otherwise it is "<original> (copy)".
func (h *SavedQueriesHandler) Duplicate(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}
	original := h.loadSavedQuery(w, r, session, false)
	if original == nil {
		return
	}

	var body struct {
		Name string `json:"name"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	newName := strings.TrimSpace(body.Name)
	if newName == "" {
		newName = strings.TrimSpace(original.Name + " (copy)")
	}

	actor := middleware.Actor(session)
	newID, err := h.DB.CreateSavedQuery(database.CreateSavedQueryParams{
		Name:         newName,
		Description:  deref(original.Description),
		Query:        original.Query,
		Parameters:   deref(original.Parameters),
		ConnectionID: deref(original.ConnectionID),
		CreatedBy:    actor,
		FolderID:     deref(original.FolderID),
		Tags:         original.Tags,
		Visibility:   database.SavedQueryPrivate,
	})
	if err != nil {
		slog.Error("Failed to duplicate saved query", "error", err, "id", original.ID)
		writeError(w, http.StatusInternalServerError, "Failed to duplicate saved query")
		return
	}
	h.recordVersion(newID, actor, "Duplicated from "+original.Name)
	h.audit(session, "saved_query.duplicated", "Duplicated from "+original.ID)

	duplicated, err := h.DB.GetSavedQueryByID(newID)
	if err != nil || duplicated == nil {
		writeJSON(w, http.StatusCreated, map[string]string{"id": newID})
		return
	}
	duplicated.CanEdit = true
	writeJSON(w, http.StatusCreated, duplicated)
}

// deref returns the string a pointer points to, or "" when nil.
func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
