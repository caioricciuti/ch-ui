package otlp

import (
	"bytes"
	"errors"
	"reflect"
	"testing"
	"time"
)

var (
	testTraceID = []byte{0x5b, 0x8e, 0xff, 0xf7, 0x98, 0x03, 0x81, 0x03, 0xd2, 0x69, 0xb6, 0x33, 0x81, 0x3f, 0xc6, 0x0c}
	testSpanID  = []byte{0xee, 0xe1, 0x9b, 0x7e, 0xc3, 0xc1, 0xb1, 0x74}
)

const (
	testTraceHex = "5b8efff798038103d269b633813fc60c"
	testSpanHex  = "eee19b7ec3c1b174"
)

// testLogsProto builds an ExportLogsServiceRequest with one resource, one
// scope and two records. The resource and scope come after the records that
// use them, and every level carries unknown fields of every wire type.
func testLogsProto() []byte {
	resource := pb(nil).
		msg(fResourceAttributes, kv("service.name", anyStr("checkout"))).
		msg(fResourceAttributes, kv("host", anyStr("h1"))).
		unknowns()
	scope := pb(nil).str(fScopeName, "lib").str(fScopeVersion, "1.2").
		msg(fScopeAttributes, kv("sa", anyInt(-3))).unknowns()
	rec1 := pb(nil).
		fixed64(fLogTimeUnixNano, 1700000000123456789).
		fixed64(fLogObservedTimeUnixNano, 1700000009000000000).
		varint(fLogSeverityNumber, 17).
		str(fLogSeverityText, "ERROR").
		msg(fLogBody, anyStr("boom")).
		msg(fLogAttributes, kv("a", anyBool(true))).
		msg(fLogAttributes, kv("i", anyInt(-42))).
		msg(fLogAttributes, kv("d", anyDouble(1.5))).
		msg(fLogAttributes, kv("big", anyDouble(1e21))).
		msg(fLogAttributes, kv("small", anyDouble(1e-7))).
		msg(fLogAttributes, kv("b", anyBytes([]byte{0, 1, 2}))).
		msg(fLogAttributes, kv("arr", anyArray(anyInt(1), anyStr("x"), anyBool(false), pb(nil)))).
		msg(fLogAttributes, kv("kv", anyKV(kv("b", anyInt(1)), kv("a", anyStr(`q"`)), kv("b", anyInt(2))))).
		msg(fLogAttributes, kv("empty", pb(nil))).
		fixed32(fLogFlags, 0x101).
		bytes(fLogTraceID, testTraceID).
		bytes(fLogSpanID, testSpanID).
		str(fLogEventName, "ev").
		unknowns()
	rec2 := pb(nil).
		fixed64(fLogObservedTimeUnixNano, 1700000001000000000).
		msg(fLogBody, anyKV(kv("msg", anyStr("hi")), kv("n", anyArray(anyInt(1))))).
		bytes(fLogTraceID, make([]byte, 16))
	scopeLogs := pb(nil).
		msg(fScopeLogsLogRecords, rec1).
		msg(fScopeLogsLogRecords, rec2).
		msg(fScopeLogsScope, scope).
		str(fScopeLogsSchemaURL, "https://schema/scope").
		unknowns()
	resourceLogs := pb(nil).
		msg(fResourceLogsScopeLogs, scopeLogs).
		msg(fResourceLogsResource, resource).
		str(fResourceLogsSchemaURL, "https://schema/resource").
		unknowns()
	return pb(nil).msg(fExportLogsRequestResourceLogs, resourceLogs).unknowns()
}

