// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/caioricciuti/ch-ui/internal/clusterhealth"
	"github.com/caioricciuti/ch-ui/internal/queryinsights"
)

// Performance diagnostics: read-only tools over query_log, EXPLAIN,
// system.parts, system.merges and system.mutations. They run as the session
// user with readonly=1 like every Brain read. A user whose profile is
// already read only gets the query without Brain's caps (the gateway drops
// refused settings), so every statement here carries its own LIMIT, and
// every query_log read its own event_date and event_time window.

// RegisterDiagnostics registers the performance tools.
func RegisterDiagnostics(r *Registry) {
	r.Register(analyzeQueryLog)
	r.Register(queryPatternTool)
	r.Register(explainQueryTool)
	r.Register(tableStorageTool)
	r.Register(mergesAndMutationsTool)
	r.Register(compareQueryEstimatesTool)
}

const (
	// diagTimeout bounds one diagnostics tool call, all its queries included.
	diagTimeout = 30 * time.Second
	// diagTopN is how many rows analyze_query_log returns.
	diagTopN = 20
	// diagSampleCap clips the sample query of each analyze_query_log row.
	diagSampleCap = 400
	// diagExampleCap clips the example query of query_pattern.
	diagExampleCap = 2000
	// diagExplainMaxLines caps the EXPLAIN lines returned to the model.
	diagExplainMaxLines = 400
	// diagErrorCap clips ClickHouse errors put into tool results.
	diagErrorCap = 1500
)

// diagWindows is the allowlist of query_log windows the tools accept.
var diagWindows = map[string]bool{"1h": true, "6h": true, "24h": true, "7d": true}

// diagExcludedComments hides CH-UI's own introspection from the numbers.
// Brain's own queries are left out of its query_log diagnostics: the
// introspection tag of Query Insights exactly, and every Brain query by the
// prefix of its JSON tag.
var (
	diagExcludedComments        = []string{queryinsights.LogComment}
	diagExcludedCommentPrefixes = []string{tunnel.LogCommentPrefix("brain")}
)

const queryLogEmptyNote = "No matching queries in system.query_log for this window. Queries are logged only when log_queries=1 for the user (the default), and the log is flushed every few seconds (flush_interval_milliseconds, 7.5 s by default), so the latest queries may not show yet."

// ErrorCode returns the ClickHouse error code in err ("Code: 60. ..."), or 0.
func ErrorCode(err error) int {
	if err == nil {
		return 0
	}
	m := chErrorCodeRE.FindStringSubmatch(err.Error())
	if m == nil {
		return 0
	}
	n, _ := strconv.Atoi(m[1])
	return n
}

var chErrorCodeRE = regexp.MustCompile(`Code:\s*(\d+)\.`)

// ReadOnlySettings are the settings every Brain read runs with, exported for
// the Ask AI validation that runs outside a tool call.
func ReadOnlySettings(maxRows int, timeout time.Duration) map[string]string {
	return readOnlySettings(maxRows, timeout)
}

// StripTrailingFormat removes a FORMAT clause (and a trailing semicolon) at
// the end of sql, so an EXPLAIN of it is still read as JSON.
func StripTrailingFormat(sql string) string {
	s := strings.TrimRight(strings.TrimSpace(sql), "; \t\n")
	if !hasTrailingFormat(s) {
		return s
	}
	blank := literalOrComment.ReplaceAllStringFunc(s, func(m string) string { return strings.Repeat(" ", len(m)) })
	if loc := trailingFormatRE.FindStringIndex(blank); loc != nil {
		s = s[:loc[0]]
	}
	return strings.TrimRight(s, "; \t\n")
}

// IsSelectStatement reports whether sql, once comments are blanked, starts
// with SELECT or WITH.
func IsSelectStatement(sql string) bool {
	return limitableRE.MatchString(literalOrComment.ReplaceAllString(sql, " "))
}

var leadingExplainRE = regexp.MustCompile(`(?is)^\s*EXPLAIN\b`)

