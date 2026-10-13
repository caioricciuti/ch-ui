package models

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

// Data test kinds.
const (
	TestNotNull        = "not_null"
	TestUnique         = "unique"
	TestAcceptedValues = "accepted_values"
	TestRelationship   = "relationship"
	TestExpression     = "expression"
)

// Data test severities.
const (
	SeverityError = "error"
	SeverityWarn  = "warn"
)

// TestArgs holds the kind-specific settings of a data test, stored as JSON.
type TestArgs struct {
	// Values lists the accepted values (accepted_values). They are compared
	// as text: toString(column) NOT IN (...).
	Values []string `json:"values,omitempty" yaml:"values,omitempty"`
	// To names the parent of a relationship: a model name, a table in the
	// model's database, or database.table.
	To string `json:"to,omitempty" yaml:"to,omitempty"`
	// Field is the parent's key column (relationship).
	Field string `json:"field,omitempty" yaml:"field,omitempty"`
	// Expression must hold for every row (expression).
	Expression string `json:"expression,omitempty" yaml:"expression,omitempty"`
}

// NormalizeTest validates a test definition and canonicalizes its args JSON.
func NormalizeTest(t *database.ModelTest) error {
	t.Kind = strings.TrimSpace(t.Kind)
	t.ColumnName = strings.TrimSpace(t.ColumnName)
	t.Severity = strings.TrimSpace(t.Severity)
	if t.Severity == "" {
		t.Severity = SeverityError
	}
	if t.Severity != SeverityError && t.Severity != SeverityWarn {
		return fmt.Errorf("severity must be 'error' or 'warn' (got %q)", t.Severity)
	}
	var args TestArgs
	if s := strings.TrimSpace(t.Args); s != "" && s != "null" {
		if err := json.Unmarshal([]byte(s), &args); err != nil {
			return fmt.Errorf("test args must be a JSON object: %w", err)
		}
	}
	needColumn := func() error {
		if !identPattern.MatchString(t.ColumnName) {
			return fmt.Errorf("%s test needs a column name (got %q)", t.Kind, t.ColumnName)
		}
		return nil
	}
	clean := TestArgs{}
	switch t.Kind {
	case TestNotNull, TestUnique:
		if err := needColumn(); err != nil {
			return err
		}
	case TestAcceptedValues:
		if err := needColumn(); err != nil {
			return err
		}
		if len(args.Values) == 0 {
			return fmt.Errorf("accepted_values test needs at least one value")
		}
		clean.Values = args.Values
	case TestRelationship:
		if err := needColumn(); err != nil {
			return err
		}
		if _, err := splitTableName(args.To); err != nil {
			return fmt.Errorf("relationship test: %w", err)
		}
		if !identPattern.MatchString(strings.TrimSpace(args.Field)) {
			return fmt.Errorf("relationship test needs the parent's key column in field (got %q)", args.Field)
		}
		clean.To, clean.Field = strings.TrimSpace(args.To), strings.TrimSpace(args.Field)
	case TestExpression:
		expr := strings.TrimSpace(args.Expression)
		if expr == "" {
			return fmt.Errorf("expression test needs an expression")
		}
		if err := ValidateExpression("test expression", expr); err != nil {
			return err
		}
		if t.ColumnName != "" && !identPattern.MatchString(t.ColumnName) {
			return fmt.Errorf("column %q is not a column name", t.ColumnName)
		}
		clean.Expression = expr
	default:
		return fmt.Errorf("test kind must be one of not_null, unique, accepted_values, relationship, expression (got %q)", t.Kind)
	}
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(clean); err != nil {
		return fmt.Errorf("test args: %w", err)
	}
	t.Args = strings.TrimSpace(b.String())
	return nil
}

// splitTableName parses "name" or "database.name" (identifiers only).
func splitTableName(s string) ([]string, error) {
	s = strings.TrimSpace(s)
	parts := strings.Split(s, ".")
	if s == "" || len(parts) > 2 {
		return nil, fmt.Errorf("to must be a model or table name, optionally database.table (got %q)", s)
	}
	for _, p := range parts {
		if !identPattern.MatchString(p) {
			return nil, fmt.Errorf("to must be a model or table name, optionally database.table (got %q)", s)
		}
	}
	return parts, nil
}

