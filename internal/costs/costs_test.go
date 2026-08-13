// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.

package costs

import (
	"math"
	"strings"
	"testing"
)

func TestRangeSpec(t *testing.T) {
	for _, name := range []string{"24h", "7d", "30d"} {
		if _, ok := RangeSpec(name); !ok {
			t.Errorf("expected range %q to be allowed", name)
		}
	}
	if _, ok := RangeSpec("90d"); ok {
		t.Error("unlisted range must be rejected")
	}
	if DefaultRange.Name != "7d" {
		t.Errorf("unexpected default range %q", DefaultRange.Name)
	}
}

func TestSourceFanOut(t *testing.T) {
	cfg := DefaultConfig()
	q := SummaryQuery("prod", DefaultRange, cfg)
	if !strings.Contains(q, "clusterAllReplicas('prod', system.query_log)") {
		t.Errorf("cluster query must fan out:\n%s", q)
	}
	q = SummaryQuery("", DefaultRange, cfg)
	if strings.Contains(q, "clusterAllReplicas") {
		t.Errorf("single-node query must not fan out:\n%s", q)
	}
	q = StorageQuery("prod", cfg)
	if !strings.Contains(q, "clusterAllReplicas('prod', system.parts)") {
		t.Errorf("storage query must fan out over system.parts:\n%s", q)
	}
}

func TestStorageQueryDoesNotNestAggregates(t *testing.T) {
	// `sum(bytes_on_disk) AS bytes_on_disk` shadows the column, so any later
	// sum(bytes_on_disk) becomes sum(sum(...)) and ClickHouse rejects the
	// query with ILLEGAL_AGGREGATION. monthly_cost must use the bare alias.
	q := StorageQuery("", DefaultConfig())
	if strings.Count(q, "sum(bytes_on_disk)") != 1 {
		t.Errorf("bytes_on_disk must be aggregated exactly once:\n%s", q)
	}
	if !strings.Contains(q, "round(bytes_on_disk / 1e9") {
		t.Errorf("monthly_cost must reference the alias, not re-aggregate:\n%s", q)
	}
}

func TestQueryLogSectionsShareBaseFilters(t *testing.T) {
	cfg := DefaultConfig()
	builders := map[string]string{
		"summary": SummaryQuery("", DefaultRange, cfg),
		"trend":   TrendQuery("", DefaultRange, cfg),
		"users":   UsersQuery("", DefaultRange, cfg),
		"queries": QueriesQuery("", DefaultRange, cfg),
	}
	for name, q := range builders {
		for _, must := range []string{"is_initial_query = 1", LogComment, "INTERVAL 7 DAY", "OSCPUVirtualTimeMicroseconds"} {
			if !strings.Contains(q, must) {
				t.Errorf("%s query missing %q:\n%s", name, must, q)
			}
		}
	}
}

func TestRateRendering(t *testing.T) {
	q := SummaryQuery("", DefaultRange, Config{CPUPerCoreHour: 0.000012})
	if strings.Contains(q, "e-") || strings.Contains(q, "E-") {
		t.Errorf("rates must not render in exponent notation:\n%s", q)
	}
	if !strings.Contains(q, "0.000012") {
		t.Errorf("rate literal missing:\n%s", q)
	}
	// Hostile rates degrade to zero instead of breaking the SQL.
	for _, bad := range []float64{-5, math.NaN(), math.Inf(1)} {
		q := SummaryQuery("", DefaultRange, Config{CPUPerCoreHour: bad})
		if !strings.Contains(q, "* 0,") {
			t.Errorf("rate %v must clamp to 0:\n%s", bad, q)
		}
	}
}

func TestMapTeam(t *testing.T) {
	teams := []TeamRule{
		{Name: "Data Eng", Users: []string{"etl_*", "airflow"}},
		{Name: "Analytics", Users: []string{"looker"}},
	}
	if got := MapTeam("etl_daily", teams); got != "Data Eng" {
		t.Errorf("prefix match: got %q", got)
	}
	if got := MapTeam("airflow", teams); got != "Data Eng" {
		t.Errorf("exact match: got %q", got)
	}
	if got := MapTeam("looker", teams); got != "Analytics" {
		t.Errorf("second rule: got %q", got)
	}
	if got := MapTeam("random_user", teams); got != Unallocated {
		t.Errorf("no match must be %q, got %q", Unallocated, got)
	}
	if got := MapTeam("anything", nil); got != Unallocated {
		t.Errorf("nil rules must be %q, got %q", Unallocated, got)
	}
}

func TestConfigSanitize(t *testing.T) {
	c := Config{
		Currency:       " usd ",
		CPUPerCoreHour: -3,
		StorageGBMonth: math.Inf(1),
		Teams: []TeamRule{
			{Name: "  ", Users: []string{"x"}},
			{Name: Unallocated, Users: []string{"y"}},
			{Name: " Core ", Users: []string{" a ", "", "b*"}},
		},
	}.Sanitize()
	if c.Currency != "USD" {
		t.Errorf("currency: got %q", c.Currency)
	}
	if c.CPUPerCoreHour != 0 || c.StorageGBMonth != 0 {
		t.Errorf("bad rates must clamp to 0: %+v", c)
	}
	if len(c.Teams) != 1 || c.Teams[0].Name != "Core" {
		t.Fatalf("teams not cleaned: %+v", c.Teams)
	}
	if len(c.Teams[0].Users) != 2 || c.Teams[0].Users[0] != "a" || c.Teams[0].Users[1] != "b*" {
		t.Errorf("users not cleaned: %+v", c.Teams[0].Users)
	}
	if c := (Config{}).Sanitize(); c.Currency != "USD" {
		t.Errorf("empty currency must default to USD, got %q", c.Currency)
	}
}
