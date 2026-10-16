// Package otlp decodes OTLP/HTTP export requests (protobuf and JSON) into
// rows laid out like the OpenTelemetry Collector ClickHouse exporter's
// otel_logs and otel_traces tables, encodes those rows as JSONEachRow, and
// renders the matching CREATE TABLE statements. It has no dependency on the
// protobuf runtime: wire.go reads the wire format directly.
package otlp

import (
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

var (
	// ErrMalformed wraps every decoding error caused by the request body.
	ErrMalformed = errors.New("malformed OTLP payload")
	// ErrTooManyRecords is returned when a request holds more log records
	// or spans than Limits.MaxRecords.
	ErrTooManyRecords = errors.New("too many records in one request")
	// ErrUnsupportedContentType is returned for anything but OTLP protobuf
	// or OTLP JSON.
	ErrUnsupportedContentType = errors.New("unsupported content type")
)

// Encoding is the wire format of a request body.
type Encoding int

const (
	EncodingProtobuf Encoding = iota
	EncodingJSON
)

// ParseContentType maps a Content-Type header onto an Encoding. Parameters
// such as charset are ignored.
func ParseContentType(contentType string) (Encoding, error) {
	mt, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		mt = strings.ToLower(strings.TrimSpace(contentType))
	}
	switch mt {
	case "application/x-protobuf", "application/protobuf":
		return EncodingProtobuf, nil
	case "application/json":
		return EncodingJSON, nil
	}
	return 0, fmt.Errorf("%w: %q", ErrUnsupportedContentType, contentType)
}

// Limits bound what one request may produce. Zero fields take the value from
// DefaultLimits.
//
//   - MaxRecords: log records or spans per request. More is an error
//     (ErrTooManyRecords), nothing is decoded partially.
//   - MaxAttributes: distinct keys kept per attribute map (resource, scope,
//     record, span, event, link). Keys past the limit are dropped in wire
//     order; a later duplicate of a kept key still overwrites it, as in the
//     exporter.
//   - MaxValueBytes: attribute keys and values, and every other string
//     column except Body, are cut to this many bytes on a UTF-8 boundary. A
//     cut array or map value is no longer valid JSON.
//   - MaxBodyBytes: same for the log Body.
//   - MaxEvents, MaxLinks: per span; the rest are dropped.
//   - MaxDepth: nesting of array and map attribute values. Deeper is an
//     error.
type Limits struct {
	MaxRecords    int
	MaxAttributes int
	MaxValueBytes int
	MaxBodyBytes  int
	MaxEvents     int
	MaxLinks      int
	MaxDepth      int
}

// DefaultLimits are the limits used for zero fields.
func DefaultLimits() Limits {
	return Limits{
		MaxRecords:    100_000,
		MaxAttributes: 128,
		MaxValueBytes: 16 << 10,
		MaxBodyBytes:  1 << 20,
		MaxEvents:     128,
		MaxLinks:      128,
		MaxDepth:      16,
	}
}

func (l Limits) withDefaults() Limits {
	d := DefaultLimits()
	if l.MaxRecords <= 0 {
		l.MaxRecords = d.MaxRecords
	}
	if l.MaxAttributes <= 0 {
		l.MaxAttributes = d.MaxAttributes
	}
	if l.MaxValueBytes <= 0 {
		l.MaxValueBytes = d.MaxValueBytes
	}
	if l.MaxBodyBytes <= 0 {
		l.MaxBodyBytes = d.MaxBodyBytes
	}
	if l.MaxEvents <= 0 {
		l.MaxEvents = d.MaxEvents
	}
	if l.MaxLinks <= 0 {
		l.MaxLinks = d.MaxLinks
	}
	if l.MaxDepth <= 0 {
		l.MaxDepth = d.MaxDepth
	}
	return l
}