// checkInnerSelect validates the query handed to an EXPLAIN tool: one
// SELECT or WITH statement, no EXPLAIN of its own, no SETTINGS or FORMAT.
// It returns the query without a trailing semicolon.
func checkInnerSelect(sql string) (string, error) {
	s := strings.TrimRight(strings.TrimSpace(sql), "; \t\n")
	if s == "" {
		return "", errors.New("sql is required")
	}
	blank := literalOrComment.ReplaceAllString(s, " ")
	if strings.Contains(blank, ";") {
		return "", errors.New("pass a single statement; this one contains several")
	}
	if leadingExplainRE.MatchString(blank) {
		return "", errors.New("pass the query itself, not an EXPLAIN; the mode argument picks the EXPLAIN kind")
	}
	if !limitableRE.MatchString(blank) || !isReadOnly(blank) {
		return "", errors.New("only a single SELECT or WITH query can be explained")
	}
	if hasSettingsClause(s) {
		return "", errors.New("remove the SETTINGS clause: Brain queries run with readonly=1 and fixed limits")
	}
	if hasTrailingFormat(s) {
		return "", errors.New("remove the FORMAT clause: Brain always reads results as JSON")
	}
	return s, nil
}

// withDiagTimeout bounds a tool call's queries to diagTimeout.
func withDiagTimeout(tctx Context) (Context, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(tctx.context(), diagTimeout)
	tctx.Ctx = ctx
	return tctx, cancel
}

// queryLogUnavailable maps the errors that mean query_log cannot be read to
// a result the model can explain, or nil for any other error.
func queryLogUnavailable(err error, chUser string) map[string]any {
	switch ErrorCode(err) {
	case 60:
		return map[string]any{
			"available": false,
			"reason":    "query_log_missing",
			"hint":      "system.query_log does not exist on this server. Enable the query_log section in the server config (it is on by default) and restart.",
		}
	case 497:
		user := strings.TrimSpace(chUser)
		if user == "" {
			user = "<user>"
		}
		return map[string]any{
			"available": false,
			"reason":    "not_granted",
			"hint":      fmt.Sprintf("GRANT SELECT ON system.query_log TO %s", user),
		}
	}
	return nil
}

// localClusterNote returns a note when the server is part of a cluster: the
// diagnostics read the connected node's system tables only.
func localClusterNote(tctx Context) string {
	sql := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(clusterhealth.ResolveClusterQuery), "FORMAT JSON"))
	rows, err := runSelect(tctx, sql, 8*time.Second)
	if err != nil || len(rows) == 0 {
		return ""
	}
	name, _ := rows[0]["cluster"].(string)
	if !clusterhealth.IsValidClusterName(name) {
		return ""
	}
	return fmt.Sprintf("This server is part of cluster %q. Results are local-node only: they cover the connected node, not the other replicas or shards.", name)
}

var (
	trailingLimitRE = regexp.MustCompile(`(?i)\bLIMIT\s+\d+\s*$`)
)

// plainTopN turns a queryinsights builder's statement into one Brain can run:
// no FORMAT clause (Brain reads JSON on its own) and a LIMIT of n.
func plainTopN(sql string, n int) string {
	s := strings.TrimSpace(trailingFormatRE.ReplaceAllString(strings.TrimSpace(sql), ""))
	if trailingLimitRE.MatchString(s) {
		return trailingLimitRE.ReplaceAllString(s, fmt.Sprintf("LIMIT %d", n))
	}
	return fmt.Sprintf("%s\nLIMIT %d", s, n)
}

// clipRunes shortens s to at most n runes, marking the cut.
func clipRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "..."
}

// num reads a ClickHouse JSON number, which may arrive quoted (64-bit ints).
func num(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case string:
		f, _ := strconv.ParseFloat(x, 64)
		return f
	case json.Number:
		f, _ := x.Float64()
		return f
	}
	return 0
}

func diagWindow(name string) (queryinsights.Range, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "24h"
	}
	if !diagWindows[name] {
		return queryinsights.Range{}, fmt.Errorf("window must be one of 1h, 6h, 24h, 7d; got %q", name)
	}
	rng, _ := queryinsights.RangeSpec(name)
	return rng, nil
}

// ── analyze_query_log ──────────────────────────────────────────────────────

var queryLogViews = map[string]func(string, queryinsights.Range, queryinsights.Filters) string{
	"slow":          queryinsights.SlowQueriesQuery,
	"failed":        queryinsights.ErrorsQuery,
	"heavy_readers": queryinsights.HeavyReadersQuery,
	"patterns":      queryinsights.FrequentQuery,
	"users":         queryinsights.UsersQuery,
}

