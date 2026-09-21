// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. See LICENSE.BSL.

// Package performance compares observed query patterns over equal windows.
// It never executes workload queries, estimates monetary savings, or treats
// missing observations as an improvement.
package performance

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	LogComment   = "ch-ui:performance"
	MinSamples   = 10
	PatternLimit = 1000
)

var validClusterName = regexp.MustCompile(`^[a-zA-Z0-9_.\-]+$`)

func ValidCluster(name string) bool {
	return name != "" && len(name) <= 128 && validClusterName.MatchString(name)
}

type Window struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

type Metrics struct {
	Runs            int64   `json:"runs"`
	Failures        int64   `json:"failures"`
	MeanMS          float64 `json:"mean_ms"`
	P95MS           float64 `json:"p95_ms"`
	MeanMemoryBytes float64 `json:"mean_memory_bytes"`
	MeanReadBytes   float64 `json:"mean_read_bytes"`
	MeanCPUMS       float64 `json:"mean_cpu_ms"`
}

type Change struct {
	Metric string  `json:"metric"`
	Before float64 `json:"before"`
	After  float64 `json:"after"`
	// nil means a percentage cannot be computed (the baseline was zero).
	Percent   *float64 `json:"percent"`
	Regressed bool     `json:"regressed"`
}

type Pattern struct {
	Hash        string   `json:"hash"`
	Database    string   `json:"database"`
	SampleQuery string   `json:"sample_query"`
	Baseline    Metrics  `json:"baseline"`
	Current     Metrics  `json:"current"`
	Changes     []Change `json:"changes"`
	Status      string   `json:"status"`
	// Relative to mean baseline duration at the observed current frequency.
	ExtraDurationMS float64 `json:"extra_duration_ms"`
}

type Report struct {
	Range        string    `json:"range"`
	Baseline     Window    `json:"baseline"`
	Current      Window    `json:"current"`
	Cluster      string    `json:"cluster"`
	Node         string    `json:"node"`
	Supported    bool      `json:"supported"`
	Coverage     string    `json:"coverage"`
	Truncated    bool      `json:"truncated"`
	MinSamples   int       `json:"min_samples"`
	Compared     int       `json:"compared"`
	Insufficient int       `json:"insufficient"`
	Regressions  []Pattern `json:"regressions"`
	Patterns     []Pattern `json:"patterns"`
}

type Snapshot struct {
	Range       string  `json:"range"`
	Window      Window  `json:"window"`
	Cluster     string  `json:"cluster"`
	Node        string  `json:"node"`
	Metrics     Metrics `json:"metrics"`
	Hash        string  `json:"hash"`
	Database    string  `json:"database"`
	SampleQuery string  `json:"sample_query"`
}

type Comparison struct {
	Baseline   Snapshot `json:"baseline"`
	Current    Snapshot `json:"current"`
	Changes    []Change `json:"changes"`
	Sufficient bool     `json:"sufficient"`
}

type Executor func(string) ([]map[string]interface{}, error)

func Duration(name string) (time.Duration, bool) {
	switch name {
	case "1h":
		return time.Hour, true
	case "6h":
		return 6 * time.Hour, true
	case "24h":
		return 24 * time.Hour, true
	case "7d":
		return 7 * 24 * time.Hour, true
	default:
		return 0, false
	}
}

// Windows uses UTC, whole seconds, and half-open adjacent intervals. A minute
// of lag avoids comparing an unflushed tail with a complete historical window.
func Windows(now time.Time, name string) (baseline, current Window, err error) {
	d, ok := Duration(name)
	if !ok {
		return Window{}, Window{}, fmt.Errorf("range must be 1h, 6h, 24h, or 7d")
	}
	end := now.UTC().Truncate(time.Second).Add(-time.Minute)
	current = Window{Start: end.Add(-d), End: end}
	baseline = Window{Start: current.Start.Add(-d), End: current.Start}
	return baseline, current, nil
}

func ValidHash(hash string) bool {
	if hash == "" {
		return false
	}
	_, err := strconv.ParseUint(hash, 10, 64)
	return err == nil && strings.Trim(hash, "0123456789") == ""
}

func quote(s string) string {
	return "'" + strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\\`), "'", `\'`) + "'"
}

