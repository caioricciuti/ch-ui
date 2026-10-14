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

func RegisterUpdates(r *Registry) {
	r.Register(updateSavedQuery)
	r.Register(updateModel)
}

var updateSavedQuery = Tool{
	Name:             "update_saved_query",
	Description:      "Update an existing saved query (name, description, SQL). Use list_saved_queries to find the id.",
	RequiresApproval: true,
	Parameters: mustJSON(map[string]any{
		"type":     "object",
		"required": []string{"id"},
		"properties": map[string]any{
			"id":          map[string]any{"type": "string"},
			"name":        map[string]any{"type": "string"},
			"description": map[string]any{"type": "string"},
			"sql":         map[string]any{"type": "string"},
		},
	}),
	Handler: func(tctx Context, args json.RawMessage) (any, error) {
		var in struct {
			ID          string `json:"id"`
			Name        string `json:"name"`
			Description string `json:"description"`
			SQL         string `json:"sql"`
		}
		if err := json.Unmarshal(args, &in); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}
		in.ID = strings.TrimSpace(in.ID)
		if in.ID == "" {
			return nil, errors.New("id is required")
		}
		viewer := savedQueryViewer(tctx)
		current, err := tctx.DB.GetSavedQueryVisibleTo(in.ID, viewer)
		if err != nil {
			return nil, fmt.Errorf("lookup saved query: %w", err)
		}
		if current == nil {
			return nil, fmt.Errorf("saved query %s not found", in.ID)
		}
		if !database.CanEditSavedQuery(current, viewer) {
			return nil, errors.New("only the owner or an admin can change this saved query")
		}
		name := in.Name
		if name == "" {
			name = current.Name
		}
		desc := in.Description
		if desc == "" && current.Description != nil {
			desc = *current.Description
		}
		sql := in.SQL
		if sql == "" {
			sql = current.Query
		}
		connID := ""
		if current.ConnectionID != nil {
			connID = *current.ConnectionID
		}
		params := database.UpdateSavedQueryParams{
			Name:         name,
			Description:  desc,
			Query:        sql,
			ConnectionID: connID,
			Verified:     current.Verified,
		}
		if current.Parameters != nil {
			params.Parameters = *current.Parameters
		}
		if err := tctx.DB.EnsureSavedQueryBaseline(in.ID); err != nil {
			return nil, fmt.Errorf("record baseline version: %w", err)
		}
		if err := tctx.DB.UpdateSavedQuery(in.ID, params); err != nil {
			return nil, fmt.Errorf("update saved query: %w", err)
		}
		if _, err := tctx.DB.RecordSavedQueryVersion(in.ID, tctx.Username, "Updated by Brain"); err != nil {
			return nil, fmt.Errorf("record version: %w", err)
		}
		return map[string]any{
			"updated":        true,
			"saved_query_id": in.ID,
			"open_url":       tctx.AbsURL("/saved-queries"),
		}, nil
	},
}

var updateModel = Tool{
	Name:             "update_model",
	Description:      "Update an existing model. Unspecified fields keep their current value. Materializations: view, table, incremental (append or delete_insert, with optional watermark_column), materialized_view. Changing an incremental or materialized_view model's columns needs a full refresh (build_model with full_refresh). Use list_models to find the id; propose build_model in the same turn so the user sees the new output.",
	RequiresApproval: true,
	Parameters: mustJSON(map[string]any{
		"type":     "object",
		"required": []string{"id"},
		"properties": withProps(modelOptionParams, map[string]any{
			"id":              map[string]any{"type": "string"},
			"name":            map[string]any{"type": "string"},
			"description":     map[string]any{"type": "string"},
			"target_database": map[string]any{"type": "string"},
			"materialization": map[string]any{"type": "string", "enum": []string{models.MaterializationView, models.MaterializationTable, models.MaterializationIncremental, models.MaterializationMaterializedView}},
			"sql_body":        map[string]any{"type": "string"},
		}),
	}),
	Handler: func(tctx Context, args json.RawMessage) (any, error) {
		var in struct {
			ID                  string  `json:"id"`
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
		if err := json.Unmarshal(args, &in); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}
		in.ID = strings.TrimSpace(in.ID)
		if in.ID == "" {
			return nil, errors.New("id is required")
		}
		current, err := tctx.DB.GetModelByID(in.ID)
		if err != nil {
			return nil, fmt.Errorf("lookup model: %w", err)
		}
		if current == nil || current.ConnectionID != tctx.ConnectionID {
			return nil, fmt.Errorf("model %s not found", in.ID)
		}
		if current.Source == "github" {
			return nil, errors.New("this model is managed by GitHub; edit it in the repository")
		}
		pick := func(new, old string) string {
			if strings.TrimSpace(new) != "" {
				return new
			}
			return old
		}
		opt := func(new *string, old string) string {
			if new != nil {
				return *new
			}
			return old
		}
		m := *current
		m.Name = pick(in.Name, current.Name)
		m.Description = pick(in.Description, current.Description)
		m.TargetDatabase = pick(in.TargetDatabase, current.TargetDatabase)
		m.Materialization = pick(in.Materialization, current.Materialization)
		m.SQLBody = pick(in.SQLBody, current.SQLBody)
		m.TableEngine = pick(in.TableEngine, current.TableEngine)
		m.OrderBy = pick(in.OrderBy, current.OrderBy)
		m.PartitionBy = opt(in.PartitionBy, current.PartitionBy)
		m.EngineArgs = opt(in.EngineArgs, current.EngineArgs)
		m.IncrementalStrategy = opt(in.IncrementalStrategy, current.IncrementalStrategy)
		m.UniqueKey = opt(in.UniqueKey, current.UniqueKey)
		m.WatermarkColumn = opt(in.WatermarkColumn, current.WatermarkColumn)
		if err := models.ValidateModelName(m.Name); err != nil {
			return nil, err
		}
		if err := models.NormalizeModel(&m); err != nil {
			return nil, err
		}
		// One transaction; a rename rewrites the pipeline selectors naming
		// the old name.
		if _, err := tctx.DB.SaveModel(database.ModelSave{Model: m, RenameInSelector: models.RenameInSelector}); err != nil {
			return nil, fmt.Errorf("update model: %w", err)
		}
		return map[string]any{
			"updated":  true,
			"model_id": in.ID,
			"open_url": tctx.AbsURL("/models"),
			"hint":     "Updated. Propose build_model(model_id) so the user sees the change materialized.",
		}, nil
	},
}
