package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/caioricciuti/ch-ui/internal/alerts"
	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/queryproc"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

// DashboardsHandler handles dashboard and panel CRUD operations.
type DashboardsHandler struct {
	DB      *database.DB
	Gateway *tunnel.Gateway
	Config  *config.Config
}

// Routes returns a chi.Router with all dashboard and panel routes mounted.
func (h *DashboardsHandler) Routes() chi.Router {
	r := chi.NewRouter()

	// Writes require admin/analyst; viewers are read-only. Viewing dashboards and
	// executing their panel queries stays open (governed by ClickHouse grants).
	writer := middleware.RequireWriter()

	r.Get("/", h.ListDashboards)
	r.With(writer).Post("/", h.CreateDashboard)
	r.Post("/query", h.ExecutePanelQuery)

	// Folders (nested) organize dashboards; stars are per user.
	r.Get("/folders", h.ListFolders)
	r.With(writer).Post("/folders", h.CreateFolder)
	r.With(writer).Put("/folders/{folderId}", h.UpdateFolder)
	r.With(writer).Delete("/folders/{folderId}", h.DeleteFolder)

	r.Route("/{id}", func(r chi.Router) {
		r.Get("/", h.GetDashboard)
		r.With(writer).Put("/", h.UpdateDashboard)
		r.With(writer).Delete("/", h.DeleteDashboard)
		r.With(writer).Put("/move", h.MoveDashboard)
		r.With(writer).Put("/tags", h.SetDashboardTags)
		r.Post("/star", h.StarDashboard)
		r.Delete("/star", h.UnstarDashboard)

		// Panel CRUD
		r.With(writer).Post("/panels", h.CreatePanel)
		r.With(writer).Put("/panels/{panelId}", h.UpdatePanel)
		r.With(writer).Delete("/panels/{panelId}", h.DeletePanel)

		// Sharing
		r.Get("/shares", h.ListShares)
		r.With(writer).Post("/shares", h.CreateShare)
		r.With(writer).Delete("/shares/{shareId}", h.DeleteShare)
		r.With(writer).Post("/shares/{shareId}/invite", h.InviteToShare)
	})

	return r
}

// PublicRoutes returns a chi.Router for unauthenticated public dashboard access.
func (h *DashboardsHandler) PublicRoutes() chi.Router {
	r := chi.NewRouter()
	r.Get("/{token}", h.GetPublicDashboard)
	r.Post("/{token}/query", h.ExecutePublicQuery)
	return r
}

// ---------- Dashboard CRUD ----------

// ListDashboards returns all dashboards.
func (h *DashboardsHandler) ListDashboards(w http.ResponseWriter, r *http.Request) {
	if err := h.DB.EnsureSystemOverviewDashboard(); err != nil {
		slog.Warn("Failed to ensure default system dashboard", "error", err)
	}

	forUser := ""
	if session := middleware.GetSession(r); session != nil {
		forUser = session.ClickhouseUser
	}
	dashboards, err := h.DB.GetDashboards(forUser)
	if err != nil {
		slog.Error("Failed to list dashboards", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to list dashboards")
		return
	}

	if dashboards == nil {
		dashboards = []database.Dashboard{}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"dashboards": dashboards})
}

// GetDashboard returns a single dashboard with all its panels.
func (h *DashboardsHandler) GetDashboard(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "Dashboard ID is required")
		return
	}

	forUser := ""
	if session := middleware.GetSession(r); session != nil {
		forUser = session.ClickhouseUser
	}
	dashboard, err := h.DB.GetDashboardByIDFor(id, forUser)
	if err != nil {
		slog.Error("Failed to get dashboard", "error", err, "id", id)
		writeError(w, http.StatusInternalServerError, "Failed to get dashboard")
		return
	}
	if dashboard == nil {
		writeError(w, http.StatusNotFound, "Dashboard not found")
		return
	}

	panels, err := h.DB.GetPanelsByDashboard(id)
	if err != nil {
		slog.Error("Failed to get panels", "error", err, "dashboard", id)
		panels = []database.Panel{}
	}
	if panels == nil {
		panels = []database.Panel{}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"dashboard": dashboard,
		"panels":    panels,
	})
}

