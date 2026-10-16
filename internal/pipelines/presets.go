package pipelines

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// Table presets of the ClickHouse sink: the layout of the table it creates
// and, for wide events, how each record is mapped onto it.
const (
	// PresetCustom creates columns inferred from the data (the default).
	PresetCustom = "custom"
	// PresetWideEvents is one table for JSON events and logs: a few typed
	// columns plus everything else in attributes.
	PresetWideEvents = "wide_events"
	// PresetOTelLogs and PresetOTelTraces are the OpenTelemetry exporter
	// layouts Telemetry reads.
	PresetOTelLogs   = "otel_logs"
	PresetOTelTraces = "otel_traces"
)

// maxTTLDays bounds ttl_days (100 years).
const maxTTLDays = 36500

// sinkPreset returns the sink's table preset. A sink without one (every
// sink saved before presets existed) is custom, unless the runner set the
// OpenTelemetry signal of an ingest source: logs and traces keep their
// exporter layout.
func sinkPreset(fields map[string]interface{}) string {
	switch p := stringField(fields, "preset", ""); p {
	case PresetCustom, PresetWideEvents, PresetOTelLogs, PresetOTelTraces:
		return p
	}
	switch stringField(fields, sinkOTelSignal, "") {
	case SignalLogs:
		return PresetOTelLogs
	case SignalTraces:
		return PresetOTelTraces
	}
	return PresetCustom
}

// otelPreset reports whether a preset is an OpenTelemetry layout.
func otelPreset(p string) bool { return p == PresetOTelLogs || p == PresetOTelTraces }

// validatePresetFields checks preset, ttl_days and mapping.
func validatePresetFields(fields map[string]interface{}) error {
	if v, ok := fields["preset"]; ok && v != nil && v != "" {
		switch v {
		case PresetCustom, PresetWideEvents, PresetOTelLogs, PresetOTelTraces:
		default:
			return fmt.Errorf("unknown table layout %v", v)
		}
	}
	if _, err := sinkTTLDays(fields); err != nil {
		return err
	}
	_, err := wideMapping(fields)
	return err
}

// sinkTTLDays reads ttl_days: 0 (or unset) keeps data forever.
func sinkTTLDays(fields map[string]interface{}) (int, error) {
	v, ok := fields["ttl_days"]
	if !ok || v == nil || v == "" {
		return 0, nil
	}
	f, ok := v.(float64)
	if n, isInt := v.(int); isInt {
		f, ok = float64(n), true
	}
	if !ok || f != math.Trunc(f) || f < 0 || f > maxTTLDays {
		return 0, fmt.Errorf("ttl_days must be a whole number between 0 and %d", maxTTLDays)
	}
	return int(f), nil
}

// Roles of the wide events layout: the column each is written to, and the
// record fields looked up for it, first match wins. A dotted name matches
// a literal key with dots or a nested path. The capitalised names are the
// OpenTelemetry fields of an ingest logs source.
var wideRoles = []struct {
	role, column string
	defaults     []string
}{
	{"timestamp", "timestamp", []string{"@timestamp", "timestamp", "time", "ts", "Timestamp"}},
	{"service", "service", []string{"service.name", "service", "host.name", "app", "ServiceName"}},
	{"level", "level", []string{"level", "log.level", "severity", "levelname", "SeverityText"}},
	{"message", "message", []string{"message", "msg", "log", "event", "Body"}},
	{"trace_id", "trace_id", []string{"trace_id", "traceId", "trace.id", "TraceId"}},
}

