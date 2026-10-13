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
)

func RegisterModelActions(r *Registry) {
	r.Register(runModel)
	r.Register(buildModel)
}

var runModel = Tool{
	Name:             "run_model",
	Description:      "Build a model and its upstream models in the user's ClickHouse, then run their data tests. view: CREATE OR REPLACE VIEW; table: rebuilt through a temp table and swap; incremental: first run builds the table, later runs insert new rows; materialized_view: creates the target table and the <name>__mv trigger (backfilled on first build), later runs update the trigger's query. Same as build_model without full_refresh.",
	RequiresApproval: true,
	Parameters: mustJSON(map[string]any{
		"type":     "object",
		"required": []string{"model_id"},
		"properties": map[string]any{
			"model_id": map[string]any{"type": "string"},
		},
	}),
	Handler: func(tctx Context, args json.RawMessage) (any, error) {
		var in struct {
			ModelID string `json:"model_id"`
		}
		if err := json.Unmarshal(args, &in); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}
		in.ModelID = strings.TrimSpace(in.ModelID)
		if in.ModelID == "" {
			return nil, errors.New("model_id is required")
		}
		if tctx.RunModel == nil {
			return nil, errors.New("run_model is not available in this context")
		}
		runID, err := tctx.RunModel(in.ModelID)
		if err != nil {
			return nil, fmt.Errorf("run model: %w", err)
		}
		return map[string]any{
			"finished": true,
			"model_id": in.ModelID,
			"run_id":   runID,
			"open_url": tctx.AbsURL("/models"),
			"hint":     "The run finished successfully. Run history is on /models.",
		}, nil
	},
}

var buildModel = Tool{
	Name:             "build_model",
	Description:      "Build a model and its upstream models, then run their data tests (not_null, unique, accepted_values, relationship, expression). A failed error-severity test marks the model failed and skips its downstream models; warn tests are only recorded. Pass full_refresh=true to rebuild an incremental or materialized_view model from scratch (needed after its columns change).",
	RequiresApproval: true,
	Parameters: mustJSON(map[string]any{
		"type":     "object",
		"required": []string{"model_id"},
		"properties": map[string]any{
			"model_id":     map[string]any{"type": "string"},
			"full_refresh": map[string]any{"type": "boolean", "description": "Rebuild this model from scratch (incremental and materialized_view)."},
		},
	}),
	Handler: func(tctx Context, args json.RawMessage) (any, error) {
		var in struct {
			ModelID     string `json:"model_id"`
			FullRefresh bool   `json:"full_refresh"`
		}
		if err := json.Unmarshal(args, &in); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}
		in.ModelID = strings.TrimSpace(in.ModelID)
		if in.ModelID == "" {
			return nil, errors.New("model_id is required")
		}
		if tctx.BuildModel == nil {
			return nil, errors.New("build_model is not available in this context")
		}
		runID, err := tctx.BuildModel(in.ModelID, in.FullRefresh)
		if err != nil {
			return nil, fmt.Errorf("build model: %w", err)
		}
		return map[string]any{
			"finished": true,
			"model_id": in.ModelID,
			"run_id":   runID,
			"open_url": tctx.AbsURL("/models"),
			"hint":     "Build and data tests finished successfully. Run history is on /models.",
		}, nil
	},
}
