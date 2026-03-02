package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/pipelines"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

// PipelinesHandler handles pipeline CRUD and lifecycle operations.
type PipelinesHandler struct {
	DB      *database.DB
	Gateway *tunnel.Gateway
	Config  *config.Config
	Runner  *pipelines.Runner
}

// Routes returns a chi.Router with all pipeline routes mounted.
func (h *PipelinesHandler) Routes() chi.Router {
	r := chi.NewRouter()

	r.Get("/", h.ListPipelines)
	r.Post("/", h.CreatePipeline)

	r.Route("/{id}", func(r chi.Router) {
		r.Get("/", h.GetPipeline)
		r.Put("/", h.UpdatePipeline)
		r.Delete("/", h.DeletePipeline)

		// Graph operations
		r.Put("/graph", h.SaveGraph)

		// Lifecycle
		r.Post("/start", h.StartPipeline)
		r.Post("/stop", h.StopPipeline)

		// Status & monitoring
		r.Get("/status", h.GetStatus)
		r.Get("/runs", h.ListRuns)
		r.Get("/runs/{runId}/logs", h.GetRunLogs)
	})

	return r
}

// ListPipelines returns all pipelines.
func (h *PipelinesHandler) ListPipelines(w http.ResponseWriter, r *http.Request) {
	pipelines, err := h.DB.GetPipelines()
	if err != nil {
		slog.Error("Failed to list pipelines", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to list pipelines"})
		return
	}

	if pipelines == nil {
		pipelines = []database.Pipeline{}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"pipelines": pipelines})
}

// GetPipeline returns a single pipeline with its graph.
func (h *PipelinesHandler) GetPipeline(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Pipeline ID is required"})
		return
	}

	pipeline, err := h.DB.GetPipelineByID(id)
	if err != nil {
		slog.Error("Failed to get pipeline", "error", err, "id", id)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to get pipeline"})
		return
	}
	if pipeline == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Pipeline not found"})
		return
	}

	nodes, edges, err := h.DB.GetPipelineGraph(id)
	if err != nil {
		slog.Error("Failed to get pipeline graph", "error", err, "pipeline", id)
		nodes = []database.PipelineNode{}
		edges = []database.PipelineEdge{}
	}
	if nodes == nil {
		nodes = []database.PipelineNode{}
	}
	if edges == nil {
		edges = []database.PipelineEdge{}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"pipeline": pipeline,
		"graph": map[string]interface{}{
			"nodes": nodes,
			"edges": edges,
		},
	})
}

// CreatePipeline creates a new pipeline.
func (h *PipelinesHandler) CreatePipeline(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Not authenticated"})
		return
	}

	var body struct {
		Name         string `json:"name"`
		Description  string `json:"description"`
		ConnectionID string `json:"connection_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid JSON body"})
		return
	}

	name := strings.TrimSpace(body.Name)
	if name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Name is required"})
		return
	}

	connectionID := strings.TrimSpace(body.ConnectionID)
	if connectionID == "" {
		// Use the session's connection ID as default
		connectionID = session.ConnectionID
	}

	id, err := h.DB.CreatePipeline(name, strings.TrimSpace(body.Description), connectionID, session.ClickhouseUser)
	if err != nil {
		slog.Error("Failed to create pipeline", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to create pipeline"})
		return
	}

	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:   "pipeline.created",
		Username: &session.ClickhouseUser,
		Details:  &name,
	})

	pipeline, _ := h.DB.GetPipelineByID(id)
	writeJSON(w, http.StatusCreated, map[string]interface{}{"pipeline": pipeline})
}

// UpdatePipeline updates a pipeline's name and description.
func (h *PipelinesHandler) UpdatePipeline(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Not authenticated"})
		return
	}

	id := chi.URLParam(r, "id")

	var body struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid JSON body"})
		return
	}

	name := strings.TrimSpace(body.Name)
	if name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Name is required"})
		return
	}

	if err := h.DB.UpdatePipeline(id, name, strings.TrimSpace(body.Description)); err != nil {
		slog.Error("Failed to update pipeline", "error", err, "id", id)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to update pipeline"})
		return
	}

	pipeline, _ := h.DB.GetPipelineByID(id)
	writeJSON(w, http.StatusOK, map[string]interface{}{"pipeline": pipeline})
}

// DeletePipeline deletes a pipeline.
func (h *PipelinesHandler) DeletePipeline(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Not authenticated"})
		return
	}

	id := chi.URLParam(r, "id")

	// Check if pipeline exists and is not running
	pipeline, err := h.DB.GetPipelineByID(id)
	if err != nil {
		slog.Error("Failed to get pipeline", "error", err, "id", id)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to get pipeline"})
		return
	}
	if pipeline == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Pipeline not found"})
		return
	}
	if pipeline.Status == "running" || pipeline.Status == "starting" {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "Cannot delete a running pipeline. Stop it first."})
		return
	}

	if err := h.DB.DeletePipeline(id); err != nil {
		slog.Error("Failed to delete pipeline", "error", err, "id", id)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to delete pipeline"})
		return
	}

	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:   "pipeline.deleted",
		Username: &session.ClickhouseUser,
		Details:  &pipeline.Name,
	})

	writeJSON(w, http.StatusOK, map[string]string{"success": "true"})
}

// SaveGraph saves the entire pipeline graph (nodes + edges).
func (h *PipelinesHandler) SaveGraph(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Not authenticated"})
		return
	}

	id := chi.URLParam(r, "id")

	var body struct {
		Nodes    []graphNode    `json:"nodes"`
		Edges    []graphEdge    `json:"edges"`
		Viewport *graphViewport `json:"viewport"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid JSON body"})
		return
	}

	// Convert to database types
	var nodes []database.PipelineNode
	for _, n := range body.Nodes {
		configJSON, _ := json.Marshal(n.Config)
		nodes = append(nodes, database.PipelineNode{
			ID:              n.ID,
			PipelineID:      id,
			NodeType:        n.NodeType,
			Label:           n.Label,
			PositionX:       n.PositionX,
			PositionY:       n.PositionY,
			ConfigEncrypted: string(configJSON),
		})
	}

	var edges []database.PipelineEdge
	for _, e := range body.Edges {
		edges = append(edges, database.PipelineEdge{
			ID:           e.ID,
			PipelineID:   id,
			SourceNodeID: e.SourceNodeID,
			TargetNodeID: e.TargetNodeID,
			SourceHandle: e.SourceHandle,
			TargetHandle: e.TargetHandle,
		})
	}

	viewportJSON := ""
	if body.Viewport != nil {
		vp, _ := json.Marshal(body.Viewport)
		viewportJSON = string(vp)
	}

	if err := h.DB.SavePipelineGraph(id, nodes, edges, viewportJSON); err != nil {
		slog.Error("Failed to save pipeline graph", "error", err, "id", id)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to save pipeline graph"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"success": "true"})
}