// Query returns at most PatternLimit+1 rows, letting callers report truncation.
// Each row contains BOTH windows: row limits cannot silently split a pair.
// Query text is normalized before leaving ClickHouse, so saved investigations
// do not retain literal values or credentials from workload queries.
func Query(cluster string, baseline, current Window, hash, database string) (string, error) {
	if !baseline.Start.Before(baseline.End) || !current.Start.Before(current.End) ||
		baseline.End.After(current.Start) || baseline.End.Sub(baseline.Start) != current.End.Sub(current.Start) {
		return "", fmt.Errorf("comparison windows must be equal, non-overlapping, and nonempty")
	}
	source := "system.query_log"
	if cluster != "" {
		if !ValidCluster(cluster) {
			return "", fmt.Errorf("invalid cluster name")
		}
		source = "clusterAllReplicas(" + quote(cluster) + ", system.query_log)"
	}
	filter := ""
	if hash != "" {
		if !ValidHash(hash) {
			return "", fmt.Errorf("invalid normalized query hash")
		}
		filter = fmt.Sprintf(" AND normalized_query_hash = toUInt64('%s') AND current_database = %s", hash, quote(database))
	}
	var fields []string
	for _, period := range []struct {
		name   string
		window Window
	}{{"baseline", baseline}, {"current", current}} {
		within := fmt.Sprintf("event_time >= toDateTime(%d) AND event_time < toDateTime(%d)", period.window.Start.Unix(), period.window.End.Unix())
		finished := "(" + within + ") AND type = 'QueryFinish'"
		fields = append(fields,
			fmt.Sprintf("countIf(%s) AS %s_runs", finished, period.name),
			fmt.Sprintf("countIf((%s) AND type != 'QueryFinish') AS %s_failures", within, period.name),
		)
		for _, metric := range []struct{ alias, expression string }{
			{"mean_ms", "avgIf(query_duration_ms, %s)"},
			{"p95_ms", "quantileTDigestIf(0.95)(query_duration_ms, %s)"},
			{"mean_memory_bytes", "avgIf(memory_usage, %s)"},
			{"mean_read_bytes", "avgIf(read_bytes, %s)"},
			{"mean_cpu_ms", "avgIf((ProfileEvents['UserTimeMicroseconds'] + ProfileEvents['SystemTimeMicroseconds']) / 1000.0, %s)"},
		} {
			fields = append(fields, fmt.Sprintf("if(%s_runs > 0, %s, 0) AS %s_%s", period.name, fmt.Sprintf(metric.expression, finished), period.name, metric.alias))
		}
	}
	return fmt.Sprintf(`SELECT toString(normalized_query_hash) AS hash, any(hostName()) AS node,
 current_database AS database, substring(normalizeQuery(any(query)), 1, 2000) AS sample_query,
 %s
FROM %s
WHERE event_time >= toDateTime(%d) AND event_time < toDateTime(%d)
 AND event_date >= toDate(toDateTime(%d))
 AND is_initial_query = 1
 AND type IN ('QueryFinish', 'ExceptionBeforeStart', 'ExceptionWhileProcessing')
 AND NOT startsWith(log_comment, 'ch-ui:')%s
GROUP BY normalized_query_hash, current_database
ORDER BY current_runs DESC, hash, database
LIMIT %d FORMAT JSON`, strings.Join(fields, ",\n "), source, baseline.Start.Unix(), current.End.Unix(), baseline.Start.Unix(), filter, PatternLimit+1), nil
}

func metric(row map[string]interface{}, key string) float64 {
	var n float64
	switch v := row[key].(type) {
	case float64:
		n = v
	case int:
		n = float64(v)
	case int64:
		n = float64(v)
	case uint64:
		n = float64(v)
	case json.Number:
		n, _ = v.Float64()
	case string:
		n, _ = strconv.ParseFloat(v, 64)
	}
	if math.IsNaN(n) || math.IsInf(n, 0) || n < 0 {
		return 0
	}
	return n
}

func metrics(row map[string]interface{}, prefix string) Metrics {
	return Metrics{
		Runs: int64(metric(row, prefix+"_runs")), Failures: int64(metric(row, prefix+"_failures")),
		MeanMS: metric(row, prefix+"_mean_ms"), P95MS: metric(row, prefix+"_p95_ms"),
		MeanMemoryBytes: metric(row, prefix+"_mean_memory_bytes"), MeanReadBytes: metric(row, prefix+"_mean_read_bytes"),
		MeanCPUMS: metric(row, prefix+"_mean_cpu_ms"),
	}
}

func Changes(before, after Metrics) []Change {
	changes := make([]Change, 0, 5)
	sufficient := before.Runs >= MinSamples && after.Runs >= MinSamples
	for _, m := range []struct {
		name        string
		a, b, floor float64
		detect      bool
	}{
		{"p95_ms", before.P95MS, after.P95MS, 100, true},
		{"mean_memory_bytes", before.MeanMemoryBytes, after.MeanMemoryBytes, 16 * 1024 * 1024, true},
		{"mean_read_bytes", before.MeanReadBytes, after.MeanReadBytes, 16 * 1024 * 1024, true},
		{"mean_cpu_ms", before.MeanCPUMS, after.MeanCPUMS, 0, false},
		{"mean_ms", before.MeanMS, after.MeanMS, 0, false},
	} {
		c := Change{Metric: m.name, Before: m.a, After: m.b}
		if m.a > 0 {
			p := (m.b - m.a) / m.a * 100
			c.Percent = &p
		}
		// Both a relative and absolute increase are required. Zero baselines
		// can regress, but their percent change remains unknown (never infinity).
		c.Regressed = sufficient && m.detect && m.b >= m.a*1.5 && m.b-m.a >= m.floor
		changes = append(changes, c)
	}
	return changes
}

