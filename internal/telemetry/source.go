// Package telemetry models observability "sources": which ClickHouse
// tables hold OpenTelemetry logs, traces and metrics, and how their
// columns map onto the roles the Telemetry pages understand.
package telemetry

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode"

	"github.com/caioricciuti/ch-ui/internal/telemetry/query"
)

// Kind is the signal a source holds.
type Kind string

const (
	KindLogs    Kind = "logs"
	KindTraces  Kind = "traces"
	KindMetrics Kind = "metrics"
)

// LogsMapping names the columns of a logs table by role. Defaults match
// the OpenTelemetry ClickHouse exporter.
type LogsMapping struct {
	Timestamp          string `json:"timestamp"`
	Body               string `json:"body"`
	Severity           string `json:"severity"`
	SeverityNumber     string `json:"severity_number"`
	Service            string `json:"service"`
	TraceID            string `json:"trace_id"`
	SpanID             string `json:"span_id"`
	ResourceAttributes string `json:"resource_attributes"`
	ScopeAttributes    string `json:"scope_attributes"`
	LogAttributes      string `json:"log_attributes"`
	ScopeName          string `json:"scope_name"`
	EventName          string `json:"event_name"`
}

// TracesMapping names the columns of a spans table by role.
type TracesMapping struct {
	Timestamp    string `json:"timestamp"`
	TraceID      string `json:"trace_id"`
	SpanID       string `json:"span_id"`
	ParentSpanID string `json:"parent_span_id"`
	SpanName     string `json:"span_name"`
	SpanKind     string `json:"span_kind"`
	Service      string `json:"service"`
	Duration     string `json:"duration"`
	// DurationUnit is ns, us, ms or s. The exporter writes nanoseconds.
	DurationUnit       string `json:"duration_unit"`
	StatusCode         string `json:"status_code"`
	StatusMessage      string `json:"status_message"`
	ResourceAttributes string `json:"resource_attributes"`
	SpanAttributes     string `json:"span_attributes"`
	// Events and Links are Nested column prefixes (Events.Timestamp …).
	Events string `json:"events"`
	Links  string `json:"links"`
}

// MetricsTables names the per-type metric tables (empty = not present).
type MetricsTables struct {
	Gauge                string `json:"gauge"`
	Sum                  string `json:"sum"`
	Histogram            string `json:"histogram"`
	ExponentialHistogram string `json:"exponential_histogram"`
	Summary              string `json:"summary"`
}

// Source is one configured signal on one connection.
type Source struct {
	ID           string `json:"id"`
	ConnectionID string `json:"connection_id"`
	Kind         Kind   `json:"kind"`
	Name         string `json:"name"`
	Database     string `json:"database"`
	// Table is the logs or traces table; metrics use Tables.
	Table  string         `json:"table"`
	Tables *MetricsTables `json:"tables,omitempty"`
	Logs   *LogsMapping   `json:"logs,omitempty"`
	Traces *TracesMapping `json:"traces,omitempty"`
	// Correlated sources: a logs source points at its traces source and
	// vice versa, so the UI can jump between them.
	CorrelatedLogs   string `json:"correlated_logs,omitempty"`
	CorrelatedTraces string `json:"correlated_traces,omitempty"`
	Enabled          bool   `json:"enabled"`
	CreatedBy        string `json:"created_by"`
	CreatedAt        string `json:"created_at"`
	UpdatedAt        string `json:"updated_at"`
}

// DefaultLogsMapping is the exporter's otel_logs layout.
func DefaultLogsMapping() LogsMapping {
	return LogsMapping{
		Timestamp: "Timestamp", Body: "Body", Severity: "SeverityText", SeverityNumber: "SeverityNumber",
		Service: "ServiceName", TraceID: "TraceId", SpanID: "SpanId",
		ResourceAttributes: "ResourceAttributes", ScopeAttributes: "ScopeAttributes", LogAttributes: "LogAttributes",
		ScopeName: "ScopeName", EventName: "EventName",
	}
}

