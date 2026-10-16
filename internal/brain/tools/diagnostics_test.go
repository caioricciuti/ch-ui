// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package tools

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/caioricciuti/ch-ui/internal/queryinsights"
	"github.com/caioricciuti/ch-ui/internal/testutil"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

// diagFake answers each statement with the first matching canned reply and
// records every statement that reached ClickHouse.
type diagFake struct {
	mu      sync.Mutex
	sql     []string
	replies []diagReply
}

type diagReply struct {
	match string // substring of the statement
	data  string // JSON rows, or "" for an error
	err   string
}

func (f *diagFake) respond(msg tunnel.GatewayMessage) *tunnel.AgentMessage {
	f.mu.Lock()
	f.sql = append(f.sql, msg.SQL)
	f.mu.Unlock()
	for _, r := range f.replies {
		if strings.Contains(msg.SQL, r.match) {
			if r.err != "" {
				return &tunnel.AgentMessage{Type: "query_error", Error: r.err}
			}
			return &tunnel.AgentMessage{Type: "query_result", Data: json.RawMessage(r.data), Meta: json.RawMessage(`[]`)}
		}
	}
	return &tunnel.AgentMessage{Type: "query_result", Data: json.RawMessage(`[]`), Meta: json.RawMessage(`[]`)}
}

func (f *diagFake) statements() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.sql...)
}

func diagCtx(t *testing.T, replies ...diagReply) (Context, *diagFake) {
	t.Helper()
	db, conn := testutil.WorkerDB(t)
	f := &diagFake{replies: replies}
	agent := testutil.NewAgent(t, db, "worker-token", f.respond)
	return Context{Ctx: context.Background(), ConnectionID: conn, CHUser: "alice", Gateway: agent.Gateway}, f
}

var (
	limitAtEndRE = regexp.MustCompile(`(?i)\bLIMIT\s+\d+\s*$`)
	formatClause = regexp.MustCompile(`(?i)\bFORMAT\s+\w+`)
	eventDateRE  = regexp.MustCompile(`event_date >= toDate\(now\(\) - INTERVAL \d+ (HOUR|DAY)\)`)
	eventTimeRE  = regexp.MustCompile(`event_time >= now\(\) - INTERVAL \d+ (HOUR|DAY)`)
	excludeBrain = "log_comment NOT IN ('ch-ui:query-insights')\n  AND NOT startsWith(log_comment, '{\"chui\":\"brain\"')"
)

func assertDiagShape(t *testing.T, name, sql string, queryLog bool) {
	t.Helper()
	if !limitAtEndRE.MatchString(strings.TrimSpace(sql)) {
		t.Errorf("%s: no LIMIT at the end:\n%s", name, sql)
	}
	if formatClause.MatchString(sql) {
		t.Errorf("%s: carries a FORMAT clause:\n%s", name, sql)
	}
	if queryLog {
		if !eventDateRE.MatchString(sql) || !eventTimeRE.MatchString(sql) {
			t.Errorf("%s: no event_date and event_time window:\n%s", name, sql)
		}
		if !strings.Contains(sql, excludeBrain) {
			t.Errorf("%s: does not exclude CH-UI's own queries:\n%s", name, sql)
		}
		if strings.Contains(sql, "clusterAllReplicas") {
			t.Errorf("%s: must read the local node only", name)
		}
	}
}

