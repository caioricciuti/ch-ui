package otlp

import (
	"fmt"
	"math"
	"time"
)

// Field numbers from opentelemetry/proto/collector/logs/v1/logs_service.proto
// and opentelemetry/proto/logs/v1/logs.proto.
const (
	fExportLogsRequestResourceLogs = 1 // ExportLogsServiceRequest.resource_logs

	fResourceLogsResource  = 1 // ResourceLogs.resource
	fResourceLogsScopeLogs = 2 // ResourceLogs.scope_logs
	fResourceLogsSchemaURL = 3 // ResourceLogs.schema_url

	fScopeLogsScope      = 1 // ScopeLogs.scope
	fScopeLogsLogRecords = 2 // ScopeLogs.log_records
	fScopeLogsSchemaURL  = 3 // ScopeLogs.schema_url

	fLogTimeUnixNano         = 1  // LogRecord.time_unix_nano (fixed64)
	fLogSeverityNumber       = 2  // LogRecord.severity_number (enum)
	fLogSeverityText         = 3  // LogRecord.severity_text
	fLogBody                 = 5  // LogRecord.body (AnyValue)
	fLogAttributes           = 6  // LogRecord.attributes
	fLogFlags                = 8  // LogRecord.flags (fixed32)
	fLogTraceID              = 9  // LogRecord.trace_id (bytes)
	fLogSpanID               = 10 // LogRecord.span_id (bytes)
	fLogObservedTimeUnixNano = 11 // LogRecord.observed_time_unix_nano (fixed64)
	fLogEventName            = 12 // LogRecord.event_name
)

// LogRow is one row of the exporter's otel_logs table. Rows decoded from the
// same resource or scope share those attribute maps; treat them as read-only.
type LogRow struct {
	// Timestamp is time_unix_nano, or observed_time_unix_nano when that is
	// zero. Both zero gives the Unix epoch, as in the exporter.
	Timestamp          time.Time
	TraceID            string
	SpanID             string
	TraceFlags         uint8
	SeverityText       string
	SeverityNumber     int32
	ServiceName        string
	Body               string
	ResourceSchemaURL  string
	ResourceAttributes map[string]string
	ScopeSchemaURL     string
	ScopeName          string
	ScopeVersion       string
	ScopeAttributes    map[string]string
	LogAttributes      map[string]string
	EventName          string
}

// decoder carries the limits and the record count of one request.
type decoder struct {
	lim Limits
	n   int
}

func newDecoder(lim Limits) *decoder { return &decoder{lim: lim.withDefaults()} }

// count reserves one more record against MaxRecords.
func (d *decoder) count() error {
	if d.n >= d.lim.MaxRecords {
		return fmt.Errorf("%w: limit is %d", ErrTooManyRecords, d.lim.MaxRecords)
	}
	d.n++
	return nil
}

// unixNano converts an OTLP timestamp. Values past the int64 range (year
// 2262) cannot be represented and read as zero.
func unixNano(ns uint64) time.Time {
	if ns > math.MaxInt64 {
		ns = 0
	}
	return time.Unix(0, int64(ns)).UTC()
}

// DecodeLogs decodes an OTLP/HTTP logs export body in the encoding named by
// the Content-Type header.
func DecodeLogs(body []byte, contentType string, lim Limits) ([]LogRow, error) {
	enc, err := ParseContentType(contentType)
	if err != nil {
		return nil, err
	}
	if enc == EncodingJSON {
		return DecodeLogsJSON(body, lim)
	}
	return DecodeLogsProto(body, lim)
}

