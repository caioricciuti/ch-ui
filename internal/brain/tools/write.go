// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package tools

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/models"
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

// modelOptionParams are the per-materialization settings create_model and
// update_model accept.
var modelOptionParams = map[string]any{
	"table_engine":         map[string]any{"type": "string", "description": "ClickHouse engine for table, incremental and materialized_view models (default MergeTree)."},
	"order_by":             map[string]any{"type": "string", "description": "ORDER BY for MergeTree-family engines (default tuple())."},
	"partition_by":         map[string]any{"type": "string", "description": "PARTITION BY expression for MergeTree-family engines, e.g. toYYYYMM(event_date). One expression, no ';'."},
	"engine_args":          map[string]any{"type": "string", "description": "Engine arguments without the parentheses, e.g. the version column for ReplacingMergeTree."},
	"incremental_strategy": map[string]any{"type": "string", "enum": []string{models.StrategyAppend, models.StrategyDeleteInsert}, "description": "incremental only. append inserts new rows; delete_insert replaces rows by unique_key (default append)."},
	"unique_key":           map[string]any{"type": "string", "description": "incremental delete_insert only: column or comma-separated columns identifying a row."},
	"watermark_column":     map[string]any{"type": "string", "description": "incremental only: column; later runs only take rows above the table's current max (>= for delete_insert)."},
}

// modelTestParam describes the tests create_model accepts.
var modelTestParam = map[string]any{
	"type":        "array",
	"description": "Data tests run after every successful build. A failed test with severity error fails the model and skips its downstream models; warn only records it.",
	"items": map[string]any{
		"type":     "object",
		"required": []string{"kind"},
		"properties": map[string]any{
			"kind":       map[string]any{"type": "string", "enum": []string{models.TestNotNull, models.TestUnique, models.TestAcceptedValues, models.TestRelationship, models.TestExpression}},
			"column":     map[string]any{"type": "string", "description": "Column under test (all kinds except expression)."},
			"severity":   map[string]any{"type": "string", "enum": []string{models.SeverityError, models.SeverityWarn}},
			"values":     map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "accepted_values: the allowed values, compared as text."},
			"to":         map[string]any{"type": "string", "description": "relationship: parent model name, table in the same database, or database.table."},
			"field":      map[string]any{"type": "string", "description": "relationship: the parent's key column."},
			"expression": map[string]any{"type": "string", "description": "expression: boolean that must hold for every row, e.g. amount >= 0."},
		},
	},
}

type modelTestArg struct {
	Kind       string   `json:"kind"`
	Column     string   `json:"column"`
	Severity   string   `json:"severity"`
	Values     []string `json:"values"`
	To         string   `json:"to"`
	Field      string   `json:"field"`
	Expression string   `json:"expression"`
}

func (a modelTestArg) test(modelID string) (database.ModelTest, error) {
	args, _ := json.Marshal(models.TestArgs{Values: a.Values, To: a.To, Field: a.Field, Expression: a.Expression})
	t := database.ModelTest{ModelID: modelID, Kind: a.Kind, ColumnName: a.Column, Severity: a.Severity, Args: string(args)}
	return t, models.NormalizeTest(&t)
}

func withProps(base map[string]any, extra map[string]any) map[string]any {
	out := make(map[string]any, len(base)+len(extra))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range extra {
		out[k] = v
	}
	return out
}

var createModel = Tool{
	Name: "create_model",
	Description: "Create a SQL model (a SELECT that ClickHouse materializes into the target database). Materializations: " +
		"'view' (recomputed on read); 'table' (rebuilt in full on every run through a temp table and swap); " +
		"'incremental' (first run builds the table, later runs append new rows past watermark_column, or with delete_insert replace rows by unique_key; a full refresh rebuilds it); " +
		"'materialized_view' (a target table fed by <name>__mv on every insert into the source, backfilled when first built). " +
		"Reference other models with $ref(model_name) and the model's own table with $this. Optional tests are data tests run after each build.",
	RequiresApproval: true,
	Parameters: mustJSON(map[string]any{
		"type":     "object",
		"required": []string{"name", "target_database", "materialization", "sql_body"},
		"properties": withProps(modelOptionParams, map[string]any{
			"name":            map[string]any{"type": "string", "description": "Identifier: letters, digits and underscores, not starting with a digit. Also the name of the table or view it builds."},
			"description":     map[string]any{"type": "string"},
			"target_database": map[string]any{"type": "string", "description": "ClickHouse database the model writes to."},
			"materialization": map[string]any{"type": "string", "enum": []string{models.MaterializationView, models.MaterializationTable, models.MaterializationIncremental, models.MaterializationMaterializedView}},
			"sql_body":        map[string]any{"type": "string", "description": "The SELECT body. Write $ref(other_model) (no quotes) to depend on another model, $this for the model's own table."},
			"tests":           modelTestParam,
		}),
	}),
	Handler: func(tctx Context, args json.RawMessage) (any, error) {
		var in struct {
			Name                string         `json:"name"`
			Description         string         `json:"description"`
			TargetDatabase      string         `json:"target_database"`
			Materialization     string         `json:"materialization"`
			SQLBody             string         `json:"sql_body"`
			TableEngine         string         `json:"table_engine"`
			OrderBy             string         `json:"order_by"`
			PartitionBy         string         `json:"partition_by"`
			EngineArgs          string         `json:"engine_args"`
			IncrementalStrategy string         `json:"incremental_strategy"`
			UniqueKey           string         `json:"unique_key"`
			WatermarkColumn     string         `json:"watermark_column"`
			Tests               []modelTestArg `json:"tests"`
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
		if err := models.ValidateModelName(in.Name); err != nil {
			return nil, err
		}
		m := database.Model{
			Name: in.Name, Description: in.Description, TargetDatabase: in.TargetDatabase,
			Materialization: in.Materialization, SQLBody: in.SQLBody, TableEngine: in.TableEngine, OrderBy: in.OrderBy,
			PartitionBy: in.PartitionBy, EngineArgs: in.EngineArgs, IncrementalStrategy: in.IncrementalStrategy,
			UniqueKey: in.UniqueKey, WatermarkColumn: in.WatermarkColumn,
		}
		if err := models.NormalizeModel(&m); err != nil {
			return nil, err
		}
		if !models.BuildsTable(m.Materialization) {
			m.TableEngine, m.OrderBy = "", ""
		}
		tests := make([]database.ModelTest, 0, len(in.Tests))
		for i, a := range in.Tests {
			t, err := a.test("")
			if err != nil {
				return nil, fmt.Errorf("test %d: %w", i+1, err)
			}
			tests = append(tests, t)
		}
		// The model, its options and its tests are written in one transaction.
		creator := tctx.Username
		m.ConnectionID, m.CreatedBy = tctx.ConnectionID, &creator
		id, err := tctx.DB.SaveModel(database.ModelSave{Model: m, ReplaceTests: len(tests) > 0, Tests: tests})
		if err != nil {
			return nil, fmt.Errorf("create model: %w", err)
		}
		return map[string]any{
			"created":  true,
			"model_id": id,
			"name":     in.Name,
			"tests":    len(tests),
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