// analyzeQueryLogSQL builds the statement for one view. Local node only.
func analyzeQueryLogSQL(view string, rng queryinsights.Range, f queryinsights.Filters) (string, error) {
	build, ok := queryLogViews[view]
	if !ok {
		return "", fmt.Errorf("view must be one of slow, failed, heavy_readers, patterns, users; got %q", view)
	}
	f.ExcludeLogComments = diagExcludedComments
	f.ExcludeLogCommentPrefixes = diagExcludedCommentPrefixes
	return plainTopN(build("", rng, f), diagTopN), nil
}

var analyzeQueryLog = Tool{
	Name:        "analyze_query_log",
	Description: "Find what is slow, failing or expensive in system.query_log over a window. Views: slow (query patterns by p95 latency), failed (failures grouped by error code), heavy_readers (patterns that read the most bytes), patterns (most-run patterns), users (load per ClickHouse user). Returns the top 20 rows; the pattern views carry a hash to pass to query_pattern. Local node only.",
	Parameters: mustJSON(map[string]any{
		"type":     "object",
		"required": []string{"view"},
		"properties": map[string]any{
			"view":            map[string]any{"type": "string", "enum": []string{"slow", "failed", "heavy_readers", "patterns", "users"}},
			"window":          map[string]any{"type": "string", "enum": []string{"1h", "6h", "24h", "7d"}, "description": "Default 24h."},
			"user":            map[string]any{"type": "string", "description": "Only this ClickHouse user."},
			"table":           map[string]any{"type": "string", "description": "Only queries touching this table, as database.table."},
			"min_duration_ms": map[string]any{"type": "integer", "minimum": 0, "description": "Only queries at least this slow."},
		},
	}),
	Handler: func(tctx Context, args json.RawMessage) (any, error) {
		var input struct {
			View          string `json:"view"`
			Window        string `json:"window"`
			User          string `json:"user"`
			Table         string `json:"table"`
			MinDurationMS int    `json:"min_duration_ms"`
		}
		if err := json.Unmarshal(args, &input); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}
		rng, err := diagWindow(input.Window)
		if err != nil {
			return nil, err
		}
		view := strings.TrimSpace(input.View)
		sql, err := analyzeQueryLogSQL(view, rng, queryinsights.Filters{
			User:          strings.TrimSpace(input.User),
			Table:         strings.TrimSpace(input.Table),
			MinDurationMS: input.MinDurationMS,
		})
		if err != nil {
			return nil, err
		}
		tctx, cancel := withDiagTimeout(tctx)
		defer cancel()

		rows, err := runSelect(tctx, sql, diagTimeout)
		if err != nil {
			if out := queryLogUnavailable(err, tctx.CHUser); out != nil {
				return out, nil
			}
			return nil, err
		}
		for _, row := range rows {
			if s, ok := row["sample_query"].(string); ok {
				row["sample_query"] = clipRunes(s, diagSampleCap)
			}
		}
		out := map[string]any{"available": true, "view": view, "window": rng.Name, "rows": rows}
		if len(rows) == 0 {
			out["note"] = queryLogEmptyNote
		}
		if note := localClusterNote(tctx); note != "" {
			out["scope"] = note
		}
		return out, nil
	},
}

// ── query_pattern ──────────────────────────────────────────────────────────

var patternHashRE = regexp.MustCompile(`^\d{1,20}$`)

// patternWhere selects one normalized pattern's finished queries in a window.
func patternWhere(rng queryinsights.Range, hash string) string {
	quoted := make([]string, 0, len(diagExcludedComments))
	for _, c := range diagExcludedComments {
		quoted = append(quoted, "'"+sqlEscape(c)+"'")
	}
	return fmt.Sprintf(`event_date >= toDate(now() - INTERVAL %s)
  AND event_time >= now() - INTERVAL %s
  AND is_initial_query = 1
  AND type = 'QueryFinish'
  AND normalized_query_hash = %s
  AND log_comment NOT IN (%s)
  AND NOT startsWith(log_comment, '%s')`, rng.Interval, rng.Interval, hash, strings.Join(quoted, ", "), sqlEscape(tunnel.LogCommentPrefix("brain")))
}

