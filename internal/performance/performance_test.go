package performance

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/caioricciuti/ch-ui/connector"
)

var liveFixtures = flag.Bool("performance-fixtures", false, "Insert synthetic query-log rows; use only with a disposable ClickHouse server")

func TestWindowsEqualAdjacentAndDelayed(t *testing.T) {
	now := time.Date(2026, 9, 21, 15, 4, 5, 123, time.FixedZone("offset", 7200))
	for _, name := range []string{"1h", "6h", "24h", "7d"} {
		baseline, current, err := Windows(now, name)
		d, _ := Duration(name)
		if err != nil || current.End.Sub(current.Start) != d || baseline.End.Sub(baseline.Start) != d || !baseline.End.Equal(current.Start) || !current.End.Equal(now.UTC().Truncate(time.Second).Add(-time.Minute)) {
			t.Fatalf("bad %s windows: %+v %+v %v", name, baseline, current, err)
		}
	}
	if _, _, err := Windows(now, "24h; DROP TABLE x"); err == nil {
		t.Fatal("untrusted interval accepted")
	}
}

func TestRegressionThresholdsAndInsufficientData(t *testing.T) {
	base := Metrics{Runs: 10, P95MS: 1000, MeanMS: 500, MeanMemoryBytes: 32 << 20, MeanReadBytes: 64 << 20}
	for _, tc := range []struct {
		name    string
		current Metrics
		want    []bool
	}{
		{"all metrics", Metrics{Runs: 10, P95MS: 1500, MeanMemoryBytes: 48 << 20, MeanReadBytes: 96 << 20}, []bool{true, true, true}},
		{"tiny growth", Metrics{Runs: 10, P95MS: 1499, MeanMemoryBytes: 47 << 20, MeanReadBytes: 95 << 20}, []bool{false, false, false}},
		{"low sample", Metrics{Runs: 9, P95MS: 10000, MeanMemoryBytes: 128 << 20, MeanReadBytes: 256 << 20}, []bool{false, false, false}},
		{"improvement", Metrics{Runs: 100, P95MS: 800, MeanMemoryBytes: 16 << 20, MeanReadBytes: 32 << 20}, []bool{false, false, false}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			changes := Changes(base, tc.current)
			for i, want := range tc.want {
				if changes[i].Regressed != want {
					t.Fatalf("%s: got regression %v want %v", changes[i].Metric, changes[i].Regressed, want)
				}
			}
		})
	}
	changes := Changes(Metrics{Runs: 10, P95MS: 1, MeanMemoryBytes: 1, MeanReadBytes: 1}, Metrics{Runs: 10, P95MS: 99, MeanMemoryBytes: 1000, MeanReadBytes: 1000})
	for _, c := range changes {
		if c.Regressed {
			t.Fatalf("absolute floor ignored: %+v", c)
		}
	}
	changes = Changes(Metrics{Runs: 10}, Metrics{Runs: 10, P95MS: 120})
	if changes[0].Percent != nil || !changes[0].Regressed {
		t.Fatalf("zero denominator handled incorrectly: %+v", changes[0])
	}
}

func TestAnalyzePreservesPatternsAndCapsPairs(t *testing.T) {
	base, current, _ := Windows(time.Now(), "1h")
	rows := []map[string]interface{}{
		{"hash": "18446744073709551615", "database": "a", "baseline_runs": "10", "current_runs": 20, "baseline_p95_ms": 100, "current_p95_ms": 200, "baseline_mean_ms": 10, "current_mean_ms": 30},
		{"hash": "18446744073709551615", "database": "b", "baseline_runs": "0", "current_runs": 20, "current_p95_ms": 10000},
		{"hash": "3", "database": "a", "baseline_runs": 20, "current_runs": 0, "current_p95_ms": math.NaN()},
	}
	r := Analyze(rows, "", "1h", base, current)
	if len(r.Patterns) != 3 || len(r.Regressions) != 1 || r.Insufficient != 2 || r.Compared != 1 || r.Regressions[0].ExtraDurationMS != 400 {
		t.Fatalf("unexpected report: %+v", r)
	}
	if _, err := json.Marshal(r); err != nil {
		t.Fatalf("unsafe numeric response: %v", err)
	}
	for len(rows) <= PatternLimit {
		rows = append(rows, rows[0])
	}
	r = Analyze(rows, "cluster", "1h", base, current)
	if !r.Truncated || len(r.Patterns) != PatternLimit || !strings.Contains(r.Coverage, "cluster") {
		t.Fatal("truncation or cluster coverage omitted")
	}
}

