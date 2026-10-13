// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package sqlalerts

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

func TestValidateSQL(t *testing.T) {
	ok := []string{
		"SELECT count() FROM system.parts",
		"  with x AS (SELECT 1) SELECT * FROM x;",
		"SELECT 'a;b' AS s, settings FROM t",
		"SELECT 1 -- SETTINGS readonly = 0;\n",
		"SELECT formatReadableSize(1) AS format",
	}
	for _, q := range ok {
		if _, err := ValidateSQL(q); err != nil {
			t.Errorf("rejected %q: %v", q, err)
		}
	}
	bad := map[string]string{
		"":                                        "required",
		"DROP TABLE t":                            "SELECT or WITH",
		"ALTER TABLE t DELETE WHERE 1":            "SELECT or WITH",
		"INSERT INTO t SELECT 1":                  "SELECT or WITH",
		"SHOW TABLES":                             "SELECT or WITH",
		"SELECT 1; DROP TABLE t":                  "one statement",
		"SELECT 1; SELECT 2":                      "one statement",
		"SELECT 1 SETTINGS max_threads = 64":      "SETTINGS",
		"SELECT 1 FORMAT CSV":                     "FORMAT",
		"SELECT 1 INTO OUTFILE '/tmp/x'":          "OUTFILE",
		"/* x */ DELETE FROM t WHERE 1":           "SELECT or WITH",
		"SELECT * FROM t SETTINGS readonly=0":     "SETTINGS",
		strings.Repeat("SELECT 1 ", MaxSQLLength): "characters",
	}
	for q, want := range bad {
		if _, err := ValidateSQL(q); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%.40q: err %v, want mention of %q", q, err, want)
		}
	}
	if got, _ := ValidateSQL("SELECT 1;;"); got != "SELECT 1" {
		t.Errorf("trailing semicolons kept: %q", got)
	}
	if got := WrapSQL("SELECT 1"); got != "SELECT * FROM (\nSELECT 1\n) LIMIT 101" {
		t.Errorf("wrap = %q", got)
	}
}

func TestValidateAlert(t *testing.T) {
	base := func() database.SQLAlert {
		return database.SQLAlert{Name: "x", SQL: "SELECT 1", Mode: "value", Comparator: "gt", Threshold: 5}
	}
	a := base()
	if err := ValidateAlert(&a); err != nil {
		t.Fatal(err)
	}
	if a.Severity != "warn" || a.IntervalSeconds != DefaultInterval || a.TimeoutSeconds != DefaultTimeout || a.ForEvaluations != 1 || a.ResolveEvaluations != 1 {
		t.Fatalf("defaults not applied: %+v", a)
	}
	mutate := []func(*database.SQLAlert){
		func(a *database.SQLAlert) { a.Name = " " },
		func(a *database.SQLAlert) { a.Mode = "avg" },
		func(a *database.SQLAlert) { a.Comparator = "between" },
		func(a *database.SQLAlert) { a.IntervalSeconds = 30 },
		func(a *database.SQLAlert) { a.IntervalSeconds = 2 * MaxInterval },
		func(a *database.SQLAlert) { a.TimeoutSeconds = 61 },
		func(a *database.SQLAlert) { a.TimeoutSeconds = 2 },
		func(a *database.SQLAlert) { a.Severity = "fatal" },
		func(a *database.SQLAlert) { a.Mode, a.Threshold = "rows", 101 },
		func(a *database.SQLAlert) { a.ForEvaluations = 21 },
		func(a *database.SQLAlert) { a.SQL = "TRUNCATE TABLE t" },
	}
	for i, m := range mutate {
		a := base()
		m(&a)
		if err := ValidateAlert(&a); err == nil {
			t.Errorf("case %d accepted: %+v", i, a)
		}
	}
}

func TestExtract(t *testing.T) {
	res := func(data, meta string) *tunnel.QueryResult {
		r := &tunnel.QueryResult{Data: json.RawMessage(data)}
		if meta != "" {
			r.Meta = json.RawMessage(meta)
		}
		return r
	}
	cases := []struct {
		name, data, meta, mode string
		want                   float64
		rows                   int
		err                    string
	}{
		{"meta orders columns", `[{"b":2,"a":"7"}]`, `[{"name":"a","type":"UInt64"},{"name":"b","type":"UInt8"}]`, "value", 7, 1, ""},
		{"key order without meta", `[{"z":3.5,"a":1}]`, "", "value", 3.5, 1, ""},
		{"quoted 64-bit integer", `[{"c":"18446744073709551615"}]`, "", "value", 18446744073709551615, 1, ""},
		{"bool", `[{"ok":true}]`, "", "value", 1, 1, ""},
		{"null", `[{"c":null}]`, "", "value", 0, 1, "NULL"},
		{"text", `[{"c":"abc"}]`, "", "value", 0, 1, "not a number"},
		{"no rows in value mode", `[]`, "", "value", 0, 0, "no rows"},
		{"rows mode counts", `[{"a":1},{"a":2},{"a":3}]`, "", "rows", 3, 3, ""},
		{"rows mode empty", `[]`, "", "rows", 0, 0, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Extract(res(tc.data, tc.meta), tc.mode)
			if tc.err != "" {
				if err == nil || !strings.Contains(err.Error(), tc.err) {
					t.Fatalf("err %v, want %q", err, tc.err)
				}
				return
			}
			if err != nil || got.Value != tc.want || got.Rows != tc.rows {
				t.Fatalf("got %+v %v, want value %v rows %d", got, err, tc.want, tc.rows)
			}
		})
	}
	var many []string
	for i := 0; i < 30; i++ {
		many = append(many, `{"a":1}`)
	}
	got, err := Extract(res("["+strings.Join(many, ",")+"]", ""), "rows")
	if err != nil || len(got.Preview) != PreviewRows || got.Rows != 30 {
		t.Fatalf("preview %d rows %d %v", len(got.Preview), got.Rows, err)
	}
}
