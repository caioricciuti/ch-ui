package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/models"
	"github.com/caioricciuti/ch-ui/internal/scheduler"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

// ModelsHandler handles model CRUD and execution.
type ModelsHandler struct {
	DB      *database.DB
	Gateway *tunnel.Gateway
	Config  *config.Config
	Runner  *models.Runner
}

// Routes returns a chi.Router with all model routes.
func (h *ModelsHandler) Routes() chi.Router {
	r := chi.NewRouter()

	r.Get("/", h.ListModels)
	r.Post("/", h.CreateModel)
	r.Get("/dag", h.GetDAG)
	r.Get("/validate", h.ValidateAll)
	r.Post("/run", h.RunAll)
	r.Get("/runs", h.ListRuns)
	r.Get("/runs/{runId}", h.GetRun)
	r.Get("/pipelines", h.ListPipelines)
	r.Post("/pipelines/{anchorId}/run", h.RunPipeline)
	r.Get("/schedules", h.ListSchedules)
	r.Get("/schedule/{anchorId}", h.GetSchedule)
	r.Put("/schedule/{anchorId}", h.UpsertSchedule)
	r.Delete("/schedule/{anchorId}", h.DeleteSchedule)

	r.Route("/{id}", func(r chi.Router) {
		r.Get("/", h.GetModel)
		r.Put("/", h.UpdateModel)
		r.Delete("/", h.DeleteModel)
		r.Post("/run", h.RunSingle)
	})

	return r
}

// ListModels returns all models for the current connection.
func (h *ModelsHandler) ListModels(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Not authenticated"})
		return
	}

	modelList, err := h.DB.GetModelsByConnection(session.ConnectionID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to list models"})
		return
	}
	if modelList == nil {
		modelList = []database.Model{}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"models": modelList})
}

// CreateModel creates a new model.
func (h *ModelsHandler) CreateModel(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Not authenticated"})
		return
	}

	var body struct {
		Name            string `json:"name"`
		Description     string `json:"description"`
		TargetDatabase  string `json:"target_database"`
		Materialization string `json:"materialization"`
		SQLBody         string `json:"sql_body"`
		TableEngine     string `json:"table_engine"`
		OrderBy         string `json:"order_by"`
	}

	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
		return
	}

	if err := models.ValidateModelName(body.Name); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	if body.TargetDatabase == "" {
		body.TargetDatabase = "default"
	}
	if body.Materialization == "" {
		body.Materialization = "view"
	}
	if body.Materialization != "view" && body.Materialization != "table" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "materialization must be 'view' or 'table'"})
		return
	}
	if body.Materialization == "table" {
		if body.TableEngine == "" {
			body.TableEngine = "MergeTree"
		}
		if body.OrderBy == "" {
			body.OrderBy = "tuple()"
		}
	}

	id, err := h.DB.CreateModel(
		session.ConnectionID, body.Name, body.Description,
		body.TargetDatabase, body.Materialization, body.SQLBody,
		body.TableEngine, body.OrderBy, session.ClickhouseUser,
	)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("Failed to create model: %v", err)})
		return
	}

	model, _ := h.DB.GetModelByID(id)
	writeJSON(w, http.StatusCreated, map[string]interface{}{"model": model})
}

// GetModel returns a single model.
func (h *ModelsHandler) GetModel(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	model, err := h.DB.GetModelByID(id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to get model"})
		return
	}
	if model == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Model not found"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"model": model})
}

// UpdateModel updates an existing model.
func (h *ModelsHandler) UpdateModel(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	existing, err := h.DB.GetModelByID(id)
	if err != nil || existing == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Model not found"})
		return
	}

	var body struct {
		Name            string `json:"name"`
		Description     string `json:"description"`
		TargetDatabase  string `json:"target_database"`
		Materialization string `json:"materialization"`
		SQLBody         string `json:"sql_body"`
		TableEngine     string `json:"table_engine"`
		OrderBy         string `json:"order_by"`
	}

	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
		return
	}

	if body.Name != "" {
		if err := models.ValidateModelName(body.Name); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
	} else {
		body.Name = existing.Name
	}

	if body.TargetDatabase == "" {
		body.TargetDatabase = existing.TargetDatabase
	}
	if body.Materialization == "" {
		body.Materialization = existing.Materialization
	}
	if body.Materialization != "view" && body.Materialization != "table" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "materialization must be 'view' or 'table'"})
		return
	}
	if body.TableEngine == "" {
		body.TableEngine = existing.TableEngine
	}
	if body.OrderBy == "" {
		body.OrderBy = existing.OrderBy
	}
	if body.SQLBody == "" {
		body.SQLBody = existing.SQLBody
	}

	if err := h.DB.UpdateModel(id, body.Name, body.Description, body.TargetDatabase,
		body.Materialization, body.SQLBody, body.TableEngine, body.OrderBy); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("Failed to update model: %v", err)})
		return
	}

	model, _ := h.DB.GetModelByID(id)
	writeJSON(w, http.StatusOK, map[string]interface{}{"model": model})
}