func TestBrainAnalyzeQueryLogSQLShape(t *testing.T) {
	for _, window := range []string{"1h", "6h", "24h", "7d"} {
		rng, err := diagWindow(window)
		if err != nil {
			t.Fatal(err)
		}
		for view := range queryLogViews {
			sql, err := analyzeQueryLogSQL(view, rng, queryinsights.Filters{User: "bob", Table: "db.t", MinDurationMS: 50})
			if err != nil {
				t.Fatal(err)
			}
			assertDiagShape(t, view+"/"+window, sql, true)
			if !strings.HasSuffix(strings.TrimSpace(sql), "LIMIT 20") {
				t.Errorf("%s: want top 20, got %q", view, sql[len(sql)-20:])
			}
			for _, want := range []string{"user = 'bob'", "has(tables, 'db.t')", "query_duration_ms >= 50"} {
				if !strings.Contains(sql, want) {
					t.Errorf("%s: missing filter %s", view, want)
				}
			}
		}
	}
	if _, err := diagWindow("30d"); err == nil {
		t.Error("30d is not offered to Brain")
	}
	rng, _ := diagWindow("")
	if rng.Name != "24h" {
		t.Errorf("default window %q", rng.Name)
	}
	if _, err := analyzeQueryLogSQL("everything", rng, queryinsights.Filters{}); err == nil {
		t.Error("unknown view must be rejected")
	}
}

func TestBrainQueryPatternSQLShape(t *testing.T) {
	rng, _ := diagWindow("6h")
	summary, users := queryPatternSQL(rng, "123456789")
	for name, sql := range map[string]string{"summary": summary, "users": users} {
		assertDiagShape(t, name, sql, true)
		if !strings.Contains(sql, "normalized_query_hash = 123456789") {
			t.Errorf("%s: no hash predicate", name)
		}
	}
	for _, want := range []string{"ProfileEvents['SelectedParts']", "ProfileEvents['SelectedMarks']", "ProfileEvents['SelectedRows']", "quantile(0.5)", "substring(argMax(query, event_time), 1, 2000)"} {
		if !strings.Contains(summary, want) {
			t.Errorf("summary missing %s", want)
		}
	}

	tctx, f := diagCtx(t)
	if _, err := queryPatternTool.Handler(tctx, json.RawMessage(`{"hash":"1 OR 1=1"}`)); err == nil {
		t.Error("a non-numeric hash must be rejected")
	}
	if len(f.statements()) != 0 {
		t.Error("a rejected hash must not reach ClickHouse")
	}
	out, err := queryPatternTool.Handler(tctx, json.RawMessage(`{"hash":42}`))
	if err != nil {
		t.Fatal(err)
	}
	if m := out.(map[string]any); m["note"] == nil {
		t.Errorf("zero rows should carry the log_queries note: %v", m)
	}
}

func TestBrainStorageAndMergesSQLShape(t *testing.T) {
	tables, parts, cols := tableStorageSQL("db", "t'x")
	for name, sql := range map[string]string{"tables": tables, "parts": parts, "cols": cols} {
		assertDiagShape(t, name, sql, false)
		if !strings.Contains(sql, "table = 't''x'") {
			t.Errorf("%s: table not escaped:\n%s", name, sql)
		}
	}
	merges, mutations := mergesSQL("db", "")
	for name, sql := range map[string]string{"merges": merges, "mutations": mutations} {
		assertDiagShape(t, name, sql, false)
	}
	if !strings.Contains(mutations, "is_done = 0") {
		t.Error("only unfinished mutations")
	}
}

func TestBrainStorageAndMergeFlags(t *testing.T) {
	tables := []map[string]any{
		{"table": "events", "partitions": "1500", "max_parts_in_partition": 12, "max_partition_bytes": "1000", "median_partition_bytes": "100"},
		{"table": "logs", "partitions": 3.0, "max_parts_in_partition": "400"},
	}
	flags := storageFlags(tables)
	if len(flags) != 2 || !strings.Contains(flags[0], "events has 1500") || !strings.Contains(flags[1], "logs has a partition with 400") {
		t.Errorf("flags %v", flags)
	}
	if tables[0]["partition_skew"] != 10.0 {
		t.Errorf("skew %v", tables[0]["partition_skew"])
	}

	mflags := mergeFlags(
		[]map[string]any{{"elapsed_s": 4000.0, "result_part_name": "all_1_9_2", "database": "db", "table": "t"}, {"elapsed_s": 10.0}},
		[]map[string]any{
			{"mutation_id": "m1", "database": "db", "table": "t", "age_s": "3600", "parts_to_do": "5", "latest_fail_reason": ""},
			{"mutation_id": "m2", "database": "db", "table": "t", "age_s": "60", "parts_to_do": "5", "latest_fail_reason": "Code: 341. boom"},
			{"mutation_id": "m3", "database": "db", "table": "t", "age_s": "3600", "parts_to_do": "0", "latest_fail_reason": ""},
		},
	)
	if len(mflags) != 3 {
		t.Errorf("merge flags %v", mflags)
	}
}

