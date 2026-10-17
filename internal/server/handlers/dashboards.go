package handlers

import (
	"container/list"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/dashboards"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/governance"
	"github.com/caioricciuti/ch-ui/internal/mail"
	"github.com/caioricciuti/ch-ui/internal/queryproc"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

// DashboardsHandler handles dashboard and panel CRUD operations.
type DashboardsHandler struct {
	DB      *database.DB
	Gateway *tunnel.Gateway
	Config  *config.Config
	// Guardrails, when set, applies the governance query policies to panel
	// queries exactly as the SQL editor does.
	Guardrails *governance.GuardrailService

	publicOnce  sync.Once
	publicCache *publicQueryCache
	publicLimit *keyedRateLimiter

	// optionsCache holds variable options for share links.
	optionsOnce  sync.Once
	optionsCache *publicQueryCache

	// tzCache holds the session time zone of each connection and user, for
	// the options of DateTime variables.
	tzMu    sync.Mutex
	tzCache map[string]cachedTimezone
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

	// Built-in templates.
	r.Get("/templates", h.ListTemplates)
	r.With(writer).Post("/from-template", h.CreateFromTemplate)

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
		r.With(writer).Post("/import", h.ImportDashboard)
		r.With(writer).Post("/duplicate", h.DuplicateDashboard)

		// Variables
		r.With(writer).Put("/variables", h.SetVariables)
		r.Post("/variables/{name}/options", h.VariableOptions)

		// Version history
		r.Get("/versions", h.ListVersions)
		r.Get("/versions/{version}", h.GetVersion)
		r.With(writer).Post("/versions/{version}/restore", h.RestoreVersion)

		// Sharing
		r.With(writer).Get("/shares", h.ListShares)
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
	r.Post("/{token}/variables/{name}/options", h.PublicVariableOptions)
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
		forUser = middleware.Actor(session)
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
		forUser = middleware.Actor(session)
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
		"variables": h.loadVariables(id),
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

	id, err := h.DB.CreateDashboard(name, strings.TrimSpace(body.Description), middleware.Actor(session))
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

	h.recordVersion(r, id, "Created")

	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:         "dashboard.created",
		Username:       strPtr(middleware.Actor(session)),
		ClickhouseUser: strPtr(session.ClickhouseUser),
		Details:        strPtr(name),
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

	h.ensureBaselineVersion(id)
	if err := h.DB.UpdateDashboard(id, name, description); err != nil {
		slog.Error("Failed to update dashboard", "error", err, "id", id)
		writeError(w, http.StatusInternalServerError, "Failed to update dashboard")
		return
	}
	h.recordVersion(r, id, "")

	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:         "dashboard.updated",
		Username:       strPtr(middleware.Actor(session)),
		ClickhouseUser: strPtr(session.ClickhouseUser),
		Details:        strPtr(name),
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
		Action:         "dashboard.deleted",
		Username:       strPtr(middleware.Actor(session)),
		ClickhouseUser: strPtr(session.ClickhouseUser),
		Details:        strPtr(existing.Name),
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
	id, err := h.DB.CreateDashboardFolder(name, parentID, middleware.Actor(session))
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
		Action:         "dashboard.folder.created",
		Username:       strPtr(middleware.Actor(session)),
		ClickhouseUser: strPtr(session.ClickhouseUser),
		Details:        strPtr(name),
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
		Action:         "dashboard.folder.updated",
		Username:       strPtr(middleware.Actor(session)),
		ClickhouseUser: strPtr(session.ClickhouseUser),
		Details:        strPtr(details),
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
		Action:         "dashboard.folder.deleted",
		Username:       strPtr(middleware.Actor(session)),
		ClickhouseUser: strPtr(session.ClickhouseUser),
		Details:        strPtr(folder.Name),
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
		Action:         "dashboard.moved",
		Username:       strPtr(middleware.Actor(session)),
		ClickhouseUser: strPtr(session.ClickhouseUser),
		Details:        strPtr(existing.Name),
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
	updated, _ := h.DB.GetDashboardByIDFor(id, middleware.Actor(session))
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
		err = h.DB.StarDashboard(id, middleware.Actor(session))
	} else {
		err = h.DB.UnstarDashboard(id, middleware.Actor(session))
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
	if !validPanelType(panelType) {
		writeError(w, http.StatusBadRequest, invalidPanelTypeMessage)
		return
	}

	connectionID := strings.TrimSpace(body.ConnectionID)
	panelConfig := strings.TrimSpace(body.Config)
	if panelConfig != "" && !json.Valid([]byte(panelConfig)) {
		writeError(w, http.StatusBadRequest, "config must be valid JSON")
		return
	}
	if err := dashboards.ValidatePanelLinks(panelConfig); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	x, y, w2, h2 := 0, 0, 6, 4
	if body.LayoutX != nil {
		x = *body.LayoutX
	}
	if body.LayoutY != nil {
		y = *body.LayoutY
	} else {
		// No row given: append below every existing panel instead of
		// stacking on top of the first row.
		existing, err := h.DB.GetPanelsByDashboard(dashboardID)
		if err != nil {
			slog.Error("Failed to list panels for placement", "error", err, "dashboard", dashboardID)
			writeError(w, http.StatusInternalServerError, "Failed to create panel")
			return
		}
		y = dashboards.NextPanelRow(existing)
	}
	if body.LayoutW != nil {
		w2 = *body.LayoutW
	}
	if body.LayoutH != nil {
		h2 = *body.LayoutH
	}
	if msg := dashboards.ValidateLayout(x, y, w2, h2); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}

	h.ensureBaselineVersion(dashboardID)
	id, err := h.DB.CreatePanel(dashboardID, name, description, panelType, query, connectionID, panelConfig, x, y, w2, h2)
	if err != nil {
		slog.Error("Failed to create panel", "error", err, "dashboard", dashboardID)
		writeError(w, http.StatusInternalServerError, "Failed to create panel")
		return
	}
	h.recordVersion(r, dashboardID, "")

	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:         "panel.created",
		Username:       strPtr(middleware.Actor(session)),
		ClickhouseUser: strPtr(session.ClickhouseUser),
		Details:        strPtr(name),
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
	if body.Name != nil && name == "" {
		writeError(w, http.StatusBadRequest, "Name cannot be empty")
		return
	}
	if body.PanelType != nil && !validPanelType(panelType) {
		writeError(w, http.StatusBadRequest, invalidPanelTypeMessage)
		return
	}
	if body.Query != nil && query == "" && panelType != "text" {
		writeError(w, http.StatusBadRequest, "Query cannot be empty")
		return
	}
	if body.Config != nil && strings.TrimSpace(panelConfig) != "" && !json.Valid([]byte(panelConfig)) {
		writeError(w, http.StatusBadRequest, "config must be valid JSON")
		return
	}
	if body.Config != nil {
		if err := dashboards.ValidatePanelLinks(panelConfig); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	// Only a request that moves or resizes the panel is held to the layout
	// bounds, so a panel saved before validation existed can still be renamed.
	if body.LayoutX != nil || body.LayoutY != nil || body.LayoutW != nil || body.LayoutH != nil {
		if msg := dashboards.ValidateLayout(x, y, pw, ph); msg != "" {
			writeError(w, http.StatusBadRequest, msg)
			return
		}
	}

	h.ensureBaselineVersion(dashboardID)
	if err := h.DB.UpdatePanel(panelID, name, description, panelType, query, connectionID, panelConfig, x, y, pw, ph); err != nil {
		slog.Error("Failed to update panel", "error", err, "panel", panelID)
		writeError(w, http.StatusInternalServerError, "Failed to update panel")
		return
	}
	h.recordVersion(r, dashboardID, "")

	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:         "panel.updated",
		Username:       strPtr(middleware.Actor(session)),
		ClickhouseUser: strPtr(session.ClickhouseUser),
		Details:        strPtr(name),
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

	h.ensureBaselineVersion(dashboardID)
	if err := h.DB.DeletePanel(panelID); err != nil {
		slog.Error("Failed to delete panel", "error", err, "panel", panelID)
		writeError(w, http.StatusInternalServerError, "Failed to delete panel")
		return
	}
	h.recordVersion(r, dashboardID, "")

	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:         "panel.deleted",
		Username:       strPtr(middleware.Actor(session)),
		ClickhouseUser: strPtr(session.ClickhouseUser),
		Details:        strPtr(existing.Name),
	})

	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