// CreateDashboard creates a new dashboard.
func (h *DashboardsHandler) CreateDashboard(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}

	var body struct {
		Name        string   `json:"name"`
		Description string   `json:"description"`
		FolderID    string   `json:"folder_id"`
		Tags        []string `json:"tags"`
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
	folderID := strings.TrimSpace(body.FolderID)
	if folderID != "" {
		folder, err := h.DB.GetDashboardFolder(folderID)
		if err != nil || folder == nil {
			writeError(w, http.StatusBadRequest, "Folder not found")
			return
		}
	}

	id, err := h.DB.CreateDashboard(name, strings.TrimSpace(body.Description), session.ClickhouseUser)
	if err != nil {
		slog.Error("Failed to create dashboard", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to create dashboard")
		return
	}
	if folderID != "" {
		if err := h.DB.SetDashboardFolder(id, folderID); err != nil {
			slog.Warn("Failed to place new dashboard in folder", "error", err, "id", id)
		}
	}
	if len(body.Tags) > 0 {
		if err := h.DB.SetDashboardTags(id, body.Tags); err != nil {
			slog.Warn("Failed to tag new dashboard", "error", err, "id", id)
		}
	}

	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:   "dashboard.created",
		Username: strPtr(session.ClickhouseUser),
		Details:  strPtr(name),
	})

	dashboard, err := h.DB.GetDashboardByID(id)
	if err != nil || dashboard == nil {
		writeJSON(w, http.StatusCreated, map[string]string{"id": id})
		return
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{"dashboard": dashboard})
}

// UpdateDashboard partially updates a dashboard.
func (h *DashboardsHandler) UpdateDashboard(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}

	id := chi.URLParam(r, "id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "Dashboard ID is required")
		return
	}

	existing, err := h.DB.GetDashboardByID(id)
	if err != nil {
		slog.Error("Failed to get dashboard for update", "error", err, "id", id)
		writeError(w, http.StatusInternalServerError, "Failed to get dashboard")
		return
	}
	if existing == nil {
		writeError(w, http.StatusNotFound, "Dashboard not found")
		return
	}

	var body struct {
		Name        *string `json:"name"`
		Description *string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON body")
		return
	}

	name := existing.Name
	description := ""
	if existing.Description != nil {
		description = *existing.Description
	}

	changed := false
	if body.Name != nil {
		n := strings.TrimSpace(*body.Name)
		if n == "" {
			writeError(w, http.StatusBadRequest, "Name cannot be empty")
			return
		}
		name = n
		changed = true
	}
	if body.Description != nil {
		description = strings.TrimSpace(*body.Description)
		changed = true
	}

	if !changed {
		writeError(w, http.StatusBadRequest, "No fields to update")
		return
	}

	if err := h.DB.UpdateDashboard(id, name, description); err != nil {
		slog.Error("Failed to update dashboard", "error", err, "id", id)
		writeError(w, http.StatusInternalServerError, "Failed to update dashboard")
		return
	}

	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:   "dashboard.updated",
		Username: strPtr(session.ClickhouseUser),
		Details:  strPtr(name),
	})

	dashboard, err := h.DB.GetDashboardByID(id)
	if err != nil || dashboard == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"dashboard": dashboard})
}

// DeleteDashboard deletes a dashboard and all its panels.
func (h *DashboardsHandler) DeleteDashboard(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}

	id := chi.URLParam(r, "id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "Dashboard ID is required")
		return
	}

	existing, err := h.DB.GetDashboardByID(id)
	if err != nil {
		slog.Error("Failed to get dashboard for delete", "error", err, "id", id)
		writeError(w, http.StatusInternalServerError, "Failed to get dashboard")
		return
	}
	if existing == nil {
		writeError(w, http.StatusNotFound, "Dashboard not found")
		return
	}

	if err := h.DB.DeleteDashboard(id); err != nil {
		slog.Error("Failed to delete dashboard", "error", err, "id", id)
		writeError(w, http.StatusInternalServerError, "Failed to delete dashboard")
		return
	}

	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:   "dashboard.deleted",
		Username: strPtr(session.ClickhouseUser),
		Details:  strPtr(existing.Name),
	})

	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

// ---------- Folders, moving, tags, stars ----------

