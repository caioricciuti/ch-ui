package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
)

// ---------- Visibility and ownership ----------

// listViewer is who lists saved queries. Admins see other people's private
// queries only when they ask for all owners (offboarding).
func listViewer(session *middleware.SessionInfo, allOwners bool) database.SavedQueryViewer {
	if session == nil {
		return database.SavedQueryViewer{}
	}
	return database.SavedQueryViewer{
		Actor:     middleware.Actor(session),
		Role:      session.UserRole,
		AllOwners: allOwners && session.UserRole == "admin",
	}
}

// byIDViewer is who opens one saved query. An admin may open any query by
// id, so the All owners list can be acted on.
func byIDViewer(session *middleware.SessionInfo) database.SavedQueryViewer {
	return listViewer(session, true)
}

// loadSavedQuery loads the saved query named by the {id} URL parameter.
// A query the caller cannot see is 404, never 403. With forWrite only the
// owner, an admin, or any writer for a legacy row without owner passes.
// On failure it has written the error and returns nil.
func (h *SavedQueriesHandler) loadSavedQuery(w http.ResponseWriter, r *http.Request, session *middleware.SessionInfo, forWrite bool) *database.SavedQuery {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "Query ID is required")
		return nil
	}
	viewer := byIDViewer(session)
	q, err := h.DB.GetSavedQueryVisibleTo(id, viewer)
	if err != nil {
		slog.Error("Failed to get saved query", "error", err, "id", id)
		writeError(w, http.StatusInternalServerError, "Failed to fetch saved query")
		return nil
	}
	if q == nil {
		writeError(w, http.StatusNotFound, "Saved query not found")
		return nil
	}
	q.CanEdit = database.CanEditSavedQuery(q, viewer)
	if forWrite && !q.CanEdit {
		writeError(w, http.StatusForbidden, "Only the owner or an admin can change this saved query")
		return nil
	}
	return q
}

// checkVisibility validates a visibility value; only admins may restrict a
// query to admins. On failure it has written the error.
func (h *SavedQueriesHandler) checkVisibility(w http.ResponseWriter, session *middleware.SessionInfo, visibility string) bool {
	if !database.ValidSavedQueryVisibility(visibility) {
		writeError(w, http.StatusBadRequest, "Visibility must be private, shared or admins")
		return false
	}
	if visibility == database.SavedQueryAdmins && session.UserRole != "admin" {
		writeError(w, http.StatusForbidden, "Only admins can limit a query to admins")
		return false
	}
	return true
}

// checkFolder validates a folder id ("" is the root). On failure it has
// written the error.
func (h *SavedQueriesHandler) checkFolder(w http.ResponseWriter, folderID string) bool {
	if folderID == "" {
		return true
	}
	f, err := h.DB.GetSavedQueryFolder(folderID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to load folder")
		return false
	}
	if f == nil {
		writeError(w, http.StatusBadRequest, "Folder not found")
		return false
	}
	return true
}

// checkSavedQueryTags applies the dashboard tag limits: 20 tags of 40
// characters. On failure it has written the error.
func checkSavedQueryTags(w http.ResponseWriter, tags []string) bool {
	if len(tags) > 20 {
		writeError(w, http.StatusBadRequest, "At most 20 tags")
		return false
	}
	for _, t := range tags {
		if len(strings.TrimSpace(t)) > 40 {
			writeError(w, http.StatusBadRequest, "Tags are limited to 40 characters")
			return false
		}
	}
	return true
}

// folderSubtree returns the ids of a folder and every folder below it.
func (h *SavedQueriesHandler) folderSubtree(id string) (map[string]bool, error) {
	folders, err := h.DB.ListSavedQueryFolders()
	if err != nil {
		return nil, err
	}
	out := map[string]bool{id: true}
	for changed := true; changed; {
		changed = false
		for _, f := range folders {
			if f.ParentID != nil && out[*f.ParentID] && !out[f.ID] {
				out[f.ID] = true
				changed = true
			}
		}
	}
	return out, nil
}

// recordVersion saves the state of a saved query after a change. A failure
// is logged, never returned: the change itself has been saved.
func (h *SavedQueriesHandler) recordVersion(id, actor, note string) {
	if _, err := h.DB.RecordSavedQueryVersion(id, actor, note); err != nil {
		slog.Warn("Failed to record saved query version", "error", err, "id", id)
	}
}

func (h *SavedQueriesHandler) audit(session *middleware.SessionInfo, action, details string) {
	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:         action,
		Username:       strPtr(middleware.Actor(session)),
		ClickhouseUser: strPtr(session.ClickhouseUser),
		Details:        strPtr(details),
	})
}

// ---------- Folders ----------