// wideMapping returns the field names looked up for each role: the sink's
// mapping (a comma-separated list or an array of names per role) or the
// defaults.
func wideMapping(fields map[string]interface{}) (map[string][]string, error) {
	out := make(map[string][]string, len(wideRoles))
	for _, r := range wideRoles {
		out[r.role] = r.defaults
	}
	v, ok := fields["mapping"]
	if !ok || v == nil {
		return out, nil
	}
	m, ok := v.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("mapping must be an object of role: field names")
	}
	for role, raw := range m {
		if _, known := out[role]; !known {
			return nil, fmt.Errorf("mapping has an unknown role %q", role)
		}
		var names []string
		switch x := raw.(type) {
		case nil:
		case string:
			names = strings.Split(x, ",")
		case []interface{}:
			for _, e := range x {
				s, ok := e.(string)
				if !ok {
					return nil, fmt.Errorf("mapping.%s must list field names", role)
				}
				names = append(names, s)
			}
		default:
			return nil, fmt.Errorf("mapping.%s must list field names", role)
		}
		var clean []string
		for _, n := range names {
			if n = strings.TrimSpace(n); n != "" {
				if len(n) > 200 {
					return nil, fmt.Errorf("mapping.%s has a field name that is too long", role)
				}
				clean = append(clean, n)
			}
		}
		if len(clean) > 0 {
			out[role] = clean
		}
	}
	return out, nil
}

// jsonTypeMajor.jsonTypeMinor is the first ClickHouse release where the JSON type is
// production ready and on by default. Older servers get a Map for objects.
const jsonTypeMajor, jsonTypeMinor = 25, 3

// jsonTypeSupported reports whether a version() string is at least 25.3.
func jsonTypeSupported(version string) bool {
	parts := strings.SplitN(strings.TrimSpace(version), ".", 3)
	if len(parts) < 2 {
		return false
	}
	major, err1 := strconv.Atoi(parts[0])
	minor, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return false
	}
	return major > jsonTypeMajor || major == jsonTypeMajor && minor >= jsonTypeMinor
}

// Types of the attributes column: the JSON type, or a map for servers
// before 25.3.
const (
	wideAttrsJSON = "JSON"
	wideAttrsMap  = "Map(LowCardinality(String), String)"
)

// wideColumns are the column definitions of the wide events layout.
func wideColumns(jsonType, keepRaw bool) []SchemaColumn {
	attrs := wideAttrsMap + " CODEC(ZSTD(1))"
	if jsonType {
		attrs = wideAttrsJSON
	}
	cols := []SchemaColumn{
		{Name: "timestamp", Type: "DateTime64(3) CODEC(Delta, ZSTD(1))"},
		{Name: "service", Type: "LowCardinality(String)"},
		{Name: "level", Type: "LowCardinality(String)"},
		{Name: "message", Type: "String CODEC(ZSTD(1))"},
		{Name: "trace_id", Type: "String"},
		{Name: "attributes", Type: attrs},
	}
	if keepRaw {
		cols = append(cols, SchemaColumn{Name: "raw", Type: "String CODEC(ZSTD(3))"})
	}
	return cols
}

// wideEventsDDL is the CREATE TABLE of the wide events layout. The ui
// builds the same statement for its preview (createTableDDL).
func wideEventsDDL(db, table, engine string, jsonType, keepRaw bool, ttlDays int) string {
	var lines []string
	for _, c := range wideColumns(jsonType, keepRaw) {
		lines = append(lines, quoteIdent(c.Name)+" "+c.Type)
	}
	lines = append(lines,
		"INDEX idx_message `message` TYPE tokenbf_v1(32768, 3, 0) GRANULARITY 4",
		"INDEX idx_trace_id `trace_id` TYPE bloom_filter(0.001) GRANULARITY 1",
	)
	ddl := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s.%s (\n  %s\n) ENGINE = %s\nPARTITION BY toDate(`timestamp`)\nORDER BY (`service`, `level`, `timestamp`)",
		quoteIdent(db), quoteIdent(table), strings.Join(lines, ",\n  "), engine)
	if ttlDays > 0 {
		ddl += fmt.Sprintf("\nTTL toDateTime(`timestamp`) + toIntervalDay(%d)", ttlDays)
	}
	var settings []string
	if !replicatedEngine(engine) {
		settings = append(settings, fmt.Sprintf("non_replicated_deduplication_window = %d", dedupWindow))
	}
	if ttlDays > 0 {
		settings = append(settings, "ttl_only_drop_parts = 1")
	}
	if len(settings) > 0 {
		ddl += "\nSETTINGS " + strings.Join(settings, ", ")
	}
	return ddl
}