// ListFolders returns every dashboard folder (flat; the client builds the tree).
func (h *DashboardsHandler) ListFolders(w http.ResponseWriter, r *http.Request) {
	folders, err := h.DB.ListDashboardFolders()
	if err != nil {
		slog.Error("Failed to list dashboard folders", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to list folders")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"folders": folders})
}

// CreateFolder creates a folder, optionally under a parent.
func (h *DashboardsHandler) CreateFolder(w http.ResponseWriter, r *http.Request) {
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
		parent, err := h.DB.GetDashboardFolder(parentID)
		if err != nil || parent == nil {
			writeError(w, http.StatusBadRequest, "Parent folder not found")
			return
		}
	}
	id, err := h.DB.CreateDashboardFolder(name, parentID, session.ClickhouseUser)
	if err == database.ErrFolderNameTaken {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		slog.Error("Failed to create dashboard folder", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to create folder")
		return
	}
	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:   "dashboard.folder.created",
		Username: strPtr(session.ClickhouseUser),
		Details:  strPtr(name),
	})
	folder, err := h.DB.GetDashboardFolder(id)
	if err != nil || folder == nil {
		writeJSON(w, http.StatusCreated, map[string]string{"id": id})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]interface{}{"folder": folder})
}

// UpdateFolder renames a folder and/or moves it under another parent.
func (h *DashboardsHandler) UpdateFolder(w http.ResponseWriter, r *http.Request) {
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
		parent, err := h.DB.GetDashboardFolder(strings.TrimSpace(*body.ParentID))
		if err != nil || parent == nil {
			writeError(w, http.StatusBadRequest, "Parent folder not found")
			return
		}
	}
	err := h.DB.UpdateDashboardFolder(id, body.Name, body.ParentID)
	switch {
	case err == sql.ErrNoRows:
		writeError(w, http.StatusNotFound, "Folder not found")
		return
	case err == database.ErrFolderNameTaken, err == database.ErrFolderCycle:
		writeError(w, http.StatusConflict, err.Error())
		return
	case err != nil:
		slog.Error("Failed to update dashboard folder", "error", err, "id", id)
		writeError(w, http.StatusInternalServerError, "Failed to update folder")
		return
	}
	folder, _ := h.DB.GetDashboardFolder(id)
	details := id
	if folder != nil {
		details = folder.Name
	}
	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:   "dashboard.folder.updated",
		Username: strPtr(session.ClickhouseUser),
		Details:  strPtr(details),
	})
	writeJSON(w, http.StatusOK, map[string]interface{}{"folder": folder})
}

// DeleteFolder removes a folder; its contents move up one level.
func (h *DashboardsHandler) DeleteFolder(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}
	id := chi.URLParam(r, "folderId")
	folder, err := h.DB.GetDashboardFolder(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to load folder")
		return
	}
	if folder == nil {
		writeError(w, http.StatusNotFound, "Folder not found")
		return
	}
	if err := h.DB.DeleteDashboardFolder(id); err != nil {
		slog.Error("Failed to delete dashboard folder", "error", err, "id", id)
		writeError(w, http.StatusInternalServerError, "Failed to delete folder")
		return
	}
	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:   "dashboard.folder.deleted",
		Username: strPtr(session.ClickhouseUser),
		Details:  strPtr(folder.Name),
	})
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

// MoveDashboard places a dashboard in a folder (empty folder_id: root).
func (h *DashboardsHandler) MoveDashboard(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}
	id := chi.URLParam(r, "id")
	existing, err := h.DB.GetDashboardByID(id)
	if err != nil || existing == nil {
		writeError(w, http.StatusNotFound, "Dashboard not found")
		return
	}
	var body struct {
		FolderID string `json:"folder_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON body")
		return
	}
	folderID := strings.TrimSpace(body.FolderID)
	if folderID != "" {
		folder, err := h.DB.GetDashboardFolder(folderID)
		if err != nil || folder == nil {
			writeError(w, http.StatusBadRequest, "Folder not found")
			return
		}
	}
	if err := h.DB.SetDashboardFolder(id, folderID); err != nil {
		slog.Error("Failed to move dashboard", "error", err, "id", id)
		writeError(w, http.StatusInternalServerError, "Failed to move dashboard")
		return
	}
	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:   "dashboard.moved",
		Username: strPtr(session.ClickhouseUser),
		Details:  strPtr(existing.Name),
	})
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

// SetDashboardTags replaces a dashboard's tags.
func (h *DashboardsHandler) SetDashboardTags(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}
	id := chi.URLParam(r, "id")
	existing, err := h.DB.GetDashboardByID(id)
	if err != nil || existing == nil {
		writeError(w, http.StatusNotFound, "Dashboard not found")
		return
	}
	var body struct {
		Tags []string `json:"tags"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON body")
		return
	}
	if len(body.Tags) > 20 {
		writeError(w, http.StatusBadRequest, "At most 20 tags")
		return
	}
	for _, t := range body.Tags {
		if len(strings.TrimSpace(t)) > 40 {
			writeError(w, http.StatusBadRequest, "Tags are limited to 40 characters")
			return
		}
	}
	if err := h.DB.SetDashboardTags(id, body.Tags); err != nil {
		slog.Error("Failed to set dashboard tags", "error", err, "id", id)
		writeError(w, http.StatusInternalServerError, "Failed to set tags")
		return
	}
	updated, _ := h.DB.GetDashboardByIDFor(id, session.ClickhouseUser)
	writeJSON(w, http.StatusOK, map[string]interface{}{"dashboard": updated})
}

