package otlp

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
)

// OTLP JSON. Field names are lowerCamelCase in the spec; the proto names
// (snake_case) are accepted too, as protobuf JSON parsers do. Both are
// matched after dropping underscores and case, so traceId, trace_id and
// TraceId are the same field. 64-bit integers may be numbers or strings,
// enums names or numbers, trace and span ids hex.

// parseJSONBody decodes the request into a generic object. Numbers stay
// json.Number so 64-bit nanosecond timestamps keep their precision.
func parseJSONBody(body []byte) (map[string]any, error) {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrMalformed, err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%w: trailing data after the JSON object", ErrMalformed)
	}
	obj, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%w: body is not a JSON object", ErrMalformed)
	}
	return obj, nil
}

// normKey folds a JSON field name for matching.
func normKey(k string) string {
	return strings.ToLower(strings.ReplaceAll(k, "_", ""))
}

func jsonTypeErr(what string, v any) error {
	return fmt.Errorf("%w: %s has unexpected JSON type %T", ErrMalformed, what, v)
}

// jsonObject returns v as an object; null is an empty one.
func jsonObject(v any, what string) (map[string]any, error) {
	switch t := v.(type) {
	case nil:
		return nil, nil
	case map[string]any:
		return t, nil
	}
	return nil, jsonTypeErr(what, v)
}

func jsonArray(v any, what string) ([]any, error) {
	switch t := v.(type) {
	case nil:
		return nil, nil
	case []any:
		return t, nil
	}
	return nil, jsonTypeErr(what, v)
}

func jsonString(v any, what string) (string, error) {
	switch t := v.(type) {
	case nil:
		return "", nil
	case string:
		return t, nil
	}
	return "", jsonTypeErr(what, v)
}

// jsonNumberText returns the text of a number given as a JSON number or a
// JSON string.
func jsonNumberText(v any, what string) (string, bool, error) {
	switch t := v.(type) {
	case nil:
		return "", false, nil
	case json.Number:
		return string(t), true, nil
	case string:
		return strings.TrimSpace(t), true, nil
	}
	return "", false, jsonTypeErr(what, v)
}

func jsonUint64(v any, what string) (uint64, error) {
	s, ok, err := jsonNumberText(v, what)
	if err != nil || !ok || s == "" {
		return 0, err
	}
	if n, err := strconv.ParseUint(s, 10, 64); err == nil {
		return n, nil
	}
	// 1.7e18 and the like: accept integral values in range.
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || f < 0 || f >= math.MaxUint64 || f != math.Trunc(f) {
		return 0, fmt.Errorf("%w: %s %q is not an unsigned integer", ErrMalformed, what, truncate(s, 64))
	}
	return uint64(f), nil
}

func jsonInt64(v any, what string) (int64, error) {
	s, ok, err := jsonNumberText(v, what)
	if err != nil || !ok || s == "" {
		return 0, err
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		return n, nil
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || f < math.MinInt64 || f >= math.MaxInt64 || f != math.Trunc(f) {
		return 0, fmt.Errorf("%w: %s %q is not an integer", ErrMalformed, what, truncate(s, 64))
	}
	return int64(f), nil
}

func jsonFloat(v any, what string) (float64, error) {
	s, ok, err := jsonNumberText(v, what)
	if err != nil || !ok {
		return 0, err
	}
	switch s {
	case "NaN":
		return math.NaN(), nil
	case "Infinity":
		return math.Inf(1), nil
	case "-Infinity":
		return math.Inf(-1), nil
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("%w: %s %q is not a number", ErrMalformed, what, truncate(s, 64))
	}
	return f, nil
}

// jsonEnum reads an enum given by number or by name. Unknown names read as
// zero, the unspecified value.
func jsonEnum(v any, names map[string]int64, what string) (int64, error) {
	if s, ok := v.(string); ok {
		if n, ok := names[s]; ok {
			return n, nil
		}
		if n, err := strconv.ParseInt(s, 10, 32); err == nil {
			return n, nil
		}
		return 0, nil
	}
	n, err := jsonInt64(v, what)
	if err != nil {
		return 0, err
	}
	if n < math.MinInt32 || n > math.MaxInt32 {
		return 0, nil
	}
	return n, nil
}

// jsonAny is a decoded AnyValue: kind is one of the fAny* field numbers or
// 0 for an empty value.
type jsonAny struct {
	kind int32
	v    any
}

func parseJSONAny(v any) (jsonAny, error) {
	obj, err := jsonObject(v, "AnyValue")
	if err != nil {
		return jsonAny{}, err
	}
	var a jsonAny
	for k, val := range obj {
		if val == nil {
			continue
		}
		switch normKey(k) {
		case "stringvalue":
			a.kind = fAnyStringValue
		case "boolvalue":
			a.kind = fAnyBoolValue
		case "intvalue":
			a.kind = fAnyIntValue
		case "doublevalue":
			a.kind = fAnyDoubleValue
		case "arrayvalue":
			a.kind = fAnyArrayValue
		case "kvlistvalue":
			a.kind = fAnyKvlistValue
		case "bytesvalue":
			a.kind = fAnyBytesValue
		default:
			continue
		}
		a.v = val
	}
	return a, nil
}

// jsonAnyString is pbAnyString for OTLP JSON.
func (d *decoder) jsonAnyString(v any, limit int) (string, error) {
	a, err := parseJSONAny(v)
	if err != nil {
		return "", err
	}
	switch a.kind {
	case 0:
		return "", nil
	case fAnyStringValue:
		s, err := jsonString(a.v, "stringValue")
		return truncate(s, limit), err
	case fAnyBoolValue:
		b, ok := a.v.(bool)
		if !ok {
			return "", jsonTypeErr("boolValue", a.v)
		}
		return strconv.FormatBool(b), nil
	case fAnyIntValue:
		n, err := jsonInt64(a.v, "intValue")
		return strconv.FormatInt(n, 10), err
	case fAnyDoubleValue:
		f, err := jsonFloat(a.v, "doubleValue")
		if err != nil {
			return "", err
		}
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return strconv.FormatFloat(f, 'g', -1, 64), nil
		}
		return string(appendFloat(nil, f)), nil
	case fAnyBytesValue:
		b, err := jsonBytes(a.v)
		return truncate(base64.StdEncoding.EncodeToString(b), limit), err
	}
	out, err := d.appendJSONAnyJSON(nil, a, 0)
	return string(truncate(out, limit)), err
}