var panelTypes = map[string]bool{
	"table": true, "stat": true, "gauge": true, "timeseries": true,
	"bar": true, "pie": true, "text": true,
	"histogram": true, "scatter": true, "heatmap": true,
}

const invalidPanelTypeMessage = "panel_type must be one of table, stat, gauge, timeseries, bar, pie, text, histogram, scatter, heatmap"

func validPanelType(t string) bool { return panelTypes[t] }

// maxImportPanels bounds one import, so a pasted file cannot create an
// unbounded number of rows.
const maxImportPanels = 200

// importPanel is one panel in an exported dashboard file. Config may be a JSON
// object (what the exporter writes) or a JSON string.
type importPanel struct {
	Name         string          `json:"name"`
	Description  string          `json:"description"`
	PanelType    string          `json:"panel_type"`
	Query        string          `json:"query"`
	ConnectionID string          `json:"connection_id"`
	Config       json.RawMessage `json:"config"`
	LayoutX      *int            `json:"layout_x"`
	LayoutY      *int            `json:"layout_y"`
	LayoutW      *int            `json:"layout_w"`
	LayoutH      *int            `json:"layout_h"`
}

type validImportPanel struct {
	name, description, panelType, query, connectionID, config string
	x, y, w, h                                                int
}

// validate checks one imported panel and fills in defaults. The error text
// names the panel by position so the user can find it in the file.
func (p importPanel) validate(index int) (validImportPanel, error) {
	where := fmt.Sprintf("panel %d", index+1)
	out := validImportPanel{
		name:         strings.TrimSpace(p.Name),
		description:  strings.TrimSpace(p.Description),
		panelType:    strings.TrimSpace(p.PanelType),
		query:        strings.TrimSpace(p.Query),
		connectionID: strings.TrimSpace(p.ConnectionID),
		x:            0, y: 0, w: 6, h: 4,
	}
	if out.name == "" {
		return out, fmt.Errorf("%s: name is required", where)
	}
	where = fmt.Sprintf("panel %d (%s)", index+1, out.name)
	if out.panelType == "" {
		out.panelType = "table"
	}
	if !validPanelType(out.panelType) {
		return out, fmt.Errorf("%s: %s", where, invalidPanelTypeMessage)
	}
	if out.query == "" {
		if out.panelType != "text" {
			return out, fmt.Errorf("%s: query is required", where)
		}
		out.query = "SELECT 1"
	}

	raw := strings.TrimSpace(string(p.Config))
	switch {
	case raw == "" || raw == "null":
		out.config = "{}"
	case strings.HasPrefix(raw, "{"):
		if !json.Valid([]byte(raw)) {
			return out, fmt.Errorf("%s: config is not valid JSON", where)
		}
		out.config = raw
	case strings.HasPrefix(raw, `"`):
		var inner string
		if err := json.Unmarshal([]byte(raw), &inner); err != nil || !json.Valid([]byte(inner)) {
			return out, fmt.Errorf("%s: config is not valid JSON", where)
		}
		out.config = inner
	default:
		return out, fmt.Errorf("%s: config must be an object", where)
	}
	if err := dashboards.ValidatePanelLinks(out.config); err != nil {
		return out, fmt.Errorf("%s: %w", where, err)
	}

	if p.LayoutX != nil {
		out.x = *p.LayoutX
	}
	if p.LayoutY != nil {
		out.y = *p.LayoutY
	}
	if p.LayoutW != nil {
		out.w = *p.LayoutW
	}
	if p.LayoutH != nil {
		out.h = *p.LayoutH
	}
	if msg := dashboards.ValidateLayout(out.x, out.y, out.w, out.h); msg != "" {
		return out, fmt.Errorf("%s: %s", where, msg)
	}
	return out, nil
}

