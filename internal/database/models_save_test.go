package database

import (
	"errors"
	"strings"
	"testing"
)

func TestSaveModelRenameRewritesPipelines(t *testing.T) {
	db := openTestDB(t)
	connA, err := db.CreateConnection(CreateConnectionParams{Name: "a", TunnelToken: "a"})
	if err != nil {
		t.Fatal(err)
	}
	connB, err := db.CreateConnection(CreateConnectionParams{Name: "b", TunnelToken: "b"})
	if err != nil {
		t.Fatal(err)
	}
	id, err := db.SaveModel(ModelSave{Model: Model{ConnectionID: connA, Name: "orders", TargetDatabase: "default", Materialization: "view", SQLBody: "SELECT 1"}})
	if err != nil {
		t.Fatal(err)
	}
	mine, _ := db.CreateModelPipeline(ModelPipeline{ConnectionID: connA, Name: "p", Selector: "+orders+"})
	theirs, _ := db.CreateModelPipeline(ModelPipeline{ConnectionID: connB, Name: "p", Selector: "orders"})

	// The rename function is the caller's; this one only swaps exact tokens.
	var calls int
	rename := func(sel, oldName, newName string) (string, bool) {
		calls++
		if oldName != "orders" || newName != "sales" {
			t.Errorf("rename got %q -> %q", oldName, newName)
		}
		return strings.ReplaceAll(sel, oldName, newName), strings.Contains(sel, oldName)
	}

	m, _ := db.GetModelByID(id)
	m.SQLBody = "SELECT 2"
	if _, err := db.SaveModel(ModelSave{Model: *m, RenameInSelector: rename}); err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatalf("no rename, but the rewrite ran %d times", calls)
	}

	m.Name, m.UniqueKey = "sales", "id"
	if _, err := db.SaveModel(ModelSave{Model: *m, RenameInSelector: rename}); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("only the model's connection's pipelines are rewritten, got %d calls", calls)
	}
	if p, _ := db.GetModelPipelineByID(mine); p.Selector != "+sales+" {
		t.Fatalf("selector not rewritten: %q", p.Selector)
	}
	if p, _ := db.GetModelPipelineByID(theirs); p.Selector != "orders" {
		t.Fatalf("another connection's pipeline was rewritten: %q", p.Selector)
	}
	if got, _ := db.GetModelByID(id); got.Name != "sales" || got.SQLBody != "SELECT 2" || got.UniqueKey != "id" || got.Source != "manual" {
		t.Fatalf("model not saved: %+v", got)
	}

	// A failed rename (name taken) leaves the selectors alone.
	if _, err := db.SaveModel(ModelSave{Model: Model{ConnectionID: connA, Name: "taken", TargetDatabase: "default", Materialization: "view"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SaveModel(ModelSave{Model: Model{ConnectionID: connA, Name: "taken", TargetDatabase: "default", Materialization: "view"}}); !errors.Is(err, ErrModelNameTaken) {
		t.Fatalf("creating a second model with a taken name: want ErrModelNameTaken, got %v", err)
	}
	m.Name = "taken"
	if _, err := db.SaveModel(ModelSave{Model: *m, RenameInSelector: rename}); !errors.Is(err, ErrModelNameTaken) {
		t.Fatalf("renaming onto an existing name: want ErrModelNameTaken, got %v", err)
	}
	if p, _ := db.GetModelPipelineByID(mine); p.Selector != "+sales+" {
		t.Fatalf("a failed rename changed the selector: %q", p.Selector)
	}
}

func TestSaveModelIsAtomic(t *testing.T) {
	db := openTestDB(t)
	conn, err := db.CreateConnection(CreateConnectionParams{Name: "c", TunnelToken: "c"})
	if err != nil {
		t.Fatal(err)
	}
	mustExec(t, db, `CREATE TRIGGER fail_tests BEFORE INSERT ON model_tests BEGIN SELECT RAISE(ABORT, 'injected'); END`)

	tests := []ModelTest{{Kind: "not_null", ColumnName: "id", Severity: "error"}}
	m := Model{ConnectionID: conn, Name: "m", TargetDatabase: "default", Materialization: "table", SQLBody: "SELECT 1", PartitionBy: "id", Source: "github"}
	if _, err := db.SaveModel(ModelSave{Model: m, ReplaceTests: true, Tests: tests}); err == nil || !strings.Contains(err.Error(), "injected") {
		t.Fatalf("want the injected failure, got %v", err)
	}
	if got, _ := db.GetModelByName(conn, "m"); got != nil {
		t.Fatalf("a failed create left a model behind: %+v", got)
	}

	mustExec(t, db, `DROP TRIGGER fail_tests`)
	id, err := db.SaveModel(ModelSave{Model: m, ReplaceTests: true, Tests: tests})
	if err != nil {
		t.Fatal(err)
	}
	mustExec(t, db, `CREATE TRIGGER fail_tests BEFORE INSERT ON model_tests BEGIN SELECT RAISE(ABORT, 'injected'); END`)
	m.ID, m.SQLBody, m.PartitionBy = id, "SELECT 2", ""
	if _, err := db.SaveModel(ModelSave{Model: m, ReplaceTests: true, Tests: tests}); err == nil {
		t.Fatal("want the injected failure on update")
	}
	got, _ := db.GetModelByID(id)
	if got == nil || got.SQLBody != "SELECT 1" || got.PartitionBy != "id" || got.Source != "github" {
		t.Fatalf("a failed update changed the model: %+v", got)
	}
	if ts, _ := db.GetModelTests(id); len(ts) != 1 {
		t.Fatalf("a failed update changed the tests: %+v", ts)
	}
}