func jsonBytes(v any) ([]byte, error) {
	s, err := jsonString(v, "bytesValue")
	if err != nil {
		return nil, err
	}
	b, err := decodeBase64(s)
	if err != nil {
		return nil, fmt.Errorf("%w: bytesValue is not base64", ErrMalformed)
	}
	return b, nil
}

// appendJSONAnyJSON is appendPbAnyJSON for OTLP JSON.
func (d *decoder) appendJSONAnyJSON(dst []byte, a jsonAny, depth int) ([]byte, error) {
	switch a.kind {
	case 0:
		return append(dst, "null"...), nil
	case fAnyStringValue:
		s, err := jsonString(a.v, "stringValue")
		return appendJSONString(dst, s), err
	case fAnyBoolValue:
		b, ok := a.v.(bool)
		if !ok {
			return nil, jsonTypeErr("boolValue", a.v)
		}
		return strconv.AppendBool(dst, b), nil
	case fAnyIntValue:
		n, err := jsonInt64(a.v, "intValue")
		return strconv.AppendInt(dst, n, 10), err
	case fAnyDoubleValue:
		f, err := jsonFloat(a.v, "doubleValue")
		if err != nil {
			return nil, err
		}
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return append(dst, "null"...), nil
		}
		return appendFloat(dst, f), nil
	case fAnyBytesValue:
		b, err := jsonBytes(a.v)
		if err != nil {
			return nil, err
		}
		dst = append(dst, '"')
		dst = base64.StdEncoding.AppendEncode(dst, b)
		return append(dst, '"'), nil
	}
	if depth >= d.lim.MaxDepth {
		return nil, fmt.Errorf("%w: attribute value nested deeper than %d", ErrMalformed, d.lim.MaxDepth)
	}
	obj, err := jsonObject(a.v, "arrayValue or kvlistValue")
	if err != nil {
		return nil, err
	}
	var values []any
	for k, v := range obj {
		if normKey(k) == "values" {
			if values, err = jsonArray(v, "values"); err != nil {
				return nil, err
			}
		}
	}
	if a.kind == fAnyArrayValue {
		dst = append(dst, '[')
		for i, v := range values {
			if i > 0 {
				dst = append(dst, ',')
			}
			item, err := parseJSONAny(v)
			if err != nil {
				return nil, err
			}
			if dst, err = d.appendJSONAnyJSON(dst, item, depth+1); err != nil {
				return nil, err
			}
		}
		return append(dst, ']'), nil
	}
	entries := make([]kvJSON, 0, len(values))
	for _, v := range values {
		key, val, err := jsonKeyValue(v)
		if err != nil {
			return nil, err
		}
		item, err := parseJSONAny(val)
		if err != nil {
			return nil, err
		}
		enc, err := d.appendJSONAnyJSON(nil, item, depth+1)
		if err != nil {
			return nil, err
		}
		entries = append(entries, kvJSON{key: key, val: enc})
	}
	return appendKVObject(dst, entries), nil
}

