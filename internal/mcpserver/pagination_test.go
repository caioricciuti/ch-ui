package mcpserver

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/caioricciuti/ch-ui/internal/database"
)

func TestPageOf(t *testing.T) {
	items := make([]map[string]any, 0, 5)
	for i := 0; i < 5; i++ {
		items = append(items, map[string]any{"i": i})
	}
	page, next, err := pageOf(items, "", 2)
	if err != nil || len(page) != 2 || next == "" {
		t.Fatalf("first page: %v %d %q", err, len(page), next)
	}
	page, next, err = pageOf(items, next, 2)
	if err != nil || len(page) != 2 || page[0]["i"] != 2 || next == "" {
		t.Fatalf("second page: %v %v %q", err, page, next)
	}
	page, next, err = pageOf(items, next, 2)
	if err != nil || len(page) != 1 || next != "" {
		t.Fatalf("last page: %v %d %q", err, len(page), next)
	}
	if _, _, err := pageOf(items, "not-a-cursor!", 2); err == nil {
		t.Error("garbage cursor should fail")
	}
	if _, _, err := pageOf(items, encodeCursor("name", "x"), 2); err == nil {
		t.Error("keyset cursor on an offset list should fail")
	}
	if clampPageSize(0) != defaultPageSize || clampPageSize(10_000) != hardMaxPageSize || clampPageSize(7) != 7 {
		t.Error("clampPageSize bounds")
	}
}

func TestToCSV(t *testing.T) {
	cols := []chColumn{{Name: "b", Type: "String"}, {Name: "a", Type: "Float64"}, {Name: "n", Type: "Nullable(Int8)"}}
	rows := []map[string]any{
		{"a": 1.5, "b": "x,y", "n": nil},
		{"a": float64(2), "b": `say "hi"`, "n": float64(3)},
	}
	got := toCSV(cols, rows)
	want := "b,a,n\n\"x,y\",1.5,\n\"say \"\"hi\"\"\",2,3\n"
	if got != want {
		t.Errorf("csv mismatch:\n got: %q\nwant: %q", got, want)
	}
}

func TestListToolsPaginateOverHTTP(t *testing.T) {
	deps, key := testDeps(t)
	h := Handler(deps)
	keys, _ := deps.DB.ListMCPKeys()
	for _, name := range []string{"q1", "q2", "q3"} {
		if _, err := deps.DB.CreateSavedQuery(database.CreateSavedQueryParams{Name: name, Query: "SELECT 1", ConnectionID: keys[0].ConnectionID}); err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
	}
	type page struct {
		SavedQueries []map[string]any `json:"saved_queries"`
		NextCursor   string           `json:"next_cursor"`
	}
	call := func(cursor string) page {
		body := `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"list_saved_queries","arguments":{"page_size":2,"cursor":"` + cursor + `"}}}`
		rec := mcpRequest(t, h, key, body)
		var env struct {
			Result struct {
				IsError bool `json:"isError"`
				Content []struct {
					Text string `json:"text"`
				} `json:"content"`
			} `json:"result"`
		}
		if err := json.Unmarshal(sseData(t, rec.Body.String()), &env); err != nil || env.Result.IsError || len(env.Result.Content) == 0 {
			t.Fatalf("bad tool response: %v %s", err, rec.Body.String())
		}
		var p page
		if err := json.Unmarshal([]byte(env.Result.Content[0].Text), &p); err != nil {
			t.Fatalf("decode page: %v", err)
		}
		return p
	}
	first := call("")
	if len(first.SavedQueries) != 2 || first.NextCursor == "" {
		t.Fatalf("first page: %d items, cursor %q", len(first.SavedQueries), first.NextCursor)
	}
	second := call(first.NextCursor)
	if len(second.SavedQueries) != 1 || second.NextCursor != "" {
		t.Fatalf("second page: %d items, cursor %q", len(second.SavedQueries), second.NextCursor)
	}
	if strings.EqualFold(first.SavedQueries[0]["name"].(string), second.SavedQueries[0]["name"].(string)) {
		t.Error("pages overlap")
	}
}
