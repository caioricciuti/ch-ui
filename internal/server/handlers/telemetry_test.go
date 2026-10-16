package handlers

import (
	"testing"

	"github.com/caioricciuti/ch-ui/internal/telemetry"
)

// The Gateway is a concrete type, so the handler is exercised through its
// pure helpers here; the SQL builders are covered in internal/telemetry.

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

func TestAnchorOffsetMatchesRowIdentity(t *testing.T) {
	after := []map[string]interface{}{
		{"timestamp_ns": "100", "row_id": "a"},
		{"timestamp_ns": "100", "row_id": "b"},
		{"timestamp_ns": "100", "row_id": "c"},
		{"timestamp_ns": "200", "row_id": "d"},
	}
	if got := anchorOffset(after, "100", "c"); got != 2 {
		t.Errorf("tied rows: anchor must be the matching row, got %d", got)
	}
	if got := anchorOffset(after, "100", ""); got != 0 {
		t.Errorf("no row id: first row at the timestamp, got %d", got)
	}
	if got := anchorOffset(after, "100", "zzz"); got != 0 {
		t.Errorf("row not on the page: fall back to the first row, got %d", got)
	}
}
