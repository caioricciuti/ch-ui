package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/models"
	"github.com/caioricciuti/ch-ui/internal/pipelines"
)

// Lifecycle actions run work that already exists in CH-UI: build a model,
// start or stop a data pipeline (and, on Pro, run a schedule or send a
// report now). They sit behind their own per-key permission (allow_actions,
// or the OAuth "actions" scope) on top of read_write, so a key that may only
// create drafts never sees them. Every action asks for confirmation on
// clients that support it and writes an audit row.
//
// The status reads (get_model_run, pipeline_status) are available to every
// key so a client can check on work a person started in the UI.

// canAct reports whether the lifecycle actions are available to this
// request: write access (which an OAuth demotion revokes) plus the opt-in.
func (ak *authedKey) canAct() bool {
	return ak.canWrite() && ak.key.AllowActions
}

// actionTool returns the metadata for a tool that runs work in ClickHouse
// or outside CH-UI. destructive marks tools that can replace or remove data.
func actionTool(title string, destructive, idempotent, openWorld bool) (string, *mcp.ToolAnnotations) {
	return title, &mcp.ToolAnnotations{
		Title:           title,
		ReadOnlyHint:    false,
		DestructiveHint: &destructive,
		IdempotentHint:  idempotent,
		OpenWorldHint:   &openWorld,
	}
}

// ---- tool args ----

type runModelArgs struct {
	Model       string `json:"model" jsonschema:"model name or id on this connection"`
	Scope       string `json:"scope,omitempty" jsonschema:"only (default: just this model), upstream (this model and the models it reads), or downstream (this model and the models that read it)"`
	FullRefresh bool   `json:"full_refresh,omitempty" jsonschema:"rebuild this model from scratch instead of adding rows (incremental and materialized_view models)"`
}

type modelRunArgs struct {
	RunID string `json:"run_id,omitempty" jsonschema:"a run id returned by run_model or shown on the Models page"`
	Model string `json:"model,omitempty" jsonschema:"model name or id; returns its current status and latest result"`
}

type pipelineArgs struct {
	Pipeline string `json:"pipeline" jsonschema:"pipeline name or id on this connection"`
}

type pipelineStatusArgs struct {
	Pipeline string `json:"pipeline" jsonschema:"pipeline name or id on this connection"`
	Runs     int    `json:"runs,omitempty" jsonschema:"recent runs to include (default 5, max 50)"`
}

const (
	defaultStatusRuns = 5
	maxStatusRuns     = 50
)

// findModel resolves a model by id or name among this connection's models.
func findModel(deps Deps, ak *authedKey, ref string) (*database.Model, *mcp.CallToolResult) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, errResult("model is required")
	}
	all, err := deps.DB.GetModelsByConnection(ak.key.ConnectionID)
	if err != nil {
		return nil, errResult("failed to load models: %v", err)
	}
	for i := range all {
		if all[i].ID == ref || all[i].Name == ref {
			return &all[i], nil
		}
	}
	return nil, errResult("model %q not found on this connection; list_models shows the names", ref)
}

// findPipeline resolves a data pipeline by id or name on this connection.
func findPipeline(deps Deps, ak *authedKey, ref string) (*database.Pipeline, *mcp.CallToolResult) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, errResult("pipeline is required")
	}
	all, err := deps.DB.GetPipelines()
	if err != nil {
		return nil, errResult("failed to load pipelines: %v", err)
	}
	var byName []database.Pipeline
	for _, p := range all {
		if p.ConnectionID != ak.key.ConnectionID {
			continue
		}
		if p.ID == ref {
			return &p, nil
		}
		if p.Name == ref {
			byName = append(byName, p)
		}
	}
	switch len(byName) {
	case 0:
		return nil, errResult("pipeline %q not found on this connection; list_pipelines shows the names", ref)
	case 1:
		return &byName[0], nil
	default:
		return nil, errResult("%d pipelines are named %q; pass the id from list_pipelines", len(byName), ref)
	}
}