func TestBrainExplainRejectsInnerSQL(t *testing.T) {
	tctx, f := diagCtx(t)
	for sql, want := range map[string]string{
		"EXPLAIN SELECT 1":                     "not an EXPLAIN",
		"SELECT 1; SELECT 2":                   "single statement",
		"INSERT INTO t VALUES (1)":             "SELECT or WITH",
		"SHOW TABLES":                          "SELECT or WITH",
		"SELECT 1 SETTINGS max_threads = 1":    "SETTINGS",
		"SELECT 1 FORMAT CSV":                  "FORMAT",
		"":                                     "required",
		"/* x */ EXPLAIN PIPELINE SELECT 1":    "not an EXPLAIN",
		"SELECT 1;\nDROP TABLE t":              "single statement",
		"ALTER TABLE t DELETE WHERE 1 = 1":     "SELECT or WITH",
		"WITH 1 AS x SELECT x; DROP TABLE abc": "single statement",
	} {
		args, _ := json.Marshal(map[string]string{"sql": sql})
		if _, err := explainQueryTool.Handler(tctx, args); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: want %q, got %v", sql, want, err)
		}
	}
	if _, err := explainQueryTool.Handler(tctx, json.RawMessage(`{"sql":"SELECT 1","mode":"ast"}`)); err == nil {
		t.Error("unknown mode must be rejected")
	}
	if _, err := compareQueryEstimatesTool.Handler(tctx, json.RawMessage(`{"original_sql":"SELECT 1","candidate_sql":"DROP TABLE t"}`)); err == nil || !strings.Contains(err.Error(), "candidate_sql") {
		t.Errorf("compare must validate both queries, got %v", err)
	}
	if n := len(f.statements()); n != 0 {
		t.Errorf("%d rejected statements reached ClickHouse", n)
	}

	// A semicolon or the word EXPLAIN inside a literal is fine.
	if _, err := explainQueryTool.Handler(tctx, json.RawMessage(`{"sql":"SELECT 'a; EXPLAIN' AS s;","mode":"plan"}`)); err != nil {
		t.Fatal(err)
	}
	if got := f.statements(); len(got) != 1 || got[0] != "EXPLAIN PLAN SELECT 'a; EXPLAIN' AS s" {
		t.Errorf("statements %q", got)
	}
}

