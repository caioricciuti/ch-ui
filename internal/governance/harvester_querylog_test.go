// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package governance

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caioricciuti/ch-ui/connector"
	"github.com/caioricciuti/ch-ui/internal/querylogactor"
	"github.com/caioricciuti/ch-ui/internal/testutil"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

func TestBuildQueryLogHarvestSQL(t *testing.T) {
	wm := parseQueryLogWatermark("2026-10-12 10:00:00.000002|q'2")
	sql := buildQueryLogHarvestSQL(wm, []string{"svc'x", "admin"}, 0, true, []querylogactor.Account{{User: "sso:a@x", Person: "a@x"}})
	for _, must := range []string{
		"user AS ch_user,",
		"JSONExtractString(log_comment, 'person')",
		"transform(user, ['sso:a@x'], ['a@x'], user)) AS actor,",
		"type IN ('QueryFinish', 'ExceptionBeforeStart', 'ExceptionWhileProcessing')",
		"(event_time_microseconds, query_id) > (parseDateTime64BestEffort('2026-10-12 10:00:00.000002', 6), 'q\\'2')",
		"NOT (log_comment = 'ch-ui:governance' AND user IN ('svc\\'x', 'admin'))",
		"ORDER BY event_time_microseconds ASC, query_id ASC",
		"event_date >= toDate(",
	} {
		if !strings.Contains(sql, must) {
			t.Fatalf("missing %q in:\n%s", must, sql)
		}
	}
	for _, mustNot := range []string{"NOT LIKE", "query_duration_ms >="} {
		if strings.Contains(sql, mustNot) {
			t.Fatalf("unexpected %q in:\n%s", mustNot, sql)
		}
	}
	if s := buildQueryLogHarvestSQL(wm, []string{"u"}, 25, false, nil); !strings.Contains(s, "(type != 'QueryFinish' OR query_duration_ms >= 25)") || !strings.Contains(s, "CAST([], 'Array(String)') AS tables") {
		t.Fatalf("fallback/floor SQL wrong:\n%s", s)
	}
}

func TestParseQueryLogWatermark(t *testing.T) {
	cases := map[string]queryLogWatermark{
		"":                                {TS: defaultQueryLogWatermark},
		"2026-10-12 10:00:05":             {TS: "2026-10-12 10:00:05.000000"},
		"2026-10-12T10:00:05Z":            {TS: "2026-10-12 10:00:05.000000"},
		"2026-10-12 10:00:05.123456|abc":  {TS: "2026-10-12 10:00:05.123456", QueryID: "abc"},
		"garbage'; DROP TABLE x|id|extra": {TS: defaultQueryLogWatermark, QueryID: "id|extra"},
	}
	for in, want := range cases {
		if got := parseQueryLogWatermark(in); got != want {
			t.Errorf("parse(%q) = %+v, want %+v", in, got, want)
		}
	}
}

type harvestFixture struct {
	mu   sync.Mutex
	rows string
	fail bool
	sqls []tunnel.GatewayMessage
}

func (f *harvestFixture) respond(msg tunnel.GatewayMessage) *tunnel.AgentMessage {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sqls = append(f.sqls, msg)
	if f.fail {
		return &tunnel.AgentMessage{Type: "query_error", Error: "Code: 241. Memory limit exceeded"}
	}
	return &tunnel.AgentMessage{Type: "query_result", Data: json.RawMessage(f.rows)}
}

func newHarvestTest(t *testing.T) (*Syncer, *Store, *harvestFixture, string) {
	t.Helper()
	t.Setenv("CHUI_TEST_CLICKHOUSE_URL", "") // deterministic fixture
	db, conn := testutil.WorkerDB(t)
	f := &harvestFixture{rows: "[]"}
	agent := testutil.NewAgent(t, db, "worker-token", f.respond)
	store := NewStore(db)
	if _, err := store.CreatePolicy(conn, "pii", "", "table", "db", "tbl", "", "analyst", "warn", "warn", "admin"); err != nil {
		t.Fatal(err)
	}
	return NewSyncer(store, db, agent.Gateway, "test-secret", func() bool { return true }), store, f, conn
}