// pipelineAllowed refuses a pipeline whose sinks write outside the key's
// database allow-list, and returns its graph nodes.
func pipelineAllowed(deps Deps, ak *authedKey, p *database.Pipeline) ([]database.PipelineNode, *mcp.CallToolResult) {
	nodes, _, err := deps.DB.GetPipelineGraph(p.ID)
	if err != nil {
		return nil, errResult("failed to load pipeline %q: %v", p.Name, err)
	}
	if allowedDBs(ak.key) == nil {
		return nodes, nil
	}
	dbs, ok := pipelines.SinkDatabases(nodes)
	if !ok {
		return nil, errResult("pipeline %q has a sink this key cannot check against its allowlist (%s)", p.Name, allowList(ak.key.AllowedDatabases).describe())
	}
	for _, db := range dbs {
		if !dbAllowed(ak.key, db) {
			return nil, errResult("pipeline %q writes to database %q, which is not in this key's allowlist", p.Name, db)
		}
	}
	return nodes, nil
}

// sourceLabel names the pipeline's source for prompts: "kafka", "s3", ...
func sourceLabel(nodes []database.PipelineNode) string {
	for _, n := range nodes {
		if strings.HasPrefix(n.NodeType, "source_") {
			return strings.TrimPrefix(n.NodeType, "source_")
		}
	}
	return "unknown"
}

// grantsHint explains a ClickHouse permission error in terms of the key.
func grantsHint(ak *authedKey, msg string) string {
	if strings.Contains(msg, "ACCESS_DENIED") || strings.Contains(msg, "Not enough privileges") {
		return fmt.Sprintf("the ClickHouse user %q this key runs as lacks a grant the build needs; ask an admin to grant it, or use a key with a user that has it", ak.key.CHUser)
	}
	return ""
}

// modelResult is the per-model line of a run, without the resolved DDL.
func modelResult(r database.ModelRunResult) map[string]any {
	out := map[string]any{
		"model": r.ModelName, "status": r.Status, "elapsed_ms": r.ElapsedMs,
	}
	if r.Error != nil && *r.Error != "" {
		out["error"] = *r.Error
	}
	if r.TestsPassed+r.TestsFailed > 0 {
		out["tests_passed"] = r.TestsPassed
		out["tests_failed"] = r.TestsFailed
	}
	if r.FinishedAt != nil {
		out["finished_at"] = *r.FinishedAt
	}
	return out
}

// runSummary renders a recorded model run and its per-model results,
// leaving out models whose target database the key may not see.
func runSummary(deps Deps, ak *authedKey, runID string) (map[string]any, error) {
	run, err := deps.DB.GetModelRunByID(runID)
	if err != nil {
		return nil, err
	}
	if run == nil || run.ConnID != ak.key.ConnectionID {
		return nil, nil
	}
	results, err := deps.DB.GetModelRunResults(runID)
	if err != nil {
		return nil, err
	}
	targets := map[string]string{}
	if all, err := deps.DB.GetModelsByConnection(ak.key.ConnectionID); err == nil {
		for _, m := range all {
			targets[m.ID] = m.TargetDatabase
		}
	}
	lines := make([]map[string]any, 0, len(results))
	hidden := 0
	for _, r := range results {
		if db, ok := targets[r.ModelID]; ok && !dbAllowed(ak.key, db) {
			hidden++
			continue
		}
		lines = append(lines, modelResult(r))
	}
	out := map[string]any{
		"run_id": run.ID, "status": run.Status, "selector": run.Selector,
		"succeeded": run.Succeeded, "failed": run.Failed, "skipped": run.Skipped,
		"started_at": run.StartedAt, "finished_at": run.FinishedAt, "triggered_by": run.TriggeredBy,
		"models": lines,
	}
	if hidden > 0 {
		out["omitted"] = fmt.Sprintf("%d model(s) outside this key's database allowlist", hidden)
	}
	return out, nil
}