// DeleteModel removes a model.
func (h *ModelsHandler) DeleteModel(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.DB.DeleteModel(id); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to delete model"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// GetDAG returns the dependency graph for XyFlow visualization.
func (h *ModelsHandler) GetDAG(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Not authenticated"})
		return
	}

	allModels, err := h.DB.GetModelsByConnection(session.ConnectionID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to load models"})
		return
	}

	if len(allModels) == 0 {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"nodes": []interface{}{},
			"edges": []interface{}{},
		})
		return
	}

	// Build DAG for layout computation
	nameToID := make(map[string]string)
	var modelIDs []string
	refsByID := make(map[string][]string)
	idToModel := make(map[string]database.Model)

	for _, m := range allModels {
		nameToID[m.Name] = m.ID
		idToModel[m.ID] = m
		modelIDs = append(modelIDs, m.ID)
		refsByID[m.ID] = models.ExtractRefs(m.SQLBody)
	}

	dag, dagErr := models.BuildDAG(modelIDs, refsByID, nameToID)

	// Compute depth for layout
	depth := make(map[string]int)
	if dagErr == nil {
		for _, id := range dag.Order {
			d := 0
			for _, depID := range dag.Deps[id] {
				if depth[depID] >= d {
					d = depth[depID] + 1
				}
			}
			depth[id] = d
		}
	}

	// Group by depth for y positioning
	layers := make(map[int]int) // depth -> count at that depth

	type dagNode struct {
		ID       string      `json:"id"`
		Data     interface{} `json:"data"`
		Position struct {
			X float64 `json:"x"`
			Y float64 `json:"y"`
		} `json:"position"`
	}
	type dagEdge struct {
		ID     string `json:"id"`
		Source string `json:"source"`
		Target string `json:"target"`
	}

	var nodes []dagNode
	var edges []dagEdge

	for _, m := range allModels {
		d := depth[m.ID]
		idx := layers[d]
		layers[d]++

		n := dagNode{
			ID: m.ID,
			Data: map[string]interface{}{
				"name":            m.Name,
				"materialization": m.Materialization,
				"status":          m.Status,
				"target_database": m.TargetDatabase,
			},
		}
		n.Position.X = float64(d) * 300
		n.Position.Y = float64(idx) * 120

		nodes = append(nodes, n)
	}

	// Build edges from refs
	for _, m := range allModels {
		refs := models.ExtractRefs(m.SQLBody)
		for _, ref := range refs {
			if srcID, ok := nameToID[ref]; ok {
				edges = append(edges, dagEdge{
					ID:     fmt.Sprintf("e-%s-%s", srcID, m.ID),
					Source: srcID,
					Target: m.ID,
				})
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"nodes": nodes,
		"edges": edges,
	})
}

// ValidateAll checks all models for reference errors and cycles.
func (h *ModelsHandler) ValidateAll(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Not authenticated"})
		return
	}

	errors, err := h.Runner.Validate(session.ConnectionID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("Validation failed: %v", err)})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"valid":  len(errors) == 0,
		"errors": errors,
	})
}

// RunAll triggers execution of all models.
func (h *ModelsHandler) RunAll(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Not authenticated"})
		return
	}

	runID, err := h.Runner.RunAll(session.ConnectionID, session.ClickhouseUser)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"run_id": runID})
}

// RunSingle triggers execution of a single model and its deps.
func (h *ModelsHandler) RunSingle(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Not authenticated"})
		return
	}

	id := chi.URLParam(r, "id")
	runID, err := h.Runner.RunSingle(session.ConnectionID, id, session.ClickhouseUser)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"run_id": runID})
}

// ListRuns returns recent model runs.
func (h *ModelsHandler) ListRuns(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Not authenticated"})
		return
	}

	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 20
	}
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))

	runs, err := h.DB.GetModelRuns(session.ConnectionID, limit, offset)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to list runs"})
		return
	}
	if runs == nil {
		runs = []database.ModelRun{}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"runs": runs})
}

// GetRun returns a single run with per-model results.
func (h *ModelsHandler) GetRun(w http.ResponseWriter, r *http.Request) {
	runID := chi.URLParam(r, "runId")

	run, err := h.DB.GetModelRunByID(runID)
	if err != nil || run == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Run not found"})
		return
	}

	results, err := h.DB.GetModelRunResults(runID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to load run results"})
		return
	}
	if results == nil {
		results = []database.ModelRunResult{}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"run":     run,
		"results": results,
	})
}