func storedWatermark(t *testing.T, store *Store, conn string) string {
	t.Helper()
	st, err := store.GetSyncState(conn, string(SyncQueryLog))
	if err != nil || st == nil || st.Watermark == nil {
		t.Fatalf("sync state: %v %+v", err, st)
	}
	return *st.Watermark
}

// B2: a failed harvest must keep the stored watermark, not jump to now.
func TestQueryLogHarvestFailureKeepsWatermark(t *testing.T) {
	s, store, f, conn := newHarvestTest(t)
	const old = "2026-01-01 00:00:00.000000|abc"
	if err := store.UpsertSyncState(conn, string(SyncQueryLog), "idle", strPtrTest(old), nil, 0); err != nil {
		t.Fatal(err)
	}
	f.fail = true
	if _, err := s.syncQueryLog(context.Background(), CHCredentials{ConnectionID: conn, User: "svc"}); err == nil {
		t.Fatal("expected harvest error")
	}
	if got := storedWatermark(t, store, conn); got != old {
		t.Fatalf("watermark moved on failure: %q", got)
	}
}

// B3: the watermark carries microseconds plus query_id, and a re-read row is
// neither stored twice nor evaluated twice.
func TestQueryLogHarvestWatermarkAndNoDuplicateViolations(t *testing.T) {
	s, store, f, conn := newHarvestTest(t)
	f.rows = `[
	 {"query_id":"q1","ch_user":"alice","actor":"alice@example.com","query":"SELECT 1 FROM db.tbl","event_time":"2026-10-12 10:00:00","event_time_us":"2026-10-12 10:00:00.000001","tables":["db.tbl"],"exception_code":0},
	 {"query_id":"q2","ch_user":"alice","actor":"alice","query":"SELECT 2 FROM db.tbl","event_time":"2026-10-12 10:00:00","event_time_us":"2026-10-12 10:00:00.000002","tables":["db.tbl"],"exception_code":0}
	]`
	creds := CHCredentials{ConnectionID: conn, User: "svc"}
	if _, err := s.syncQueryLog(context.Background(), creds); err != nil {
		t.Fatal(err)
	}
	if got := storedWatermark(t, store, conn); got != "2026-10-12 10:00:00.000002|q2" {
		t.Fatalf("watermark = %q", got)
	}
	// Same rows again (an overlapping read): nothing new may be recorded.
	res, err := s.syncQueryLog(context.Background(), creds)
	if err != nil {
		t.Fatal(err)
	}
	if res.QueriesIngested != 0 || res.ViolationsFound != 0 {
		t.Fatalf("re-read ingested %d, violations %d", res.QueriesIngested, res.ViolationsFound)
	}
	violations, err := store.GetViolations(conn, 100, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 2 {
		t.Fatalf("violations = %d, want 2", len(violations))
	}
	for _, v := range violations {
		if v.User != "alice" {
			t.Fatalf("violation must name the ClickHouse user, got %q", v.User)
		}
	}
	// The stored row keeps both names, and the user filter matches either.
	stored, _, err := store.GetQueryLog(conn, 10, 0, "alice@example.com", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != 1 || stored[0].User != "alice" || stored[0].Actor != "alice@example.com" {
		t.Fatalf("actor filter: %+v", stored)
	}
	if stored, _, err = store.GetQueryLog(conn, 10, 0, "alice", ""); err != nil || len(stored) != 2 {
		t.Fatalf("ch_user filter: %d rows, %v", len(stored), err)
	}
	f.mu.Lock()
	last := f.sqls[len(f.sqls)-1]
	f.mu.Unlock()
	sql := last.SQL + last.Query
	if !strings.Contains(sql, "'2026-10-12 10:00:00.000002', 6), 'q2')") {
		t.Fatalf("second harvest did not resume after q2:\n%s", sql)
	}
	if last.Settings["log_comment"] != GovernanceLogComment {
		t.Fatalf("governance query not tagged: %v", last.Settings)
	}
}

func TestQueryLogHarvestSQLLive(t *testing.T) {
	endpoint := os.Getenv("CHUI_TEST_CLICKHOUSE_URL")
	if endpoint == "" {
		t.Skip("set CHUI_TEST_CLICKHOUSE_URL to run against ClickHouse")
	}
	client := connector.NewCHClient(endpoint, false)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	user := os.Getenv("CHUI_TEST_CLICKHOUSE_USER")
	pass := os.Getenv("CHUI_TEST_CLICKHOUSE_PASSWORD")
	if _, err := client.Execute(ctx, "SYSTEM FLUSH LOGS", user, pass, nil); err != nil {
		t.Fatal(err)
	}
	wm := parseQueryLogWatermark(time.Now().Add(-time.Hour).UTC().Format("2006-01-02 15:04:05") + "|x'y")
	for _, withTables := range []bool{true, false} {
		if _, err := client.Execute(ctx, buildQueryLogHarvestSQL(wm, []string{"default"}, 10, withTables, []querylogactor.Account{{User: "sso:a@x", Person: "a@x"}}), user, pass, nil); err != nil {
			t.Fatalf("harvest SQL (tables=%v) failed on ClickHouse: %v", withTables, err)
		}
	}
}

func strPtrTest(s string) *string { return &s }

// B5: unqualified names resolve against the current database, and SELECT *
// touches column policies.
func TestQueryTouchesObjectResolvesStarAndDatabase(t *testing.T) {
	col := Policy{ObjectType: "column", ObjectDatabase: strPtrTest("db"), ObjectTable: strPtrTest("tbl"), ObjectColumn: strPtrTest("ssn")}
	tables := []string{"db.tbl"}
	cases := []struct {
		q    string
		cols []string
		want bool
	}{
		{"SELECT * FROM db.tbl", nil, true},
		{"SELECT DISTINCT * FROM db.tbl", nil, true},
		{"SELECT t.* FROM db.tbl t", nil, true},
		{"SELECT * EXCEPT (ssn) FROM db.tbl", nil, false},
		{"SELECT * EXCEPT ssn FROM db.tbl", nil, false},
		{"SELECT count(*) FROM db.tbl", nil, false},
		{"SELECT * FROM db.tbl", []string{"id", "name"}, false},
		{"SELECT * FROM db.tbl", []string{"id", "SSN"}, true},
	}
	for _, c := range cases {
		var lookup func(string, string) []string
		if c.cols != nil {
			cols := c.cols
			lookup = func(string, string) []string { return cols }
		}
		if got := queryTouchesObject(tables, c.q, col, lookup); got != c.want {
			t.Errorf("%q cols=%v: got %v want %v", c.q, c.cols, got, c.want)
		}
	}

	if got := qualifyTables([]string{"tbl", "x.y"}, "db"); got[0] != "db.tbl" || got[1] != "x.y" {
		t.Fatalf("qualify = %v", got)
	}
	if got := qualifyTables([]string{"tbl"}, ""); got[0] != "default.tbl" {
		t.Fatalf("qualify default = %v", got)
	}
}

func TestGuardrailBlocksUnqualifiedTableInCurrentDatabase(t *testing.T) {
	ctx := newGuardrailTestContext(t)
	ctx.setAccessSyncFresh(t)
	ctx.createPolicy(t, "block-policy", "critical", "block")

	decision, err := ctx.service.EvaluateQueryInDatabase(ctx.connID, "alice", "db", "SELECT * FROM tbl", "/api/query/run")
	if err != nil {
		t.Fatal(err)
	}
	if decision.Allowed {
		t.Fatal("unqualified table in the current database was not blocked")
	}
	decision, err = ctx.service.EvaluateQueryInDatabase(ctx.connID, "alice", "other", "SELECT * FROM tbl", "/api/query/run")
	if err != nil {
		t.Fatal(err)
	}
	if !decision.Allowed {
		t.Fatal("table in another database was blocked")
	}
}