// DefaultTracesMapping is the exporter's otel_traces layout.
func DefaultTracesMapping() TracesMapping {
	return TracesMapping{
		Timestamp: "Timestamp", TraceID: "TraceId", SpanID: "SpanId", ParentSpanID: "ParentSpanId",
		SpanName: "SpanName", SpanKind: "SpanKind", Service: "ServiceName",
		Duration: "Duration", DurationUnit: "ns", StatusCode: "StatusCode", StatusMessage: "StatusMessage",
		ResourceAttributes: "ResourceAttributes", SpanAttributes: "SpanAttributes",
		Events: "Events", Links: "Links",
	}
}

// DefaultMetricsTables is the exporter's otel_metrics_* layout.
func DefaultMetricsTables() MetricsTables {
	return MetricsTables{
		Gauge: "otel_metrics_gauge", Sum: "otel_metrics_sum", Histogram: "otel_metrics_histogram",
		ExponentialHistogram: "otel_metrics_exponential_histogram", Summary: "otel_metrics_summary",
	}
}

// HasNestedPrefix reports whether a DESCRIBE column map holds any
// sub-column of a Nested prefix. ClickHouse flattens Events Nested(...)
// into Events.Timestamp, Events.Name and so on, so the bare prefix never
// appears as a column in its own right and a plain lookup always misses.
func HasNestedPrefix(cols map[string]string, prefix string) bool {
	if prefix == "" {
		return false
	}
	p := prefix + "."
	for name := range cols {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

// ResolveNested fills the Events and Links prefixes from the table's real
// columns when they are unset. Sources stored before the prefix check
// understood flattening carry an empty value; without this they keep
// returning spans with no events and no links.
func (m *TracesMapping) ResolveNested(cols map[string]string) {
	if m.Events == "" && HasNestedPrefix(cols, "Events") {
		m.Events = "Events"
	}
	if m.Links == "" && HasNestedPrefix(cols, "Links") {
		m.Links = "Links"
	}
}

// ValidIdent reports whether s is a plain ClickHouse identifier (letters,
// digits, underscore, dot for nested columns). Everything that reaches SQL
// as an identifier passes through this.
func ValidIdent(s string) bool {
	if s == "" || len(s) > 128 {
		return false
	}
	for _, r := range s {
		if !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '.') {
			return false
		}
	}
	return true
}

// Quote backtick-quotes an identifier that passed ValidIdent.
func Quote(s string) string {
	return "`" + strings.ReplaceAll(s, "`", "") + "`"
}

// IsJSONColumn reports whether a DESCRIBE type is the exporter's JSON
// attribute column (the `json: true` mode) rather than a Map.
func IsJSONColumn(typ string) bool {
	t := strings.TrimSpace(typ)
	t = strings.TrimPrefix(t, "Nullable(")
	return strings.HasPrefix(t, "JSON")
}

// AttrAccess renders a string read of attribute `key` from a quoted
// attribute column, for either storage layout.
func AttrAccess(col, key string, json bool) string {
	if json {
		return query.JSONPathAccess(col, key)
	}
	return fmt.Sprintf("%s[%s]", col, Literal(key))
}

// AttrKeys renders the expression listing an attribute column's keys.
func AttrKeys(col string, json bool) string {
	if json {
		return "JSONAllPaths(" + col + ")"
	}
	return "mapKeys(" + col + ")"
}

// jsonFlags marks which quoted attribute columns are JSON typed.
func jsonFlags(columns map[string]string, names ...string) map[string]bool {
	out := map[string]bool{}
	for _, n := range names {
		if n == "" {
			continue
		}
		if IsJSONColumn(columns[n]) {
			out[Quote(n)] = true
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// Validate checks names and identifiers; it does not touch ClickHouse.
func (s *Source) Validate() error {
	if strings.TrimSpace(s.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if !ValidIdent(s.Database) {
		return fmt.Errorf("invalid database name")
	}
	switch s.Kind {
	case KindLogs:
		if s.Logs == nil {
			return fmt.Errorf("logs mapping is required")
		}
		if !ValidIdent(s.Table) {
			return fmt.Errorf("invalid table name")
		}
		for name, v := range map[string]string{
			"timestamp": s.Logs.Timestamp, "body": s.Logs.Body, "severity": s.Logs.Severity, "service": s.Logs.Service,
		} {
			if !ValidIdent(v) {
				return fmt.Errorf("logs mapping: %s must be a column name", name)
			}
		}
		for name, v := range map[string]string{
			"severity_number": s.Logs.SeverityNumber, "trace_id": s.Logs.TraceID, "span_id": s.Logs.SpanID,
			"resource_attributes": s.Logs.ResourceAttributes, "scope_attributes": s.Logs.ScopeAttributes,
			"log_attributes": s.Logs.LogAttributes, "scope_name": s.Logs.ScopeName, "event_name": s.Logs.EventName,
		} {
			if v != "" && !ValidIdent(v) {
				return fmt.Errorf("logs mapping: %s must be a column name", name)
			}
		}
	case KindTraces:
		if s.Traces == nil {
			return fmt.Errorf("traces mapping is required")
		}
		if !ValidIdent(s.Table) {
			return fmt.Errorf("invalid table name")
		}
		for name, v := range map[string]string{
			"timestamp": s.Traces.Timestamp, "trace_id": s.Traces.TraceID, "span_id": s.Traces.SpanID,
			"parent_span_id": s.Traces.ParentSpanID, "span_name": s.Traces.SpanName, "service": s.Traces.Service,
			"duration": s.Traces.Duration,
		} {
			if !ValidIdent(v) {
				return fmt.Errorf("traces mapping: %s must be a column name", name)
			}
		}
		switch s.Traces.DurationUnit {
		case "ns", "us", "ms", "s":
		default:
			return fmt.Errorf("traces mapping: duration_unit must be ns, us, ms or s")
		}
		for name, v := range map[string]string{
			"span_kind": s.Traces.SpanKind, "status_code": s.Traces.StatusCode, "status_message": s.Traces.StatusMessage,
			"resource_attributes": s.Traces.ResourceAttributes, "span_attributes": s.Traces.SpanAttributes,
			"events": s.Traces.Events, "links": s.Traces.Links,
		} {
			if v != "" && !ValidIdent(v) {
				return fmt.Errorf("traces mapping: %s must be a column name", name)
			}
		}
	case KindMetrics:
		if s.Tables == nil {
			return fmt.Errorf("metrics tables are required")
		}
		any := false
		for name, v := range map[string]string{
			"gauge": s.Tables.Gauge, "sum": s.Tables.Sum, "histogram": s.Tables.Histogram,
			"exponential_histogram": s.Tables.ExponentialHistogram, "summary": s.Tables.Summary,
		} {
			if v == "" {
				continue
			}
			any = true
			if !ValidIdent(v) {
				return fmt.Errorf("metrics tables: %s must be a table name", name)
			}
		}
		if !any {
			return fmt.Errorf("metrics source needs at least one table")
		}
	default:
		return fmt.Errorf("unknown source kind %q", s.Kind)
	}
	return nil
}

// RequiredColumns lists the columns a source must have, so the handler
// can check them against DESCRIBE and report what is missing.
func (s *Source) RequiredColumns() []string {
	var out []string
	add := func(vals ...string) {
		for _, v := range vals {
			if v != "" {
				out = append(out, v)
			}
		}
	}
	switch s.Kind {
	case KindLogs:
		m := s.Logs
		add(m.Timestamp, m.Body, m.Severity, m.SeverityNumber, m.Service, m.TraceID, m.SpanID,
			m.ResourceAttributes, m.ScopeAttributes, m.LogAttributes, m.ScopeName, m.EventName)
	case KindTraces:
		m := s.Traces
		add(m.Timestamp, m.TraceID, m.SpanID, m.ParentSpanID, m.SpanName, m.SpanKind, m.Service,
			m.Duration, m.StatusCode, m.StatusMessage, m.ResourceAttributes, m.SpanAttributes)
	}
	return out
}

// DurationScale converts the user's milliseconds into the column's unit.
func (m *TracesMapping) DurationScale() float64 {
	switch m.DurationUnit {
	case "us":
		return 1e3
	case "ms":
		return 1
	case "s":
		return 1e-3
	default:
		return 1e6
	}
}

// QuerySchema builds the search-compiler schema for a logs source.
func (m *LogsMapping) QuerySchema(columns map[string]string) query.Schema {
	roles := map[string]string{
		"level":   Quote(m.Severity),
		"service": Quote(m.Service),
		"body":    Quote(m.Body),
	}
	if m.TraceID != "" {
		roles["trace_id"] = Quote(m.TraceID)
	}
	if m.SpanID != "" {
		roles["span_id"] = Quote(m.SpanID)
	}
	if m.ScopeName != "" {
		roles["scope"] = Quote(m.ScopeName)
	}
	if m.EventName != "" {
		roles["event"] = Quote(m.EventName)
	}
	numeric := map[string]query.NumericField{}
	if m.SeverityNumber != "" {
		numeric["severity_number"] = query.NumericField{Expr: Quote(m.SeverityNumber)}
	}
	var maps []string
	for _, c := range []string{m.LogAttributes, m.ResourceAttributes, m.ScopeAttributes} {
		if c != "" {
			maps = append(maps, Quote(c))
		}
	}
	return query.Schema{
		TextExpr:        Quote(m.Body),
		Roles:           roles,
		CaseInsensitive: map[string]bool{"level": true},
		Numeric:         numeric,
		Aliases: map[string]string{
			"severity": "level", "severitytext": "level", "service_name": "service", "servicename": "service",
			"trace": "trace_id", "traceid": "trace_id", "span": "span_id", "spanid": "span_id", "message": "body",
		},
		AttributeMaps: maps,
		AttributeJSON: jsonFlags(columns, m.LogAttributes, m.ResourceAttributes, m.ScopeAttributes),
		Columns:       columns,
	}
}

// QuerySchema builds the search-compiler schema for a traces source.
func (m *TracesMapping) QuerySchema(columns map[string]string) query.Schema {
	roles := map[string]string{
		"service":   Quote(m.Service),
		"span_name": Quote(m.SpanName),
		"trace_id":  Quote(m.TraceID),
		"span_id":   Quote(m.SpanID),
	}
	if m.SpanKind != "" {
		roles["kind"] = Quote(m.SpanKind)
	}
	if m.StatusCode != "" {
		roles["status"] = Quote(m.StatusCode)
	}
	if m.ParentSpanID != "" {
		roles["parent_span_id"] = Quote(m.ParentSpanID)
	}
	var maps []string
	for _, c := range []string{m.SpanAttributes, m.ResourceAttributes} {
		if c != "" {
			maps = append(maps, Quote(c))
		}
	}
	return query.Schema{
		TextExpr:        Quote(m.SpanName),
		Roles:           roles,
		CaseInsensitive: map[string]bool{"status": true, "kind": true},
		Numeric:         map[string]query.NumericField{"duration": {Expr: Quote(m.Duration), Scale: m.DurationScale()}},
		Aliases: map[string]string{
			"service_name": "service", "name": "span_name", "operation": "span_name", "trace": "trace_id",
			"span": "span_id", "status_code": "status", "span_kind": "kind", "parent": "parent_span_id",
		},
		AttributeMaps: maps,
		AttributeJSON: jsonFlags(columns, m.SpanAttributes, m.ResourceAttributes),
		Columns:       columns,
	}
}

// MarshalConfig serializes the kind-specific part for storage.
func (s *Source) MarshalConfig() (string, error) {
	cfg := map[string]interface{}{}
	switch s.Kind {
	case KindLogs:
		cfg["logs"] = s.Logs
	case KindTraces:
		cfg["traces"] = s.Traces
	case KindMetrics:
		cfg["tables"] = s.Tables
	}
	cfg["correlated_logs"] = s.CorrelatedLogs
	cfg["correlated_traces"] = s.CorrelatedTraces
	raw, err := json.Marshal(cfg)
	return string(raw), err
}

// UnmarshalConfig restores the kind-specific part from storage.
func (s *Source) UnmarshalConfig(raw string) error {
	var cfg struct {
		Logs             *LogsMapping   `json:"logs"`
		Traces           *TracesMapping `json:"traces"`
		Tables           *MetricsTables `json:"tables"`
		CorrelatedLogs   string         `json:"correlated_logs"`
		CorrelatedTraces string         `json:"correlated_traces"`
	}
	if raw == "" {
		return nil
	}
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return err
	}
	s.Logs, s.Traces, s.Tables = cfg.Logs, cfg.Traces, cfg.Tables
	s.CorrelatedLogs, s.CorrelatedTraces = cfg.CorrelatedLogs, cfg.CorrelatedTraces
	return nil
}