func queryPatternSQL(rng queryinsights.Range, hash string) (summary, users string) {
	where := patternWhere(rng, hash)
	summary = fmt.Sprintf(`SELECT
  count() AS runs,
  round(quantile(0.5)(query_duration_ms), 1) AS p50_ms,
  round(quantile(0.95)(query_duration_ms), 1) AS p95_ms,
  max(query_duration_ms) AS max_ms,
  sum(read_rows) AS read_rows,
  sum(read_bytes) AS read_bytes,
  round(avg(read_rows)) AS avg_read_rows,
  round(avg(read_bytes)) AS avg_read_bytes,
  max(memory_usage) AS max_memory,
  round(avg(ProfileEvents['SelectedParts']), 1) AS avg_selected_parts,
  round(avg(ProfileEvents['SelectedMarks']), 1) AS avg_selected_marks,
  round(avg(ProfileEvents['SelectedRows'])) AS avg_selected_rows,
  substring(argMax(query, event_time), 1, %d) AS example_query,
  min(event_time) AS first_seen,
  max(event_time) AS last_seen
FROM system.query_log
WHERE %s
LIMIT 1`, diagExampleCap, where)
	users = fmt.Sprintf(`SELECT
  user,
  count() AS runs,
  round(quantile(0.95)(query_duration_ms), 1) AS p95_ms,
  sum(read_rows) AS read_rows,
  sum(read_bytes) AS read_bytes
FROM system.query_log
WHERE %s
GROUP BY user
ORDER BY runs DESC
LIMIT %d`, where, diagTopN)
	return summary, users
}

var queryPatternTool = Tool{
	Name:        "query_pattern",
	Description: "Drill into one normalized query pattern from analyze_query_log (its hash): runs, p50/p95/max latency, rows and bytes read, average SelectedParts/SelectedMarks/SelectedRows from ProfileEvents, a per-user split and one example query. Local node only.",
	Parameters: mustJSON(map[string]any{
		"type":     "object",
		"required": []string{"hash"},
		"properties": map[string]any{
			"hash":   map[string]any{"type": "string", "description": "normalized_query_hash from analyze_query_log."},
			"window": map[string]any{"type": "string", "enum": []string{"1h", "6h", "24h", "7d"}, "description": "Default 24h."},
		},
	}),
	Handler: func(tctx Context, args json.RawMessage) (any, error) {
		var input struct {
			Hash   json.RawMessage `json:"hash"`
			Window string          `json:"window"`
		}
		if err := json.Unmarshal(args, &input); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}
		hash := strings.Trim(strings.TrimSpace(string(input.Hash)), `"`)
		if !patternHashRE.MatchString(hash) {
			return nil, errors.New("hash must be the numeric normalized_query_hash from analyze_query_log")
		}
		rng, err := diagWindow(input.Window)
		if err != nil {
			return nil, err
		}
		summarySQL, usersSQL := queryPatternSQL(rng, hash)
		tctx, cancel := withDiagTimeout(tctx)
		defer cancel()

		summary, err := runSelect(tctx, summarySQL, diagTimeout)
		if err != nil {
			if out := queryLogUnavailable(err, tctx.CHUser); out != nil {
				return out, nil
			}
			return nil, err
		}
		out := map[string]any{"available": true, "hash": hash, "window": rng.Name}
		if len(summary) == 0 || num(summary[0]["runs"]) == 0 {
			out["note"] = queryLogEmptyNote
			return out, nil
		}
		out["summary"] = summary[0]
		users, err := runSelect(tctx, usersSQL, diagTimeout)
		if err != nil {
			out["users_error"] = clipRunes(err.Error(), diagErrorCap)
		} else {
			out["users"] = users
		}
		if note := localClusterNote(tctx); note != "" {
			out["scope"] = note
		}
		return out, nil
	},
}

// ── explain_query ──────────────────────────────────────────────────────────

var explainPrefixes = map[string]string{
	"indexes":  "EXPLAIN indexes = 1 ",
	"pipeline": "EXPLAIN PIPELINE ",
	"estimate": "EXPLAIN ESTIMATE ",
	"plan":     "EXPLAIN PLAN ",
}

// buildExplain validates the inner query and returns the EXPLAIN statement.
func buildExplain(mode, sql string) (inner, stmt string, err error) {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		mode = "indexes"
	}
	prefix, ok := explainPrefixes[mode]
	if !ok {
		return "", "", fmt.Errorf("mode must be one of indexes, pipeline, estimate, plan; got %q", mode)
	}
	inner, err = checkInnerSelect(sql)
	if err != nil {
		return "", "", err
	}
	return inner, prefix + inner, nil
}

// runExplain runs an EXPLAIN after the guardrail has seen the inner query.
func runExplain(tctx Context, inner, stmt string) ([]map[string]any, error) {
	if err := tctx.checkGuardrail(inner); err != nil {
		return nil, err
	}
	return runSelect(tctx, stmt, diagTimeout)
}