// jsonKeyValue splits a KeyValue object into its key and AnyValue.
func jsonKeyValue(v any) (string, any, error) {
	obj, err := jsonObject(v, "KeyValue")
	if err != nil {
		return "", nil, err
	}
	var key string
	var val any
	for k, f := range obj {
		switch normKey(k) {
		case "key":
			if key, err = jsonString(f, "key"); err != nil {
				return "", nil, err
			}
		case "value":
			val = f
		}
	}
	return key, val, nil
}

// jsonAttrs adds a JSON attributes array to set.
func (d *decoder) jsonAttrs(set *attrSet, v any) error {
	list, err := jsonArray(v, "attributes")
	if err != nil {
		return err
	}
	for _, item := range list {
		key, val, err := jsonKeyValue(item)
		if err != nil {
			return err
		}
		key = truncate(key, d.lim.MaxValueBytes)
		if !set.has(key) {
			continue
		}
		s, err := d.jsonAnyString(val, d.lim.MaxValueBytes)
		if err != nil {
			return err
		}
		set.put(key, s)
	}
	return nil
}

func (d *decoder) jsonResource(v any) (map[string]string, error) {
	obj, err := jsonObject(v, "resource")
	if err != nil {
		return nil, err
	}
	set := newAttrSet(&d.lim)
	for k, f := range obj {
		if normKey(k) == "attributes" {
			if err := d.jsonAttrs(&set, f); err != nil {
				return nil, err
			}
		}
	}
	return set.m, nil
}

func (d *decoder) jsonScope(v any) (pbScope, error) {
	s := pbScope{}
	obj, err := jsonObject(v, "scope")
	if err != nil {
		return s, err
	}
	set := newAttrSet(&d.lim)
	for k, f := range obj {
		switch normKey(k) {
		case "name":
			s.name, err = d.jsonStr(f, "scope name")
		case "version":
			s.version, err = d.jsonStr(f, "scope version")
		case "attributes":
			err = d.jsonAttrs(&set, f)
		}
		if err != nil {
			return s, err
		}
	}
	s.attrs = set.m
	return s, nil
}

// jsonStr reads a string field cut to MaxValueBytes.
func (d *decoder) jsonStr(v any, what string) (string, error) {
	s, err := jsonString(v, what)
	return truncate(s, d.lim.MaxValueBytes), err
}

// DecodeLogsJSON decodes an OTLP JSON ExportLogsServiceRequest.
func DecodeLogsJSON(body []byte, lim Limits) ([]LogRow, error) {
	root, err := parseJSONBody(body)
	if err != nil {
		return nil, err
	}
	d := newDecoder(lim)
	var rows []LogRow
	for k, v := range root {
		if normKey(k) != "resourcelogs" {
			continue
		}
		list, err := jsonArray(v, "resourceLogs")
		if err != nil {
			return nil, err
		}
		for _, rl := range list {
			if rows, err = d.jsonResourceLogs(rows, rl); err != nil {
				return nil, err
			}
		}
	}
	return rows, nil
}

func (d *decoder) jsonResourceLogs(rows []LogRow, v any) ([]LogRow, error) {
	obj, err := jsonObject(v, "resourceLogs entry")
	if err != nil {
		return nil, err
	}
	var ctx logContext
	var scopes []any
	for k, f := range obj {
		switch normKey(k) {
		case "resource":
			ctx.resAttrs, err = d.jsonResource(f)
		case "schemaurl":
			ctx.resSchemaURL, err = d.jsonStr(f, "schemaUrl")
		case "scopelogs":
			scopes, err = jsonArray(f, "scopeLogs")
		}
		if err != nil {
			return nil, err
		}
	}
	if ctx.resAttrs == nil {
		ctx.resAttrs = map[string]string{}
	}
	ctx.service = ctx.resAttrs[serviceNameKey]
	for _, s := range scopes {
		if rows, err = d.jsonScopeLogs(rows, s, ctx); err != nil {
			return nil, err
		}
	}
	return rows, nil
}

