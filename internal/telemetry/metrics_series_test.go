// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package telemetry

import (
	"strings"
	"testing"
)

func TestShapeCatalogRows(t *testing.T) {
	out := ShapeCatalogRows(decodeRowsJSON(t, `[{"name":"http.requests","type":"sum","unit":"1","description":"d","services":"3","series":12}]`))
	if len(out) != 1 || out[0]["name"] != "http.requests" || out[0]["services"] != int64(3) || out[0]["series"] != int64(12) {
		t.Errorf("catalog: %v", out)
	}
}

// 08:00 to 08:03 in 60 s buckets: axis 08:00, 08:01, 08:02, 08:03.
var seriesParams = MetricsParams{
	From: "2026-10-16T08:00:00Z", To: "2026-10-16T08:03:00Z", Metric: "cpu",
	Aggregation: "avg", GroupBy: []string{"host"}, BucketSeconds: 60, LimitSeries: 1,
}

func TestBuildSeriesGroupsRanksAndFills(t *testing.T) {
	rows := decodeRowsJSON(t, `[
	 {"t":1792137600,"g0":"a","n":"5","unit":"%","v":10},
	 {"t":1792137660,"g0":"a","n":"5","v":30},
	 {"t":1792137600,"g0":"b","n":"1","v":99}
	]`)
	res, err := BuildSeries(rows, seriesParams)
	if err != nil {
		t.Fatal(err)
	}
	if res.Unit != "%" || res.BucketSeconds != 60 || !res.Truncated || res.RowsTruncated {
		t.Errorf("result: %+v", res)
	}
	if len(res.Series) != 1 {
		t.Fatalf("limit 1: %d series", len(res.Series))
	}
	s := res.Series[0]
	if s["name"] != "a" || s["labels"].(map[string]string)["host"] != "a" {
		t.Errorf("busiest series first: %v", s)
	}
	points := s["points"].([][2]interface{})
	if len(points) != 4 || points[0][1] != 10.0 || points[1][1] != 30.0 || points[2][1] != nil {
		t.Errorf("points: %v", points)
	}
	if s["last"] != 30.0 || s["min"] != 10.0 || s["max"] != 30.0 || s["avg"] != 20.0 {
		t.Errorf("stats: %v", s)
	}
}

func TestBuildSeriesRate(t *testing.T) {
	p := seriesParams
	p.Aggregation, p.GroupBy, p.LimitSeries = "rate", nil, 10
	if _, err := BuildSeries(decodeRowsJSON(t, `[{"t":1792137600,"sid":"x","cum":0,"v":1}]`), p); err == nil || !strings.Contains(err.Error(), "cumulative monotonic") {
		t.Errorf("delta counter: want rate error, got %v", err)
	}
	res, err := BuildSeries(decodeRowsJSON(t, `[
	 {"t":1792137600,"sid":"x","cum":1,"n":1,"v":100},
	 {"t":1792137660,"sid":"x","cum":1,"n":1,"v":160}
	]`), p)
	if err != nil {
		t.Fatal(err)
	}
	points := res.Series[0]["points"].([][2]interface{})
	if res.Series[0]["name"] != "cpu" || points[1][1] != 1.0 || points[0][1] != nil {
		t.Errorf("rate 60 per 60 s = 1/s: %v", res.Series[0])
	}
}