// StarDashboard and UnstarDashboard toggle the session user's star. Viewers
// may star: it is personal state, not a change to the dashboard.
func (h *DashboardsHandler) StarDashboard(w http.ResponseWriter, r *http.Request) {
	h.setStar(w, r, true)
}

func (h *DashboardsHandler) UnstarDashboard(w http.ResponseWriter, r *http.Request) {
	h.setStar(w, r, false)
}

func (h *DashboardsHandler) setStar(w http.ResponseWriter, r *http.Request, on bool) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}
	id := chi.URLParam(r, "id")
	existing, err := h.DB.GetDashboardByID(id)
	if err != nil || existing == nil {
		writeError(w, http.StatusNotFound, "Dashboard not found")
		return
	}
	if on {
		err = h.DB.StarDashboard(id, session.ClickhouseUser)
	} else {
		err = h.DB.UnstarDashboard(id, session.ClickhouseUser)
	}
	if err != nil {
		slog.Error("Failed to update dashboard star", "error", err, "id", id)
		writeError(w, http.StatusInternalServerError, "Failed to update star")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "starred": on})
}

// ---------- Panel CRUD ----------

// CreatePanel creates a new panel in a dashboard.
func (h *DashboardsHandler) CreatePanel(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}

	dashboardID := chi.URLParam(r, "id")
	if dashboardID == "" {
		writeError(w, http.StatusBadRequest, "Dashboard ID is required")
		return
	}

	dashboard, err := h.DB.GetDashboardByID(dashboardID)
	if err != nil || dashboard == nil {
		writeError(w, http.StatusNotFound, "Dashboard not found")
		return
	}

	var body struct {
		Name         string `json:"name"`
		Description  string `json:"description"`
		PanelType    string `json:"panel_type"`
		Query        string `json:"query"`
		ConnectionID string `json:"connection_id"`
		Config       string `json:"config"`
		LayoutX      *int   `json:"layout_x"`
		LayoutY      *int   `json:"layout_y"`
		LayoutW      *int   `json:"layout_w"`
		LayoutH      *int   `json:"layout_h"`
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

	description := strings.TrimSpace(body.Description)

	query := strings.TrimSpace(body.Query)
	if query == "" {
		writeError(w, http.StatusBadRequest, "Query is required")
		return
	}

	panelType := strings.TrimSpace(body.PanelType)
	if panelType == "" {
		panelType = "table"
	}

	connectionID := strings.TrimSpace(body.ConnectionID)
	panelConfig := strings.TrimSpace(body.Config)

	x, y, w2, h2 := 0, 0, 6, 4
	if body.LayoutX != nil {
		x = *body.LayoutX
	}
	if body.LayoutY != nil {
		y = *body.LayoutY
	}
	if body.LayoutW != nil {
		w2 = *body.LayoutW
	}
	if body.LayoutH != nil {
		h2 = *body.LayoutH
	}

	id, err := h.DB.CreatePanel(dashboardID, name, description, panelType, query, connectionID, panelConfig, x, y, w2, h2)
	if err != nil {
		slog.Error("Failed to create panel", "error", err, "dashboard", dashboardID)
		writeError(w, http.StatusInternalServerError, "Failed to create panel")
		return
	}

	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:   "panel.created",
		Username: strPtr(session.ClickhouseUser),
		Details:  strPtr(name),
	})

	panel, err := h.DB.GetPanelByID(id)
	if err != nil || panel == nil {
		writeJSON(w, http.StatusCreated, map[string]string{"id": id})
		return
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{"panel": panel})
}

