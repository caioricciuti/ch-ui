package otlp

import (
	"encoding/json"
	"testing"
)

// FuzzDecode feeds arbitrary bytes to every decoder. Nothing may panic, and
// whatever decodes must encode to valid JSON lines.
func FuzzDecode(f *testing.F) {
	f.Add(testLogsProto())
	f.Add(testTracesProto())
	f.Add([]byte(testLogsJSON))
	f.Add([]byte(testTracesJSON))
	f.Add([]byte{0x0b, 0x0b, 0x0c, 0x0c})
	f.Add([]byte{0x0a, 0xff, 0xff, 0xff, 0xff, 0x0f})
	f.Add([]byte(`{"resourceLogs":[{"scopeLogs":[{"logRecords":[{"body":{"arrayValue":{"values":[{"arrayValue":{}}]}}}]}]}]}`))
	lim := Limits{MaxRecords: 64, MaxAttributes: 8, MaxValueBytes: 64, MaxBodyBytes: 128, MaxEvents: 4, MaxLinks: 4, MaxDepth: 4}
	f.Fuzz(func(t *testing.T, body []byte) {
		check := func(line []byte) {
			if !json.Valid(line) {
				t.Fatalf("invalid JSON line: %q", line)
			}
		}
		if rows, err := DecodeLogsProto(body, lim); err == nil {
			for i := range rows {
				check(AppendLogJSON(nil, &rows[i]))
			}
		}
		if rows, err := DecodeTracesProto(body, lim); err == nil {
			for i := range rows {
				check(AppendSpanJSON(nil, &rows[i]))
			}
		}
		if rows, err := DecodeLogsJSON(body, lim); err == nil {
			for i := range rows {
				check(AppendLogJSON(nil, &rows[i]))
			}
		}
		if rows, err := DecodeTracesJSON(body, lim); err == nil {
			for i := range rows {
				check(AppendSpanJSON(nil, &rows[i]))
			}
		}
	})
}
