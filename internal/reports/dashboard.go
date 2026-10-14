// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package reports

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/caioricciuti/ch-ui/internal/database"
)

// dashboardTableRows is how many rows a table panel shows in a summary.
const dashboardTableRows = 20

// pieSlices is how many slices a pie panel lists.
const pieSlices = 10

// heatmapCells is how many of the largest cells a heatmap panel lists.
const heatmapCells = 5

// maxSeries is how many series a split chart keeps, the largest by total, as
// the chart on screen does (pivotSeries in ui/src/lib/utils/chart-transform.ts).
const maxSeries = 10

// panelConfig is the part of a panel's stored config a summary reads.
type panelConfig struct {
	ChartType       string   `json:"chartType"`
	XColumn         string   `json:"xColumn"`
	YColumns        []string `json:"yColumns"`
	SeriesColumn    string   `json:"seriesColumn"`
	StatField       string   `json:"statField"`
	StatCalculation string   `json:"statCalculation"`
	StatUnit        string   `json:"statUnit"`
	StatPrefix      string   `json:"statPrefix"`
	StatSuffix      string   `json:"statSuffix"`
	PieLabelColumn  string   `json:"pieLabelColumn"`
	PieValueColumn  string   `json:"pieValueColumn"`
	HistogramColumn string   `json:"histogramColumn"`
	ScatterX        string   `json:"scatterX"`
	ScatterY        string   `json:"scatterY"`
	HeatmapX        string   `json:"heatmapX"`
	HeatmapY        string   `json:"heatmapY"`
	HeatmapValue    string   `json:"heatmapValue"`
}

func parsePanelConfig(raw string) panelConfig {
	var c panelConfig
	_ = json.Unmarshal([]byte(raw), &c)
	return c
}

// PanelKind is the visualization of a panel: its stored type, else the
// chart type of its config.
func PanelKind(p database.Panel) string {
	kind := strings.ToLower(strings.TrimSpace(p.PanelType))
	if kind == "" || kind == "chart" {
		if c := parsePanelConfig(p.Config); c.ChartType != "" {
			kind = strings.ToLower(c.ChartType)
		}
	}
	if kind == "" {
		kind = "table"
	}
	return kind
}

// SortPanels orders panels as they appear on the grid: top to bottom, then
// left to right.
func SortPanels(panels []database.Panel) {
	sort.SliceStable(panels, func(i, j int) bool {
		if panels[i].LayoutY != panels[j].LayoutY {
			return panels[i].LayoutY < panels[j].LayoutY
		}
		return panels[i].LayoutX < panels[j].LayoutX
	})
}