// importBody is an exported dashboard file: what import, restore and the
// templates read.
type importBody struct {
	Dashboard *struct {
		Name        *string `json:"name"`
		Description *string `json:"description"`
	} `json:"dashboard"`
	Variables json.RawMessage `json:"variables"`
	Panels    []importPanel   `json:"panels"`
}

// buildContent validates an exported dashboard file against the dashboard it
// is applied to (existing nil: a new dashboard) and returns what to write.
// Every panel is checked before anything is written.
func (h *DashboardsHandler) buildContent(body importBody, existing *database.Dashboard) (database.DashboardContent, error) {
	var c database.DashboardContent
	if body.Dashboard == nil || body.Panels == nil {
		return c, fmt.Errorf(`JSON must have "dashboard" and "panels" fields`)
	}
	if len(body.Panels) > maxImportPanels {
		return c, fmt.Errorf("At most %d panels per dashboard", maxImportPanels)
	}

	if existing != nil {
		c.Name = existing.Name
		c.Description = existing.Description
	}
	if body.Dashboard.Name != nil {
		c.Name = strings.TrimSpace(*body.Dashboard.Name)
	}
	if c.Name == "" {
		return c, fmt.Errorf("Dashboard name cannot be empty")
	}
	if body.Dashboard.Description != nil {
		d := strings.TrimSpace(*body.Dashboard.Description)
		c.Description = &d
	}

	// A file without "variables" keeps the current ones.
	if raw := strings.TrimSpace(string(body.Variables)); raw != "" {
		vars, err := dashboards.ParseVariables(body.Variables)
		if err != nil {
			return c, err
		}
		encoded := dashboards.Encode(vars)
		c.Variables = &encoded
	}

	c.Panels = make([]database.PanelInput, 0, len(body.Panels))
	knownConn := map[string]bool{}
	for i, p := range body.Panels {
		v, err := p.validate(i)
		if err != nil {
			return c, err
		}
		// A file exported from another CH-UI can name a connection that does
		// not exist here; the panel is kept, without the connection.
		if v.connectionID != "" {
			ok, seen := knownConn[v.connectionID]
			if !seen {
				conn, err := h.DB.GetConnectionByID(v.connectionID)
				ok = err == nil && conn != nil
				knownConn[v.connectionID] = ok
			}
			if !ok {
				v.connectionID = ""
			}
		}
		c.Panels = append(c.Panels, database.PanelInput{
			Name: v.name, Description: v.description, PanelType: v.panelType, Query: v.query,
			ConnectionID: v.connectionID, Config: v.config, X: v.x, Y: v.y, W: v.w, H: v.h,
		})
	}
	return c, nil
}

