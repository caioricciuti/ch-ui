package models

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

// Runner executes model builds against ClickHouse.
type Runner struct {
	db      *database.DB
	gateway *tunnel.Gateway
	secret  string
	mu      sync.Mutex // prevents concurrent runs per connection
	running map[string]bool
}

// NewRunner creates a new model runner.
func NewRunner(db *database.DB, gw *tunnel.Gateway, secret string) *Runner {
	return &Runner{
		db:      db,
		gateway: gw,
		secret:  secret,
		running: make(map[string]bool),
	}
}

// RunOptions selects what a run builds and how.
type RunOptions struct {
	// Selector picks the models (see ParseSelector); empty runs every model.
	Selector string
	// FullRefresh lists model IDs to rebuild from scratch: incremental and
	// materialized_view models then go through the safe swap instead of
	// adding rows.
	FullRefresh map[string]bool
	// PipelineID is recorded on the run when a named pipeline started it.
	PipelineID  string
	TriggeredBy string
}

// Model run scopes for RunModel.
const (
	ScopeUpstream   = "upstream"   // the model and everything it reads
	ScopeDownstream = "downstream" // the model and everything that reads it
	ScopeOnly       = "only"       // just the model
)

// RunAll executes all models for a connection in dependency order.
func (r *Runner) RunAll(connectionID, triggeredBy string) (string, error) {
	return r.Run(connectionID, RunOptions{TriggeredBy: triggeredBy})
}

// RunSingle executes a single model and its upstream dependencies.
func (r *Runner) RunSingle(connectionID, modelID, triggeredBy string) (string, error) {
	return r.RunModel(connectionID, modelID, ScopeUpstream, false, triggeredBy)
}

// RunModel runs one model with its upstream or downstream models (scope);
// fullRefresh rebuilds that model (not the others) from scratch.
func (r *Runner) RunModel(connectionID, modelID, scope string, fullRefresh bool, triggeredBy string) (string, error) {
	m, err := r.db.GetModelByID(modelID)
	if err != nil {
		return "", fmt.Errorf("load model: %w", err)
	}
	if m == nil || m.ConnectionID != connectionID {
		return "", ErrModelNotFound
	}
	selector := m.Name
	switch scope {
	case ScopeUpstream, "":
		selector = "+" + m.Name
	case ScopeDownstream:
		selector = m.Name + "+"
	case ScopeOnly:
	default:
		return "", fmt.Errorf("scope must be upstream, downstream or only (got %q)", scope)
	}
	opts := RunOptions{Selector: selector, TriggeredBy: triggeredBy}
	if fullRefresh {
		opts.FullRefresh = map[string]bool{m.ID: true}
	}
	return r.Run(connectionID, opts)
}

// RunNamedPipeline runs a stored pipeline's selector.
func (r *Runner) RunNamedPipeline(connectionID, pipelineID, triggeredBy string) (string, error) {
	p, err := r.db.GetModelPipelineByID(pipelineID)
	if err != nil {
		return "", fmt.Errorf("load pipeline: %w", err)
	}
	if p == nil || p.ConnectionID != connectionID {
		return "", ErrPipelineNotFound
	}
	return r.Run(connectionID, RunOptions{Selector: p.Selector, PipelineID: p.ID, TriggeredBy: triggeredBy})
}

// Run builds the selected models in dependency order. Selection errors
// (unknown model, bad selector) return before anything is recorded.
func (r *Runner) Run(connectionID string, opts RunOptions) (string, error) {
	if err := r.acquireLock(connectionID); err != nil {
		return "", err
	}
	defer r.releaseLock(connectionID)

	allModels, err := r.db.GetModelsByConnection(connectionID)
	if err != nil {
		return "", fmt.Errorf("load models: %w", err)
	}
	if len(allModels) == 0 {
		return "", fmt.Errorf("no models defined")
	}

	dag, idToModel, modelTargets, err := r.buildDAG(allModels)
	if err != nil {
		return "", err
	}
	if s := strings.TrimSpace(opts.Selector); s != "" {
		nameToID := make(map[string]string, len(allModels))
		for _, m := range allModels {
			nameToID[m.Name] = m.ID
		}
		ids, err := dag.Select(s, nameToID)
		if err != nil {
			return "", &SelectorError{Err: err}
		}
		dag.Order = ids
	}
	if len(dag.Order) == 0 {
		return "", fmt.Errorf("the selector picks no models")
	}

	if !r.gateway.IsTunnelOnline(connectionID) {
		return "", fmt.Errorf("tunnel not connected")
	}
	user, password, err := r.findCredentials(connectionID)
	if err != nil {
		return "", fmt.Errorf("no credentials: %w", err)
	}

	return r.execute(connectionID, opts, dag, idToModel, modelTargets, user, password)
}

