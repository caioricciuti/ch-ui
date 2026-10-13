// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/caioricciuti/ch-ui/connector"
	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/rbac"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
	"github.com/caioricciuti/ch-ui/internal/testutil"
)

// The access control editor against a real ClickHouse: roles, grants, a row
// policy that filters what a user sees, quota and profile round trips, a
// partial failure and, when a cluster is configured, ON CLUSTER.
func TestRBACLive(t *testing.T) {
	endpoint := os.Getenv("CHUI_TEST_CLICKHOUSE_URL")
	if endpoint == "" {
		t.Skip("set CHUI_TEST_CLICKHOUSE_URL to a disposable test instance")
	}
	client := connector.NewCHClient(endpoint, false)
	admin, adminPassword := os.Getenv("CHUI_TEST_CLICKHOUSE_USER"), os.Getenv("CHUI_TEST_CLICKHOUSE_PASSWORD")
	if admin == "" {
		admin = "default"
	}
	exec := func(user, password, sql string) (*connector.QueryResult, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		return client.Execute(ctx, sql, user, password, nil)
	}
	must := func(sql string) {
		t.Helper()
		if _, err := exec(admin, adminPassword, sql); err != nil {
			t.Fatal(err)
		}
	}

	suffix := time.Now().UnixNano()
	name := func(kind string) string { return fmt.Sprintf("chui_rbac_%s_%d", kind, suffix) }
	db, roleName, otherRole, userName := name("db"), name("role"), name("role2"), name("user")
	policy, quota, profile := name("policy"), name("quota"), name("profile")
	userPassword := "rbac-test-only"

	must("CREATE DATABASE " + db)
	must("CREATE TABLE " + db + ".events (id UInt64, tenant String) ENGINE = MergeTree ORDER BY id")
	must("INSERT INTO " + db + ".events VALUES (1, 'a'), (2, 'a'), (3, 'b')")
	must("CREATE USER " + userName + " IDENTIFIED WITH plaintext_password BY '" + userPassword + "'")
	t.Cleanup(func() {
		for _, sql := range []string{
			"DROP ROW POLICY IF EXISTS " + policy + " ON " + db + ".events",
			"DROP USER IF EXISTS " + userName,
			"DROP ROLE IF EXISTS " + roleName,
			"DROP ROLE IF EXISTS " + otherRole,
			"DROP QUOTA IF EXISTS " + quota,
			"DROP SETTINGS PROFILE IF EXISTS " + profile,
			"DROP DATABASE IF EXISTS " + db,
		} {
			if _, err := exec(admin, adminPassword, sql); err != nil {
				t.Error(err)
			}
		}
	})

	sqlite, conn := testutil.WorkerDB(t)
	agent := testutil.NewWorkerAgent(t, sqlite, nil)
	cfg := &config.Config{AppSecretKey: "rbac-live-secret"}
	cfg.SetProAccessForTest(func() config.ProAccess { return config.ProActive })
	enc, err := crypto.Encrypt(adminPassword, cfg.AppSecretKey)
	if err != nil {
		t.Fatal(err)
	}
	router := (&RBACHandler{DB: sqlite, Gateway: agent.Gateway, Config: cfg}).Routes()
	call := func(path string, body any) (int, []byte) {
		b, _ := json.Marshal(body)
		req := httptest.NewRequest("POST", path, strings.NewReader(string(b)))
		req = req.WithContext(middleware.SetSession(req.Context(), &middleware.SessionInfo{
			UserRole: "admin", ClickhouseUser: admin, ConnectionID: conn, EncryptedPassword: enc,
		}))
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec.Code, rec.Body.Bytes()
	}
	type applyResponse struct {
		Results     []rbac.Result `json:"results"`
		RollbackOps []rbac.Op     `json:"rollback_ops"`
	}
	// apply plans ops, acknowledges every ack warning and applies.
	apply := func(ops []rbac.Op, cluster string) applyResponse {
		t.Helper()
		code, body := call("/plan", map[string]any{"ops": ops, "cluster": cluster})
		if code != http.StatusOK {
			t.Fatalf("plan: %d %s", code, body)
		}
		var pr struct {
			Plan rbac.Plan `json:"plan"`
		}
		if err := json.Unmarshal(body, &pr); err != nil {
			t.Fatal(err)
		}
		if pr.Plan.Blocked() {
			t.Fatalf("plan blocked: %+v", pr.Plan.Warnings)
		}
		var acks []string
		for _, w := range pr.Plan.Warnings {
			acks = append(acks, w.Code)
		}
		code, body = call("/apply", map[string]any{"ops": ops, "cluster": cluster, "plan_hash": pr.Plan.PlanHash, "acknowledged": acks})
		if code != http.StatusOK {
			t.Fatalf("apply: %d %s", code, body)
		}
		var ar applyResponse
		if err := json.Unmarshal(body, &ar); err != nil {
			t.Fatal(err)
		}
		return ar
	}
	countAs := func() string {
		t.Helper()
		res, err := exec(userName, userPassword, "SELECT count() AS c FROM "+db+".events")
		if err != nil {
			t.Fatal(err)
		}
		return fmt.Sprint(res.Data[0]["c"])
	}
	snapshot := func() *rbac.Snapshot {
		t.Helper()
		ex := sessionExecutor{gw: agent.Gateway, connID: conn, user: admin, pass: adminPassword}
		snap, err := rbac.ReadSnapshot(context.Background(), ex)
		if err != nil {
			t.Fatal(err)
		}
		return snap
	}

	t.Run("role, grant and row policy filter rows", func(t *testing.T) {
		ar := apply([]rbac.Op{
			{Kind: rbac.OpCreateRole, Name: roleName},
			{Kind: rbac.OpGrantPrivilege, Target: &rbac.Target{Type: "role", Name: roleName}, Access: []string{"SELECT"}, Database: db, Table: "events"},
			{Kind: rbac.OpGrantRole, Target: &rbac.Target{Type: "user", Name: userName}, Roles: []string{roleName}},
			{Kind: rbac.OpSetDefaultRoles, Target: &rbac.Target{Type: "user", Name: userName}, Roles: []string{roleName}},
		}, "")
		for _, r := range ar.Results {
			if r.Status != rbac.StatusApplied {
				t.Fatalf("result: %+v", r)
			}
		}
		if got := countAs(); got != "3" {
			t.Fatalf("before policy: %s rows", got)
		}
		apply([]rbac.Op{{Kind: rbac.OpCreateRowPolicy, RowPolicy: &rbac.RowPolicySpec{
			Name: policy, Database: db, Table: "events", Using: "tenant = 'a'", To: rbac.Assignees{Names: []string{roleName}},
		}}}, "")
		if got := countAs(); got != "2" {
			t.Fatalf("with policy: %s rows, want 2", got)
		}
		// A condition naming an unknown column is refused at plan time.
		code, body := call("/plan", map[string]any{"ops": []rbac.Op{{Kind: rbac.OpAlterRowPolicy, RowPolicy: &rbac.RowPolicySpec{
			Name: policy, Database: db, Table: "events", Using: "no_such_column = 1", To: rbac.Assignees{Names: []string{roleName}},
		}}}})
		if code != http.StatusBadRequest {
			t.Fatalf("bad condition: %d %s", code, body)
		}
	})

	t.Run("quota and profile round trip", func(t *testing.T) {
		four, one, eight := "4", "1", "8"
		apply([]rbac.Op{
			{Kind: rbac.OpCreateSettingsProfile, SettingsProfile: &rbac.ProfileSpec{Name: profile, Settings: []rbac.ProfileSetting{
				{Name: "max_threads", Value: &four, Min: &one, Max: &eight},
			}, To: rbac.Assignees{Names: []string{roleName}}}},
			{Kind: rbac.OpCreateQuota, Quota: &rbac.QuotaSpec{Name: quota, KeyedBy: "user_name", Intervals: []rbac.QuotaInterval{
				{Duration: 1, Unit: "hour", Limits: map[string]uint64{"queries": 100, "errors": 5}},
			}, To: rbac.Assignees{Names: []string{userName}}}},
		}, "")
		snap := snapshot()
		var q *rbac.Quota
		for i := range snap.Quotas {
			if snap.Quotas[i].Name == quota {
				q = &snap.Quotas[i]
			}
		}
		if q == nil || q.KeyedBy != "user_name" || len(q.Intervals) != 1 || q.Intervals[0].Limits["queries"] != 100 || q.Intervals[0].Unit != "hour" {
			t.Fatalf("quota = %+v", q)
		}
		var p *rbac.SettingsProfile
		for i := range snap.SettingsProfiles {
			if snap.SettingsProfiles[i].Name == profile {
				p = &snap.SettingsProfiles[i]
			}
		}
		if p == nil || len(p.Settings) != 1 || p.Settings[0].Value == nil || *p.Settings[0].Value != "4" {
			t.Fatalf("profile = %+v", p)
		}

		// Alter drops the errors limit and adds a day interval, then the
		// rollback ops bring the original back.
		ar := apply([]rbac.Op{{Kind: rbac.OpAlterQuota, Quota: &rbac.QuotaSpec{Name: quota, KeyedBy: "user_name", Intervals: []rbac.QuotaInterval{
			{Duration: 1, Unit: "hour", Limits: map[string]uint64{"queries": 50}},
			{Duration: 1, Unit: "day", Limits: map[string]uint64{"read_rows": 1000}},
		}, To: rbac.Assignees{Names: []string{userName}}}}}, "")
		snap = snapshot()
		for _, sq := range snap.Quotas {
			if sq.Name == quota && (len(sq.Intervals) != 2 || sq.Intervals[0].Limits["errors"] != 0 || sq.Intervals[0].Limits["queries"] != 50) {
				t.Fatalf("altered quota = %+v", sq)
			}
		}
		apply(ar.RollbackOps, "")
		snap = snapshot()
		for _, sq := range snap.Quotas {
			if sq.Name == quota && (len(sq.Intervals) != 1 || sq.Intervals[0].Limits["errors"] != 5 || sq.Intervals[0].Limits["queries"] != 100) {
				t.Fatalf("rolled back quota = %+v", sq)
			}
		}
	})

	t.Run("partial failure stops the run", func(t *testing.T) {
		ops := []rbac.Op{
			{Kind: rbac.OpCreateRole, Name: otherRole},
			{Kind: rbac.OpCreateRole, Name: roleName}, // exists: error 493
			{Kind: rbac.OpCreateRole, Name: otherRole + "_never"},
		}
		ar := apply(ops, "")
		got := []string{}
		for _, r := range ar.Results {
			got = append(got, r.Status)
		}
		if strings.Join(got, ",") != "applied,failed,skipped" || ar.Results[1].Code != "already_exists" {
			t.Fatalf("results = %+v", ar.Results)
		}
		if snap := snapshot(); func() bool {
			for _, r := range snap.Roles {
				if r.Name == otherRole+"_never" {
					return true
				}
			}
			return false
		}() {
			t.Fatal("a skipped statement ran")
		}
	})

	t.Run("on cluster", func(t *testing.T) {
		cluster := os.Getenv("CHUI_TEST_CLICKHOUSE_CLUSTER")
		if cluster == "" {
			t.Skip("set CHUI_TEST_CLICKHOUSE_CLUSTER to test ON CLUSTER")
		}
		clusterRole := name("cluster_role")
		t.Cleanup(func() { _, _ = exec(admin, adminPassword, "DROP ROLE IF EXISTS "+clusterRole+" ON CLUSTER "+cluster) })
		ar := apply([]rbac.Op{{Kind: rbac.OpCreateRole, Name: clusterRole}}, cluster)
		if ar.Results[0].Status != rbac.StatusApplied || len(ar.Results[0].Hosts) == 0 {
			t.Fatalf("on cluster result = %+v", ar.Results[0])
		}
	})
}
