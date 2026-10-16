package ingest

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/caioricciuti/ch-ui/internal/ingest/otlp"
)

func TestParseTime(t *testing.T) {
	want := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		in   any
		want time.Time
		ok   bool
	}{
		{"2026-10-16T12:00:00Z", want, true},
		{"2026-10-16T12:00:00.123Z", want.Add(123 * time.Millisecond), true},
		{"2026-10-16T14:00:00+02:00", want, true},
		{"2026-10-16T14:00:00.5+0200", want.Add(500 * time.Millisecond), true},
		{"2026-10-16T12:00:00", want, true},
		{"2026-10-16 12:00:00.000001", want.Add(time.Microsecond), true},
		{json.Number("1792152000"), want, true},
		{json.Number("1792152000.25"), want.Add(250 * time.Millisecond), true},
		{json.Number("1792152000123"), want.Add(123 * time.Millisecond), true},
		{json.Number("1792152000123456"), want.Add(123456 * time.Microsecond), true},
		{json.Number("1792152000123456789"), want.Add(123456789), true},
		{json.Number("1.792152e9"), want, true},
		{"1792152000", want, true},
		{float64(1792152000), want, true},
		{json.Number("0"), time.Time{}, false},
		{json.Number("-5"), time.Time{}, false},
		{"yesterday", time.Time{}, false},
		{"1792152000.x", time.Time{}, false},
		{"1900-01-01T00:00:00Z", time.Time{}, false},
		{true, time.Time{}, false},
		{map[string]any{}, time.Time{}, false},
	}
	for _, c := range cases {
		got, ok := parseTime(c.in)
		if ok != c.ok || !got.Equal(c.want) {
			t.Errorf("parseTime(%#v) = %v %v, want %v %v", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestSeverityNumber(t *testing.T) {
	for in, want := range map[string]int32{"TRACE": 1, "debug": 5, "Info": 9, "notice": 10, "WARNING": 13, "warn": 13, "err": 17, "ERROR": 17, "crit": 19, "fatal": 21, "panic": 21, "verbose": 0, "": 0} {
		if got := severityNumber(in); got != want {
			t.Errorf("severityNumber(%q) = %d, want %d", in, got, want)
		}
	}
}

func mustDoc(t *testing.T, s string) map[string]any {
	t.Helper()
	doc, err := decodeObject([]byte(s))
	if err != nil {
		t.Fatalf("%s: %v", s, err)
	}
	return doc
}

func TestDecodeObject(t *testing.T) {
	for _, bad := range []string{"", "null", "[1]", "7", `"s"`, `{"a":1} {"b":2}`, `{"a":`} {
		if _, err := decodeObject([]byte(bad)); err == nil {
			t.Errorf("decodeObject(%q) accepted", bad)
		}
	}
	doc := mustDoc(t, `{"n": 12345678901234567890}`)
	if doc["n"] != json.Number("12345678901234567890") {
		t.Fatalf("number not kept as written: %#v", doc["n"])
	}
}

func TestLogRowMapping(t *testing.T) {
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	m := newDocMapper(otlp.Limits{}, now)

	// ECS: nested log.level, a host object, a log object kept as attributes.
	row := m.logRow(mustDoc(t, `{"@timestamp":"2026-10-16T11:00:00Z","message":"boom","log":{"level":"error","file":{"path":"/var/log/a.log"}},"host":{"name":"web-1","ip":["10.0.0.1"]},"service":{"name":"checkout"},"user":{"id":7,"admin":false},"note":null}`), map[string]string{"es.index": "logs"})
	if !row.Timestamp.Equal(now.Add(-time.Hour)) || row.Body != "boom" || row.SeverityText != "error" || row.SeverityNumber != 17 || row.ServiceName != "checkout" {
		t.Fatalf("row: %+v", row)
	}
	wantRes := map[string]string{"host.name": "web-1", "host.ip": `["10.0.0.1"]`, "service.name": "checkout"}
	wantAttrs := map[string]string{"log.file.path": "/var/log/a.log", "user.id": "7", "user.admin": "false", "es.index": "logs"}
	if !equalMaps(row.ResourceAttributes, wantRes) || !equalMaps(row.LogAttributes, wantAttrs) {
		t.Fatalf("resource %v attrs %v", row.ResourceAttributes, row.LogAttributes)
	}

	// No timestamp, a bare host string, "log" as the body, a literal
	// dotted key, an unparseable time kept as an attribute.
	row = m.logRow(mustDoc(t, `{"log":"line\n","host":"node-3","log.level":"WARN","time":"soon","kubernetes.pod_name":"api-1"}`), nil)
	if !row.Timestamp.Equal(now) || row.Body != "line\n" || row.SeverityNumber != 13 || row.ServiceName != "node-3" {
		t.Fatalf("row: %+v", row)
	}
	if !equalMaps(row.ResourceAttributes, map[string]string{"host.name": "node-3", "kubernetes.pod_name": "api-1"}) || !equalMaps(row.LogAttributes, map[string]string{"time": "soon"}) {
		t.Fatalf("resource %v attrs %v", row.ResourceAttributes, row.LogAttributes)
	}

	// No body field: the rest of the document is the body, not attributes;
	// the extra attributes stay.
	row = m.logRow(mustDoc(t, `{"timestamp":1792152000000,"cpu":0.5,"host":{"name":"h"},"tags":["a"]}`), map[string]string{"es.index": "metrics"})
	if row.Body != `{"cpu":0.5,"tags":["a"]}` || !equalMaps(row.LogAttributes, map[string]string{"es.index": "metrics"}) || !row.Timestamp.Equal(now) {
		t.Fatalf("row: %+v", row)
	}

	// A JSON object as the body field becomes JSON.
	row = m.logRow(mustDoc(t, `{"event":{"b":1,"a":"x"}}`), nil)
	if row.Body != `{"a":"x","b":1}` {
		t.Fatalf("object body: %q", row.Body)
	}
}

func TestLogRowLimits(t *testing.T) {
	m := newDocMapper(otlp.Limits{MaxAttributes: 3, MaxValueBytes: 4, MaxBodyBytes: 5, MaxDepth: 2}, time.Now())
	row := m.logRow(mustDoc(t, `{"message":"héllo world","a":"123456","b":{"c":{"d":{"e":1}}},"z1":"1","z2":"2"}`), map[string]string{"es.index": "i"})
	// Body cut at 5 bytes without splitting é (2 bytes): "héll".
	if row.Body != "héll" {
		t.Fatalf("body %q", row.Body)
	}
	// es.index first, then keys in order until three: a, then b.c.d (JSON
	// past depth 2, cut to 4 bytes).
	want := map[string]string{"es.index": "i", "a": "1234", "b.c.d": `{"e"`}
	if !equalMaps(row.LogAttributes, want) {
		t.Fatalf("attrs %v", row.LogAttributes)
	}
}

func equalMaps(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if bv, ok := b[k]; !ok || bv != v {
			return false
		}
	}
	return true
}

func TestTruncateString(t *testing.T) {
	if got := truncateString("aé", 2); got != "a" {
		t.Fatalf("%q", got)
	}
	if got := truncateString(strings.Repeat("x", 3), 5); got != "xxx" {
		t.Fatalf("%q", got)
	}
}
