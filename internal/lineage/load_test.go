package lineage

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
)

// fakeQuery answers Load's queries from canned JSON keyed by a substring of
// the SQL, and records every call.
type fakeQuery struct {
	mu      sync.Mutex
	answers map[string]func(sql string, params map[string]string) (string, error)
	calls   []string
}

func (f *fakeQuery) query(_ context.Context, sql string, params map[string]string, dst any) error {
	f.mu.Lock()
	f.calls = append(f.calls, sql)
	f.mu.Unlock()
	for key, answer := range f.answers {
		if strings.Contains(sql, key) {
			data, err := answer(sql, params)
			if err != nil {
				return err
			}
			return json.Unmarshal([]byte(data), dst)
		}
	}
	return json.Unmarshal([]byte("[]"), dst)
}

func tablesJSON(n int) string {
	rows := make([]string, n)
	for i := range rows {
		rows[i] = `{"database":"app","name":"t` + strings.Repeat("x", i%3) + `","engine":"MergeTree","total_rows":"1"}`
	}
	return "[" + strings.Join(rows, ",") + "]"
}

func TestLoadBuildsGraphAndBindsDatabases(t *testing.T) {
	var gotParam string
	f := &fakeQuery{answers: map[string]func(string, map[string]string) (string, error){
		"FROM system.tables": func(_ string, p map[string]string) (string, error) {
			gotParam = p["param_databases"]
			return `[{"database":"app","name":"src","engine":"MergeTree","dependencies_database":["app"],"dependencies_table":["mv"],"total_rows":"10"},
			{"database":"app","name":"mv","engine":"MaterializedView","ddl":"CREATE MATERIALIZED VIEW app.mv TO app.tgt AS SELECT * FROM app.src","as_select":"SELECT * FROM app.src"},
			{"database":"app","name":"tgt","engine":"MergeTree"}]`, nil
		},
		"system.view_refreshes": func(string, map[string]string) (string, error) {
			return "", errors.New("Code: 60. UNKNOWN_TABLE")
		},
		"hostName()": func(string, map[string]string) (string, error) { return `[{"host":"h1","hosts":"1"}]`, nil },
	}}
	g, err := Load(context.Background(), f.query, []string{"app", "o'x"})
	if err != nil {
		t.Fatal(err)
	}
	if gotParam != `['app','o''x']` {
		t.Errorf("param_databases = %s", gotParam)
	}
	if g.Meta.Host != "h1" || g.Meta.MultiNode || len(g.Meta.Warnings) != 0 {
		t.Errorf("meta: %+v", g.Meta)
	}
	want := map[[2]string]bool{{"app.src", "app.mv"}: true, {"app.mv", "app.tgt"}: true}
	for _, e := range g.Edges {
		delete(want, [2]string{e.From, e.To})
	}
	if len(want) != 0 {
		t.Errorf("missing edges %v in %+v", want, g.Edges)
	}
}

func TestLoadTooLarge(t *testing.T) {
	big := tablesJSON(MaxObjects + 1)
	f := &fakeQuery{answers: map[string]func(string, map[string]string) (string, error){
		"FROM system.tables": func(string, map[string]string) (string, error) { return big, nil },
	}}
	_, err := Load(context.Background(), f.query, nil)
	var tooLarge TooLargeError
	if !errors.As(err, &tooLarge) {
		t.Fatalf("unfiltered over the cap: want TooLargeError, got %v", err)
	}

	// Filtered: the strict query (only the selected databases) is tried.
	f = &fakeQuery{answers: map[string]func(string, map[string]string) (string, error){
		"FROM system.tables": func(sql string, _ map[string]string) (string, error) {
			if strings.Contains(sql, "AND has({databases") {
				return tablesJSON(2), nil
			}
			return big, nil
		},
	}}
	g, err := Load(context.Background(), f.query, []string{"app"})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, w := range g.Meta.Warnings {
		found = found || strings.Contains(w, "objects outside the selected databases are not read")
	}
	if !found {
		t.Errorf("strict retry should warn, got %q", g.Meta.Warnings)
	}
}

func TestLoadWithoutAsSelect(t *testing.T) {
	f := &fakeQuery{answers: map[string]func(string, map[string]string) (string, error){
		"FROM system.tables": func(sql string, _ map[string]string) (string, error) {
			if strings.Contains(sql, "as_select AS as_select") {
				return "", errors.New("Code: 47. Missing columns: 'as_select'")
			}
			return `[{"database":"app","name":"a","engine":"MergeTree"}]`, nil
		},
	}}
	g, err := Load(context.Background(), f.query, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Nodes) != 1 || g.Nodes[0].ID != "app.a" {
		t.Errorf("nodes: %+v", g.Nodes)
	}
}

func TestLineageTablesSQL(t *testing.T) {
	all := TablesSQL(false, false, true)
	if strings.Contains(all, "{databases") || !strings.Contains(all, "as_select AS as_select") {
		t.Fatalf("unfiltered: %s", all)
	}
	wide := TablesSQL(true, false, true)
	// Definitions of views, MVs and dictionaries are read in every database,
	// so readers outside the selection still link to it.
	if strings.Contains(wide, "AND has(") || !strings.Contains(wide, "as_select AS as_select") ||
		!strings.Contains(wide, "if(engine IN ('View', 'MaterializedView', 'Dictionary'), create_table_query, '') AS ddl") {
		t.Fatalf("filtered: %s", wide)
	}
	strict := TablesSQL(true, true, false)
	if !strings.Contains(strict, "AND has({databases:Array(String)}, database)") || !strings.Contains(strict, "'' AS as_select") {
		t.Fatalf("strict: %s", strict)
	}
	for _, sql := range []string{all, wide, strict} {
		if !strings.Contains(sql, "LIMIT 50001") || strings.Contains(sql, "query_log") {
			t.Fatalf("unbounded: %s", sql)
		}
	}
}