// UpdatePanel partially updates a panel.
func (h *DashboardsHandler) UpdatePanel(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}

	dashboardID := chi.URLParam(r, "id")
	panelID := chi.URLParam(r, "panelId")
	if dashboardID == "" || panelID == "" {
		writeError(w, http.StatusBadRequest, "Dashboard ID and panel ID are required")
		return
	}

	existing, err := h.DB.GetPanelByID(panelID)
	if err != nil {
		slog.Error("Failed to get panel for update", "error", err, "panel", panelID)
		writeError(w, http.StatusInternalServerError, "Failed to get panel")
		return
	}
	if existing == nil || existing.DashboardID != dashboardID {
		writeError(w, http.StatusNotFound, "Panel not found")
		return
	}

	var body struct {
		Name         *string `json:"name"`
		Description  *string `json:"description"`
		PanelType    *string `json:"panel_type"`
		Query        *string `json:"query"`
		ConnectionID *string `json:"connection_id"`
		Config       *string `json:"config"`
		LayoutX      *int    `json:"layout_x"`
		LayoutY      *int    `json:"layout_y"`
		LayoutW      *int    `json:"layout_w"`
		LayoutH      *int    `json:"layout_h"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON body")
		return
	}

	name := existing.Name
	description := existing.Description
	panelType := existing.PanelType
	query := existing.Query
	connectionID := ""
	if existing.ConnectionID != nil {
		connectionID = *existing.ConnectionID
	}
	panelConfig := existing.Config
	x, y, pw, ph := existing.LayoutX, existing.LayoutY, existing.LayoutW, existing.LayoutH

	changed := false
	if body.Name != nil {
		name = strings.TrimSpace(*body.Name)
		changed = true
	}
	if body.Description != nil {
		description = strings.TrimSpace(*body.Description)
		changed = true
	}
	if body.PanelType != nil {
		panelType = strings.TrimSpace(*body.PanelType)
		changed = true
	}
	if body.Query != nil {
		query = strings.TrimSpace(*body.Query)
		changed = true
	}
	if body.ConnectionID != nil {
		connectionID = strings.TrimSpace(*body.ConnectionID)
		changed = true
	}
	if body.Config != nil {
		panelConfig = *body.Config
		changed = true
	}
	if body.LayoutX != nil {
		x = *body.LayoutX
		changed = true
	}
	if body.LayoutY != nil {
		y = *body.LayoutY
		changed = true
	}
	if body.LayoutW != nil {
		pw = *body.LayoutW
		changed = true
	}
	if body.LayoutH != nil {
		ph = *body.LayoutH
		changed = true
	}

	if !changed {
		writeError(w, http.StatusBadRequest, "No fields to update")
		return
	}

	if err := h.DB.UpdatePanel(panelID, name, description, panelType, query, connectionID, panelConfig, x, y, pw, ph); err != nil {
		slog.Error("Failed to update panel", "error", err, "panel", panelID)
		writeError(w, http.StatusInternalServerError, "Failed to update panel")
		return
	}

	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:   "panel.updated",
		Username: strPtr(session.ClickhouseUser),
		Details:  strPtr(name),
	})

	panel, err := h.DB.GetPanelByID(panelID)
	if err != nil || panel == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"panel": panel})
}

// DeletePanel deletes a panel from a dashboard.
func (h *DashboardsHandler) DeletePanel(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}

	dashboardID := chi.URLParam(r, "id")
	panelID := chi.URLParam(r, "panelId")
	if dashboardID == "" || panelID == "" {
		writeError(w, http.StatusBadRequest, "Dashboard ID and panel ID are required")
		return
	}

	existing, err := h.DB.GetPanelByID(panelID)
	if err != nil {
		slog.Error("Failed to get panel for delete", "error", err, "panel", panelID)
		writeError(w, http.StatusInternalServerError, "Failed to get panel")
		return
	}
	if existing == nil || existing.DashboardID != dashboardID {
		writeError(w, http.StatusNotFound, "Panel not found")
		return
	}

	if err := h.DB.DeletePanel(panelID); err != nil {
		slog.Error("Failed to delete panel", "error", err, "panel", panelID)
		writeError(w, http.StatusInternalServerError, "Failed to delete panel")
		return
	}

	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:   "panel.deleted",
		Username: strPtr(session.ClickhouseUser),
		Details:  strPtr(existing.Name),
	})

	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

// ---------- Panel Query Execution ----------

// ExecutePanelQuery executes a SQL query through the tunnel for a panel.
func (h *DashboardsHandler) ExecutePanelQuery(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}

	var body struct {
		Query         string               `json:"query"`
		Timeout       *int                 `json:"timeout"`
		TimeRange     *queryproc.TimeRange `json:"time_range"`
		TimeField     string               `json:"time_field"`
		TimeFieldUnit string               `json:"time_field_unit"`
		MaxDataPoints *int                 `json:"max_data_points"`
		Table         string               `json:"table"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON body")
		return
	}

	query := strings.TrimSpace(body.Query)
	if query == "" {
		writeError(w, http.StatusBadRequest, "Query is required")
		return
	}

	maxDataPoints := 1000
	if body.MaxDataPoints != nil && *body.MaxDataPoints > 0 {
		maxDataPoints = *body.MaxDataPoints
	}

	processed := queryproc.ProcessQueryVariables(queryproc.ProcessorOptions{
		Query:         query,
		TimeRange:     body.TimeRange,
		TimeField:     strings.TrimSpace(body.TimeField),
		TimeFieldUnit: strings.TrimSpace(body.TimeFieldUnit),
		MaxDataPoints: maxDataPoints,
		Table:         strings.TrimSpace(body.Table),
	})

	if len(processed.Errors) > 0 {
		writeError(w, http.StatusBadRequest, strings.Join(processed.Errors, "; "))
		return
	}

	query = strings.TrimSpace(processed.Query)
	if query == "" {
		writeError(w, http.StatusBadRequest, "Processed query is empty")
		return
	}

	password, err := crypto.Decrypt(session.EncryptedPassword, h.Config.AppSecretKey)
	if err != nil {
		slog.Error("Failed to decrypt password", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to decrypt credentials")
		return
	}

	timeout := 30 * time.Second
	if body.Timeout != nil && *body.Timeout > 0 {
		timeout = time.Duration(*body.Timeout) * time.Second
		if timeout > 5*time.Minute {
			timeout = 5 * time.Minute
		}
	}

	start := time.Now()
	result, err := h.Gateway.ExecuteQuery(session.ConnectionID, query, session.ClickhouseUser, password, timeout)
	elapsed := time.Since(start)

	if err != nil {
		slog.Warn("Panel query failed", "error", err, "user", session.ClickhouseUser)
		writeJSON(w, http.StatusBadGateway, map[string]interface{}{
			"success":    false,
			"error":      err.Error(),
			"elapsed_ms": elapsed.Milliseconds(),
		})
		return
	}

	rows := countRows(result.Data)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":    true,
		"data":       result.Data,
		"meta":       result.Meta,
		"statistics": result.Stats,
		"rows":       rows,
		"elapsed_ms": elapsed.Milliseconds(),
		"query":      query,
		"variables":  processed.InterpolatedVars,
	})
}