func TestBrainExplainModesAndGuardrail(t *testing.T) {
	tctx, f := diagCtx(t, diagReply{match: "EXPLAIN indexes = 1", data: `[{"explain":"Expression"},{"explain":"  ReadFromMergeTree (db.t)"},{"explain":"  Indexes:"},{"explain":"    MinMax"},{"explain":"      Granules: 100/120"},{"explain":"    PrimaryKey"},{"explain":"      Granules: 7/100"}]`})
	out, err := explainQueryTool.Handler(tctx, json.RawMessage(`{"sql":"SELECT * FROM db.t WHERE id = 1"}`))
	if err != nil {
		t.Fatal(err)
	}
	m := out.(map[string]any)
	if m["mode"] != "indexes" || !strings.Contains(m["text"].(string), "Granules: 7/100") {
		t.Errorf("explain %v", m)
	}
	if g := m["granules"].(granuleCounts); g.Selected != 7 || g.Total != 120 {
		t.Errorf("granules %+v", g)
	}
	for mode, prefix := range map[string]string{"pipeline": "EXPLAIN PIPELINE SELECT", "estimate": "EXPLAIN ESTIMATE SELECT", "plan": "EXPLAIN PLAN SELECT"} {
		args, _ := json.Marshal(map[string]string{"sql": "SELECT 1", "mode": mode})
		if _, err := explainQueryTool.Handler(tctx, args); err != nil {
			t.Fatal(err)
		}
		got := f.statements()
		if !strings.HasPrefix(got[len(got)-1], prefix) {
			t.Errorf("%s: sent %q", mode, got[len(got)-1])
		}
	}

	before := len(f.statements())
	var seen string
	tctx.Guardrail = func(sql string) error {
		seen = sql
		return errors.New("query blocked by governance policy \"pii\"")
	}
	if _, err := explainQueryTool.Handler(tctx, json.RawMessage(`{"sql":"SELECT * FROM hr.salaries"}`)); err == nil || !strings.Contains(err.Error(), "governance") {
		t.Fatalf("want guardrail block, got %v", err)
	}
	if seen != "SELECT * FROM hr.salaries" || len(f.statements()) != before {
		t.Errorf("guardrail saw %q; a blocked EXPLAIN must not reach ClickHouse", seen)
	}
}

func TestBrainCompareQueryEstimates(t *testing.T) {
	tctx, _ := diagCtx(t,
		diagReply{match: "EXPLAIN ESTIMATE SELECT * FROM db.t WHERE day", data: `[{"database":"db","table":"t","parts":"2","rows":"8192","marks":"1"}]`},
		diagReply{match: "EXPLAIN ESTIMATE", data: `[{"database":"db","table":"t","parts":"10","rows":"819200","marks":"100"}]`},
		diagReply{match: "EXPLAIN indexes = 1 SELECT * FROM db.t WHERE day", data: `[{"explain":"ReadFromMergeTree"},{"explain":"Granules: 100/100"},{"explain":"Granules: 1/100"}]`},
		diagReply{match: "EXPLAIN indexes = 1", data: `[{"explain":"ReadFromMergeTree"},{"explain":"Granules: 100/100"}]`},
	)
	out, err := compareQueryEstimatesTool.Handler(tctx, json.RawMessage(`{"original_sql":"SELECT * FROM db.t WHERE toDate(ts) = today()","candidate_sql":"SELECT * FROM db.t WHERE day = today()"}`))
	if err != nil {
		t.Fatal(err)
	}
	m := out.(map[string]any)
	before, after := m["before"].(queryEstimate), m["after"].(queryEstimate)
	if before.Rows != 819200 || after.Rows != 8192 || before.GranulesSelected != 100 || after.GranulesSelected != 1 || after.GranulesTotal != 100 {
		t.Errorf("before %+v after %+v", before, after)
	}
	d := m["delta"].(map[string]any)
	if d["rows"] != int64(8192-819200) || d["rows_pct"] != -99.0 || d["granules"] != int64(-99) {
		t.Errorf("delta %v", d)
	}
	if !strings.Contains(m["note"].(string), "not how long") {
		t.Error("the result must say estimates are not latency")
	}
}

func TestBrainQueryLogUnavailableMapping(t *testing.T) {
	for code, reason := range map[string]string{
		"Code: 60. DB::Exception: Table system.query_log does not exist. (UNKNOWN_TABLE)":                                        "query_log_missing",
		"Code: 497. DB::Exception: alice: Not enough privileges. To execute this query, it's necessary to have the grant SELECT": "not_granted",
	} {
		tctx, _ := diagCtx(t, diagReply{match: "system.query_log", err: code})
		out, err := analyzeQueryLog.Handler(tctx, json.RawMessage(`{"view":"slow"}`))
		if err != nil {
			t.Fatalf("%s: want a soft result, got %v", reason, err)
		}
		m := out.(map[string]any)
		if m["available"] != false || m["reason"] != reason {
			t.Errorf("%s: got %v", reason, m)
		}
		if reason == "not_granted" && m["hint"] != "GRANT SELECT ON system.query_log TO alice" {
			t.Errorf("hint %v", m["hint"])
		}
		out, err = queryPatternTool.Handler(tctx, json.RawMessage(`{"hash":"7"}`))
		if err != nil || out.(map[string]any)["reason"] != reason {
			t.Errorf("query_pattern %s: %v %v", reason, out, err)
		}
	}

	// Another error stays an error.
	tctx, _ := diagCtx(t, diagReply{match: "system.query_log", err: "Code: 241. DB::Exception: Memory limit exceeded"})
	if _, err := analyzeQueryLog.Handler(tctx, json.RawMessage(`{"view":"slow"}`)); err == nil {
		t.Error("a memory error is not an availability problem")
	}
}

