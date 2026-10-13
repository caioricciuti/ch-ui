// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/caioricciuti/ch-ui/internal/testutil"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

// explainFake answers EXPLAIN statements with scripted errors, one per
// check ("" passes), and every other statement with one row.
type explainFake struct {
	mu       sync.Mutex
	errs     []string
	explains []tunnel.GatewayMessage
}

func (f *explainFake) respond(msg tunnel.GatewayMessage) *tunnel.AgentMessage {
	if !strings.HasPrefix(msg.SQL, "EXPLAIN") {
		return &tunnel.AgentMessage{Type: "query_result", Data: json.RawMessage(`[{"x":1}]`), Meta: json.RawMessage(`[{"name":"x","type":"UInt8"}]`)}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.explains = append(f.explains, msg)
	errText := ""
	if len(f.errs) > 0 {
		errText, f.errs = f.errs[0], f.errs[1:]
	}
	if errText != "" {
		return &tunnel.AgentMessage{Type: "query_error", Error: errText}
	}
	return &tunnel.AgentMessage{Type: "query_result", Data: json.RawMessage(`[{"explain":"Expression"}]`), Meta: json.RawMessage(`[]`)}
}

func (f *explainFake) sent() []tunnel.GatewayMessage {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]tunnel.GatewayMessage(nil), f.explains...)
}

// postSSE posts body and returns every SSE data event.
func postSSE(t *testing.T, url, body string) []map[string]any {
	t.Helper()
	resp, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		var out map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&out)
		t.Fatalf("status %d %v", resp.StatusCode, out)
	}
	raw := new(strings.Builder)
	buf := make([]byte, 4096)
	for {
		n, rerr := resp.Body.Read(buf)
		raw.Write(buf[:n])
		if rerr != nil {
			break
		}
	}
	var events []map[string]any
	for _, line := range strings.Split(raw.String(), "\n") {
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var ev map[string]any
		if json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &ev) == nil {
			events = append(events, ev)
		}
	}
	return events
}

func eventTypes(events []map[string]any) []string {
	out := make([]string, 0, len(events))
	for _, ev := range events {
		out = append(out, ev["type"].(string))
	}
	return out
}

func lastEvent(t *testing.T, events []map[string]any) map[string]any {
	t.Helper()
	if len(events) == 0 || events[len(events)-1]["type"] != "done" {
		t.Fatalf("no done event: %v", events)
	}
	return events[len(events)-1]
}

func ledgerPurposes(t *testing.T, rg *brainRig) map[string]int {
	t.Helper()
	rows, err := rg.db.Conn().Query(`SELECT purpose, count(*) FROM brain_usage GROUP BY purpose`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var p string
		var n int
		if err := rows.Scan(&p, &n); err != nil {
			t.Fatal(err)
		}
		out[p] = n
	}
	return out
}

const unknownColumn = "Code: 47. DB::Exception: Missing columns: 'nme' while processing query. (UNKNOWN_IDENTIFIER)"

// A query ClickHouse keeps rejecting is repaired at most twice; every
// repair call lands in the ledger as ask_ai_repair and carries the error.
func TestBrainAskAIRepairStopsAfterTwoRounds(t *testing.T) {
	ch := &explainFake{errs: []string{unknownColumn, unknownColumn, unknownColumn, unknownColumn}}
	rg, fc := newClaudeRigWith(t, ch.respond,
		claudeText("SELECT nme FROM db.users", "end_turn", 40, 8),
		claudeText("SELECT nam FROM db.users", "end_turn", 50, 8),
		claudeText("SELECT nmae FROM db.users", "end_turn", 60, 8),
		claudeText("SELECT never FROM db.users", "end_turn", 70, 8),
	)
	events := postSSE(t, rg.app.URL+"/generate-sql/stream", `{"question":"user names"}`)
	done := lastEvent(t, events)
	v := done["validation"].(map[string]any)
	if v["status"] != "failed" || v["rounds"] != float64(2) || !strings.Contains(v["error"].(string), "Code: 47") {
		t.Fatalf("validation %v", v)
	}
	if done["sql"] != "SELECT nmae FROM db.users" {
		t.Errorf("sql %v", done["sql"])
	}
	if fc.request(3) != nil {
		t.Error("a third repair was requested")
	}
	if n := len(ch.sent()); n != 3 {
		t.Errorf("want 3 EXPLAIN checks, got %d", n)
	}
	types := strings.Join(eventTypes(events), ",")
	if !strings.HasPrefix(types, "delta,validating,repair,reset,delta,validating,repair,reset,delta,validating,done") {
		t.Errorf("events %s", types)
	}
	if ev := events[2]; ev["round"] != float64(1) || !strings.Contains(ev["error"].(string), "nme") {
		t.Errorf("repair event %v", ev)
	}

	second, _ := json.Marshal(fc.request(1)["messages"])
	if !strings.Contains(string(second), "SELECT nme FROM db.users") || !strings.Contains(string(second), "ClickHouse rejected it: Code: 47") ||
		!strings.Contains(string(second), "Return the full corrected query only.") {
		t.Errorf("repair request %s", second)
	}
	if p := ledgerPurposes(t, rg); p["ask_ai"] != 1 || p["ask_ai_repair"] != 2 {
		t.Errorf("ledger %v", p)
	}
	usage := done["usage"].(map[string]any)
	if usage["input_tokens"] != float64(150) {
		t.Errorf("usage should sum all calls: %v", usage)
	}
	for _, m := range ch.sent() {
		if m.Settings["readonly"] != "1" || m.Settings["max_execution_time"] != "10" || !strings.HasPrefix(m.SQL, "EXPLAIN PLAN SELECT") {
			t.Errorf("check %q settings %v", m.SQL, m.Settings)
		}
	}
}

