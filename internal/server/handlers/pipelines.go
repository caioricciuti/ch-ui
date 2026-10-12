package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

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

	// Writes and lifecycle control require admin/analyst; viewers are read-only.
	writer := middleware.RequireWriter()

	r.Get("/", h.ListPipelines)
	r.With(writer).Post("/", h.CreatePipeline)

	r.Route("/{id}", func(r chi.Router) {
		r.Get("/", h.GetPipeline)
		r.With(writer).Put("/", h.UpdatePipeline)
		r.With(writer).Delete("/", h.DeletePipeline)

		// Graph operations
		r.With(writer).Put("/graph", h.SaveGraph)

		// Lifecycle
		r.With(writer).Post("/start", h.StartPipeline)
		r.With(writer).Post("/stop", h.StopPipeline)

		// Status & monitoring
		r.Get("/status", h.GetStatus)
		r.Get("/runs", h.ListRuns)
		r.Get("/runs/{runId}/logs", h.GetRunLogs)
	})

	return r
}

// ListPipelines returns the pipelines on the caller's connection.
func (h *PipelinesHandler) ListPipelines(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}

	all, err := h.DB.GetPipelines()
	if err != nil {
		slog.Error("Failed to list pipelines", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to list pipelines")
		return
	}

	pipelines := []database.Pipeline{}
	for _, p := range all {
		if p.ConnectionID == session.ConnectionID {
			pipelines = append(pipelines, p)
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"pipelines": pipelines})
}

// pipelineForSession loads the pipeline named in the URL when it belongs to
// the caller's connection. Pipelines run with their connection's credentials,
// so one on another connection is reported as not found. On failure it writes
// the response and returns nil.
func (h *PipelinesHandler) pipelineForSession(w http.ResponseWriter, r *http.Request) (*database.Pipeline, *middleware.SessionInfo) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return nil, nil
	}
	id := chi.URLParam(r, "id")
	pipeline, err := h.DB.GetPipelineByID(id)
	if err != nil {
		slog.Error("Failed to get pipeline", "error", err, "id", id)
		writeError(w, http.StatusInternalServerError, "Failed to get pipeline")
		return nil, nil
	}
	if pipeline == nil || pipeline.ConnectionID != session.ConnectionID {
		writeError(w, http.StatusNotFound, "Pipeline not found")
		return nil, nil
	}
	return pipeline, session
}

// GetPipeline returns a single pipeline with its graph.
func (h *PipelinesHandler) GetPipeline(w http.ResponseWriter, r *http.Request) {
	pipeline, _ := h.pipelineForSession(w, r)
	if pipeline == nil {
		return
	}
	id := pipeline.ID

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
	// Secrets never leave the server; the editor sends the mask back and
	// SaveGraph keeps the stored value.
	for i := range nodes {
		nodes[i].ConfigEncrypted = maskNodeConfig(nodes[i].ConfigEncrypted)
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
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}

	var body struct {
		Name         string `json:"name"`
		Description  string `json:"description"`
		ConnectionID string `json:"connection_id"`
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

	// A pipeline runs with its connection's credentials, so it can only be
	// created on the connection the caller is signed in to.
	connectionID := session.ConnectionID
	if requested := strings.TrimSpace(body.ConnectionID); requested != "" && requested != connectionID {
		writeError(w, http.StatusForbidden, "Pipelines can only be created on the connection you are signed in to")
		return
	}

	id, err := h.DB.CreatePipeline(name, strings.TrimSpace(body.Description), connectionID, middleware.Actor(session))
	if err != nil {
		slog.Error("Failed to create pipeline", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to create pipeline")
		return
	}

	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:         "pipeline.created",
		Username:       strPtr(middleware.Actor(session)),
		ClickhouseUser: &session.ClickhouseUser,
		Details:        &name,
	})

	pipeline, _ := h.DB.GetPipelineByID(id)
	writeJSON(w, http.StatusCreated, map[string]interface{}{"pipeline": pipeline})
}