// registerActionStatusTools exposes read-only status of model runs and data
// pipelines. Available to every key.
func registerActionStatusTools(srv *mcp.Server, deps Deps, ak *authedKey) {
	title, ann := readOnlyTool("Model run status")
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_model_run",
		Title:       title,
		Annotations: ann,
		Description: "Status of a model run (pass run_id) with per-model results, or a model's current status and latest result (pass model). Use it to check whether a model built and why it failed.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args modelRunArgs) (*mcp.CallToolResult, any, error) {
		if runID := strings.TrimSpace(args.RunID); runID != "" {
			out, err := runSummary(deps, ak, runID)
			if err != nil {
				return errResult("failed to load run: %v", err), nil, nil
			}
			if out == nil {
				return errResult("run %q not found on this connection", runID), nil, nil
			}
			return jsonResult(out), nil, nil
		}
		if strings.TrimSpace(args.Model) == "" {
			return errResult("pass run_id or model"), nil, nil
		}
		m, res := findModel(deps, ak, args.Model)
		if res != nil {
			return res, nil, nil
		}
		if !dbAllowed(ak.key, m.TargetDatabase) {
			return errResult("model %q builds into database %q, which is not in this key's allowlist", m.Name, m.TargetDatabase), nil, nil
		}
		out := map[string]any{
			"model": m.Name, "status": m.Status, "target_database": m.TargetDatabase,
			"materialization": m.Materialization, "last_error": m.LastError, "last_run_at": m.LastRunAt,
		}
		latest, err := deps.DB.GetLatestModelRunResult(m.ID)
		if err != nil {
			return errResult("failed to load the latest result: %v", err), nil, nil
		}
		if latest != nil {
			line := modelResult(*latest)
			line["run_id"] = latest.RunID
			out["latest_result"] = line
		}
		return jsonResult(out), nil, nil
	})

	if deps.PipelineRunner == nil {
		return
	}
	title, ann = readOnlyTool("Pipeline status")
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "pipeline_status",
		Title:       title,
		Annotations: ann,
		Description: "State of a data pipeline on this connection: status, last error, dead-letter rows, live throughput while it runs, and its recent runs with rows ingested.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args pipelineStatusArgs) (*mcp.CallToolResult, any, error) {
		p, res := findPipeline(deps, ak, args.Pipeline)
		if res != nil {
			return res, nil, nil
		}
		if _, res := pipelineAllowed(deps, ak, p); res != nil {
			return res, nil, nil
		}
		n := args.Runs
		if n <= 0 {
			n = defaultStatusRuns
		}
		if n > maxStatusRuns {
			n = maxStatusRuns
		}
		out := deps.PipelineRunner.Status(p)
		out["name"] = p.Name
		runs, err := deps.DB.GetPipelineRuns(p.ID, n, 0)
		if err != nil {
			return errResult("failed to load pipeline runs: %v", err), nil, nil
		}
		recent := make([]map[string]any, 0, len(runs))
		for _, r := range runs {
			recent = append(recent, map[string]any{
				"status": r.Status, "started_at": r.StartedAt, "finished_at": r.FinishedAt,
				"rows_ingested": r.RowsIngested, "bytes_ingested": r.BytesIngested,
				"errors_count": r.ErrorsCount, "last_error": r.LastError,
			})
		}
		out["recent_runs"] = recent
		return jsonResult(out), nil, nil
	})
}

// registerActionTools adds the lifecycle actions. Called only when
// ak.canAct().
func registerActionTools(srv *mcp.Server, deps Deps, ak *authedKey) {
	if deps.ModelRunner != nil {
		registerRunModel(srv, deps, ak)
	}
	if deps.PipelineRunner != nil {
		registerPipelineActions(srv, deps, ak)
	}
	if deps.Config != nil && deps.Config.IsPro() {
		registerProActionTools(srv, deps, ak)
	}
}