// One repair that ClickHouse accepts ends the loop as passed.
func TestBrainAskAIRepairPasses(t *testing.T) {
	ch := &explainFake{errs: []string{"Code: 62. DB::Exception: Syntax error: failed at position 8 ('FRM'). (SYNTAX_ERROR)", ""}}
	rg, _ := newClaudeRigWith(t, ch.respond,
		claudeText("SELECT * FRM db.t", "end_turn", 40, 8),
		claudeText("```sql\nSELECT * FROM db.t\n```", "end_turn", 40, 8),
	)
	code, out := doJSON(t, rg.app.Config.Handler, http.MethodPost, "/generate-sql", `{"question":"all rows"}`)
	if code != http.StatusOK {
		t.Fatalf("status %d %v", code, out)
	}
	v := out["validation"].(map[string]any)
	if v["status"] != "passed" || v["rounds"] != float64(1) || out["sql"] != "SELECT * FROM db.t" {
		t.Fatalf("result %v", out)
	}
}

// Errors no SQL edit fixes are not repaired: the check is skipped.
func TestBrainAskAITerminalCodesSkipRepair(t *testing.T) {
	for _, errText := range []string{
		"Code: 497. DB::Exception: alice: Not enough privileges. (ACCESS_DENIED)",
		"Code: 164. DB::Exception: Cannot modify 'max_threads' setting in readonly mode. (READONLY)",
		"Code: 456. DB::Exception: Substitution `d` is not set. (UNKNOWN_QUERY_PARAMETER)",
	} {
		ch := &explainFake{errs: []string{errText}}
		rg, fc := newClaudeRigWith(t, ch.respond, claudeText("SELECT 1", "end_turn", 40, 8))
		events := postSSE(t, rg.app.URL+"/generate-sql/stream", `{"question":"one"}`)
		v := lastEvent(t, events)["validation"].(map[string]any)
		if v["status"] != "skipped" || v["reason"] != "not_checkable" || v["rounds"] != float64(0) {
			t.Errorf("%s: validation %v", errText[:9], v)
		}
		if fc.request(1) != nil {
			t.Errorf("%s: no repair for a terminal error", errText[:9])
		}
		if p := ledgerPurposes(t, rg); p["ask_ai_repair"] != 0 {
			t.Errorf("ledger %v", p)
		}
	}
}

// The daily budget is checked before each repair.
func TestBrainAskAIRepairStopsAtBudget(t *testing.T) {
	ch := &explainFake{errs: []string{unknownColumn}}
	rg, fc := newClaudeRigWith(t, ch.respond, claudeText("SELECT nme FROM db.users", "end_turn", 40, 8))
	if err := rg.db.SetSetting(settingBrainDailyBudget, "45"); err != nil {
		t.Fatal(err)
	}
	events := postSSE(t, rg.app.URL+"/generate-sql/stream", `{"question":"names"}`)
	v := lastEvent(t, events)["validation"].(map[string]any)
	if v["status"] != "failed" || v["reason"] != "budget" || v["rounds"] != float64(0) {
		t.Fatalf("validation %v", v)
	}
	if fc.request(1) != nil {
		t.Error("no repair once the budget is used up")
	}
}

