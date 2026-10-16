package otlp

import (
	"math"
	"slices"
	"strconv"
	"time"
	"unicode/utf8"
)

// JSONEachRow encoding of LogRow and SpanRow. Column names are the
// exporter's. Nested columns are written flattened (Events.Timestamp and so
// on), which is how ClickHouse exposes them with the default flatten_nested.
//
// DateTime64(9) values are written as "<unix seconds>.<9 digits>". ClickHouse
// reads that form as an absolute instant, while "2006-01-02 15:04:05" would
// be read in the server or session time zone and shift every row on a
// server that is not on UTC.

// AppendLogJSON appends r as one JSONEachRow line, newline included.
func AppendLogJSON(buf []byte, r *LogRow) []byte {
	buf = append(buf, `{"Timestamp":`...)
	buf = appendDateTime64(buf, r.Timestamp)
	buf = appendStringField(buf, "TraceId", r.TraceID)
	buf = appendStringField(buf, "SpanId", r.SpanID)
	buf = append(buf, `,"TraceFlags":`...)
	buf = strconv.AppendUint(buf, uint64(r.TraceFlags), 10)
	buf = appendStringField(buf, "SeverityText", r.SeverityText)
	buf = append(buf, `,"SeverityNumber":`...)
	// The column is UInt8; the OTLP enum stops at 24.
	sev := r.SeverityNumber
	if sev < 0 || sev > math.MaxUint8 {
		sev = 0
	}
	buf = strconv.AppendInt(buf, int64(sev), 10)
	buf = appendStringField(buf, "ServiceName", r.ServiceName)
	buf = appendStringField(buf, "Body", r.Body)
	buf = appendStringField(buf, "ResourceSchemaUrl", r.ResourceSchemaURL)
	buf = append(buf, `,"ResourceAttributes":`...)
	buf = appendStringMap(buf, r.ResourceAttributes)
	buf = appendStringField(buf, "ScopeSchemaUrl", r.ScopeSchemaURL)
	buf = appendStringField(buf, "ScopeName", r.ScopeName)
	buf = appendStringField(buf, "ScopeVersion", r.ScopeVersion)
	buf = append(buf, `,"ScopeAttributes":`...)
	buf = appendStringMap(buf, r.ScopeAttributes)
	buf = append(buf, `,"LogAttributes":`...)
	buf = appendStringMap(buf, r.LogAttributes)
	buf = appendStringField(buf, "EventName", r.EventName)
	return append(buf, "}\n"...)
}

// AppendSpanJSON appends r as one JSONEachRow line, newline included.
func AppendSpanJSON(buf []byte, r *SpanRow) []byte {
	buf = append(buf, `{"Timestamp":`...)
	buf = appendDateTime64(buf, r.Timestamp)
	buf = appendStringField(buf, "TraceId", r.TraceID)
	buf = appendStringField(buf, "SpanId", r.SpanID)
	buf = appendStringField(buf, "ParentSpanId", r.ParentSpanID)
	buf = appendStringField(buf, "TraceState", r.TraceState)
	buf = appendStringField(buf, "SpanName", r.SpanName)
	buf = appendStringField(buf, "SpanKind", r.SpanKind)
	buf = appendStringField(buf, "ServiceName", r.ServiceName)
	buf = append(buf, `,"ResourceAttributes":`...)
	buf = appendStringMap(buf, r.ResourceAttributes)
	buf = appendStringField(buf, "ScopeName", r.ScopeName)
	buf = appendStringField(buf, "ScopeVersion", r.ScopeVersion)
	buf = append(buf, `,"SpanAttributes":`...)
	buf = appendStringMap(buf, r.SpanAttributes)
	buf = append(buf, `,"Duration":`...)
	buf = strconv.AppendInt(buf, max(r.Duration, 0), 10)
	buf = appendStringField(buf, "StatusCode", r.StatusCode)
	buf = appendStringField(buf, "StatusMessage", r.StatusMessage)

	buf = append(buf, `,"Events.Timestamp":[`...)
	for i, t := range r.Events.Timestamp {
		if i > 0 {
			buf = append(buf, ',')
		}
		buf = appendDateTime64(buf, t)
	}
	buf = append(buf, `],"Events.Name":`...)
	buf = appendStringArray(buf, r.Events.Name)
	buf = append(buf, `,"Events.Attributes":`...)
	buf = appendMapArray(buf, r.Events.Attributes)
	buf = append(buf, `,"Links.TraceId":`...)
	buf = appendStringArray(buf, r.Links.TraceID)
	buf = append(buf, `,"Links.SpanId":`...)
	buf = appendStringArray(buf, r.Links.SpanID)
	buf = append(buf, `,"Links.TraceState":`...)
	buf = appendStringArray(buf, r.Links.TraceState)
	buf = append(buf, `,"Links.Attributes":`...)
	buf = appendMapArray(buf, r.Links.Attributes)
	return append(buf, "}\n"...)
}

