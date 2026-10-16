package otlp

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

func testTracesProto() []byte {
	resource := pb(nil).msg(fResourceAttributes, kv("service.name", anyStr("api"))).unknowns()
	scope := pb(nil).str(fScopeName, "otel-go").str(fScopeVersion, "1.30")
	event := pb(nil).fixed64(fEventTimeUnixNano, 1700000000500000000).str(fEventName, "exception").
		msg(fEventAttributes, kv("exception.type", anyStr("IOError"))).unknowns()
	link := pb(nil).bytes(fLinkTraceID, testTraceID).bytes(fLinkSpanID, testSpanID).
		str(fLinkTraceState, "k=v").msg(fLinkAttributes, kv("l", anyInt(1))).fixed32(6, 1)
	status := pb(nil).str(fStatusMessage, "boom").varint(fStatusCode, 2).unknowns()
	span := pb(nil).
		bytes(fSpanTraceID, testTraceID).
		bytes(fSpanSpanID, testSpanID).
		str(fSpanTraceState, "rojo=00f067aa0ba902b7").
		bytes(fSpanParentSpanID, []byte{0, 0, 0, 0, 0, 0, 0, 1}).
		fixed32(16, 0x301). // Span.flags, not stored
		str(fSpanName, "GET /users").
		varint(fSpanKind, 2).
		fixed64(fSpanStartTimeUnixNano, 1700000000000000000).
		fixed64(fSpanEndTimeUnixNano, 1700000001250000000).
		msg(fSpanAttributes, kv("http.status_code", anyInt(500))).
		varint(10, 3). // dropped_attributes_count
		msg(fSpanEvents, event).
		varint(12, 0).
		msg(fSpanLinks, link).
		varint(14, 0).
		msg(fSpanStatus, status).
		unknowns()
	// A second span ending before it starts, with an unknown kind.
	span2 := pb(nil).bytes(fSpanSpanID, []byte{1, 2, 3, 4, 5, 6, 7, 8}).str(fSpanName, "odd").varint(fSpanKind, 9).
		fixed64(fSpanStartTimeUnixNano, 2000).fixed64(fSpanEndTimeUnixNano, 1000)
	scopeSpans := pb(nil).msg(fScopeSpansSpans, span).msg(fScopeSpansSpans, span2).msg(fScopeSpansScope, scope).str(3, "schema")
	resourceSpans := pb(nil).msg(fResourceSpansScopeSpans, scopeSpans).msg(fResourceSpansResource, resource).str(fResourceSpansSchemaURL, "s")
	return pb(nil).msg(fExportTraceRequestResourceSpans, resourceSpans).unknowns()
}

const testTracesJSON = `{"resourceSpans": [{
  "resource": {"attributes": [{"key": "service.name", "value": {"stringValue": "api"}}]},
  "scopeSpans": [{
    "scope": {"name": "otel-go", "version": "1.30"},
    "spans": [{
      "traceId": "5b8efff798038103d269b633813fc60c",
      "spanId": "eee19b7ec3c1b174",
      "traceState": "rojo=00f067aa0ba902b7",
      "parentSpanId": "0000000000000001",
      "flags": 769,
      "name": "GET /users",
      "kind": "SPAN_KIND_SERVER",
      "startTimeUnixNano": "1700000000000000000",
      "endTimeUnixNano": "1700000001250000000",
      "attributes": [{"key": "http.status_code", "value": {"intValue": "500"}}],
      "droppedAttributesCount": 3,
      "events": [{"timeUnixNano": "1700000000500000000", "name": "exception",
        "attributes": [{"key": "exception.type", "value": {"stringValue": "IOError"}}]}],
      "links": [{"traceId": "5b8efff798038103d269b633813fc60c", "spanId": "7uGbfsPBsXQ=", "traceState": "k=v",
        "attributes": [{"key": "l", "value": {"intValue": 1}}], "flags": 1}],
      "status": {"message": "boom", "code": "STATUS_CODE_ERROR"}
    }, {
      "span_id": "0102030405060708", "name": "odd", "kind": 9,
      "start_time_unix_nano": 2000, "end_time_unix_nano": "1000"
    }]
  }]
}]}`

func wantSpanRows() []SpanRow {
	res := map[string]string{"service.name": "api"}
	return []SpanRow{{
		Timestamp:          time.Unix(0, 1700000000000000000).UTC(),
		TraceID:            testTraceHex,
		SpanID:             testSpanHex,
		ParentSpanID:       "0000000000000001",
		TraceState:         "rojo=00f067aa0ba902b7",
		SpanName:           "GET /users",
		SpanKind:           "Server",
		ServiceName:        "api",
		ResourceAttributes: res,
		ScopeName:          "otel-go",
		ScopeVersion:       "1.30",
		SpanAttributes:     map[string]string{"http.status_code": "500"},
		Duration:           1250000000,
		StatusCode:         "Error",
		StatusMessage:      "boom",
		Events: SpanEvents{
			Timestamp:  []time.Time{time.Unix(0, 1700000000500000000).UTC()},
			Name:       []string{"exception"},
			Attributes: []map[string]string{{"exception.type": "IOError"}},
		},
		Links: SpanLinks{
			TraceID:    []string{testTraceHex},
			SpanID:     []string{testSpanHex},
			TraceState: []string{"k=v"},
			Attributes: []map[string]string{{"l": "1"}},
		},
	}, {
		Timestamp:          time.Unix(0, 2000).UTC(),
		SpanID:             "0102030405060708",
		SpanName:           "odd",
		SpanKind:           "Unspecified",
		ServiceName:        "api",
		ResourceAttributes: res,
		ScopeName:          "otel-go",
		ScopeVersion:       "1.30",
		SpanAttributes:     map[string]string{},
		StatusCode:         "Unset",
	}}
}