// explainLines returns the "explain" column of EXPLAIN output, capped.
func explainLines(rows []map[string]any) (lines []string, truncated bool) {
	for _, row := range rows {
		if len(lines) >= diagExplainMaxLines {
			return lines, true
		}
		s, _ := row["explain"].(string)
		lines = append(lines, s)
	}
	return lines, false
}

// estimateTotals sums EXPLAIN ESTIMATE rows (one per table read).
type estimateTotals struct {
	Parts int64 `json:"parts"`
	Rows  int64 `json:"rows"`
	Marks int64 `json:"marks"`
}

func sumEstimate(rows []map[string]any) estimateTotals {
	var t estimateTotals
	for _, r := range rows {
		t.Parts += int64(num(r["parts"]))
		t.Rows += int64(num(r["rows"]))
		t.Marks += int64(num(r["marks"]))
	}
	return t
}

var explainQueryTool = Tool{
	Name:        "explain_query",
	Description: "EXPLAIN a single SELECT or WITH query without running it. mode: indexes (EXPLAIN indexes = 1: which partitions, parts and granules the primary key and skip indexes keep; the default), pipeline (EXPLAIN PIPELINE: processors and parallelism), estimate (EXPLAIN ESTIMATE: rows, marks and parts to read per table), plan (EXPLAIN PLAN). Pass the query itself, not an EXPLAIN, with no SETTINGS or FORMAT clause.",
	Parameters: mustJSON(map[string]any{
		"type":     "object",
		"required": []string{"sql"},
		"properties": map[string]any{
			"sql":  map[string]any{"type": "string", "description": "A single SELECT or WITH query."},
			"mode": map[string]any{"type": "string", "enum": []string{"indexes", "pipeline", "estimate", "plan"}, "description": "Default indexes."},
		},
	}),
	Handler: func(tctx Context, args json.RawMessage) (any, error) {
		var input struct {
			SQL  string `json:"sql"`
			Mode string `json:"mode"`
		}
		if err := json.Unmarshal(args, &input); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}
		inner, stmt, err := buildExplain(input.Mode, input.SQL)
		if err != nil {
			return nil, err
		}
		mode := strings.ToLower(strings.TrimSpace(input.Mode))
		if mode == "" {
			mode = "indexes"
		}
		tctx, cancel := withDiagTimeout(tctx)
		defer cancel()
		rows, err := runExplain(tctx, inner, stmt)
		if err != nil {
			return nil, err
		}
		if mode == "estimate" {
			return map[string]any{"mode": mode, "statement": stmt, "tables": rows, "totals": sumEstimate(rows)}, nil
		}
		lines, truncated := explainLines(rows)
		out := map[string]any{"mode": mode, "statement": stmt, "text": strings.Join(lines, "\n")}
		if truncated {
			out["truncated"] = true
		}
		if mode == "indexes" {
			if g, ok := parseGranules(lines); ok {
				out["granules"] = g
			}
		}
		return out, nil
	},
}

// granuleCounts are the granules the indexes kept out of those considered,
// summed over every table read.
type granuleCounts struct {
	Selected int64 `json:"selected"`
	Total    int64 `json:"total"`
}

var granulesLineRE = regexp.MustCompile(`^Granules:\s*(\d+)/(\d+)`)

// parseGranules reads EXPLAIN indexes = 1 output. Each ReadFromMergeTree
// step lists its indexes in order (MinMax, Partition, PrimaryKey, Skip), so
// a step's first Granules line has the total and its last the granules
// left after every index.
func parseGranules(lines []string) (granuleCounts, bool) {
	type step struct {
		first, last [2]int64
		seen        bool
	}
	var steps []step
	cur := -1
	for _, l := range lines {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "ReadFromMergeTree") {
			steps = append(steps, step{})
			cur = len(steps) - 1
			continue
		}
		m := granulesLineRE.FindStringSubmatch(t)
		if m == nil {
			continue
		}
		if cur < 0 {
			steps = append(steps, step{})
			cur = 0
		}
		sel, _ := strconv.ParseInt(m[1], 10, 64)
		tot, _ := strconv.ParseInt(m[2], 10, 64)
		s := &steps[cur]
		if !s.seen {
			s.first = [2]int64{sel, tot}
			s.seen = true
		}
		s.last = [2]int64{sel, tot}
	}
	var g granuleCounts
	found := false
	for _, s := range steps {
		if !s.seen {
			continue
		}
		found = true
		g.Selected += s.last[0]
		g.Total += s.first[1]
	}
	return g, found
}

