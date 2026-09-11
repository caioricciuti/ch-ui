package handlers

import (
	"encoding/json"
	"testing"

	"github.com/caioricciuti/ch-ui/internal/telemetry"
)

// The Gateway is a concrete type, so the handler is exercised through its
// pure helpers here; the SQL builders are covered in internal/telemetry.

func TestShapeLogRowsParsesMapColumns(t *testing.T) {
	raw := `[{"timestamp":"2026-09-10T12:51:10.225Z","timestamp_ns":"1757508670225000000","severity":"INFO","severity_number":9,
	  "service":"api","body":"hi","trace_id":"t","span_id":"s","resource":{"host.name":"a"},"scope":"{\"k\":\"v\"}","attributes":{"n":5},
	  "scope_name":"otel","event_name":""}]`
	rows := decodeRows(json.RawMessage(raw))
	out := shapeLogRows(rows)
	if len(out) != 1 {
		t.Fatalf("expected 1 row, got %d", len(out))
	}
	row := out[0]
	if row["severity_number"] != int64(9) {
		t.Errorf("severity_number: %v", row["severity_number"])
	}
	if row["resource"].(map[string]string)["host.name"] != "a" {
		t.Errorf("resource map not parsed: %v", row["resource"])
	}
	if row["scope"].(map[string]string)["k"] != "v" {
		t.Errorf("scope string-json not parsed: %v", row["scope"])
	}
	if row["attributes"].(map[string]string)["n"] != "5" {
		t.Errorf("numeric attribute not stringified: %v", row["attributes"])
	}
}

func TestMissingColumnsAndTrim(t *testing.T) {
	m := telemetry.DefaultLogsMapping()
	src := &telemetry.Source{Kind: telemetry.KindLogs, Logs: &m}
	cols := map[string]string{"Timestamp": "DateTime64(9)", "Body": "String", "SeverityText": "String", "ServiceName": "String"}
	missing := missingColumns(src, cols)
	if len(missing) == 0 {
		t.Fatalf("expected missing optional columns")
	}
	trimMissing(src, cols)
	if got := missingColumns(src, cols); len(got) != 0 {
		t.Errorf("after trim, still missing: %v", got)
	}
	if src.Logs.TraceID != "" || src.Logs.LogAttributes != "" {
		t.Errorf("optional columns should be blanked: %+v", src.Logs)
	}
	if src.Logs.Body != "Body" {
		t.Errorf("required columns must stay")
	}
}

func TestLogsRequestParams(t *testing.T) {
	r := logsRequest{From: "a", To: "b", Q: "x", Severity: []string{"ERROR"}, Services: []string{"s"}}
	p := r.params()
	if p.From != "a" || p.To != "b" || p.Q != "x" || len(p.Severity) != 1 || len(p.Services) != 1 {
		t.Errorf("params not copied: %+v", p)
	}
}

func TestStringMapFlattensJSONColumns(t *testing.T) {
	got := stringMap(map[string]interface{}{
		"http":   map[string]interface{}{"method": "GET", "status_code": json.Number("500")},
		"plain":  "v",
		"nested": map[string]interface{}{"a": map[string]interface{}{"b": "c"}},
	})
	want := map[string]string{"http.method": "GET", "http.status_code": "500", "plain": "v", "nested.a.b": "c"}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: got %q want %q (all: %v)", k, got[k], v, got)
		}
	}
	if len(got) != len(want) {
		t.Errorf("extra keys: %v", got)
	}
}