func TestBrainAnalyzeQueryLogResultAndClusterNote(t *testing.T) {
	long := strings.Repeat("x", 900)
	tctx, f := diagCtx(t,
		diagReply{match: "system.clusters", data: `[{"cluster":"prod","nodes":"3"}]`},
		diagReply{match: "system.query_log", data: `[{"hash":"1","sample_query":"` + long + `","runs":"3"}]`},
	)
	out, err := analyzeQueryLog.Handler(tctx, json.RawMessage(`{"view":"heavy_readers","window":"1h"}`))
	if err != nil {
		t.Fatal(err)
	}
	m := out.(map[string]any)
	rows := m["rows"].([]map[string]any)
	if s := rows[0]["sample_query"].(string); len([]rune(s)) != diagSampleCap+3 {
		t.Errorf("sample clipped to %d runes", len([]rune(s)))
	}
	if !strings.Contains(m["scope"].(string), "local-node only") {
		t.Errorf("cluster note missing: %v", m)
	}
	for _, sql := range f.statements() {
		assertDiagShape(t, "statement", sql, strings.Contains(sql, "query_log"))
	}

	tctx, _ = diagCtx(t)
	out, _ = analyzeQueryLog.Handler(tctx, json.RawMessage(`{"view":"users"}`))
	if m := out.(map[string]any); m["note"] == nil || m["scope"] != nil {
		t.Errorf("empty single-node result: %v", m)
	}
}

func TestBrainStripTrailingFormatAndErrorCode(t *testing.T) {
	for in, want := range map[string]string{
		"SELECT 1 FORMAT CSV;":            "SELECT 1",
		"SELECT 'FORMAT JSON' AS f":       "SELECT 'FORMAT JSON' AS f",
		"SELECT 1;":                       "SELECT 1",
		"SELECT 1 -- note\nFORMAT Pretty": "SELECT 1 -- note",
	} {
		if got := StripTrailingFormat(in); got != want {
			t.Errorf("StripTrailingFormat(%q) = %q, want %q", in, got, want)
		}
	}
	if ErrorCode(errors.New("ClickHouse error: Code: 62. DB::Exception: Syntax error")) != 62 || ErrorCode(errors.New("tunnel not connected")) != 0 || ErrorCode(nil) != 0 {
		t.Error("ErrorCode")
	}
	if !IsSelectStatement("-- hi\nWITH a AS (SELECT 1) SELECT * FROM a") || IsSelectStatement("INSERT INTO t SELECT 1") {
		t.Error("IsSelectStatement")
	}
}

func TestBrainDiagnosticsRegisteredAsReads(t *testing.T) {
	r := New()
	RegisterDiagnostics(r)
	names := map[string]bool{}
	for _, d := range r.DefinitionsFor(false) {
		names[d.Function.Name] = true
	}
	for _, n := range []string{"analyze_query_log", "query_pattern", "explain_query", "table_storage", "merges_and_mutations", "compare_query_estimates"} {
		if !names[n] {
			t.Errorf("%s must be offered to viewers", n)
		}
		if tool, _ := r.Get(n); tool.RequiresApproval {
			t.Errorf("%s must not need approval", n)
		}
	}
}