// ListFolders returns every saved query folder (flat; the client builds the tree).
func (h *SavedQueriesHandler) ListFolders(w http.ResponseWriter, r *http.Request) {
	folders, err := h.DB.ListSavedQueryFolders()
	if err != nil {
		slog.Error("Failed to list saved query folders", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to list folders")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"folders": folders})
}

// CreateFolder creates a folder, optionally under a parent.
func (h *SavedQueriesHandler) CreateFolder(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}
	var body struct {
		Name     string `json:"name"`
		ParentID string `json:"parent_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON body")
		return
	}
	name := strings.TrimSpace(body.Name)
	if name == "" {
		writeError(w, http.StatusBadRequest, "Name is required")
		return
	}
	parentID := strings.TrimSpace(body.ParentID)
	if parentID != "" {
		parent, err := h.DB.GetSavedQueryFolder(parentID)
		if err != nil || parent == nil {
			writeError(w, http.StatusBadRequest, "Parent folder not found")
			return
		}
	}
	id, err := h.DB.CreateSavedQueryFolder(name, parentID, middleware.Actor(session))
	if err == database.ErrFolderNameTaken {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		slog.Error("Failed to create saved query folder", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to create folder")
		return
	}
	h.audit(session, "saved_query.folder.created", name)
	folder, err := h.DB.GetSavedQueryFolder(id)
	if err != nil || folder == nil {
		writeJSON(w, http.StatusCreated, map[string]string{"id": id})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]interface{}{"folder": folder})
}

// UpdateFolder renames a folder and/or moves it under another parent.
func (h *SavedQueriesHandler) UpdateFolder(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}
	id := chi.URLParam(r, "folderId")
	var body struct {
		Name     *string `json:"name"`
		ParentID *string `json:"parent_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON body")
		return
	}
	if body.Name == nil && body.ParentID == nil {
		writeError(w, http.StatusBadRequest, "No fields to update")
		return
	}
	if body.Name != nil && strings.TrimSpace(*body.Name) == "" {
		writeError(w, http.StatusBadRequest, "Name cannot be empty")
		return
	}
	if body.ParentID != nil && strings.TrimSpace(*body.ParentID) != "" {
		parent, err := h.DB.GetSavedQueryFolder(strings.TrimSpace(*body.ParentID))
		if err != nil || parent == nil {
			writeError(w, http.StatusBadRequest, "Parent folder not found")
			return
		}
	}
	err := h.DB.UpdateSavedQueryFolder(id, body.Name, body.ParentID)
	switch {
	case err == sql.ErrNoRows:
		writeError(w, http.StatusNotFound, "Folder not found")
		return
	case err == database.ErrFolderNameTaken, err == database.ErrFolderCycle:
		writeError(w, http.StatusConflict, err.Error())
		return
	case err != nil:
		slog.Error("Failed to update saved query folder", "error", err, "id", id)
		writeError(w, http.StatusInternalServerError, "Failed to update folder")
		return
	}
	folder, _ := h.DB.GetSavedQueryFolder(id)
	details := id
	if folder != nil {
		details = folder.Name
	}
	h.audit(session, "saved_query.folder.updated", details)
	writeJSON(w, http.StatusOK, map[string]interface{}{"folder": folder})
}

// DeleteFolder removes a folder and its subfolders; the saved queries in
// them move to the root.
func (h *SavedQueriesHandler) DeleteFolder(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}
	id := chi.URLParam(r, "folderId")
	folder, err := h.DB.GetSavedQueryFolder(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to load folder")
		return
	}
	if folder == nil {
		writeError(w, http.StatusNotFound, "Folder not found")
		return
	}
	moved, err := h.DB.DeleteSavedQueryFolder(id)
	if err != nil {
		slog.Error("Failed to delete saved query folder", "error", err, "id", id)
		writeError(w, http.StatusInternalServerError, "Failed to delete folder")
		return
	}
	h.audit(session, "saved_query.folder.deleted", fmt.Sprintf("%s (%d queries moved to root)", folder.Name, moved))
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "moved": moved})
}

// ---------- Versions ----------

// ListVersions returns a saved query's versions, newest first, without
// their snapshots.
func (h *SavedQueriesHandler) ListVersions(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}
	q := h.loadSavedQuery(w, r, session, false)
	if q == nil {
		return
	}
	versions, err := h.DB.ListSavedQueryVersions(q.ID)
	if err != nil {
		slog.Error("Failed to list saved query versions", "error", err, "id", q.ID)
		writeError(w, http.StatusInternalServerError, "Failed to list versions")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"versions": versions})
}

func versionParam(w http.ResponseWriter, r *http.Request) (int, bool) {
	n, err := strconv.Atoi(chi.URLParam(r, "version"))
	if err != nil || n < 1 {
		writeError(w, http.StatusBadRequest, "Invalid version")
		return 0, false
	}
	return n, true
}

// GetVersion returns one version with its snapshot.
func (h *SavedQueriesHandler) GetVersion(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}
	q := h.loadSavedQuery(w, r, session, false)
	if q == nil {
		return
	}
	n, ok := versionParam(w, r)
	if !ok {
		return
	}
	v, err := h.DB.GetSavedQueryVersion(q.ID, n)
	if err != nil {
		slog.Error("Failed to get saved query version", "error", err, "id", q.ID)
		writeError(w, http.StatusInternalServerError, "Failed to load version")
		return
	}
	if v == nil {
		writeError(w, http.StatusNotFound, "Version not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"version": v})
}

// RestoreVersion writes a version back and records "Restored version N".
// That new version makes editors still holding the old one conflict on
// their next save instead of overwriting the restore.
func (h *SavedQueriesHandler) RestoreVersion(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}
	savedQueryWrites.Lock()
	defer savedQueryWrites.Unlock()
	q := h.loadSavedQuery(w, r, session, true)
	if q == nil {
		return
	}
	n, ok := versionParam(w, r)
	if !ok {
		return
	}
	newVersion, err := h.DB.RestoreSavedQueryVersion(q.ID, n, middleware.Actor(session))
	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "Version not found")
		return
	}
	if err != nil {
		slog.Error("Failed to restore saved query version", "error", err, "id", q.ID, "version", n)
		writeError(w, http.StatusInternalServerError, "Failed to restore version")
		return
	}
	h.audit(session, "saved_query.version_restored", fmt.Sprintf("%s: restored version %d", q.Name, n))
	updated, err := h.DB.GetSavedQueryByID(q.ID)
	if err != nil || updated == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "version": newVersion})
		return
	}
	updated.CanEdit = true
	writeJSON(w, http.StatusOK, map[string]interface{}{"saved_query": updated, "version": newVersion})
}