// ---------- Sharing ----------

// ListShares returns all share links for a dashboard.
func (h *DashboardsHandler) ListShares(w http.ResponseWriter, r *http.Request) {
	dashboardID := chi.URLParam(r, "id")
	shares, err := h.DB.GetDashboardSharesByDashboard(dashboardID)
	if err != nil {
		slog.Error("Failed to list shares", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to list shares")
		return
	}
	if shares == nil {
		shares = []database.DashboardShare{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"shares": shares})
}

// CreateShare creates a share link for a dashboard.
func (h *DashboardsHandler) CreateShare(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}

	dashboardID := chi.URLParam(r, "id")
	dashboard, err := h.DB.GetDashboardByID(dashboardID)
	if err != nil || dashboard == nil {
		writeError(w, http.StatusNotFound, "Dashboard not found")
		return
	}

	var body struct {
		Visibility    string   `json:"visibility"`
		AllowedEmails []string `json:"allowed_emails"`
		ExpiresAt     *string  `json:"expires_at"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		body.Visibility = "public"
	}

	visibility := strings.TrimSpace(body.Visibility)
	if visibility != "private" {
		visibility = "public"
	}

	var cleanEmails []string
	for _, e := range body.AllowedEmails {
		e = strings.TrimSpace(strings.ToLower(e))
		if e != "" && strings.Contains(e, "@") {
			cleanEmails = append(cleanEmails, e)
		}
	}

	if visibility == "private" && len(cleanEmails) == 0 {
		writeError(w, http.StatusBadRequest, "Private shares require at least one email")
		return
	}

	share, err := h.DB.CreateDashboardShare(
		dashboardID, "viewer", visibility, session.ClickhouseUser,
		session.ConnectionID, session.ClickhouseUser, session.EncryptedPassword,
		cleanEmails, body.ExpiresAt,
	)
	if err != nil {
		slog.Error("Failed to create share", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to create share")
		return
	}

	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:   "dashboard.shared",
		Username: strPtr(session.ClickhouseUser),
		Details:  strPtr(dashboard.Name),
	})

	writeJSON(w, http.StatusCreated, map[string]interface{}{"share": share})
}

// DeleteShare revokes a share link.
func (h *DashboardsHandler) DeleteShare(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}

	shareID := chi.URLParam(r, "shareId")
	if err := h.DB.DeleteDashboardShare(shareID); err != nil {
		slog.Error("Failed to delete share", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to delete share")
		return
	}

	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:   "dashboard.share_revoked",
		Username: strPtr(session.ClickhouseUser),
		Details:  strPtr(shareID),
	})

	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

// InviteToShare sends magic-link emails to the allowed recipients of a private share.
func (h *DashboardsHandler) InviteToShare(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}

	shareID := chi.URLParam(r, "shareId")
	share, err := h.DB.GetDashboardShareByID(shareID)
	if err != nil || share == nil {
		writeError(w, http.StatusNotFound, "Share not found")
		return
	}

	if share.Visibility != "private" {
		writeError(w, http.StatusBadRequest, "Invites are only for private shares")
		return
	}

	if len(share.AllowedEmails) == 0 {
		writeError(w, http.StatusBadRequest, "No emails configured on this share")
		return
	}

	channel, err := h.DB.GetFirstActiveAlertChannel()
	if err != nil || channel == nil {
		writeError(w, http.StatusBadRequest, "No email channel configured. Set up an SMTP, Resend, or Brevo channel in Admin > Alerts first.")
		return
	}

	decrypted, err := crypto.Decrypt(channel.ConfigEncrypted, h.Config.AppSecretKey)
	if err != nil {
		slog.Error("Failed to decrypt alert channel config", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to read email channel config")
		return
	}
	var channelConfig map[string]interface{}
	if err := json.Unmarshal([]byte(decrypted), &channelConfig); err != nil {
		slog.Error("Failed to parse alert channel config", "error", err)
		writeError(w, http.StatusInternalServerError, "Invalid email channel config")
		return
	}

	dashboard, _ := h.DB.GetDashboardByID(share.DashboardID)
	dashboardName := "Dashboard"
	if dashboard != nil {
		dashboardName = dashboard.Name
	}

	appURL := strings.TrimRight(h.Config.AppURL, "/")
	if origin := r.Header.Get("Origin"); origin != "" {
		appURL = strings.TrimRight(origin, "/")
	} else if appURL == "" {
		appURL = fmt.Sprintf("http://localhost:%d", h.Config.Port)
	}

	invitesTTL := 30 * 24 * time.Hour // 30 days
	var sent []string
	var failed []string

	for _, email := range share.AllowedEmails {
		invite, err := h.DB.CreateShareInvite(share.ID, email, invitesTTL)
		if err != nil {
			slog.Error("Failed to create invite", "error", err, "email", email)
			failed = append(failed, email)
			continue
		}

		magicLink := fmt.Sprintf("%s/public/d/%s?key=%s", appURL, share.Token, invite.Token)
		subject := fmt.Sprintf("You've been invited to view: %s", dashboardName)
		body := fmt.Sprintf(
			"Hi,\n\nYou've been invited to view the dashboard \"%s\" on CH-UI.\n\nClick the link below to access it:\n%s\n\nThis link expires in 30 days.\n\n— CH-UI",
			dashboardName, magicLink,
		)

		if _, err := alerts.SendDirect(r.Context(), channel.ChannelType, channelConfig, []string{email}, subject, body); err != nil {
			slog.Error("Failed to send invite email", "error", err, "email", email)
			failed = append(failed, email)
		} else {
			sent = append(sent, email)
		}
	}

	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:   "dashboard.invites_sent",
		Username: strPtr(session.ClickhouseUser),
		Details:  strPtr(fmt.Sprintf("%d sent, %d failed", len(sent), len(failed))),
	})

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"sent":   sent,
		"failed": failed,
	})
}

// ---------- Public (unauthenticated) ----------

// validateShareAccess checks if the request is authorized to view a share.
// For public shares, always returns true. For private shares, validates the invite key.
func (h *DashboardsHandler) validateShareAccess(share *database.DashboardShare, r *http.Request) bool {
	if share.Visibility == "public" {
		return true
	}

	key := r.URL.Query().Get("key")
	if key == "" {
		return false
	}

	invite, err := h.DB.GetShareInviteByToken(key)
	if err != nil || invite == nil {
		return false
	}

	if invite.ShareID != share.ID {
		return false
	}

	exp, err := time.Parse(time.RFC3339, invite.ExpiresAt)
	if err == nil && time.Now().UTC().After(exp) {
		return false
	}

	return true
}

// GetPublicDashboard returns a dashboard and its panels via a share token.
func (h *DashboardsHandler) GetPublicDashboard(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	share, err := h.DB.GetDashboardShareByToken(token)
	if err != nil {
		slog.Error("Failed to look up share token", "error", err)
		writeError(w, http.StatusInternalServerError, "Internal error")
		return
	}
	if share == nil {
		writeError(w, http.StatusNotFound, "Dashboard not found or share link expired")
		return
	}

	if share.ExpiresAt != nil {
		exp, parseErr := time.Parse(time.RFC3339, *share.ExpiresAt)
		if parseErr == nil && time.Now().UTC().After(exp) {
			writeError(w, http.StatusGone, "Share link has expired")
			return
		}
	}

	if !h.validateShareAccess(share, r) {
		writeJSON(w, http.StatusForbidden, map[string]interface{}{
			"success":    false,
			"error":      "This dashboard requires an invite. Check your email for a magic link.",
			"visibility": "private",
		})
		return
	}

	dashboard, err := h.DB.GetDashboardByID(share.DashboardID)
	if err != nil || dashboard == nil {
		writeError(w, http.StatusNotFound, "Dashboard not found")
		return
	}

	panels, err := h.DB.GetPanelsByDashboard(share.DashboardID)
	if err != nil {
		panels = []database.Panel{}
	}
	if panels == nil {
		panels = []database.Panel{}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"dashboard": dashboard,
		"panels":    panels,
	})
}

// ExecutePublicQuery executes a panel query via a share token.
func (h *DashboardsHandler) ExecutePublicQuery(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	share, err := h.DB.GetDashboardShareByToken(token)
	if err != nil || share == nil {
		writeError(w, http.StatusNotFound, "Share not found")
		return
	}

	if share.ExpiresAt != nil {
		exp, parseErr := time.Parse(time.RFC3339, *share.ExpiresAt)
		if parseErr == nil && time.Now().UTC().After(exp) {
			writeError(w, http.StatusGone, "Share link has expired")
			return
		}
	}

	if !h.validateShareAccess(share, r) {
		writeError(w, http.StatusForbidden, "Access denied")
		return
	}

	var body struct {
		Query     string               `json:"query"`
		TimeRange *queryproc.TimeRange `json:"time_range"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON body")
		return
	}

	query := strings.TrimSpace(body.Query)
	if query == "" {
		writeError(w, http.StatusBadRequest, "Query is required")
		return
	}

	processed := queryproc.ProcessQueryVariables(queryproc.ProcessorOptions{
		Query:         query,
		TimeRange:     body.TimeRange,
		MaxDataPoints: 1000,
	})
	if len(processed.Errors) > 0 {
		writeError(w, http.StatusBadRequest, strings.Join(processed.Errors, "; "))
		return
	}

	query = strings.TrimSpace(processed.Query)
	if query == "" {
		writeError(w, http.StatusBadRequest, "Processed query is empty")
		return
	}

	password, err := crypto.Decrypt(share.EncryptedPassword, h.Config.AppSecretKey)
	if err != nil {
		slog.Error("Failed to decrypt share credentials", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to process query")
		return
	}

	result, qErr := h.Gateway.ExecuteQuery(share.ConnectionID, query, share.ClickhouseUser, password, 30*time.Second)
	if qErr != nil {
		writeError(w, http.StatusBadGateway, qErr.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"data":    result.Data,
		"meta":    result.Meta,
	})
}