// wideTarget is how rows are shaped for the existing wide events table.
type wideTarget struct {
	// attrs is the attributes column's encoding: "json" (nested object),
	// "map" (flat dotted keys, string values) or "string" (JSON text).
	attrs string
	// raw writes the original record to the raw column.
	raw bool
}

// attrsEncoding picks the attributes encoding for a column type.
func attrsEncoding(colType string) string {
	switch {
	case strings.HasPrefix(colType, "JSON"), strings.HasPrefix(colType, "Object"):
		return "json"
	case strings.HasPrefix(colType, "Map"):
		return "map"
	default:
		return "string"
	}
}

// wideRow maps one record onto the wide events columns. Fields used for a
// role are removed from attributes; everything else stays there. A record
// without a usable timestamp gets now.
func wideRow(rec Record, roles map[string][]string, t wideTarget, now time.Time) map[string]interface{} {
	data := decodeRecord(rec) // a copy this function may change
	row := make(map[string]interface{}, 7)

	ts := now
	if v, ok := takeField(data, roles["timestamp"], func(v interface{}) bool { _, ok := parseEventTime(v); return ok }); ok {
		ts, _ = parseEventTime(v)
	}
	row["timestamp"] = ts.UTC().Format("2006-01-02T15:04:05.000Z")

	isString := func(v interface{}) bool { _, ok := v.(string); return ok }
	for _, r := range []string{"service", "message", "trace_id"} {
		v, _ := takeField(data, roles[r], isString)
		s, _ := v.(string)
		row[r] = s
	}
	lv, _ := takeField(data, roles["level"], func(v interface{}) bool { return canonicalLevel(v) != "" })
	row["level"] = canonicalLevel(lv)

	switch t.attrs {
	case "map":
		flat := map[string]string{}
		flattenAttrs("", data, flat)
		row["attributes"] = flat
	case "string":
		b, _ := marshalCompact(data)
		row["attributes"] = string(b)
	default:
		row["attributes"] = data
	}
	if t.raw {
		row["raw"] = rawJSON(rec)
	}
	return row
}

// rawJSON is a record's original JSON, compacted.
func rawJSON(rec Record) string {
	var buf bytes.Buffer
	if len(rec.RawJSON) > 0 && json.Compact(&buf, rec.RawJSON) == nil {
		return buf.String()
	}
	b, _ := marshalCompact(rec.Data)
	return string(b)
}

// marshalCompact is json.Marshal without HTML escaping.
func marshalCompact(v interface{}) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// takeField finds the first of names whose value passes ok, removes it from
// data and returns it. A name matches a literal key first, then a nested
// path split at dots; parents left empty by the removal are removed too.
func takeField(data map[string]interface{}, names []string, ok func(interface{}) bool) (interface{}, bool) {
	for _, name := range names {
		if v, found := data[name]; found && ok(v) {
			delete(data, name)
			return v, true
		}
		if !strings.Contains(name, ".") {
			continue
		}
		if v, found := takePath(data, strings.Split(name, "."), ok); found {
			return v, true
		}
	}
	return nil, false
}

func takePath(m map[string]interface{}, path []string, ok func(interface{}) bool) (interface{}, bool) {
	v, found := m[path[0]]
	if !found {
		return nil, false
	}
	if len(path) == 1 {
		if !ok(v) {
			return nil, false
		}
		delete(m, path[0])
		return v, true
	}
	child, isMap := v.(map[string]interface{})
	if !isMap {
		return nil, false
	}
	got, found := takePath(child, path[1:], ok)
	if found && len(child) == 0 {
		delete(m, path[0])
	}
	return got, found
}

// flattenAttrs writes nested maps as dotted keys with string values. Nulls
// are dropped; arrays become their JSON text.
func flattenAttrs(prefix string, m map[string]interface{}, out map[string]string) {
	for k, v := range m {
		key := k
		if prefix != "" {
			key = prefix + "." + k
		}
		switch x := v.(type) {
		case nil:
		case map[string]interface{}:
			flattenAttrs(key, x, out)
		default:
			out[key] = scalarString(x)
		}
	}
}

