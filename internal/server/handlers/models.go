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

	// Writes and runs (which materialize tables) require admin/analyst; viewers
	// are read-only.
	writer := middleware.RequireWriter()

	r.Get("/", h.ListModels)
	r.With(writer).Post("/", h.CreateModel)
	r.Get("/dag", h.GetDAG)
	r.Get("/validate", h.ValidateAll)
	r.With(writer).Post("/run", h.RunAll)
	r.Get("/runs", h.ListRuns)
	r.Get("/runs/{runId}", h.GetRun)
	r.Get("/pipelines", h.ListPipelines)
	r.With(writer).Post("/pipelines/{anchorId}/run", h.RunPipeline)
	r.Get("/schedules", h.ListSchedules)
	r.Get("/schedule/{anchorId}", h.GetSchedule)
	r.With(writer).Put("/schedule/{anchorId}", h.UpsertSchedule)
	r.With(writer).Delete("/schedule/{anchorId}", h.DeleteSchedule)

	r.Route("/{id}", func(r chi.Router) {
		r.Get("/", h.GetModel)
		r.With(writer).Put("/", h.UpdateModel)
		r.With(writer).Delete("/", h.DeleteModel)
		r.With(writer).Post("/run", h.RunSingle)
	})

	return r
}

// ListModels returns all models for the current connection.
func (h *ModelsHandler) ListModels(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}

	modelList, err := h.DB.GetModelsByConnection(session.ConnectionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to list models")
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
		writeError(w, http.StatusUnauthorized, "Not authenticated")
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
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if err := models.ValidateModelName(body.Name); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if body.TargetDatabase == "" {
		body.TargetDatabase = "default"
	}
	if body.Materialization == "" {
		body.Materialization = "view"
	}
	if body.Materialization != "view" && body.Materialization != "table" {
		writeError(w, http.StatusBadRequest, "materialization must be 'view' or 'table'")
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
		body.TableEngine, body.OrderBy, middleware.Actor(session),
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to create model: %v", err))
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
		writeError(w, http.StatusInternalServerError, "Failed to get model")
		return
	}
	if model == nil {
		writeError(w, http.StatusNotFound, "Model not found")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"model": model})
}

// UpdateModel updates an existing model.
func (h *ModelsHandler) UpdateModel(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	existing, err := h.DB.GetModelByID(id)
	if err != nil || existing == nil {
		writeError(w, http.StatusNotFound, "Model not found")
		return
	}
	if existing.Source == "github" {
		writeError(w, http.StatusForbidden, "This model is managed by GitHub — edit it in your repository and re-sync")
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
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if body.Name != "" {
		if err := models.ValidateModelName(body.Name); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
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
		writeError(w, http.StatusBadRequest, "materialization must be 'view' or 'table'")
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
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to update model: %v", err))
		return
	}

	model, _ := h.DB.GetModelByID(id)
	writeJSON(w, http.StatusOK, map[string]interface{}{"model": model})
}

// DeleteModel removes a model.
func (h *ModelsHandler) DeleteModel(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	existing, _ := h.DB.GetModelByID(id)
	if existing != nil && existing.Source == "github" {
		writeError(w, http.StatusForbidden, "This model is managed by GitHub — remove it from your repository and re-sync")
		return
	}
	if err := h.DB.DeleteModel(id); err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to delete model")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// GetDAG returns the dependency graph for XyFlow visualization.
func (h *ModelsHandler) GetDAG(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}

	allModels, err := h.DB.GetModelsByConnection(session.ConnectionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to load models")
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
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}

	errors, err := h.Runner.Validate(session.ConnectionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("Validation failed: %v", err))
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
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}

	runID, err := h.Runner.RunAll(session.ConnectionID, middleware.Actor(session))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"run_id": runID})
}

// RunSingle triggers execution of a single model and its deps.
func (h *ModelsHandler) RunSingle(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}

	id := chi.URLParam(r, "id")
	runID, err := h.Runner.RunSingle(session.ConnectionID, id, middleware.Actor(session))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"run_id": runID})
}

// ListRuns returns recent model runs.
func (h *ModelsHandler) ListRuns(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}

	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 20
	}
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))

	runs, err := h.DB.GetModelRuns(session.ConnectionID, limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to list runs")
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
		writeError(w, http.StatusNotFound, "Run not found")
		return
	}

	results, err := h.DB.GetModelRunResults(runID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to load run results")
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
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}

	allModels, err := h.DB.GetModelsByConnection(session.ConnectionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to load models")
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
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("DAG error: %v", dagErr))
		return
	}

	components := dag.ConnectedComponents()

	// Load all schedules for this connection
	schedules, err := h.DB.GetModelSchedulesByConnection(session.ConnectionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to load schedules")
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
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}

	anchorID := chi.URLParam(r, "anchorId")
	runID, err := h.Runner.RunPipeline(session.ConnectionID, anchorID, middleware.Actor(session))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"run_id": runID})
}

// ── Schedule endpoints ──────────────────────────────────────────────

// ListSchedules returns all schedules for the current connection.
func (h *ModelsHandler) ListSchedules(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}

	schedules, err := h.DB.GetModelSchedulesByConnection(session.ConnectionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to list schedules")
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
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}

	anchorID := chi.URLParam(r, "anchorId")
	sched, err := h.DB.GetModelScheduleByAnchor(session.ConnectionID, anchorID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to get schedule")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"schedule": sched})
}

// UpsertSchedule creates or updates the schedule for a specific pipeline anchor.
func (h *ModelsHandler) UpsertSchedule(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}

	anchorID := chi.URLParam(r, "anchorId")

	var body struct {
		Cron    string `json:"cron"`
		Enabled bool   `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if body.Cron == "" {
		writeError(w, http.StatusBadRequest, "cron expression is required")
		return
	}
	if !scheduler.ValidateCron(body.Cron) {
		writeError(w, http.StatusBadRequest, "invalid cron expression")
		return
	}

	var nextRunAt string
	if next := scheduler.ComputeNextRun(body.Cron, time.Now().UTC()); next != nil {
		nextRunAt = next.Format(time.RFC3339)
	}

	_, err := h.DB.UpsertModelSchedule(session.ConnectionID, anchorID, body.Cron, nextRunAt, middleware.Actor(session))
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to save schedule: %v", err))
		return
	}

	sched, _ := h.DB.GetModelScheduleByAnchor(session.ConnectionID, anchorID)
	writeJSON(w, http.StatusOK, map[string]interface{}{"schedule": sched})
}

// DeleteSchedule removes the schedule for a specific pipeline anchor.
func (h *ModelsHandler) DeleteSchedule(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}

	anchorID := chi.URLParam(r, "anchorId")
	if err := h.DB.DeleteModelScheduleByAnchor(session.ConnectionID, anchorID); err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to delete schedule")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