// SelectorError wraps a selector that does not parse or names unknown models.
type SelectorError struct{ Err error }

func (e *SelectorError) Error() string { return e.Err.Error() }
func (e *SelectorError) Unwrap() error { return e.Err }

// Validate checks all models for reference errors and cycles.
func (r *Runner) Validate(connectionID string) ([]ValidationError, error) {
	allModels, err := r.db.GetModelsByConnection(connectionID)
	if err != nil {
		return nil, fmt.Errorf("load models: %w", err)
	}
	if len(allModels) == 0 {
		return nil, nil
	}

	nameToID := make(map[string]string)
	for _, m := range allModels {
		nameToID[m.Name] = m.ID
	}

	var errors []ValidationError
	refsByID := make(map[string][]string)

	for _, m := range allModels {
		refs := ExtractRefs(m.SQLBody)
		refsByID[m.ID] = refs
		for _, ref := range refs {
			if _, ok := nameToID[ref]; !ok {
				errors = append(errors, ValidationError{
					ModelID:   m.ID,
					ModelName: m.Name,
					Error:     fmt.Sprintf("references unknown model %q via $ref()", ref),
				})
			}
			if nameToID[ref] == m.ID {
				errors = append(errors, ValidationError{
					ModelID:   m.ID,
					ModelName: m.Name,
					Error:     fmt.Sprintf("cannot reference itself via $ref(%s)", ref),
				})
			}
		}
	}

	if len(errors) > 0 {
		return errors, nil
	}

	// Check for cycles
	var modelIDs []string
	for _, m := range allModels {
		modelIDs = append(modelIDs, m.ID)
	}

	_, dagErr := BuildDAG(modelIDs, refsByID, nameToID)
	if dagErr != nil {
		errors = append(errors, ValidationError{
			Error: dagErr.Error(),
		})
	}

	return errors, nil
}

// ValidationError represents a validation problem.
type ValidationError struct {
	ModelID   string `json:"model_id,omitempty"`
	ModelName string `json:"model_name,omitempty"`
	Error     string `json:"error"`
}

// ── Internal helpers ────────────────────────────────────────────────

func (r *Runner) buildDAG(allModels []database.Model) (*DepGraph, map[string]database.Model, map[string]string, error) {
	nameToID := make(map[string]string)
	idToModel := make(map[string]database.Model)
	modelTargets := make(map[string]string)
	var modelIDs []string
	refsByID := make(map[string][]string)

	for _, m := range allModels {
		nameToID[m.Name] = m.ID
		idToModel[m.ID] = m
		modelTargets[m.Name] = m.TargetDatabase
		modelIDs = append(modelIDs, m.ID)
		refsByID[m.ID] = ExtractRefs(m.SQLBody)
	}

	dag, err := BuildDAG(modelIDs, refsByID, nameToID)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("build DAG: %w", err)
	}

	return dag, idToModel, modelTargets, nil
}

