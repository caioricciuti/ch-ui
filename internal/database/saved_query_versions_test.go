package database

import (
	"database/sql"
	"fmt"
	"testing"
)

func TestMigrateSavedQueriesV2IdempotentAndLegacyRowsShared(t *testing.T) {
	db := openTestDB(t)
	// Simulate a database from before v2: drop the plain columns, insert a
	// row the old way, then migrate again (twice).
	for _, stmt := range []string{
		"ALTER TABLE saved_queries DROP COLUMN visibility",
		"ALTER TABLE saved_queries DROP COLUMN tags",
		`INSERT INTO saved_queries (id, name, query, created_by, created_at, updated_at) VALUES ('legacy', 'Legacy', 'SELECT 1', 'bob', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
		`INSERT INTO saved_queries (id, name, query, created_at, updated_at) VALUES ('orphan', 'Orphan', 'SELECT 2', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
	} {
		if _, err := db.conn.Exec(stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	for i := 0; i < 2; i++ {
		if err := db.migrateSavedQueriesV2(); err != nil {
			t.Fatalf("migrate run %d: %v", i+1, err)
		}
	}
	for _, id := range []string{"legacy", "orphan"} {
		q, err := db.GetSavedQueryVisibleTo(id, SavedQueryViewer{Actor: "carol", Role: "viewer"})
		if err != nil || q == nil {
			t.Fatalf("%s should stay visible to everyone: %v %v", id, q, err)
		}
		if q.Visibility != SavedQueryShared || len(q.Tags) != 0 || q.FolderID != nil {
			t.Fatalf("%s: got visibility %q tags %v folder %v", id, q.Visibility, q.Tags, q.FolderID)
		}
	}
	orphan, _ := db.GetSavedQueryByID("orphan")
	if !CanEditSavedQuery(orphan, SavedQueryViewer{Actor: "carol", Role: "analyst"}) {
		t.Fatal("a legacy row without owner must stay editable by any analyst")
	}
	if CanEditSavedQuery(orphan, SavedQueryViewer{Actor: "carol", Role: "viewer"}) {
		t.Fatal("viewers never edit")
	}
	if n, _ := db.ListSavedQueryVersions("legacy"); len(n) != 0 {
		t.Fatal("no version backfill expected")
	}
}

func TestSavedQueryVisibilityRule(t *testing.T) {
	owner := "alice"
	for _, tc := range []struct {
		visibility, actor, role string
		allOwners, want         bool
	}{
		{SavedQueryShared, "bob", "viewer", false, true},
		{SavedQueryPrivate, "bob", "viewer", false, false},
		{SavedQueryPrivate, "bob", "admin", false, false},
		{SavedQueryPrivate, "bob", "admin", true, true},
		{SavedQueryPrivate, "bob", "analyst", true, false},
		{SavedQueryPrivate, owner, "viewer", false, true},
		{SavedQueryAdmins, "bob", "analyst", false, false},
		{SavedQueryAdmins, "bob", "admin", false, true},
		{SavedQueryAdmins, owner, "analyst", false, true},
	} {
		q := &SavedQuery{CreatedBy: &owner, Visibility: tc.visibility}
		got := SavedQueryVisibleTo(q, SavedQueryViewer{Actor: tc.actor, Role: tc.role, AllOwners: tc.allOwners})
		if got != tc.want {
			t.Errorf("%+v: got %v", tc, got)
		}
	}
}

func TestSavedQueryVersionsRecordSkipCapRestore(t *testing.T) {
	db := openTestDB(t)
	id, err := db.CreateSavedQuery(CreateSavedQueryParams{Name: "Q", Query: "SELECT 1", CreatedBy: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	if v, err := db.RecordSavedQueryVersion(id, "alice", "Created"); err != nil || v != 1 {
		t.Fatalf("first version: %d %v", v, err)
	}
	if v, _ := db.RecordSavedQueryVersion(id, "alice", ""); v != 0 {
		t.Fatalf("identical save must be skipped, got version %d", v)
	}
	// Baseline is a no-op once history exists.
	if err := db.EnsureSavedQueryBaseline(id); err != nil {
		t.Fatal(err)
	}
	for i := 2; i <= 55; i++ {
		if err := db.UpdateSavedQuery(id, UpdateSavedQueryParams{Name: "Q", Query: fmt.Sprintf("SELECT %d", i)}); err != nil {
			t.Fatal(err)
		}
		if v, err := db.RecordSavedQueryVersion(id, "alice", ""); err != nil || v != i {
			t.Fatalf("version %d: got %d %v", i, v, err)
		}
	}
	list, err := db.ListSavedQueryVersions(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 50 || list[0].Version != 55 || list[49].Version != 6 {
		t.Fatalf("want versions 55..6, got %d rows from %d to %d", len(list), list[0].Version, list[len(list)-1].Version)
	}
	if list[0].Snapshot != nil {
		t.Fatal("list must not carry snapshots")
	}

	v, err := db.RestoreSavedQueryVersion(id, 10, "bob")
	if err != nil || v != 56 {
		t.Fatalf("restore: %d %v", v, err)
	}
	q, _ := db.GetSavedQueryByID(id)
	if q.Query != "SELECT 10" {
		t.Fatalf("restored query = %q", q.Query)
	}
	got, _ := db.GetSavedQueryVersion(id, 56)
	if got == nil || got.Note != "Restored version 10" || got.Snapshot.Query != "SELECT 10" {
		t.Fatalf("restore version: %+v", got)
	}
	if _, err := db.RestoreSavedQueryVersion(id, 1, "bob"); err != sql.ErrNoRows {
		t.Fatalf("pruned version: want ErrNoRows, got %v", err)
	}
}

func TestSavedQueryBaselineForLegacyQuery(t *testing.T) {
	db := openTestDB(t)
	id, _ := db.CreateSavedQuery(CreateSavedQueryParams{Name: "Old", Query: "SELECT 1"})
	if err := db.EnsureSavedQueryBaseline(id); err != nil {
		t.Fatal(err)
	}
	list, _ := db.ListSavedQueryVersions(id)
	if len(list) != 1 || list[0].Note != "Before first recorded change" || list[0].CreatedBy != nil {
		t.Fatalf("baseline: %+v", list)
	}
}

func TestSavedQueryFolders(t *testing.T) {
	db := openTestDB(t)
	top, err := db.CreateSavedQueryFolder("Finance", "", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateSavedQueryFolder("Finance", "", "bob"); err != ErrFolderNameTaken {
		t.Fatalf("duplicate root name: want ErrFolderNameTaken, got %v", err)
	}
	child, err := db.CreateSavedQueryFolder("Monthly", top, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateSavedQueryFolder("Monthly", "", "alice"); err != nil {
		t.Fatalf("same name under another parent is fine: %v", err)
	}
	if err := db.UpdateSavedQueryFolder(top, nil, &child); err != ErrFolderCycle {
		t.Fatalf("move under own child: want ErrFolderCycle, got %v", err)
	}

	inTop, _ := db.CreateSavedQuery(CreateSavedQueryParams{Name: "A", Query: "SELECT 1", FolderID: top})
	inChild, _ := db.CreateSavedQuery(CreateSavedQueryParams{Name: "B", Query: "SELECT 2", FolderID: child})
	outside, _ := db.CreateSavedQuery(CreateSavedQueryParams{Name: "C", Query: "SELECT 3"})

	moved, err := db.DeleteSavedQueryFolder(top)
	if err != nil || moved != 2 {
		t.Fatalf("delete folder: moved %d, %v", moved, err)
	}
	for _, id := range []string{inTop, inChild, outside} {
		q, err := db.GetSavedQueryByID(id)
		if err != nil || q == nil {
			t.Fatalf("query %s must survive folder delete: %v", id, err)
		}
		if q.FolderID != nil {
			t.Fatalf("query %s should be at the root, folder %v", id, *q.FolderID)
		}
	}
	if f, _ := db.GetSavedQueryFolder(child); f != nil {
		t.Fatal("child folder should cascade")
	}
	if _, err := db.DeleteSavedQueryFolder("missing"); err != sql.ErrNoRows {
		t.Fatalf("missing folder: %v", err)
	}
}

func TestSavedQueryVisibilityClaimsLegacyOwner(t *testing.T) {
	db := openTestDB(t)
	id, _ := db.CreateSavedQuery(CreateSavedQueryParams{Name: "Old", Query: "SELECT 1"})
	if err := db.SetSavedQueryVisibility(id, SavedQueryPrivate, "carol"); err != nil {
		t.Fatal(err)
	}
	q, _ := db.GetSavedQueryVisibleTo(id, SavedQueryViewer{Actor: "carol", Role: "analyst"})
	if q == nil || q.Owner != "carol" || q.Visibility != SavedQueryPrivate {
		t.Fatalf("legacy row made private should belong to carol: %+v", q)
	}
	if err := db.SetSavedQueryVisibility(id, "public", ""); err == nil {
		t.Fatal("unknown visibility must fail")
	}
}
