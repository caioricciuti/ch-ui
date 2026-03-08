package models

import (
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/caioricciuti/ch-ui/internal/crypto"
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

// RunAll executes all models for a connection in dependency order.
func (r *Runner) RunAll(connectionID, triggeredBy string) (string, error) {
	if err := r.acquireLock(connectionID); err != nil {
		return "", err
	}
	defer r.releaseLock(connectionID)

	if !r.gateway.IsTunnelOnline(connectionID) {
		return "", fmt.Errorf("tunnel not connected")
	}

	user, password, err := r.findCredentials(connectionID)
	if err != nil {
		return "", fmt.Errorf("no credentials: %w", err)
	}

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

	return r.execute(connectionID, triggeredBy, dag, idToModel, modelTargets, user, password)
}

// RunSingle executes a single model and its upstream dependencies.
func (r *Runner) RunSingle(connectionID, modelID, triggeredBy string) (string, error) {
	if err := r.acquireLock(connectionID); err != nil {
		return "", err
	}
	defer r.releaseLock(connectionID)

	if !r.gateway.IsTunnelOnline(connectionID) {
		return "", fmt.Errorf("tunnel not connected")
	}

	user, password, err := r.findCredentials(connectionID)
	if err != nil {
		return "", fmt.Errorf("no credentials: %w", err)
	}

	allModels, err := r.db.GetModelsByConnection(connectionID)
	if err != nil {
		return "", fmt.Errorf("load models: %w", err)
	}

	dag, idToModel, modelTargets, err := r.buildDAG(allModels)
	if err != nil {
		return "", err
	}

	// Filter to only the target model and its upstream deps
	upstream := GetUpstreamDeps(modelID, dag.Deps)
	upstream[modelID] = true

	var filteredIDs []string
	for _, id := range dag.Order {
		if upstream[id] {
			filteredIDs = append(filteredIDs, id)
		}
	}
	dag.Order = filteredIDs

	return r.execute(connectionID, triggeredBy, dag, idToModel, modelTargets, user, password)
}

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

func (r *Runner) execute(connectionID, triggeredBy string, dag *DepGraph, idToModel map[string]database.Model, modelTargets map[string]string, user, password string) (string, error) {
	runID, err := r.db.CreateModelRun(connectionID, len(dag.Order), triggeredBy)
	if err != nil {
		return "", fmt.Errorf("create run: %w", err)
	}

	// Create pending result records
	for _, id := range dag.Order {
		m := idToModel[id]
		if _, err := r.db.CreateModelRunResult(runID, m.ID, m.Name); err != nil {
			slog.Error("Failed to create run result", "model", m.Name, "error", err)
		}
	}

	// Execute in topological order
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

		// Resolve $ref()
		resolvedSQL, resolveErr := ResolveRefs(m.SQLBody, modelTargets)
		if resolveErr != nil {
			failedCount++
			failed[id] = true
			r.db.UpdateModelRunResult(runID, id, "error", resolvedSQL, 0, resolveErr.Error())
			r.db.UpdateModelStatus(id, "error", resolveErr.Error())
			continue
		}

		// Mark as running
		r.db.UpdateModelRunResult(runID, id, "running", "", 0, "")

		// Build and execute DDL
		stmts := buildDDL(m, resolvedSQL)
		start := time.Now()
		var execErr error

		for _, stmt := range stmts {
			_, execErr = r.gateway.ExecuteQuery(connectionID, stmt, user, password, 5*time.Minute)
			if execErr != nil {
				break
			}
		}

		elapsed := time.Since(start).Milliseconds()
		ddlForLog := stmts[len(stmts)-1] // log the main statement

		if execErr != nil {
			failedCount++
			failed[id] = true
			r.db.UpdateModelRunResult(runID, id, "error", ddlForLog, elapsed, execErr.Error())
			r.db.UpdateModelStatus(id, "error", execErr.Error())
			slog.Error("Model execution failed", "model", m.Name, "error", execErr)
		} else {
			succeeded++
			r.db.UpdateModelRunResult(runID, id, "success", ddlForLog, elapsed, "")
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

	return runID, nil
}

// buildDDL generates the DDL statement(s) for a model.
// Returns a slice because TABLE needs DROP + CREATE as separate statements.
func buildDDL(m database.Model, resolvedSQL string) []string {
	switch m.Materialization {
	case "table":
		drop := fmt.Sprintf("DROP TABLE IF EXISTS `%s`.`%s`", m.TargetDatabase, m.Name)
		create := fmt.Sprintf("CREATE TABLE `%s`.`%s` ENGINE = %s ORDER BY %s AS %s",
			m.TargetDatabase, m.Name, m.TableEngine, m.OrderBy, resolvedSQL)
		return []string{drop, create}
	default: // view
		return []string{
			fmt.Sprintf("CREATE OR REPLACE VIEW `%s`.`%s` AS %s",
				m.TargetDatabase, m.Name, resolvedSQL),
		}
	}
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
	sessions, err := r.db.GetActiveSessionsByConnection(connectionID, 3)
	if err != nil {
		return "", "", fmt.Errorf("failed to load sessions: %w", err)
	}
	for _, s := range sessions {
		password, err := crypto.Decrypt(s.EncryptedPassword, r.secret)
		if err != nil {
			continue
		}
		return s.ClickhouseUser, password, nil
	}
	return "", "", fmt.Errorf("no active sessions with valid credentials for connection %s", connectionID)
}