// DecodeLogsProto decodes a protobuf ExportLogsServiceRequest.
func DecodeLogsProto(body []byte, lim Limits) ([]LogRow, error) {
	d := newDecoder(lim)
	var rows []LogRow
	err := walk(body, func(f field) error {
		if f.num != fExportLogsRequestResourceLogs {
			return nil
		}
		b, err := f.bytesVal()
		if err != nil {
			return err
		}
		rows, err = d.pbResourceLogs(rows, b)
		return err
	})
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// logContext is what a record inherits from its resource and scope.
type logContext struct {
	resAttrs     map[string]string
	service      string
	resSchemaURL string
	scope        pbScope
	scopeSchema  string
}

func (d *decoder) pbResourceLogs(rows []LogRow, b []byte) ([]LogRow, error) {
	// Fields may come in any order, so the resource is read in a first pass
	// before the records that need it.
	var ctx logContext
	err := walk(b, func(f field) error {
		var err error
		switch f.num {
		case fResourceLogsResource:
			var v []byte
			if v, err = f.bytesVal(); err == nil {
				ctx.resAttrs, err = d.parseResource(v)
			}
		case fResourceLogsSchemaURL:
			var v []byte
			v, err = f.bytesVal()
			ctx.resSchemaURL = d.str(v)
		}
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
		if f.num != fResourceLogsScopeLogs {
			return nil
		}
		v, err := f.bytesVal()
		if err != nil {
			return err
		}
		rows, err = d.pbScopeLogs(rows, v, ctx)
		return err
	})
	return rows, err
}

func (d *decoder) pbScopeLogs(rows []LogRow, b []byte, ctx logContext) ([]LogRow, error) {
	err := walk(b, func(f field) error {
		var err error
		switch f.num {
		case fScopeLogsScope:
			var v []byte
			if v, err = f.bytesVal(); err == nil {
				ctx.scope, err = d.parseScope(v)
			}
		case fScopeLogsSchemaURL:
			var v []byte
			v, err = f.bytesVal()
			ctx.scopeSchema = d.str(v)
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	if ctx.scope.attrs == nil {
		ctx.scope.attrs = map[string]string{}
	}
	err = walk(b, func(f field) error {
		if f.num != fScopeLogsLogRecords {
			return nil
		}
		v, err := f.bytesVal()
		if err != nil {
			return err
		}
		if err := d.count(); err != nil {
			return err
		}
		row, err := d.pbLogRecord(v, &ctx)
		if err != nil {
			return err
		}
		rows = append(rows, row)
		return nil
	})
	return rows, err
}

func (d *decoder) pbLogRecord(b []byte, ctx *logContext) (LogRow, error) {
	row := ctx.row()
	set := newAttrSet(&d.lim)
	var ts, observed uint64
	err := walk(b, func(f field) error {
		var err error
		var v []byte
		switch f.num {
		case fLogTimeUnixNano:
			ts, err = f.fixed64Val()
		case fLogObservedTimeUnixNano:
			observed, err = f.fixed64Val()
		case fLogSeverityNumber:
			var n uint64
			n, err = f.varintVal()
			row.SeverityNumber = int32(n)
		case fLogSeverityText:
			v, err = f.bytesVal()
			row.SeverityText = d.str(v)
		case fLogBody:
			if v, err = f.bytesVal(); err == nil {
				row.Body, err = d.pbAnyString(v, d.lim.MaxBodyBytes)
			}
		case fLogAttributes:
			if v, err = f.bytesVal(); err == nil {
				err = d.pbAttr(&set, v)
			}
		case fLogFlags:
			var n uint32
			n, err = f.fixed32Val()
			row.TraceFlags = uint8(n)
		case fLogTraceID:
			if v, err = f.bytesVal(); err == nil {
				row.TraceID, err = idHex(v, 16, "trace_id")
			}
		case fLogSpanID:
			if v, err = f.bytesVal(); err == nil {
				row.SpanID, err = idHex(v, 8, "span_id")
			}
		case fLogEventName:
			v, err = f.bytesVal()
			row.EventName = d.str(v)
		}
		return err
	})
	if ts == 0 {
		ts = observed
	}
	row.Timestamp = unixNano(ts)
	row.LogAttributes = set.m
	return row, err
}

// row starts a LogRow with the resource and scope columns filled.
func (c *logContext) row() LogRow {
	return LogRow{
		ServiceName:        c.service,
		ResourceSchemaURL:  c.resSchemaURL,
		ResourceAttributes: c.resAttrs,
		ScopeSchemaURL:     c.scopeSchema,
		ScopeName:          c.scope.name,
		ScopeVersion:       c.scope.version,
		ScopeAttributes:    c.scope.attrs,
	}
}
