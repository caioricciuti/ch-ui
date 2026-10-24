package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/caioricciuti/ch-ui/internal/cronexpr"
	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/models"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
)

// previewRowLimit caps a model preview.
const previewRowLimit = 100

// modelTargetsFor maps every model name of the connection to its database.
func (h *ModelsHandler) modelTargetsFor(connectionID string) (map[string]string, []database.Model, error) {
	all, err := h.DB.GetModelsByConnection(connectionID)
	if err != nil {
		return nil, nil, err
	}
	targets := make(map[string]string, len(all))
	for _, m := range all {
		targets[m.Name] = m.TargetDatabase
	}
	return targets, all, nil
}

// ── Compile and preview ─────────────────────────────────────────────

// CompileModel returns the model's resolved SQL and the statements a run
// would execute. An optional body {"sql_body": "..."} compiles unsaved SQL
// with the model's saved settings.
func (h *ModelsHandler) CompileModel(w http.ResponseWriter, r *http.Request) {
	model := h.modelForSession(w, r, "id")
	if model == nil {
		return
	}
	var body struct {
		SQLBody *string `json:"sql_body"`
	}
	if err := decodeOptional(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	m := *model
	if body.SQLBody != nil {
		m.SQLBody = *body.SQLBody
	}
	targets, _, err := h.modelTargetsFor(m.ConnectionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to load models")
		return
	}
	resolved, steps, err := models.Compile(m, targets)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"resolved_sql": resolved, "statements": []models.PlanStep{}, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"resolved_sql": resolved, "statements": steps})
}

