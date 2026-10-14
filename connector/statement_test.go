package connector

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestMetaProbeFor(t *testing.T) {
	tests := []struct {
		query string
		want  metaProbe
	}{
		{"SELECT 1", probeWrap},
		{"with x AS (SELECT 1) SELECT * FROM x", probeWrap},
		{"(SELECT 1) UNION ALL (SELECT 2)", probeWrap},
		{"-- x\nSELECT 1", probeWrap},
		{"# x\nSELECT 1", probeWrap},
		{"#!x\nSELECT 1", probeWrap},
		{"/* a /* nested */ b */ SELECT 1", probeWrap},
		{";; \n SELECT 1", probeWrap},
		{"SHOW TABLES", probeIntrospect},
		{"/* x */ DESCRIBE TABLE t", probeIntrospect},
		{"DESC t", probeIntrospect},
		{"EXISTS TABLE t", probeIntrospect},
		{"EXPLAIN SELECT 1", probeIntrospect},
		// Everything else runs once, never probed.
		{"# c\nCREATE TABLE t ENGINE = Memory AS SELECT 1", probeNone},
		{"REPLACE TABLE t ENGINE = Memory AS SELECT 1", probeNone},
		{"CREATE OR REPLACE TABLE t ENGINE = Memory AS SELECT 1", probeNone},
		{"/* x */ INSERT INTO t SELECT 1", probeNone},
		{"CHECK TABLE t", probeNone},
		{"BACKUP TABLE t TO Disk('backups', 'x')", probeNone},
		{"RESTORE TABLE t FROM Disk('backups', 'x')", probeNone},
		{"EXCHANGE TABLES a AND b", probeNone},
		{"MOVE USER u TO disk", probeNone},
		{"UNDROP TABLE t", probeNone},
		{"KILL QUERY WHERE 1", probeNone},
		{"SYSTEM FLUSH LOGS", probeNone},
		{"OPTIMIZE TABLE t FINAL", probeNone},
		{"WATCH lv", probeNone},
		{"SELECTED", probeNone},
		{"-- SELECT 1\nDROP TABLE t", probeNone},
		{"/* SELECT */ DROP TABLE t", probeNone},
		{"#\nSELECT 1", probeNone}, // '#' without a space is not a comment
		{"'SELECT' DROP", probeNone},
		{"", probeNone},
	}
	for _, tt := range tests {
		if got := metaProbeFor(tt.query); got != tt.want {
			t.Errorf("metaProbeFor(%q) = %v, want %v", tt.query, got, tt.want)
		}
	}
}

func TestTrimTrailingNoise(t *testing.T) {
	tests := []struct{ query, want string }{
		{"SELECT 1", "SELECT 1"},
		{"SELECT 1;", "SELECT 1"},
		{"SELECT 1; -- note", "SELECT 1"},
		{"SELECT 1 -- note\n", "SELECT 1"},
		{"SELECT 1 # note", "SELECT 1"},
		{"SELECT 1 /* a */ ; /* b */\n", "SELECT 1"},
		{"SELECT '--x'", "SELECT '--x'"},
		{"SELECT '--x' -- y", "SELECT '--x'"},
		{"SELECT 'it''s -- not' ;", "SELECT 'it''s -- not'"},
		{"SELECT 'a\\'--b'", "SELECT 'a\\'--b'"},
		{"SELECT \"--\", `/*` -- c", "SELECT \"--\", `/*`"},
		{"SELECT $$ -- $$ -- c", "SELECT $$ -- $$"},
		{"SELECT 1 -- a\n+ 1 -- b", "SELECT 1 -- a\n+ 1"},
		{"-- only", ""},
	}
	for _, tt := range tests {
		if got := trimTrailingNoise(tt.query); got != tt.want {
			t.Errorf("trimTrailingNoise(%q) = %q, want %q", tt.query, got, tt.want)
		}
	}
}