// RenderPanel writes the text summary of one panel result.
func RenderPanel(p database.Panel, t Table) string {
	kind := PanelKind(p)
	cfg := parsePanelConfig(p.Config)
	name := strings.TrimSpace(p.Name)
	if name == "" {
		name = "Untitled panel"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s (%s)\n", name, kind)
	if len(t.Rows) == 0 {
		b.WriteString("  No data in this time range.\n")
		return b.String()
	}
	switch kind {
	case "stat", "gauge":
		col := t.columnIndex(cfg.StatField)
		if col < 0 {
			col = firstNumeric(t, -1)
		}
		if col < 0 {
			b.WriteString("  No numeric column.\n")
			break
		}
		calc := cfg.StatCalculation
		if calc == "" {
			calc = "last"
		}
		v, ok := calculate(columnValues(t, col), calc)
		if !ok {
			b.WriteString("  No numeric values.\n")
			break
		}
		fmt.Fprintf(&b, "  %s%s%s (%s of %s)\n", cfg.StatPrefix, formatNumber(v), cfg.StatSuffix, calc, t.Columns[col])
	case "timeseries", "bar", "line", "area":
		x := t.columnIndex(cfg.XColumn)
		if x < 0 {
			x = 0
		}
		series, dropped := seriesOf(t, x, cfg.YColumns, cfg.SeriesColumn)
		if len(series) == 0 {
			b.WriteString("  No numeric series.\n")
			break
		}
		for _, s := range series {
			if len(s.values) == 0 {
				continue
			}
			minV, maxV := s.values[0], s.values[0]
			for _, v := range s.values {
				minV = math.Min(minV, v)
				maxV = math.Max(maxV, v)
			}
			fmt.Fprintf(&b, "  %s: last %s, min %s, max %s\n", s.name, formatNumber(s.values[len(s.values)-1]), formatNumber(minV), formatNumber(maxV))
		}
		if dropped > 0 {
			fmt.Fprintf(&b, "  ... %d smaller series left out\n", dropped)
		}
	case "pie":
		label := t.columnIndex(cfg.PieLabelColumn)
		value := t.columnIndex(cfg.PieValueColumn)
		if value < 0 {
			value = firstNumeric(t, label)
		}
		if label < 0 {
			for i := range t.Columns {
				if i != value {
					label = i
					break
				}
			}
		}
		if value < 0 || label < 0 {
			b.WriteString("  Needs a label column and a numeric column.\n")
			break
		}
		type slice struct {
			label string
			value float64
		}
		var slices []slice
		total := 0.0
		for _, row := range t.Rows {
			v, err := strconv.ParseFloat(cell(row, value), 64)
			if err != nil {
				continue
			}
			slices = append(slices, slice{cell(row, label), v})
			total += v
		}
		sort.SliceStable(slices, func(i, j int) bool { return slices[i].value > slices[j].value })
		for i, s := range slices {
			if i == pieSlices {
				fmt.Fprintf(&b, "  ... %d more\n", len(slices)-pieSlices)
				break
			}
			share := ""
			if total != 0 {
				share = fmt.Sprintf(" (%.1f%%)", s.value/total*100)
			}
			fmt.Fprintf(&b, "  %s: %s%s\n", s.label, formatNumber(s.value), share)
		}
	case "histogram":
		col := t.columnIndex(cfg.HistogramColumn)
		if col < 0 {
			col = firstNumeric(t, -1)
		}
		var values []float64
		if col >= 0 {
			values = columnValues(t, col)
		}
		if len(values) == 0 {
			b.WriteString("  No numeric values.\n")
			break
		}
		sorted := append([]float64(nil), values...)
		sort.Float64s(sorted)
		mean, _ := calculate(values, "mean")
		fmt.Fprintf(&b, "  %s: %d values, min %s, median %s, mean %s, max %s\n", t.Columns[col], len(values),
			formatNumber(sorted[0]), formatNumber(median(sorted)), formatNumber(mean), formatNumber(sorted[len(sorted)-1]))
	case "scatter":
		x := t.columnIndex(cfg.ScatterX)
		if x < 0 {
			x = firstNumeric(t, -1)
		}
		y := t.columnIndex(cfg.ScatterY)
		if y < 0 || y == x {
			y = firstNumeric(t, x)
		}
		if x < 0 || y < 0 {
			b.WriteString("  Needs two numeric columns.\n")
			break
		}
		var xs, ys []float64
		for _, row := range t.Rows {
			xv, errX := strconv.ParseFloat(cell(row, x), 64)
			yv, errY := strconv.ParseFloat(cell(row, y), 64)
			if errX != nil || errY != nil {
				continue
			}
			xs = append(xs, xv)
			ys = append(ys, yv)
		}
		if len(xs) == 0 {
			b.WriteString("  No numeric points.\n")
			break
		}
		fmt.Fprintf(&b, "  %s vs %s: %d points\n", t.Columns[y], t.Columns[x], len(xs))
		if r, ok := correlation(xs, ys); ok {
			fmt.Fprintf(&b, "  correlation %.2f\n", r)
		}
	case "heatmap":
		x := t.columnIndex(cfg.HeatmapX)
		if x < 0 {
			x = 0
		}
		y := t.columnIndex(cfg.HeatmapY)
		if y < 0 || y == x {
			y = -1
			for i := range t.Columns {
				if i != x {
					y = i
					break
				}
			}
		}
		if y < 0 {
			b.WriteString("  Needs two columns.\n")
			break
		}
		value := t.columnIndex(cfg.HeatmapValue)
		if value < 0 || value == x || value == y {
			value = -1
			for i := range t.Columns {
				if i != x && i != y && t.numericColumn(i) {
					value = i
					break
				}
			}
		}
		type heatCell struct {
			x, y  string
			value float64
		}
		var order []string
		cells := map[string]*heatCell{}
		for _, row := range t.Rows {
			v := 1.0
			if value >= 0 {
				var err error
				if v, err = strconv.ParseFloat(cell(row, value), 64); err != nil {
					continue
				}
			}
			key := cell(row, x) + "\x00" + cell(row, y)
			c, ok := cells[key]
			if !ok {
				c = &heatCell{x: cell(row, x), y: cell(row, y)}
				cells[key] = c
				order = append(order, key)
			}
			c.value += v
		}
		list := make([]heatCell, 0, len(order))
		for _, k := range order {
			list = append(list, *cells[k])
		}
		sort.SliceStable(list, func(i, j int) bool { return list[i].value > list[j].value })
		what := "rows"
		if value >= 0 {
			what = t.Columns[value]
		}
		cellsWord := "cells"
		if len(list) == 1 {
			cellsWord = "cell"
		}
		fmt.Fprintf(&b, "  %d %s of %s by %s and %s, largest:\n", len(list), cellsWord, what, t.Columns[x], t.Columns[y])
		for i, c := range list {
			if i == heatmapCells {
				break
			}
			fmt.Fprintf(&b, "  %s / %s: %s\n", c.x, c.y, formatNumber(c.value))
		}
	default:
		rows := t.Rows
		if len(rows) > dashboardTableRows {
			rows = rows[:dashboardTableRows]
		}
		for _, line := range strings.Split(TextTable(t.Columns, rows), "\n") {
			b.WriteString("  " + line + "\n")
		}
		if len(t.Rows) > len(rows) || t.Truncated {
			more := fmt.Sprintf("%d", len(t.Rows))
			if t.Truncated {
				more = "more than " + more
			}
			fmt.Fprintf(&b, "  Showing the first %d of %s rows.\n", len(rows), more)
		}
	}
	return b.String()
}

type series struct {
	name   string
	values []float64
}

// seriesOf returns the numeric series of a chart, each in x order (see
// plotOrder), so "last" is the latest point and not the last row the query
// happened to return. The series are the configured y columns, else every
// numeric column but x. With a series column, as on screen, the first y
// column is split into one series per value of that column. Without one, long
// form (x, one label column, one value) is split by the label. A split keeps
// the maxSeries largest series and also returns how many it left out.
func seriesOf(t Table, x int, yColumns []string, seriesColumn string) ([]series, int) {
	split := t.columnIndex(seriesColumn)
	if split == x {
		split = -1
	}
	var ys []int
	for _, name := range yColumns {
		if i := t.columnIndex(name); i >= 0 && i != x && i != split {
			ys = append(ys, i)
		}
	}
	if len(ys) == 0 {
		for i := range t.Columns {
			if i != x && i != split && t.numericColumn(i) {
				ys = append(ys, i)
			}
		}
	}
	if len(ys) == 0 {
		return nil, 0
	}
	rows := plotOrder(t, x)
	if split >= 0 {
		return pivot(rows, x, ys[0], split)
	}
	var labels []int
	for i := range t.Columns {
		if i != x && !t.numericColumn(i) {
			labels = append(labels, i)
		}
	}
	if len(ys) == 1 && len(labels) == 1 && len(yColumns) == 0 {
		return pivot(rows, x, ys[0], labels[0])
	}
	out := make([]series, 0, len(ys))
	for _, i := range ys {
		out = append(out, series{name: t.Columns[i], values: columnValues(Table{Rows: rows}, i)})
	}
	return out, 0
}

// pivot splits column y into one series per value of column split, like
// pivotSeries on screen: rows that repeat an (x, series) pair are summed, an
// empty label is NULL, and the maxSeries series with the largest total |y|
// are kept, in first-seen order. rows must be in x order already.
func pivot(rows [][]string, x, y, split int) ([]series, int) {
	var order []string
	byName := map[string]*series{}
	totals := map[string]float64{}
	at := map[string]map[string]int{}
	for _, row := range rows {
		name := cell(row, split)
		if name == "" {
			name = "NULL"
		}
		s, ok := byName[name]
		if !ok {
			s = &series{name: name}
			byName[name] = s
			at[name] = map[string]int{}
			order = append(order, name)
		}
		v, err := strconv.ParseFloat(cell(row, y), 64)
		if err != nil || math.IsNaN(v) {
			continue
		}
		totals[name] += math.Abs(v)
		key := cell(row, x)
		if i, seen := at[name][key]; seen {
			s.values[i] += v
			continue
		}
		at[name][key] = len(s.values)
		s.values = append(s.values, v)
	}
	ranked := append([]string(nil), order...)
	sort.SliceStable(ranked, func(i, j int) bool { return totals[ranked[i]] > totals[ranked[j]] })
	if len(ranked) > maxSeries {
		ranked = ranked[:maxSeries]
	}
	keep := map[string]bool{}
	for _, n := range ranked {
		keep[n] = true
	}
	out := make([]series, 0, len(ranked))
	for _, n := range order {
		if keep[n] {
			out = append(out, *byName[n])
		}
	}
	return out, len(order) - len(out)
}

// plotOrder returns the rows in the order a chart plots them: sorted by x
// when x holds times or numbers, leaving out rows whose x cannot be read, as
// the chart does; in query order when x is a category.
func plotOrder(t Table, x int) [][]string {
	if x < 0 || x >= len(t.Columns) {
		return t.Rows
	}
	var key func(string) (float64, bool)
	switch {
	case t.timeColumn(x):
		key = timeKey
	case t.numericColumn(x):
		key = numberKey
	default:
		return t.Rows
	}
	type keyed struct {
		k   float64
		row []string
	}
	list := make([]keyed, 0, len(t.Rows))
	for _, row := range t.Rows {
		if k, ok := key(cell(row, x)); ok {
			list = append(list, keyed{k, row})
		}
	}
	sort.SliceStable(list, func(i, j int) bool { return list[i].k < list[j].k })
	out := make([][]string, len(list))
	for i, r := range list {
		out[i] = r.row
	}
	return out
}

func numberKey(s string) (float64, bool) {
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return v, err == nil && !math.IsNaN(v)
}

// timeKey reads a Date or DateTime cell (ISO, as report queries ask for, or
// ClickHouse's plain text form) or an epoch number as microseconds.
func timeKey(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.999999999", "2006-01-02 15:04:05.999999999", "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return float64(t.UnixMicro()), true
		}
	}
	if v, ok := numberKey(s); ok {
		return v * 1e6, true
	}
	return 0, false
}