func TestDecodeTraces(t *testing.T) {
	for _, tc := range []struct {
		name, contentType string
		body              []byte
	}{
		{"protobuf", "application/x-protobuf", testTracesProto()},
		{"json", "application/json", []byte(testTracesJSON)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rows, err := DecodeTraces(tc.body, tc.contentType, Limits{})
			if err != nil {
				t.Fatal(err)
			}
			want := wantSpanRows()
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

func TestSpanEnums(t *testing.T) {
	kinds := map[int64]string{0: "Unspecified", 1: "Internal", 2: "Server", 3: "Client", 4: "Producer", 5: "Consumer", 6: "Unspecified", -1: "Unspecified"}
	for v, want := range kinds {
		if got := spanKindString(v); got != want {
			t.Errorf("kind %d = %q, want %q", v, got, want)
		}
	}
	codes := map[int64]string{0: "Unset", 1: "Ok", 2: "Error", 3: "Unset"}
	for v, want := range codes {
		if got := statusCodeString(v); got != want {
			t.Errorf("status %d = %q, want %q", v, got, want)
		}
	}
	for name, want := range map[string]int64{
		"SEVERITY_NUMBER_UNSPECIFIED": 0, "SEVERITY_NUMBER_TRACE": 1, "SEVERITY_NUMBER_DEBUG": 5,
		"SEVERITY_NUMBER_INFO": 9, "SEVERITY_NUMBER_INFO4": 12, "SEVERITY_NUMBER_WARN": 13,
		"SEVERITY_NUMBER_ERROR": 17, "SEVERITY_NUMBER_FATAL4": 24,
	} {
		if got := severityNumberByName[name]; got != want {
			t.Errorf("%s = %d, want %d", name, got, want)
		}
	}
}

func TestSpanEventLinkLimits(t *testing.T) {
	span := pb(nil)
	for range 5 {
		span = span.msg(fSpanEvents, pb(nil).str(fEventName, "e")).msg(fSpanLinks, pb(nil).str(fLinkTraceState, "s"))
	}
	body := pb(nil).msg(1, pb(nil).msg(fResourceSpansScopeSpans, pb(nil).msg(fScopeSpansSpans, span)))
	rows, err := DecodeTracesProto(body, Limits{MaxEvents: 2, MaxLinks: 3})
	if err != nil {
		t.Fatal(err)
	}
	ev, lk := rows[0].Events, rows[0].Links
	if len(ev.Name) != 2 || len(ev.Timestamp) != 2 || len(ev.Attributes) != 2 {
		t.Errorf("events not cut to 2: %+v", ev)
	}
	if len(lk.TraceID) != 3 || len(lk.SpanID) != 3 || len(lk.TraceState) != 3 || len(lk.Attributes) != 3 {
		t.Errorf("links not cut to 3: %+v", lk)
	}

	js := `{"resourceSpans":[{"scopeSpans":[{"spans":[{"events":[{},{},{}],"links":[{},{},{},{}]}]}]}]}`
	jrows, err := DecodeTracesJSON([]byte(js), Limits{MaxEvents: 2, MaxLinks: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(jrows[0].Events.Name) != 2 || len(jrows[0].Links.SpanID) != 3 {
		t.Errorf("JSON events/links not cut: %+v", jrows[0])
	}
}

func TestDecodeTracesErrors(t *testing.T) {
	span := func(f pb) []byte {
		return pb(nil).msg(1, pb(nil).msg(fResourceSpansScopeSpans, pb(nil).msg(fScopeSpansSpans, f)))
	}
	for name, body := range map[string][]byte{
		"short parent id":   span(pb(nil).bytes(fSpanParentSpanID, []byte{1})),
		"link bad trace id": span(pb(nil).msg(fSpanLinks, pb(nil).bytes(fLinkTraceID, []byte{1, 2}))),
		"kind as bytes":     span(pb(nil).str(fSpanKind, "SERVER")),
		"status as varint":  span(pb(nil).varint(fSpanStatus, 1)),
	} {
		if _, err := DecodeTracesProto(body, Limits{}); !errors.Is(err, ErrMalformed) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if _, err := DecodeTracesJSON([]byte(`{"resourceSpans":[{"scopeSpans":[{"spans":[{"status":"bad"}]}]}]}`), Limits{}); !errors.Is(err, ErrMalformed) {
		t.Errorf("JSON status as string: err = %v", err)
	}
}
