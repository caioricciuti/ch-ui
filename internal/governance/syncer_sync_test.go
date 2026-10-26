// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package governance

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/database/dbtest"
	"github.com/caioricciuti/ch-ui/internal/testutil"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

// sourceFixture answers governance queries by the system table they read.
// A source listed in fail gets a ClickHouse access error; block, when set,
// holds every answer until it is closed.
type sourceFixture struct {
	mu    sync.Mutex
	rows  map[string]string
	fail  map[string]bool
	block chan struct{}
	seen  []string
}

func sourceOf(sql string) string {
	switch {
	case strings.Contains(sql, "system.parts"):
		return "system.parts"
	case strings.Contains(sql, "FROM system.tables"):
		return "system.tables"
	case strings.Contains(sql, "FROM system.databases"):
		return "system.databases"
	case strings.Contains(sql, "FROM system.columns"):
		return "system.columns"
	case strings.Contains(sql, "FROM system.users"):
		return "system.users"
	case strings.Contains(sql, "FROM system.roles"):
		return "system.roles"
	case strings.Contains(sql, "FROM system.role_grants"):
		return "system.role_grants"
	case strings.Contains(sql, "FROM system.grants"):
		return "system.grants"
	}
	return "other"
}

func (f *sourceFixture) respond(msg tunnel.GatewayMessage) *tunnel.AgentMessage {
	if f.block != nil {
		<-f.block
	}
	sql := msg.Query
	if sql == "" {
		sql = msg.SQL
	}
	src := sourceOf(sql)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seen = append(f.seen, src)
	if f.fail[src] {
		return &tunnel.AgentMessage{Type: "query_error", Error: "Code: 497. DB::Exception: restricted: Not enough privileges. (ACCESS_DENIED)"}
	}
	rows, ok := f.rows[src]
	if !ok {
		rows = "[]"
	}
	return &tunnel.AgentMessage{Type: "query_result", Data: json.RawMessage(rows)}
}

func (f *sourceFixture) set(src, rows string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rows[src] = rows
}

func (f *sourceFixture) setFail(src string, fail bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fail[src] = fail
}

func (f *sourceFixture) queried(src string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, s := range f.seen {
		if s == src {
			n++
		}
	}
	return n
}

func (f *sourceFixture) total() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.seen)
}

func newSourceTest(t *testing.T) (*Syncer, *Store, *database.DB, *sourceFixture, string) {
	t.Helper()
	db, conn := testutil.WorkerDB(t)
	f := &sourceFixture{rows: map[string]string{}, fail: map[string]bool{}}
	agent := testutil.NewAgent(t, db, "worker-token", f.respond)
	store := NewStore(db)
	return NewSyncer(store, db, agent.Gateway, "test-secret", func() bool { return true }), store, db, f, conn
}

func setGovernanceAccount(t *testing.T, db *database.DB, conn string) {
	t.Helper()
	enc, err := crypto.Encrypt("gov-pass", "test-secret")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetBackgroundCredential(conn, database.BackgroundCredential{Worker: "governance", Mode: "service_account", Username: "gov", EncryptedPassword: enc}); err != nil {
		t.Fatal(err)
	}
}

func mustSyncState(t *testing.T, store *Store, conn string, st SyncType) *SyncState {
	t.Helper()
	state, err := store.GetSyncState(conn, string(st))
	if err != nil || state == nil {
		t.Fatalf("sync state %s: %v %+v", st, err, state)
	}
	return state
}