func (r *Runner) execute(connectionID string, opts RunOptions, dag *DepGraph, idToModel map[string]database.Model, modelTargets map[string]string, user, password string) (string, error) {
	runID, err := r.db.CreateModelRun(connectionID, len(dag.Order), opts.TriggeredBy)
	if err != nil {
		return "", fmt.Errorf("create run: %w", err)
	}
	if opts.Selector != "" || opts.PipelineID != "" {
		if err := r.db.SetModelRunScope(runID, opts.Selector, opts.PipelineID); err != nil {
			slog.Error("Failed to record run scope", "run", runID, "error", err)
		}
	}

	// Create pending result records
	for _, id := range dag.Order {
		m := idToModel[id]
		if _, err := r.db.CreateModelRunResult(runID, m.ID, m.Name); err != nil {
			slog.Error("Failed to create run result", "model", m.Name, "error", err)
		}
	}

	// Execute in topological order
	exec := func(sql string) (*tunnel.QueryResult, error) {
		return r.gateway.ExecuteQuery(connectionID, sql, user, password, 5*time.Minute)
	}
	readOnly := func(sql string) (*tunnel.QueryResult, error) {
		return r.gateway.ExecuteQueryWithSettings(connectionID, sql, user, password, testSettings, 3*time.Minute)
	}
	failed := make(map[string]bool)
	var succeeded, failedCount, skipped int

	for _, id := range dag.Order {
		m := idToModel[id]

		// Skip if any upstream dependency failed
		shouldSkip := false
		for _, depID := range dag.Deps[id] {
			if failed[depID] {
				shouldSkip = true
				break
			}
		}

		if shouldSkip {
			skipped++
			failed[id] = true
			r.db.UpdateModelRunResult(runID, id, "skipped", "", 0, "upstream dependency failed")
			r.db.UpdateModelStatus(id, "error", "upstream dependency failed")
			continue
		}

		// Resolve $ref() and $this
		resolvedSQL, resolveErr := ResolveModelSQL(m.SQLBody, modelTargets, m.TargetDatabase, m.Name)
		if resolveErr != nil {
			failedCount++
			failed[id] = true
			r.db.UpdateModelRunResult(runID, id, "error", resolvedSQL, 0, resolveErr.Error())
			r.db.UpdateModelStatus(id, "error", resolveErr.Error())
			continue
		}

		// Mark as running
		r.db.UpdateModelRunResult(runID, id, "running", "", 0, "")

		mz := &materializer{
			exec:         exec,
			model:        m,
			priorSuccess: func() (bool, error) { return r.db.HasSuccessfulModelRun(m.ID) },
			fullRefresh:  opts.FullRefresh[m.ID],
			usesThis:     UsesThis(m.SQLBody),
		}
		start := time.Now()
		ddl, execErr := mz.run(resolvedSQL)
		if ddl == "" {
			ddl = resolvedSQL
		}

		// Data tests run only on a model that built.
		if execErr == nil {
			execErr = r.runTests(runID, m, modelTargets, readOnly)
		}
		elapsed := time.Since(start).Milliseconds()

		if execErr != nil {
			failedCount++
			failed[id] = true
			r.db.UpdateModelRunResult(runID, id, "error", ddl, elapsed, execErr.Error())
			r.db.UpdateModelStatus(id, "error", execErr.Error())
			slog.Error("Model execution failed", "model", m.Name, "error", execErr)
		} else {
			succeeded++
			r.db.UpdateModelRunResult(runID, id, "success", ddl, elapsed, "")
			r.db.UpdateModelStatus(id, "success", "")
		}
	}

	// Finalize run
	runStatus := "success"
	if failedCount > 0 && succeeded > 0 {
		runStatus = "partial"
	} else if failedCount > 0 || skipped == len(dag.Order) {
		runStatus = "error"
	}
	r.db.FinalizeModelRun(runID, runStatus, succeeded, failedCount, skipped)

	if runStatus != "success" {
		return runID, &RunFailedError{RunID: runID, Status: runStatus, Failed: failedCount, Skipped: skipped}
	}
	return runID, nil
}

// runTests runs the model's data tests, records their outcomes on the run
// result and returns an error when an error-severity test failed.
func (r *Runner) runTests(runID string, m database.Model, modelTargets map[string]string, query func(string) (*tunnel.QueryResult, error)) error {
	tests, err := r.db.GetModelTests(m.ID)
	if err != nil {
		return fmt.Errorf("load data tests: %w", err)
	}
	if len(tests) == 0 {
		return nil
	}
	target := quoteIdent(m.TargetDatabase) + "." + quoteIdent(m.Name)
	outcomes, blocking := runDataTests(query, tests, target, modelTargets, m.TargetDatabase)
	passed := 0
	for _, o := range outcomes {
		if o.Status == "pass" {
			passed++
		}
	}
	details, _ := json.Marshal(outcomes)
	if err := r.db.UpdateModelRunResultTests(runID, m.ID, passed, len(outcomes)-passed, string(details)); err != nil {
		slog.Error("Failed to record data test results", "model", m.Name, "error", err)
	}
	if blocking {
		return errors.New(summarizeFailures(outcomes))
	}
	return nil
}

// ErrModelNotFound means the model (or pipeline anchor) is not one of the
// connection's models. Nothing is recorded.
var ErrModelNotFound = errors.New("model not found")

// ErrPipelineNotFound means the pipeline is not one of the connection's.
var ErrPipelineNotFound = errors.New("pipeline not found")

// RunFailedError is returned, together with the run ID, when a run finished
// but at least one model failed or was skipped. The run is recorded.
type RunFailedError struct {
	RunID   string
	Status  string // "partial" or "error"
	Failed  int
	Skipped int
}

func (e *RunFailedError) Error() string {
	return fmt.Sprintf("model run finished with status %s: %d failed, %d skipped", e.Status, e.Failed, e.Skipped)
}

func (r *Runner) acquireLock(connectionID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.running[connectionID] {
		return fmt.Errorf("a model run is already in progress for this connection")
	}
	r.running[connectionID] = true
	return nil
}

func (r *Runner) releaseLock(connectionID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.running, connectionID)
}

func (r *Runner) findCredentials(connectionID string) (string, string, error) {
	return r.db.BackgroundCredentials(connectionID, "model", r.secret)
}
