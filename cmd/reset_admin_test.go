package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caioricciuti/ch-ui/internal/database"
)

// newResetAdminDB creates a migrated database file with the given roles and
// closes it, so runResetAdmin opens it the way the CLI does.
func newResetAdminDB(t *testing.T, roles map[string]string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ch-ui.db")
	db, err := database.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	for user, role := range roles {
		if err := db.SetUserRole(user, role); err != nil {
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

func roleOf(t *testing.T, db *database.DB, user string) string {
	t.Helper()
	role, err := db.GetUserRole(user)
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
	if err := runResetAdmin(&out, path, ""); err != nil {
		t.Fatalf("runResetAdmin: %v", err)
	}
	if !strings.Contains(out.String(), "Removed CH-UI admin from: alice, sso:bob@example.com") {
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
	if err := runResetAdmin(&out, path, ""); err != nil {
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
	if err := runResetAdmin(&out, path, "  dave  "); err != nil {
		t.Fatalf("runResetAdmin: %v", err)
	}
	if out.String() != "dave is now a CH-UI admin.\n" {
		t.Fatalf("unexpected output: %q", out.String())
	}
	db := reopen(t, path)
	if r := roleOf(t, db, "dave"); r != "admin" {
		t.Fatalf("dave role = %q, want admin", r)
	}
	if r := roleOf(t, db, "alice"); r != "admin" {
		t.Fatalf("alice role = %q, want admin kept", r)
	}
	entry := lastResetAudit(t, db)
	if entry == nil || entry.Details == nil || *entry.Details != "Granted admin to dave from the command line" {
		t.Fatalf("missing or wrong audit entry: %+v", entry)
	}
}

func TestResetAdminMissingDatabaseIsNotCreated(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.db")
	err := runResetAdmin(&bytes.Buffer{}, path, "")
	if err == nil || !strings.Contains(err.Error(), "database not found") {
		t.Fatalf("err = %v, want database not found", err)
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatalf("reset-admin created the missing database file (stat err %v)", statErr)
	}
}
