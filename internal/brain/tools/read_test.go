// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package tools

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/caioricciuti/ch-ui/internal/testutil"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

type recorder struct {
	mu   sync.Mutex
	msgs []tunnel.GatewayMessage
}

func (r *recorder) respond(msg tunnel.GatewayMessage) *tunnel.AgentMessage {
	r.mu.Lock()
	r.msgs = append(r.msgs, msg)
	r.mu.Unlock()
	return &tunnel.AgentMessage{Type: "query_result", Data: json.RawMessage(`[{"x":1}]`), Meta: json.RawMessage(`[{"name":"x","type":"UInt8"}]`)}
}

func (r *recorder) last(t *testing.T) tunnel.GatewayMessage {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.msgs) == 0 {
		t.Fatal("nothing reached ClickHouse")
	}
	return r.msgs[len(r.msgs)-1]
}

func readCtx(t *testing.T) (Context, *recorder) {
	t.Helper()
	db, conn := testutil.WorkerDB(t)
	rec := &recorder{}
	agent := testutil.NewAgent(t, db, "worker-token", rec.respond)
	return Context{Ctx: context.Background(), ConnectionID: conn, CHUser: "u", Gateway: agent.Gateway}, rec
}

func TestRunQueryToolIsReadonlyWithCaps(t *testing.T) {
	tctx, rec := readCtx(t)
	if _, err := runQuery.Handler(tctx, json.RawMessage(`{"sql":"SELECT 1","limit":7}`)); err != nil {
		t.Fatal(err)
	}
	s := rec.last(t).Settings
	if s["readonly"] != "1" {
		t.Errorf("readonly = %q, want 1", s["readonly"])
	}
	if s["max_result_rows"] != "7" || s["result_overflow_mode"] != "break" || s["max_execution_time"] == "" || s["max_result_bytes"] == "" {
		t.Errorf("limits must travel as query settings, got %v", s)
	}
}

func TestReadToolsMetadataQueriesAreReadonly(t *testing.T) {
	tctx, rec := readCtx(t)
	if _, err := listTables.Handler(tctx, json.RawMessage(`{"database":"a"}`)); err != nil {
		t.Fatal(err)
	}
	if s := rec.last(t).Settings; s["readonly"] != "1" || s["max_execution_time"] == "" {
		t.Errorf("list_tables settings = %v", s)
	}
}

func TestRunQueryToolRejects(t *testing.T) {
	tctx, rec := readCtx(t)
	for sql, want := range map[string]string{
		"DROP TABLE t":                          "read-only",
		"SELECT 1 SETTINGS max_result_rows = 0": "SETTINGS",
	} {
		args, _ := json.Marshal(map[string]string{"sql": sql})
		if _, err := runQuery.Handler(tctx, args); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: want error mentioning %s, got %v", sql, want, err)
		}
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.msgs) != 0 {
		t.Errorf("rejected SQL reached ClickHouse: %v", rec.msgs[0].SQL)
	}
}

func TestSQLEscapes(t *testing.T) {
	for in, want := range map[string]string{
		"plain": "plain",
		"it's":  "it''s",
		`a\`:    `a\\`,
		`a\'b`:  `a\\''b`,
	} {
		if got := sqlEscape(in); got != want {
			t.Errorf("sqlEscape(%q) = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[string]string{
		"plain": "plain",
		"a`b":   "a``b",
		`a\`:    `a\\`,
		"a\\`b": "a\\\\``b",
	} {
		if got := sqlEscapeBacktick(in); got != want {
			t.Errorf("sqlEscapeBacktick(%q) = %q, want %q", in, got, want)
		}
	}
}