// median of sorted values.
func median(sorted []float64) float64 {
	n := len(sorted)
	if n%2 == 1 {
		return sorted[n/2]
	}
	return (sorted[n/2-1] + sorted[n/2]) / 2
}

// correlation is the Pearson correlation of two equally long series; false
// when either series is constant or there are fewer than two points.
func correlation(xs, ys []float64) (float64, bool) {
	n := float64(len(xs))
	if len(xs) < 2 {
		return 0, false
	}
	var mx, my float64
	for i := range xs {
		mx += xs[i]
		my += ys[i]
	}
	mx /= n
	my /= n
	var sxy, sxx, syy float64
	for i := range xs {
		dx, dy := xs[i]-mx, ys[i]-my
		sxy += dx * dy
		sxx += dx * dx
		syy += dy * dy
	}
	if sxx == 0 || syy == 0 {
		return 0, false
	}
	return sxy / math.Sqrt(sxx*syy), true
}

func cell(row []string, i int) string {
	if i < 0 || i >= len(row) {
		return ""
	}
	return row[i]
}

func firstNumeric(t Table, skip int) int {
	for i := range t.Columns {
		if i != skip && t.numericColumn(i) {
			return i
		}
	}
	return -1
}

func columnValues(t Table, col int) []float64 {
	var out []float64
	for _, row := range t.Rows {
		if v, err := strconv.ParseFloat(cell(row, col), 64); err == nil && !math.IsNaN(v) {
			out = append(out, v)
		}
	}
	return out
}

