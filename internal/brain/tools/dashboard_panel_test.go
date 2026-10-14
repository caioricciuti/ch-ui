// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package tools

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func TestPanelTypeEnumMatchesSupportedTypes(t *testing.T) {
	var schema struct {
		Properties struct {
			PanelType struct {
				Enum []string `json:"enum"`
			} `json:"panel_type"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(addDashboardPanel.Parameters, &schema); err != nil {
		t.Fatal(err)
	}
	enum := append([]string(nil), schema.Properties.PanelType.Enum...)
	sort.Strings(enum)
	var supported []string
	for k := range supportedPanelTypes {
		supported = append(supported, k)
	}
	sort.Strings(supported)
	if !reflect.DeepEqual(enum, supported) {
		t.Fatalf("enum %v, supported %v", enum, supported)
	}
	for _, want := range []string{"histogram", "scatter", "heatmap"} {
		if !supportedPanelTypes[want] {
			t.Errorf("%s not supported", want)
		}
		if !strings.Contains(addDashboardPanel.Description, `"`+want+`"`) {
			t.Errorf("%s not described", want)
		}
	}
}

func TestPickChartColumns(t *testing.T) {
	date, num, str := []string{"t"}, []string{"a", "b", "c"}, []string{"host"}

	h := panelArgs{PanelType: "histogram"}
	pickChartColumns(&h, date, num, str)
	if h.HistogramColumn != "a" {
		t.Errorf("histogram column %q", h.HistogramColumn)
	}

	s := panelArgs{PanelType: "scatter", ScatterX: "b"}
	notes := pickChartColumns(&s, date, num, str)
	if s.ScatterX != "b" || s.ScatterY != "a" || len(notes) != 1 {
		t.Errorf("scatter %q %q, notes %v", s.ScatterX, s.ScatterY, notes)
	}

	m := panelArgs{PanelType: "heatmap"}
	pickChartColumns(&m, date, num, str)
	if m.HeatmapX != "t" || m.HeatmapY != "host" || m.HeatmapValue != "a" {
		t.Errorf("heatmap %q %q %q", m.HeatmapX, m.HeatmapY, m.HeatmapValue)
	}

	// Without text or date columns the heatmap axes fall back to numbers.
	n := panelArgs{PanelType: "heatmap"}
	pickChartColumns(&n, nil, num, nil)
	if n.HeatmapX != "a" || n.HeatmapY != "b" || n.HeatmapValue != "c" {
		t.Errorf("numeric heatmap %q %q %q", n.HeatmapX, n.HeatmapY, n.HeatmapValue)
	}
}

func TestCheckChartColumns(t *testing.T) {
	meta := []colMeta{{Name: "a", Type: "UInt64"}, {Name: "b", Type: "Float64"}}
	cases := []struct {
		in   panelArgs
		want string
	}{
		{panelArgs{PanelType: "histogram", HistogramColumn: "a"}, ""},
		{panelArgs{PanelType: "histogram"}, "no usable histogram_column"},
		{panelArgs{PanelType: "scatter", ScatterX: "a", ScatterY: "gone"}, `scatter_y "gone" is not a column`},
		{panelArgs{PanelType: "heatmap", HeatmapX: "a", HeatmapY: "b"}, ""},
		{panelArgs{PanelType: "heatmap", HeatmapX: "a"}, "no usable heatmap_y"},
	}
	for _, c := range cases {
		err := checkChartColumns(c.in, meta)
		switch {
		case c.want == "" && err != nil:
			t.Errorf("%s: unexpected %v", c.in.PanelType, err)
		case c.want != "" && (err == nil || !strings.Contains(err.Error(), c.want)):
			t.Errorf("%s: got %v, want %q", c.in.PanelType, err, c.want)
		}
	}
}

func TestPanelConfigForNewTypes(t *testing.T) {
	buckets := 500
	cases := []struct {
		in   panelArgs
		want map[string]any
	}{
		{
			panelArgs{PanelType: "timeseries", XColumn: "t", YColumns: []string{"v"}, Fill: "area", Stacked: true},
			map[string]any{"chartType": "timeseries", "xColumn": "t", "yColumns": []string{"v"}, "fill": "area", "stacked": true, "legendPosition": "bottom"},
		},
		{
			// Fill and stacking are time series settings; a bar ignores them.
			panelArgs{PanelType: "bar", XColumn: "x", YColumns: []string{"v"}, Fill: "area", Stacked: true},
			map[string]any{"chartType": "bar", "xColumn": "x", "yColumns": []string{"v"}, "legendPosition": "bottom"},
		},
		{
			panelArgs{PanelType: "histogram", HistogramColumn: "v", HistogramBuckets: &buckets},
			map[string]any{"chartType": "histogram", "histogramColumn": "v", "histogramBuckets": 200},
		},
		{
			panelArgs{PanelType: "scatter", ScatterX: "a", ScatterY: "b", ScatterSeries: "host"},
			map[string]any{"chartType": "scatter", "scatterX": "a", "scatterY": "b", "scatterSeries": "host", "legendPosition": "bottom"},
		},
		{
			panelArgs{PanelType: "heatmap", HeatmapX: "d", HeatmapY: "h"},
			map[string]any{"chartType": "heatmap", "heatmapX": "d", "heatmapY": "h"},
		},
	}
	for _, c := range cases {
		if got := panelConfigFor(c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %v, want %v", c.in.PanelType, got, c.want)
		}
	}
}