// Preview runs SELECT * FROM (<resolved sql>) LIMIT 100 with the caller's
// own ClickHouse credentials and readonly=1. Body: {"sql_body": "...",
// "model_id": "..."}; model_id (optional) lets $this resolve.
func (h *ModelsHandler) Preview(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}
	var body struct {
		SQLBody string `json:"sql_body"`
		ModelID string `json:"model_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if strings.TrimSpace(body.SQLBody) == "" {
		writeError(w, http.StatusBadRequest, "sql_body is required")
		return
	}
	targets, _, err := h.modelTargetsFor(session.ConnectionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to load models")
		return
	}
	var resolved string
	if body.ModelID != "" {
		m, err := h.DB.GetModelByID(body.ModelID)
		if err != nil || m == nil || m.ConnectionID != session.ConnectionID {
			writeError(w, http.StatusNotFound, "Model not found")
			return
		}
		resolved, err = models.ResolveModelSQL(body.SQLBody, targets, m.TargetDatabase, m.Name)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	} else {
		if models.UsesThis(body.SQLBody) {
			writeError(w, http.StatusBadRequest, "$this needs a saved model (send model_id)")
			return
		}
		resolved, err = models.ResolveRefs(body.SQLBody, targets)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if h.Gateway == nil || !h.Gateway.IsTunnelOnline(session.ConnectionID) {
		writeError(w, http.StatusServiceUnavailable, "Tunnel not connected")
		return
	}
	secret := ""
	if h.Config != nil {
		secret = h.Config.AppSecretKey
	}
	password, err := crypto.Decrypt(session.EncryptedPassword, secret)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to decrypt credentials")
		return
	}
	sql := models.PreviewSQL(resolved, previewRowLimit)
	res, err := h.Gateway.ExecuteQueryWithSettingsCtx(r.Context(), session.ConnectionID, sql, session.ClickhouseUser, password, map[string]string{
		"readonly":                                "1",
		"max_execution_time":                      "30",
		"max_result_rows":                         fmt.Sprint(previewRowLimit),
		"result_overflow_mode":                    "break",
		"output_format_json_quote_64bit_integers": "0",
	}, 35*time.Second)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"sql": sql, "error": err.Error()})
		return
	}
	meta := json.RawMessage(`[]`)
	data := json.RawMessage(`[]`)
	if res != nil && len(res.Meta) > 0 {
		meta = res.Meta
	}
	if res != nil && len(res.Data) > 0 {
		data = res.Data
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"sql": sql, "meta": meta, "data": data})
}

// ── Data tests ──────────────────────────────────────────────────────

// ListTests returns a model's data tests.
func (h *ModelsHandler) ListTests(w http.ResponseWriter, r *http.Request) {
	model := h.modelForSession(w, r, "id")
	if model == nil {
		return
	}
	tests, err := h.DB.GetModelTests(model.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to list tests")
		return
	}
	if tests == nil {
		tests = []database.ModelTest{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"tests": tests})
}

type testBody struct {
	Kind       string          `json:"kind"`
	ColumnName string          `json:"column_name"`
	Args       json.RawMessage `json:"args"`
	Severity   string          `json:"severity"`
}

// editableModel is modelForSession for test writes: GitHub-managed models
// get their tests from the repository.
func (h *ModelsHandler) editableModel(w http.ResponseWriter, r *http.Request) *database.Model {
	model := h.modelForSession(w, r, "id")
	if model == nil {
		return nil
	}
	if model.Source == "github" {
		writeError(w, http.StatusForbidden, "This model is managed by GitHub. Edit its tests in your repository and sync again.")
		return nil
	}
	return model
}

func (h *ModelsHandler) decodeTest(w http.ResponseWriter, r *http.Request, t *database.ModelTest) bool {
	var body testBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return false
	}
	t.Kind, t.ColumnName, t.Severity = body.Kind, body.ColumnName, body.Severity
	t.Args = string(body.Args)
	if err := models.NormalizeTest(t); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return false
	}
	return true
}

// CreateTest adds a data test to a model.
func (h *ModelsHandler) CreateTest(w http.ResponseWriter, r *http.Request) {
	model := h.editableModel(w, r)
	if model == nil {
		return
	}
	t := database.ModelTest{ModelID: model.ID}
	if !h.decodeTest(w, r, &t) {
		return
	}
	id, err := h.DB.CreateModelTest(t)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to create test")
		return
	}
	created, _ := h.DB.GetModelTestByID(id)
	writeJSON(w, http.StatusCreated, map[string]interface{}{"test": created})
}

// testForModel loads the test named in the URL when it belongs to model.
func (h *ModelsHandler) testForModel(w http.ResponseWriter, r *http.Request, model *database.Model) *database.ModelTest {
	t, err := h.DB.GetModelTestByID(chi.URLParam(r, "testId"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to load test")
		return nil
	}
	if t == nil || t.ModelID != model.ID {
		writeError(w, http.StatusNotFound, "Test not found")
		return nil
	}
	return t
}

// UpdateTest replaces a data test's definition.
func (h *ModelsHandler) UpdateTest(w http.ResponseWriter, r *http.Request) {
	model := h.editableModel(w, r)
	if model == nil {
		return
	}
	t := h.testForModel(w, r, model)
	if t == nil {
		return
	}
	if !h.decodeTest(w, r, t) {
		return
	}
	if err := h.DB.UpdateModelTest(*t); err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to update test")
		return
	}
	updated, _ := h.DB.GetModelTestByID(t.ID)
	writeJSON(w, http.StatusOK, map[string]interface{}{"test": updated})
}

// DeleteTest removes a data test.
func (h *ModelsHandler) DeleteTest(w http.ResponseWriter, r *http.Request) {
	model := h.editableModel(w, r)
	if model == nil {
		return
	}
	t := h.testForModel(w, r, model)
	if t == nil {
		return
	}
	if err := h.DB.DeleteModelTest(t.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to delete test")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// ── Named pipelines ─────────────────────────────────────────────────

type pipelineResp struct {
	database.ModelPipeline
	// ModelIDs are the models the selector picks today, in run order.
	ModelIDs []string `json:"model_ids"`
	// NextRuns are the next three scheduled times (UTC), when scheduled.
	NextRuns []string `json:"next_runs"`
	// SelectorError is set when the selector no longer matches (a model it
	// names was renamed or deleted).
	SelectorError string `json:"selector_error,omitempty"`
}

func formatTimes(ts []time.Time) []string {
	out := make([]string, len(ts))
	for i, t := range ts {
		out[i] = t.UTC().Format(time.RFC3339)
	}
	return out
}

// ListPipelines returns the connection's named pipelines with the models
// each one picks, plus the models no pipeline picks.
func (h *ModelsHandler) ListPipelines(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}
	pipelines, err := h.DB.GetModelPipelines(session.ConnectionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to load model groups")
		return
	}
	_, all, err := h.modelTargetsFor(session.ConnectionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to load models")
		return
	}
	nameToID := make(map[string]string, len(all))
	refs := make(map[string][]string, len(all))
	ids := make([]string, 0, len(all))
	for _, m := range all {
		nameToID[m.Name] = m.ID
		refs[m.ID] = models.ExtractRefs(m.SQLBody)
		ids = append(ids, m.ID)
	}
	dag, dagErr := models.BuildDAG(ids, refs, nameToID)

	grouped := make(map[string]bool)
	out := make([]pipelineResp, 0, len(pipelines))
	for _, p := range pipelines {
		resp := pipelineResp{ModelPipeline: p, ModelIDs: []string{}, NextRuns: []string{}}
		switch {
		case dagErr != nil:
			resp.SelectorError = dagErr.Error()
		default:
			picked, err := dag.Select(p.Selector, nameToID)
			if err != nil {
				resp.SelectorError = err.Error()
			} else {
				resp.ModelIDs = picked
				for _, id := range picked {
					grouped[id] = true
				}
			}
		}
		if p.Cron != "" && p.Enabled {
			resp.NextRuns = formatTimes(models.NextRuns(p.Cron, p.Timezone, time.Now(), 3))
		}
		out = append(out, resp)
	}
	ungrouped := []string{}
	for _, m := range all {
		if !grouped[m.ID] {
			ungrouped = append(ungrouped, m.ID)
		}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"pipelines": out, "ungrouped_model_ids": ungrouped})
}

// SchedulePreview returns the next three run times for ?cron=&timezone=.
func (h *ModelsHandler) SchedulePreview(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	cron, tz := strings.TrimSpace(q.Get("cron")), strings.TrimSpace(q.Get("timezone"))
	if _, err := models.LoadTimezone(tz); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !cronexpr.ValidateCron(cron) {
		writeError(w, http.StatusBadRequest, "invalid cron expression")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"next_runs": formatTimes(models.NextRuns(cron, tz, time.Now(), 3))})
}

type pipelineBody struct {
	Name     string `json:"name"`
	Selector string `json:"selector"`
	Cron     string `json:"cron"`
	// Enabled pauses (false) or resumes (true) the schedule. Omitted, a new
	// pipeline with a cron starts enabled and an existing one keeps its state.
	Enabled  *bool  `json:"enabled"`
	Timezone string `json:"timezone"`
}

// applyPipelineBody validates body against the connection's models and
// copies it onto p. On failure it writes the response and returns false.
func (h *ModelsHandler) applyPipelineBody(w http.ResponseWriter, connectionID string, body pipelineBody, p *database.ModelPipeline) bool {
	body.Name = strings.TrimSpace(body.Name)
	body.Selector = strings.TrimSpace(body.Selector)
	body.Cron = strings.TrimSpace(body.Cron)
	body.Timezone = strings.TrimSpace(body.Timezone)
	if body.Name == "" || len(body.Name) > 100 {
		writeError(w, http.StatusBadRequest, "name is required (at most 100 characters)")
		return false
	}
	_, all, err := h.modelTargetsFor(connectionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to load models")
		return false
	}
	names := make(map[string]bool, len(all))
	for _, m := range all {
		names[m.Name] = true
	}
	if err := models.ValidateSelector(body.Selector, names); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return false
	}
	if body.Timezone == "" {
		body.Timezone = "UTC"
	}
	if _, err := models.LoadTimezone(body.Timezone); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return false
	}
	if body.Cron != "" && !cronexpr.ValidateCron(body.Cron) {
		writeError(w, http.StatusBadRequest, "invalid cron expression")
		return false
	}
	p.Name, p.Selector, p.Cron, p.Timezone = body.Name, body.Selector, body.Cron, body.Timezone
	if body.Enabled != nil {
		p.Enabled = *body.Enabled
	} else if p.ID == "" {
		p.Enabled = body.Cron != ""
	}
	if p.Cron == "" {
		p.Enabled = false
	}
	p.NextRunAt = nil
	if next := models.NextRuns(p.Cron, p.Timezone, time.Now(), 1); len(next) > 0 {
		s := next[0].Format(time.RFC3339)
		p.NextRunAt = &s
	}
	return true
}

func pipelineSaveError(w http.ResponseWriter, err error) {
	if strings.Contains(err.Error(), "UNIQUE") {
		writeError(w, http.StatusConflict, "A model group with this name already exists")
		return
	}
	writeError(w, http.StatusInternalServerError, "Failed to save model group")
}

// CreatePipeline stores a named pipeline.
func (h *ModelsHandler) CreatePipeline(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}
	var body pipelineBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	actor := middleware.Actor(session)
	p := database.ModelPipeline{ConnectionID: session.ConnectionID, CreatedBy: &actor}
	if !h.applyPipelineBody(w, session.ConnectionID, body, &p) {
		return
	}
	id, err := h.DB.CreateModelPipeline(p)
	if err != nil {
		pipelineSaveError(w, err)
		return
	}
	created, _ := h.DB.GetModelPipelineByID(id)
	writeJSON(w, http.StatusCreated, map[string]interface{}{"pipeline": created})
}

// pipelineForSession loads the pipeline named in the URL when it belongs to
// the caller's connection.
func (h *ModelsHandler) pipelineForSession(w http.ResponseWriter, r *http.Request) *database.ModelPipeline {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return nil
	}
	p, err := h.DB.GetModelPipelineByID(chi.URLParam(r, "pipelineId"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to load model group")
		return nil
	}
	if p == nil || p.ConnectionID != session.ConnectionID {
		writeError(w, http.StatusNotFound, "Model group not found")
		return nil
	}
	return p
}

// UpdatePipeline saves a pipeline's name, selector and schedule.
func (h *ModelsHandler) UpdatePipeline(w http.ResponseWriter, r *http.Request) {
	p := h.pipelineForSession(w, r)
	if p == nil {
		return
	}
	var body pipelineBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if !h.applyPipelineBody(w, p.ConnectionID, body, p) {
		return
	}
	if err := h.DB.UpdateModelPipeline(*p); err != nil {
		pipelineSaveError(w, err)
		return
	}
	updated, _ := h.DB.GetModelPipelineByID(p.ID)
	writeJSON(w, http.StatusOK, map[string]interface{}{"pipeline": updated})
}

// DeletePipeline removes a pipeline (its models stay).
func (h *ModelsHandler) DeletePipeline(w http.ResponseWriter, r *http.Request) {
	p := h.pipelineForSession(w, r)
	if p == nil {
		return
	}
	if err := h.DB.DeleteModelPipeline(p.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to delete model group")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// RunPipeline runs a named pipeline's selector now.
func (h *ModelsHandler) RunPipeline(w http.ResponseWriter, r *http.Request) {
	p := h.pipelineForSession(w, r)
	if p == nil {
		return
	}
	session := middleware.GetSession(r)
	runID, err := h.Runner.RunNamedPipeline(session.ConnectionID, p.ID, middleware.Actor(session))
	writeRunOutcome(w, runID, err)
}