// ImportDashboard replaces a dashboard's name, description, variables and
// panels with the content of an exported dashboard file. Every panel is
// validated before anything changes, and the replacement runs in one
// transaction: a failure leaves the dashboard exactly as it was.
func (h *DashboardsHandler) ImportDashboard(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}

	id := chi.URLParam(r, "id")
	existing, err := h.DB.GetDashboardByID(id)
	if err != nil {
		slog.Error("Failed to get dashboard for import", "error", err, "id", id)
		writeError(w, http.StatusInternalServerError, "Failed to get dashboard")
		return
	}
	if existing == nil {
		writeError(w, http.StatusNotFound, "Dashboard not found")
		return
	}

	var body importBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON body")
		return
	}
	h.applyContent(w, r, existing, body, "dashboard.imported", "")
}

// applyContent validates body, replaces the dashboard with it in one
// transaction, records a version and writes the dashboard, its panels and
// variables as the response.
func (h *DashboardsHandler) applyContent(w http.ResponseWriter, r *http.Request, existing *database.Dashboard, body importBody, action, note string) {
	session := middleware.GetSession(r)
	id := existing.ID
	content, err := h.buildContent(body, existing)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	h.ensureBaselineVersion(id)
	if err := h.DB.ReplaceDashboardContent(id, content); err != nil {
		slog.Error("Failed to replace dashboard content", "error", err, "id", id)
		writeError(w, http.StatusInternalServerError, "Failed to import dashboard")
		return
	}
	h.recordVersion(r, id, note)

	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:         action,
		Username:       strPtr(middleware.Actor(session)),
		ClickhouseUser: strPtr(session.ClickhouseUser),
		Details:        strPtr(fmt.Sprintf("%s (%d panels)", content.Name, len(content.Panels))),
	})

	h.writeDashboardDetail(w, r, id, http.StatusOK)
}