// ── compare_query_estimates ────────────────────────────────────────────────

// queryEstimate is what ClickHouse expects one query to read.
type queryEstimate struct {
	Rows             int64 `json:"rows"`
	Marks            int64 `json:"marks"`
	Parts            int64 `json:"parts"`
	GranulesSelected int64 `json:"granules_selected"`
	GranulesTotal    int64 `json:"granules_total"`
	// GranulesKnown is false when EXPLAIN indexes found no MergeTree read.
	GranulesKnown bool `json:"granules_known"`
}

func estimateQuery(tctx Context, sql string) (queryEstimate, error) {
	var e queryEstimate
	inner, estStmt, err := buildExplain("estimate", sql)
	if err != nil {
		return e, err
	}
	rows, err := runExplain(tctx, inner, estStmt)
	if err != nil {
		return e, err
	}
	t := sumEstimate(rows)
	e.Rows, e.Marks, e.Parts = t.Rows, t.Marks, t.Parts

	_, idxStmt, _ := buildExplain("indexes", inner)
	idxRows, err := runSelect(tctx, idxStmt, diagTimeout)
	if err != nil {
		return e, err
	}
	lines, _ := explainLines(idxRows)
	if g, ok := parseGranules(lines); ok {
		e.GranulesSelected, e.GranulesTotal, e.GranulesKnown = g.Selected, g.Total, true
	}
	return e, nil
}

// pctChange is the change from a to b in percent, or nil when a is zero.
func pctChange(a, b int64) any {
	if a == 0 {
		return nil
	}
	return float64(int64(float64(b-a)/float64(a)*1000)) / 10
}

var compareQueryEstimatesTool = Tool{
	Name:        "compare_query_estimates",
	Description: "Compare what two SELECT queries would read, without running them: EXPLAIN ESTIMATE (rows, marks, parts) and EXPLAIN indexes = 1 (granules kept) for the original and the candidate, with before/after numbers and deltas. Use it to show that a rewrite reads less. Estimates describe data read, not latency.",
	Parameters: mustJSON(map[string]any{
		"type":     "object",
		"required": []string{"original_sql", "candidate_sql"},
		"properties": map[string]any{
			"original_sql":  map[string]any{"type": "string"},
			"candidate_sql": map[string]any{"type": "string"},
		},
	}),
	Handler: func(tctx Context, args json.RawMessage) (any, error) {
		var input struct {
			Original  string `json:"original_sql"`
			Candidate string `json:"candidate_sql"`
		}
		if err := json.Unmarshal(args, &input); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}
		if _, err := checkInnerSelect(input.Original); err != nil {
			return nil, fmt.Errorf("original_sql: %w", err)
		}
		if _, err := checkInnerSelect(input.Candidate); err != nil {
			return nil, fmt.Errorf("candidate_sql: %w", err)
		}
		tctx, cancel := withDiagTimeout(tctx)
		defer cancel()
		before, err := estimateQuery(tctx, input.Original)
		if err != nil {
			return nil, fmt.Errorf("original_sql: %w", err)
		}
		after, err := estimateQuery(tctx, input.Candidate)
		if err != nil {
			return nil, fmt.Errorf("candidate_sql: %w", err)
		}
		delta := map[string]any{
			"rows":      after.Rows - before.Rows,
			"marks":     after.Marks - before.Marks,
			"parts":     after.Parts - before.Parts,
			"rows_pct":  pctChange(before.Rows, after.Rows),
			"marks_pct": pctChange(before.Marks, after.Marks),
		}
		if before.GranulesKnown && after.GranulesKnown {
			delta["granules"] = after.GranulesSelected - before.GranulesSelected
			delta["granules_pct"] = pctChange(before.GranulesSelected, after.GranulesSelected)
		}
		return map[string]any{
			"before": before,
			"after":  after,
			"delta":  delta,
			"note":   "Estimates describe how much data ClickHouse expects to read, not how long the query takes.",
		}, nil
	},
}

// ── table_storage ──────────────────────────────────────────────────────────

const (
	maxPartitionsPerTable = 1000
	maxPartsPerPartition  = 300
)