func calculate(values []float64, calc string) (float64, bool) {
	if calc == "count" {
		return float64(len(values)), true
	}
	if len(values) == 0 {
		return 0, false
	}
	switch calc {
	case "first":
		return values[0], true
	case "sum", "mean":
		sum := 0.0
		for _, v := range values {
			sum += v
		}
		if calc == "mean" {
			return sum / float64(len(values)), true
		}
		return sum, true
	case "min", "max", "range":
		minV, maxV := values[0], values[0]
		for _, v := range values {
			minV = math.Min(minV, v)
			maxV = math.Max(maxV, v)
		}
		switch calc {
		case "min":
			return minV, true
		case "max":
			return maxV, true
		}
		return maxV - minV, true
	default:
		return values[len(values)-1], true
	}
}

func formatNumber(v float64) string {
	if math.IsInf(v, 0) || math.IsNaN(v) {
		return strconv.FormatFloat(v, 'f', -1, 64)
	}
	if v == math.Trunc(v) && math.Abs(v) < 1e15 {
		return strconv.FormatInt(int64(v), 10)
	}
	s := strconv.FormatFloat(v, 'f', 4, 64)
	s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	return s
}

// DashboardPanelResult is the outcome of one panel query.
type DashboardPanelResult struct {
	Panel database.Panel
	Table Table
	Err   error
}