// writeDashboardDetail writes {dashboard, panels, variables} for one dashboard.
func (h *DashboardsHandler) writeDashboardDetail(w http.ResponseWriter, r *http.Request, id string, status int) {
	forUser := ""
	if session := middleware.GetSession(r); session != nil {
		forUser = middleware.Actor(session)
	}
	dashboard, err := h.DB.GetDashboardByIDFor(id, forUser)
	if err != nil || dashboard == nil {
		writeJSON(w, status, map[string]interface{}{"success": true, "id": id})
		return
	}
	saved, err := h.DB.GetPanelsByDashboard(id)
	if err != nil || saved == nil {
		saved = []database.Panel{}
	}
	writeJSON(w, status, map[string]interface{}{"dashboard": dashboard, "panels": saved, "variables": h.loadVariables(id)})
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
		// DashboardID names the dashboard whose variable definitions apply;
		// Variables holds the values the viewer picked, by name.
		DashboardID string              `json:"dashboard_id"`
		Variables   map[string][]string `json:"variables"`
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

	vars, varTypes, verr := h.sessionVariableValues(r, session, strings.TrimSpace(body.DashboardID), query, body.Variables, body.TimeRange)
	if verr != nil {
		writeError(w, verr.status, verr.msg)
		return
	}

	processed := queryproc.ProcessQueryVariables(queryproc.ProcessorOptions{
		Query:         query,
		TimeRange:     body.TimeRange,
		TimeField:     strings.TrimSpace(body.TimeField),
		TimeFieldUnit: strings.TrimSpace(body.TimeFieldUnit),
		MaxDataPoints: maxDataPoints,
		Table:         strings.TrimSpace(body.Table),
		Vars:          vars,
		VarTypes:      varTypes,
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

	// The SQL that runs is checked, after macros are expanded, with the same
	// governance policies the SQL editor applies.
	if !h.enforceGuardrails(w, r, query) {
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
	result, err := h.Gateway.ExecuteQueryWithSettingsCtx(tunnel.WithObjectTag(r.Context(), "dash", body.DashboardID), session.ConnectionID, query, session.ClickhouseUser, password, panelQuerySettings(), timeout)
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

// panelResultRowCap bounds how many rows one panel query returns. Panels
// render at most a few thousand points or 100 table rows, so a larger result
// only costs memory in the browser.
const panelResultRowCap = "100000"

// panelQuerySettings are the ClickHouse settings every authenticated panel
// query runs with. date_time_output_format=iso makes DateTime values come back
// as UTC instants ("2026-01-01T10:00:00Z") so the browser never has to guess
// the server time zone.
func panelQuerySettings() map[string]string {
	return map[string]string{
		"max_result_rows":         panelResultRowCap,
		"result_overflow_mode":    "break",
		"date_time_output_format": "iso",
	}
}

// enforceGuardrails runs the governance pre-execution check for a panel
// query. It writes the error response itself and returns false when the query
// must not run.
func (h *DashboardsHandler) enforceGuardrails(w http.ResponseWriter, r *http.Request, query string) bool {
	q := &QueryHandler{DB: h.DB, Config: h.Config, Guardrails: h.Guardrails}
	return q.enforceGuardrailsForQuery(w, r, query, r.URL.Path)
}

// ---------- Sharing ----------

// ListShares returns all share links for a dashboard. A share link runs
// panels with its creator's ClickHouse credentials and needs no login, so
// only the creator and admins get the token; other writers see that the
// link exists (and may revoke it) without the token.
func (h *DashboardsHandler) ListShares(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}
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
	for i := range shares {
		if !ownsShare(session, &shares[i]) {
			shares[i].Token = ""
		}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"shares": shares})
}

// ownsShare reports whether the caller created the share or is an admin.
func ownsShare(session *middleware.SessionInfo, share *database.DashboardShare) bool {
	if session.UserRole == "admin" {
		return true
	}
	return share.CreatedBy != nil && *share.CreatedBy == middleware.Actor(session)
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

	// An SSO person with a per-person ClickHouse user gets a reference to
	// that user, never a copy of its password: the password rotates at every
	// sign-in, and the share must stop working when the user is removed.
	chUser, encryptedPassword, identityID := session.ClickhouseUser, session.EncryptedPassword, ""
	if session.SSOIdentityID != "" {
		u, err := h.DB.GetSSOClickHouseUser(session.SSOIdentityID, session.ConnectionID)
		if err != nil {
			slog.Error("Failed to resolve the session's per-person user", "error", err)
			writeError(w, http.StatusInternalServerError, "Failed to create share")
			return
		}
		if u != nil {
			chUser, encryptedPassword, identityID = u.CHUser, "", session.SSOIdentityID
		}
	}

	share, err := h.DB.CreateDashboardShare(database.CreateDashboardShareParams{
		DashboardID: dashboardID, AccessLevel: "viewer", Visibility: visibility, CreatedBy: middleware.Actor(session),
		ConnectionID: session.ConnectionID, ClickhouseUser: chUser, EncryptedPassword: encryptedPassword, SSOIdentityID: identityID,
		AllowedEmails: cleanEmails, ExpiresAt: body.ExpiresAt,
	})
	if err != nil {
		slog.Error("Failed to create share", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to create share")
		return
	}

	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:         "dashboard.shared",
		Username:       strPtr(middleware.Actor(session)),
		ClickhouseUser: strPtr(session.ClickhouseUser),
		Details:        strPtr(dashboard.Name),
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
		Action:         "dashboard.share_revoked",
		Username:       strPtr(middleware.Actor(session)),
		ClickhouseUser: strPtr(session.ClickhouseUser),
		Details:        strPtr(shareID),
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
	if err != nil || share == nil || share.DashboardID != chi.URLParam(r, "id") {
		writeError(w, http.StatusNotFound, "Share not found")
		return
	}
	// The invite mail carries the share token, so it follows the same rule
	// as seeing the token in ListShares.
	if !ownsShare(session, share) {
		writeError(w, http.StatusForbidden, "Only the creator of this link or an admin can send its invites")
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

		if _, err := mail.Send(r.Context(), channel.ChannelType, channelConfig, []string{email}, subject, body); err != nil {
			slog.Error("Failed to send invite email", "error", err, "email", email)
			failed = append(failed, email)
		} else {
			sent = append(sent, email)
		}
	}

	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:         "dashboard.invites_sent",
		Username:       strPtr(middleware.Actor(session)),
		ClickhouseUser: strPtr(session.ClickhouseUser),
		Details:        strPtr(fmt.Sprintf("%d sent, %d failed", len(sent), len(failed))),
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

// publicQuerySettings bound what a share link can do with the sharer's
// credentials: read only, and capped in time and size. DateTime values come
// back as UTC instants, as for authenticated panel queries.
var publicQuerySettings = map[string]string{
	"readonly":                "1",
	"max_execution_time":      "30",
	"max_result_rows":         "10000",
	"max_result_bytes":        "33554432",
	"result_overflow_mode":    "break",
	"date_time_output_format": "iso",
}

// publicShareContext is the query context for a public dashboard share: not
// the viewer's request, and tagged with the person who created the share.
func publicShareContext(share *database.DashboardShare) context.Context {
	owner := ""
	if share.CreatedBy != nil {
		owner = *share.CreatedBy
	}
	return tunnel.WithTag(context.Background(), tunnel.QueryTag{Surface: "public_dashboard", Person: owner}.WithObject("dash", share.DashboardID))
}

// resolvePublicShare loads the share behind the URL token and checks expiry
// and access. It writes the error response itself and returns nil when the
// request must stop. An expiry that does not parse counts as expired.
func (h *DashboardsHandler) resolvePublicShare(w http.ResponseWriter, r *http.Request) *database.DashboardShare {
	share, err := h.DB.GetDashboardShareByToken(chi.URLParam(r, "token"))
	if err != nil {
		slog.Error("Failed to look up share token", "error", err)
		writeError(w, http.StatusInternalServerError, "Internal error")
		return nil
	}
	if share == nil {
		writeError(w, http.StatusNotFound, "Dashboard not found or share link expired")
		return nil
	}

	if share.ExpiresAt != nil {
		exp, parseErr := time.Parse(time.RFC3339, *share.ExpiresAt)
		if parseErr != nil || time.Now().UTC().After(exp) {
			writeError(w, http.StatusGone, "Share link has expired")
			return nil
		}
	}

	if !h.validateShareAccess(share, r) {
		writeJSON(w, http.StatusForbidden, map[string]interface{}{
			"success":    false,
			"error":      "This dashboard requires an invite. Check your email for a magic link.",
			"visibility": "private",
		})
		return nil
	}
	return share
}

// publicDashboard is the part of a dashboard a share link shows. Owner,
// folder, tags and connection stay on the server.
type publicDashboard struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Description *string `json:"description"`
	UpdatedAt   string  `json:"updated_at"`
}

// publicPanel is a panel as a share link draws it: no SQL and no connection.
type publicPanel struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	PanelType   string `json:"panel_type"`
	Config      string `json:"config"`
	LayoutX     int    `json:"layout_x"`
	LayoutY     int    `json:"layout_y"`
	LayoutW     int    `json:"layout_w"`
	LayoutH     int    `json:"layout_h"`
}

// GetPublicDashboard returns a dashboard and its panels via a share token,
// with only what the shared page draws. Panel SQL is left out: viewers run
// panels by ID, never by query text.
func (h *DashboardsHandler) GetPublicDashboard(w http.ResponseWriter, r *http.Request) {
	share := h.resolvePublicShare(w, r)
	if share == nil {
		return
	}

	dashboard, err := h.DB.GetDashboardByID(share.DashboardID)
	if err != nil || dashboard == nil {
		writeError(w, http.StatusNotFound, "Dashboard not found")
		return
	}

	panels, err := h.DB.GetPanelsByDashboard(share.DashboardID)
	if err != nil || panels == nil {
		panels = []database.Panel{}
	}
	// Share links never see panel SQL, SQL link templates or links to other
	// dashboards.
	public := make([]publicPanel, 0, len(panels))
	for _, p := range panels {
		public = append(public, publicPanel{
			ID: p.ID, Name: p.Name, Description: p.Description, PanelType: p.PanelType,
			Config:  dashboards.PublicPanelConfig(p.Config),
			LayoutX: p.LayoutX, LayoutY: p.LayoutY, LayoutW: p.LayoutW, LayoutH: p.LayoutH,
		})
	}
	// Constants are left out; ExecutePublicQuery substitutes them from the
	// stored definition.
	vars := dashboards.PublicList(h.loadVariables(share.DashboardID))

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"dashboard": publicDashboard{ID: dashboard.ID, Name: dashboard.Name, Description: dashboard.Description, UpdatedAt: dashboard.UpdatedAt},
		"panels":    public,
		"variables": vars,
	})
}

