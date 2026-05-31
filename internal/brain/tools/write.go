package tools

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/caioricciuti/ch-ui/internal/database"
)

func RegisterWrite(r *Registry) {
	r.Register(createSavedQuery)
	r.Register(createModel)
	r.Register(createDashboard)
	r.Register(createPipeline)
}

var createSavedQuery = Tool{
	Name:             "create_saved_query",
	Description:      "Save a SQL query to the user's library so they can re-run it from the Saved Queries page. The user must approve before this runs.",
	RequiresApproval: true,
	Parameters: mustJSON(map[string]any{
		"type":     "object",
		"required": []string{"name", "sql"},
		"properties": map[string]any{
			"name":        map[string]any{"type": "string", "description": "Short display name for the query."},
			"sql":         map[string]any{"type": "string", "description": "The full SQL."},
			"description": map[string]any{"type": "string", "description": "Optional one-line description."},
		},
	}),
	Handler: func(tctx Context, args json.RawMessage) (any, error) {
		var in struct {
			Name        string `json:"name"`
			SQL         string `json:"sql"`
			Description string `json:"description"`
		}
		if err := json.Unmarshal(args, &in); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}
		in.Name = strings.TrimSpace(in.Name)
		in.SQL = strings.TrimSpace(in.SQL)
		if in.Name == "" || in.SQL == "" {
			return nil, errors.New("name and sql are required")
		}
		id, err := tctx.DB.CreateSavedQuery(database.CreateSavedQueryParams{
			Name:         in.Name,
			Description:  strings.TrimSpace(in.Description),
			Query:        in.SQL,
			ConnectionID: tctx.ConnectionID,
			CreatedBy:    tctx.Username,
		})
		if err != nil {
			return nil, fmt.Errorf("create saved query: %w", err)
		}
		return map[string]any{
			"created":        true,
			"saved_query_id": id,
			"name":           in.Name,
			"open_url":       tctx.AbsURL("/saved-queries"),
		}, nil
	},
}

var createModel = Tool{
	Name:             "create_model",
	Description:      "Create a dbt-style data model (a SQL transformation that ClickHouse runs to produce a target table or view). Choose materialization based on the use case: 'view' (logical, recomputes on read), 'table' (full rebuild), 'incremental' (append new rows by watermark), or 'materialized_view' (CH materialized view triggered by inserts).",
	RequiresApproval: true,
	Parameters: mustJSON(map[string]any{
		"type":     "object",
		"required": []string{"name", "target_database", "materialization", "sql_body"},
		"properties": map[string]any{
			"name":               map[string]any{"type": "string"},
			"description":        map[string]any{"type": "string"},
			"target_database":    map[string]any{"type": "string", "description": "ClickHouse database the model writes to."},
			"materialization":    map[string]any{"type": "string", "enum": []string{"view", "table", "incremental", "materialized_view"}},
			"sql_body":           map[string]any{"type": "string", "description": "The SELECT body. Use ref('other_model') if you depend on another model."},
			"table_engine":       map[string]any{"type": "string", "description": "ClickHouse engine for table/incremental/MV (e.g., MergeTree)."},
			"order_by":           map[string]any{"type": "string", "description": "ORDER BY clause for table/incremental/MV."},
			"incremental_column": map[string]any{"type": "string", "description": "Watermark column for incremental (required if materialization=incremental)."},
			"unique_key":         map[string]any{"type": "string", "description": "Optional unique key for incremental dedupe."},
		},
	}),
	Handler: func(tctx Context, args json.RawMessage) (any, error) {
		var in struct {
			Name              string `json:"name"`
			Description       string `json:"description"`
			TargetDatabase    string `json:"target_database"`
			Materialization   string `json:"materialization"`
			SQLBody           string `json:"sql_body"`
			TableEngine       string `json:"table_engine"`
			OrderBy           string `json:"order_by"`
			IncrementalColumn string `json:"incremental_column"`
			UniqueKey         string `json:"unique_key"`
		}
		if err := json.Unmarshal(args, &in); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}
		in.Name = strings.TrimSpace(in.Name)
		in.TargetDatabase = strings.TrimSpace(in.TargetDatabase)
		in.Materialization = strings.TrimSpace(in.Materialization)
		in.SQLBody = strings.TrimSpace(in.SQLBody)
		if in.Name == "" || in.TargetDatabase == "" || in.Materialization == "" || in.SQLBody == "" {
			return nil, errors.New("name, target_database, materialization, sql_body are required")
		}
		switch in.Materialization {
		case "view", "table", "incremental", "materialized_view":
		default:
			return nil, fmt.Errorf("invalid materialization: %s", in.Materialization)
		}
		if (in.Materialization == "table" || in.Materialization == "incremental" || in.Materialization == "materialized_view") && in.TableEngine == "" {
			in.TableEngine = "MergeTree"
		}
		id, err := tctx.DB.CreateModel(
			tctx.ConnectionID, in.Name, in.Description, in.TargetDatabase, in.Materialization,
			in.SQLBody, in.TableEngine, in.OrderBy,
			tctx.Username,
		)
		if err != nil {
			return nil, fmt.Errorf("create model: %w", err)
		}
		return map[string]any{
			"created":  true,
			"model_id": id,
			"name":     in.Name,
			"open_url": tctx.AbsURL("/models"),
			"hint":     "Model created. Propose build_model(model_id) in the same turn so the user sees data immediately.",
		}, nil
	},
}

