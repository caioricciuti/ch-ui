// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

// Package sqlalerts evaluates user-defined SQL threshold alerts: a read-only
// query whose first value (or row count) is compared to a threshold on an
// interval, with the same pending, firing and resolved state machine as the
// built-in health checks.
package sqlalerts

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

const (
	// RowLimit is the LIMIT every alert query is wrapped in. Rows mode
	// counts at most this many rows.
	RowLimit = 101
	// PreviewRows bounds the rows a test run returns.
	PreviewRows = 10

	MinInterval       = 60
	MaxInterval       = 24 * 3600
	MinTimeout        = 5
	MaxTimeout        = 60
	DefaultTimeout    = 30
	DefaultInterval   = 300
	MaxSQLLength      = 20000
	MaxEvaluations    = 20
	maxMemoryUsage    = 512 << 20
	maxResultBytes    = 1 << 20
	maxThreads        = 2
	MaxConsecutiveErr = 5
)

var (
	// statementRE is the first-line check on the statement kind; readonly=1
	// on every run is the real guard. Only SELECT and WITH can be wrapped
	// in a subquery.
	statementRE      = regexp.MustCompile(`(?is)^\s*(SELECT|WITH)\b`)
	settingsRE       = regexp.MustCompile(`(?i)\bSETTINGS\s+[A-Za-z_][A-Za-z0-9_]*\s*=`)
	formatRE         = regexp.MustCompile(`(?is)\bFORMAT\s+[A-Za-z][A-Za-z0-9_]*\s*$`)
	intoOutfileRE    = regexp.MustCompile(`(?i)\bINTO\s+OUTFILE\b`)
	literalOrComment = regexp.MustCompile(`(?s)'(?:[^'\\]|\\.|'')*'|"(?:[^"\\]|\\.)*"|` + "`(?:[^`\\\\]|\\\\.)*`" + `|--[^\n]*|/\*.*?\*/`)
)

// stripLiterals blanks string literals, quoted identifiers and comments so
// keyword checks only see SQL.
func stripLiterals(sql string) string {
	return literalOrComment.ReplaceAllString(sql, " ")
}

// ValidateSQL checks an alert query: one SELECT or WITH statement with no
// SETTINGS, FORMAT or INTO OUTFILE clause. It returns the statement with a
// trailing semicolon removed.
func ValidateSQL(sql string) (string, error) {
	trimmed := strings.TrimSpace(sql)
	for strings.HasSuffix(trimmed, ";") {
		trimmed = strings.TrimSpace(strings.TrimSuffix(trimmed, ";"))
	}
	if trimmed == "" {
		return "", fmt.Errorf("sql is required")
	}
	if len(trimmed) > MaxSQLLength {
		return "", fmt.Errorf("sql is limited to %d characters", MaxSQLLength)
	}
	bare := stripLiterals(trimmed)
	if strings.Contains(bare, ";") {
		return "", fmt.Errorf("only one statement is allowed")
	}
	if !statementRE.MatchString(bare) {
		return "", fmt.Errorf("only SELECT or WITH queries are allowed")
	}
	if settingsRE.MatchString(bare) {
		return "", fmt.Errorf("remove the SETTINGS clause; alerts run with fixed limits")
	}
	if formatRE.MatchString(bare) || intoOutfileRE.MatchString(bare) {
		return "", fmt.Errorf("remove the FORMAT or INTO OUTFILE clause")
	}
	return trimmed, nil
}

// WrapSQL bounds a validated query to RowLimit rows.
func WrapSQL(sql string) string {
	return fmt.Sprintf("SELECT * FROM (\n%s\n) LIMIT %d", sql, RowLimit)
}

// QuerySettings are sent with every alert query. readonly=1 refuses writes
// and any setting change inside the SQL. A user whose profile is already
// read only refuses the other settings; the gateway then drops them, and
// the LIMIT wrap still bounds the result. The log_comment comes from the
// query tag on the context (see QueryTag).
func QuerySettings(timeoutSeconds int) map[string]string {
	return map[string]string{
		"readonly":           "1",
		"max_execution_time": strconv.Itoa(timeoutSeconds),
		"max_memory_usage":   strconv.Itoa(maxMemoryUsage),
		"max_threads":        strconv.Itoa(maxThreads),
		"max_result_bytes":   strconv.Itoa(maxResultBytes),
	}
}

