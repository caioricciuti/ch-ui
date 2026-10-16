package ingest

import (
	"bytes"
	"encoding/json"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/caioricciuti/ch-ui/internal/ingest/otlp"
)

// Mapping of free-form JSON log documents (Elasticsearch bulk documents and
// Splunk HEC events) to otel_logs rows, so Telemetry reads them like OTLP
// logs.

// timestampKeys are tried in order for the record time.
var timestampKeys = []string{"@timestamp", "timestamp", "time"}

// severityPaths are tried in order for the severity text.
var severityPaths = []string{"level", "log.level", "severity"}

// bodyKeys are tried in order for the body.
var bodyKeys = []string{"message", "log", "event"}

// servicePaths are tried in order for ServiceName.
var servicePaths = []string{"service.name", "service", "host.name", "host"}

// resourceKeys are the top-level fields that describe where a record comes
// from; they go to ResourceAttributes.
var resourceKeys = []string{"agent", "cloud", "container", "host", "kubernetes", "service"}

// docMapper maps documents of one request.
type docMapper struct {
	lim otlp.Limits
	now time.Time
}

func newDocMapper(lim otlp.Limits, now time.Time) *docMapper {
	d := otlp.DefaultLimits()
	if lim.MaxAttributes > 0 {
		d.MaxAttributes = lim.MaxAttributes
	}
	if lim.MaxValueBytes > 0 {
		d.MaxValueBytes = lim.MaxValueBytes
	}
	if lim.MaxBodyBytes > 0 {
		d.MaxBodyBytes = lim.MaxBodyBytes
	}
	if lim.MaxDepth > 0 {
		d.MaxDepth = lim.MaxDepth
	}
	if lim.MaxRecords > 0 {
		d.MaxRecords = lim.MaxRecords
	}
	return &docMapper{lim: d, now: now}
}

// decodeObject decodes one JSON object with numbers kept as written.
func decodeObject(b []byte) (map[string]any, error) {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var doc map[string]any
	if err := dec.Decode(&doc); err != nil {
		return nil, err
	}
	if doc == nil {
		return nil, errNotObject
	}
	if dec.More() {
		return nil, errTrailingData
	}
	return doc, nil
}

type mapError string

func (e mapError) Error() string { return string(e) }

const (
	errNotObject    mapError = "expected a JSON object"
	errTrailingData mapError = "unexpected data after the JSON object"
)

// logRow maps doc to a row. doc is consumed: the fields that fill columns
// are removed from it. extra attributes (es.index, splunk.*) are added to
// LogAttributes and win over the document's. A document without a body
// field gets the JSON of its remaining fields as the body, and no
// LogAttributes from them.
func (m *docMapper) logRow(doc map[string]any, extra map[string]string) otlp.LogRow {
	row := otlp.LogRow{Timestamp: m.now.UTC()}
	for _, k := range timestampKeys {
		if v, ok := doc[k]; ok {
			if t, ok := parseTime(v); ok {
				row.Timestamp = t
				delete(doc, k)
				break
			}
		}
	}
	for _, p := range severityPaths {
		if v, ok := lookup(doc, p); ok {
			if s, ok := scalarString(v); ok && s != "" {
				row.SeverityText = truncateString(s, m.lim.MaxValueBytes)
				row.SeverityNumber = severityNumber(s)
				remove(doc, p)
				break
			}
		}
	}
	for _, p := range servicePaths {
		if v, ok := lookup(doc, p); ok {
			if s, ok := v.(string); ok && s != "" {
				row.ServiceName = truncateString(s, m.lim.MaxValueBytes)
				break
			}
		}
	}

	res := m.attrs()
	for _, k := range sortedKeys(doc) {
		top, _, _ := strings.Cut(k, ".")
		if !slices.Contains(resourceKeys, top) {
			continue
		}
		if s, ok := doc[k].(string); ok && k == "host" {
			// A bare host string (Fluent Bit, Vector, syslog) is the host name.
			res.put("host.name", s)
		} else {
			res.flatten(k, doc[k], 0)
		}
		delete(doc, k)
	}
	row.ResourceAttributes = res.m

	body, found := "", false
	for _, k := range bodyKeys {
		if v, ok := doc[k]; ok && v != nil {
			body, found = valueString(v), true
			delete(doc, k)
			break
		}
	}
	la := m.attrs()
	for _, k := range sortedKeys(extra) {
		la.put(k, extra[k])
	}
	if found {
		for _, k := range sortedKeys(doc) {
			la.flatten(k, doc[k], 0)
		}
	} else if len(doc) > 0 {
		body = valueString(doc)
	}
	row.Body = truncateString(body, m.lim.MaxBodyBytes)
	row.LogAttributes = la.m
	return row
}

// attrSet builds one attribute map under the count and size limits: past
// MaxAttributes keys are dropped, values are cut at MaxValueBytes, and
// objects nested deeper than MaxDepth are kept as JSON.
type attrSet struct {
	m   map[string]string
	lim *otlp.Limits
}

func (m *docMapper) attrs() *attrSet {
	return &attrSet{m: map[string]string{}, lim: &m.lim}
}

func (a *attrSet) put(key, value string) {
	if _, ok := a.m[key]; !ok && len(a.m) >= a.lim.MaxAttributes {
		return
	}
	a.m[key] = truncateString(value, a.lim.MaxValueBytes)
}

