package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/models"
)

// Write tools create CH-UI entities (saved queries, dashboards, models,
// pipelines) in SQLite. They are registered only for keys with the
// 'read_write' scope; a plain 'read' key never sees them. Everything is
// created as a draft where the concept exists — nothing runs from here.
//
// created_by is always "mcp:<key name>" so the provenance is visible in the
// UI, and every create lands in the audit log like its HTTP counterpart.

// mcpAuthor is the created_by/provenance marker for MCP-created entities.
func mcpAuthor(ak *authedKey) string { return "mcp:" + ak.key.Name }

func audit(deps Deps, ak *authedKey, action, details string) {
	user := ak.key.CHUser
	connID := ak.key.ConnectionID
	d := details
	go deps.DB.CreateAuditLog(database.AuditLogParams{
		Action:       action,
		Username:     &user,
		ConnectionID: &connID,
		Details:      &d,
	})
}

// ---- tool args ----

type saveQueryArgs struct {
	Name        string `json:"name" jsonschema:"name of the saved query"`
	SQL         string `json:"sql" jsonschema:"the SQL text to save"`
	Description string `json:"description,omitempty" jsonschema:"optional description"`
}

type dashboardPanelArgs struct {
	Name string `json:"name" jsonschema:"panel title"`
	SQL  string `json:"sql" jsonschema:"the SQL the panel runs"`
	Type string `json:"type,omitempty" jsonschema:"panel type: table (default), stat, line, bar, area, pie"`
	X    *int   `json:"x,omitempty" jsonschema:"grid column (0-11); omit for automatic layout"`
	Y    *int   `json:"y,omitempty" jsonschema:"grid row; omit for automatic layout"`
	W    *int   `json:"w,omitempty" jsonschema:"width in grid units (default 6)"`
	H    *int   `json:"h,omitempty" jsonschema:"height in grid units (default 4)"`
}

type createDashboardArgs struct {
	Name        string               `json:"name" jsonschema:"dashboard name"`
	Description string               `json:"description,omitempty" jsonschema:"optional description"`
	Panels      []dashboardPanelArgs `json:"panels" jsonschema:"panels to create; each runs one SQL statement"`
}

type createModelArgs struct {
	Name            string `json:"name" jsonschema:"model name; must be a valid ClickHouse identifier, unique per connection"`
	SQL             string `json:"sql" jsonschema:"the model's SELECT body; reference other models with $ref(model_name)"`
	Description     string `json:"description,omitempty" jsonschema:"optional description"`
	TargetDatabase  string `json:"target_database,omitempty" jsonschema:"database the model materializes into (default: default)"`
	Materialization string `json:"materialization,omitempty" jsonschema:"view (default) or table"`
	TableEngine     string `json:"table_engine,omitempty" jsonschema:"engine when materialization=table (default MergeTree)"`
	OrderBy         string `json:"order_by,omitempty" jsonschema:"ORDER BY when materialization=table (default tuple())"`
}

type createPipelineArgs struct {
	Name           string         `json:"name" jsonschema:"pipeline name"`
	Description    string         `json:"description,omitempty" jsonschema:"optional description"`
	SourceType     string         `json:"source_type" jsonschema:"source connector: source_kafka, source_webhook, source_database, or source_s3"`
	SourceConfig   map[string]any `json:"source_config,omitempty" jsonschema:"connector-specific source settings (brokers/topic for kafka, bucket/prefix for s3, ...); can be completed later in the UI"`
	TargetDatabase string         `json:"target_database" jsonschema:"ClickHouse database the sink writes to"`
	TargetTable    string         `json:"target_table" jsonschema:"ClickHouse table the sink writes to"`
}

var pipelineSourceTypes = map[string]bool{
	"source_kafka":    true,
	"source_webhook":  true,
	"source_database": true,
	"source_s3":       true,
}