// maxDateTime64Seconds is the last whole second DateTime64(9) can hold.
const maxDateTime64Seconds = math.MaxInt64 / 1_000_000_000

// appendDateTime64 writes t as a quoted "<seconds>.<nanoseconds>" string,
// clamped to the DateTime64(9) range starting at the Unix epoch.
func appendDateTime64(buf []byte, t time.Time) []byte {
	var ns int64
	switch sec := t.Unix(); {
	case sec < 0:
		ns = 0
	case sec >= maxDateTime64Seconds:
		ns = math.MaxInt64
	default:
		ns = t.UnixNano()
	}
	buf = append(buf, '"')
	buf = strconv.AppendInt(buf, ns/1e9, 10)
	frac := ns % 1e9
	buf = append(buf, '.')
	for div := int64(1e8); div > 0; div /= 10 {
		buf = append(buf, byte('0'+frac/div%10))
	}
	return append(buf, '"')
}

func appendStringField(buf []byte, name, value string) []byte {
	buf = append(buf, ',', '"')
	buf = append(buf, name...)
	buf = append(buf, '"', ':')
	return appendJSONString(buf, value)
}

func appendStringArray(buf []byte, values []string) []byte {
	buf = append(buf, '[')
	for i, v := range values {
		if i > 0 {
			buf = append(buf, ',')
		}
		buf = appendJSONString(buf, v)
	}
	return append(buf, ']')
}

func appendMapArray(buf []byte, maps []map[string]string) []byte {
	buf = append(buf, '[')
	for i, m := range maps {
		if i > 0 {
			buf = append(buf, ',')
		}
		buf = appendStringMap(buf, m)
	}
	return append(buf, ']')
}

// appendStringMap writes m as a JSON object with sorted keys, so equal rows
// encode to equal bytes. A nil map is {}.
func appendStringMap(buf []byte, m map[string]string) []byte {
	var scratch [32]string
	keys := scratch[:0]
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	buf = append(buf, '{')
	for i, k := range keys {
		if i > 0 {
			buf = append(buf, ',')
		}
		buf = appendJSONString(buf, k)
		buf = append(buf, ':')
		buf = appendJSONString(buf, m[k])
	}
	return append(buf, '}')
}

const hexDigits = "0123456789abcdef"

// appendJSONString writes s as a JSON string. Quote, backslash and control
// characters are escaped; invalid UTF-8 becomes U+FFFD, as encoding/json
// does. HTML characters are left alone.
func appendJSONString(buf []byte, s string) []byte {
	buf = append(buf, '"')
	start := 0
	for i := 0; i < len(s); {
		c := s[i]
		if c < utf8.RuneSelf {
			if c >= 0x20 && c != '"' && c != '\\' {
				i++
				continue
			}
			buf = append(buf, s[start:i]...)
			switch c {
			case '"', '\\':
				buf = append(buf, '\\', c)
			case '\n':
				buf = append(buf, '\\', 'n')
			case '\r':
				buf = append(buf, '\\', 'r')
			case '\t':
				buf = append(buf, '\\', 't')
			case '\b':
				buf = append(buf, '\\', 'b')
			case '\f':
				buf = append(buf, '\\', 'f')
			default:
				buf = append(buf, '\\', 'u', '0', '0', hexDigits[c>>4], hexDigits[c&0xf])
			}
			i++
			start = i
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			buf = append(buf, s[start:i]...)
			buf = append(buf, `�`...)
			i++
			start = i
			continue
		}
		i += size
	}
	buf = append(buf, s[start:]...)
	return append(buf, '"')
}
