// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package reports

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

// Table is a query result as text cells, in column order.
type Table struct {
	Columns []string
	Types   []string
	Rows    [][]string
	// Truncated means the query returned more rows than the limit.
	Truncated bool
}

// ParseResult reads a FORMAT JSON result into a table of at most limit
// rows. Column order comes from the result meta. Invalid UTF-8 becomes the
// replacement character.
func ParseResult(res *tunnel.QueryResult, limit int) (Table, error) {
	var t Table
	if res == nil {
		return t, nil
	}
	var meta []struct {
		Name string `json:"name"`
		Type string `json:"type"`
	}
	if len(res.Meta) > 0 {
		if err := json.Unmarshal(res.Meta, &meta); err != nil {
			return t, fmt.Errorf("parse result columns: %w", err)
		}
	}
	for _, m := range meta {
		t.Columns = append(t.Columns, cleanText(m.Name))
		t.Types = append(t.Types, m.Type)
	}
	if len(res.Data) == 0 || string(res.Data) == "null" {
		return t, nil
	}
	var raw []json.RawMessage
	if err := json.Unmarshal(res.Data, &raw); err != nil {
		return t, fmt.Errorf("parse result rows: %w", err)
	}
	for i, r := range raw {
		if limit > 0 && i >= limit {
			t.Truncated = true
			break
		}
		r = bytes.TrimSpace(r)
		if len(r) > 0 && r[0] == '[' {
			var cells []json.RawMessage
			if err := json.Unmarshal(r, &cells); err != nil {
				return t, fmt.Errorf("parse row %d: %w", i+1, err)
			}
			row := make([]string, len(cells))
			for j, c := range cells {
				row[j] = cellText(c)
			}
			t.Rows = append(t.Rows, row)
			continue
		}
		if len(t.Columns) == 0 {
			t.Columns = objectKeys(r)
			t.Types = make([]string, len(t.Columns))
		}
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(r, &obj); err != nil {
			return t, fmt.Errorf("parse row %d: %w", i+1, err)
		}
		row := make([]string, len(t.Columns))
		for j, name := range t.Columns {
			row[j] = cellText(obj[name])
		}
		t.Rows = append(t.Rows, row)
	}
	return t, nil
}

// objectKeys returns the keys of a JSON object in document order.
func objectKeys(raw json.RawMessage) []string {
	dec := json.NewDecoder(bytes.NewReader(raw))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return nil
	}
	var keys []string
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return keys
		}
		name, _ := tok.(string)
		keys = append(keys, cleanText(name))
		var skip json.RawMessage
		if err := dec.Decode(&skip); err != nil {
			return keys
		}
	}
	return keys
}

// cellText renders one JSON cell: strings as they are, null as empty,
// numbers and booleans literally, arrays and objects as compact JSON.
func cellText(raw json.RawMessage) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err == nil {
			return cleanText(s)
		}
	}
	if raw[0] == '[' || raw[0] == '{' {
		var buf bytes.Buffer
		if err := json.Compact(&buf, raw); err == nil {
			return cleanText(buf.String())
		}
	}
	return cleanText(string(raw))
}

func cleanText(s string) string {
	if utf8.ValidString(s) {
		return s
	}
	return strings.ToValidUTF8(s, "�")
}

// csvSafe neutralizes a cell a spreadsheet would read as a formula: one
// that starts with =, +, - or @ (or a tab or carriage return) gets a
// leading apostrophe. Plain numbers such as -5 are left alone.
func csvSafe(s string) string {
	if s == "" {
		return s
	}
	switch s[0] {
	case '=', '+', '-', '@', '\t', '\r':
		if _, err := strconv.ParseFloat(s, 64); err == nil && s[0] != '=' && s[0] != '@' {
			return s
		}
		return "'" + s
	}
	return s
}

// BuildCSV writes the header and as many rows as fit in maxBytes. It returns
// the file, the rows written and whether rows were left out for size.
func BuildCSV(t Table, maxBytes int) ([]byte, int, bool) {
	var out bytes.Buffer
	var line bytes.Buffer
	w := csv.NewWriter(&line)
	w.UseCRLF = true
	encode := func(cells []string) []byte {
		line.Reset()
		safe := make([]string, len(cells))
		for i, c := range cells {
			safe[i] = csvSafe(c)
		}
		_ = w.Write(safe)
		w.Flush()
		return line.Bytes()
	}
	out.Write(encode(t.Columns))
	written := 0
	for _, row := range t.Rows {
		b := encode(row)
		if maxBytes > 0 && out.Len()+len(b) > maxBytes {
			return out.Bytes(), written, true
		}
		out.Write(b)
		written++
	}
	return out.Bytes(), written, false
}

// maxCellWidth caps one column of a text table.
const maxCellWidth = 40

// TextTable renders rows as a fixed-width table for mail and chat.
func TextTable(columns []string, rows [][]string) string {
	if len(columns) == 0 {
		return ""
	}
	cut := func(s string) string {
		s = strings.NewReplacer("\r", " ", "\n", " ", "\t", " ").Replace(s)
		if utf8.RuneCountInString(s) > maxCellWidth {
			r := []rune(s)
			return string(r[:maxCellWidth-3]) + "..."
		}
		return s
	}
	widths := make([]int, len(columns))
	for i, c := range columns {
		widths[i] = utf8.RuneCountInString(cut(c))
	}
	for _, row := range rows {
		for i := range columns {
			if i < len(row) {
				if n := utf8.RuneCountInString(cut(row[i])); n > widths[i] {
					widths[i] = n
				}
			}
		}
	}
	var b strings.Builder
	writeRow := func(cells []string) {
		for i := range columns {
			v := ""
			if i < len(cells) {
				v = cut(cells[i])
			}
			if i > 0 {
				b.WriteString(" | ")
			}
			b.WriteString(v)
			if i < len(columns)-1 {
				b.WriteString(strings.Repeat(" ", widths[i]-utf8.RuneCountInString(v)))
			}
		}
		b.WriteString("\n")
	}
	writeRow(columns)
	for i, w := range widths {
		if i > 0 {
			b.WriteString("-+-")
		}
		b.WriteString(strings.Repeat("-", w))
	}
	b.WriteString("\n")
	for _, row := range rows {
		writeRow(row)
	}
	return strings.TrimRight(b.String(), "\n")
}

// isNumericType reports whether a ClickHouse type holds numbers.
func isNumericType(t string) bool {
	t = strings.TrimPrefix(t, "Nullable(")
	t = strings.TrimPrefix(t, "LowCardinality(")
	for _, p := range []string{"Int", "UInt", "Float", "Decimal"} {
		if strings.HasPrefix(t, p) {
			return true
		}
	}
	return false
}

// numericColumn reports whether column i holds numbers: by type, or, when
// the type is unknown, by every non-empty cell parsing as one.
func (t Table) numericColumn(i int) bool {
	if i < len(t.Types) && t.Types[i] != "" {
		return isNumericType(t.Types[i])
	}
	seen := false
	for _, row := range t.Rows {
		if i >= len(row) || row[i] == "" {
			continue
		}
		if _, err := strconv.ParseFloat(row[i], 64); err != nil {
			return false
		}
		seen = true
	}
	return seen
}

func (t Table) columnIndex(name string) int {
	for i, c := range t.Columns {
		if c == name {
			return i
		}
	}
	return -1
}