var createDashboard = Tool{
	Name:             "create_dashboard",
	Description:      "Create an empty dashboard the user can then add charts to. Use this when the user asks for a dashboard but you don't yet have specific charts in mind, or as a first step before suggesting panels.",
	RequiresApproval: true,
	Parameters: mustJSON(map[string]any{
		"type":     "object",
		"required": []string{"name"},
		"properties": map[string]any{
			"name":        map[string]any{"type": "string"},
			"description": map[string]any{"type": "string"},
		},
	}),
	Handler: func(tctx Context, args json.RawMessage) (any, error) {
		var in struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		}
		if err := json.Unmarshal(args, &in); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}
		in.Name = strings.TrimSpace(in.Name)
		if in.Name == "" {
			return nil, errors.New("name is required")
		}
		id, err := tctx.DB.CreateDashboard(in.Name, strings.TrimSpace(in.Description), tctx.Username)
		if err != nil {
			return nil, fmt.Errorf("create dashboard: %w", err)
		}
		return map[string]any{
			"created":      true,
			"dashboard_id": id,
			"name":         in.Name,
			"open_url":     tctx.AbsURL("/dashboards/" + id),
			"hint":         "Empty dashboard created. CHAIN add_dashboard_panel calls right now to fill it — do NOT stop and ask the user what charts to add.",
		}, nil
	},
}

var createPipeline = Tool{
	Name:             "create_pipeline",
	Description:      "Create a new ingestion pipeline (draft, empty config). Use this when the user wants to ingest data from a source (Kafka, S3, webhook, database). They'll wire up the source + destination in the Pipelines page.",
	RequiresApproval: true,
	Parameters: mustJSON(map[string]any{
		"type":     "object",
		"required": []string{"name"},
		"properties": map[string]any{
			"name":        map[string]any{"type": "string"},
			"description": map[string]any{"type": "string"},
		},
	}),
	Handler: func(tctx Context, args json.RawMessage) (any, error) {
		var in struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		}
		if err := json.Unmarshal(args, &in); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}
		in.Name = strings.TrimSpace(in.Name)
		if in.Name == "" {
			return nil, errors.New("name is required")
		}
		id, err := tctx.DB.CreatePipeline(in.Name, strings.TrimSpace(in.Description), tctx.ConnectionID, tctx.Username)
		if err != nil {
			return nil, fmt.Errorf("create pipeline: %w", err)
		}
		return map[string]any{
			"created":     true,
			"pipeline_id": id,
			"name":        in.Name,
			"open_url":    tctx.AbsURL("/pipelines/" + id),
			"hint":        "Empty draft pipeline created. The user finishes wiring source + destination in the Pipelines page.",
		}, nil
	},
}