// TestSQL renders the query that counts a test's failing rows. target is the
// model's quoted table; modelTargets maps model names to their database (for
// relationships to other models); defaultDB is the model's database.
func TestSQL(t database.ModelTest, target string, modelTargets map[string]string, defaultDB string) (string, error) {
	var args TestArgs
	if strings.TrimSpace(t.Args) != "" {
		if err := json.Unmarshal([]byte(t.Args), &args); err != nil {
			return "", fmt.Errorf("test args: %w", err)
		}
	}
	col := quoteIdent(t.ColumnName)
	switch t.Kind {
	case TestNotNull:
		return fmt.Sprintf("SELECT count() AS failures FROM %s WHERE %s IS NULL", target, col), nil
	case TestUnique:
		return fmt.Sprintf("SELECT count() AS failures FROM (SELECT %s FROM %s GROUP BY %s HAVING count() > 1)", col, target, col), nil
	case TestAcceptedValues:
		vals := make([]string, len(args.Values))
		for i, v := range args.Values {
			vals[i] = quoteString(v)
		}
		return fmt.Sprintf("SELECT count() AS failures FROM %s WHERE toString(%s) NOT IN (%s)", target, col, strings.Join(vals, ", ")), nil
	case TestRelationship:
		parts, err := splitTableName(args.To)
		if err != nil {
			return "", err
		}
		var parent string
		if len(parts) == 2 {
			parent = quoteIdent(parts[0]) + "." + quoteIdent(parts[1])
		} else if db, ok := modelTargets[parts[0]]; ok {
			parent = quoteIdent(db) + "." + quoteIdent(parts[0])
		} else {
			parent = quoteIdent(defaultDB) + "." + quoteIdent(parts[0])
		}
		return fmt.Sprintf("SELECT count() AS failures FROM %s WHERE %s IS NOT NULL AND %s NOT IN (SELECT %s FROM %s)",
			target, col, col, quoteIdent(args.Field), parent), nil
	case TestExpression:
		if err := ValidateExpression("test expression", args.Expression); err != nil {
			return "", err
		}
		return fmt.Sprintf("SELECT count() AS failures FROM %s WHERE NOT (%s)", target, args.Expression), nil
	}
	return "", fmt.Errorf("unknown test kind %q", t.Kind)
}

// TestOutcome is one test's result in a run, stored as JSON in the run result.
type TestOutcome struct {
	TestID   string `json:"test_id"`
	Kind     string `json:"kind"`
	Column   string `json:"column,omitempty"`
	Severity string `json:"severity"`
	// Status is pass, fail (failing rows found) or error (the query failed).
	Status   string `json:"status"`
	Failures int64  `json:"failures"`
	Error    string `json:"error,omitempty"`
	SQL      string `json:"sql"`
}

// Label names a test in messages, like not_null(user_id).
func (o TestOutcome) Label() string {
	if o.Column != "" {
		return o.Kind + "(" + o.Column + ")"
	}
	return o.Kind
}

// testSettings bound every data test query: read only and time limited.
var testSettings = map[string]string{
	"readonly":           "1",
	"max_execution_time": "120",
	"output_format_json_quote_64bit_integers": "0",
}

// runDataTests runs each test against target and returns the outcomes and
// whether an error-severity test failed.
func runDataTests(query func(sql string) (*tunnel.QueryResult, error), tests []database.ModelTest, target string, modelTargets map[string]string, defaultDB string) ([]TestOutcome, bool) {
	var outcomes []TestOutcome
	blocking := false
	for _, t := range tests {
		o := TestOutcome{TestID: t.ID, Kind: t.Kind, Column: t.ColumnName, Severity: t.Severity}
		sql, err := TestSQL(t, target, modelTargets, defaultDB)
		o.SQL = sql
		if err == nil {
			var res *tunnel.QueryResult
			res, err = query(sql)
			if err == nil {
				o.Failures, err = firstCount(res)
			}
		}
		switch {
		case err != nil:
			o.Status, o.Error = "error", err.Error()
		case o.Failures > 0:
			o.Status = "fail"
		default:
			o.Status = "pass"
		}
		if o.Status != "pass" && t.Severity != SeverityWarn {
			blocking = true
		}
		outcomes = append(outcomes, o)
	}
	return outcomes, blocking
}

// firstCount reads the failures column of a one-row result.
func firstCount(res *tunnel.QueryResult) (int64, error) {
	if res == nil || len(res.Data) == 0 {
		return 0, fmt.Errorf("empty result")
	}
	var rows []map[string]json.RawMessage
	if err := json.Unmarshal(res.Data, &rows); err != nil {
		return 0, fmt.Errorf("read test result: %w", err)
	}
	if len(rows) == 0 {
		return 0, fmt.Errorf("empty result")
	}
	raw := strings.Trim(string(rows[0]["failures"]), `"`)
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("read test result %q: %w", raw, err)
	}
	return n, nil
}

// summarizeFailures describes the failed tests for the model's error.
func summarizeFailures(outcomes []TestOutcome) string {
	var parts []string
	for _, o := range outcomes {
		if o.Status == "pass" || o.Severity == SeverityWarn {
			continue
		}
		if o.Status == "error" {
			parts = append(parts, fmt.Sprintf("%s: %s", o.Label(), o.Error))
		} else {
			parts = append(parts, fmt.Sprintf("%s: %d failing rows", o.Label(), o.Failures))
		}
	}
	return "data tests failed: " + strings.Join(parts, "; ")
}
