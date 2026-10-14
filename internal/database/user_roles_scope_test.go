package database

import (
	"errors"
	"testing"
	"time"
)

// scopeFixture makes two saved connections (a, b) and one created from the
// login page (l).
func scopeFixture(t *testing.T) (db *DB, a, b, l string) {
	t.Helper()
	db = openTestDB(t)
	mk := func(name string) string {
		id, err := db.CreateConnection(CreateConnectionParams{Name: name, TunnelToken: "cht_" + name, Type: ConnectionTypeTunnel})
		if err != nil {
			t.Fatalf("CreateConnection %s: %v", name, err)
		}
		return id
	}
	a, b, l = mk("conn-a"), mk("conn-b"), mk("login-url")
	if err := db.SetSetting(SettingLoginURLConnectionIDs, `["`+l+`"]`); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	return db, a, b, l
}

func countUserRoles(t *testing.T, db *DB) int {
	t.Helper()
	var n int
	if err := db.conn.QueryRow("SELECT COUNT(*) FROM user_roles").Scan(&n); err != nil {
		t.Fatalf("count user_roles: %v", err)
	}
	return n
}

func mustRole(t *testing.T, db *DB, key, conn, want string) {
	t.Helper()
	got, err := db.GetUserRole(key, conn)
	if err != nil {
		t.Fatalf("GetUserRole(%s, %s): %v", key, conn, err)
	}
	if got != want {
		t.Fatalf("GetUserRole(%s, %s) = %q, want %q", key, conn, got, want)
	}
}

// putLegacyUserRoles swaps user_roles back to the pre-2026.10.15 schema,
// keyed on the username alone.
func putLegacyUserRoles(t *testing.T, db *DB, rows map[string]string) {
	t.Helper()
	mustExec(t, db, `DROP TABLE user_roles`)
	mustExec(t, db, `CREATE TABLE user_roles (
		username TEXT PRIMARY KEY,
		role TEXT NOT NULL DEFAULT 'viewer',
		created_at TEXT DEFAULT CURRENT_TIMESTAMP
	)`)
	for user, role := range rows {
		mustExec(t, db, `INSERT INTO user_roles (username, role) VALUES (?, ?)`, user, role)
	}
}

func TestMigrateUserRolesV2ScopesOverrides(t *testing.T) {
	db, a, b, l := scopeFixture(t)
	expires := time.Now().UTC().Add(time.Hour).Format(time.RFC3339)
	for _, p := range []CreateSessionParams{
		{ConnectionID: l, ClickhouseUser: "default", EncryptedPassword: "enc", Token: "tok-l", ExpiresAt: expires, UserRole: "admin"},
		{ConnectionID: a, ClickhouseUser: "default", EncryptedPassword: "enc", Token: "tok-a", ExpiresAt: expires, UserRole: "admin"},
		{ConnectionID: a, ClickhouseUser: "sso:alice@example.com", EncryptedPassword: "enc", Token: "tok-reserved", ExpiresAt: expires, UserRole: "viewer"},
		{ConnectionID: a, ClickhouseUser: "svc", EncryptedPassword: "enc", Token: "tok-sso", ExpiresAt: expires, UserRole: "viewer", AuthSubject: "alice@example.com"},
	} {
		if _, err := db.CreateSession(p); err != nil {
			t.Fatalf("CreateSession %s: %v", p.Token, err)
		}
	}
	putLegacyUserRoles(t, db, map[string]string{"default": "admin", "ana": "analyst", "sso:alice@example.com": "admin"})

	if err := db.migrateUserRolesV2(); err != nil {
		t.Fatalf("migrateUserRolesV2: %v", err)
	}

	for _, conn := range []string{a, b} {
		mustRole(t, db, "default", conn, "admin")
		mustRole(t, db, "ana", conn, "analyst")
	}
	mustRole(t, db, "default", l, "")
	mustRole(t, db, "ana", l, "")
	mustRole(t, db, "sso:alice@example.com", l, "admin") // SSO stays global
	if n := countUserRoles(t, db); n != 5 {
		t.Fatalf("rows = %d, want 5 (2 users x 2 saved connections + 1 SSO)", n)
	}
	var global int
	if err := db.conn.QueryRow(`SELECT COUNT(*) FROM user_roles WHERE connection_id = ''`).Scan(&global); err != nil || global != 1 {
		t.Fatalf("global rows = %d (err %v), want only the SSO one", global, err)
	}

	if r := sessionRole(t, db, "tok-l"); r != "analyst" {
		t.Fatalf("cached admin on login URL connection = %q, want analyst", r)
	}
	if r := sessionRole(t, db, "tok-a"); r != "admin" {
		t.Fatalf("cached admin on saved connection = %q, want admin kept", r)
	}
	if s, err := db.GetSession("tok-reserved"); err != nil || s != nil {
		t.Fatalf("password session named sso:... kept: %+v %v", s, err)
	}
	if s, err := db.GetSession("tok-sso"); err != nil || s == nil {
		t.Fatalf("real SSO session dropped: %v", err)
	}

	// Idempotent.
	if err := db.migrateUserRolesV2(); err != nil {
		t.Fatalf("second migrateUserRolesV2: %v", err)
	}
	if n := countUserRoles(t, db); n != 5 {
		t.Fatalf("rows after rerun = %d, want 5", n)
	}

	// Deleting a connection drops its overrides.
	if err := db.DeleteConnection(a); err != nil {
		t.Fatalf("DeleteConnection: %v", err)
	}
	if n := countUserRoles(t, db); n != 3 {
		t.Fatalf("rows after deleting a connection = %d, want 3", n)
	}
}

