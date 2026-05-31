package tools

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

func RegisterAwareness(r *Registry) {
	r.Register(listDashboards)
	r.Register(getDashboard)
	r.Register(listSavedQueries)
	r.Register(listModels)
	r.Register(listPipelines)
}

var listDashboards = Tool{
	Name:        "list_dashboards",
	Description: "List every dashboard in the workspace (id, name, description, updated_at). ALWAYS call this before create_dashboard so you don't duplicate one that already exists.",
	Parameters:  mustJSON(map[string]any{"type": "object", "properties": map[string]any{}}),
	Handler: func(tctx Context, _ json.RawMessage) (any, error) {
		ds, err := tctx.DB.GetDashboards()
		if err != nil {
			return nil, fmt.Errorf("list dashboards: %w", err)
		}
		out := make([]map[string]any, 0, len(ds))
		for _, d := range ds {
			row := map[string]any{
				"id":         d.ID,
				"name":       d.Name,
				"updated_at": d.UpdatedAt,
			}
			if d.Description != nil {
				row["description"] = *d.Description
			}
			out = append(out, row)
		}
		return map[string]any{"dashboards": out}, nil
	},
}

var getDashboard = Tool{
	Name:        "get_dashboard",
	Description: "Get one dashboard with its panels (panel_type, name, sql, layout). Use this before add_dashboard_panel to avoid duplicating panels. IMPORTANT: the id parameter must be the UUID returned by list_dashboards, NOT the dashboard name.",
	Parameters: mustJSON(map[string]any{
		"type":     "object",
		"required": []string{"id"},
		"properties": map[string]any{
			"id": map[string]any{"type": "string", "description": "The dashboard UUID from list_dashboards (e.g. '550e8400-e29b-41d4-a716-446655440000'), not the name."},
		},
	}),
	Handler: func(tctx Context, args json.RawMessage) (any, error) {
		var in struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(args, &in); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}
		in.ID = strings.TrimSpace(in.ID)
		if in.ID == "" {
			return nil, errors.New("id is required")
		}
		d, err := tctx.DB.GetDashboardByID(in.ID)
		if err != nil {
			return nil, fmt.Errorf("get dashboard: %w", err)
		}
		if d == nil {
			return nil, fmt.Errorf("dashboard %s not found", in.ID)
		}
		panels, err := tctx.DB.GetPanelsByDashboard(in.ID)
		if err != nil {
			return nil, fmt.Errorf("get panels: %w", err)
		}
		ps := make([]map[string]any, 0, len(panels))
		for _, p := range panels {
			ps = append(ps, map[string]any{
				"id":         p.ID,
				"name":       p.Name,
				"panel_type": p.PanelType,
				"sql":        p.Query,
				"layout":     map[string]int{"x": p.LayoutX, "y": p.LayoutY, "w": p.LayoutW, "h": p.LayoutH},
			})
		}
		result := map[string]any{
			"id":     d.ID,
			"name":   d.Name,
			"panels": ps,
		}
		if d.Description != nil {
			result["description"] = *d.Description
		}
		return result, nil
	},
}

var listSavedQueries = Tool{
	Name:        "list_saved_queries",
	Description: "List every saved query (id, name, description, updated_at). Check this before create_saved_query.",
	Parameters:  mustJSON(map[string]any{"type": "object", "properties": map[string]any{}}),
	Handler: func(tctx Context, _ json.RawMessage) (any, error) {
		qs, err := tctx.DB.GetSavedQueries()
		if err != nil {
			return nil, fmt.Errorf("list saved queries: %w", err)
		}
		out := make([]map[string]any, 0, len(qs))
		for _, q := range qs {
			row := map[string]any{
				"id":         q.ID,
				"name":       q.Name,
				"updated_at": q.UpdatedAt,
			}
			if q.Description != nil {
				row["description"] = *q.Description
			}
			out = append(out, row)
		}
		return map[string]any{"saved_queries": out}, nil
	},
}

var listModels = Tool{
	Name:        "list_models",
	Description: "List every dbt-style model in the workspace (id, name, target_database, materialization, status). Check before create_model and use these ids for run_model / build_model.",
	Parameters:  mustJSON(map[string]any{"type": "object", "properties": map[string]any{}}),
	Handler: func(tctx Context, _ json.RawMessage) (any, error) {
		ms, err := tctx.DB.GetModelsByConnection(tctx.ConnectionID)
		if err != nil {
			return nil, fmt.Errorf("list models: %w", err)
		}
		out := make([]map[string]any, 0, len(ms))
		for _, m := range ms {
			out = append(out, map[string]any{
				"id":              m.ID,
				"name":            m.Name,
				"target_database": m.TargetDatabase,
				"materialization": m.Materialization,
				"status":          m.Status,
				"updated_at":      m.UpdatedAt,
			})
		}
		return map[string]any{"models": out}, nil
	},
}

var listPipelines = Tool{
	Name:        "list_pipelines",
	Description: "List every ingestion pipeline (id, name, status, updated_at). Check before create_pipeline.",
	Parameters:  mustJSON(map[string]any{"type": "object", "properties": map[string]any{}}),
	Handler: func(tctx Context, _ json.RawMessage) (any, error) {
		ps, err := tctx.DB.GetPipelines()
		if err != nil {
			return nil, fmt.Errorf("list pipelines: %w", err)
		}
		out := make([]map[string]any, 0, len(ps))
		for _, p := range ps {
			row := map[string]any{
				"id":         p.ID,
				"name":       p.Name,
				"status":     p.Status,
				"updated_at": p.UpdatedAt,
			}
			if p.Description != nil {
				row["description"] = *p.Description
			}
			out = append(out, row)
		}
		return map[string]any{"pipelines": out}, nil
	},
}