// Fix with AI strips the reason line, checks with the query's parameter
// values and records fix_sql and fix_sql_repair.
func TestBrainFixSQLExplanationParamsAndLedger(t *testing.T) {
	ch := &explainFake{errs: []string{unknownColumn, ""}}
	rg, fc := newClaudeRigWith(t, ch.respond,
		claudeText("-- fix: nme is not a column; the table has name\nSELECT nam FROM db.users WHERE id = {id:UInt32}", "end_turn", 40, 8),
		claudeText("SELECT name FROM db.users WHERE id = {id:UInt32}", "end_turn", 40, 8),
	)
	body := `{"sql":"SELECT nme FROM db.users WHERE id = {id:UInt32}","error":"Code: 47. Missing columns: 'nme'","params":{"id":"7"}}`
	events := postSSE(t, rg.app.URL+"/fix-sql/stream", body)
	done := lastEvent(t, events)
	if done["explanation"] != "nme is not a column; the table has name" || done["sql"] != "SELECT name FROM db.users WHERE id = {id:UInt32}" {
		t.Fatalf("done %v", done)
	}
	if v := done["validation"].(map[string]any); v["status"] != "passed" || v["rounds"] != float64(1) {
		t.Errorf("validation %v", v)
	}
	for _, m := range ch.sent() {
		if m.Settings["param_id"] != "7" {
			t.Errorf("the check must bind the parameter values: %v", m.Settings)
		}
	}
	first, _ := json.Marshal(fc.request(0))
	if !strings.Contains(string(first), "-- fix:") || !strings.Contains(string(first), "Missing columns") || !strings.Contains(string(first), "id = 7") {
		t.Errorf("fix prompt %s", first)
	}
	if p := ledgerPurposes(t, rg); p["fix_sql"] != 1 || p["fix_sql_repair"] != 1 {
		t.Errorf("ledger %v", p)
	}

	code, out := doJSON(t, rg.app.Config.Handler, http.MethodPost, "/fix-sql", `{"sql":"SELECT 1","error":"x","params":{"bad name":"1"}}`)
	if code != http.StatusBadRequest {
		t.Errorf("bad parameter name: %d %v", code, out)
	}
	code, _ = doJSON(t, rg.app.Config.Handler, http.MethodPost, "/fix-sql", `{"sql":"SELECT 1"}`)
	if code != http.StatusBadRequest {
		t.Errorf("the error is required, got %d", code)
	}
}

// validateGeneratedSQL skips the check when the tunnel is down or a policy
// blocks the query, and never sends that query to ClickHouse.
func TestBrainValidateGeneratedSQLSkips(t *testing.T) {
	db, conn := testutil.WorkerDB(t)
	ch := &explainFake{}
	agent := testutil.NewAgent(t, db, "worker-token", ch.respond)
	h := &BrainHandler{DB: db, Gateway: agent.Gateway}
	ctx := context.Background()

	job := &sqlGenJob{connID: conn, chUser: "alice", guardrail: func(string) error { return errors.New("query blocked by governance policy \"pii\"") }}
	if v := h.validateGeneratedSQL(ctx, job, "SELECT * FROM hr.salaries"); v.status != validationSkipped || v.reason != "guardrail" {
		t.Errorf("guardrail: %+v", v)
	}
	job = &sqlGenJob{connID: "no-such-connection"}
	if v := h.validateGeneratedSQL(ctx, job, "SELECT 1"); v.status != validationSkipped || v.reason != "tunnel_offline" {
		t.Errorf("offline: %+v", v)
	}
	job = &sqlGenJob{connID: conn}
	if v := h.validateGeneratedSQL(ctx, job, "-- only a comment"); v.status != validationSkipped || v.reason != "no_sql" {
		t.Errorf("comment only: %+v", v)
	}
	if n := len(ch.sent()); n != 0 {
		t.Errorf("%d checks reached ClickHouse", n)
	}

	if v := h.validateGeneratedSQL(ctx, job, "CREATE TABLE t (a UInt8) ENGINE = Memory FORMAT JSON;"); v.status != validationPassed {
		t.Errorf("ddl: %+v", v)
	}
	if got := ch.sent(); len(got) != 1 || got[0].SQL != "EXPLAIN AST CREATE TABLE t (a UInt8) ENGINE = Memory" {
		t.Errorf("non-SELECT statements are checked with EXPLAIN AST: %v", got)
	}
}

func TestBrainSplitFixReason(t *testing.T) {
	for in, want := range map[string][2]string{
		"-- fix: typo in FROM\nSELECT 1":    {"typo in FROM", "SELECT 1"},
		"\n  -- FIX: x\n\nSELECT 1\n-- end": {"x", "SELECT 1\n-- end"},
		"-- other comment\nSELECT 1":        {"", "-- other comment\nSELECT 1"},
		"SELECT 1":                          {"", "SELECT 1"},
	} {
		r, s := splitFixReason(in)
		if r != want[0] || s != want[1] {
			t.Errorf("splitFixReason(%q) = %q, %q", in, r, s)
		}
	}
}
