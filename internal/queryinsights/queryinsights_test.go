// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package queryinsights

import (
	"strings"
	"testing"

	"github.com/caioricciuti/ch-ui/internal/querylogactor"
)

func TestRangeSpec(t *testing.T) {
	for _, name := range []string{"1h", "6h", "24h", "7d", "30d"} {
		r, ok := RangeSpec(name)
		if !ok {
			t.Fatalf("expected range %q to exist", name)
		}
		if r.BucketSeconds <= 0 || r.Interval == "" {
			t.Fatalf("invalid spec for %q: %+v", name, r)
		}
	}
	if _, ok := RangeSpec("99y"); ok {
		t.Fatalf("unknown range must be rejected")
	}
	if _, ok := RangeSpec("1 HOUR; DROP TABLE x"); ok {
		t.Fatalf("injection-shaped range must be rejected")
	}
}

func TestSourceFanOut(t *testing.T) {
	q := SummaryQuery("prod", DefaultRange, Filters{})
	if !strings.Contains(q, "clusterAllReplicas('prod', system.query_log)") {
		t.Fatalf("expected cluster fan-out, got: %s", q)
	}
	q = SummaryQuery("", DefaultRange, Filters{})
	if !strings.Contains(q, "FROM system.query_log") || strings.Contains(q, "clusterAllReplicas") {
		t.Fatalf("expected local source for single node, got: %s", q)
	}
	// Invalid cluster names degrade to local rather than being interpolated.
	q = SummaryQuery("bad'name", DefaultRange, Filters{})
	if strings.Contains(q, "bad'name") {
		t.Fatalf("invalid cluster name must not be interpolated: %s", q)
	}
}

func TestAllSectionsShareBaseFilters(t *testing.T) {
	builders := map[string]func(string, Range, Filters) string{
		"summary":  SummaryQuery,
		"volume":   VolumeQuery,
		"latency":  LatencyQuery,
		"slow":     SlowQueriesQuery,
		"memory":   MemoryQuery,
		"frequent": FrequentQuery,
		"errors":   ErrorsQuery,
		"users":    UsersQuery,
		"tables":   TablesQuery,
	}
	rng, _ := RangeSpec("6h")
	for name, build := range builders {
		q := build("", rng, Filters{})
		if !strings.Contains(q, "INTERVAL 6 HOUR") {
			t.Fatalf("%s: missing time window: %s", name, q)
		}
		if !strings.Contains(q, "is_initial_query = 1") {
			t.Fatalf("%s: missing is_initial_query filter", name)
		}
		if !strings.Contains(q, "system.query_log") {
			t.Fatalf("%s: not querying query_log", name)
		}
		if !strings.Contains(q, "FORMAT JSON") {
			t.Fatalf("%s: missing FORMAT JSON", name)
		}
		if !strings.Contains(q, "log_comment != '"+LogComment+"'") {
			t.Fatalf("%s: missing log_comment self-exclusion filter", name)
		}
		if !strings.Contains(q, "event_date >= toDate(now() - INTERVAL 6 HOUR)") {
			t.Fatalf("%s: missing event_date partition-pruning predicate", name)
		}
	}
}

func TestChartQueriesUseBuckets(t *testing.T) {
	rng, _ := RangeSpec("7d")
	for _, q := range []string{VolumeQuery("", rng, Filters{}), LatencyQuery("", rng, Filters{})} {
		if !strings.Contains(q, "toStartOfInterval(event_time, INTERVAL 3600 SECOND)") {
			t.Fatalf("expected 1h buckets for 7d range: %s", q)
		}
		if !strings.Contains(q, "ORDER BY t ASC") {
			t.Fatalf("chart data must be time-ascending for uplot: %s", q)
		}
	}
}

func TestPatternSectionsGroupByNormalizedHash(t *testing.T) {
	rng := DefaultRange
	for name, q := range map[string]string{
		"slow":     SlowQueriesQuery("", rng, Filters{}),
		"memory":   MemoryQuery("", rng, Filters{}),
		"frequent": FrequentQuery("", rng, Filters{}),
	} {
		if !strings.Contains(q, "GROUP BY normalized_query_hash") {
			t.Fatalf("%s: must group by normalized_query_hash", name)
		}
		if !strings.Contains(q, "toString(normalized_query_hash) AS hash") {
			t.Fatalf("%s: hash must be stringified (UInt64 precision in JSON)", name)
		}
	}
	if !strings.Contains(SlowQueriesQuery("", rng, Filters{}), "ORDER BY p95_ms DESC") {
		t.Fatalf("slow section must order by p95")
	}
	if !strings.Contains(MemoryQuery("", rng, Filters{}), "ORDER BY max_memory DESC") {
		t.Fatalf("memory section must order by max memory")
	}
	if !strings.Contains(FrequentQuery("", rng, Filters{}), "ORDER BY runs DESC") {
		t.Fatalf("frequent section must order by runs")
	}
}