// StartPipeline starts a pipeline (placeholder for Phase 3).
func (h *PipelinesHandler) StartPipeline(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Not authenticated"})
		return
	}

	id := chi.URLParam(r, "id")

	pipeline, err := h.DB.GetPipelineByID(id)
	if err != nil || pipeline == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Pipeline not found"})
		return
	}

	if pipeline.Status == "running" || pipeline.Status == "starting" {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "Pipeline is already running"})
		return
	}

	if err := h.Runner.StartPipeline(id); err != nil {
		slog.Error("Failed to start pipeline", "error", err, "id", id)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:   "pipeline.started",
		Username: &session.ClickhouseUser,
		Details:  &pipeline.Name,
	})

	writeJSON(w, http.StatusOK, map[string]string{"success": "true"})
}

// StopPipeline stops a running pipeline (placeholder for Phase 3).
func (h *PipelinesHandler) StopPipeline(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Not authenticated"})
		return
	}

	id := chi.URLParam(r, "id")

	pipeline, err := h.DB.GetPipelineByID(id)
	if err != nil || pipeline == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Pipeline not found"})
		return
	}

	if pipeline.Status != "running" && pipeline.Status != "starting" {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "Pipeline is not running"})
		return
	}

	if err := h.Runner.StopPipeline(id); err != nil {
		slog.Error("Failed to stop pipeline", "error", err, "id", id)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:   "pipeline.stopped",
		Username: &session.ClickhouseUser,
		Details:  &pipeline.Name,
	})

	writeJSON(w, http.StatusOK, map[string]string{"success": "true"})
}

// GetStatus returns the current status of a pipeline.
func (h *PipelinesHandler) GetStatus(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	pipeline, err := h.DB.GetPipelineByID(id)
	if err != nil || pipeline == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Pipeline not found"})
		return
	}

	resp := map[string]interface{}{
		"pipeline_id": pipeline.ID,
		"status":      pipeline.Status,
		"last_error":  pipeline.LastError,
	}

	// Add live metrics if pipeline is running
	if metrics := h.Runner.GetRunningMetrics(id); metrics != nil {
		resp["rows_ingested"] = metrics.RowsIngested.Load()
		resp["bytes_ingested"] = metrics.BytesIngested.Load()
		resp["batches_sent"] = metrics.BatchesSent.Load()
		resp["errors_count"] = metrics.ErrorsCount.Load()
	}

	writeJSON(w, http.StatusOK, resp)
}

// ListRuns returns execution runs for a pipeline.
func (h *PipelinesHandler) ListRuns(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	limit := 20
	offset := 0
	if v := r.URL.Query().Get("limit"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed >= 0 {
			offset = parsed
		}
	}

	runs, err := h.DB.GetPipelineRuns(id, limit, offset)
	if err != nil {
		slog.Error("Failed to list pipeline runs", "error", err, "pipeline", id)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to list runs"})
		return
	}
	if runs == nil {
		runs = []database.PipelineRun{}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"runs": runs})
}

// GetRunLogs returns logs for a specific pipeline run.
func (h *PipelinesHandler) GetRunLogs(w http.ResponseWriter, r *http.Request) {
	runID := chi.URLParam(r, "runId")

	limit := 200
	if v := r.URL.Query().Get("limit"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	logs, err := h.DB.GetPipelineRunLogs(runID, limit)
	if err != nil {
		slog.Error("Failed to get run logs", "error", err, "run", runID)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to get run logs"})
		return
	}
	if logs == nil {
		logs = []database.PipelineRunLog{}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"logs": logs})
}

// ── Graph request types ────────────────────────────────────────────

type graphNode struct {
	ID        string                 `json:"id"`
	NodeType  string                 `json:"node_type"`
	Label     string                 `json:"label"`
	PositionX float64                `json:"position_x"`
	PositionY float64                `json:"position_y"`
	Config    map[string]interface{} `json:"config"`
}

type graphEdge struct {
	ID           string  `json:"id"`
	SourceNodeID string  `json:"source_node_id"`
	TargetNodeID string  `json:"target_node_id"`
	SourceHandle *string `json:"source_handle"`
	TargetHandle *string `json:"target_handle"`
}

type graphViewport struct {
	X    float64 `json:"x"`
	Y    float64 `json:"y"`
	Zoom float64 `json:"zoom"`
}
