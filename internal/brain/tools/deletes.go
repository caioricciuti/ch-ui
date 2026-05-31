package tools

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

func RegisterDeletes(r *Registry) {
	r.Register(deleteDashboard)
	r.Register(deleteDashboardPanel)
	r.Register(deleteModel)
	r.Register(deleteSavedQuery)
	r.Register(deletePipeline)
}

func idArgSchema() json.RawMessage {
	return mustJSON(map[string]any{
		"type":     "object",
		"required": []string{"id"},
		"properties": map[string]any{
			"id": map[string]any{"type": "string"},
		},
	})
}

func decodeIDArg(args json.RawMessage) (string, error) {
	var in struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	id := strings.TrimSpace(in.ID)
	if id == "" {
		return "", errors.New("id is required")
	}
	return id, nil
}

var deleteDashboard = Tool{
	Name:             "delete_dashboard",
	Description:      "Delete a dashboard AND all its panels. Irreversible. Call list_dashboards first if you're not certain of the id.",
	RequiresApproval: true,
	Parameters:       idArgSchema(),
	Handler: func(tctx Context, args json.RawMessage) (any, error) {
		id, err := decodeIDArg(args)
		if err != nil {
			return nil, err
		}
		if err := tctx.DB.DeleteDashboard(id); err != nil {
			return nil, fmt.Errorf("delete dashboard: %w", err)
		}
		return map[string]any{"deleted": true, "dashboard_id": id}, nil
	},
}

var deleteDashboardPanel = Tool{
	Name:             "delete_dashboard_panel",
	Description:      "Delete a single panel from a dashboard. Use after get_dashboard to find the panel id.",
	RequiresApproval: true,
	Parameters:       idArgSchema(),
	Handler: func(tctx Context, args json.RawMessage) (any, error) {
		id, err := decodeIDArg(args)
		if err != nil {
			return nil, err
		}
		if err := tctx.DB.DeletePanel(id); err != nil {
			return nil, fmt.Errorf("delete panel: %w", err)
		}
		return map[string]any{"deleted": true, "panel_id": id}, nil
	},
}

var deleteModel = Tool{
	Name:             "delete_model",
	Description:      "Delete a dbt-style model. Does NOT drop the target ClickHouse table — the user must do that separately if desired.",
	RequiresApproval: true,
	Parameters:       idArgSchema(),
	Handler: func(tctx Context, args json.RawMessage) (any, error) {
		id, err := decodeIDArg(args)
		if err != nil {
			return nil, err
		}
		if err := tctx.DB.DeleteModel(id); err != nil {
			return nil, fmt.Errorf("delete model: %w", err)
		}
		return map[string]any{"deleted": true, "model_id": id}, nil
	},
}

var deleteSavedQuery = Tool{
	Name:             "delete_saved_query",
	Description:      "Delete a saved query.",
	RequiresApproval: true,
	Parameters:       idArgSchema(),
	Handler: func(tctx Context, args json.RawMessage) (any, error) {
		id, err := decodeIDArg(args)
		if err != nil {
			return nil, err
		}
		if err := tctx.DB.DeleteSavedQuery(id); err != nil {
			return nil, fmt.Errorf("delete saved query: %w", err)
		}
		return map[string]any{"deleted": true, "saved_query_id": id}, nil
	},
}

var deletePipeline = Tool{
	Name:             "delete_pipeline",
	Description:      "Delete a pipeline AND its graph (nodes/edges/runs). Stop the pipeline first if it's running.",
	RequiresApproval: true,
	Parameters:       idArgSchema(),
	Handler: func(tctx Context, args json.RawMessage) (any, error) {
		id, err := decodeIDArg(args)
		if err != nil {
			return nil, err
		}
		if err := tctx.DB.DeletePipeline(id); err != nil {
			return nil, fmt.Errorf("delete pipeline: %w", err)
		}
		return map[string]any{"deleted": true, "pipeline_id": id}, nil
	},
}
