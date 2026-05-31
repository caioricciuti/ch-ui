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
	Description:      "Materialize a single model — execute its SQL against the user's ClickHouse (CREATE OR REPLACE / INSERT depending on materialization). Use this after create_model so the user immediately sees results. Skips tests.",
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
			"started":  true,
			"model_id": in.ModelID,
			"run_id":   runID,
			"open_url": tctx.AbsURL("/models"),
			"hint":     "Run is in-flight. Check /models for the run history.",
		}, nil
	},
}

var buildModel = Tool{
	Name:             "build_model",
	Description:      "Materialize a model AND execute its data tests (dbt-style build). Same as run_model plus test execution. Prefer this over run_model when the model has tests.",
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
		if tctx.BuildModel == nil {
			return nil, errors.New("build_model is not available in this context")
		}
		runID, err := tctx.BuildModel(in.ModelID)
		if err != nil {
			return nil, fmt.Errorf("build model: %w", err)
		}
		return map[string]any{
			"started":  true,
			"model_id": in.ModelID,
			"run_id":   runID,
			"open_url": tctx.AbsURL("/models"),
			"hint":     "Build (run + tests) is in-flight. Check /models for the run history.",
		}, nil
	},
}
