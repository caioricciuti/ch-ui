// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package tools

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/caioricciuti/ch-ui/internal/database"
)

func RegisterPanel(r *Registry) {
	r.Register(addDashboardPanel)
}

var supportedPanelTypes = map[string]bool{
	"table":      true,
	"timeseries": true,
	"bar":        true,
	"pie":        true,
	"stat":       true,
	"gauge":      true,
	"text":       true,
	"histogram":  true,
	"scatter":    true,
	"heatmap":    true,
}

var addDashboardPanel = Tool{
	Name: "add_dashboard_panel",
	Description: `Add a chart/panel to an existing dashboard. Always call get_dashboard first to see current panels (so layout doesn't collide and you don't duplicate).

panel_type options:
- "timeseries" → time-series line chart. Needs sql, x_column (time), y_columns (numeric). Optional fill (line|area) and stacked (true stacks the series on each other).
- "bar" → bar chart. Needs sql, x_column, y_columns.
- "pie" → pie chart. Needs sql, pie_label_column (text), pie_value_column (numeric).
- "stat" → single big number. Needs sql, stat_field, optional stat_calculation (last|first|min|max|mean|sum, default last).
- "gauge" → gauge. Needs sql, stat_field, optional gauge_min/gauge_max.
- "table" → raw table. Needs sql.
- "text" → markdown note. Needs content (no sql).
- "histogram" → distribution of one numeric column, bucketed in the browser. Needs sql returning raw values (not pre-aggregated), histogram_column, optional histogram_buckets (default 30).
- "scatter" → one point per row. Needs sql, scatter_x and scatter_y (numeric), optional scatter_size (numeric) and scatter_series (text column that colors points). Up to 10,000 points are drawn.
- "heatmap" → grid of cells colored by value. Needs sql, heatmap_x and heatmap_y (any type, e.g. day and hour), optional heatmap_value (numeric, summed per cell; rows are counted without one).

The SQL should reference a real ClickHouse table — describe_table or run_query to validate it first.
Layout defaults to width 6, height 4 on a 12-col grid. Pass x/y to position; omit and we'll stack vertically.`,
	RequiresApproval: true,
	Parameters: mustJSON(map[string]any{
		"type":     "object",
		"required": []string{"dashboard_id", "name", "panel_type"},
		"properties": map[string]any{
			"dashboard_id":      map[string]any{"type": "string"},
			"name":              map[string]any{"type": "string"},
			"description":       map[string]any{"type": "string"},
			"panel_type":        map[string]any{"type": "string", "enum": panelTypeEnum},
			"sql":               map[string]any{"type": "string", "description": "ClickHouse query (omit for text panels)."},
			"x_column":          map[string]any{"type": "string", "description": "Column used as X axis (timeseries/bar)."},
			"y_columns":         map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Numeric columns to plot (timeseries/bar)."},
			"bar_mode":          map[string]any{"type": "string", "enum": []string{"grouped", "stacked"}},
			"fill":              map[string]any{"type": "string", "enum": []string{"line", "area"}, "description": "Timeseries only: a line (default) or a filled area."},
			"stacked":           map[string]any{"type": "boolean", "description": "Timeseries only: stack the series on each other."},
			"histogram_column":  map[string]any{"type": "string", "description": "Numeric column to bucket (histogram)."},
			"histogram_buckets": map[string]any{"type": "integer", "minimum": 1, "maximum": 200, "description": "Bucket count (histogram). Default 30."},
			"scatter_x":         map[string]any{"type": "string", "description": "X column (scatter)."},
			"scatter_y":         map[string]any{"type": "string", "description": "Y column (scatter)."},
			"scatter_size":      map[string]any{"type": "string", "description": "Numeric column that sizes points (scatter)."},
			"scatter_series":    map[string]any{"type": "string", "description": "Column whose values color points (scatter)."},
			"heatmap_x":         map[string]any{"type": "string", "description": "Column for heatmap columns."},
			"heatmap_y":         map[string]any{"type": "string", "description": "Column for heatmap rows."},
			"heatmap_value":     map[string]any{"type": "string", "description": "Numeric column summed per cell (heatmap)."},
			"pie_label_column":  map[string]any{"type": "string"},
			"pie_value_column":  map[string]any{"type": "string"},
			"pie_donut":         map[string]any{"type": "boolean"},
			"stat_field":        map[string]any{"type": "string"},
			"stat_calculation":  map[string]any{"type": "string", "enum": []string{"last", "first", "min", "max", "mean", "sum", "count"}},
			"stat_unit":         map[string]any{"type": "string", "enum": []string{"none", "percent", "ms", "s", "bytes", "kb", "mb", "gb", "currency"}},
			"stat_prefix":       map[string]any{"type": "string"},
			"stat_suffix":       map[string]any{"type": "string"},
			"gauge_min":         map[string]any{"type": "number"},
			"gauge_max":         map[string]any{"type": "number"},
			"content":           map[string]any{"type": "string", "description": "Markdown body (text panels only)."},
			"layout_x":          map[string]any{"type": "integer", "minimum": 0, "maximum": 12},
			"layout_y":          map[string]any{"type": "integer", "minimum": 0},
			"layout_w":          map[string]any{"type": "integer", "minimum": 1, "maximum": 12, "description": "Width on the 12-col grid. Default 6."},
			"layout_h":          map[string]any{"type": "integer", "minimum": 1, "description": "Height. Default 4."},
		},
	}),
	Handler: func(tctx Context, args json.RawMessage) (any, error) {
		var in panelArgs
		if err := json.Unmarshal(args, &in); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}
		in.DashboardID = strings.TrimSpace(in.DashboardID)
		in.Name = strings.TrimSpace(in.Name)
		in.PanelType = strings.TrimSpace(in.PanelType)
		if in.DashboardID == "" || in.Name == "" || in.PanelType == "" {
			return nil, errors.New("dashboard_id, name, panel_type are required")
		}
		if !supportedPanelTypes[in.PanelType] {
			return nil, fmt.Errorf("unsupported panel_type: %s", in.PanelType)
		}

		d, err := tctx.DB.GetDashboardByID(in.DashboardID)
		if err != nil {
			return nil, fmt.Errorf("get dashboard: %w", err)
		}
		if d == nil {
			return nil, fmt.Errorf("dashboard %s not found", in.DashboardID)
		}
		existing, _ := tctx.DB.GetPanelsByDashboard(in.DashboardID)

		var inferenceNotes []string
		if in.PanelType == "text" {
			if strings.TrimSpace(in.Content) == "" {
				return nil, errors.New("text panels require content")
			}
		} else {
			if strings.TrimSpace(in.SQL) == "" {
				return nil, errors.New("sql is required for this panel_type")
			}
			meta, peekErr := peekSQLMeta(tctx, in.SQL)
			if peekErr != nil {
				return nil, fmt.Errorf("preview SQL failed: %w. Fix the SQL first (try run_query) and try again", peekErr)
			}
			if len(meta) == 0 {
				return nil, errors.New("SQL produced no columns")
			}
			dateCols, numCols, strCols := classifyColumns(meta)
			availableSummary := summarizeColumns(meta)

			switch in.PanelType {
			case "timeseries", "bar":
				if in.XColumn == "" {
					switch {
					case len(dateCols) > 0:
						in.XColumn = dateCols[0]
						inferenceNotes = append(inferenceNotes, "x_column auto-picked: "+in.XColumn)
					case in.PanelType == "bar" && len(strCols) > 0:
						in.XColumn = strCols[0]
						inferenceNotes = append(inferenceNotes, "x_column auto-picked: "+in.XColumn)
					case in.PanelType == "timeseries" && len(strCols) == 0 && len(dateCols) == 0 && len(numCols) > 0:
						in.PanelType = "bar"
						in.XColumn = meta[0].Name
						inferenceNotes = append(inferenceNotes, "downgraded to bar (no datetime column found); x_column="+in.XColumn)
					default:
						return nil, fmt.Errorf("no usable x_column found in SQL result. Available columns: %s. Pass x_column explicitly or rewrite the SQL", availableSummary)
					}
				}
				if len(in.YColumns) == 0 {
					var ys []string
					for _, c := range numCols {
						if c != in.XColumn {
							ys = append(ys, c)
						}
					}
					if len(ys) == 0 {
						return nil, fmt.Errorf("no numeric y_columns found in SQL result. Available columns: %s. Pass y_columns explicitly or rewrite the SQL to include a numeric metric", availableSummary)
					}
					if len(ys) > 4 {
						ys = ys[:4]
					}
					in.YColumns = ys
					inferenceNotes = append(inferenceNotes, "y_columns auto-picked: "+strings.Join(ys, ", "))
				}
				if in.PanelType == "timeseries" && !containsString(dateCols, in.XColumn) {
					in.PanelType = "bar"
					inferenceNotes = append(inferenceNotes, "downgraded to bar (x_column '"+in.XColumn+"' is not a datetime)")
				}
			case "pie":
				if in.PieLabelColumn == "" {
					if len(strCols) > 0 {
						in.PieLabelColumn = strCols[0]
						inferenceNotes = append(inferenceNotes, "pie_label_column auto-picked: "+in.PieLabelColumn)
					} else if len(meta) > 0 {
						in.PieLabelColumn = meta[0].Name
						inferenceNotes = append(inferenceNotes, "pie_label_column auto-picked: "+in.PieLabelColumn)
					} else {
						return nil, fmt.Errorf("no usable pie_label_column. Available columns: %s", availableSummary)
					}
				}
				if in.PieValueColumn == "" {
					if len(numCols) > 0 {
						in.PieValueColumn = numCols[0]
						inferenceNotes = append(inferenceNotes, "pie_value_column auto-picked: "+in.PieValueColumn)
					} else {
						return nil, fmt.Errorf("no numeric column for pie_value_column. Available columns: %s", availableSummary)
					}
				}
			case "histogram", "scatter", "heatmap":
				inferenceNotes = append(inferenceNotes, pickChartColumns(&in, dateCols, numCols, strCols)...)
				if err := checkChartColumns(in, meta); err != nil {
					return nil, fmt.Errorf("%w. Available columns: %s", err, availableSummary)
				}
			case "stat", "gauge":
				if in.StatField == "" {
					if len(numCols) > 0 {
						in.StatField = numCols[0]
						inferenceNotes = append(inferenceNotes, "stat_field auto-picked: "+in.StatField)
					} else {
						return nil, fmt.Errorf("no numeric column for stat_field. Available columns: %s", availableSummary)
					}
				}
			}
		}

		configJSON, _ := json.Marshal(panelConfigFor(in))

		x, y, wd, ht := 0, autoNextY(existing), 6, 4
		if in.LayoutX != nil {
			x = clamp(*in.LayoutX, 0, 11)
		}
		if in.LayoutY != nil {
			y = *in.LayoutY
			if y < 0 {
				y = 0
			}
		}
		if in.LayoutW != nil {
			wd = clamp(*in.LayoutW, 1, 12)
		}
		if in.LayoutH != nil {
			ht = *in.LayoutH
			if ht < 1 {
				ht = 1
			}
		}

		id, err := tctx.DB.CreatePanel(
			in.DashboardID, in.Name, strings.TrimSpace(in.Description),
			in.PanelType, in.SQL, tctx.ConnectionID, string(configJSON),
			x, y, wd, ht,
		)
		if err != nil {
			return nil, fmt.Errorf("create panel: %w", err)
		}
		result := map[string]any{
			"created":      true,
			"panel_id":     id,
			"dashboard_id": in.DashboardID,
			"name":         in.Name,
			"panel_type":   in.PanelType,
			"open_url":     tctx.AbsURL("/dashboards/" + in.DashboardID),
		}
		if len(inferenceNotes) > 0 {
			result["notes"] = inferenceNotes
		}
		return result, nil
	},
}

