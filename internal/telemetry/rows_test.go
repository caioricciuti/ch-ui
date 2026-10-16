package telemetry

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestShapeLogRowsParsesMapColumns(t *testing.T) {
	raw := `[{"timestamp":"2026-09-10T12:51:10.225Z","timestamp_ns":"1757508670225000000","severity":"INFO","severity_number":9,
	  "service":"api","body":"hi","trace_id":"t","span_id":"s","resource":{"host.name":"a"},"scope":"{\"k\":\"v\"}","attributes":{"n":5},
	  "scope_name":"otel","event_name":""}]`
	var rows []map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &rows); err != nil {
		t.Fatal(err)
	}
	out := ShapeLogRows(rows)
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
	if row["timestamp_ns"] != "1757508670225000000" {
		t.Errorf("timestamp_ns: %v", row["timestamp_ns"])
	}
}

func TestStringMapFlattensJSONColumns(t *testing.T) {
	got := StringMap(map[string]interface{}{
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

func TestCellReaders(t *testing.T) {
	for _, tc := range []struct {
		in    interface{}
		i     int64
		f     float64
		valid bool
	}{
		{float64(42), 42, 42, true},
		{json.Number("7"), 7, 7, true},
		{"18446744073709551", 18446744073709551, 18446744073709551, true},
		{"nan", 0, 0, false},
		{nil, 0, 0, false},
		{int64(-3), -3, -3, true},
	} {
		if got := Int64(tc.in); got != tc.i {
			t.Errorf("Int64(%#v) = %d, want %d", tc.in, got, tc.i)
		}
		f, ok := Numeric(tc.in)
		if ok != tc.valid || f != tc.f {
			t.Errorf("Numeric(%#v) = %v, %v; want %v, %v", tc.in, f, ok, tc.f, tc.valid)
		}
		if got := Float(tc.in); got != tc.f {
			t.Errorf("Float(%#v) = %v, want %v", tc.in, got, tc.f)
		}
	}
}

func TestLogCursorRoundTrips(t *testing.T) {
	c := LogCursor(map[string]interface{}{"timestamp_ns": "1757508670225000000", "trace_id": "t|1", "span_id": "s", "row_id": "99"})
	got, err := DecodeCursor(c)
	if err != nil {
		t.Fatal(err)
	}
	if got != (Cursor{TimestampNs: "1757508670225000000", TraceID: "t|1", SpanID: "s", RowHash: "99"}) {
		t.Errorf("round trip: %+v", got)
	}
}

func TestDescribeSQLAndColumnTypes(t *testing.T) {
	if _, err := DescribeSQL("otel", "logs; DROP"); err == nil {
		t.Error("invalid identifier must be rejected")
	}
	q, err := DescribeSQL("otel", "otel_logs")
	if err != nil || !strings.HasPrefix(q, "DESCRIBE TABLE ") || !strings.Contains(q, "otel_logs") {
		t.Fatalf("DescribeSQL: %q, %v", q, err)
	}
	cols := ColumnTypes([]map[string]interface{}{{"name": "Body", "type": "String"}, {"name": "", "type": "x"}})
	if len(cols) != 1 || cols["Body"] != "String" {
		t.Errorf("ColumnTypes: %v", cols)
	}
}