// ── Pipeline endpoints ──────────────────────────────────────────────

// ListPipelines returns connected components with their schedules.
func (h *ModelsHandler) ListPipelines(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Not authenticated"})
		return
	}

	allModels, err := h.DB.GetModelsByConnection(session.ConnectionID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to load models"})
		return
	}

	if len(allModels) == 0 {
		writeJSON(w, http.StatusOK, map[string]interface{}{"pipelines": []interface{}{}})
		return
	}

	nameToID := make(map[string]string)
	var modelIDs []string
	refsByID := make(map[string][]string)

	for _, m := range allModels {
		nameToID[m.Name] = m.ID
		modelIDs = append(modelIDs, m.ID)
		refsByID[m.ID] = models.ExtractRefs(m.SQLBody)
	}

	dag, dagErr := models.BuildDAG(modelIDs, refsByID, nameToID)
	if dagErr != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("DAG error: %v", dagErr)})
		return
	}

	components := dag.ConnectedComponents()

	// Load all schedules for this connection
	schedules, err := h.DB.GetModelSchedulesByConnection(session.ConnectionID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to load schedules"})
		return
	}
	schedByAnchor := make(map[string]database.ModelSchedule)
	for _, s := range schedules {
		if s.AnchorModelID != nil {
			schedByAnchor[*s.AnchorModelID] = s
		}
	}

	type pipelineResp struct {
		AnchorModelID string                  `json:"anchor_model_id"`
		ModelIDs      []string                `json:"model_ids"`
		Schedule      *database.ModelSchedule `json:"schedule"`
	}

	var pipelines []pipelineResp
	for _, comp := range components {
		if len(comp) == 0 {
			continue
		}
		anchor := comp[0] // first in topo order
		p := pipelineResp{
			AnchorModelID: anchor,
			ModelIDs:      comp,
		}
		if s, ok := schedByAnchor[anchor]; ok {
			p.Schedule = &s
		} else {
			// Check if any model in this component has a schedule
			for _, id := range comp {
				if s, ok := schedByAnchor[id]; ok {
					p.Schedule = &s
					break
				}
			}
		}
		pipelines = append(pipelines, p)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"pipelines": pipelines})
}

// RunPipeline triggers execution of a single pipeline (connected component).
func (h *ModelsHandler) RunPipeline(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Not authenticated"})
		return
	}

	anchorID := chi.URLParam(r, "anchorId")
	runID, err := h.Runner.RunPipeline(session.ConnectionID, anchorID, session.ClickhouseUser)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"run_id": runID})
}

// ── Schedule endpoints ──────────────────────────────────────────────

// ListSchedules returns all schedules for the current connection.
func (h *ModelsHandler) ListSchedules(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Not authenticated"})
		return
	}

	schedules, err := h.DB.GetModelSchedulesByConnection(session.ConnectionID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to list schedules"})
		return
	}
	if schedules == nil {
		schedules = []database.ModelSchedule{}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"schedules": schedules})
}

// GetSchedule returns the schedule for a specific pipeline anchor.
func (h *ModelsHandler) GetSchedule(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Not authenticated"})
		return
	}

	anchorID := chi.URLParam(r, "anchorId")
	sched, err := h.DB.GetModelScheduleByAnchor(session.ConnectionID, anchorID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to get schedule"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"schedule": sched})
}

// UpsertSchedule creates or updates the schedule for a specific pipeline anchor.
func (h *ModelsHandler) UpsertSchedule(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Not authenticated"})
		return
	}

	anchorID := chi.URLParam(r, "anchorId")

	var body struct {
		Cron    string `json:"cron"`
		Enabled bool   `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
		return
	}

	if body.Cron == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "cron expression is required"})
		return
	}
	if !scheduler.ValidateCron(body.Cron) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid cron expression"})
		return
	}

	var nextRunAt string
	if next := scheduler.ComputeNextRun(body.Cron, time.Now().UTC()); next != nil {
		nextRunAt = next.Format(time.RFC3339)
	}

	_, err := h.DB.UpsertModelSchedule(session.ConnectionID, anchorID, body.Cron, nextRunAt, session.ClickhouseUser)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("Failed to save schedule: %v", err)})
		return
	}

	sched, _ := h.DB.GetModelScheduleByAnchor(session.ConnectionID, anchorID)
	writeJSON(w, http.StatusOK, map[string]interface{}{"schedule": sched})
}

// DeleteSchedule removes the schedule for a specific pipeline anchor.
func (h *ModelsHandler) DeleteSchedule(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Not authenticated"})
		return
	}

	anchorID := chi.URLParam(r, "anchorId")
	if err := h.DB.DeleteModelScheduleByAnchor(session.ConnectionID, anchorID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to delete schedule"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