// panelTypeEnum is the panel_type enum the tool offers: every supported type.
var panelTypeEnum = []string{"table", "timeseries", "bar", "pie", "stat", "gauge", "text", "histogram", "scatter", "heatmap"}

// panelArgs are the add_dashboard_panel arguments.
type panelArgs struct {
	DashboardID      string   `json:"dashboard_id"`
	Name             string   `json:"name"`
	Description      string   `json:"description"`
	PanelType        string   `json:"panel_type"`
	SQL              string   `json:"sql"`
	XColumn          string   `json:"x_column"`
	YColumns         []string `json:"y_columns"`
	BarMode          string   `json:"bar_mode"`
	PieLabelColumn   string   `json:"pie_label_column"`
	PieValueColumn   string   `json:"pie_value_column"`
	PieDonut         bool     `json:"pie_donut"`
	StatField        string   `json:"stat_field"`
	StatCalculation  string   `json:"stat_calculation"`
	StatUnit         string   `json:"stat_unit"`
	StatPrefix       string   `json:"stat_prefix"`
	StatSuffix       string   `json:"stat_suffix"`
	GaugeMin         *float64 `json:"gauge_min"`
	GaugeMax         *float64 `json:"gauge_max"`
	Content          string   `json:"content"`
	LayoutX          *int     `json:"layout_x"`
	LayoutY          *int     `json:"layout_y"`
	LayoutW          *int     `json:"layout_w"`
	LayoutH          *int     `json:"layout_h"`
	Fill             string   `json:"fill"`
	Stacked          bool     `json:"stacked"`
	HistogramColumn  string   `json:"histogram_column"`
	HistogramBuckets *int     `json:"histogram_buckets"`
	ScatterX         string   `json:"scatter_x"`
	ScatterY         string   `json:"scatter_y"`
	ScatterSize      string   `json:"scatter_size"`
	ScatterSeries    string   `json:"scatter_series"`
	HeatmapX         string   `json:"heatmap_x"`
	HeatmapY         string   `json:"heatmap_y"`
	HeatmapValue     string   `json:"heatmap_value"`
}