// UpdatePipeline updates a pipeline's name and description.
func (h *PipelinesHandler) UpdatePipeline(w http.ResponseWriter, r *http.Request) {
	existing, _ := h.pipelineForSession(w, r)
	if existing == nil {
		return
	}
	id := existing.ID

	var body struct {
		Name        string `json:"name"`
		Description string `json:"description"`
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

	if err := h.DB.UpdatePipeline(id, name, strings.TrimSpace(body.Description)); err != nil {
		slog.Error("Failed to update pipeline", "error", err, "id", id)
		writeError(w, http.StatusInternalServerError, "Failed to update pipeline")
		return
	}

	pipeline, _ := h.DB.GetPipelineByID(id)
	writeJSON(w, http.StatusOK, map[string]interface{}{"pipeline": pipeline})
}

// DeletePipeline deletes a pipeline.
func (h *PipelinesHandler) DeletePipeline(w http.ResponseWriter, r *http.Request) {
	pipeline, session := h.pipelineForSession(w, r)
	if pipeline == nil {
		return
	}
	id := pipeline.ID
	if pipeline.Status == "running" || pipeline.Status == "starting" {
		writeError(w, http.StatusConflict, "Cannot delete a running pipeline. Stop it first.")
		return
	}

	if err := h.DB.DeletePipeline(id); err != nil {
		slog.Error("Failed to delete pipeline", "error", err, "id", id)
		writeError(w, http.StatusInternalServerError, "Failed to delete pipeline")
		return
	}

	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:         "pipeline.deleted",
		Username:       strPtr(middleware.Actor(session)),
		ClickhouseUser: &session.ClickhouseUser,
		Details:        &pipeline.Name,
	})

	writeJSON(w, http.StatusOK, map[string]string{"success": "true"})
}