func tableStorageSQL(database, table string) (tables, partitions, columns string) {
	filter := fmt.Sprintf("database = '%s'", sqlEscape(database))
	if table != "" {
		filter += fmt.Sprintf(" AND table = '%s'", sqlEscape(table))
	}
	tables = fmt.Sprintf(`SELECT
  table,
  count() AS partitions,
  sum(parts) AS active_parts,
  max(parts) AS max_parts_in_partition,
  sum(rows) AS rows,
  sum(bytes) AS bytes_on_disk,
  sum(compressed) AS compressed_bytes,
  sum(uncompressed) AS uncompressed_bytes,
  round(sum(uncompressed) / greatest(sum(compressed), 1), 2) AS compression_ratio,
  max(bytes) AS max_partition_bytes,
  quantileExact(0.5)(bytes) AS median_partition_bytes
FROM (
  SELECT table, partition, count() AS parts, sum(rows) AS rows, sum(bytes_on_disk) AS bytes,
    sum(data_compressed_bytes) AS compressed, sum(data_uncompressed_bytes) AS uncompressed
  FROM system.parts
  WHERE active AND %s
  GROUP BY table, partition
)
GROUP BY table
ORDER BY bytes_on_disk DESC
LIMIT 50`, filter)
	partitions = fmt.Sprintf(`SELECT table, partition, count() AS parts, sum(rows) AS rows, sum(bytes_on_disk) AS bytes_on_disk
FROM system.parts
WHERE active AND %s
GROUP BY table, partition
ORDER BY bytes_on_disk DESC
LIMIT 50`, filter)
	columns = fmt.Sprintf(`SELECT table, name, type, data_compressed_bytes, data_uncompressed_bytes,
  round(data_uncompressed_bytes / greatest(data_compressed_bytes, 1), 2) AS compression_ratio, compression_codec
FROM system.columns
WHERE %s
ORDER BY data_compressed_bytes DESC
LIMIT 20`, filter)
	return tables, partitions, columns
}

// storageFlags names the layouts that hurt: too many partitions or parts.
func storageFlags(tables []map[string]any) []string {
	var flags []string
	for _, t := range tables {
		name, _ := t["table"].(string)
		if p := num(t["partitions"]); p > maxPartitionsPerTable {
			flags = append(flags, fmt.Sprintf("%s has %d active partitions (over %d): the partition key is probably too fine-grained.", name, int64(p), maxPartitionsPerTable))
		}
		if p := num(t["max_parts_in_partition"]); p > maxPartsPerPartition {
			flags = append(flags, fmt.Sprintf("%s has a partition with %d active parts (over %d): inserts are too small or too frequent, or merges are behind.", name, int64(p), maxPartsPerPartition))
		}
		if med := num(t["median_partition_bytes"]); med > 0 {
			skew := num(t["max_partition_bytes"]) / med
			t["partition_skew"] = float64(int64(skew*10)) / 10
		}
	}
	return flags
}

var tableStorageTool = Tool{
	Name:        "table_storage",
	Description: "How a database's tables (or one table) are stored: active rows, bytes on disk, compression ratio, partitions and parts per table with skew (largest partition over median), the 50 largest partitions and the 20 largest columns. Flags tables with over 1000 partitions or over 300 parts in one partition. Local node only.",
	Parameters: mustJSON(map[string]any{
		"type":     "object",
		"required": []string{"database"},
		"properties": map[string]any{
			"database": map[string]any{"type": "string"},
			"table":    map[string]any{"type": "string", "description": "Omit for every table in the database."},
		},
	}),
	Handler: func(tctx Context, args json.RawMessage) (any, error) {
		var input struct {
			Database string `json:"database"`
			Table    string `json:"table"`
		}
		if err := json.Unmarshal(args, &input); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}
		db := strings.TrimSpace(input.Database)
		tbl := strings.TrimSpace(input.Table)
		if db == "" {
			return nil, errors.New("database is required")
		}
		tablesSQL, partsSQL, colsSQL := tableStorageSQL(db, tbl)
		tctx, cancel := withDiagTimeout(tctx)
		defer cancel()

		tables, err := runSelect(tctx, tablesSQL, diagTimeout)
		if err != nil {
			return nil, err
		}
		out := map[string]any{"database": db, "tables": tables}
		if tbl != "" {
			out["table"] = tbl
		}
		if len(tables) == 0 {
			out["note"] = "No active parts found. The table may be empty, not a MergeTree table, or not visible to this user."
			return out, nil
		}
		flags := storageFlags(tables)
		if len(flags) > 0 {
			out["flags"] = flags
		}
		if parts, err := runSelect(tctx, partsSQL, diagTimeout); err == nil {
			out["largest_partitions"] = parts
		} else {
			out["partitions_error"] = clipRunes(err.Error(), diagErrorCap)
		}
		if cols, err := runSelect(tctx, colsSQL, diagTimeout); err == nil {
			out["largest_columns"] = cols
		} else {
			out["columns_error"] = clipRunes(err.Error(), diagErrorCap)
		}
		if note := localClusterNote(tctx); note != "" {
			out["scope"] = note
		}
		return out, nil
	},
}