func TestQueryUsesBoundedExactPatternsAndNormalizesText(t *testing.T) {
	b, c, _ := Windows(time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC), "1h")
	q, err := Query("", b, c, "18446744073709551615", `db' OR 1=1 --`)
	if err != nil {
		t.Fatal(err)
	}
	for _, must := range []string{"normalizeQuery(any(query))", "current_database = 'db\\' OR 1=1 --'", "toUInt64('18446744073709551615')", "is_initial_query = 1", "event_time < toDateTime(", "NOT startsWith(log_comment, 'ch-ui:')", "LIMIT 1001", "baseline_runs", "current_runs"} {
		if !strings.Contains(q, must) {
			t.Fatalf("query missing %s: %s", must, q)
		}
	}
	if _, err := Query("cluster'); DROP TABLE foo;--", b, c, "", ""); err == nil {
		t.Fatal("cluster injection accepted")
	}
	if _, err := Query("", b, c, "1 OR 1=1", ""); err == nil {
		t.Fatal("hash injection accepted")
	}
	if _, err := Query("", b, b, "", ""); err == nil {
		t.Fatal("overlapping windows accepted")
	}
}

func TestCaptureAndCompareRejectFalseImprovements(t *testing.T) {
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	baseline, _, err := Capture(func(q string) ([]map[string]interface{}, error) {
		if !strings.Contains(q, "normalized_query_hash = toUInt64('42')") {
			t.Fatal("pattern not constrained")
		}
		return []map[string]interface{}{{"hash": "42", "sample_query": "SELECT ?", "current_runs": 25, "current_p95_ms": 200}}, nil
	}, "", now, "1h", "42", "analytics")
	if err != nil {
		t.Fatal(err)
	}
	missing, _, err := Capture(func(string) ([]map[string]interface{}, error) { return nil, nil }, "", now.Add(time.Hour), "1h", "99", "analytics")
	if err != nil {
		t.Fatal(err)
	}
	comparison, err := Compare(baseline, missing)
	if err != nil || comparison.Sufficient || comparison.Current.Hash != "99" {
		t.Fatalf("missing/replacement pattern compared as improvement: %+v %v", comparison, err)
	}
	if _, err := Compare(baseline, baseline); err == nil {
		t.Fatal("overlapping comparison accepted")
	}
	missing.Cluster = "another"
	if _, err := Compare(baseline, missing); err == nil {
		t.Fatal("coverage change accepted")
	}
}

func TestComparisonRequiresSamePhysicalNode(t *testing.T) {
	_, window, _ := Windows(time.Now(), "1h")
	before := Snapshot{Range: "1h", Node: "node-a", Window: window, Metrics: Metrics{Runs: 20, P95MS: 200}}
	after := before
	after.Window = Window{Start: window.End, End: window.End.Add(time.Hour)}
	after.Metrics.P95MS = 100
	if comparison, err := Compare(before, after); err != nil || !comparison.Sufficient {
		t.Fatalf("same-node comparison rejected: %v", err)
	}
	after.Node = "node-b"
	if _, err := Compare(before, after); err == nil {
		t.Fatal("load-balanced comparison accepted changed node")
	}
	before.Cluster, after.Cluster = "same-cluster", "same-cluster"
	if _, err := Compare(before, after); err != nil {
		t.Fatalf("explicit cluster scope rejected: %v", err)
	}
}