// ValidateAlert checks every field of an alert and its SQL. It fills the
// defaults for zero timeout and evaluation counts and stores the normalized
// SQL.
func ValidateAlert(a *database.SQLAlert) error {
	a.Name = strings.TrimSpace(a.Name)
	if a.Name == "" {
		return fmt.Errorf("name is required")
	}
	if len(a.Name) > 200 {
		return fmt.Errorf("name is limited to 200 characters")
	}
	sql, err := ValidateSQL(a.SQL)
	if err != nil {
		return err
	}
	a.SQL = sql
	if a.Mode == "" {
		a.Mode = "value"
	}
	if a.Mode != "value" && a.Mode != "rows" {
		return fmt.Errorf("mode must be value or rows")
	}
	if !ValidComparator(a.Comparator) {
		return fmt.Errorf("comparator must be gt, gte, lt, lte, eq or ne")
	}
	if math.IsNaN(a.Threshold) || math.IsInf(a.Threshold, 0) {
		return fmt.Errorf("threshold must be a number")
	}
	if a.Mode == "rows" && (a.Threshold < 0 || a.Threshold >= RowLimit) {
		return fmt.Errorf("in rows mode the threshold must be between 0 and %d", RowLimit-1)
	}
	switch a.Severity {
	case "":
		a.Severity = "warn"
	case "info", "warn", "error", "critical":
	default:
		return fmt.Errorf("severity must be info, warn, error or critical")
	}
	if a.IntervalSeconds == 0 {
		a.IntervalSeconds = DefaultInterval
	}
	if a.IntervalSeconds < MinInterval || a.IntervalSeconds > MaxInterval {
		return fmt.Errorf("interval_seconds must be between %d and %d", MinInterval, MaxInterval)
	}
	if a.TimeoutSeconds == 0 {
		a.TimeoutSeconds = DefaultTimeout
	}
	if a.TimeoutSeconds < MinTimeout || a.TimeoutSeconds > MaxTimeout {
		return fmt.Errorf("timeout_seconds must be between %d and %d", MinTimeout, MaxTimeout)
	}
	if a.ForEvaluations == 0 {
		a.ForEvaluations = 1
	}
	if a.ResolveEvaluations == 0 {
		a.ResolveEvaluations = 1
	}
	if a.ForEvaluations < 1 || a.ForEvaluations > MaxEvaluations || a.ResolveEvaluations < 1 || a.ResolveEvaluations > MaxEvaluations {
		return fmt.Errorf("for and resolve evaluations must be between 1 and %d", MaxEvaluations)
	}
	return nil
}

// ValidComparator reports whether c is a supported comparator.
func ValidComparator(c string) bool {
	switch c {
	case "gt", "gte", "lt", "lte", "eq", "ne":
		return true
	}
	return false
}

// ComparatorSymbol renders a comparator for titles.
func ComparatorSymbol(c string) string {
	switch c {
	case "gt":
		return ">"
	case "gte":
		return ">="
	case "lt":
		return "<"
	case "lte":
		return "<="
	case "eq":
		return "="
	case "ne":
		return "!="
	}
	return c
}

// Extracted is what an alert reads from a result.
type Extracted struct {
	// Value is compared to the threshold: the first column of the first
	// row in value mode, the row count in rows mode.
	Value float64
	Rows  int
	// Preview holds the first PreviewRows rows.
	Preview []map[string]interface{}
}

// Extract reads the value of a result in the given mode.
func Extract(res *tunnel.QueryResult, mode string) (Extracted, error) {
	var out Extracted
	var rows []json.RawMessage
	if res != nil && len(res.Data) > 0 {
		if err := json.Unmarshal(res.Data, &rows); err != nil {
			return out, fmt.Errorf("parse result: %w", err)
		}
	}
	out.Rows = len(rows)
	for i := 0; i < len(rows) && i < PreviewRows; i++ {
		var row map[string]interface{}
		if err := json.Unmarshal(rows[i], &row); err == nil {
			out.Preview = append(out.Preview, row)
		}
	}
	if mode == "rows" {
		out.Value = float64(len(rows))
		return out, nil
	}
	if len(rows) == 0 {
		return out, fmt.Errorf("the query returned no rows; use rows mode to alert on a row count")
	}
	col := firstColumn(res.Meta, rows[0])
	if col == "" {
		return out, fmt.Errorf("the query returned no columns")
	}
	var first map[string]json.RawMessage
	if err := json.Unmarshal(rows[0], &first); err != nil {
		return out, fmt.Errorf("parse first row: %w", err)
	}
	v, err := numeric(first[col])
	if err != nil {
		return out, fmt.Errorf("first column %q: %w", col, err)
	}
	out.Value = v
	return out, nil
}

// firstColumn returns the name of the first column, from the result meta
// when present, else from the key order of the first row.
func firstColumn(meta json.RawMessage, row json.RawMessage) string {
	var cols []struct {
		Name string `json:"name"`
	}
	if len(meta) > 0 && json.Unmarshal(meta, &cols) == nil && len(cols) > 0 && cols[0].Name != "" {
		return cols[0].Name
	}
	dec := json.NewDecoder(bytes.NewReader(row))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return ""
	}
	if t, err := dec.Token(); err == nil {
		if name, ok := t.(string); ok {
			return name
		}
	}
	return ""
}

// numeric reads a JSON cell as a number. ClickHouse quotes 64-bit integers
// and decimals; booleans count as 0 or 1.
func numeric(raw json.RawMessage) (float64, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return 0, fmt.Errorf("value is NULL")
	}
	var v interface{}
	if err := json.Unmarshal(raw, &v); err != nil {
		return 0, err
	}
	switch t := v.(type) {
	case float64:
		return t, nil
	case bool:
		if t {
			return 1, nil
		}
		return 0, nil
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(t), 64)
		if err != nil {
			return 0, fmt.Errorf("value %q is not a number", t)
		}
		return f, nil
	}
	return 0, fmt.Errorf("value is not a number")
}
