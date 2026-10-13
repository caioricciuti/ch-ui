// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package reports

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

func TestCSVEscapingAndFormulaInjection(t *testing.T) {
	tbl := Table{
		Columns: []string{"name", "note", "n"},
		Rows: [][]string{
			{"=HYPERLINK(\"http://x\")", "a, \"quoted\"\nline", "-5"},
			{"+1+2", "@SUM(A1)", "-1e3"},
			{"-cmd", "plain", "3.5"},
		},
	}
	data, written, cut := BuildCSV(tbl, 0)
	if cut || written != 3 {
		t.Fatalf("written %d cut %v", written, cut)
	}
	records, err := csv.NewReader(bytes.NewReader(data)).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"name", "note", "n"},
		{"'=HYPERLINK(\"http://x\")", "a, \"quoted\"\nline", "-5"},
		{"'+1+2", "'@SUM(A1)", "-1e3"},
		{"'-cmd", "plain", "3.5"},
	}
	for i := range want {
		for j := range want[i] {
			if records[i][j] != want[i][j] {
				t.Errorf("cell %d,%d = %q, want %q", i, j, records[i][j], want[i][j])
			}
		}
	}
}

func TestCSVTruncatesAtMaxBytes(t *testing.T) {
	tbl := Table{Columns: []string{"v"}}
	for i := 0; i < 1000; i++ {
		tbl.Rows = append(tbl.Rows, []string{strings.Repeat("x", 100)})
	}
	data, written, cut := BuildCSV(tbl, 10<<10)
	if !cut || written == 0 || written >= 1000 {
		t.Fatalf("written %d cut %v", written, cut)
	}
	if len(data) > 10<<10 {
		t.Fatalf("csv is %d bytes, over the cap", len(data))
	}
	records, err := csv.NewReader(bytes.NewReader(data)).ReadAll()
	if err != nil || len(records) != written+1 {
		t.Fatalf("csv not whole rows: %d records, %v", len(records), err)
	}
}

func TestParseResultOrderLimitAndUTF8(t *testing.T) {
	res := &tunnel.QueryResult{
		Meta: json.RawMessage(`[{"name":"z","type":"String"},{"name":"a","type":"UInt64"},{"name":"arr","type":"Array(UInt8)"}]`),
		Data: json.RawMessage(`[{"a":"1","z":"first\ud800","arr":[1,2]},{"a":"2","z":null,"arr":[]},{"a":"3","z":"c","arr":[]}]`),
	}
	tbl, err := ParseResult(res, 2)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(tbl.Columns, ",") != "z,a,arr" {
		t.Fatalf("column order %v", tbl.Columns)
	}
	if !tbl.Truncated || len(tbl.Rows) != 2 {
		t.Fatalf("limit: %d rows truncated=%v", len(tbl.Rows), tbl.Truncated)
	}
	if !utf8.ValidString(tbl.Rows[0][0]) || !strings.HasPrefix(tbl.Rows[0][0], "first") {
		t.Fatalf("invalid UTF-8 kept: %q", tbl.Rows[0][0])
	}
	if tbl.Rows[0][2] != "[1,2]" || tbl.Rows[1][0] != "" {
		t.Fatalf("cells %v", tbl.Rows)
	}
	// Raw invalid bytes in a cell are replaced too.
	if got := cleanText("ok\xff"); !utf8.ValidString(got) {
		t.Fatalf("cleanText kept invalid bytes: %q", got)
	}
}

func TestRenderQueryStatesTruncation(t *testing.T) {
	s := &database.ReportSubscription{ID: "r", Name: "Sales", Format: FormatBoth, RowLimit: 3, InlineRows: 2, MaxBytes: 1 << 20, Timezone: "UTC"}
	tbl := Table{Columns: []string{"a"}, Rows: [][]string{{"1"}, {"2"}, {"3"}}, Truncated: true}
	snap := RenderQuery(s, tbl, "https://x/schedules?section=reports", time.Date(2026, 10, 13, 8, 0, 0, 0, time.UTC))
	for _, want := range []string{"row limit of 3", "first 2 of 3 rows", "attached as sales-20261013-0800.csv"} {
		if !strings.Contains(snap.Text, want) {
			t.Errorf("text misses %q:\n%s", want, snap.Text)
		}
	}
	if !strings.Contains(snap.HTML, "<table") || len(snap.CSV) == 0 || len(snap.Rows) != 2 {
		t.Fatal("html table, csv and inline rows expected")
	}
	if snap.Subject != "[CH-UI] Report: Sales" {
		t.Fatalf("subject %q", snap.Subject)
	}
	s.Name = "<script>"
	snap = RenderQuery(s, tbl, "", time.Now())
	if strings.Contains(snap.HTML, "<script>") {
		t.Fatal("html is not escaped")
	}
}
