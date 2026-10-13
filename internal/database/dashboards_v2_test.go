package database

import "testing"

func TestDashboardsV2MigrationIsIdempotent(t *testing.T) {
	db := openTestDB(t)
	for i := 0; i < 2; i++ {
		if err := db.migrateDashboardsV2(); err != nil {
			t.Fatalf("run %d: %v", i+1, err)
		}
	}
	id, err := db.CreateDashboard("D", "", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if v, err := db.GetDashboardVariables(id); err != nil || v != "[]" {
		t.Fatalf("new dashboards have no variables, got %q %v", v, err)
	}
}

func TestRecordDashboardVersionMergesLayoutOnlyChanges(t *testing.T) {
	db := openTestDB(t)
	id, _ := db.CreateDashboard("D", "", "alice")
	panelID, _ := db.CreatePanel(id, "P", "", "table", "SELECT 1", "", "{}", 0, 0, 6, 4)

	v1, err := db.RecordDashboardVersion(id, "alice", "Created")
	if err != nil || v1 != 1 {
		t.Fatalf("first version: %d %v", v1, err)
	}
	// Same content: nothing written.
	if v, _ := db.RecordDashboardVersion(id, "alice", ""); v != 0 {
		t.Fatalf("unchanged dashboard wrote version %d", v)
	}
	// Layout only, within 30 s: the latest version is updated in place.
	_ = db.UpdatePanel(panelID, "P", "", "table", "SELECT 1", "", "{}", 6, 0, 6, 4)
	if v, _ := db.RecordDashboardVersion(id, "alice", ""); v != 1 {
		t.Fatalf("layout-only change should update version 1, got %d", v)
	}
	// A material change (query) always adds a version.
	_ = db.UpdatePanel(panelID, "P", "", "table", "SELECT 2", "", "{}", 6, 0, 6, 4)
	if v, _ := db.RecordDashboardVersion(id, "bob", ""); v != 2 {
		t.Fatalf("query change should add version 2, got %d", v)
	}
	list, err := db.ListDashboardVersions(id)
	if err != nil || len(list) != 2 || list[0].Version != 2 || *list[0].CreatedBy != "bob" || list[1].PanelCount != 1 {
		t.Fatalf("versions: %+v %v", list, err)
	}
	got, err := db.GetDashboardVersion(id, 1)
	if err != nil || got == nil || len(got.Snapshot) == 0 {
		t.Fatalf("get version: %+v %v", got, err)
	}
}

func TestRecordDashboardVersionKeepsLatest50(t *testing.T) {
	db := openTestDB(t)
	id, _ := db.CreateDashboard("D", "", "alice")
	for i := 0; i < 55; i++ {
		if err := db.UpdateDashboard(id, "D", string(rune('a'+i%26))+string(rune('a'+i/26))); err != nil {
			t.Fatal(err)
		}
		if _, err := db.RecordDashboardVersion(id, "alice", ""); err != nil {
			t.Fatal(err)
		}
	}
	list, _ := db.ListDashboardVersions(id)
	if len(list) != 50 || list[0].Version != 55 || list[49].Version != 6 {
		t.Fatalf("kept %d versions, newest %d oldest %d", len(list), list[0].Version, list[len(list)-1].Version)
	}
}

func TestCreateDashboardWithContentIsOneTransaction(t *testing.T) {
	db := openTestDB(t)
	vars := `[{"name":"db","type":"text"}]`
	desc := "d"
	id, err := db.CreateDashboardWithContent(DashboardContent{
		Name: "T", Description: &desc, Variables: &vars,
		Panels: []PanelInput{{Name: "A", PanelType: "table", Query: "SELECT 1", W: 6, H: 4}},
	}, "alice", "", []string{"x"})
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := db.GetDashboardVariables(id); v != vars {
		t.Fatalf("variables: %s", v)
	}
	if p, _ := db.GetPanelsByDashboard(id); len(p) != 1 {
		t.Fatalf("panels: %d", len(p))
	}

	// A panel that violates a constraint rolls back the dashboard row too.
	before, _ := db.GetDashboards("")
	_, err = db.CreateDashboardWithContent(DashboardContent{
		Name:   "Broken",
		Panels: []PanelInput{{Name: "A", PanelType: "table", Query: "SELECT 1", ConnectionID: "no-such-connection", W: 6, H: 4}},
	}, "alice", "", nil)
	if err == nil {
		t.Fatal("expected a foreign key error")
	}
	after, _ := db.GetDashboards("")
	if len(after) != len(before) {
		t.Fatalf("a failed create left a dashboard behind")
	}
}