func registerRunModel(srv *mcp.Server, deps Deps, ak *authedKey) {
	title, ann := actionTool("Run model", true, false, false)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "run_model",
		Title:       title,
		Annotations: ann,
		Description: "Build a CH-UI model in ClickHouse now as this key's ClickHouse user. A view model is replaced (CREATE OR REPLACE VIEW); a table model is rebuilt and swapped in, and the previous table and its rows are dropped; incremental models add rows unless full_refresh rebuilds them the same way. Only objects the model created are replaced. Returns the run status and per-model results; get_model_run checks it later.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args runModelArgs) (*mcp.CallToolResult, any, error) {
		m, res := findModel(deps, ak, args.Model)
		if res != nil {
			return res, nil, nil
		}
		scope := strings.TrimSpace(args.Scope)
		if scope == "" {
			scope = models.ScopeOnly
		}
		if scope != models.ScopeOnly && scope != models.ScopeUpstream && scope != models.ScopeDownstream {
			return errResult("scope must be only, upstream or downstream"), nil, nil
		}
		if !dbAllowed(ak.key, m.TargetDatabase) {
			return errResult("model %q builds into database %q, which is not in this key's allowlist", m.Name, m.TargetDatabase), nil, nil
		}
		what := fmt.Sprintf("model %q (%s in %s)", m.Name, m.Materialization, m.TargetDatabase)
		switch scope {
		case models.ScopeUpstream:
			what += " and the models it reads"
		case models.ScopeDownstream:
			what += " and the models that read it"
		}
		effect := ""
		switch {
		case m.Materialization == "view":
			effect = " The view is replaced."
		case m.Materialization == "table" || args.FullRefresh:
			effect = " The existing table is rebuilt and the old one, with its rows, is dropped."
		default:
			effect = " Rows are added to the existing table."
		}
		if scope != models.ScopeOnly {
			effect += " Other selected models are rebuilt the same way for their type."
		}
		msg := fmt.Sprintf("Run %s in ClickHouse now as user %q?%s", what, ak.key.CHUser, effect)
		if res := confirmWrite(deps, ak, req, msg); res != nil {
			return res, nil, nil
		}

		opts := models.RunOptions{
			TriggeredBy: mcpAuthor(ak),
			Credentials: &models.Credentials{User: ak.key.CHUser, Password: ak.chPassword},
			Check:       modelAllowListCheck(ctx, deps, ak),
		}
		if args.FullRefresh {
			opts.FullRefresh = map[string]bool{m.ID: true}
		}
		runID, err := deps.ModelRunner.RunModelWith(ak.key.ConnectionID, m.ID, scope, opts)

		var failed *models.RunFailedError
		var refused *allowListError
		switch {
		case errors.As(err, &refused):
			return errResult("%v", err), nil, nil
		case err != nil && !errors.As(err, &failed):
			audit(deps, ak, "model.run", fmt.Sprintf("mcp key: %s, model: %s, scope: %s, status: error, error: %v", ak.key.Name, m.Name, scope, err))
			return errResult("run_model failed before building anything: %v", err), nil, nil
		}
		out, serr := runSummary(deps, ak, runID)
		if serr != nil || out == nil {
			out = map[string]any{"run_id": runID}
		}
		status, _ := out["status"].(string)
		audit(deps, ak, "model.run", fmt.Sprintf("mcp key: %s, model: %s, scope: %s, run: %s, status: %s", ak.key.Name, m.Name, scope, runID, status))
		if failed != nil {
			lines, _ := out["models"].([]map[string]any)
			for _, line := range lines {
				if e, _ := line["error"].(string); grantsHint(ak, e) != "" {
					out["hint"] = grantsHint(ak, e)
					break
				}
			}
			r := jsonResult(out)
			r.IsError = true
			return r, nil, nil
		}
		return jsonResult(out), nil, nil
	})
}

// allowListError is a run refused by the key's database allow-list.
type allowListError struct{ msg string }

func (e *allowListError) Error() string { return e.msg }

// modelAllowListCheck refuses a run for an allow-listed key when a selected
// model builds into, or reads from, a database outside the list.
func modelAllowListCheck(ctx context.Context, deps Deps, ak *authedKey) func(database.Model, string) error {
	if allowedDBs(ak.key) == nil {
		return nil
	}
	currentDB := func() (string, error) {
		rows, err := runCH(ctx, deps, ak, "SELECT currentDatabase() AS db FORMAT JSON", nil, metaTimeout)
		if err != nil {
			return "", err
		}
		if len(rows) != 1 {
			return "", fmt.Errorf("unexpected currentDatabase() result")
		}
		db, _ := rows[0]["db"].(string)
		return db, nil
	}
	return func(m database.Model, resolved string) error {
		if !dbAllowed(ak.key, m.TargetDatabase) {
			return &allowListError{fmt.Sprintf("model %q builds into database %q, which is not in this key's allowlist; nothing ran", m.Name, m.TargetDatabase)}
		}
		if resolved == "" {
			return nil
		}
		if err := checkAllowedDatabases(currentDB, allowList(ak.key.AllowedDatabases), resolved, nil); err != nil {
			return &allowListError{fmt.Sprintf("model %q: %v; nothing ran", m.Name, err)}
		}
		return nil
	}
}