// scalarString renders a decoded JSON value as text: strings as they are,
// numbers and bools as JSON, arrays and objects as JSON text.
func scalarString(v interface{}) string {
	switch x := v.(type) {
	case string:
		return x
	case json.Number:
		return x.String()
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(x)
	default:
		b, _ := marshalCompact(x)
		return string(b)
	}
}

// levelNames maps lowercased level spellings to the canonical names.
var levelNames = map[string]string{
	"trace": "trace", "trc": "trace", "verbose": "trace", "finest": "trace",
	"debug": "debug", "dbg": "debug", "fine": "debug",
	"info": "info", "inf": "info", "information": "info", "informational": "info", "notice": "info",
	"warn": "warn", "warning": "warn", "wrn": "warn",
	"error": "error", "err": "error", "eror": "error",
	"fatal": "fatal", "critical": "fatal", "crit": "fatal", "panic": "fatal", "emerg": "fatal", "emergency": "fatal", "alert": "fatal", "dpanic": "fatal",
}

// canonicalLevel returns trace/debug/info/warn/error/fatal for a level
// string or number (pino 10..60, OpenTelemetry severity 1..24). An unknown
// string is kept lowercased; anything else is "".
func canonicalLevel(v interface{}) string {
	var n float64
	switch x := v.(type) {
	case string:
		s := strings.ToLower(strings.TrimSpace(x))
		if c, ok := levelNames[s]; ok {
			return c
		}
		if f, err := strconv.ParseFloat(s, 64); err == nil {
			n = f
			break
		}
		return s
	case json.Number:
		f, err := x.Float64()
		if err != nil {
			return ""
		}
		n = f
	case float64:
		n = x
	default:
		return ""
	}
	if n >= 10 && n <= 60 && math.Mod(n, 10) == 0 {
		return [...]string{"trace", "debug", "info", "warn", "error", "fatal"}[int(n)/10-1]
	}
	if n >= 1 && n <= 24 {
		return [...]string{"trace", "debug", "info", "warn", "error", "fatal"}[(int(n)-1)/4]
	}
	return ""
}

// eventTimeLayouts are the ISO 8601 / RFC 3339 shapes parseEventTime
// reads. Times without a zone are UTC.
var eventTimeLayouts = []string{
	time.RFC3339Nano,
	"2006-01-02T15:04:05.999999999Z0700",
	"2006-01-02T15:04:05.999999999",
	"2006-01-02 15:04:05.999999999Z07:00",
	"2006-01-02 15:04:05.999999999Z0700",
	"2006-01-02 15:04:05.999999999 -0700",
	"2006-01-02 15:04:05.999999999",
	"2006-01-02T15:04Z07:00",
	"2006-01-02T15:04",
	"2006-01-02 15:04",
}

// parseEventTime reads an ISO 8601 / RFC 3339 string, or a Unix epoch in
// seconds, milliseconds, microseconds or nanoseconds (picked by magnitude),
// as a number or a numeric string.
func parseEventTime(v interface{}) (time.Time, bool) {
	var n float64
	switch x := v.(type) {
	case string:
		s := strings.TrimSpace(x)
		if s == "" {
			return time.Time{}, false
		}
		for _, layout := range eventTimeLayouts {
			if t, err := time.Parse(layout, s); err == nil {
				return t, true
			}
		}
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return time.Time{}, false
		}
		n = f
	case json.Number:
		f, err := x.Float64()
		if err != nil {
			return time.Time{}, false
		}
		n = f
	case float64:
		n = x
	default:
		return time.Time{}, false
	}
	if n <= 0 || math.IsInf(n, 0) || math.IsNaN(n) {
		return time.Time{}, false
	}
	switch {
	case n < 1e11: // seconds, up to the year 5138
		sec, frac := math.Modf(n)
		return time.Unix(int64(sec), int64(frac*1e9)), true
	case n < 1e14:
		return time.UnixMilli(int64(n)), true
	case n < 1e17:
		return time.UnixMicro(int64(n)), true
	case n < 9.2e18:
		return time.Unix(0, int64(n)), true
	}
	return time.Time{}, false
}