// Optional live integration uses the same disposable ClickHouse environment as
// worker tests. It checks real aggregate SQL, including empty query logs.
func TestPerformanceQueryLive(t *testing.T) {
	endpoint := os.Getenv("CHUI_TEST_CLICKHOUSE_URL")
	if endpoint == "" {
		t.Skip("CHUI_TEST_CLICKHOUSE_URL not configured")
	}
	client := connector.NewCHClient(endpoint, false)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	fixture := ""
	fixtureDatabase := ""
	if *liveFixtures {
		fixture = fmt.Sprintf("SELECT user_id FROM performance_fixture_%d WHERE event = 42", time.Now().UnixNano())
		fixtureDatabase = fmt.Sprintf("chui_performance_fixture_%d", time.Now().UnixNano())
		for _, sql := range []string{
			"SYSTEM FLUSH LOGS",
			fmt.Sprintf(`INSERT INTO system.query_log (type,event_date,event_time,query_start_time,query_duration_ms,memory_usage,read_bytes,current_database,normalized_query_hash,query,is_initial_query,ProfileEvents)
			SELECT 'QueryFinish',toDate(now()-INTERVAL 90 MINUTE),now()-INTERVAL 90 MINUTE,now()-INTERVAL 90 MINUTE,1000,33554432,67108864,'%s',normalizedQueryHash('%s'),'%s',1,map('UserTimeMicroseconds',toUInt64(100000),'SystemTimeMicroseconds',toUInt64(20000)) FROM numbers(10)`, fixtureDatabase, fixture, fixture),
			fmt.Sprintf(`INSERT INTO system.query_log (type,event_date,event_time,query_start_time,query_duration_ms,memory_usage,read_bytes,current_database,normalized_query_hash,query,is_initial_query,ProfileEvents)
			SELECT 'QueryFinish',toDate(now()-INTERVAL 30 MINUTE),now()-INTERVAL 30 MINUTE,now()-INTERVAL 30 MINUTE,2000,67108864,134217728,'%s',normalizedQueryHash('%s'),'%s',1,map('UserTimeMicroseconds',toUInt64(200000),'SystemTimeMicroseconds',toUInt64(40000)) FROM numbers(12)`, fixtureDatabase, fixture, fixture),
		} {
			if _, err := client.Execute(ctx, sql, os.Getenv("CHUI_TEST_CLICKHOUSE_USER"), os.Getenv("CHUI_TEST_CLICKHOUSE_PASSWORD"), map[string]string{"log_comment": LogComment}); err != nil {
				t.Fatalf("fixture setup: %v", err)
			}
		}
	}
	b, c, _ := Windows(time.Now(), "1h")
	query, err := Query("", b, c, "", "")
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.Execute(ctx, query, os.Getenv("CHUI_TEST_CLICKHOUSE_USER"), os.Getenv("CHUI_TEST_CLICKHOUSE_PASSWORD"), map[string]string{"readonly": "1", "log_comment": LogComment})
	if err != nil {
		t.Fatalf("query rejected by ClickHouse: %v", err)
	}
	if *liveFixtures {
		report := Analyze(result.Data, "", "1h", b, c)
		for _, pattern := range report.Regressions {
			if pattern.Database != fixtureDatabase {
				continue
			}
			if pattern.Baseline.Runs != 10 || pattern.Current.Runs != 12 || pattern.Baseline.P95MS != 1000 || pattern.Current.P95MS != 2000 || pattern.Current.MeanCPUMS != 240 {
				t.Fatalf("incorrect live aggregation: %+v", pattern)
			}
			if strings.Contains(pattern.SampleQuery, "= 42") || !strings.Contains(pattern.SampleQuery, "?") {
				t.Fatalf("query literal retained: %s", pattern.SampleQuery)
			}
			return
		}
		t.Fatalf("synthetic regression not detected: %+v", report)
	}
}