func registerWriteTools(srv *mcp.Server, deps Deps, ak *authedKey) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "save_query",
		Description: "Save a SQL query in CH-UI's saved queries library (visible to every user of this instance).",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args saveQueryArgs) (*mcp.CallToolResult, any, error) {
		name := strings.TrimSpace(args.Name)
		sqlText := strings.TrimSpace(args.SQL)
		if name == "" || sqlText == "" {
			return errResult("name and sql are required"), nil, nil
		}
		id, err := deps.DB.CreateSavedQuery(database.CreateSavedQueryParams{
			Name:         name,
			Description:  strings.TrimSpace(args.Description),
			Query:        sqlText,
			ConnectionID: ak.key.ConnectionID,
			CreatedBy:    mcpAuthor(ak),
		})
		if err != nil {
			return errResult("failed to save query: %v", err), nil, nil
		}
		audit(deps, ak, "saved_query.created", "mcp key: "+ak.key.Name+", query: "+name)
		return jsonResult(map[string]any{"id": id, "name": name, "note": "saved; find it under Saved Queries"}), nil, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "create_dashboard",
		Description: "Create a CH-UI dashboard with SQL panels. Panels lay out automatically two per row unless x/y coordinates are given.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args createDashboardArgs) (*mcp.CallToolResult, any, error) {
		name := strings.TrimSpace(args.Name)
		if name == "" {
			return errResult("name is required"), nil, nil
		}
		if len(args.Panels) == 0 {
			return errResult("at least one panel is required"), nil, nil
		}
		for i, p := range args.Panels {
			if strings.TrimSpace(p.Name) == "" || strings.TrimSpace(p.SQL) == "" {
				return errResult("panel %d needs both name and sql", i+1), nil, nil
			}
		}

		dashID, err := deps.DB.CreateDashboard(name, strings.TrimSpace(args.Description), mcpAuthor(ak))
		if err != nil {
			return errResult("failed to create dashboard: %v", err), nil, nil
		}

		created := make([]map[string]any, 0, len(args.Panels))
		for i, p := range args.Panels {
			panelType := strings.TrimSpace(p.Type)
			if panelType == "" {
				panelType = "table"
			}
			config := "{}"
			if panelType != "table" {
				b, _ := json.Marshal(map[string]string{"chartType": panelType})
				config = string(b)
			}
			// automatic layout: two panels per row, 6x4 grid units each
			w, h := 6, 4
			if p.W != nil && *p.W > 0 {
				w = *p.W
			}
			if p.H != nil && *p.H > 0 {
				h = *p.H
			}
			x, y := (i%2)*6, (i/2)*4
			if p.X != nil {
				x = *p.X
			}
			if p.Y != nil {
				y = *p.Y
			}
			panelID, err := deps.DB.CreatePanel(dashID, strings.TrimSpace(p.Name), "", panelType, strings.TrimSpace(p.SQL), ak.key.ConnectionID, config, x, y, w, h)
			if err != nil {
				return errResult("dashboard %q created, but panel %d failed: %v", name, i+1, err), nil, nil
			}
			created = append(created, map[string]any{"id": panelID, "name": p.Name, "type": panelType})
		}
		audit(deps, ak, "dashboard.created", "mcp key: "+ak.key.Name+", dashboard: "+name)
		return jsonResult(map[string]any{"id": dashID, "name": name, "panels": created}), nil, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "create_model",
		Description: "Create a CH-UI SQL model as a draft. Models materialize as a view or table when run from the UI; reference other models with $ref(model_name). Model names are unique per connection.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args createModelArgs) (*mcp.CallToolResult, any, error) {
		name := strings.TrimSpace(args.Name)
		sqlBody := strings.TrimSpace(args.SQL)
		if sqlBody == "" {
			return errResult("sql is required"), nil, nil
		}
		if err := models.ValidateModelName(name); err != nil {
			return errResult("invalid model name: %v", err), nil, nil
		}
		targetDB := strings.TrimSpace(args.TargetDatabase)
		if targetDB == "" {
			targetDB = "default"
		}
		if !dbAllowed(ak.key, targetDB) {
			return errResult("target database %q is not in this key's allowlist", targetDB), nil, nil
		}
		mat := strings.TrimSpace(args.Materialization)
		if mat == "" {
			mat = "view"
		}
		if mat != "view" && mat != "table" {
			return errResult("materialization must be view or table"), nil, nil
		}
		engine, orderBy := "", ""
		if mat == "table" {
			engine = strings.TrimSpace(args.TableEngine)
			if engine == "" {
				engine = "MergeTree"
			}
			orderBy = strings.TrimSpace(args.OrderBy)
			if orderBy == "" {
				orderBy = "tuple()"
			}
		}
		id, err := deps.DB.CreateModel(ak.key.ConnectionID, name, strings.TrimSpace(args.Description), targetDB, mat, sqlBody, engine, orderBy, mcpAuthor(ak))
		if err != nil {
			return errResult("failed to create model (name taken on this connection?): %v", err), nil, nil
		}
		audit(deps, ak, "model.created", "mcp key: "+ak.key.Name+", model: "+name)
		return jsonResult(map[string]any{
			"id": id, "name": name, "status": "draft",
			"note": "created as draft; run it from the Models page to materialize",
		}), nil, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "create_pipeline",
		Description: "Create a CH-UI data pipeline as a draft: one source (kafka, webhook, database, or s3) wired to a ClickHouse sink table. Source settings can be completed in the UI; the pipeline never starts from here.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args createPipelineArgs) (*mcp.CallToolResult, any, error) {
		name := strings.TrimSpace(args.Name)
		if name == "" {
			return errResult("name is required"), nil, nil
		}
		if !pipelineSourceTypes[args.SourceType] {
			return errResult("source_type must be one of: source_kafka, source_webhook, source_database, source_s3"), nil, nil
		}
		targetDB := strings.TrimSpace(args.TargetDatabase)
		targetTable := strings.TrimSpace(args.TargetTable)
		if targetDB == "" || targetTable == "" {
			return errResult("target_database and target_table are required"), nil, nil
		}
		if !dbAllowed(ak.key, targetDB) {
			return errResult("target database %q is not in this key's allowlist", targetDB), nil, nil
		}

		pipelineID, err := deps.DB.CreatePipeline(name, strings.TrimSpace(args.Description), ak.key.ConnectionID, mcpAuthor(ak))
		if err != nil {
			return errResult("failed to create pipeline: %v", err), nil, nil
		}

		sourceCfg := args.SourceConfig
		if sourceCfg == nil {
			sourceCfg = map[string]any{}
		}
		sourceJSON, _ := json.Marshal(sourceCfg)
		sinkJSON, _ := json.Marshal(map[string]any{"database": targetDB, "table": targetTable})

		sourceLabel := strings.TrimPrefix(args.SourceType, "source_")
		nodes := []database.PipelineNode{
			{ID: "source-1", NodeType: args.SourceType, Label: sourceLabel + " source", PositionX: 80, PositionY: 160, ConfigEncrypted: string(sourceJSON)},
			{ID: "sink-1", NodeType: "sink_clickhouse", Label: fmt.Sprintf("%s.%s", targetDB, targetTable), PositionX: 480, PositionY: 160, ConfigEncrypted: string(sinkJSON)},
		}
		edges := []database.PipelineEdge{
			{ID: "edge-1", SourceNodeID: "source-1", TargetNodeID: "sink-1"},
		}
		if err := deps.DB.SavePipelineGraph(pipelineID, nodes, edges, `{"x":0,"y":0,"zoom":1}`); err != nil {
			return errResult("pipeline %q created, but saving its graph failed: %v", name, err), nil, nil
		}
		audit(deps, ak, "pipeline.created", "mcp key: "+ak.key.Name+", pipeline: "+name)
		return jsonResult(map[string]any{
			"id": pipelineID, "name": name, "status": "draft",
			"source": args.SourceType, "sink": targetDB + "." + targetTable,
			"note": "created as draft; review the source settings and start it from the Pipelines page",
		}), nil, nil
	})
}

