package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/models"
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
	// are read-only. Preview only reads, with the caller's own credentials.
	writer := middleware.RequireWriter()

	r.Get("/", h.ListModels)
	r.With(writer).Post("/", h.CreateModel)
	r.Get("/dag", h.GetDAG)
	r.Get("/validate", h.ValidateAll)
	r.With(writer).Post("/run", h.RunAll)
	r.Post("/preview", h.Preview)
	r.Get("/runs", h.ListRuns)
	r.Get("/runs/{runId}", h.GetRun)
	r.Get("/schedule-preview", h.SchedulePreview)
	r.Get("/pipelines", h.ListPipelines)
	r.With(writer).Post("/pipelines", h.CreatePipeline)
	r.With(writer).Put("/pipelines/{pipelineId}", h.UpdatePipeline)
	r.With(writer).Delete("/pipelines/{pipelineId}", h.DeletePipeline)
	r.With(writer).Post("/pipelines/{pipelineId}/run", h.RunPipeline)

	r.Route("/{id}", func(r chi.Router) {
		r.Get("/", h.GetModel)
		r.With(writer).Put("/", h.UpdateModel)
		r.With(writer).Delete("/", h.DeleteModel)
		r.With(writer).Post("/run", h.RunSingle)
		r.Post("/compile", h.CompileModel)
		r.Get("/tests", h.ListTests)
		r.With(writer).Post("/tests", h.CreateTest)
		r.With(writer).Put("/tests/{testId}", h.UpdateTest)
		r.With(writer).Delete("/tests/{testId}", h.DeleteTest)
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

// modelBody is the editable part of a model. Option fields are pointers: on
// update, a field that is left out keeps its current value.
type modelBody struct {
	Name                string  `json:"name"`
	Description         string  `json:"description"`
	TargetDatabase      string  `json:"target_database"`
	Materialization     string  `json:"materialization"`
	SQLBody             string  `json:"sql_body"`
	TableEngine         string  `json:"table_engine"`
	OrderBy             string  `json:"order_by"`
	PartitionBy         *string `json:"partition_by"`
	EngineArgs          *string `json:"engine_args"`
	IncrementalStrategy *string `json:"incremental_strategy"`
	UniqueKey           *string `json:"unique_key"`
	WatermarkColumn     *string `json:"watermark_column"`
}

// applyOptions copies the option fields that were sent onto m.
func (b modelBody) applyOptions(m *database.Model) {
	set := func(dst *string, src *string) {
		if src != nil {
			*dst = *src
		}
	}
	set(&m.PartitionBy, b.PartitionBy)
	set(&m.EngineArgs, b.EngineArgs)
	set(&m.IncrementalStrategy, b.IncrementalStrategy)
	set(&m.UniqueKey, b.UniqueKey)
	set(&m.WatermarkColumn, b.WatermarkColumn)
}

// CreateModel creates a new model.
func (h *ModelsHandler) CreateModel(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}

	var body modelBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if err := models.ValidateModelName(body.Name); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	m := database.Model{
		Name: body.Name, Description: body.Description, TargetDatabase: body.TargetDatabase,
		Materialization: body.Materialization, SQLBody: body.SQLBody,
		TableEngine: body.TableEngine, OrderBy: body.OrderBy,
	}
	if m.TargetDatabase == "" {
		m.TargetDatabase = "default"
	}
	body.applyOptions(&m)
	if err := models.NormalizeModel(&m); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	actor := middleware.Actor(session)
	m.ConnectionID, m.CreatedBy = session.ConnectionID, &actor
	id, err := h.DB.SaveModel(database.ModelSave{Model: m})
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to create model: %v", err))
		return
	}

	model, _ := h.DB.GetModelByID(id)
	writeJSON(w, http.StatusCreated, map[string]interface{}{"model": model})
}

// modelForSession loads the model named in the URL when it belongs to the
// caller's connection. Models run with their connection's credentials, so one
// on another connection is reported as not found. On failure it writes the
// response and returns nil.
func (h *ModelsHandler) modelForSession(w http.ResponseWriter, r *http.Request, param string) *database.Model {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return nil
	}
	model, err := h.DB.GetModelByID(chi.URLParam(r, param))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to get model")
		return nil
	}
	if model == nil || model.ConnectionID != session.ConnectionID {
		writeError(w, http.StatusNotFound, "Model not found")
		return nil
	}
	return model
}

// GetModel returns a single model.
func (h *ModelsHandler) GetModel(w http.ResponseWriter, r *http.Request) {
	model := h.modelForSession(w, r, "id")
	if model == nil {
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"model": model})
}