// testLogsJSON is the same request as testLogsProto in OTLP JSON, mixing
// lowerCamel and proto field names, string and number int64s, enum names.
const testLogsJSON = `{
  "resourceLogs": [{
    "resource": {"attributes": [
      {"key": "service.name", "value": {"stringValue": "checkout"}},
      {"key": "host", "value": {"stringValue": "h1"}}
    ], "droppedAttributesCount": 0},
    "schemaUrl": "https://schema/resource",
    "scope_logs": [{
      "scope": {"name": "lib", "version": "1.2", "attributes": [{"key": "sa", "value": {"intValue": "-3"}}]},
      "schema_url": "https://schema/scope",
      "logRecords": [{
        "timeUnixNano": "1700000000123456789",
        "observed_time_unix_nano": 1700000009000000000,
        "severityNumber": "SEVERITY_NUMBER_ERROR",
        "severityText": "ERROR",
        "body": {"stringValue": "boom"},
        "attributes": [
          {"key": "a", "value": {"boolValue": true}},
          {"key": "i", "value": {"intValue": -42}},
          {"key": "d", "value": {"doubleValue": 1.5}},
          {"key": "big", "value": {"doubleValue": 1e21}},
          {"key": "small", "value": {"doubleValue": "1e-7"}},
          {"key": "b", "value": {"bytesValue": "AAEC"}},
          {"key": "arr", "value": {"arrayValue": {"values": [{"intValue": "1"}, {"stringValue": "x"}, {"boolValue": false}, {}]}}},
          {"key": "kv", "value": {"kvlistValue": {"values": [{"key": "b", "value": {"intValue": 1}}, {"key": "a", "value": {"stringValue": "q\""}}, {"key": "b", "value": {"intValue": 2}}]}}},
          {"key": "empty", "value": {}}
        ],
        "flags": 257,
        "traceId": "5B8EFFF798038103D269B633813FC60C",
        "spanId": "eee19b7ec3c1b174",
        "eventName": "ev",
        "someFutureField": {"nested": [1, 2, 3]}
      }, {
        "observedTimeUnixNano": "1700000001000000000",
        "body": {"kvlistValue": {"values": [{"key": "msg", "value": {"stringValue": "hi"}}, {"key": "n", "value": {"arrayValue": {"values": [{"intValue": 1}]}}}]}},
        "traceId": "00000000000000000000000000000000",
        "spanId": ""
      }]
    }]
  }],
  "unknownTopLevel": true
}`

func wantLogRows() []LogRow {
	res := map[string]string{"service.name": "checkout", "host": "h1"}
	scope := map[string]string{"sa": "-3"}
	base := LogRow{
		ServiceName: "checkout", ResourceSchemaURL: "https://schema/resource", ResourceAttributes: res,
		ScopeSchemaURL: "https://schema/scope", ScopeName: "lib", ScopeVersion: "1.2", ScopeAttributes: scope,
	}
	r1 := base
	r1.Timestamp = time.Unix(0, 1700000000123456789).UTC()
	r1.TraceID = testTraceHex
	r1.SpanID = testSpanHex
	r1.TraceFlags = 1
	r1.SeverityText = "ERROR"
	r1.SeverityNumber = 17
	r1.Body = "boom"
	r1.EventName = "ev"
	r1.LogAttributes = map[string]string{
		"a": "true", "i": "-42", "d": "1.5", "big": "1e+21", "small": "1e-7", "b": "AAEC",
		"arr": `[1,"x",false,null]`, "kv": `{"a":"q\"","b":2}`, "empty": "",
	}
	r2 := base
	r2.Timestamp = time.Unix(0, 1700000001000000000).UTC()
	r2.Body = `{"msg":"hi","n":[1]}`
	r2.LogAttributes = map[string]string{}
	return []LogRow{r1, r2}
}

func TestDecodeLogs(t *testing.T) {
	for _, tc := range []struct {
		name, contentType string
		body              []byte
	}{
		{"protobuf", "application/x-protobuf", testLogsProto()},
		{"json", "application/json; charset=utf-8", []byte(testLogsJSON)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rows, err := DecodeLogs(tc.body, tc.contentType, Limits{})
			if err != nil {
				t.Fatal(err)
			}
			want := wantLogRows()
			if len(rows) != len(want) {
				t.Fatalf("got %d rows, want %d", len(rows), len(want))
			}
			for i := range want {
				if !reflect.DeepEqual(rows[i], want[i]) {
					t.Errorf("row %d:\n got %+v\nwant %+v", i, rows[i], want[i])
				}
			}
		})
	}
}

func TestDecodeLogsEmpty(t *testing.T) {
	rows, err := DecodeLogsProto(nil, Limits{})
	if err != nil || len(rows) != 0 {
		t.Fatalf("empty body: rows=%v err=%v", rows, err)
	}
	rows, err = DecodeLogsJSON([]byte(`{}`), Limits{})
	if err != nil || len(rows) != 0 {
		t.Fatalf("empty JSON: rows=%v err=%v", rows, err)
	}
	// A record with nothing set still gets non-nil maps and empty strings.
	body := pb(nil).msg(1, pb(nil).msg(fResourceLogsScopeLogs, pb(nil).msg(fScopeLogsLogRecords, nil)))
	rows, err = DecodeLogsProto(body, Limits{})
	if err != nil || len(rows) != 1 {
		t.Fatalf("bare record: rows=%v err=%v", rows, err)
	}
	r := rows[0]
	if r.ResourceAttributes == nil || r.ScopeAttributes == nil || r.LogAttributes == nil || !r.Timestamp.Equal(time.Unix(0, 0)) {
		t.Errorf("bare record: %+v", r)
	}
}

