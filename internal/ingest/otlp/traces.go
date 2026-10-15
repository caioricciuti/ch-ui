package otlp

import (
	"time"
)

// Field numbers from opentelemetry/proto/collector/trace/v1/trace_service.proto
// and opentelemetry/proto/trace/v1/trace.proto.
const (
	fExportTraceRequestResourceSpans = 1 // ExportTraceServiceRequest.resource_spans

	fResourceSpansResource   = 1 // ResourceSpans.resource
	fResourceSpansScopeSpans = 2 // ResourceSpans.scope_spans
	fResourceSpansSchemaURL  = 3 // ResourceSpans.schema_url

	fScopeSpansScope = 1 // ScopeSpans.scope
	fScopeSpansSpans = 2 // ScopeSpans.spans

	fSpanTraceID           = 1  // Span.trace_id (bytes)
	fSpanSpanID            = 2  // Span.span_id (bytes)
	fSpanTraceState        = 3  // Span.trace_state
	fSpanParentSpanID      = 4  // Span.parent_span_id (bytes)
	fSpanName              = 5  // Span.name
	fSpanKind              = 6  // Span.kind (enum)
	fSpanStartTimeUnixNano = 7  // Span.start_time_unix_nano (fixed64)
	fSpanEndTimeUnixNano   = 8  // Span.end_time_unix_nano (fixed64)
	fSpanAttributes        = 9  // Span.attributes
	fSpanEvents            = 11 // Span.events
	fSpanLinks             = 13 // Span.links
	fSpanStatus            = 15 // Span.status

	fEventTimeUnixNano = 1 // Span.Event.time_unix_nano (fixed64)
	fEventName         = 2 // Span.Event.name
	fEventAttributes   = 3 // Span.Event.attributes

	fLinkTraceID    = 1 // Span.Link.trace_id (bytes)
	fLinkSpanID     = 2 // Span.Link.span_id (bytes)
	fLinkTraceState = 3 // Span.Link.trace_state
	fLinkAttributes = 4 // Span.Link.attributes

	fStatusMessage = 2 // Status.message (field 1 is reserved)
	fStatusCode    = 3 // Status.code (enum)
)

// SpanEvents is the Events Nested column: parallel arrays, one entry per
// event.
type SpanEvents struct {
	Timestamp  []time.Time
	Name       []string
	Attributes []map[string]string
}

// SpanLinks is the Links Nested column.
type SpanLinks struct {
	TraceID    []string
	SpanID     []string
	TraceState []string
	Attributes []map[string]string
}

// SpanRow is one row of the exporter's otel_traces table. Rows decoded from
// the same resource share its attribute map; treat it as read-only.
type SpanRow struct {
	Timestamp          time.Time
	TraceID            string
	SpanID             string
	ParentSpanID       string
	TraceState         string
	SpanName           string
	SpanKind           string
	ServiceName        string
	ResourceAttributes map[string]string
	ScopeName          string
	ScopeVersion       string
	SpanAttributes     map[string]string
	// Duration is end minus start in nanoseconds, never negative.
	Duration      int64
	StatusCode    string
	StatusMessage string
	Events        SpanEvents
	Links         SpanLinks
}

// spanDuration is end minus start, clamped at zero for spans that end before
// they start (the column is UInt64).
func spanDuration(start, end uint64) int64 {
	if end <= start {
		return 0
	}
	d := end - start
	if d > 1<<63-1 {
		return 1<<63 - 1
	}
	return int64(d)
}

// DecodeTraces decodes an OTLP/HTTP traces export body in the encoding named
// by the Content-Type header.
func DecodeTraces(body []byte, contentType string, lim Limits) ([]SpanRow, error) {
	enc, err := ParseContentType(contentType)
	if err != nil {
		return nil, err
	}
	if enc == EncodingJSON {
		return DecodeTracesJSON(body, lim)
	}
	return DecodeTracesProto(body, lim)
}

