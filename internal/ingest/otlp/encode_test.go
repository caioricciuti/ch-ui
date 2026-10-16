package otlp

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestAppendJSONString(t *testing.T) {
	for in, want := range map[string]string{
		"plain":            `"plain"`,
		`q"b\s`:            `"q\"b\\s"`,
		"nl\nrt\rtab\t":    `"nl\nrt\rtab\t"`,
		"\b\f\x00\x01\x1f": `"\b\f\u0000\u0001\u001f"`,
		"<html>&":          `"<html>&"`,
		"é 日本 🎉":           `"é 日本 🎉"`,
		"bad\xffbyte":      `"bad` + "\ufffd" + `byte"`,
		"cut\xe6\x97":      `"cut` + "\ufffd\ufffd" + `"`,
		"\x7f":             "\"\x7f\"",
		"":                 `""`,
	} {
		got := string(appendJSONString(nil, in))
		if got != want {
			t.Errorf("appendJSONString(%q) = %s, want %s", in, got, want)
		}
		if !json.Valid([]byte(got)) {
			t.Errorf("appendJSONString(%q) is not valid JSON: %s", in, got)
		}
	}
}

func TestAppendDateTime64(t *testing.T) {
	for _, tc := range []struct {
		in   time.Time
		want string
	}{
		{time.Unix(0, 1700000000123456789), `"1700000000.123456789"`},
		{time.Unix(5, 7), `"5.000000007"`},
		{time.Unix(0, 0), `"0.000000000"`},
		{time.Unix(-10, 0), `"0.000000000"`},
		{time.Date(3000, 1, 1, 0, 0, 0, 0, time.UTC), `"9223372036.854775807"`},
	} {
		if got := string(appendDateTime64(nil, tc.in)); got != tc.want {
			t.Errorf("%v: got %s, want %s", tc.in, got, tc.want)
		}
	}
}

func TestAppendLogJSON(t *testing.T) {
	rows := wantLogRows()
	rows[0].Body = "line1\nline2 \"quoted\" \x01"
	rows[0].SeverityNumber = 300 // out of UInt8 range
	var buf []byte
	for i := range rows {
		buf = AppendLogJSON(buf, &rows[i])
	}
	lines := strings.Split(strings.TrimSuffix(string(buf), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 lines, got %d:\n%s", len(lines), buf)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &got); err != nil {
		t.Fatalf("line 0 is not JSON: %v\n%s", err, lines[0])
	}
	cols := []string{"Timestamp", "TraceId", "SpanId", "TraceFlags", "SeverityText", "SeverityNumber", "ServiceName", "Body",
		"ResourceSchemaUrl", "ResourceAttributes", "ScopeSchemaUrl", "ScopeName", "ScopeVersion", "ScopeAttributes",
		"LogAttributes", "EventName"}
	if len(got) != len(cols) {
		t.Errorf("got %d columns, want %d: %v", len(got), len(cols), got)
	}
	for _, c := range cols {
		if _, ok := got[c]; !ok {
			t.Errorf("missing column %s", c)
		}
	}
	checks := map[string]any{
		"Timestamp": "1700000000.123456789", "TraceId": testTraceHex, "SpanId": testSpanHex, "TraceFlags": 1.0,
		"SeverityNumber": 0.0, "ServiceName": "checkout", "Body": rows[0].Body, "EventName": "ev",
	}
	for k, want := range checks {
		if got[k] != want {
			t.Errorf("%s = %#v, want %#v", k, got[k], want)
		}
	}
	la, _ := got["LogAttributes"].(map[string]any)
	if la["kv"] != `{"a":"q\"","b":2}` || la["arr"] != `[1,"x",false,null]` {
		t.Errorf("LogAttributes = %v", la)
	}
	// Map keys are sorted, so encoding is deterministic.
	if !strings.Contains(lines[0], `"ResourceAttributes":{"host":"h1","service.name":"checkout"}`) {
		t.Errorf("resource attributes not sorted: %s", lines[0])
	}
	// A row with nil maps still writes objects.
	if out := string(AppendLogJSON(nil, &LogRow{})); !strings.Contains(out, `"LogAttributes":{}`) || !json.Valid([]byte(out)) {
		t.Errorf("zero row: %s", out)
	}
}

func TestAppendSpanJSON(t *testing.T) {
	rows := wantSpanRows()
	line := AppendSpanJSON(nil, &rows[0])
	if line[len(line)-1] != '\n' {
		t.Fatal("no trailing newline")
	}
	var got map[string]any
	if err := json.Unmarshal(line, &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, line)
	}
	for k, want := range map[string]any{
		"Timestamp": "1700000000.000000000", "SpanKind": "Server", "StatusCode": "Error", "Duration": 1250000000.0,
		"ParentSpanId": "0000000000000001", "TraceState": "rojo=00f067aa0ba902b7",
	} {
		if got[k] != want {
			t.Errorf("%s = %#v, want %#v", k, got[k], want)
		}
	}
	for k, want := range map[string]string{
		"Events.Timestamp":  `["1700000000.500000000"]`,
		"Events.Name":       `["exception"]`,
		"Events.Attributes": `[{"exception.type":"IOError"}]`,
		"Links.TraceId":     `["` + testTraceHex + `"]`,
		"Links.SpanId":      `["` + testSpanHex + `"]`,
		"Links.TraceState":  `["k=v"]`,
		"Links.Attributes":  `[{"l":"1"}]`,
	} {
		if !strings.Contains(string(line), `"`+k+`":`+want) {
			t.Errorf("missing %s:%s in %s", k, want, line)
		}
	}
	if len(got) != 22 {
		t.Errorf("got %d columns, want 22", len(got))
	}
	empty := string(AppendSpanJSON(nil, &rows[1]))
	if !strings.Contains(empty, `"Events.Timestamp":[],"Events.Name":[],"Events.Attributes":[]`) || !json.Valid([]byte(empty)) {
		t.Errorf("span without events: %s", empty)
	}
	neg := SpanRow{Duration: -5}
	if out := string(AppendSpanJSON(nil, &neg)); !strings.Contains(out, `"Duration":0,`) {
		t.Errorf("negative duration: %s", out)
	}
}

func TestAppendFloat(t *testing.T) {
	// Same output as encoding/json, which is what pcommon AsString mirrors.
	for _, f := range []float64{0, 1, -1.5, 3.14159, 1e20, 1e21, 1e-6, 1e-7, 123456789.125, -2.5e-10} {
		want, _ := json.Marshal(f)
		if got := string(appendFloat(nil, f)); got != string(want) {
			t.Errorf("appendFloat(%v) = %s, want %s", f, got, want)
		}
	}
}

func BenchmarkAppendLogJSON(b *testing.B) {
	rows := wantLogRows()
	buf := make([]byte, 0, 4096)
	b.ReportAllocs()
	for b.Loop() {
		buf = AppendLogJSON(buf[:0], &rows[0])
	}
}

func BenchmarkDecodeLogsProto(b *testing.B) {
	body := testLogsProto()
	b.SetBytes(int64(len(body)))
	b.ReportAllocs()
	for b.Loop() {
		if _, err := DecodeLogsProto(body, Limits{}); err != nil {
			b.Fatal(err)
		}
	}
}
