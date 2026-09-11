package telemetry

import (
	"strings"
	"testing"
)

func TestHasNestedPrefix(t *testing.T) {
	cols := map[string]string{
		"Timestamp":        "DateTime64(9)",
		"Events.Name":      "Array(LowCardinality(String))",
		"Events.Timestamp": "Array(DateTime64(9))",
	}
	if !HasNestedPrefix(cols, "Events") {
		t.Error("Events should be found through its flattened sub-columns")
	}
	if HasNestedPrefix(cols, "Links") {
		t.Error("Links has no sub-column here")
	}
	if HasNestedPrefix(cols, "") {
		t.Error("an empty prefix is never present")
	}
	if HasNestedPrefix(cols, "Timestamp") {
		t.Error("a plain column is not a nested prefix")
	}
}

func TestResolveNested(t *testing.T) {
	cols := map[string]string{
		"Events.Name":   "Array(LowCardinality(String))",
		"Links.TraceId": "Array(String)",
	}

	m := TracesMapping{}
	m.ResolveNested(cols)
	if m.Events != "Events" || m.Links != "Links" {
		t.Errorf("unset prefixes should resolve, got events=%q links=%q", m.Events, m.Links)
	}

	custom := TracesMapping{Events: "SpanEvents", Links: "SpanLinks"}
	custom.ResolveNested(cols)
	if custom.Events != "SpanEvents" || custom.Links != "SpanLinks" {
		t.Error("an explicit mapping must not be overwritten")
	}

	none := TracesMapping{}
	none.ResolveNested(map[string]string{"Timestamp": "DateTime64(9)"})
	if none.Events != "" || none.Links != "" {
		t.Error("nothing to resolve when the table has no nested columns")
	}
}

func TestTraceSpansSQLNestedColumns(t *testing.T) {
	m := DefaultTracesMapping()
	src := &Source{Kind: KindTraces, Database: "default", Table: "otel_traces", Traces: &m}

	sql := TraceSpansSQL(src, "abc")
	for _, want := range []string{
		Quote("Events.Name") + " AS ev_name",
		Quote("Events.Attributes") + " AS ev_attrs",
		Quote("Links.TraceId") + " AS lk_trace",
		Quote("Events.Timestamp"),
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("span SQL is missing %s", want)
		}
	}
	if strings.Contains(sql, "[] AS ev_name") {
		t.Error("mapped events must not fall back to an empty array")
	}

	m.Events, m.Links = "", ""
	off := TraceSpansSQL(src, "abc")
	for _, want := range []string{"[] AS ev_name", "[] AS ev_time", "[] AS lk_trace"} {
		if !strings.Contains(off, want) {
			t.Errorf("unmapped nested columns should yield %s", want)
		}
	}
}
