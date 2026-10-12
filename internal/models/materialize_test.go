package models

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

func TestValidateMaterialization(t *testing.T) {
	for _, ok := range []string{"view", "table"} {
		if err := ValidateMaterialization(ok); err != nil {
			t.Errorf("%s: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "incremental", "materialized_view", "TABLE"} {
		if err := ValidateMaterialization(bad); err == nil {
			t.Errorf("%q must be rejected", bad)
		}
	}
}

func TestCreateTableSQLOrderBy(t *testing.T) {
	cases := []struct{ engine, orderBy, want string }{
		{"MergeTree", "", "CREATE TABLE `db`.`m` ENGINE = MergeTree ORDER BY tuple() AS SELECT 1"},
		{"", "", "CREATE TABLE `db`.`m` ENGINE = MergeTree ORDER BY tuple() AS SELECT 1"},
		{"ReplacingMergeTree", "id", "CREATE TABLE `db`.`m` ENGINE = ReplacingMergeTree ORDER BY id AS SELECT 1"},
		{"Memory", "tuple()", "CREATE TABLE `db`.`m` ENGINE = Memory AS SELECT 1"},
		{"Log", "", "CREATE TABLE `db`.`m` ENGINE = Log AS SELECT 1"},
	}
	for _, c := range cases {
		if got := createTableSQL("db", "m", c.engine, c.orderBy, "SELECT 1;\n"); got != c.want {
			t.Errorf("engine %q order %q:\n got %s\nwant %s", c.engine, c.orderBy, got, c.want)
		}
	}
}

// fakeCH answers the catalog queries from a map and records every statement.
type fakeCH struct {
	objects  []chObject
	dbEngine string
	failOn   string // statements starting with this prefix fail
	failErr  error  // error returned for failOn; "boom" when nil
	stmts    []string
}

func (f *fakeCH) exec(sql string) (*tunnel.QueryResult, error) {
	f.stmts = append(f.stmts, sql)
	switch {
	case strings.HasPrefix(sql, "SELECT name, engine, comment FROM system.tables"):
		data, _ := json.Marshal(f.objects)
		return &tunnel.QueryResult{Data: data}, nil
	case strings.HasPrefix(sql, "SELECT engine FROM system.databases"):
		data, _ := json.Marshal([]map[string]string{{"engine": f.dbEngine}})
		return &tunnel.QueryResult{Data: data}, nil
	case f.failOn != "" && strings.HasPrefix(sql, f.failOn):
		if f.failErr != nil {
			return nil, f.failErr
		}
		return nil, errors.New("boom")
	}
	return &tunnel.QueryResult{Data: json.RawMessage(`[]`)}, nil
}

// ddl drops the catalog reads so tests compare only what changed ClickHouse.
func (f *fakeCH) ddl() []string {
	var out []string
	for _, s := range f.stmts {
		if !strings.HasPrefix(s, "SELECT ") {
			out = append(out, s)
		}
	}
	return out
}

func newMZ(f *fakeCH, mat string, prior bool) *materializer {
	return &materializer{
		exec:         f.exec,
		model:        database.Model{ID: "m1", Name: "orders", TargetDatabase: "db", Materialization: mat, TableEngine: "MergeTree"},
		priorSuccess: func() (bool, error) { return prior, nil },
	}
}

func assertStmts(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("statements:\n got %q\nwant %q", got, want)
	}
	for i := range want {
		if !strings.HasPrefix(got[i], want[i]) {
			t.Fatalf("statement %d:\n got %s\nwant prefix %s\nall: %q", i, got[i], want[i], got)
		}
	}
}

const mark = "ALTER TABLE `db`.`orders` MODIFY COMMENT 'ch-ui model m1'"

func TestMaterializeTableFirstRun(t *testing.T) {
	f := &fakeCH{dbEngine: "Atomic"}
	if _, err := newMZ(f, "table", false).run("SELECT 1"); err != nil {
		t.Fatal(err)
	}
	assertStmts(t, f.ddl(), []string{"CREATE TABLE `db`.`orders` ENGINE = MergeTree ORDER BY tuple() AS SELECT 1", mark})
}

func TestMaterializeTableRebuildAtomic(t *testing.T) {
	f := &fakeCH{dbEngine: "Atomic", objects: []chObject{{Name: "orders", Engine: "MergeTree", Comment: "ch-ui model m1"}}}
	if _, err := newMZ(f, "table", false).run("SELECT 1"); err != nil {
		t.Fatal(err)
	}
	assertStmts(t, f.ddl(), []string{
		"CREATE TABLE `db`.`orders__chui_tmp`",
		"ALTER TABLE `db`.`orders__chui_tmp` MODIFY COMMENT 'ch-ui model m1'",
		"EXCHANGE TABLES `db`.`orders__chui_tmp` AND `db`.`orders`",
		"DROP TABLE IF EXISTS `db`.`orders__chui_tmp`",
	})
}

func TestMaterializeTableRebuildRenameFallback(t *testing.T) {
	f := &fakeCH{dbEngine: "Ordinary", objects: []chObject{{Name: "orders", Engine: "MergeTree", Comment: "ch-ui model m1"}}}
	if _, err := newMZ(f, "table", false).run("SELECT 1"); err != nil {
		t.Fatal(err)
	}
	assertStmts(t, f.ddl(), []string{
		"CREATE TABLE `db`.`orders__chui_tmp`",
		"ALTER TABLE `db`.`orders__chui_tmp`",
		"RENAME TABLE `db`.`orders` TO `db`.`orders__chui_old`",
		"RENAME TABLE `db`.`orders__chui_tmp` TO `db`.`orders`",
		"DROP TABLE IF EXISTS `db`.`orders__chui_old`",
	})
}

func TestMaterializeTableFailedBuildKeepsOldTable(t *testing.T) {
	f := &fakeCH{dbEngine: "Atomic", failOn: "CREATE TABLE", objects: []chObject{{Name: "orders", Engine: "MergeTree", Comment: "ch-ui model m1"}}}
	if _, err := newMZ(f, "table", false).run("SELECT broken"); err == nil {
		t.Fatal("expected the failed CREATE to fail the build")
	}
	assertStmts(t, f.ddl(), []string{"CREATE TABLE `db`.`orders__chui_tmp`", "DROP TABLE IF EXISTS `db`.`orders__chui_tmp`"})
}

func TestMaterializeRefusesForeignObjects(t *testing.T) {
	cases := []struct {
		name  string
		mat   string
		prior bool
		obj   chObject
	}{
		{"hand-made table, no runs", "table", false, chObject{Name: "orders", Engine: "MergeTree"}},
		{"hand-made table with a description", "table", true, chObject{Name: "orders", Engine: "MergeTree", Comment: "sales data"}},
		{"another model's table", "table", true, chObject{Name: "orders", Engine: "MergeTree", Comment: "ch-ui model other"}},
		{"unmarked view for a table model", "table", true, chObject{Name: "orders", Engine: "View"}},
		{"unmarked table for a view model", "view", true, chObject{Name: "orders", Engine: "MergeTree"}},
		{"hand-made view, no runs", "view", false, chObject{Name: "orders", Engine: "View"}},
		{"foreign leftover tmp", "table", false, chObject{Name: "orders__chui_tmp", Engine: "MergeTree"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := &fakeCH{dbEngine: "Atomic", objects: []chObject{c.obj}}
			_, err := newMZ(f, c.mat, c.prior).run("SELECT 1")
			if err == nil || !strings.Contains(err.Error(), "was not created by this model") {
				t.Fatalf("want ownership refusal, got %v", err)
			}
			if ddl := f.ddl(); len(ddl) != 0 {
				t.Fatalf("refusal must not touch ClickHouse, ran %q", ddl)
			}
		})
	}
}

func TestMaterializeAdoptsLegacyObjects(t *testing.T) {
	f := &fakeCH{dbEngine: "Atomic", objects: []chObject{{Name: "orders", Engine: "MergeTree"}}}
	if _, err := newMZ(f, "table", true).run("SELECT 1"); err != nil {
		t.Fatal(err)
	}
	f = &fakeCH{dbEngine: "Atomic", objects: []chObject{{Name: "orders", Engine: "View"}}}
	if _, err := newMZ(f, "view", true).run("SELECT 1"); err != nil {
		t.Fatal(err)
	}
	assertStmts(t, f.ddl(), []string{"CREATE OR REPLACE VIEW `db`.`orders` AS SELECT 1", mark})
}

func TestMaterializeViewKindChangeSwaps(t *testing.T) {
	f := &fakeCH{dbEngine: "Atomic", objects: []chObject{{Name: "orders", Engine: "MergeTree", Comment: "ch-ui model m1"}}}
	if _, err := newMZ(f, "view", false).run("SELECT 1"); err != nil {
		t.Fatal(err)
	}
	assertStmts(t, f.ddl(), []string{
		"CREATE VIEW `db`.`orders__chui_tmp` AS SELECT 1",
		"ALTER TABLE `db`.`orders__chui_tmp`",
		"EXCHANGE TABLES",
		"DROP TABLE IF EXISTS `db`.`orders__chui_tmp`",
	})
}

func TestMaterializeRejectsUnknownMaterialization(t *testing.T) {
	f := &fakeCH{}
	if _, err := newMZ(f, "incremental", false).run("SELECT 1"); err == nil {
		t.Fatal("unknown materialization must fail")
	}
	if len(f.stmts) != 0 {
		t.Fatalf("nothing may run, ran %q", f.stmts)
	}
}

func TestMaterializeWithoutCommentPrivilegeStillRuns(t *testing.T) {
	denied := errors.New("Code: 497. DB::Exception: worker_model: Not enough privileges. To execute this query, it's necessary to have the grant ALTER MODIFY COMMENT. (ACCESS_DENIED)")
	f := &fakeCH{dbEngine: "Atomic", failOn: "ALTER TABLE", failErr: denied}
	if _, err := newMZ(f, "table", false).run("SELECT 1"); err != nil {
		t.Fatalf("a refused ownership comment must not fail the run: %v", err)
	}

	f = &fakeCH{dbEngine: "Atomic", failOn: "ALTER TABLE"}
	if _, err := newMZ(f, "table", false).run("SELECT 1"); err == nil {
		t.Fatal("other ALTER errors must still fail the run")
	}
}