// registerListTools exposes read-only listings of CH-UI entities so a client
// can see what already exists before creating (model names are unique per
// connection). Available to every key.
func registerListTools(srv *mcp.Server, deps Deps, ak *authedKey) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_saved_queries",
		Description: "List the saved queries in this CH-UI instance (name, description, creator).",
	}, func(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
		queries, err := deps.DB.GetSavedQueries()
		if err != nil {
			return errResult("failed to list saved queries: %v", err), nil, nil
		}
		out := make([]map[string]any, 0, len(queries))
		for _, q := range queries {
			out = append(out, map[string]any{"id": q.ID, "name": q.Name, "description": q.Description, "created_by": q.CreatedBy})
		}
		return jsonResult(map[string]any{"saved_queries": out}), nil, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_dashboards",
		Description: "List the dashboards in this CH-UI instance.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
		dashboards, err := deps.DB.GetDashboards()
		if err != nil {
			return errResult("failed to list dashboards: %v", err), nil, nil
		}
		out := make([]map[string]any, 0, len(dashboards))
		for _, d := range dashboards {
			out = append(out, map[string]any{"id": d.ID, "name": d.Name, "description": d.Description, "created_by": d.CreatedBy})
		}
		return jsonResult(map[string]any{"dashboards": out}), nil, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_models",
		Description: "List the SQL models on this connection (name, status, materialization).",
	}, func(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
		modelRows, err := deps.DB.GetModelsByConnection(ak.key.ConnectionID)
		if err != nil {
			return errResult("failed to list models: %v", err), nil, nil
		}
		out := make([]map[string]any, 0, len(modelRows))
		for _, m := range modelRows {
			out = append(out, map[string]any{
				"id": m.ID, "name": m.Name, "status": m.Status,
				"materialization": m.Materialization, "target_database": m.TargetDatabase,
			})
		}
		return jsonResult(map[string]any{"models": out}), nil, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_pipelines",
		Description: "List the data pipelines in this CH-UI instance (name, status).",
	}, func(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
		pipelines, err := deps.DB.GetPipelines()
		if err != nil {
			return errResult("failed to list pipelines: %v", err), nil, nil
		}
		out := make([]map[string]any, 0, len(pipelines))
		for _, p := range pipelines {
			out = append(out, map[string]any{"id": p.ID, "name": p.Name, "status": p.Status, "created_by": p.CreatedBy})
		}
		return jsonResult(map[string]any{"pipelines": out}), nil, nil
	})
}