// panelConfigFor is the stored panel config for the arguments, after the
// column picks: only the settings of the chosen panel type.
func panelConfigFor(in panelArgs) map[string]any {
	config := map[string]any{
		"chartType": in.PanelType,
	}
	switch in.PanelType {
	case "timeseries", "bar":
		config["xColumn"] = in.XColumn
		config["yColumns"] = in.YColumns
		if in.BarMode != "" {
			config["barMode"] = in.BarMode
		}
		if in.PanelType == "timeseries" {
			if in.Fill == "line" || in.Fill == "area" {
				config["fill"] = in.Fill
			}
			if in.Stacked {
				config["stacked"] = true
			}
		}
		config["legendPosition"] = "bottom"
	case "histogram":
		config["histogramColumn"] = in.HistogramColumn
		if in.HistogramBuckets != nil {
			config["histogramBuckets"] = clamp(*in.HistogramBuckets, 1, 200)
		}
	case "scatter":
		config["scatterX"] = in.ScatterX
		config["scatterY"] = in.ScatterY
		if in.ScatterSize != "" {
			config["scatterSize"] = in.ScatterSize
		}
		if in.ScatterSeries != "" {
			config["scatterSeries"] = in.ScatterSeries
		}
		config["legendPosition"] = "bottom"
	case "heatmap":
		config["heatmapX"] = in.HeatmapX
		config["heatmapY"] = in.HeatmapY
		if in.HeatmapValue != "" {
			config["heatmapValue"] = in.HeatmapValue
		}
	case "pie":
		config["pieLabelColumn"] = in.PieLabelColumn
		config["pieValueColumn"] = in.PieValueColumn
		if in.PieDonut {
			config["pieDonut"] = true
		}
		config["legendPosition"] = "bottom"
	case "stat":
		config["statField"] = in.StatField
		if in.StatCalculation != "" {
			config["statCalculation"] = in.StatCalculation
		} else {
			config["statCalculation"] = "last"
		}
		if in.StatUnit != "" {
			config["statUnit"] = in.StatUnit
		}
		if in.StatPrefix != "" {
			config["statPrefix"] = in.StatPrefix
		}
		if in.StatSuffix != "" {
			config["statSuffix"] = in.StatSuffix
		}
	case "gauge":
		config["statField"] = in.StatField
		if in.GaugeMin != nil {
			config["gaugeMin"] = *in.GaugeMin
		} else {
			config["gaugeMin"] = 0
		}
		if in.GaugeMax != nil {
			config["gaugeMax"] = *in.GaugeMax
		} else {
			config["gaugeMax"] = 100
		}
	case "text":
		config["content"] = in.Content
	}
	return config
}