func TestErrorsAndTablesQueries(t *testing.T) {
	rng := DefaultRange
	eq := ErrorsQuery("", rng, Filters{})
	if !strings.Contains(eq, "exception_code != 0") || !strings.Contains(eq, "GROUP BY exception_code") {
		t.Fatalf("errors query malformed: %s", eq)
	}
	tq := TablesQuery("", rng, Filters{})
	// The ARRAY JOIN alias must NOT be referenced in WHERE: that pattern is
	// broken over clusterAllReplicas on analyzer-era ClickHouse (#62043, #70851).
	if !strings.Contains(tq, "ARRAY JOIN arrayFilter(") {
		t.Fatalf("tables query must filter inside arrayFilter: %s", tq)
	}
	if strings.Contains(tq, "AND t NOT LIKE") || strings.Contains(tq, "WHERE t ") {
		t.Fatalf("tables query must not reference the ARRAY JOIN alias in WHERE: %s", tq)
	}
	if !strings.Contains(tq, "query_kind = 'Select'") {
		t.Fatalf("tables query must count reads only: %s", tq)
	}
	if !strings.Contains(tq, "greatest(length(tables), 1)") {
		t.Fatalf("tables query must fair-share I/O across the query's tables: %s", tq)
	}
}

func TestVolumeQueryZeroFills(t *testing.T) {
	q := VolumeQuery("", DefaultRange, Filters{})
	if !strings.Contains(q, "WITH FILL STEP 900") {
		t.Fatalf("volume query must zero-fill empty buckets: %s", q)
	}
}

func TestFilters_EscapingAndAllowlist(t *testing.T) {
	actor := querylogactor.Expr(nil)
	q := SummaryQuery("", DefaultRange, Filters{User: "o'brien"})
	if !strings.Contains(q, `AND `+actor+` = 'o\'brien'`) {
		t.Fatalf("user filter must match the actor and escape quotes: %s", q)
	}
	if strings.Contains(q, "AND user = ") {
		t.Fatalf("user filter must not match the raw ClickHouse user: %s", q)
	}

	q = SummaryQuery("", DefaultRange, Filters{Search: `100%_\`})
	if !strings.Contains(q, `AND query ILIKE '%100\%\_\\\\%'`) && !strings.Contains(q, `\%`) {
		t.Fatalf("search filter must escape LIKE wildcards: %s", q)
	}
	if strings.Contains(q, "ILIKE '%100%_") {
		t.Fatalf("unescaped wildcards leaked into search pattern: %s", q)
	}

	q = SummaryQuery("", DefaultRange, Filters{Table: "db.events"})
	if !strings.Contains(q, `AND has(tables, 'db.events')`) {
		t.Fatalf("table filter missing: %s", q)
	}

	q = SummaryQuery("", DefaultRange, Filters{MinDurationMS: 1000})
	if !strings.Contains(q, "AND query_duration_ms >= 1000") {
		t.Fatalf("min duration filter missing: %s", q)
	}

	// Kinds outside the allowlist must be dropped, not interpolated.
	q = SummaryQuery("", DefaultRange, Filters{QueryKind: "Select'; DROP TABLE x"})
	if strings.Contains(q, "DROP TABLE") {
		t.Fatalf("non-allowlisted query kind interpolated: %s", q)
	}
	q = SummaryQuery("", DefaultRange, Filters{QueryKind: "Insert"})
	if !strings.Contains(q, "AND query_kind = 'Insert'") {
		t.Fatalf("allowlisted kind missing: %s", q)
	}

	// Filters apply to every section, including charts and pattern groups.
	for name, q := range map[string]string{
		"volume": VolumeQuery("", DefaultRange, Filters{User: "alice"}),
		"slow":   SlowQueriesQuery("", DefaultRange, Filters{User: "alice"}),
		"tables": TablesQuery("", DefaultRange, Filters{User: "alice"}),
	} {
		if !strings.Contains(q, "AND "+actor+" = 'alice'") {
			t.Fatalf("%s: filters not applied: %s", name, q)
		}
	}
}

// Load is attributed to the actor of a row (tag person, else ClickHouse
// user), returned as `user`, with the accounts behind each actor alongside.
func TestUsersQueryGroupsByActor(t *testing.T) {
	actor := querylogactor.Expr(nil)
	q := UsersQuery("prod", DefaultRange, Filters{})
	for _, must := range []string{
		actor + " AS actor,", "user AS account,", "actor AS user,",
		"groupUniqArray(account) AS accounts,", "GROUP BY actor", "ORDER BY runs DESC",
		"FROM clusterAllReplicas('prod', system.query_log)",
	} {
		if !strings.Contains(q, must) {
			t.Fatalf("users query missing %q:\n%s", must, q)
		}
	}
	if strings.Contains(q, "GROUP BY user") || strings.Contains(q, "groupUniqArray(user)") {
		t.Fatalf("users query must not read the shadowed user alias:\n%s", q)
	}
	if s := SummaryQuery("", DefaultRange, Filters{}); !strings.Contains(s, "uniqIf("+actor+", type = 'QueryFinish') AS active_users") {
		t.Fatalf("summary must count actors:\n%s", s)
	}

	f := Filters{User: "ann@x", Accounts: []querylogactor.Account{{User: "sso:ann@x", Person: "ann@x"}}}
	want := "transform(user, ['sso:ann@x'], ['ann@x'], user)"
	for name, q := range map[string]string{"users": UsersQuery("", DefaultRange, f), "slow": SlowQueriesQuery("", DefaultRange, f)} {
		if !strings.Contains(q, want) {
			t.Fatalf("%s must relabel SSO accounts:\n%s", name, q)
		}
	}
}

func TestSampleQueriesUseRunnableCap(t *testing.T) {
	want := "substring(any(query), 1, 2000)"
	for name, q := range map[string]string{
		"slow":   SlowQueriesQuery("", DefaultRange, Filters{}),
		"errors": ErrorsQuery("", DefaultRange, Filters{}),
	} {
		if !strings.Contains(q, want) {
			t.Fatalf("%s: sample_query must use the %d-char cap: %s", name, SampleQueryCap, q)
		}
	}
}
