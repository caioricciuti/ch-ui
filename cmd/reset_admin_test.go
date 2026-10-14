package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caioricciuti/ch-ui/internal/database"
)

// newResetAdminDB creates a migrated database file with two saved
// connections ("main", "second"), one created from the login page
// ("from-login"), and the given roles on "main" (SSO roles are global), and
// closes it, so runResetAdmin opens it the way the CLI does.
func newResetAdminDB(t *testing.T, roles map[string]string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ch-ui.db")
	db, err := database.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	ids := map[string]string{}
	for _, name := range []string{"main", "second", "from-login"} {
		id, err := db.CreateConnection(database.CreateConnectionParams{Name: name, TunnelToken: "cht_" + name, Type: database.ConnectionTypeTunnel})
		if err != nil {
			t.Fatalf("CreateConnection(%s): %v", name, err)
		}
		ids[name] = id
	}
	if err := db.SetSetting(database.SettingLoginURLConnectionIDs, `["`+ids["from-login"]+`"]`); err != nil {
		t.Fatal(err)
	}
	for user, role := range roles {
		if err := db.SetUserRole(user, ids["main"], role); err != nil {
			t.Fatalf("SetUserRole(%s): %v", user, err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func reopen(t *testing.T, path string) *database.DB {
	t.Helper()
	db, err := database.Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// roleOf returns user's override on the named connection ("main" when
// omitted).
func roleOf(t *testing.T, db *database.DB, user string, connName ...string) string {
	t.Helper()
	name := "main"
	if len(connName) > 0 {
		name = connName[0]
	}
	conns, err := db.GetConnections()
	if err != nil {
		t.Fatal(err)
	}
	connID := ""
	for _, c := range conns {
		if c.Name == name {
			connID = c.ID
		}
	}
	role, err := db.GetUserRole(user, connID)
	if err != nil {
		t.Fatalf("GetUserRole(%s): %v", user, err)
	}
	return role
}

func lastResetAudit(t *testing.T, db *database.DB) *database.AuditLog {
	t.Helper()
	logs, err := db.GetAuditLogs(50)
	if err != nil {
		t.Fatalf("GetAuditLogs: %v", err)
	}
	for i := range logs {
		if logs[i].Action == "user_role.reset_admin" {
			return &logs[i]
		}
	}
	return nil
}

func TestResetAdminNoFlagRevokesAdminsKeepsOtherRoles(t *testing.T) {
	path := newResetAdminDB(t, map[string]string{"alice": "admin", "sso:bob@example.com": "admin", "carol": "analyst"})
	var out bytes.Buffer
	if err := runResetAdmin(&out, path, "", ""); err != nil {
		t.Fatalf("runResetAdmin: %v", err)
	}
	if !strings.Contains(out.String(), "Removed CH-UI admin from: alice on main, sso:bob@example.com") {
		t.Fatalf("unexpected output: %q", out.String())
	}
	db := reopen(t, path)
	if r := roleOf(t, db, "alice"); r != "" {
		t.Fatalf("alice role = %q, want none", r)
	}
	if r := roleOf(t, db, "sso:bob@example.com"); r != "" {
		t.Fatalf("bob role = %q, want none", r)
	}
	if r := roleOf(t, db, "carol"); r != "analyst" {
		t.Fatalf("carol role = %q, want analyst", r)
	}
	entry := lastResetAudit(t, db)
	if entry == nil || entry.Username == nil || *entry.Username != "cli:reset-admin" || entry.Details == nil || !strings.Contains(*entry.Details, "alice") {
		t.Fatalf("missing or wrong audit entry: %+v", entry)
	}
}

func TestResetAdminNoFlagWithoutAdmins(t *testing.T) {
	path := newResetAdminDB(t, map[string]string{"carol": "viewer"})
	var out bytes.Buffer
	if err := runResetAdmin(&out, path, "", ""); err != nil {
		t.Fatalf("runResetAdmin: %v", err)
	}
	if !strings.HasPrefix(out.String(), "No CH-UI admin was assigned.") {
		t.Fatalf("unexpected output: %q", out.String())
	}
	db := reopen(t, path)
	if r := roleOf(t, db, "carol"); r != "viewer" {
		t.Fatalf("carol role = %q, want viewer", r)
	}
	if entry := lastResetAudit(t, db); entry != nil {
		t.Fatalf("no-op reset wrote an audit entry: %+v", entry)
	}
}

func TestResetAdminUserGrantsAdminLeavesOthers(t *testing.T) {
	path := newResetAdminDB(t, map[string]string{"alice": "admin", "dave": "viewer"})
	var out bytes.Buffer
	if err := runResetAdmin(&out, path, "  dave  ", ""); err != nil {
		t.Fatalf("runResetAdmin: %v", err)
	}
	if out.String() != "dave is now a CH-UI admin on: main, second\n" {
		t.Fatalf("unexpected output: %q", out.String())
	}
	db := reopen(t, path)
	for _, conn := range []string{"main", "second"} {
		if r := roleOf(t, db, "dave", conn); r != "admin" {
			t.Fatalf("dave role on %s = %q, want admin", conn, r)
		}
	}
	if r := roleOf(t, db, "dave", "from-login"); r != "" {
		t.Fatalf("dave role on the login page connection = %q, want none", r)
	}
	if r := roleOf(t, db, "alice"); r != "admin" {
		t.Fatalf("alice role = %q, want admin kept", r)
	}
	entry := lastResetAudit(t, db)
	if entry == nil || entry.Details == nil || !strings.HasPrefix(*entry.Details, "Granted admin to dave on connection ") {
		t.Fatalf("missing or wrong audit entry: %+v", entry)
	}
}

func TestResetAdminUserOnOneConnection(t *testing.T) {
	path := newResetAdminDB(t, nil)
	var out bytes.Buffer
	if err := runResetAdmin(&out, path, "dave", "second"); err != nil {
		t.Fatalf("runResetAdmin: %v", err)
	}
	db := reopen(t, path)
	if r := roleOf(t, db, "dave", "second"); r != "admin" {
		t.Fatalf("dave role on second = %q, want admin", r)
	}
	if r := roleOf(t, db, "dave", "main"); r != "" {
		t.Fatalf("dave role on main = %q, want none", r)
	}

	err := runResetAdmin(&bytes.Buffer{}, path, "dave", "from-login")
	if err == nil || !strings.Contains(err.Error(), "login page") {
		t.Fatalf("login page connection: err = %v, want refusal", err)
	}
	if err := runResetAdmin(&bytes.Buffer{}, path, "dave", "nope"); err == nil {
		t.Fatal("unknown connection accepted")
	}
}

func TestResetAdminSSOUserIsGlobal(t *testing.T) {
	path := newResetAdminDB(t, nil)
	var out bytes.Buffer
	if err := runResetAdmin(&out, path, "sso:erin@example.com", ""); err != nil {
		t.Fatalf("runResetAdmin: %v", err)
	}
	if out.String() != "sso:erin@example.com is now a CH-UI admin.\n" {
		t.Fatalf("unexpected output: %q", out.String())
	}
	db := reopen(t, path)
	if r := roleOf(t, db, "sso:erin@example.com", "second"); r != "admin" {
		t.Fatalf("erin role = %q, want admin", r)
	}
}

func TestResetAdminMissingDatabaseIsNotCreated(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.db")
	err := runResetAdmin(&bytes.Buffer{}, path, "", "")
	if err == nil || !strings.Contains(err.Error(), "database not found") {
		t.Fatalf("err = %v, want database not found", err)
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatalf("reset-admin created the missing database file (stat err %v)", statErr)
	}
}
