package models

import (
	"strings"
	"testing"

	"github.com/caioricciuti/ch-ui/internal/database"
)

func TestValidateExpression(t *testing.T) {
	for _, ok := range []string{"", "toYYYYMM(ts)", "(a, b)", "ver", "if(x = ';', 1, 2)", "`weird col`"} {
		if err := ValidateExpression("e", ok); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	for _, bad := range []string{"a; DROP TABLE t", "a -- )", "a /* x */", "f(a", "a)", "'open", ")("} {
		if err := ValidateExpression("e", bad); err == nil {
			t.Errorf("%q must be rejected", bad)
		}
	}
}

func TestNormalizeModel(t *testing.T) {
	m := database.Model{Materialization: "incremental", IncrementalStrategy: "delete_insert", UniqueKey: " id ,tenant ", WatermarkColumn: "ts"}
	if err := NormalizeModel(&m); err != nil {
		t.Fatal(err)
	}
	if m.UniqueKey != "id, tenant" || m.TableEngine != "MergeTree" || m.OrderBy != "tuple()" {
		t.Fatalf("normalized: %+v", m)
	}
	m = database.Model{Materialization: "incremental"}
	if err := NormalizeModel(&m); err != nil || m.IncrementalStrategy != StrategyAppend {
		t.Fatalf("incremental defaults to append: %+v %v", m, err)
	}
	for _, bad := range []database.Model{
		{Materialization: "incremental", IncrementalStrategy: "delete_insert"},
		{Materialization: "incremental", IncrementalStrategy: "merge"},
		{Materialization: "incremental", WatermarkColumn: "max(ts)"},
		{Materialization: "incremental", IncrementalStrategy: "delete_insert", UniqueKey: "id; x"},
		{Materialization: "table", TableEngine: "Memory", PartitionBy: "x"},
		{Materialization: "table", TableEngine: "ReplacingMergeTree(v)", EngineArgs: "v"},
		{Materialization: "table", PartitionBy: "x -- y"},
		{Materialization: "table", OrderBy: "id; DROP TABLE x"},
	} {
		b := bad
		if err := NormalizeModel(&b); err == nil {
			t.Errorf("%+v must be rejected", bad)
		}
	}
	// A view keeps the table options it does not use.
	v := database.Model{Materialization: "view", PartitionBy: "x", TableEngine: "Memory"}
	if err := NormalizeModel(&v); err != nil || v.PartitionBy != "x" {
		t.Fatalf("view: %+v %v", v, err)
	}
}

func TestTableSpecClause(t *testing.T) {
	cases := []struct {
		spec tableSpec
		want string
	}{
		{tableSpec{engine: "ReplacingMergeTree", engineArgs: "ver", partitionBy: "toYYYYMM(day)", orderBy: "id"}, "ENGINE = ReplacingMergeTree(ver) PARTITION BY toYYYYMM(day) ORDER BY id"},
		{tableSpec{engine: "Memory", partitionBy: "x"}, "ENGINE = Memory"},
		{tableSpec{}, "ENGINE = MergeTree ORDER BY tuple()"},
	}
	for _, c := range cases {
		if got := c.spec.clause(); got != c.want {
			t.Errorf("got %q want %q", got, c.want)
		}
	}
	if got := createTableFromSpec("db", "t", tableSpec{}, "SELECT 1", true); got != "CREATE TABLE `db`.`t` ENGINE = MergeTree ORDER BY tuple() EMPTY AS SELECT 1" {
		t.Errorf("empty: %s", got)
	}
}

func TestResolveThis(t *testing.T) {
	got, err := ResolveModelSQL("SELECT * FROM $ref(a) WHERE ts > (SELECT max(ts) FROM $this) -- $this stays\n AND s = '$this'", map[string]string{"a": "raw"}, "an", "m")
	if err != nil {
		t.Fatal(err)
	}
	want := "SELECT * FROM `raw`.`a` WHERE ts > (SELECT max(ts) FROM `an`.`m`) -- $this stays\n AND s = '$this'"
	if got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
	if !UsesThis("SELECT $this") || UsesThis("SELECT '$this' -- $this") || UsesThis("SELECT $thistle") {
		t.Fatal("UsesThis")
	}
}

func TestSelectors(t *testing.T) {
	// a -> b -> c, a -> d, e alone
	names := map[string]string{"a": "A", "b": "B", "c": "C", "d": "D", "e": "E"}
	g, err := BuildDAG([]string{"A", "B", "C", "D", "E"}, map[string][]string{"B": {"a"}, "C": {"b"}, "D": {"a"}}, names)
	if err != nil {
		t.Fatal(err)
	}
	for sel, want := range map[string]string{
		"b":     "B",
		"+b":    "A,B",
		"b+":    "B,C",
		"+c":    "A,B,C",
		"a+":    "A,B,D,C",
		"+d+":   "A,D",
		"e, b+": "B,E,C",
		"*":     "A,E,B,D,C",
	} {
		ids, err := g.Select(sel, names)
		if err != nil {
			t.Fatalf("%s: %v", sel, err)
		}
		got := strings.Join(ids, ",")
		// Compare as sets plus order consistency with g.Order.
		if len(ids) != len(strings.Split(want, ",")) {
			t.Fatalf("%s: got %s want %s", sel, got, want)
		}
		for _, w := range strings.Split(want, ",") {
			if !strings.Contains(","+got+",", ","+w+",") {
				t.Fatalf("%s: got %s want %s", sel, got, want)
			}
		}
	}
	for _, bad := range []string{"", "+", "no_such", "a-b", "++a"} {
		if _, err := g.Select(bad, names); err == nil {
			t.Errorf("%q must be rejected", bad)
		}
	}
	if err := ValidateSelector("+a+, b", map[string]bool{"a": true}); err == nil {
		t.Error("unknown name must be rejected")
	}
}

func TestDataTestSQL(t *testing.T) {
	target := "`db`.`m`"
	cases := []struct {
		test database.ModelTest
		want string
	}{
		{database.ModelTest{Kind: TestNotNull, ColumnName: "id"}, "SELECT count() AS failures FROM `db`.`m` WHERE `id` IS NULL"},
		{database.ModelTest{Kind: TestUnique, ColumnName: "id"}, "SELECT count() AS failures FROM (SELECT `id` FROM `db`.`m` GROUP BY `id` HAVING count() > 1)"},
		{database.ModelTest{Kind: TestAcceptedValues, ColumnName: "s", Args: `{"values":["a","it's"]}`}, "SELECT count() AS failures FROM `db`.`m` WHERE toString(`s`) NOT IN ('a', 'it\\'s')"},
		{database.ModelTest{Kind: TestRelationship, ColumnName: "uid", Args: `{"to":"users","field":"id"}`}, "SELECT count() AS failures FROM `db`.`m` WHERE `uid` IS NOT NULL AND `uid` NOT IN (SELECT `id` FROM `an`.`users`)"},
		{database.ModelTest{Kind: TestRelationship, ColumnName: "uid", Args: `{"to":"raw.people","field":"id"}`}, "SELECT count() AS failures FROM `db`.`m` WHERE `uid` IS NOT NULL AND `uid` NOT IN (SELECT `id` FROM `raw`.`people`)"},
		{database.ModelTest{Kind: TestRelationship, ColumnName: "uid", Args: `{"to":"plain","field":"id"}`}, "SELECT count() AS failures FROM `db`.`m` WHERE `uid` IS NOT NULL AND `uid` NOT IN (SELECT `id` FROM `db`.`plain`)"},
		{database.ModelTest{Kind: TestExpression, Args: `{"expression":"amount >= 0"}`}, "SELECT count() AS failures FROM `db`.`m` WHERE NOT (amount >= 0)"},
	}
	for _, c := range cases {
		got, err := TestSQL(c.test, target, map[string]string{"users": "an"}, "db")
		if err != nil || got != c.want {
			t.Errorf("%s:\n got %s (%v)\nwant %s", c.test.Kind, got, err, c.want)
		}
	}
	for _, bad := range []database.ModelTest{
		{Kind: "nope", ColumnName: "x"},
		{Kind: TestNotNull},
		{Kind: TestNotNull, ColumnName: "a b"},
		{Kind: TestAcceptedValues, ColumnName: "x", Args: `{"values":[]}`},
		{Kind: TestRelationship, ColumnName: "x", Args: `{"to":"a.b.c","field":"id"}`},
		{Kind: TestRelationship, ColumnName: "x", Args: `{"to":"users"}`},
		{Kind: TestExpression, Args: `{"expression":"1) OR (1"}`},
		{Kind: TestNotNull, ColumnName: "x", Severity: "fatal"},
		{Kind: TestNotNull, ColumnName: "x", Args: `[1]`},
	} {
		b := bad
		if err := NormalizeTest(&b); err == nil {
			t.Errorf("%+v must be rejected", bad)
		}
	}
}

func incModel(strategy string) database.Model {
	return database.Model{ID: "m1", Name: "orders", TargetDatabase: "db", Materialization: MaterializationIncremental,
		TableEngine: "MergeTree", IncrementalStrategy: strategy, UniqueKey: "id", WatermarkColumn: "ts"}
}

func TestMaterializeIncrementalStatements(t *testing.T) {
	owned := []chObject{{Name: "orders", Engine: "MergeTree", Comment: "ch-ui model m1"}}

	f := &fakeCH{dbEngine: "Atomic", objects: owned}
	mz := &materializer{exec: f.exec, model: incModel(StrategyAppend), priorSuccess: func() (bool, error) { return true, nil }}
	if _, err := mz.run("SELECT 1 -- c"); err != nil {
		t.Fatal(err)
	}
	assertStmts(t, f.ddl(), []string{"INSERT INTO `db`.`orders` SELECT * FROM (\nSELECT 1 -- c\n) WHERE `ts` > (SELECT max(`ts`) FROM `db`.`orders`)"})

	f = &fakeCH{dbEngine: "Atomic", objects: owned}
	mz = &materializer{exec: f.exec, model: incModel(StrategyDeleteInsert), priorSuccess: func() (bool, error) { return true, nil }}
	if _, err := mz.run("SELECT 1"); err != nil {
		t.Fatal(err)
	}
	assertStmts(t, f.ddl(), []string{
		"CREATE TABLE `db`.`orders__chui_tmp` AS `db`.`orders`",
		"ALTER TABLE `db`.`orders__chui_tmp` MODIFY COMMENT",
		"INSERT INTO `db`.`orders__chui_tmp` SELECT * FROM (\nSELECT 1\n) WHERE `ts` >= (SELECT max(`ts`) FROM `db`.`orders`)",
		"DELETE FROM `db`.`orders` WHERE (`id`) IN (SELECT (`id`) FROM `db`.`orders__chui_tmp`)",
		"INSERT INTO `db`.`orders` SELECT * FROM `db`.`orders__chui_tmp`",
		"DROP TABLE IF EXISTS `db`.`orders__chui_tmp`",
	})

	// Lightweight DELETE refused: fall back to a synchronous mutation.
	f = &fakeCH{dbEngine: "Atomic", objects: owned, failOn: "DELETE FROM"}
	mz = &materializer{exec: f.exec, model: incModel(StrategyDeleteInsert), priorSuccess: func() (bool, error) { return true, nil }}
	if _, err := mz.run("SELECT 1"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(f.ddl(), "\n"), "ALTER TABLE `db`.`orders` DELETE WHERE (`id`) IN") {
		t.Fatalf("no ALTER DELETE fallback: %q", f.ddl())
	}

	// Full refresh goes through the swap.
	f = &fakeCH{dbEngine: "Atomic", objects: owned}
	mz = &materializer{exec: f.exec, model: incModel(StrategyAppend), fullRefresh: true, priorSuccess: func() (bool, error) { return true, nil }}
	if _, err := mz.run("SELECT 1"); err != nil {
		t.Fatal(err)
	}
	assertStmts(t, f.ddl(), []string{"CREATE TABLE `db`.`orders__chui_tmp`", "ALTER TABLE", "EXCHANGE TABLES", "DROP TABLE IF EXISTS"})

	// A hand-made table at the name is never touched.
	f = &fakeCH{dbEngine: "Atomic", objects: []chObject{{Name: "orders", Engine: "MergeTree"}}}
	mz = &materializer{exec: f.exec, model: incModel(StrategyAppend), priorSuccess: func() (bool, error) { return false, nil }}
	if _, err := mz.run("SELECT 1"); err == nil || len(f.ddl()) != 0 {
		t.Fatalf("must refuse without changes: %v %q", err, f.ddl())
	}
}

func TestMaterializedViewStatements(t *testing.T) {
	m := database.Model{ID: "m1", Name: "orders", TargetDatabase: "db", Materialization: MaterializationMaterializedView, TableEngine: "MergeTree", OrderBy: "id"}
	f := &fakeCH{dbEngine: "Atomic"}
	mz := &materializer{exec: f.exec, model: m, priorSuccess: func() (bool, error) { return false, nil }}
	if _, err := mz.run("SELECT 1 AS id"); err != nil {
		t.Fatal(err)
	}
	assertStmts(t, f.ddl(), []string{
		"CREATE TABLE `db`.`orders` ENGINE = MergeTree ORDER BY id EMPTY AS SELECT 1 AS id",
		"ALTER TABLE `db`.`orders` MODIFY COMMENT",
		"CREATE MATERIALIZED VIEW `db`.`orders__mv` TO `db`.`orders` AS SELECT 1 AS id",
		"ALTER TABLE `db`.`orders__mv` MODIFY COMMENT",
		"INSERT INTO `db`.`orders` SELECT 1 AS id",
	})

	// A trigger at <name>__mv that someone else made is refused.
	f = &fakeCH{dbEngine: "Atomic", objects: []chObject{{Name: "orders__mv", Engine: "MaterializedView", Comment: "mine"}}}
	mz = &materializer{exec: f.exec, model: m, priorSuccess: func() (bool, error) { return true, nil }}
	if _, err := mz.run("SELECT 1 AS id"); err == nil || len(f.ddl()) != 0 {
		t.Fatalf("must refuse a foreign __mv: %v %q", err, f.ddl())
	}

	// Another materialization drops this model's trigger first.
	tbl := m
	tbl.Materialization = MaterializationTable
	f = &fakeCH{dbEngine: "Atomic", objects: []chObject{
		{Name: "orders", Engine: "MergeTree", Comment: "ch-ui model m1"},
		{Name: "orders__mv", Engine: "MaterializedView", Comment: "ch-ui model m1"},
	}}
	mz = &materializer{exec: f.exec, model: tbl, priorSuccess: func() (bool, error) { return true, nil }}
	if _, err := mz.run("SELECT 1 AS id"); err != nil {
		t.Fatal(err)
	}
	if got := f.ddl(); len(got) == 0 || got[0] != "DROP VIEW `db`.`orders__mv`" {
		t.Fatalf("trigger not dropped first: %q", got)
	}
}

func TestCompilePlans(t *testing.T) {
	for _, mat := range []string{MaterializationView, MaterializationTable, MaterializationIncremental, MaterializationMaterializedView} {
		m := database.Model{Name: "m", TargetDatabase: "db", Materialization: mat, SQLBody: "SELECT * FROM $ref(a)", IncrementalStrategy: StrategyDeleteInsert, UniqueKey: "id"}
		if err := NormalizeModel(&m); err != nil {
			t.Fatal(err)
		}
		resolved, steps, err := Compile(m, map[string]string{"a": "raw"})
		if err != nil || resolved != "SELECT * FROM `raw`.`a`" || len(steps) == 0 {
			t.Fatalf("%s: %q %v %v", mat, resolved, steps, err)
		}
	}
	if _, _, err := Compile(database.Model{Name: "m", Materialization: "view", SQLBody: "SELECT * FROM $ref(missing)"}, nil); err == nil {
		t.Fatal("unresolved refs must fail")
	}
	if got := PreviewSQL("SELECT 1 -- x", 100); got != "SELECT * FROM (\nSELECT 1 -- x\n) LIMIT 100" {
		t.Fatalf("preview: %q", got)
	}
}