// ExecutePublicQuery runs one panel of the shared dashboard. The caller names
// the panel; the SQL always comes from the stored panel, so a share link can
// never run arbitrary SQL with the sharer's credentials.
func (h *DashboardsHandler) ExecutePublicQuery(w http.ResponseWriter, r *http.Request) {
	share := h.resolvePublicShare(w, r)
	if share == nil {
		return
	}

	var body struct {
		PanelID   string               `json:"panel_id"`
		TimeRange *queryproc.TimeRange `json:"time_range"`
		Variables map[string][]string  `json:"variables"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON body")
		return
	}

	panelID := strings.TrimSpace(body.PanelID)
	if panelID == "" {
		writeError(w, http.StatusBadRequest, "panel_id is required")
		return
	}

	panel, err := h.DB.GetPanelByID(panelID)
	if err != nil {
		slog.Error("Failed to load panel for public query", "error", err)
		writeError(w, http.StatusInternalServerError, "Internal error")
		return
	}
	if panel == nil || panel.DashboardID != share.DashboardID {
		writeError(w, http.StatusNotFound, "Panel not found")
		return
	}

	query := strings.TrimSpace(panel.Query)
	if panel.PanelType == "text" || query == "" {
		writeError(w, http.StatusBadRequest, "Panel has no query")
		return
	}

	// Snap the range to publicCacheStep so every viewer of a TV dashboard asks
	// for the same range and shares one cached result.
	timeRange, rangeKey := snapPublicTimeRange(body.TimeRange, time.Now())

	// Variable values must be options of their variable (re-run with the
	// sharer's credentials) or the default of a text or constant variable.
	vars, varTypes, verr := h.publicVariableValues(r, share, query, body.Variables, timeRange)
	if verr != nil {
		writeError(w, verr.status, verr.msg)
		return
	}
	key := strings.Join([]string{share.Token, panel.ID, panel.UpdatedAt, rangeKey, dashboards.CanonicalKey(vars)}, "|")

	cache, limiter := h.publicState()
	if status, payload, ok := cache.get(key); ok {
		writePublicPayload(w, status, payload, "HIT")
		return
	}
	// Only requests that reach ClickHouse count against the budget.
	if !limiter.allow(share.Token + "|" + middleware.ClientIP(r)) {
		w.Header().Set("Retry-After", "60")
		writeError(w, http.StatusTooManyRequests, "Rate limit exceeded. Please slow down.")
		return
	}

	status, payload := cache.do(key, func() (int, []byte) {
		return h.runPublicPanel(r, share, query, timeRange, vars, varTypes)
	})
	writePublicPayload(w, status, payload, "MISS")
}

// runPublicPanel expands the panel macros and runs the query with the
// sharer's credentials. It returns the HTTP status and the JSON body, so the
// result can be cached and shared between concurrent viewers.
func (h *DashboardsHandler) runPublicPanel(r *http.Request, share *database.DashboardShare, query string, timeRange *queryproc.TimeRange, vars map[string][]string, varTypes map[string]string) (int, []byte) {
	processed := queryproc.ProcessQueryVariables(queryproc.ProcessorOptions{
		Query:         query,
		TimeRange:     timeRange,
		MaxDataPoints: 1000,
		Vars:          vars,
		VarTypes:      varTypes,
	})
	if len(processed.Errors) > 0 {
		return errorPayload(http.StatusBadRequest, strings.Join(processed.Errors, "; "))
	}

	query = strings.TrimSpace(processed.Query)
	if query == "" {
		return errorPayload(http.StatusBadRequest, "Processed query is empty")
	}

	creds, serr := h.shareCredentials(share)
	if serr != nil {
		return errorPayload(serr.status, serr.msg)
	}

	// The sharer's governance policies still apply to what the link runs.
	if h.guardrailsEnabled() {
		decision, err := h.Guardrails.EvaluateQuery(creds.connectionID, creds.user, query, r.URL.Path)
		if err != nil {
			slog.Error("Guardrail evaluation failed for public panel", "error", err)
			return errorPayload(http.StatusInternalServerError, "Failed to evaluate governance guardrails")
		}
		if !decision.Allowed {
			return errorPayload(http.StatusForbidden, "Query blocked by governance policy")
		}
	}

	password, err := crypto.Decrypt(creds.encryptedPassword, h.Config.AppSecretKey)
	if err != nil {
		slog.Error("Failed to decrypt share credentials", "error", err)
		return errorPayload(http.StatusInternalServerError, "Failed to process query")
	}

	// Not bound to this request's context: concurrent viewers may be waiting
	// on the same result. Tagged with the share's owner and dashboard, since
	// the viewer is anonymous.
	result, qErr := h.Gateway.ExecuteQueryWithSettingsCtx(publicShareContext(share), creds.connectionID, query, creds.user, password, publicQuerySettings, 35*time.Second)
	if qErr != nil {
		// ClickHouse errors can quote the SQL, which a share link never sees.
		slog.Warn("Public panel query failed", "error", qErr, "dashboard", share.DashboardID)
		return errorPayload(http.StatusBadGateway, publicPanelError)
	}

	payload, err := json.Marshal(map[string]interface{}{
		"success": true,
		"data":    result.Data,
		"meta":    result.Meta,
		"rows":    countRows(result.Data),
	})
	if err != nil {
		return errorPayload(http.StatusInternalServerError, "Failed to encode result")
	}
	return http.StatusOK, payload
}

func (h *DashboardsHandler) guardrailsEnabled() bool {
	if h.Guardrails == nil {
		return false
	}
	return h.Config == nil || h.Config.IsPro()
}

// publicPanelError is what a share link viewer sees when a panel query
// fails; the ClickHouse error is logged instead.
const publicPanelError = "This panel failed to load"

func errorPayload(status int, message string) (int, []byte) {
	payload, _ := json.Marshal(map[string]interface{}{"success": false, "error": message})
	return status, payload
}

func writePublicPayload(w http.ResponseWriter, status int, payload []byte, cacheState string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Cache", cacheState)
	w.WriteHeader(status)
	_, _ = w.Write(payload)
}

// ---------- Public query cache and rate limit ----------

const (
	// publicCacheStep is the granularity public time ranges are snapped to,
	// and publicCacheTTL how long a result is served from memory.
	publicCacheStep = 15 * time.Second
	publicCacheTTL  = 20 * time.Second

	publicCacheMaxEntries    = 256
	publicCacheMaxBytes      = 64 << 20
	publicCacheMaxEntryBytes = 4 << 20

	// publicQueryBudget is how many uncached panel queries one viewer (share
	// token and IP) may run per minute: a 25 panel dashboard refreshing every
	// 30 s needs 50, so this leaves room for range changes and several
	// screens behind one address.
	publicQueryBudget = 300
)

func (h *DashboardsHandler) publicState() (*publicQueryCache, *keyedRateLimiter) {
	h.publicOnce.Do(func() {
		h.publicCache = newPublicQueryCache(publicCacheTTL, publicCacheMaxEntries, publicCacheMaxBytes, publicCacheMaxEntryBytes)
		h.publicLimit = newKeyedRateLimiter(publicQueryBudget, time.Minute)
	})
	return h.publicCache, h.publicLimit
}

// snapPublicTimeRange resolves a requested range against now and widens it to
// publicCacheStep boundaries: the start is rounded down and the end up, so no
// data in the requested range is left out. It returns the absolute range to
// run and the cache key part for it. A missing or invalid range is passed
// through unchanged, so the macro processor reports it as before.
func snapPublicTimeRange(tr *queryproc.TimeRange, now time.Time) (*queryproc.TimeRange, string) {
	if tr == nil {
		return nil, "none"
	}
	from, to, ok := queryproc.BoundsAt(tr, now)
	if !ok {
		return tr, "invalid|" + tr.Type + "|" + tr.From + "|" + tr.To
	}
	from = from.Truncate(publicCacheStep)
	if snapped := to.Truncate(publicCacheStep); snapped.Before(to) {
		to = snapped.Add(publicCacheStep)
	}
	return &queryproc.TimeRange{
		Type: "absolute",
		From: from.UTC().Format(time.RFC3339),
		To:   to.UTC().Format(time.RFC3339),
	}, fmt.Sprintf("%d|%d", from.Unix(), to.Unix())
}

// publicQueryCache is a small in-memory LRU of public panel results with a
// TTL. Concurrent misses for the same key share one ClickHouse query.
type publicQueryCache struct {
	mu            sync.Mutex
	ttl           time.Duration
	maxEntries    int
	maxBytes      int
	maxEntryBytes int
	bytes         int
	order         *list.List // front is most recently used
	items         map[string]*list.Element
	inflight      map[string]*publicCall
	now           func() time.Time
}

type publicCacheEntry struct {
	key     string
	status  int
	payload []byte
	expires time.Time
}

type publicCall struct {
	done    chan struct{}
	status  int
	payload []byte
}

func newPublicQueryCache(ttl time.Duration, maxEntries, maxBytes, maxEntryBytes int) *publicQueryCache {
	return &publicQueryCache{
		ttl:           ttl,
		maxEntries:    maxEntries,
		maxBytes:      maxBytes,
		maxEntryBytes: maxEntryBytes,
		order:         list.New(),
		items:         map[string]*list.Element{},
		inflight:      map[string]*publicCall{},
		now:           time.Now,
	}
}

func (c *publicQueryCache) get(key string) (int, []byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.getLocked(key)
}

func (c *publicQueryCache) getLocked(key string) (int, []byte, bool) {
	el, ok := c.items[key]
	if !ok {
		return 0, nil, false
	}
	e := el.Value.(*publicCacheEntry)
	if !c.now().Before(e.expires) {
		c.removeLocked(el)
		return 0, nil, false
	}
	c.order.MoveToFront(el)
	return e.status, e.payload, true
}

// do returns the cached result for key, waits for a query already running
// for it, or runs fn. Only 200 responses are stored.
func (c *publicQueryCache) do(key string, fn func() (int, []byte)) (int, []byte) {
	c.mu.Lock()
	if status, payload, ok := c.getLocked(key); ok {
		c.mu.Unlock()
		return status, payload
	}
	if call, ok := c.inflight[key]; ok {
		c.mu.Unlock()
		<-call.done
		return call.status, call.payload
	}
	call := &publicCall{done: make(chan struct{})}
	c.inflight[key] = call
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		delete(c.inflight, key)
		if call.status == http.StatusOK && len(call.payload) <= c.maxEntryBytes {
			c.storeLocked(key, call.status, call.payload)
		}
		c.mu.Unlock()
		close(call.done)
	}()
	// A panic in fn still releases the waiters, with a 500.
	call.status, call.payload = errorPayload(http.StatusInternalServerError, "Internal error")
	call.status, call.payload = fn()
	return call.status, call.payload
}

func (c *publicQueryCache) storeLocked(key string, status int, payload []byte) {
	if el, ok := c.items[key]; ok {
		c.removeLocked(el)
	}
	el := c.order.PushFront(&publicCacheEntry{key: key, status: status, payload: payload, expires: c.now().Add(c.ttl)})
	c.items[key] = el
	c.bytes += len(payload)
	for c.order.Len() > c.maxEntries || c.bytes > c.maxBytes {
		c.removeLocked(c.order.Back())
	}
}

func (c *publicQueryCache) removeLocked(el *list.Element) {
	e := el.Value.(*publicCacheEntry)
	c.order.Remove(el)
	delete(c.items, e.key)
	c.bytes -= len(e.payload)
}

// keyedRateLimiter is a fixed-window limiter keyed by an arbitrary string.
type keyedRateLimiter struct {
	mu     sync.Mutex
	limit  int
	window time.Duration
	counts map[string]*keyedWindow
	lastGC time.Time
	now    func() time.Time
}

type keyedWindow struct {
	count int
	reset time.Time
}

func newKeyedRateLimiter(limit int, window time.Duration) *keyedRateLimiter {
	return &keyedRateLimiter{limit: limit, window: window, counts: map[string]*keyedWindow{}, lastGC: time.Now(), now: time.Now}
}

func (l *keyedRateLimiter) allow(key string) bool {
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()
	if now.Sub(l.lastGC) > l.window {
		for k, w := range l.counts {
			if now.After(w.reset) {
				delete(l.counts, k)
			}
		}
		l.lastGC = now
	}
	w, ok := l.counts[key]
	if !ok || now.After(w.reset) {
		l.counts[key] = &keyedWindow{count: 1, reset: now.Add(l.window)}
		return true
	}
	if w.count >= l.limit {
		return false
	}
	w.count++
	return true
}
