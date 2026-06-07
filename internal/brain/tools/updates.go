package tools

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/caioricciuti/ch-ui/internal/database"
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
		current, err := tctx.DB.GetSavedQueryByID(in.ID)
		if err != nil {
			return nil, fmt.Errorf("lookup saved query: %w", err)
		}
		if current == nil {
			return nil, fmt.Errorf("saved query %s not found", in.ID)
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
		}
		if current.Parameters != nil {
			params.Parameters = *current.Parameters
		}
		if err := tctx.DB.UpdateSavedQuery(in.ID, params); err != nil {
			return nil, fmt.Errorf("update saved query: %w", err)
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
	Description:      "Update an existing model. Unspecified fields keep their current value. Use list_models to find the id; propose build_model in the same turn so the user sees the new output.",
	RequiresApproval: true,
	Parameters: mustJSON(map[string]any{
		"type":     "object",
		"required": []string{"id"},
		"properties": map[string]any{
			"id":              map[string]any{"type": "string"},
			"name":            map[string]any{"type": "string"},
			"description":     map[string]any{"type": "string"},
			"target_database": map[string]any{"type": "string"},
			"materialization": map[string]any{"type": "string", "enum": []string{"view", "table", "incremental", "materialized_view"}},
			"sql_body":        map[string]any{"type": "string"},
			"table_engine":    map[string]any{"type": "string"},
			"order_by":        map[string]any{"type": "string"},
		},
	}),
	Handler: func(tctx Context, args json.RawMessage) (any, error) {
		var in struct {
			ID              string `json:"id"`
			Name            string `json:"name"`
			Description     string `json:"description"`
			TargetDatabase  string `json:"target_database"`
			Materialization string `json:"materialization"`
			SQLBody         string `json:"sql_body"`
			TableEngine     string `json:"table_engine"`
			OrderBy         string `json:"order_by"`
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
		if current == nil {
			return nil, fmt.Errorf("model %s not found", in.ID)
		}
		pick := func(new, old string) string {
			if strings.TrimSpace(new) != "" {
				return new
			}
			return old
		}
		err = tctx.DB.UpdateModel(
			in.ID,
			pick(in.Name, current.Name),
			pick(in.Description, current.Description),
			pick(in.TargetDatabase, current.TargetDatabase),
			pick(in.Materialization, current.Materialization),
			pick(in.SQLBody, current.SQLBody),
			pick(in.TableEngine, current.TableEngine),
			pick(in.OrderBy, current.OrderBy),
		)
		if err != nil {
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
