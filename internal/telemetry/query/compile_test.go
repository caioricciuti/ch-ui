package query

import (
	"strings"
	"testing"
)

func logsSchema() Schema {
	return Schema{
		TextExpr: "Body",
		Roles: map[string]string{
			"level":    "SeverityText",
			"service":  "ServiceName",
			"trace_id": "TraceId",
			"span_id":  "SpanId",
		},
		CaseInsensitive: map[string]bool{"level": true},
		Numeric: map[string]NumericField{
			"severity_number": {Expr: "SeverityNumber"},
		},
		Aliases:       map[string]string{"severity": "level", "service_name": "service", "trace": "trace_id"},
		AttributeMaps: []string{"LogAttributes", "ResourceAttributes"},
		Columns:       map[string]string{"ScopeName": "String", "TraceFlags": "UInt8"},
	}
}

func tracesSchema() Schema {
	return Schema{
		TextExpr:        "SpanName",
		Roles:           map[string]string{"service": "ServiceName", "status": "StatusCode", "span_name": "SpanName"},
		Numeric:         map[string]NumericField{"duration": {Expr: "Duration", Scale: 1e6}},
		CaseInsensitive: map[string]bool{"status": true},
		AttributeMaps:   []string{"SpanAttributes", "ResourceAttributes"},
	}
}

func TestCompile(t *testing.T) {
	cases := []struct {
		name, q, want string
		schema        Schema
	}{
		{"empty", "", "1", logsSchema()},
		{"word", "timeout", "hasTokenCaseInsensitive(Body, 'timeout')", logsSchema()},
		{"non-token word", "id=5", "positionCaseInsensitive(Body, 'id=5') > 0", logsSchema()},
		{"two words implicit and", "cache miss", "(hasTokenCaseInsensitive(Body, 'cache') AND hasTokenCaseInsensitive(Body, 'miss'))", logsSchema()},
		{"phrase", `"rate limit exceeded"`, "positionCaseInsensitive(Body, 'rate limit exceeded') > 0", logsSchema()},
		{"wildcard text", "time*", "Body ILIKE '%time%%'", logsSchema()},
		{"role exact", "service:api-gateway", "ServiceName = 'api-gateway'", logsSchema()},
		{"role ci", "level:error", "lower(SeverityText) = 'error'", logsSchema()},
		{"alias", "severity:WARN", "lower(SeverityText) = 'warn'", logsSchema()},
		{"role wildcard", "service:api*", "ServiceName ILIKE 'api%'", logsSchema()},
		{"role negate", "service:!=ingest", "NOT ServiceName = 'ingest'", logsSchema()},
		{"role exists", "trace:*", "TraceId != ''", logsSchema()},
		{"numeric role", "severity_number:>=17", "SeverityNumber >= 17", logsSchema()},
		{"attribute", "http.status_code:500", "(LogAttributes['http.status_code'] = '500' OR ResourceAttributes['http.status_code'] = '500')", logsSchema()},
		{"attribute exists", "user.id:*", "(mapContains(LogAttributes, 'user.id') OR mapContains(ResourceAttributes, 'user.id'))", logsSchema()},
		{"attribute compare", "http.status_code:>=500", "(toFloat64OrNull(LogAttributes['http.status_code']) >= 500 OR toFloat64OrNull(ResourceAttributes['http.status_code']) >= 500)", logsSchema()},
		{"real column", "ScopeName:otel.sdk", "`ScopeName` = 'otel.sdk'", logsSchema()},
		{"real numeric column", "TraceFlags:1", "`TraceFlags` = 1", logsSchema()},
		{"not", "NOT level:debug", "NOT (lower(SeverityText) = 'debug')", logsSchema()},
		{"dash not", "-level:debug timeout", "(NOT (lower(SeverityText) = 'debug') AND hasTokenCaseInsensitive(Body, 'timeout'))", logsSchema()},
		{"dash after value", `service:checkout -"cache miss"`, "(ServiceName = 'checkout' AND NOT (positionCaseInsensitive(Body, 'cache miss') > 0))", logsSchema()},
		{"dash word after word", "timeout -retry", "(hasTokenCaseInsensitive(Body, 'timeout') AND NOT (hasTokenCaseInsensitive(Body, 'retry')))", logsSchema()},
		{"or", "level:error OR level:fatal", "(lower(SeverityText) = 'error' OR lower(SeverityText) = 'fatal')", logsSchema()},
		{"precedence", "a OR b c", "(hasTokenCaseInsensitive(Body, 'a') OR (hasTokenCaseInsensitive(Body, 'b') AND hasTokenCaseInsensitive(Body, 'c')))", logsSchema()},
		{"parens", "(a OR b) c", "((hasTokenCaseInsensitive(Body, 'a') OR hasTokenCaseInsensitive(Body, 'b')) AND hasTokenCaseInsensitive(Body, 'c'))", logsSchema()},
		{"explicit and", "a AND b", "(hasTokenCaseInsensitive(Body, 'a') AND hasTokenCaseInsensitive(Body, 'b'))", logsSchema()},
		{"quoted field value", `service:"api gateway"`, "ServiceName = 'api gateway'", logsSchema()},
		{"duration ms to ns", "duration:>250", "Duration > 250000000", tracesSchema()},
		{"duration float", "duration:<=0.5", "Duration <= 500000", tracesSchema()},
		{"status ci", "status:ERROR", "lower(StatusCode) = 'error'", tracesSchema()},
		{"hyphen inside word is text", "api-gateway", "positionCaseInsensitive(Body, 'api-gateway') > 0", logsSchema()},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := Compile(c.q, c.schema)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != c.want {
				t.Fatalf("\n q:    %s\n got:  %s\n want: %s", c.q, got, c.want)
			}
		})
	}
}