func Collect(exec Executor, cluster string, now time.Time, rangeName string) (Report, error) {
	baseline, current, err := Windows(now, rangeName)
	if err != nil {
		return Report{}, err
	}
	query, err := Query(cluster, baseline, current, "", "")
	if err != nil {
		return Report{}, err
	}
	rows, err := exec(query)
	if err != nil {
		return Report{}, err
	}
	return Analyze(rows, cluster, rangeName, baseline, current), nil
}

func Analyze(rows []map[string]interface{}, cluster, rangeName string, baseline, current Window) Report {
	r := Report{Range: rangeName, Baseline: baseline, Current: current, Cluster: cluster, Supported: true,
		Coverage:   "Connected node only; retained, logged initial queries. Sampling, log retention, workload changes, and cold caches can affect comparisons.",
		MinSamples: MinSamples, Regressions: []Pattern{}, Patterns: []Pattern{}}
	if cluster != "" {
		r.Coverage = "All replicas in cluster " + cluster + "; retained, logged initial queries. Sampling, log retention, workload changes, and cold caches can affect comparisons."
	}
	if cluster == "" && len(rows) > 0 {
		r.Node, _ = rows[0]["node"].(string)
		if r.Node != "" {
			r.Coverage = "Connected node " + r.Node + " only; retained, logged initial queries. Sampling, log retention, workload changes, and cold caches can affect comparisons."
		}
	}
	if len(rows) > PatternLimit {
		r.Truncated = true
		rows = rows[:PatternLimit]
	}
	for _, row := range rows {
		hash, _ := row["hash"].(string)
		if !ValidHash(hash) {
			continue
		}
		p := Pattern{Hash: hash, Baseline: metrics(row, "baseline"), Current: metrics(row, "current"), Status: "stable"}
		p.Database, _ = row["database"].(string)
		p.SampleQuery, _ = row["sample_query"].(string)
		p.Changes = Changes(p.Baseline, p.Current)
		if p.Baseline.Runs < MinSamples || p.Current.Runs < MinSamples {
			p.Status = "insufficient_data"
			r.Insufficient++
		} else {
			r.Compared++
			p.ExtraDurationMS = math.Max(0, (p.Current.MeanMS-p.Baseline.MeanMS)*float64(p.Current.Runs))
			for _, change := range p.Changes {
				if change.Regressed {
					p.Status = "regressed"
					break
				}
			}
			if p.Status == "regressed" {
				r.Regressions = append(r.Regressions, p)
			}
		}
		r.Patterns = append(r.Patterns, p)
	}
	sort.SliceStable(r.Regressions, func(i, j int) bool { return r.Regressions[i].ExtraDurationMS > r.Regressions[j].ExtraDurationMS })
	return r
}

// Capture reads one exact hash/database pair using the caller's credentials.
func Capture(exec Executor, cluster string, now time.Time, rangeName, hash, database string) (Snapshot, string, error) {
	baseline, current, err := Windows(now, rangeName)
	if err != nil {
		return Snapshot{}, "", err
	}
	if !ValidHash(hash) {
		return Snapshot{}, "", fmt.Errorf("invalid normalized query hash")
	}
	query, err := Query(cluster, baseline, current, hash, database)
	if err != nil {
		return Snapshot{}, "", err
	}
	rows, err := exec(query)
	if err != nil {
		return Snapshot{}, "", err
	}
	snapshot := Snapshot{Range: rangeName, Window: current, Cluster: cluster, Hash: hash, Database: database}
	if len(rows) == 0 {
		return snapshot, "", nil
	}
	snapshot.Metrics = metrics(rows[0], "current")
	snapshot.Node, _ = rows[0]["node"].(string)
	text, _ := rows[0]["sample_query"].(string)
	snapshot.SampleQuery = text
	return snapshot, text, nil
}

// Compare rejects overlapping or unequal windows, and preserves insufficient
// observations instead of interpreting a missing pattern as a 100% improvement.
func Compare(baseline, current Snapshot) (Comparison, error) {
	if baseline.Cluster != current.Cluster || baseline.Range != current.Range ||
		baseline.Window.End.After(current.Window.Start) ||
		baseline.Window.End.Sub(baseline.Window.Start) != current.Window.End.Sub(current.Window.Start) {
		return Comparison{}, fmt.Errorf("comparison requires the same coverage and a full non-overlapping window after the baseline")
	}
	if baseline.Cluster == "" && baseline.Metrics.Runs >= MinSamples && current.Metrics.Runs >= MinSamples &&
		(baseline.Node == "" || current.Node == "" || baseline.Node != current.Node) {
		return Comparison{}, fmt.Errorf("connected-node identity changed or is unavailable; use the same node as the saved baseline to compare measurements")
	}
	return Comparison{Baseline: baseline, Current: current, Changes: Changes(baseline.Metrics, current.Metrics),
		Sufficient: baseline.Metrics.Runs >= MinSamples && current.Metrics.Runs >= MinSamples}, nil
}