// DecodeTracesProto decodes a protobuf ExportTraceServiceRequest.
func DecodeTracesProto(body []byte, lim Limits) ([]SpanRow, error) {
	d := newDecoder(lim)
	var rows []SpanRow
	err := walk(body, func(f field) error {
		if f.num != fExportTraceRequestResourceSpans {
			return nil
		}
		b, err := f.bytesVal()
		if err != nil {
			return err
		}
		rows, err = d.pbResourceSpans(rows, b)
		return err
	})
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// spanContext is what a span inherits from its resource and scope.
type spanContext struct {
	resAttrs map[string]string
	service  string
	scope    pbScope
}

func (d *decoder) pbResourceSpans(rows []SpanRow, b []byte) ([]SpanRow, error) {
	var ctx spanContext
	err := walk(b, func(f field) error {
		if f.num != fResourceSpansResource {
			return nil
		}
		v, err := f.bytesVal()
		if err != nil {
			return err
		}
		ctx.resAttrs, err = d.parseResource(v)
		return err
	})
	if err != nil {
		return nil, err
	}
	if ctx.resAttrs == nil {
		ctx.resAttrs = map[string]string{}
	}
	ctx.service = ctx.resAttrs[serviceNameKey]
	err = walk(b, func(f field) error {
		if f.num != fResourceSpansScopeSpans {
			return nil
		}
		v, err := f.bytesVal()
		if err != nil {
			return err
		}
		rows, err = d.pbScopeSpans(rows, v, ctx)
		return err
	})
	return rows, err
}

func (d *decoder) pbScopeSpans(rows []SpanRow, b []byte, ctx spanContext) ([]SpanRow, error) {
	err := walk(b, func(f field) error {
		if f.num != fScopeSpansScope {
			return nil
		}
		v, err := f.bytesVal()
		if err != nil {
			return err
		}
		ctx.scope, err = d.parseScope(v)
		return err
	})
	if err != nil {
		return nil, err
	}
	err = walk(b, func(f field) error {
		if f.num != fScopeSpansSpans {
			return nil
		}
		v, err := f.bytesVal()
		if err != nil {
			return err
		}
		if err := d.count(); err != nil {
			return err
		}
		row, err := d.pbSpan(v, &ctx)
		if err != nil {
			return err
		}
		rows = append(rows, row)
		return nil
	})
	return rows, err
}

func (d *decoder) pbSpan(b []byte, ctx *spanContext) (SpanRow, error) {
	row := SpanRow{
		ServiceName:        ctx.service,
		ResourceAttributes: ctx.resAttrs,
		ScopeName:          ctx.scope.name,
		ScopeVersion:       ctx.scope.version,
		SpanKind:           spanKindString(0),
		StatusCode:         statusCodeString(0),
	}
	set := newAttrSet(&d.lim)
	var start, end uint64
	err := walk(b, func(f field) error {
		var err error
		var v []byte
		switch f.num {
		case fSpanTraceID:
			if v, err = f.bytesVal(); err == nil {
				row.TraceID, err = idHex(v, 16, "trace_id")
			}
		case fSpanSpanID:
			if v, err = f.bytesVal(); err == nil {
				row.SpanID, err = idHex(v, 8, "span_id")
			}
		case fSpanParentSpanID:
			if v, err = f.bytesVal(); err == nil {
				row.ParentSpanID, err = idHex(v, 8, "parent_span_id")
			}
		case fSpanTraceState:
			v, err = f.bytesVal()
			row.TraceState = d.str(v)
		case fSpanName:
			v, err = f.bytesVal()
			row.SpanName = d.str(v)
		case fSpanKind:
			var n uint64
			n, err = f.varintVal()
			row.SpanKind = spanKindString(int64(int32(n)))
		case fSpanStartTimeUnixNano:
			start, err = f.fixed64Val()
		case fSpanEndTimeUnixNano:
			end, err = f.fixed64Val()
		case fSpanAttributes:
			if v, err = f.bytesVal(); err == nil {
				err = d.pbAttr(&set, v)
			}
		case fSpanEvents:
			if v, err = f.bytesVal(); err == nil && len(row.Events.Name) < d.lim.MaxEvents {
				err = d.pbEvent(&row.Events, v)
			}
		case fSpanLinks:
			if v, err = f.bytesVal(); err == nil && len(row.Links.SpanID) < d.lim.MaxLinks {
				err = d.pbLink(&row.Links, v)
			}
		case fSpanStatus:
			if v, err = f.bytesVal(); err == nil {
				err = d.pbStatus(&row, v)
			}
		}
		return err
	})
	row.Timestamp = unixNano(start)
	row.Duration = spanDuration(start, end)
	row.SpanAttributes = set.m
	return row, err
}

func (d *decoder) pbEvent(ev *SpanEvents, b []byte) error {
	var ts uint64
	var name string
	set := newAttrSet(&d.lim)
	err := walk(b, func(f field) error {
		var err error
		var v []byte
		switch f.num {
		case fEventTimeUnixNano:
			ts, err = f.fixed64Val()
		case fEventName:
			v, err = f.bytesVal()
			name = d.str(v)
		case fEventAttributes:
			if v, err = f.bytesVal(); err == nil {
				err = d.pbAttr(&set, v)
			}
		}
		return err
	})
	if err != nil {
		return err
	}
	ev.Timestamp = append(ev.Timestamp, unixNano(ts))
	ev.Name = append(ev.Name, name)
	ev.Attributes = append(ev.Attributes, set.m)
	return nil
}

func (d *decoder) pbLink(lk *SpanLinks, b []byte) error {
	var traceID, spanID, state string
	set := newAttrSet(&d.lim)
	err := walk(b, func(f field) error {
		var err error
		var v []byte
		switch f.num {
		case fLinkTraceID:
			if v, err = f.bytesVal(); err == nil {
				traceID, err = idHex(v, 16, "link trace_id")
			}
		case fLinkSpanID:
			if v, err = f.bytesVal(); err == nil {
				spanID, err = idHex(v, 8, "link span_id")
			}
		case fLinkTraceState:
			v, err = f.bytesVal()
			state = d.str(v)
		case fLinkAttributes:
			if v, err = f.bytesVal(); err == nil {
				err = d.pbAttr(&set, v)
			}
		}
		return err
	})
	if err != nil {
		return err
	}
	lk.TraceID = append(lk.TraceID, traceID)
	lk.SpanID = append(lk.SpanID, spanID)
	lk.TraceState = append(lk.TraceState, state)
	lk.Attributes = append(lk.Attributes, set.m)
	return nil
}

func (d *decoder) pbStatus(row *SpanRow, b []byte) error {
	return walk(b, func(f field) error {
		var err error
		switch f.num {
		case fStatusMessage:
			var v []byte
			v, err = f.bytesVal()
			row.StatusMessage = d.str(v)
		case fStatusCode:
			var n uint64
			n, err = f.varintVal()
			row.StatusCode = statusCodeString(int64(int32(n)))
		}
		return err
	})
}
