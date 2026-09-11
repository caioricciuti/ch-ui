package database

import (
	"database/sql"
	"testing"
)

// TestDashboardFoldersAndStars covers the organization layer: nested folders,
// unique sibling names, cycle protection on move, re-parenting on delete,
// tags round-tripping and per-user stars.
func TestDashboardFoldersAndStars(t *testing.T) {
	db := openTestDB(t)

	ops, err := db.CreateDashboardFolder("Ops", "", "admin")
	if err != nil {
		t.Fatalf("CreateDashboardFolder: %v", err)
	}
	cluster, err := db.CreateDashboardFolder("Cluster", ops, "admin")
	if err != nil {
		t.Fatalf("CreateDashboardFolder nested: %v", err)
	}
	if _, err := db.CreateDashboardFolder("Ops", "", "admin"); err != ErrFolderNameTaken {
		t.Fatalf("duplicate sibling name: want ErrFolderNameTaken, got %v", err)
	}
	if _, err := db.CreateDashboardFolder("Ops", cluster, "admin"); err != nil {
		t.Fatalf("same name under another parent should be fine: %v", err)
	}

	// Moving Ops under its own child must be refused.
	child := cluster
	if err := db.UpdateDashboardFolder(ops, nil, &child); err != ErrFolderCycle {
		t.Fatalf("cycle move: want ErrFolderCycle, got %v", err)
	}

	dashID, err := db.CreateDashboard("Replication", "", "admin")
	if err != nil {
		t.Fatalf("CreateDashboard: %v", err)
	}
	if err := db.SetDashboardFolder(dashID, cluster); err != nil {
		t.Fatalf("SetDashboardFolder: %v", err)
	}
	if err := db.SetDashboardTags(dashID, []string{" ops ", "Ops", "kafka", ""}); err != nil {
		t.Fatalf("SetDashboardTags: %v", err)
	}
	if err := db.StarDashboard(dashID, "caio"); err != nil {
		t.Fatalf("StarDashboard: %v", err)
	}

	forCaio, err := db.GetDashboards("caio")
	if err != nil {
		t.Fatalf("GetDashboards: %v", err)
	}
	var got *Dashboard
	for i := range forCaio {
		if forCaio[i].ID == dashID {
			got = &forCaio[i]
		}
	}
	if got == nil {
		t.Fatalf("dashboard missing from list")
	}
	if got.FolderID == nil || *got.FolderID != cluster {
		t.Fatalf("folder_id: want %s, got %v", cluster, got.FolderID)
	}
	if len(got.Tags) != 2 || got.Tags[0] != "ops" || got.Tags[1] != "kafka" {
		t.Fatalf("tags not trimmed/deduped: %v", got.Tags)
	}
	if !got.Starred {
		t.Fatalf("expected starred for caio")
	}
	forOther, _ := db.GetDashboardByIDFor(dashID, "someone")
	if forOther == nil || forOther.Starred {
		t.Fatalf("stars must be per user")
	}

	// Deleting Cluster re-parents its dashboard and subfolder to Ops.
	if err := db.DeleteDashboardFolder(cluster); err != nil {
		t.Fatalf("DeleteDashboardFolder: %v", err)
	}
	moved, _ := db.GetDashboardByID(dashID)
	if moved.FolderID == nil || *moved.FolderID != ops {
		t.Fatalf("dashboard should move up to Ops, got %v", moved.FolderID)
	}
	folders, _ := db.ListDashboardFolders()
	for _, f := range folders {
		if f.Name == "Ops" && f.ParentID != nil && *f.ParentID == cluster {
			t.Fatalf("subfolder still points at the deleted folder")
		}
	}
	if err := db.DeleteDashboardFolder(cluster); err != sql.ErrNoRows {
		t.Fatalf("second delete: want ErrNoRows, got %v", err)
	}
	if err := db.UnstarDashboard(dashID, "caio"); err != nil {
		t.Fatalf("UnstarDashboard: %v", err)
	}
	again, _ := db.GetDashboardByIDFor(dashID, "caio")
	if again.Starred {
		t.Fatalf("expected unstarred")
	}
}