type colMeta struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// pickChartColumns fills the columns a histogram, scatter or heatmap panel
// needs when the caller left them out, and returns a note per pick.
func pickChartColumns(in *panelArgs, dateCols, numCols, strCols []string) []string {
	var notes []string
	pick := func(field *string, name string, candidates ...string) {
		if *field != "" {
			return
		}
		for _, c := range candidates {
			if c != "" {
				*field = c
				notes = append(notes, name+" auto-picked: "+c)
				return
			}
		}
	}
	first := func(cols []string, skip ...string) string {
		for _, c := range cols {
			if !containsString(skip, c) {
				return c
			}
		}
		return ""
	}
	switch in.PanelType {
	case "histogram":
		pick(&in.HistogramColumn, "histogram_column", first(numCols))
	case "scatter":
		pick(&in.ScatterX, "scatter_x", first(numCols), first(dateCols))
		pick(&in.ScatterY, "scatter_y", first(numCols, in.ScatterX))
	case "heatmap":
		pick(&in.HeatmapX, "heatmap_x", first(dateCols), first(strCols), first(numCols))
		pick(&in.HeatmapY, "heatmap_y", first(strCols, in.HeatmapX), first(dateCols, in.HeatmapX), first(numCols, in.HeatmapX))
		pick(&in.HeatmapValue, "heatmap_value", first(numCols, in.HeatmapX, in.HeatmapY))
	}
	return notes
}

