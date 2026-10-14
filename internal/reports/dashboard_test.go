// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package reports

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/caioricciuti/ch-ui/internal/database"
)

func panel(name, kind, config string) database.Panel {
	return database.Panel{Name: name, PanelType: kind, Config: config, Query: "SELECT 1"}
}

func TestRenderPanelPerType(t *testing.T) {
	ts := Table{Columns: []string{"t", "cpu", "mem"}, Types: []string{"DateTime", "Float64", "UInt64"}, Rows: [][]string{
		{"2026-10-13T00:00:00Z", "0.5", "10"}, {"2026-10-13T01:00:00Z", "0.9", "30"}, {"2026-10-13T02:00:00Z", "0.2", "20"},
	}}
	long := Table{Columns: []string{"t", "host", "v"}, Types: []string{"DateTime", "String", "UInt64"}, Rows: [][]string{
		{"1", "a", "1"}, {"1", "b", "5"}, {"2", "a", "3"}, {"2", "b", "2"},
	}}
	pie := Table{Columns: []string{"country", "n"}, Types: []string{"String", "UInt64"}}
	for i := 0; i < 12; i++ {
		pie.Rows = append(pie.Rows, []string{fmt.Sprintf("c%02d", i), fmt.Sprint(i + 1)})
	}
	dist := Table{Columns: []string{"host", "latency", "size"}, Types: []string{"String", "Float64", "UInt64"}, Rows: [][]string{
		{"a", "1", "10"}, {"b", "4", "40"}, {"a", "2", "20"}, {"b", "\\N", "0"}, {"c", "3", "30"},
	}}
	grid := Table{Columns: []string{"day", "hour", "n"}, Types: []string{"String", "UInt8", "UInt64"}, Rows: [][]string{
		{"Mon", "1", "3"}, {"Mon", "2", "9"}, {"Tue", "1", "4"}, {"Mon", "2", "1"},
	}}
	big := Table{Columns: []string{"id"}, Types: []string{"UInt64"}}
	for i := 0; i < 30; i++ {
		big.Rows = append(big.Rows, []string{fmt.Sprint(i)})
	}

	cases := []struct {
		name  string
		p     database.Panel
		t     Table
		wants []string
		not   []string
	}{
		{"stat last", panel("Errors", "stat", `{"statField":"mem","statSuffix":" MB"}`), ts, []string{"Errors (stat)", "20 MB (last of mem)"}, nil},
		{"stat sum", panel("Total", "stat", `{"statField":"mem","statCalculation":"sum"}`), ts, []string{"60 (sum of mem)"}, nil},
		{"gauge", panel("CPU", "gauge", `{}`), ts, []string{"CPU (gauge)", "0.2 (last of cpu)"}, nil},
		{"timeseries", panel("Load", "timeseries", `{"xColumn":"t"}`), ts, []string{"cpu: last 0.2, min 0.2, max 0.9", "mem: last 20, min 10, max 30"}, nil},
		{"bar long form", panel("Hosts", "bar", `{}`), long, []string{"a: last 3, min 1, max 3", "b: last 2, min 2, max 5"}, nil},
		{"pie top 10", panel("Countries", "pie", `{}`), pie, []string{"c11: 12", "c02: 3", "... 2 more"}, []string{"c01:", "c00:"}},
		{"table 20 rows", panel("Rows", "table", `{}`), big, []string{"Showing the first 20 of 30 rows", " 19"}, []string{" 25\n"}},
		{"stacked timeseries", panel("Load", "timeseries", `{"xColumn":"t","fill":"area","stacked":true}`), ts, []string{"cpu: last 0.2, min 0.2, max 0.9"}, nil},
		{"histogram", panel("Latency", "histogram", `{"histogramColumn":"latency"}`), dist, []string{"Latency (histogram)", "latency: 4 values, min 1, median 2.5, mean 2.5, max 4"}, nil},
		{"histogram default column", panel("Latency", "histogram", `{}`), dist, []string{"latency: 4 values"}, nil},
		{"scatter", panel("Size", "scatter", `{"scatterX":"size","scatterY":"latency"}`), dist, []string{"latency vs size: 4 points", "correlation 1.00"}, nil},
		{"scatter no numbers", panel("S", "scatter", `{}`), Table{Columns: []string{"a"}, Types: []string{"String"}, Rows: [][]string{{"x"}}}, []string{"Needs two numeric columns."}, nil},
		{"heatmap", panel("Load", "heatmap", `{"heatmapX":"day","heatmapY":"hour","heatmapValue":"n"}`), grid, []string{"3 cells of n by day and hour", "Mon / 2: 10", "Tue / 1: 4"}, nil},
		{"heatmap counts rows", panel("Load", "heatmap", `{"heatmapX":"day","heatmapY":"hour"}`), Table{Columns: []string{"day", "hour"}, Types: []string{"String", "String"}, Rows: [][]string{{"Mon", "1"}, {"Mon", "1"}}}, []string{"1 cell of rows by day and hour", "Mon / 1: 2"}, nil},
		{"empty", panel("Nothing", "table", `{}`), Table{Columns: []string{"a"}}, []string{"No data in this time range."}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := RenderPanel(c.p, c.t)
			for _, w := range c.wants {
				if !strings.Contains(out, w) {
					t.Errorf("missing %q in:\n%s", w, out)
				}
			}
			for _, n := range c.not {
				if strings.Contains(out, n) {
					t.Errorf("unexpected %q in:\n%s", n, out)
				}
			}
		})
	}
}

func TestRenderDashboardSummary(t *testing.T) {
	s := &database.ReportSubscription{ID: "r", Name: "Ops daily", Timezone: "UTC", SourceType: SourceDashboard}
	now := time.Date(2026, 10, 13, 8, 0, 0, 0, time.UTC)
	d := DashboardSummary{
		Name: "Ops", TimeRange: "24h", From: now.Add(-24 * time.Hour), To: now, Variables: map[string][]string{"env": {"prod"}},
		Results: []DashboardPanelResult{
			{Panel: panel("Errors", "stat", `{}`), Table: Table{Columns: []string{"n"}, Types: []string{"UInt64"}, Rows: [][]string{{"4"}}}},
			{Panel: panel("Broken", "table", `{}`), Err: errors.New("Code: 60. Table x does not exist")},
		},
		Skipped: 3,
	}
	snap := RenderDashboard(s, d, "https://chui.example.com/dashboards/d1", now)
	for _, w := range []string{"Dashboard: Ops", "last 24h", "env=prod", "Errors (stat)", "Broken (table)", "Query failed: Code: 60", "3 more panels not included", "https://chui.example.com/dashboards/d1"} {
		if !strings.Contains(snap.Text, w) {
			t.Errorf("missing %q in:\n%s", w, snap.Text)
		}
	}
	if snap.RowCount != 1 || snap.Empty || len(snap.CSV) != 0 {
		t.Fatalf("snapshot %+v", snap)
	}
	if !strings.Contains(snap.HTML, "<pre") {
		t.Fatal("html summary should be preformatted")
	}
}

func TestSortPanelsAndKind(t *testing.T) {
	ps := []database.Panel{{Name: "c", LayoutY: 2}, {Name: "b", LayoutY: 0, LayoutX: 6}, {Name: "a", LayoutY: 0, LayoutX: 0}}
	SortPanels(ps)
	if ps[0].Name != "a" || ps[1].Name != "b" || ps[2].Name != "c" {
		t.Fatalf("order %v", ps)
	}
	if PanelKind(database.Panel{Config: `{"chartType":"pie"}`}) != "pie" {
		t.Fatal("kind from config")
	}
}