func TestBuildMetaQueryTrailingComment(t *testing.T) {
	if got, want := buildMetaQuery("SELECT 1; -- note"), "SELECT * FROM (\nSELECT 1\n) LIMIT 0"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got, want := buildMetaQuery("SHOW TABLES; # note"), "SHOW TABLES\nLIMIT 0"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestUnwrapErrorPosition(t *testing.T) {
	tests := []struct{ in, want string }{
		{
			"ClickHouse error: Code: 62. DB::Exception: Syntax error: failed at position 24 ('FRM') (line 2, col 9): FRM t. (SYNTAX_ERROR)",
			"ClickHouse error: Code: 62. DB::Exception: Syntax error: failed at position 8 ('FRM') (line 1, col 9): FRM t. (SYNTAX_ERROR)",
		},
		{
			// ClickHouse >= 25.2: unquoted token.
			"Code: 62. DB::Exception: Syntax error: failed at position 40 (FROM) (line 3, col 1): FROM",
			"Code: 62. DB::Exception: Syntax error: failed at position 24 (FROM) (line 2, col 1): FROM",
		},
		{
			"Code: 62. DB::Exception: Syntax error: failed at position 30: x",
			"Code: 62. DB::Exception: Syntax error: failed at position 14: x",
		},
		{
			// An error inside the wrapper clamps to the start.
			"Syntax error: failed at position 3 (line 1, col 3)",
			"Syntax error: failed at position 1 (line 1, col 3)",
		},
		{
			// A remote shard's position indexes another query.
			"Code: 62. DB::Exception: Received from h:9000. DB::Exception: Syntax error: failed at position 40",
			"Code: 62. DB::Exception: Received from h:9000. DB::Exception: Syntax error: failed at position 40",
		},
		{"Code: 60. DB::Exception: Unknown table", "Code: 60. DB::Exception: Unknown table"},
	}
	for _, tt := range tests {
		if got := unwrapErrorPosition(tt.in); got != tt.want {
			t.Errorf("unwrapErrorPosition(%q)\n got %q\nwant %q", tt.in, got, tt.want)
		}
	}
}

// fakeClickHouse records each statement and answers it with respond.
func fakeClickHouse(t *testing.T, respond func(w http.ResponseWriter, r *http.Request, sql string)) (*CHClient, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		sql := string(body)
		if strings.Contains(sql, "system.processes") {
			return
		}
		mu.Lock()
		seen = append(seen, sql)
		mu.Unlock()
		respond(w, r, sql)
	}))
	t.Cleanup(srv.Close)
	return NewCHClient(srv.URL, false), func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), seen...)
	}
}

func stream(t *testing.T, c *CHClient, sql string) (meta []string, rows int64, err error) {
	t.Helper()
	_, rows, err = c.ExecuteStreaming(context.Background(), "", sql, "u", "", 100, nil,
		func(m json.RawMessage) error { meta = append(meta, string(m)); return nil },
		func(int, json.RawMessage) error { return nil },
		nil)
	return meta, rows, err
}

func TestExecuteStreamingRunsUnprobedStatementsOnce(t *testing.T) {
	for _, sql := range []string{
		"# c\nCREATE TABLE t ENGINE = Memory AS SELECT 1",
		"REPLACE TABLE t ENGINE = Memory AS SELECT 1",
		"/* x */ INSERT INTO t SELECT 1",
		"BACKUP TABLE t TO Disk('backups', 'x')",
	} {
		c, seen := fakeClickHouse(t, func(w http.ResponseWriter, r *http.Request, sql string) {})
		meta, _, err := stream(t, c, sql)
		if err != nil {
			t.Fatalf("%q: %v", sql, err)
		}
		if got := seen(); len(got) != 1 || got[0] != sql {
			t.Errorf("%q: want one unchanged request, got %q", sql, got)
		}
		if len(meta) != 1 || meta[0] != "[]" {
			t.Errorf("%q: want one empty meta, got %q", sql, meta)
		}
	}
}

func TestExecuteStreamingReadsHeaderFromBody(t *testing.T) {
	var format string
	c, seen := fakeClickHouse(t, func(w http.ResponseWriter, r *http.Request, sql string) {
		format = r.URL.Query().Get("default_format")
		io.WriteString(w, "[\"part_path\",\"is_passed\"]\n[\"String\",\"UInt8\"]\n[\"all_1_1_0\",1]\n")
	})
	meta, rows, err := stream(t, c, "CHECK TABLE t")
	if err != nil {
		t.Fatal(err)
	}
	if got := seen(); len(got) != 1 || got[0] != "CHECK TABLE t" {
		t.Errorf("want one unchanged request, got %q", got)
	}
	if format != "JSONCompactEachRowWithNamesAndTypes" {
		t.Errorf("default_format = %q", format)
	}
	if len(meta) != 1 || meta[0] != `[{"name":"part_path","type":"String"},{"name":"is_passed","type":"UInt8"}]` {
		t.Errorf("meta = %q", meta)
	}
	if rows != 1 {
		t.Errorf("rows = %d, want 1", rows)
	}
}

func TestExecuteStreamingTrailingCommentAndErrorPosition(t *testing.T) {
	c, seen := fakeClickHouse(t, func(w http.ResponseWriter, r *http.Request, sql string) {
		if strings.HasPrefix(sql, metaWrapPrefix) {
			io.WriteString(w, `{"meta":[{"name":"1","type":"UInt8"}],"data":[]}`)
			return
		}
		io.WriteString(w, "[1]\n")
	})
	if _, _, err := stream(t, c, "SELECT 1; -- note"); err != nil {
		t.Fatal(err)
	}
	got := seen()
	if len(got) != 2 || got[0] != "SELECT * FROM (\nSELECT 1\n) LIMIT 0\nFORMAT JSONCompact" || got[1] != "SELECT 1\nFORMAT JSONCompactEachRow" {
		t.Errorf("requests = %q", got)
	}

	c, _ = fakeClickHouse(t, func(w http.ResponseWriter, r *http.Request, sql string) {
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, "Code: 62. DB::Exception: Syntax error: failed at position 23 ('FRM') (line 2, col 8): FRM t. (SYNTAX_ERROR)")
	})
	_, _, err := stream(t, c, "SELECT FRM t")
	if err == nil || !strings.Contains(err.Error(), "failed at position 7 ('FRM') (line 1, col 8)") {
		t.Errorf("err = %v", err)
	}
}