func registerPipelineActions(srv *mcp.Server, deps Deps, ak *authedKey) {
	title, ann := actionTool("Start pipeline", false, false, true)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "start_pipeline",
		Title:       title,
		Annotations: ann,
		Description: "Start a data pipeline on this connection. It keeps reading from its source and writing into its ClickHouse sink with the pipeline's own stored credentials until stop_pipeline or a person stops it.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args pipelineArgs) (*mcp.CallToolResult, any, error) {
		p, res := findPipeline(deps, ak, args.Pipeline)
		if res != nil {
			return res, nil, nil
		}
		nodes, res := pipelineAllowed(deps, ak, p)
		if res != nil {
			return res, nil, nil
		}
		if p.Status == "running" || p.Status == "starting" {
			return errResult("pipeline %q is already running", p.Name), nil, nil
		}
		// SQLite sources read files on the CH-UI server: admins start
		// those from the UI.
		if pipelines.HasSQLiteSource(nodes) {
			return errResult("pipeline %q reads a SQLite file on the CH-UI server; only an admin can start it, from the Pipelines page", p.Name), nil, nil
		}
		msg := fmt.Sprintf("Start pipeline %q (%s source)? It keeps writing into ClickHouse until it is stopped.", p.Name, sourceLabel(nodes))
		if res := confirmWrite(deps, ak, req, msg); res != nil {
			return res, nil, nil
		}
		if err := deps.PipelineRunner.StartPipeline(p.ID); err != nil {
			audit(deps, ak, "pipeline.started", fmt.Sprintf("mcp key: %s, pipeline: %s, status: error, error: %v", ak.key.Name, p.Name, err))
			return errResult("failed to start pipeline %q: %v", p.Name, err), nil, nil
		}
		audit(deps, ak, "pipeline.started", "mcp key: "+ak.key.Name+", pipeline: "+p.Name)
		return jsonResult(map[string]any{
			"id": p.ID, "name": p.Name, "status": "starting",
			"note": "started; pipeline_status shows throughput and errors",
		}), nil, nil
	})

	title, ann = actionTool("Stop pipeline", false, true, false)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "stop_pipeline",
		Title:       title,
		Annotations: ann,
		Description: "Stop a running data pipeline on this connection. Its source position is kept, so starting it again resumes where it stopped.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args pipelineArgs) (*mcp.CallToolResult, any, error) {
		p, res := findPipeline(deps, ak, args.Pipeline)
		if res != nil {
			return res, nil, nil
		}
		if _, res := pipelineAllowed(deps, ak, p); res != nil {
			return res, nil, nil
		}
		if p.Status != "running" && p.Status != "starting" {
			return errResult("pipeline %q is not running (status: %s)", p.Name, p.Status), nil, nil
		}
		if res := confirmWrite(deps, ak, req, fmt.Sprintf("Stop pipeline %q? Ingestion pauses until it is started again.", p.Name)); res != nil {
			return res, nil, nil
		}
		if err := deps.PipelineRunner.StopPipeline(p.ID); err != nil {
			audit(deps, ak, "pipeline.stopped", fmt.Sprintf("mcp key: %s, pipeline: %s, status: error, error: %v", ak.key.Name, p.Name, err))
			return errResult("failed to stop pipeline %q: %v", p.Name, err), nil, nil
		}
		audit(deps, ak, "pipeline.stopped", "mcp key: "+ak.key.Name+", pipeline: "+p.Name)
		return jsonResult(map[string]any{"id": p.ID, "name": p.Name, "status": "stopped"}), nil, nil
	})
}
