// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package ssoidentity

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/database"
)

func sweepConfig() database.SSOIdentityConfig {
	cfg := perPersonConfig()
	cfg.LockAfterDays = 30
	cfg.DropAfterDays = 90
	return cfg
}

// seedUser gives email a per-person user on conn last seen seenAgo ago,
// locked or not, as if they had signed in back then.
func seedUser(t *testing.T, db *database.DB, conn, email string, seenAgo time.Duration, locked bool) database.SSOPersonUser {
	t.Helper()
	ident, err := db.UpsertSSOIdentity("https://idp.example", "sub-"+email, email, email, []string{"analysts"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SaveSSOClickHouseUser(database.SSOClickHouseUser{
		IdentityID: ident.ID, ConnectionID: conn, CHUser: "sso:" + email, EncryptedPassword: "enc", Roles: []string{"analyst"},
	}); err != nil {
		t.Fatal(err)
	}
	seen := time.Now().UTC().Add(-seenAgo).Format(time.RFC3339)
	var lockedAt any
	if locked {
		lockedAt = seen
	}
	if _, err := db.Conn().Exec("UPDATE sso_clickhouse_users SET last_seen_at = ?, locked_at = ? WHERE identity_id = ? AND connection_id = ?", seen, lockedAt, ident.ID, conn); err != nil {
		t.Fatal(err)
	}
	users, err := db.ListSSOClickHouseUsers(conn)
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range users {
		if u.IdentityID == ident.ID {
			return u
		}
	}
	t.Fatalf("seeded user %s not listed", email)
	return database.SSOPersonUser{}
}

func TestSweepLocksAndDropsIdleUsers(t *testing.T) {
	db, conn, agent, p := setup(t, sweepConfig(), responder{})
	day := 24 * time.Hour
	gone := seedUser(t, db, conn, "gone@x.com", 100*day, false)
	idle := seedUser(t, db, conn, "idle@x.com", 40*day, false)
	already := seedUser(t, db, conn, "already@x.com", 40*day, true)
	fresh := seedUser(t, db, conn, "fresh@x.com", 2*day, false)

	sw := NewSweeper(db, p, func() bool { return true })
	res := sw.Tick(context.Background())
	if want := (SweepResult{Locked: 1, Dropped: 1}); res != want {
		t.Fatalf("result = %+v, want %+v", res, want)
	}
	// Oldest sign-in first: the drop, then the lock. The locked and the
	// fresh user get no statement.
	want := []string{
		"DROP USER IF EXISTS `sso:gone@x.com`",
		"ALTER USER `sso:idle@x.com` HOST NONE",
	}
	if got := sqls(queries(agent, 0)); !reflect.DeepEqual(got, want) {
		t.Fatalf("statements = %q, want %q", got, want)
	}

	if u, _ := db.GetSSOClickHouseUser(gone.IdentityID, conn); u != nil {
		t.Fatalf("dropped user still stored: %+v", u)
	}
	if u, _ := db.GetSSOClickHouseUser(idle.IdentityID, conn); u == nil || !u.Locked() {
		t.Fatalf("idle user not locked: %+v", u)
	}
	for _, keep := range []database.SSOPersonUser{already, fresh} {
		u, _ := db.GetSSOClickHouseUser(keep.IdentityID, conn)
		if u == nil || u.Locked() != keep.Locked() {
			t.Fatalf("%s changed: %+v", keep.Email, u)
		}
	}

	// A second tick finds nothing left to do.
	before := len(queries(agent, 0))
	if res := sw.Tick(context.Background()); res != (SweepResult{}) {
		t.Fatalf("second tick = %+v, want nothing", res)
	}
	if n := len(queries(agent, before)); n != 0 {
		t.Fatalf("second tick sent %d statements", n)
	}

	logs, err := db.GetAuditLogs(50)
	if err != nil {
		t.Fatal(err)
	}
	actions := map[string]string{}
	for _, l := range logs {
		if l.Username != nil && *l.Username == sweepActor {
			actions[l.Action] = *l.Username
		}
	}
	if len(actions) != 2 || actions["sso.user_dropped"] == "" || actions["sso.user_locked"] == "" {
		t.Fatalf("audit actions by %s = %v, want lock and drop", sweepActor, actions)
	}
}

func TestSweepNeedsPro(t *testing.T) {
	db, conn, agent, p := setup(t, sweepConfig(), responder{})
	seedUser(t, db, conn, "gone@x.com", 100*24*time.Hour, false)

	sw := NewSweeper(db, p, func() bool { return false })
	if res := sw.Tick(context.Background()); res != (SweepResult{}) {
		t.Fatalf("free license tick = %+v, want nothing", res)
	}
	if n := len(queries(agent, 0)); n != 0 {
		t.Fatalf("free license sent %d statements", n)
	}
	// Start and Stop run the first tick inline and return.
	sw.Start()
	sw.Start()
	sw.Stop()
	sw.Stop()
}

func TestSweepSkipsOfflineAndDisabledConnections(t *testing.T) {
	db, online, agent, p := setup(t, sweepConfig(), responder{})
	day := 24 * time.Hour

	// Thresholds off: the online connection keeps its idle users.
	off := sweepConfig()
	off.LockAfterDays, off.DropAfterDays = 0, 0
	if err := db.SetSSOIdentityConfig(online, off); err != nil {
		t.Fatal(err)
	}
	seedUser(t, db, online, "kept@x.com", 400*day, false)

	// A second connection in per-person mode with no agent connected.
	offline, err := db.CreateConnection(database.CreateConnectionParams{Name: "offline", TunnelToken: "offline-token"})
	if err != nil {
		t.Fatal(err)
	}
	enc, err := crypto.Encrypt("prov-pw", testSecret)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetConnectionSSOAccount(offline, "chui_prov", enc); err != nil {
		t.Fatal(err)
	}
	if err := db.SetSSOIdentityConfig(offline, sweepConfig()); err != nil {
		t.Fatal(err)
	}
	stale := seedUser(t, db, offline, "stale@x.com", 400*day, false)

	sw := NewSweeper(db, p, func() bool { return true })
	if res := sw.Tick(context.Background()); res != (SweepResult{}) {
		t.Fatalf("tick = %+v, want nothing", res)
	}
	if n := len(queries(agent, 0)); n != 0 {
		t.Fatalf("%d statements sent, want none", n)
	}
	if u, _ := db.GetSSOClickHouseUser(stale.IdentityID, offline); u == nil || u.Locked() {
		t.Fatalf("offline connection's user changed: %+v", u)
	}
	if users, _ := db.ListSSOClickHouseUsers(online); len(users) != 1 || users[0].Locked() {
		t.Fatalf("thresholds off, but users changed: %+v", users)
	}
}