// flatten stores v under key, objects as dotted keys in key order, arrays as
// JSON, nulls not at all. Keys already set (the extra attributes) are kept.
func (a *attrSet) flatten(key string, v any, depth int) {
	switch t := v.(type) {
	case nil:
		return
	case map[string]any:
		if depth < a.lim.MaxDepth {
			for _, k := range sortedKeys(t) {
				a.flatten(key+"."+k, t[k], depth+1)
			}
			return
		}
	}
	if _, ok := a.m[key]; ok {
		return
	}
	a.put(key, valueString(v))
}

// valueString renders a JSON value as an attribute or body string: strings
// as they are, everything else as compact JSON.
func valueString(v any) string {
	if s, ok := scalarString(v); ok {
		return s
	}
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

// scalarString renders a string, number or bool.
func scalarString(v any) (string, bool) {
	switch t := v.(type) {
	case string:
		return t, true
	case json.Number:
		return t.String(), true
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64), true
	case bool:
		return strconv.FormatBool(t), true
	}
	return "", false
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// lookup finds a dotted path either as a literal key ("log.level") or
// through nested objects ({"log":{"level":...}}).
func lookup(doc map[string]any, path string) (any, bool) {
	if v, ok := doc[path]; ok {
		return v, true
	}
	head, rest, ok := strings.Cut(path, ".")
	if !ok {
		return nil, false
	}
	child, isMap := doc[head].(map[string]any)
	if !isMap {
		return nil, false
	}
	return lookup(child, rest)
}

// remove deletes what lookup found, and parents it leaves empty.
func remove(doc map[string]any, path string) {
	if _, ok := doc[path]; ok {
		delete(doc, path)
		return
	}
	head, rest, ok := strings.Cut(path, ".")
	if !ok {
		return
	}
	if child, isMap := doc[head].(map[string]any); isMap {
		remove(child, rest)
		if len(child) == 0 {
			delete(doc, head)
		}
	}
}

// truncateString cuts s to at most n bytes without splitting a UTF-8
// sequence.
func truncateString(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return strings.ToValidUTF8(s[:n], "")
}

// severityNumber maps a level name to the OTLP severity number, 0 when the
// name is not known.
func severityNumber(s string) int32 {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "trace", "trc":
		return 1
	case "debug", "dbg":
		return 5
	case "info", "information", "informational", "inf":
		return 9
	case "notice":
		return 10
	case "warn", "warning", "wrn":
		return 13
	case "error", "err", "eror":
		return 17
	case "critical", "crit", "alert":
		return 19
	case "fatal", "emerg", "emergency", "panic":
		return 21
	}
	return 0
}

// timeLayouts are the date strings parseTime reads. Layouts without a zone
// are UTC.
var timeLayouts = []string{
	time.RFC3339Nano,
	"2006-01-02T15:04:05.999999999Z0700",
	"2006-01-02T15:04:05.999999999",
	"2006-01-02 15:04:05.999999999Z07:00",
	"2006-01-02 15:04:05.999999999Z0700",
	"2006-01-02 15:04:05.999999999",
}

// parseTime reads an ISO 8601 string or an epoch number (or numeric string)
// in seconds, milliseconds, microseconds or nanoseconds, told apart by
// magnitude. Times before 1970 or past 2262 are refused.
func parseTime(v any) (time.Time, bool) {
	var s string
	switch t := v.(type) {
	case string:
		s = strings.TrimSpace(t)
		for _, layout := range timeLayouts {
			if ts, err := time.Parse(layout, s); err == nil {
				return checkedTime(ts)
			}
		}
	case json.Number:
		s = t.String()
	case float64:
		s = strconv.FormatFloat(t, 'f', -1, 64)
	default:
		return time.Time{}, false
	}
	return parseEpoch(s)
}

func checkedTime(t time.Time) (time.Time, bool) {
	if t.Year() < 1970 || t.Year() > 2262 {
		return time.Time{}, false
	}
	return t.UTC(), true
}

// parseEpoch reads "<int>[.<frac>]" exactly, or a float in seconds when it
// has an exponent.
func parseEpoch(s string) (time.Time, bool) {
	intPart, frac, _ := strings.Cut(s, ".")
	n, err := strconv.ParseInt(intPart, 10, 64)
	if err != nil || n <= 0 || strings.ContainsAny(frac, "eE+-") {
		f, ferr := strconv.ParseFloat(s, 64)
		if ferr != nil || f <= 0 || f > math.MaxInt64/1e9 {
			return time.Time{}, false
		}
		return checkedTime(time.Unix(0, int64(f*1e9)))
	}
	for _, c := range frac {
		if c < '0' || c > '9' {
			return time.Time{}, false
		}
	}
	// Digits of the unit below nanoseconds: 9 for seconds, 6 for ms, 3 for
	// microseconds, 0 for nanoseconds.
	digits := 0
	switch {
	case n < 1e11:
		digits = 9
	case n < 1e14:
		digits = 6
	case n < 1e17:
		digits = 3
	}
	scale := int64(math.Pow10(digits))
	if n > math.MaxInt64/scale {
		return time.Time{}, false
	}
	frac = (frac + "000000000")[:digits]
	f, _ := strconv.ParseInt("0"+frac, 10, 64)
	return checkedTime(time.Unix(0, n*scale+f))
}