func TestMigrateUserRolesV2RollsBackOnFailure(t *testing.T) {
	db, a, _, _ := scopeFixture(t)
	putLegacyUserRoles(t, db, map[string]string{"default": "admin"})

	failAt(t, "user_roles: copied")
	if err := db.migrateUserRolesV2(); !errors.Is(err, errInjected) {
		t.Fatalf("err = %v, want injected failure", err)
	}
	if has, err := db.columnExists("user_roles", "connection_id"); err != nil || has {
		t.Fatalf("connection_id present after rollback (has=%v, err=%v)", has, err)
	}
	if exists, err := db.tableExists("user_roles_old"); err != nil || exists {
		t.Fatalf("user_roles_old left behind (exists=%v, err=%v)", exists, err)
	}

	rebuildFailpoint = nil
	if err := db.migrateUserRolesV2(); err != nil {
		t.Fatalf("retry: %v", err)
	}
	mustRole(t, db, "default", a, "admin")
}

func TestMigrateUserRolesV2UnreadableLoginURLListCopiesNoClickHouseOverride(t *testing.T) {
	db, a, _, _ := scopeFixture(t)
	putLegacyUserRoles(t, db, map[string]string{"default": "admin", "sso:alice@example.com": "analyst"})
	if err := db.SetSetting(SettingLoginURLConnectionIDs, "not json"); err != nil {
		t.Fatal(err)
	}
	if err := db.migrateUserRolesV2(); err != nil {
		t.Fatalf("migrateUserRolesV2: %v", err)
	}
	mustRole(t, db, "default", a, "")
	mustRole(t, db, "sso:alice@example.com", a, "analyst")
}

func TestUserRoleScope(t *testing.T) {
	db, a, b, l := scopeFixture(t)

	if err := db.SetUserRole("alice", a, "admin"); err != nil {
		t.Fatalf("SetUserRole: %v", err)
	}
	mustRole(t, db, "alice", a, "admin")
	mustRole(t, db, "alice", b, "")
	mustRole(t, db, "alice", "", "")

	if err := db.SetUserRole("alice", l, "admin"); !errors.Is(err, ErrRoleLoginURLConnection) {
		t.Fatalf("override on login URL connection: err = %v", err)
	}
	if err := db.SetUserRole("alice", "", "admin"); !errors.Is(err, ErrRoleConnectionRequired) {
		t.Fatalf("override without connection: err = %v", err)
	}
	if err := db.SetUserRole("alice", "missing", "admin"); !errors.Is(err, ErrRoleConnectionNotFound) {
		t.Fatalf("override on missing connection: err = %v", err)
	}

	// SSO overrides ignore the connection and apply everywhere.
	if err := db.SetUserRole("sso:bob@example.com", a, "analyst"); err != nil {
		t.Fatalf("SetUserRole sso: %v", err)
	}
	mustRole(t, db, "sso:bob@example.com", b, "analyst")
	mustRole(t, db, "sso:bob@example.com", l, "analyst")

	// The schema refuses a global ClickHouse user row or a scoped SSO row.
	if _, err := db.conn.Exec(`INSERT INTO user_roles (username, connection_id, role) VALUES ('carol', '', 'admin')`); err == nil {
		t.Fatal("global override for a ClickHouse user was accepted")
	}
	if _, err := db.conn.Exec(`INSERT INTO user_roles (username, connection_id, role) VALUES ('sso:x@example.com', ?, 'admin')`, a); err == nil {
		t.Fatal("scoped override for an SSO key was accepted")
	}

	if err := db.DeleteUserRole("alice", b); err != nil {
		t.Fatal(err)
	}
	mustRole(t, db, "alice", a, "admin") // other connection untouched
	if err := db.DeleteUserRole("alice", a); err != nil {
		t.Fatal(err)
	}
	mustRole(t, db, "alice", a, "")
}