// ── merges_and_mutations ───────────────────────────────────────────────────

const (
	slowMergeSeconds    = 3600
	stuckMutationSecond = 1800
)

func mergesSQL(database, table string) (merges, mutations string) {
	var filter strings.Builder
	if database != "" {
		fmt.Fprintf(&filter, " AND database = '%s'", sqlEscape(database))
	}
	if table != "" {
		fmt.Fprintf(&filter, " AND table = '%s'", sqlEscape(table))
	}
	merges = fmt.Sprintf(`SELECT database, table, round(elapsed, 1) AS elapsed_s, round(progress, 3) AS progress,
  num_parts, result_part_name, total_size_bytes_compressed, is_mutation
FROM system.merges
WHERE 1 = 1%s
ORDER BY elapsed DESC
LIMIT 50`, filter.String())
	mutations = fmt.Sprintf(`SELECT database, table, mutation_id, substring(command, 1, 500) AS command, create_time,
  dateDiff('second', create_time, now()) AS age_s, parts_to_do, latest_fail_reason, latest_fail_time
FROM system.mutations
WHERE is_done = 0%s
ORDER BY create_time ASC
LIMIT 50`, filter.String())
	return merges, mutations
}

func mergeFlags(merges, mutations []map[string]any) []string {
	var flags []string
	for _, m := range merges {
		if s := num(m["elapsed_s"]); s > slowMergeSeconds {
			flags = append(flags, fmt.Sprintf("Merge into %v on %v.%v has run for %.0f minutes.", m["result_part_name"], m["database"], m["table"], s/60))
		}
	}
	for _, m := range mutations {
		age := num(m["age_s"])
		reason, _ := m["latest_fail_reason"].(string)
		if reason != "" {
			flags = append(flags, fmt.Sprintf("Mutation %v on %v.%v is failing: %s", m["mutation_id"], m["database"], m["table"], clipRunes(reason, 300)))
			continue
		}
		if age > stuckMutationSecond && num(m["parts_to_do"]) > 0 {
			flags = append(flags, fmt.Sprintf("Mutation %v on %v.%v is %.0f minutes old with %d parts still to do.", m["mutation_id"], m["database"], m["table"], age/60, int64(num(m["parts_to_do"]))))
		}
	}
	return flags
}

var mergesAndMutationsTool = Tool{
	Name:        "merges_and_mutations",
	Description: "Running merges (system.merges) and unfinished mutations (system.mutations, is_done = 0), optionally for one database or table. Flags merges running over 1 hour and mutations older than 30 minutes that still have parts to do or a failure reason. Local node only.",
	Parameters: mustJSON(map[string]any{
		"type": "object",
		"properties": map[string]any{
			"database": map[string]any{"type": "string"},
			"table":    map[string]any{"type": "string"},
		},
	}),
	Handler: func(tctx Context, args json.RawMessage) (any, error) {
		var input struct {
			Database string `json:"database"`
			Table    string `json:"table"`
		}
		_ = json.Unmarshal(args, &input)
		mSQL, mutSQL := mergesSQL(strings.TrimSpace(input.Database), strings.TrimSpace(input.Table))
		tctx, cancel := withDiagTimeout(tctx)
		defer cancel()

		merges, err := runSelect(tctx, mSQL, diagTimeout)
		if err != nil {
			return nil, err
		}
		mutations, err := runSelect(tctx, mutSQL, diagTimeout)
		if err != nil {
			return nil, err
		}
		out := map[string]any{"merges": merges, "mutations": mutations}
		if flags := mergeFlags(merges, mutations); len(flags) > 0 {
			sort.Strings(flags)
			out["flags"] = flags
		}
		if note := localClusterNote(tctx); note != "" {
			out["scope"] = note
		}
		return out, nil
	},
}