// checkChartColumns reports a histogram, scatter or heatmap panel whose
// required columns are missing from the SQL result.
func checkChartColumns(in panelArgs, meta []colMeta) error {
	has := func(name string) bool {
		for _, c := range meta {
			if c.Name == name {
				return true
			}
		}
		return false
	}
	var required map[string]string
	switch in.PanelType {
	case "histogram":
		required = map[string]string{"histogram_column": in.HistogramColumn}
	case "scatter":
		required = map[string]string{"scatter_x": in.ScatterX, "scatter_y": in.ScatterY}
	case "heatmap":
		required = map[string]string{"heatmap_x": in.HeatmapX, "heatmap_y": in.HeatmapY}
	}
	names := make([]string, 0, len(required))
	for name := range required {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		col := required[name]
		if col == "" {
			return fmt.Errorf("no usable %s found in SQL result; pass %s explicitly or rewrite the SQL", name, name)
		}
		if !has(col) {
			return fmt.Errorf("%s %q is not a column of the SQL result", name, col)
		}
	}
	return nil
}

func peekSQLMeta(tctx Context, sql string) ([]colMeta, error) {
	trimmed := strings.TrimRight(strings.TrimSpace(sql), "; \t\n")
	if trimmed == "" {
		return nil, errors.New("empty SQL")
	}
	if err := tctx.checkGuardrail(trimmed); err != nil {
		return nil, err
	}
	wrapped := "SELECT * FROM (" + trimmed + ") AS _src LIMIT 0"
	res, err := tctx.Gateway.ExecuteQueryWithSettingsCtx(tctx.context(), tctx.ConnectionID, wrapped, tctx.CHUser, tctx.CHPassword, readOnlySettings(1, 15*time.Second), 15*time.Second)
	if err != nil {
		return nil, err
	}
	var meta []colMeta
	if err := json.Unmarshal(res.Meta, &meta); err != nil {
		return nil, fmt.Errorf("decode meta: %w", err)
	}
	return meta, nil
}

func classifyColumns(meta []colMeta) (date, numeric, str []string) {
	for _, c := range meta {
		t := stripWrappers(c.Type)
		switch {
		case isDateType(t):
			date = append(date, c.Name)
		case isNumericType(t):
			numeric = append(numeric, c.Name)
		case isStringType(t):
			str = append(str, c.Name)
		}
	}
	return
}

func stripWrappers(t string) string {
	t = strings.TrimSpace(t)
	for {
		l := strings.ToLower(t)
		switch {
		case strings.HasPrefix(l, "nullable(") && strings.HasSuffix(t, ")"):
			t = t[len("Nullable(") : len(t)-1]
		case strings.HasPrefix(l, "lowcardinality(") && strings.HasSuffix(t, ")"):
			t = t[len("LowCardinality(") : len(t)-1]
		default:
			return t
		}
	}
}

func isDateType(t string) bool {
	l := strings.ToLower(t)
	return strings.HasPrefix(l, "date") || strings.HasPrefix(l, "datetime")
}

func isNumericType(t string) bool {
	l := strings.ToLower(t)
	return strings.HasPrefix(l, "int") || strings.HasPrefix(l, "uint") ||
		strings.HasPrefix(l, "float") || strings.HasPrefix(l, "decimal")
}

func isStringType(t string) bool {
	l := strings.ToLower(t)
	return strings.HasPrefix(l, "string") || strings.HasPrefix(l, "fixedstring") ||
		strings.HasPrefix(l, "enum") || strings.HasPrefix(l, "uuid")
}

func summarizeColumns(meta []colMeta) string {
	parts := make([]string, 0, len(meta))
	for _, c := range meta {
		parts = append(parts, c.Name+" ("+c.Type+")")
	}
	return strings.Join(parts, ", ")
}

func containsString(xs []string, target string) bool {
	for _, x := range xs {
		if x == target {
			return true
		}
	}
	return false
}

func autoNextY(panels []database.Panel) int {
	max := 0
	for _, p := range panels {
		bottom := p.LayoutY + p.LayoutH
		if bottom > max {
			max = bottom
		}
	}
	return max
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