// An access source the account cannot read keeps its stored rows, names the
// table in the error, and leaves guardrails on the uncertain path.
func TestAccessSyncUnreadableSourceKeepsDataAndIsUncertain(t *testing.T) {
	s, store, db, f, conn := newSourceTest(t)
	creds := CHCredentials{ConnectionID: conn, User: "restricted"}
	f.set("system.users", `[{"name":"alice","auth_type":"sha256_password","host_ip":"[]","default_roles_all":1,"default_roles_list":"[]"}]`)
	f.set("system.roles", `[{"name":"reader"}]`)
	f.set("system.grants", `[{"user_name":"alice","role_name":null,"access_type":"SELECT","grant_database":"db","grant_table":"t","grant_column":null,"is_partial_revoke":0,"grant_option":0}]`)
	if _, err := s.syncAccess(context.Background(), creds); err != nil {
		t.Fatalf("first access sync: %v", err)
	}
	first := mustSyncState(t, store, conn, SyncAccess)
	if first.Status != "idle" || first.LastSyncedAt == nil {
		t.Fatalf("first sync state: %+v", first)
	}
	guard := NewGuardrailService(store, db)
	if uncertain, reason := guard.isAccessStateUncertain(conn); uncertain {
		t.Fatalf("fresh access sync judged uncertain: %s", reason)
	}

	f.setFail("system.users", true)
	f.setFail("system.grants", true)
	_, err := s.syncAccess(context.Background(), creds)
	if err == nil {
		t.Fatal("access sync with unreadable sources: want an error")
	}
	for _, want := range []string{`"restricted"`, "system.users, system.grants", "ACCESS_DENIED"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not mention %s", err, want)
		}
	}

	users, _ := store.GetChUsers(conn)
	grants, _ := store.GetGrants(conn)
	if len(users) != 1 || users[0].Name != "alice" || len(grants) != 1 {
		t.Fatalf("unreadable sources must keep stored rows: users=%+v grants=%d", users, len(grants))
	}

	state := mustSyncState(t, store, conn, SyncAccess)
	if state.Status != "error" || state.LastError == nil || !strings.Contains(*state.LastError, "system.users") {
		t.Fatalf("state after partial sync: %+v", state)
	}
	if state.ConsecutiveFailures != 1 || state.LastSyncedAt == nil || *state.LastSyncedAt != *first.LastSyncedAt {
		t.Fatalf("failure must count and keep the last success: %+v", state)
	}
	if state.CHUser == nil || *state.CHUser != "restricted" {
		t.Fatalf("ch_user = %v, want restricted", state.CHUser)
	}
	if uncertain, _ := guard.isAccessStateUncertain(conn); !uncertain {
		t.Fatal("guardrails must treat a partial access sync as uncertain")
	}
}

// Without system.parts the table list comes from system.tables alone; without
// system.tables nothing is diffed, so no spurious schema changes appear.
func TestMetadataSyncWithoutPartsOrTables(t *testing.T) {
	s, store, _, f, conn := newSourceTest(t)
	creds := CHCredentials{ConnectionID: conn, User: "restricted"}
	f.set("system.databases", `[{"name":"db","engine":"Atomic"}]`)
	f.setFail("system.parts", true)
	f.set("system.tables", `[{"database_name":"db","table_name":"t","engine":"MergeTree","table_uuid":"u1","total_rows":0,"total_bytes":0,"partition_count":0}]`)
	f.set("system.columns", `[{"database_name":"db","table_name":"t","column_name":"id","column_type":"UInt64","column_position":1}]`)

	res, err := s.syncMetadata(context.Background(), creds)
	if err != nil {
		t.Fatalf("metadata sync without system.parts: %v", err)
	}
	if res.TablesSynced != 1 || res.ColumnsSynced != 1 || res.SchemaChanges != 3 {
		t.Fatalf("first run: %+v, want 1 table, 1 column, 3 changes (db, table, column added)", res)
	}
	res, err = s.syncMetadata(context.Background(), creds)
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if res.SchemaChanges != 0 {
		t.Fatalf("second run recorded %d schema changes, want 0", res.SchemaChanges)
	}
	if st := mustSyncState(t, store, conn, SyncMetadata); st.Status != "idle" {
		t.Fatalf("metadata state: %+v", st)
	}

	// Now system.tables is unreadable too.
	f.setFail("system.tables", true)
	columnsBefore := f.queried("system.columns")
	res, err = s.syncMetadata(context.Background(), creds)
	if err == nil || !strings.Contains(err.Error(), "system.tables") {
		t.Fatalf("want an error naming system.tables, got %v", err)
	}
	if res.SchemaChanges != 0 {
		t.Fatalf("unreadable system.tables recorded %d schema changes", res.SchemaChanges)
	}
	if f.queried("system.columns") != columnsBefore {
		t.Fatal("columns must not be read or diffed without this run's table list")
	}
	tables, _ := store.GetTables(conn)
	if len(tables) != 1 || tables[0].IsDeleted {
		t.Fatalf("stored tables must be kept: %+v", tables)
	}
	if st := mustSyncState(t, store, conn, SyncMetadata); st.Status != "error" || st.LastError == nil || !strings.Contains(*st.LastError, "system.tables") {
		t.Fatalf("metadata state: %+v", st)
	}
	changes, _ := store.GetSchemaChanges(conn, 100)
	if len(changes) != 3 {
		t.Fatalf("schema changes = %d, want the 3 from the first run", len(changes))
	}
}