// SaveGraph saves the entire pipeline graph (nodes + edges).
func (h *PipelinesHandler) SaveGraph(w http.ResponseWriter, r *http.Request) {
	pipeline, session := h.pipelineForSession(w, r)
	if pipeline == nil {
		return
	}
	id := pipeline.ID

	var body struct {
		Nodes    []graphNode    `json:"nodes"`
		Edges    []graphEdge    `json:"edges"`
		Viewport *graphViewport `json:"viewport"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON body")
		return
	}

	// Stored configs, so secrets sent back masked keep their value.
	prev := map[string]map[string]interface{}{}
	if oldNodes, _, err := h.DB.GetPipelineGraph(id); err == nil {
		for _, n := range oldNodes {
			var cfg map[string]interface{}
			if json.Unmarshal([]byte(n.ConfigEncrypted), &cfg) == nil {
				prev[n.ID] = cfg
			}
		}
	}

	// Convert to database types
	var nodes []database.PipelineNode
	for _, n := range body.Nodes {
		if pipelines.IsSQLiteSource(n.NodeType, n.Config) {
			if status, msg := h.checkSQLiteSource(session, n.Config, prev[n.ID]); status != 0 {
				writeError(w, status, msg)
				return
			}
		}
		cfg, err := pipelines.EncryptConfigSecrets(n.Config, prev[n.ID], h.secretKey())
		if err != nil {
			slog.Error("Failed to encrypt pipeline secrets", "error", err, "pipeline", id)
			writeError(w, http.StatusInternalServerError, "Failed to save pipeline graph")
			return
		}
		configJSON, _ := json.Marshal(cfg)
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
		writeError(w, http.StatusInternalServerError, "Failed to save pipeline graph")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"success": "true"})
}

// StartPipeline starts a pipeline.
func (h *PipelinesHandler) StartPipeline(w http.ResponseWriter, r *http.Request) {
	pipeline, session := h.pipelineForSession(w, r)
	if pipeline == nil {
		return
	}
	id := pipeline.ID

	if pipeline.Status == "running" || pipeline.Status == "starting" {
		writeError(w, http.StatusConflict, "Pipeline is already running")
		return
	}

	// SQLite sources read files on this server: admins only.
	if nodes, _, err := h.DB.GetPipelineGraph(id); err == nil {
		for _, n := range nodes {
			var cfg map[string]interface{}
			if json.Unmarshal([]byte(n.ConfigEncrypted), &cfg) == nil && pipelines.IsSQLiteSource(n.NodeType, cfg) && session.UserRole != "admin" {
				writeError(w, http.StatusForbidden, "Only admins can start a pipeline with a SQLite source")
				return
			}
		}
	}

	if err := h.Runner.StartPipeline(id); err != nil {
		slog.Error("Failed to start pipeline", "error", err, "id", id)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:         "pipeline.started",
		Username:       strPtr(middleware.Actor(session)),
		ClickhouseUser: &session.ClickhouseUser,
		Details:        &pipeline.Name,
	})

	writeJSON(w, http.StatusOK, map[string]string{"success": "true"})
}

// StopPipeline stops a running pipeline.
func (h *PipelinesHandler) StopPipeline(w http.ResponseWriter, r *http.Request) {
	pipeline, session := h.pipelineForSession(w, r)
	if pipeline == nil {
		return
	}
	id := pipeline.ID

	if pipeline.Status != "running" && pipeline.Status != "starting" {
		writeError(w, http.StatusConflict, "Pipeline is not running")
		return
	}

	if err := h.Runner.StopPipeline(id); err != nil {
		slog.Error("Failed to stop pipeline", "error", err, "id", id)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:         "pipeline.stopped",
		Username:       strPtr(middleware.Actor(session)),
		ClickhouseUser: &session.ClickhouseUser,
		Details:        &pipeline.Name,
	})

	writeJSON(w, http.StatusOK, map[string]string{"success": "true"})
}

// GetStatus returns the current status of a pipeline.
func (h *PipelinesHandler) GetStatus(w http.ResponseWriter, r *http.Request) {
	pipeline, _ := h.pipelineForSession(w, r)
	if pipeline == nil {
		return
	}
	id := pipeline.ID

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
		if t, ok := metrics.LastBatchAt.Load().(time.Time); ok && !t.IsZero() {
			resp["last_batch_at"] = t.UTC().Format(time.RFC3339)
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

// ListRuns returns execution runs for a pipeline.
func (h *PipelinesHandler) ListRuns(w http.ResponseWriter, r *http.Request) {
	pipeline, _ := h.pipelineForSession(w, r)
	if pipeline == nil {
		return
	}
	id := pipeline.ID

	limit := 20
	offset := 0
	if v := r.URL.Query().Get("limit"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
			limit = min(parsed, maxPipelineRuns)
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
		writeError(w, http.StatusInternalServerError, "Failed to list runs")
		return
	}
	if runs == nil {
		runs = []database.PipelineRun{}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"runs": runs})
}

// Upper bounds for the runs and run-logs list endpoints.
const (
	maxPipelineRuns    = 500
	maxPipelineRunLogs = 1000
)

// GetRunLogs returns logs for a specific pipeline run.
func (h *PipelinesHandler) GetRunLogs(w http.ResponseWriter, r *http.Request) {
	pipeline, _ := h.pipelineForSession(w, r)
	if pipeline == nil {
		return
	}
	runID := chi.URLParam(r, "runId")
	belongs, err := h.DB.PipelineRunBelongsTo(runID, pipeline.ID)
	if err != nil {
		slog.Error("Failed to look up pipeline run", "error", err, "run", runID)
		writeError(w, http.StatusInternalServerError, "Failed to get run logs")
		return
	}
	if !belongs {
		writeError(w, http.StatusNotFound, "Run not found")
		return
	}

	limit := 200
	if v := r.URL.Query().Get("limit"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
			limit = min(parsed, maxPipelineRunLogs)
		}
	}

	logs, err := h.DB.GetPipelineRunLogs(runID, limit)
	if err != nil {
		slog.Error("Failed to get run logs", "error", err, "run", runID)
		writeError(w, http.StatusInternalServerError, "Failed to get run logs")
		return
	}
	if logs == nil {
		logs = []database.PipelineRunLog{}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"logs": logs})
}

func (h *PipelinesHandler) secretKey() string {
	if h.Config == nil {
		return ""
	}
	return h.Config.AppSecretKey
}

// dataDir is CH-UI's data directory, which SQLite sources may not read.
func (h *PipelinesHandler) dataDir() string {
	if h.Config == nil || h.Config.DatabasePath == "" {
		return ""
	}
	return filepath.Dir(h.Config.DatabasePath)
}

// maskNodeConfig replaces secret values in a stored node config with the
// mask. A config that does not parse is returned empty rather than raw.
func maskNodeConfig(stored string) string {
	var cfg map[string]interface{}
	if err := json.Unmarshal([]byte(stored), &cfg); err != nil {
		return "{}"
	}
	out, err := json.Marshal(pipelines.MaskConfigSecrets(cfg))
	if err != nil {
		return "{}"
	}
	return string(out)
}

// checkSQLiteSource allows a SQLite source only for admins and never inside
// CH-UI's data directory. It returns 0 when allowed, else an HTTP status and
// message.
func (h *PipelinesHandler) checkSQLiteSource(session *middleware.SessionInfo, cfg, prev map[string]interface{}) (int, string) {
	if session == nil || session.UserRole != "admin" {
		return http.StatusForbidden, "Only admins can configure a SQLite source"
	}
	connStr, _ := cfg["connection_string"].(string)
	if connStr == pipelines.SecretMask {
		stored, err := pipelines.DecryptConfigSecrets(prev, h.secretKey())
		if err != nil {
			return http.StatusBadRequest, "Stored connection string could not be read; enter it again"
		}
		connStr, _ = stored["connection_string"].(string)
	}
	if err := pipelines.CheckSQLitePath(connStr, h.dataDir()); err != nil {
		return http.StatusBadRequest, err.Error()
	}
	return 0, ""
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