func jsonLogsSchema() Schema {
	s := logsSchema()
	s.AttributeJSON = map[string]bool{"LogAttributes": true}
	return s
}

func TestCompileJSONAttributes(t *testing.T) {
	cases := []struct{ name, q, want string }{
		{"json value", "http.status_code:500", "(toString(LogAttributes.`http`.`status_code`) = '500' OR ResourceAttributes['http.status_code'] = '500')"},
		{"json exists", "user.id:*", "(has(JSONAllPaths(LogAttributes), 'user.id') OR mapContains(ResourceAttributes, 'user.id'))"},
		{"json compare", "duration_ms:>100", "(toFloat64OrNull(toString(LogAttributes.`duration_ms`)) > 100 OR toFloat64OrNull(ResourceAttributes['duration_ms']) > 100)"},
		{"json wildcard", "http.route:/api*", "(toString(LogAttributes.`http`.`route`) ILIKE '/api%' OR ResourceAttributes['http.route'] ILIKE '/api%')"},
		{"json key with quote", "we`ird:x", "(toString(LogAttributes.`weird`) = 'x' OR ResourceAttributes['we`ird'] = 'x')"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := Compile(c.q, jsonLogsSchema())
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != c.want {
				t.Fatalf("\n q:    %s\n got:  %s\n want: %s", c.q, got, c.want)
			}
		})
	}
}

func TestCompileErrors(t *testing.T) {
	for _, q := range []string{`"unterminated`, "(a OR b", "a)", "AND a", "a OR", "NOT", "service:", "duration:abc", ":value"} {
		if _, err := Compile(q, tracesSchema()); err == nil {
			t.Errorf("expected error for %q", q)
		}
	}
	if _, err := Compile("nosuchfield:x", Schema{TextExpr: "Body"}); err == nil {
		t.Errorf("unknown field without attribute maps must error")
	}
}

func TestCompileEscapesLiterals(t *testing.T) {
	inputs := []string{
		`service:"a' OR 1=1 --"`,
		`'; DROP TABLE otel_logs; --`,
		`level:"x\" OR 1=1"`,
		`http.route:"/a/b%c_d"`,
		"body:*'*",
		`service:x\'y`,
	}
	for _, q := range inputs {
		got, err := Compile(q, logsSchema())
		if err != nil {
			t.Fatalf("%q: %v", q, err)
		}
		// Every single quote in the output must be either a literal delimiter
		// or escaped: count unescaped quotes, they must be even.
		unescaped := 0
		for i := 0; i < len(got); i++ {
			if got[i] == '\'' && (i == 0 || got[i-1] != '\\') {
				unescaped++
			}
		}
		if unescaped%2 != 0 {
			t.Errorf("%q produced an unbalanced literal: %s", q, got)
		}
		if strings.Contains(got, "DROP") {
			// still fine as long as it sits inside a literal; check it is not at the top level
			if !strings.Contains(got, "'") {
				t.Errorf("%q leaked outside a literal: %s", q, got)
			}
		}
	}
}

// stripLiterals removes single-quoted literals (honouring \' escapes) so
// the fuzz invariant can inspect only the structural SQL.
func stripLiterals(sql string) string {
	var b strings.Builder
	in := false
	for i := 0; i < len(sql); i++ {
		c := sql[i]
		if in {
			if c == '\\' && i+1 < len(sql) {
				i++
				continue
			}
			if c == '\'' {
				in = false
			}
			continue
		}
		if c == '\'' {
			in = true
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

func FuzzCompile(f *testing.F) {
	for _, seed := range []string{"a", "service:x", `"p q"`, "(a OR b) -c", "k:>1", "*", `"`, "(", ")", "a:b:c", ";0", "a'b", `a\'b`} {
		f.Add(seed)
	}
	s := logsSchema()
	f.Fuzz(func(t *testing.T, q string) {
		got, err := Compile(q, s)
		if err != nil {
			return
		}
		residue := stripLiterals(got)
		if strings.Contains(residue, "'") {
			t.Fatalf("unbalanced literal: %q -> %s", q, got)
		}
		for _, bad := range []string{";", "--", "/*", "\n"} {
			if strings.Contains(residue, bad) {
				t.Fatalf("%q leaked %q outside a literal: %s", q, bad, got)
			}
		}
	})
}