// truncate cuts s to at most n bytes without splitting a UTF-8 sequence.
func truncate[T string | []byte](s T, n int) T {
	if len(s) <= n {
		return s
	}
	cut := n
	// Back up over at most three continuation bytes to a rune start.
	for cut > 0 && cut > n-utf8.UTFMax && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

// attrSet builds one attribute map under the count and size limits.
type attrSet struct {
	m   map[string]string
	lim *Limits
}

func newAttrSet(lim *Limits) attrSet {
	return attrSet{m: map[string]string{}, lim: lim}
}

const serviceNameKey = "service.name"

// has reports whether key may be stored: it is already present or there is
// room for one more.
func (a *attrSet) has(key string) bool {
	// service.name is always kept: it feeds the ServiceName column.
	if len(a.m) < a.lim.MaxAttributes || key == serviceNameKey {
		return true
	}
	_, ok := a.m[key]
	return ok
}

func (a *attrSet) put(key, value string) {
	a.m[key] = truncate(value, a.lim.MaxValueBytes)
}

// idHex renders a trace or span id the way the exporter does: lowercase hex,
// or empty when the id is unset or all zero. size is 16 for trace ids and 8
// for span ids; any other non-empty length is an error, as in pdata.
func idHex(b []byte, size int, what string) (string, error) {
	if len(b) == 0 {
		return "", nil
	}
	if len(b) != size {
		return "", fmt.Errorf("%w: %s must be %d bytes, got %d", ErrMalformed, what, size, len(b))
	}
	for _, c := range b {
		if c != 0 {
			return hex.EncodeToString(b), nil
		}
	}
	return "", nil
}

// idFromJSON decodes an OTLP JSON id. The spec says hex; base64 (the plain
// proto3 JSON mapping for bytes) is accepted when it has the right length.
func idFromJSON(s string, size int, what string) (string, error) {
	if s == "" {
		return "", nil
	}
	if len(s) == size*2 {
		if b, err := hex.DecodeString(s); err == nil {
			return idHex(b, size, what)
		}
	}
	if b, err := decodeBase64(s); err == nil && len(b) == size {
		return idHex(b, size, what)
	}
	return "", fmt.Errorf("%w: %s %q is not %d hex-encoded bytes", ErrMalformed, what, truncate(s, 64), size)
}

// decodeBase64 accepts standard and URL-safe alphabets, padded or not, as
// proto3 JSON parsers do.
func decodeBase64(s string) ([]byte, error) {
	if strings.ContainsAny(s, "-_") {
		return base64.RawURLEncoding.DecodeString(strings.TrimRight(s, "="))
	}
	return base64.RawStdEncoding.DecodeString(strings.TrimRight(s, "="))
}

// spanKindNames are the exporter's SpanKind strings (pdata SpanKind.String).
var spanKindNames = [...]string{"Unspecified", "Internal", "Server", "Client", "Producer", "Consumer"}

// spanKindString maps the proto enum to the exporter's string. Unknown
// values read as Unspecified.
func spanKindString(v int64) string {
	if v < 0 || v >= int64(len(spanKindNames)) {
		return spanKindNames[0]
	}
	return spanKindNames[v]
}

// statusCodeNames are the exporter's StatusCode strings (pdata
// StatusCode.String), which internal/telemetry compares against.
var statusCodeNames = [...]string{"Unset", "Ok", "Error"}

func statusCodeString(v int64) string {
	if v < 0 || v >= int64(len(statusCodeNames)) {
		return statusCodeNames[0]
	}
	return statusCodeNames[v]
}

// severityNumberByName maps the OTLP JSON enum names (SEVERITY_NUMBER_INFO2
// and so on) to their numbers.
var severityNumberByName = func() map[string]int64 {
	m := map[string]int64{"SEVERITY_NUMBER_UNSPECIFIED": 0}
	for i, base := range []string{"TRACE", "DEBUG", "INFO", "WARN", "ERROR", "FATAL"} {
		for j, suffix := range []string{"", "2", "3", "4"} {
			m["SEVERITY_NUMBER_"+base+suffix] = int64(i*4 + j + 1)
		}
	}
	return m
}()

var spanKindByName = map[string]int64{
	"SPAN_KIND_UNSPECIFIED": 0, "SPAN_KIND_INTERNAL": 1, "SPAN_KIND_SERVER": 2,
	"SPAN_KIND_CLIENT": 3, "SPAN_KIND_PRODUCER": 4, "SPAN_KIND_CONSUMER": 5,
}

var statusCodeByName = map[string]int64{
	"STATUS_CODE_UNSET": 0, "STATUS_CODE_OK": 1, "STATUS_CODE_ERROR": 2,
}

// appendFloat renders a double like pcommon Value.AsString: the ES6 number
// to string conversion encoding/json uses.
func appendFloat(dst []byte, f float64) []byte {
	abs := f
	if abs < 0 {
		abs = -abs
	}
	format := byte('f')
	if abs != 0 && (abs < 1e-6 || abs >= 1e21) {
		format = 'e'
	}
	dst = strconv.AppendFloat(dst, f, format, -1, 64)
	if format == 'e' {
		// e-09 to e-9
		n := len(dst)
		if n >= 4 && dst[n-4] == 'e' && dst[n-3] == '-' && dst[n-2] == '0' {
			dst[n-2] = dst[n-1]
			dst = dst[:n-1]
		}
	}
	return dst
}

// kvJSON is one entry of a map value while it is rendered as JSON.
type kvJSON struct {
	key string
	val []byte
}

// appendKVObject writes entries as a JSON object with sorted keys, the last
// duplicate winning, matching json.Marshal of the map pdata builds.
func appendKVObject(dst []byte, entries []kvJSON) []byte {
	// Stable sort keeps wire order among duplicates, so the last one wins.
	slices.SortStableFunc(entries, func(a, b kvJSON) int { return strings.Compare(a.key, b.key) })
	dst = append(dst, '{')
	first := true
	for i, e := range entries {
		if i+1 < len(entries) && entries[i+1].key == e.key {
			continue
		}
		if !first {
			dst = append(dst, ',')
		}
		first = false
		dst = appendJSONString(dst, e.key)
		dst = append(dst, ':')
		dst = append(dst, e.val...)
	}
	return append(dst, '}')
}