func (d *decoder) jsonScopeLogs(rows []LogRow, v any, ctx logContext) ([]LogRow, error) {
	obj, err := jsonObject(v, "scopeLogs entry")
	if err != nil {
		return nil, err
	}
	var records []any
	for k, f := range obj {
		switch normKey(k) {
		case "scope":
			ctx.scope, err = d.jsonScope(f)
		case "schemaurl":
			ctx.scopeSchema, err = d.jsonStr(f, "schemaUrl")
		case "logrecords":
			records, err = jsonArray(f, "logRecords")
		}
		if err != nil {
			return nil, err
		}
	}
	if ctx.scope.attrs == nil {
		ctx.scope.attrs = map[string]string{}
	}
	for _, r := range records {
		if err := d.count(); err != nil {
			return nil, err
		}
		row, err := d.jsonLogRecord(r, &ctx)
		if err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func (d *decoder) jsonLogRecord(v any, ctx *logContext) (LogRow, error) {
	row := ctx.row()
	obj, err := jsonObject(v, "logRecord")
	if err != nil {
		return row, err
	}
	set := newAttrSet(&d.lim)
	var ts, observed uint64
	for k, f := range obj {
		var s string
		switch normKey(k) {
		case "timeunixnano":
			ts, err = jsonUint64(f, "timeUnixNano")
		case "observedtimeunixnano":
			observed, err = jsonUint64(f, "observedTimeUnixNano")
		case "severitynumber":
			var n int64
			n, err = jsonEnum(f, severityNumberByName, "severityNumber")
			row.SeverityNumber = int32(n)
		case "severitytext":
			row.SeverityText, err = d.jsonStr(f, "severityText")
		case "body":
			row.Body, err = d.jsonAnyString(f, d.lim.MaxBodyBytes)
		case "attributes":
			err = d.jsonAttrs(&set, f)
		case "flags":
			var n uint64
			n, err = jsonUint64(f, "flags")
			row.TraceFlags = uint8(n)
		case "traceid":
			if s, err = jsonString(f, "traceId"); err == nil {
				row.TraceID, err = idFromJSON(s, 16, "traceId")
			}
		case "spanid":
			if s, err = jsonString(f, "spanId"); err == nil {
				row.SpanID, err = idFromJSON(s, 8, "spanId")
			}
		case "eventname":
			row.EventName, err = d.jsonStr(f, "eventName")
		}
		if err != nil {
			return row, err
		}
	}
	if ts == 0 {
		ts = observed
	}
	row.Timestamp = unixNano(ts)
	row.LogAttributes = set.m
	return row, nil
}

// DecodeTracesJSON decodes an OTLP JSON ExportTraceServiceRequest.
func DecodeTracesJSON(body []byte, lim Limits) ([]SpanRow, error) {
	root, err := parseJSONBody(body)
	if err != nil {
		return nil, err
	}
	d := newDecoder(lim)
	var rows []SpanRow
	for k, v := range root {
		if normKey(k) != "resourcespans" {
			continue
		}
		list, err := jsonArray(v, "resourceSpans")
		if err != nil {
			return nil, err
		}
		for _, rs := range list {
			if rows, err = d.jsonResourceSpans(rows, rs); err != nil {
				return nil, err
			}
		}
	}
	return rows, nil
}

func (d *decoder) jsonResourceSpans(rows []SpanRow, v any) ([]SpanRow, error) {
	obj, err := jsonObject(v, "resourceSpans entry")
	if err != nil {
		return nil, err
	}
	var ctx spanContext
	var scopes []any
	for k, f := range obj {
		switch normKey(k) {
		case "resource":
			ctx.resAttrs, err = d.jsonResource(f)
		case "scopespans":
			scopes, err = jsonArray(f, "scopeSpans")
		}
		if err != nil {
			return nil, err
		}
	}
	if ctx.resAttrs == nil {
		ctx.resAttrs = map[string]string{}
	}
	ctx.service = ctx.resAttrs[serviceNameKey]
	for _, s := range scopes {
		if rows, err = d.jsonScopeSpans(rows, s, ctx); err != nil {
			return nil, err
		}
	}
	return rows, nil
}

func (d *decoder) jsonScopeSpans(rows []SpanRow, v any, ctx spanContext) ([]SpanRow, error) {
	obj, err := jsonObject(v, "scopeSpans entry")
	if err != nil {
		return nil, err
	}
	var spans []any
	for k, f := range obj {
		switch normKey(k) {
		case "scope":
			ctx.scope, err = d.jsonScope(f)
		case "spans":
			spans, err = jsonArray(f, "spans")
		}
		if err != nil {
			return nil, err
		}
	}
	for _, s := range spans {
		if err := d.count(); err != nil {
			return nil, err
		}
		row, err := d.jsonSpan(s, &ctx)
		if err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func (d *decoder) jsonSpan(v any, ctx *spanContext) (SpanRow, error) {
	row := SpanRow{
		ServiceName:        ctx.service,
		ResourceAttributes: ctx.resAttrs,
		ScopeName:          ctx.scope.name,
		ScopeVersion:       ctx.scope.version,
		SpanKind:           spanKindString(0),
		StatusCode:         statusCodeString(0),
	}
	obj, err := jsonObject(v, "span")
	if err != nil {
		return row, err
	}
	set := newAttrSet(&d.lim)
	var start, end uint64
	for k, f := range obj {
		var s string
		var list []any
		switch normKey(k) {
		case "traceid":
			if s, err = jsonString(f, "traceId"); err == nil {
				row.TraceID, err = idFromJSON(s, 16, "traceId")
			}
		case "spanid":
			if s, err = jsonString(f, "spanId"); err == nil {
				row.SpanID, err = idFromJSON(s, 8, "spanId")
			}
		case "parentspanid":
			if s, err = jsonString(f, "parentSpanId"); err == nil {
				row.ParentSpanID, err = idFromJSON(s, 8, "parentSpanId")
			}
		case "tracestate":
			row.TraceState, err = d.jsonStr(f, "traceState")
		case "name":
			row.SpanName, err = d.jsonStr(f, "name")
		case "kind":
			var n int64
			n, err = jsonEnum(f, spanKindByName, "kind")
			row.SpanKind = spanKindString(n)
		case "starttimeunixnano":
			start, err = jsonUint64(f, "startTimeUnixNano")
		case "endtimeunixnano":
			end, err = jsonUint64(f, "endTimeUnixNano")
		case "attributes":
			err = d.jsonAttrs(&set, f)
		case "events":
			if list, err = jsonArray(f, "events"); err == nil {
				for _, e := range list[:min(len(list), d.lim.MaxEvents)] {
					if err = d.jsonEvent(&row.Events, e); err != nil {
						break
					}
				}
			}
		case "links":
			if list, err = jsonArray(f, "links"); err == nil {
				for _, l := range list[:min(len(list), d.lim.MaxLinks)] {
					if err = d.jsonLink(&row.Links, l); err != nil {
						break
					}
				}
			}
		case "status":
			err = d.jsonStatus(&row, f)
		}
		if err != nil {
			return row, err
		}
	}
	row.Timestamp = unixNano(start)
	row.Duration = spanDuration(start, end)
	row.SpanAttributes = set.m
	return row, nil
}

func (d *decoder) jsonEvent(ev *SpanEvents, v any) error {
	obj, err := jsonObject(v, "event")
	if err != nil {
		return err
	}
	var ts uint64
	var name string
	set := newAttrSet(&d.lim)
	for k, f := range obj {
		switch normKey(k) {
		case "timeunixnano":
			ts, err = jsonUint64(f, "event timeUnixNano")
		case "name":
			name, err = d.jsonStr(f, "event name")
		case "attributes":
			err = d.jsonAttrs(&set, f)
		}
		if err != nil {
			return err
		}
	}
	ev.Timestamp = append(ev.Timestamp, unixNano(ts))
	ev.Name = append(ev.Name, name)
	ev.Attributes = append(ev.Attributes, set.m)
	return nil
}

func (d *decoder) jsonLink(lk *SpanLinks, v any) error {
	obj, err := jsonObject(v, "link")
	if err != nil {
		return err
	}
	var traceID, spanID, state, s string
	set := newAttrSet(&d.lim)
	for k, f := range obj {
		switch normKey(k) {
		case "traceid":
			if s, err = jsonString(f, "link traceId"); err == nil {
				traceID, err = idFromJSON(s, 16, "link traceId")
			}
		case "spanid":
			if s, err = jsonString(f, "link spanId"); err == nil {
				spanID, err = idFromJSON(s, 8, "link spanId")
			}
		case "tracestate":
			state, err = d.jsonStr(f, "link traceState")
		case "attributes":
			err = d.jsonAttrs(&set, f)
		}
		if err != nil {
			return err
		}
	}
	lk.TraceID = append(lk.TraceID, traceID)
	lk.SpanID = append(lk.SpanID, spanID)
	lk.TraceState = append(lk.TraceState, state)
	lk.Attributes = append(lk.Attributes, set.m)
	return nil
}

func (d *decoder) jsonStatus(row *SpanRow, v any) error {
	obj, err := jsonObject(v, "status")
	if err != nil {
		return err
	}
	for k, f := range obj {
		switch normKey(k) {
		case "message":
			row.StatusMessage, err = d.jsonStr(f, "status message")
		case "code":
			var n int64
			n, err = jsonEnum(f, statusCodeByName, "status code")
			row.StatusCode = statusCodeString(n)
		}
		if err != nil {
			return err
		}
	}
	return nil
}