func TestBootstrapFirstAdmin(t *testing.T) {
	db, a, b, _ := scopeFixture(t)

	ok, err := db.BootstrapFirstAdmin("default", a)
	if err != nil || !ok {
		t.Fatalf("first bootstrap = %v, %v", ok, err)
	}
	ok, err = db.BootstrapFirstAdmin("other", b)
	if err != nil || ok {
		t.Fatalf("second bootstrap = %v, %v, want refused", ok, err)
	}
	mustRole(t, db, "other", b, "")
	mustRole(t, db, "default", b, "")

	// An SSO admin anywhere also blocks it.
	db2, a2, _, _ := scopeFixture(t)
	if err := db2.SetUserRole("sso:boss@example.com", "", "admin"); err != nil {
		t.Fatal(err)
	}
	if ok, err := db2.BootstrapFirstAdmin("default", a2); err != nil || ok {
		t.Fatalf("bootstrap with an SSO admin = %v, %v, want refused", ok, err)
	}
}

func TestSessionRoleScopedAndCapped(t *testing.T) {
	db, a, b, l := scopeFixture(t)
	if err := db.SetUserRole("default", a, "admin"); err != nil {
		t.Fatal(err)
	}
	if err := db.SetUserRole("sso:boss@example.com", "", "admin"); err != nil {
		t.Fatal(err)
	}
	role := func(conn, user, cached, subject string) string {
		t.Helper()
		s := &Session{ConnectionID: conn, ClickhouseUser: user, UserRole: &cached}
		if subject != "" {
			s.AuthSubject = &subject
		}
		r, err := db.SessionRole(s)
		if err != nil {
			t.Fatalf("SessionRole: %v", err)
		}
		return r
	}
	cases := []struct {
		name, conn, user, cached, subject, want string
	}{
		{"override on its connection", a, "default", "viewer", "", "admin"},
		{"same name on another connection uses detected role", b, "default", "viewer", "", "viewer"},
		{"cached admin on login URL connection is capped", l, "default", "admin", "", "analyst"},
		{"SSO admin override is capped on a login URL connection", l, "svc", "viewer", "boss@example.com", "analyst"},
		{"SSO admin override applies on saved connections", b, "svc", "viewer", "boss@example.com", "admin"},
		{"password user named like an SSO key gets no override", a, "sso:boss@example.com", "admin", "", "viewer"},
	}
	for _, c := range cases {
		if got := role(c.conn, c.user, c.cached, c.subject); got != c.want {
			t.Errorf("%s: role = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestIsLoginURLConnectionFailsClosed(t *testing.T) {
	db, a, _, l := scopeFixture(t)
	if ok, err := db.IsLoginURLConnection(l); err != nil || !ok {
		t.Fatalf("login URL connection = %v, %v", ok, err)
	}
	if ok, err := db.IsLoginURLConnection(a); err != nil || ok {
		t.Fatalf("saved connection = %v, %v", ok, err)
	}
	if err := db.SetSetting(SettingLoginURLConnectionIDs, "{broken"); err != nil {
		t.Fatal(err)
	}
	if ok, err := db.IsLoginURLConnection(a); !errors.Is(err, ErrLoginURLListUnreadable) || !ok {
		t.Fatalf("unreadable list = %v, %v, want true and ErrLoginURLListUnreadable", ok, err)
	}
}

func TestSetSessionsUserRoleScopedToConnection(t *testing.T) {
	db, a, b, _ := scopeFixture(t)
	expires := time.Now().UTC().Add(time.Hour).Format(time.RFC3339)
	for _, p := range []CreateSessionParams{
		{ConnectionID: a, ClickhouseUser: "default", EncryptedPassword: "enc", Token: "tok-a", ExpiresAt: expires, UserRole: "viewer"},
		{ConnectionID: b, ClickhouseUser: "default", EncryptedPassword: "enc", Token: "tok-b", ExpiresAt: expires, UserRole: "viewer"},
	} {
		if _, err := db.CreateSession(p); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.SetSessionsUserRole("default", a, "admin"); err != nil {
		t.Fatal(err)
	}
	if r := sessionRole(t, db, "tok-a"); r != "admin" {
		t.Fatalf("session on a = %q, want admin", r)
	}
	if r := sessionRole(t, db, "tok-b"); r != "viewer" {
		t.Fatalf("session on b = %q, want viewer untouched", r)
	}

	users, err := db.GetUsers(b)
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 1 || users[0].UserRole != "viewer" {
		t.Fatalf("GetUsers(b) = %+v, want only b's session", users)
	}
	if all, err := db.GetUsers(""); err != nil || len(all) != 1 {
		t.Fatalf("GetUsers(\"\") = %+v, %v", all, err)
	}
}