// UpdateModel updates an existing model.
func (h *ModelsHandler) UpdateModel(w http.ResponseWriter, r *http.Request) {
	existing := h.modelForSession(w, r, "id")
	if existing == nil {
		return
	}
	if existing.Source == "github" {
		writeError(w, http.StatusForbidden, "This model is managed by GitHub. Edit it in your repository and sync again.")
		return
	}

	var body modelBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	m := *existing
	if body.Name != "" {
		if err := models.ValidateModelName(body.Name); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		m.Name = body.Name
	}
	m.Description = body.Description
	if body.TargetDatabase != "" {
		m.TargetDatabase = body.TargetDatabase
	}
	if body.Materialization != "" {
		m.Materialization = body.Materialization
	}
	if body.TableEngine != "" {
		m.TableEngine = body.TableEngine
	}
	if body.OrderBy != "" {
		m.OrderBy = body.OrderBy
	}
	if body.SQLBody != "" {
		m.SQLBody = body.SQLBody
	}
	body.applyOptions(&m)
	if err := models.NormalizeModel(&m); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// A rename rewrites the pipeline selectors naming the old name in the
	// same transaction, so scheduled pipelines keep working.
	if _, err := h.DB.SaveModel(database.ModelSave{Model: m, RenameInSelector: models.RenameInSelector}); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to update model: %v", err))
		return
	}

	model, _ := h.DB.GetModelByID(m.ID)
	writeJSON(w, http.StatusOK, map[string]interface{}{"model": model})
}

// DeleteModel removes a model.
func (h *ModelsHandler) DeleteModel(w http.ResponseWriter, r *http.Request) {
	existing := h.modelForSession(w, r, "id")
	if existing == nil {
		return
	}
	if existing.Source == "github" {
		writeError(w, http.StatusForbidden, "This model is managed by GitHub. Remove it from your repository and sync again.")
		return
	}
	if err := h.DB.DeleteModel(existing.ID); err != nil {
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

// decodeOptional decodes a JSON body when there is one; an empty body is fine.
func decodeOptional(r *http.Request, v interface{}) error {
	if r.Body == nil || r.ContentLength == 0 {
		return nil
	}
	if err := json.NewDecoder(r.Body).Decode(v); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

// RunAll runs every model, or the models a selector picks:
// {"selector": "+orders,customers+"}.
func (h *ModelsHandler) RunAll(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}
	var body struct {
		Selector string `json:"selector"`
	}
	if err := decodeOptional(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	runID, err := h.Runner.Run(session.ConnectionID, models.RunOptions{Selector: body.Selector, TriggeredBy: middleware.Actor(session)})
	writeRunOutcome(w, runID, err)
}

// writeRunOutcome answers a synchronous run. A run that finished with failed
// models is still a recorded run: it returns 200 with its status so the UI can
// show the results.
func writeRunOutcome(w http.ResponseWriter, runID string, err error) {
	var failed *models.RunFailedError
	var selErr *models.SelectorError
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, map[string]interface{}{"run_id": runID, "status": "success"})
	case errors.As(err, &failed):
		writeJSON(w, http.StatusOK, map[string]interface{}{"run_id": failed.RunID, "status": failed.Status, "error": failed.Error()})
	case errors.Is(err, models.ErrModelNotFound):
		writeError(w, http.StatusNotFound, "Model not found")
	case errors.Is(err, models.ErrPipelineNotFound):
		writeError(w, http.StatusNotFound, "Pipeline not found")
	case errors.As(err, &selErr):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		writeError(w, http.StatusBadRequest, err.Error())
	}
}

// RunSingle runs a model with its upstream models (default), its downstream
// models, or alone: {"scope": "upstream" | "downstream" | "only",
// "full_refresh": true}. full_refresh rebuilds this model from scratch.
func (h *ModelsHandler) RunSingle(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}

	model := h.modelForSession(w, r, "id")
	if model == nil {
		return
	}
	var body struct {
		Scope       string `json:"scope"`
		FullRefresh bool   `json:"full_refresh"`
	}
	if err := decodeOptional(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	runID, err := h.Runner.RunModel(session.ConnectionID, model.ID, body.Scope, body.FullRefresh, middleware.Actor(session))
	writeRunOutcome(w, runID, err)
}

// maxModelRunsLimit caps one page of model runs; the UI asks for 20.
const maxModelRunsLimit = 500

// modelRunsPage reads limit (default 20, max maxModelRunsLimit) and offset.
func modelRunsPage(q url.Values) (limit, offset int) {
	limit, _ = strconv.Atoi(q.Get("limit"))
	if limit <= 0 {
		limit = 20
	}
	if limit > maxModelRunsLimit {
		limit = maxModelRunsLimit
	}
	offset, _ = strconv.Atoi(q.Get("offset"))
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

// ListRuns returns recent model runs.
func (h *ModelsHandler) ListRuns(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}

	limit, offset := modelRunsPage(r.URL.Query())

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
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}
	runID := chi.URLParam(r, "runId")

	run, err := h.DB.GetModelRunByID(runID)
	if err != nil || run == nil || run.ConnID != session.ConnectionID {
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