func TestSyncStateKeepsLastSuccessAndCountsFailures(t *testing.T) {
	db, err := dbtest.Open(t, "gov-sync-state.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Conn().Exec(`INSERT INTO connections (id, name, tunnel_token, status) VALUES ('c1', 'Local', 'token-1', 'connected')`); err != nil {
		t.Fatal(err)
	}
	store := NewStore(db)
	get := func() *SyncState { return mustSyncState(t, store, "c1", SyncMetadata) }

	if err := store.MarkSyncRunning("c1", string(SyncMetadata), "admin"); err != nil {
		t.Fatal(err)
	}
	if st := get(); st.LastSyncedAt != nil || st.LastAttemptAt == nil || st.Status != "running" {
		t.Fatalf("running before any success: %+v", st)
	}
	if err := store.UpsertSyncState("c1", string(SyncMetadata), "idle", nil, nil, 5); err != nil {
		t.Fatal(err)
	}
	ok := get()
	if ok.LastSyncedAt == nil || ok.ConsecutiveFailures != 0 || ok.CHUser == nil || *ok.CHUser != "admin" {
		t.Fatalf("after success: %+v", ok)
	}
	msg := "boom"
	for want := 1; want <= 2; want++ {
		if err := store.MarkSyncRunning("c1", string(SyncMetadata), "admin"); err != nil {
			t.Fatal(err)
		}
		if err := store.UpsertSyncState("c1", string(SyncMetadata), "error", nil, &msg, 0); err != nil {
			t.Fatal(err)
		}
		st := get()
		if st.ConsecutiveFailures != want || st.LastSyncedAt == nil || *st.LastSyncedAt != *ok.LastSyncedAt || st.LastError == nil {
			t.Fatalf("failure %d: %+v", want, st)
		}
	}
	if err := store.UpsertSyncState("c1", string(SyncMetadata), "idle", nil, nil, 0); err != nil {
		t.Fatal(err)
	}
	if st := get(); st.ConsecutiveFailures != 0 || st.LastError != nil || st.Status != "idle" {
		t.Fatalf("success must reset failures: %+v", st)
	}
}

func TestSyncDueBackoff(t *testing.T) {
	for n, want := range map[int]time.Duration{1: 10 * time.Minute, 2: 20 * time.Minute, 3: 40 * time.Minute, 6: 320 * time.Minute, 7: 6 * time.Hour, 50: 6 * time.Hour} {
		if got := failureBackoff(n); got != want {
			t.Errorf("failureBackoff(%d) = %v, want %v", n, got, want)
		}
	}

	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	at := func(ago time.Duration) *string {
		v := now.Add(-ago).Format(time.RFC3339)
		return &v
	}
	cases := []struct {
		name  string
		state *SyncState
		want  bool
	}{
		{"never synced", nil, true},
		{"fresh success", &SyncState{LastSyncedAt: at(5 * time.Minute)}, false},
		{"stale success", &SyncState{LastSyncedAt: at(11 * time.Minute)}, true},
		{"no success yet", &SyncState{Status: "running", LastAttemptAt: at(time.Minute)}, true},
		{"3 failures, 30m ago", &SyncState{ConsecutiveFailures: 3, LastSyncedAt: at(48 * time.Hour), LastAttemptAt: at(30 * time.Minute)}, false},
		{"3 failures, 41m ago", &SyncState{ConsecutiveFailures: 3, LastSyncedAt: at(48 * time.Hour), LastAttemptAt: at(41 * time.Minute)}, true},
		{"many failures, 5h ago", &SyncState{ConsecutiveFailures: 20, LastAttemptAt: at(5 * time.Hour)}, false},
		{"many failures, 6h ago", &SyncState{ConsecutiveFailures: 20, LastAttemptAt: at(6 * time.Hour)}, true},
	}
	for _, c := range cases {
		if got := syncDue(c.state, now); got != c.want {
			t.Errorf("%s: syncDue = %v, want %v", c.name, got, c.want)
		}
	}
}

// A connection in failure backoff is skipped by the background tick, while a
// manual sync still runs at once.
func TestBackgroundTickHonoursBackoffManualSyncDoesNot(t *testing.T) {
	s, store, db, f, conn := newSourceTest(t)
	setGovernanceAccount(t, db, conn)
	msg := "earlier failure"
	for _, st := range allSyncTypes {
		if err := store.UpsertSyncState(conn, string(st), "error", nil, &msg, 0); err != nil {
			t.Fatal(err)
		}
	}
	s.backgroundTick()
	s.inflight.Wait()
	if n := f.total(); n != 0 {
		t.Fatalf("tick inside the backoff sent %d queries", n)
	}

	if _, err := s.SyncConnection(context.Background(), CHCredentials{ConnectionID: conn, User: "gov", Password: "gov-pass"}); err != nil {
		t.Fatal(err)
	}
	if f.queried("system.databases") != 1 {
		t.Fatal("manual sync did not run")
	}
	if st := mustSyncState(t, store, conn, SyncMetadata); st.ConsecutiveFailures != 0 || st.Status != "idle" {
		t.Fatalf("manual success must reset the backoff: %+v", st)
	}
}