// DashboardSummary is the input of RenderDashboard.
type DashboardSummary struct {
	Name      string
	TimeRange string
	From, To  time.Time
	Variables map[string][]string
	Results   []DashboardPanelResult
	// Skipped counts query panels left out over MaxDashboardPanels.
	Skipped int
}

// RenderDashboard renders a dashboard summary as text, HTML and the
// webhook fields.
func RenderDashboard(s *database.ReportSubscription, d DashboardSummary, link string, at time.Time) *Snapshot {
	var b strings.Builder
	fmt.Fprintf(&b, "Dashboard: %s\n", d.Name)
	fmt.Fprintf(&b, "Time range: last %s (%s to %s UTC)\n", d.TimeRange, d.From.UTC().Format("2006-01-02 15:04"), d.To.UTC().Format("2006-01-02 15:04"))
	if len(d.Variables) > 0 {
		names := make([]string, 0, len(d.Variables))
		for n := range d.Variables {
			names = append(names, n)
		}
		sort.Strings(names)
		parts := make([]string, 0, len(names))
		for _, n := range names {
			parts = append(parts, n+"="+strings.Join(d.Variables[n], ","))
		}
		fmt.Fprintf(&b, "Variables: %s\n", strings.Join(parts, "; "))
	}
	b.WriteString("\n")
	ok := 0
	for _, r := range d.Results {
		if r.Err != nil {
			name := strings.TrimSpace(r.Panel.Name)
			if name == "" {
				name = "Untitled panel"
			}
			fmt.Fprintf(&b, "%s (%s)\n  Query failed: %s\n\n", name, PanelKind(r.Panel), oneLine(r.Err.Error(), 300))
			continue
		}
		ok++
		b.WriteString(RenderPanel(r.Panel, r.Table))
		b.WriteString("\n")
	}
	if d.Skipped > 0 {
		fmt.Fprintf(&b, "%s not included (a summary covers at most %d).\n", plural(d.Skipped, "more panel", "more panels"), MaxDashboardPanels)
	}
	body := strings.TrimRight(b.String(), "\n")
	summary := fmt.Sprintf("%s of %s summarized, last %s.", plural(ok, "panel", "panels"), d.Name, d.TimeRange)
	snap := &Snapshot{
		ReportID: s.ID, Name: s.Name, Subject: Subject(s, ok, at), RowCount: ok, Empty: ok == 0,
		Summary: summary, Table: body, Link: link, Timestamp: at.UTC().Format(time.RFC3339),
	}
	var text strings.Builder
	text.WriteString(body + "\n")
	if link != "" {
		text.WriteString("\nOpen the dashboard: " + link + "\n")
	}
	snap.Text = text.String()
	snap.HTML = renderHTML(htmlData{Title: s.Name, Generated: at.UTC().Format("2006-01-02 15:04 UTC"), Pre: body, Link: link})
	return snap
}

func oneLine(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > max {
		s = strings.ToValidUTF8(s[:max], "") + "..."
	}
	return s
}