func TestDecodeErrors(t *testing.T) {
	record := func(f pb) []byte {
		return pb(nil).msg(1, pb(nil).msg(fResourceLogsScopeLogs, pb(nil).msg(fScopeLogsLogRecords, f)))
	}
	deep := anyStr("leaf")
	for range 20 {
		deep = anyArray(deep)
	}
	for _, tc := range []struct {
		name string
		body []byte
		want error
	}{
		{"truncated varint", []byte{0x0a, 0x80}, ErrMalformed},
		{"length past end", []byte{0x0a, 0x05, 0x01}, ErrMalformed},
		{"field number zero", []byte{0x02, 0x00}, ErrMalformed},
		{"stray end group", []byte{0x0c}, ErrMalformed},
		{"wire type 6", []byte{0x0e}, ErrMalformed},
		{"unterminated group", []byte{0x0b, 0x08, 0x01}, ErrMalformed},
		{"mismatched group", []byte{0x0b, 0x14}, ErrMalformed},
		{"varint too long", append([]byte{0x08}, bytes.Repeat([]byte{0xff}, 11)...), ErrMalformed},
		{"resource_logs as varint", pb(nil).varint(1, 5), ErrMalformed},
		{"short trace id", record(pb(nil).bytes(fLogTraceID, []byte{1, 2, 3})), ErrMalformed},
		{"long span id", record(pb(nil).bytes(fLogSpanID, make([]byte, 9))), ErrMalformed},
		{"time as varint", record(pb(nil).varint(fLogTimeUnixNano, 1)), ErrMalformed},
		{"too deep", record(pb(nil).msg(fLogBody, deep)), ErrMalformed},
		{"bad nested attribute", record(pb(nil).msg(fLogAttributes, kv("k", pb(nil).bytes(fAnyArrayValue, []byte{0x0a, 0x09})))), ErrMalformed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := DecodeLogsProto(tc.body, Limits{})
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestDecodeJSONErrors(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"not JSON", `{`},
		{"array root", `[]`},
		{"trailing data", `{} {}`},
		{"resourceLogs not array", `{"resourceLogs": {}}`},
		{"bad trace id", `{"resourceLogs":[{"scopeLogs":[{"logRecords":[{"traceId":"xyz"}]}]}]}`},
		{"short trace id", `{"resourceLogs":[{"scopeLogs":[{"logRecords":[{"traceId":"abcd"}]}]}]}`},
		{"bad time", `{"resourceLogs":[{"scopeLogs":[{"logRecords":[{"timeUnixNano":"soon"}]}]}]}`},
		{"negative time", `{"resourceLogs":[{"scopeLogs":[{"logRecords":[{"timeUnixNano":-1}]}]}]}`},
		{"bool as string", `{"resourceLogs":[{"scopeLogs":[{"logRecords":[{"body":{"boolValue":"yes"}}]}]}]}`},
		{"bad base64", `{"resourceLogs":[{"scopeLogs":[{"logRecords":[{"body":{"bytesValue":"!!"}}]}]}]}`},
		{"attributes not array", `{"resourceLogs":[{"scopeLogs":[{"logRecords":[{"attributes":{}}]}]}]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := DecodeLogsJSON([]byte(tc.body), Limits{}); !errors.Is(err, ErrMalformed) {
				t.Fatalf("err = %v, want ErrMalformed", err)
			}
		})
	}
}

// Every prefix of a valid payload must decode or fail cleanly.
func TestDecodeTruncatedPrefixes(t *testing.T) {
	body := testLogsProto()
	for i := range body {
		_, _ = DecodeLogsProto(body[:i], Limits{})
	}
	tb := testTracesProto()
	for i := range tb {
		_, _ = DecodeTracesProto(tb[:i], Limits{})
	}
	jb := []byte(testLogsJSON)
	for i := range jb {
		if _, err := DecodeLogsJSON(jb[:i], Limits{}); err == nil {
			t.Fatalf("prefix %d of the JSON decoded without error", i)
		}
	}
}

func TestLimits(t *testing.T) {
	recs := pb(nil)
	for range 3 {
		recs = recs.msg(fScopeLogsLogRecords, nil)
	}
	body := pb(nil).msg(1, pb(nil).msg(fResourceLogsScopeLogs, recs))
	if _, err := DecodeLogsProto(body, Limits{MaxRecords: 2}); !errors.Is(err, ErrTooManyRecords) {
		t.Errorf("3 records over a limit of 2: err = %v", err)
	}
	if rows, err := DecodeLogsProto(body, Limits{MaxRecords: 3}); err != nil || len(rows) != 3 {
		t.Errorf("3 records under a limit of 3: %d rows, err = %v", len(rows), err)
	}
	if _, err := DecodeLogsJSON([]byte(`{"resourceLogs":[{"scopeLogs":[{"logRecords":[{},{},{}]}]}]}`), Limits{MaxRecords: 2}); !errors.Is(err, ErrTooManyRecords) {
		t.Errorf("JSON 3 records over a limit of 2: err = %v", err)
	}

	// Attribute count: the first two distinct keys win, a later duplicate of
	// a kept key overwrites it, service.name always survives.
	res := pb(nil).
		msg(fResourceAttributes, kv("a", anyStr("1"))).
		msg(fResourceAttributes, kv("b", anyStr("2"))).
		msg(fResourceAttributes, kv("c", anyStr("3"))).
		msg(fResourceAttributes, kv("a", anyStr("4"))).
		msg(fResourceAttributes, kv("service.name", anyStr("svc")))
	body = pb(nil).msg(1, pb(nil).msg(fResourceLogsResource, res).
		msg(fResourceLogsScopeLogs, pb(nil).msg(fScopeLogsLogRecords, nil)))
	rows, err := DecodeLogsProto(body, Limits{MaxAttributes: 2})
	if err != nil {
		t.Fatal(err)
	}
	if want := map[string]string{"a": "4", "b": "2", "service.name": "svc"}; !reflect.DeepEqual(rows[0].ResourceAttributes, want) {
		t.Errorf("resource attributes = %v, want %v", rows[0].ResourceAttributes, want)
	}
	if rows[0].ServiceName != "svc" {
		t.Errorf("service = %q", rows[0].ServiceName)
	}

	// Sizes: keys, values and Body are cut on a UTF-8 boundary. "é" is two
	// bytes, so cutting "abcé" at 4 leaves "abc".
	rec := pb(nil).
		msg(fLogAttributes, kv("abcé", anyStr("abcé"))).
		msg(fLogAttributes, kv("k", anyArray(anyStr("abc")))).
		str(fLogSeverityText, "INFORMATION").
		msg(fLogBody, anyStr("ééé"))
	body = pb(nil).msg(1, pb(nil).msg(fResourceLogsScopeLogs, pb(nil).msg(fScopeLogsLogRecords, rec)))
	rows, err = DecodeLogsProto(body, Limits{MaxValueBytes: 4, MaxBodyBytes: 5})
	if err != nil {
		t.Fatal(err)
	}
	r := rows[0]
	if want := map[string]string{"abc": "abc", "k": `["ab`}; !reflect.DeepEqual(r.LogAttributes, want) {
		t.Errorf("log attributes = %q, want %q", r.LogAttributes, want)
	}
	if r.SeverityText != "INFO" {
		t.Errorf("severity text = %q", r.SeverityText)
	}
	if r.Body != "éé" {
		t.Errorf("body = %q, want two runes", r.Body)
	}
}

func TestParseContentType(t *testing.T) {
	for ct, want := range map[string]Encoding{
		"application/x-protobuf":          EncodingProtobuf,
		"application/protobuf":            EncodingProtobuf,
		"Application/JSON; charset=utf-8": EncodingJSON,
	} {
		if got, err := ParseContentType(ct); err != nil || got != want {
			t.Errorf("%q: got %v, %v", ct, got, err)
		}
	}
	for _, ct := range []string{"", "text/plain", "application/grpc"} {
		if _, err := ParseContentType(ct); !errors.Is(err, ErrUnsupportedContentType) {
			t.Errorf("%q: err = %v", ct, err)
		}
	}
	if _, err := DecodeLogs(nil, "text/plain", Limits{}); !errors.Is(err, ErrUnsupportedContentType) {
		t.Errorf("DecodeLogs text/plain: %v", err)
	}
}