// With no governance account the tick records why in the sync state, sends
// no SQL, and backs off instead of retrying every tick.
func TestBackgroundTickRecordsMissingAccount(t *testing.T) {
	s, store, db, f, conn := newSourceTest(t)

	s.backgroundTick()
	s.inflight.Wait()
	if n := f.total(); n != 0 {
		t.Fatalf("no account: %d queries sent", n)
	}
	for _, st := range allSyncTypes {
		state := mustSyncState(t, store, conn, st)
		if state.Status != "error" || state.LastError == nil || !strings.Contains(*state.LastError, "No governance account") || state.ConsecutiveFailures != 1 {
			t.Fatalf("%s state: %+v", st, state)
		}
	}
	if v, ok := s.credIssues.Load(conn); !ok || !strings.Contains(v.(string), "No governance account") {
		t.Fatalf("credential problem not remembered for once-per-change logging: %v", v)
	}

	// The next tick is inside the backoff: nothing is recorded again.
	s.backgroundTick()
	if st := mustSyncState(t, store, conn, SyncMetadata); st.ConsecutiveFailures != 1 {
		t.Fatalf("second tick inside the backoff counted again: %+v", st)
	}

	// Disabled is reported as such.
	if err := db.SetBackgroundCredential(conn, database.BackgroundCredential{Worker: "governance", Mode: "disabled"}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Conn().Exec("DELETE FROM gov_sync_state"); err != nil {
		t.Fatal(err)
	}
	s.backgroundTick()
	if st := mustSyncState(t, store, conn, SyncAccess); st.LastError == nil || !strings.Contains(*st.LastError, "disabled") {
		t.Fatalf("disabled state: %+v", st)
	}

	// Once an account exists the sync runs and the problem is cleared.
	setGovernanceAccount(t, db, conn)
	if _, err := db.Conn().Exec("DELETE FROM gov_sync_state"); err != nil {
		t.Fatal(err)
	}
	s.backgroundTick()
	s.inflight.Wait()
	if f.queried("system.databases") != 1 {
		t.Fatal("sync did not run once an account was set")
	}
	if _, ok := s.credIssues.Load(conn); ok {
		t.Fatal("credential problem not cleared")
	}
}

// backgroundTick starts syncs and returns; it does not wait for a slow one.
func TestBackgroundTickDoesNotWaitForSyncs(t *testing.T) {
	s, store, db, f, conn := newSourceTest(t)
	setGovernanceAccount(t, db, conn)
	f.block = make(chan struct{})

	done := make(chan struct{})
	go func() {
		s.backgroundTick()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		close(f.block)
		t.Fatal("backgroundTick waited for a blocked sync")
	}
	if _, running := s.activeSyncs.Load(conn); !running {
		// The goroutine may not have registered yet; give it a moment.
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			if _, running = s.activeSyncs.Load(conn); running {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if !running {
			close(f.block)
			t.Fatal("no sync in flight after the tick returned")
		}
	}

	close(f.block)
	s.inflight.Wait()
	if st := mustSyncState(t, store, conn, SyncMetadata); st.Status != "idle" {
		t.Fatalf("sync did not finish after release: %+v", st)
	}
}

// When system.parts cannot be read, existing tables keep their stored stats
// and new tables start at zero.
func TestMetadataSyncWithoutPartsKeepsStoredStats(t *testing.T) {
	s, store, _, f, conn := newSourceTest(t)
	creds := CHCredentials{ConnectionID: conn, User: "restricted"}
	f.set("system.databases", `[{"name":"db","engine":"Atomic"}]`)
	f.set("system.parts", `[{"database_name":"db","table_name":"t","engine":"MergeTree","table_uuid":"u1","total_rows":42,"total_bytes":4096,"partition_count":3}]`)
	if _, err := s.syncMetadata(context.Background(), creds); err != nil {
		t.Fatal(err)
	}

	f.setFail("system.parts", true)
	f.set("system.tables", `[
	 {"database_name":"db","table_name":"t","engine":"MergeTree","table_uuid":"u1","total_rows":0,"total_bytes":0,"partition_count":0},
	 {"database_name":"db","table_name":"n","engine":"MergeTree","table_uuid":"u2","total_rows":0,"total_bytes":0,"partition_count":0}
	]`)
	if _, err := s.syncMetadata(context.Background(), creds); err != nil {
		t.Fatal(err)
	}
	tables, err := store.GetTables(conn)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]GovTable{}
	for _, tbl := range tables {
		got[tbl.TableName] = tbl
	}
	if tb := got["t"]; tb.TotalRows != 42 || tb.TotalBytes != 4096 || tb.PartitionCount != 3 {
		t.Fatalf("existing table lost its stats: %+v", tb)
	}
	if tb, ok := got["n"]; !ok || tb.TotalRows != 0 || tb.TotalBytes != 0 || tb.PartitionCount != 0 {
		t.Fatalf("new table: %+v (found %v)", tb, ok)
	}
}
