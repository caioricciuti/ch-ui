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
	"os"
	"strings"
	"testing"
	"time"

	"github.com/caioricciuti/ch-ui/connector"
	"github.com/caioricciuti/ch-ui/internal/governance"
	"github.com/caioricciuti/ch-ui/internal/safe"
)

// B6: harvested SQL, violations and incidents are admin-only; the overview
// keeps its counts for everyone but drops the recent violations.
func TestGovernanceQueryTextReadsAreAdminOnly(t *testing.T) {
	_, store, connA, _, router := newGovernanceRoleRouter(t)
	if _, err := store.InsertQueryLogBatch([]governance.QueryLogEntry{{ConnectionID: connA, QueryID: "q1", User: "alice", QueryText: "SELECT secret FROM db.t", EventTime: "2026-10-12 10:00:00", TablesUsed: `["db.t"]`}}); err != nil {
		t.Fatal(err)
	}
	policyID, err := store.CreatePolicy(connA, "p", "", "table", "db", "t", "", "analyst", "warn", "warn", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateViolation(connA, policyID, "", "alice", "detail", "warn", "post_exec", ""); err != nil {
		t.Fatal(err)
	}

	reads := []string{"/query-log", "/query-log/q1", "/violations", "/incidents", "/incidents/x", "/incidents/x/comments"}
	for _, path := range reads {
		if rec := serveAs(router, "viewer", connA, http.MethodGet, path, ""); rec.Code != http.StatusForbidden {
			t.Errorf("viewer GET %s: status %d, want 403", path, rec.Code)
		}
		if rec := serveAs(router, "admin", connA, http.MethodGet, path, ""); rec.Code == http.StatusForbidden {
			t.Errorf("admin GET %s: got 403", path)
		}
	}

	var body struct {
		Overview governance.GovernanceOverview `json:"overview"`
	}
	rec := serveAs(router, "viewer", connA, http.MethodGet, "/overview", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("viewer overview: %d", rec.Code)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Overview.ViolationCount != 1 || len(body.Overview.RecentViolations) != 0 {
		t.Fatalf("viewer overview: count %d, recent %d", body.Overview.ViolationCount, len(body.Overview.RecentViolations))
	}
	rec = serveAs(router, "admin", connA, http.MethodGet, "/overview", "")
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Overview.RecentViolations) != 1 {
		t.Fatalf("admin overview: recent %d, want 1", len(body.Overview.RecentViolations))
	}
}

func TestQueryHarvestMinDurationSetting(t *testing.T) {
	db, _, connA, _, router := newGovernanceRoleRouter(t)
	if rec := serveAs(router, "viewer", connA, http.MethodPut, "/query-harvest", `{"min_duration_ms":5}`); rec.Code != http.StatusForbidden {
		t.Fatalf("viewer PUT: %d", rec.Code)
	}
	if rec := serveAs(router, "admin", connA, http.MethodPut, "/query-harvest", `{"min_duration_ms":-1}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("negative floor: %d", rec.Code)
	}
	rec := serveAs(router, "admin", connA, http.MethodPut, "/query-harvest", `{"mode":"always","min_duration_ms":25}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"min_duration_ms":25`) {
		t.Fatalf("admin PUT: %d %s", rec.Code, rec.Body.String())
	}
	if got := governance.QueryHarvestMinDurationMs(db); got != 25 {
		t.Fatalf("stored floor = %d", got)
	}
}

// B8: CREATE USER is never IF NOT EXISTS, and a taken name is recognised.
func TestCreateChUserSQLIsPlain(t *testing.T) {
	sql := buildCreateChUserSQL("bob", chUserAuth{Method: "no_password"})
	if strings.Contains(sql, "IF NOT EXISTS") || !strings.HasPrefix(sql, "CREATE USER `bob`") {
		t.Fatalf("sql = %q", sql)
	}
	if !isAccessEntityExistsError(errors.New("Code: 493. DB::Exception: user `bob`: cannot insert because user `bob` already exists in `local_directory`. (ACCESS_ENTITY_ALREADY_EXISTS)")) {
		t.Fatal("493 not recognised")
	}
	if isAccessEntityExistsError(errors.New("Code: 497. ACCESS_DENIED")) {
		t.Fatal("unrelated error recognised as exists")
	}
}

func TestCreateChUserExistsLive(t *testing.T) {
	endpoint := os.Getenv("CHUI_TEST_CLICKHOUSE_URL")
	if endpoint == "" {
		t.Skip("set CHUI_TEST_CLICKHOUSE_URL to a disposable test instance")
	}
	client := connector.NewCHClient(endpoint, false)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	user, pass := os.Getenv("CHUI_TEST_CLICKHOUSE_USER"), os.Getenv("CHUI_TEST_CLICKHOUSE_PASSWORD")
	name := "chui_test_b8_" + time.Now().Format("150405")
	t.Cleanup(func() {
		_, _ = client.Execute(context.Background(), "DROP USER IF EXISTS "+safe.Identifier(name), user, pass, nil)
	})
	sql := buildCreateChUserSQL(name, chUserAuth{Method: "no_password"})
	if _, err := client.Execute(ctx, sql, user, pass, nil); err != nil {
		t.Fatalf("first create: %v", err)
	}
	_, err := client.Execute(ctx, sql, user, pass, nil)
	if err == nil || !isAccessEntityExistsError(err) {
		t.Fatalf("second create: %v, want ACCESS_ENTITY_ALREADY_EXISTS", err)
	}
}
