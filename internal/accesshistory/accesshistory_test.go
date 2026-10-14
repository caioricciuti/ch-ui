// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package accesshistory

import (
	"encoding/json"
	"testing"

	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/database/dbtest"
	"github.com/caioricciuti/ch-ui/internal/rbac"
)

func snap(host string, roles ...rbac.Entity) *rbac.Snapshot {
	return &rbac.Snapshot{
		Roles: roles, Users: []rbac.UserEntity{{Name: "admin", Storage: "local_directory"}},
		RoleProfiles: map[string][]string{}, Capabilities: rbac.Capabilities{ShowAccess: true},
		Host: host, MultiNode: host != "",
	}
}

func record(t *testing.T, db *database.DB, c Capture) (*database.RBACSnapshot, Summary) {
	t.Helper()
	row, _, err := Record(db, c)
	if err != nil {
		t.Fatal(err)
	}
	var s Summary
	if err := json.Unmarshal([]byte(row.Summary), &s); err != nil {
		t.Fatal(err)
	}
	return row, s
}

func TestClassifyDrift(t *testing.T) {
	db, err := dbtest.Open(t, "history.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	conn, err := db.CreateConnection(database.CreateConnectionParams{Name: "h", TunnelToken: "h-token"})
	if err != nil {
		t.Fatal(err)
	}
	replicated := rbac.Entity{Name: "shared", Storage: "replicated"}
	local := rbac.Entity{Name: "node_only", Storage: "local_directory"}
	gov := func(s *rbac.Snapshot) Capture {
		return Capture{ConnectionID: conn, Source: SourceSync, CHUser: "gov", Snapshot: s}
	}

	if row, sum := record(t, db, gov(snap("h1", replicated, local))); row.Drift != DriftNone || !sum.First {
		t.Fatalf("first row: %+v %+v", row, sum)
	}

	// Another host without the local role: not drift.
	row, sum := record(t, db, gov(snap("h2", replicated)))
	if row.Drift != DriftNone || !sum.HostChanged || sum.PreviousHost != "h1" {
		t.Fatalf("host change: %+v %+v", row, sum)
	}

	// A replicated role appearing is drift even with the host unchanged.
	added := rbac.Entity{Name: "added", Storage: "replicated"}
	if row, sum := record(t, db, gov(snap("h2", replicated, added))); row.Drift != DriftOutside || sum.Added["entity"] != 1 {
		t.Fatalf("outside: %+v %+v", row, sum)
	}

	// An apply by the session user explains the next governance change.
	pre := snap("h2", replicated, added)
	ops := []rbac.Op{{Kind: rbac.OpCreateRole, Name: "via_ui"}}
	post := rbac.Simulate(pre, ops)
	post.Roles[len(post.Roles)-1].Storage = "replicated"
	if row, _ := record(t, db, Capture{ConnectionID: conn, Source: SourceApply, CHUser: "admin", Snapshot: post, Pre: pre, Applied: ops}); row.Drift != DriftNone {
		t.Fatalf("apply row: %+v", row)
	}
	if row, sum := record(t, db, gov(post)); row.Drift != DriftNone || sum.Note == "" {
		t.Fatalf("change made by the apply flagged: %+v %+v", row, sum)
	}

	// An incomplete read is stored but never judged.
	partial := snap("h2", replicated)
	partial.Errors = []string{"grants: denied"}
	if row, sum := record(t, db, gov(partial)); row.Complete || row.Drift != DriftNone || len(sum.Incomplete) != 1 {
		t.Fatalf("incomplete: %+v %+v", row, sum)
	}

	// Bodies round-trip.
	_, body, err := db.GetRBACSnapshot(conn, row.ID)
	if err != nil {
		t.Fatal(err)
	}
	st, err := DecodeState(body)
	if err != nil || len(st.Roles) != 1 || st.Roles[0].Name != "shared" {
		t.Fatalf("decoded state: %+v err=%v", st, err)
	}
}
