package ingest

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/caioricciuti/ch-ui/internal/ingest/spool"
)

func (e *testEnv) hec(path, body string, headers map[string]string) *httptest.ResponseRecorder {
	e.t.Helper()
	if headers == nil {
		headers = map[string]string{"Authorization": "Splunk " + e.key}
	}
	return e.send(http.MethodPost, path, []byte(body), headers)
}

type hecResponse struct {
	Text    string `json:"text"`
	Code    int    `json:"code"`
	Invalid *int   `json:"invalid-event-number"`
}

func expectHEC(t *testing.T, rec *httptest.ResponseRecorder, status, code int) hecResponse {
	t.Helper()
	var r hecResponse
	if rec.Code != status || json.Unmarshal(rec.Body.Bytes(), &r) != nil || r.Code != code || r.Text == "" {
		t.Fatalf("want %d code %d, got %d %s", status, code, rec.Code, rec.Body)
	}
	return r
}

// Two events back to back with no separator, as the Splunk logging
// libraries and the Docker splunk driver batch them: a string event with
// the full envelope, and a JSON event with a string time.
const hecBatch = `{"time":1792152000.25,"host":"web-2","source":"/var/log/nginx/access.log","sourcetype":"nginx:access","index":"main","event":"10.0.0.1 - - [16/Oct/2026:12:00:00 +0000] \"GET / HTTP/1.1\" 200 612","fields":{"env":"prod","region":["eu","us"]}}{"time":"1792152001","event":{"message":"user login","level":"WARNING","user":{"id":42}},"sourcetype":"_json"}`

func TestHECEventsToLogs(t *testing.T) {
	e := newTestEnv(t, "logs", spool.Options{})
	for _, p := range []string{"/ingest/services/collector", "/ingest/services/collector/event", "/ingest/services/collector/event/1.0"} {
		rec := e.hec(p, hecBatch, nil)
		if rec.Code != 200 || strings.TrimSpace(rec.Body.String()) != `{"code":0,"text":"Success"}` {
			t.Fatalf("%s: %d %s", p, rec.Code, rec.Body)
		}
	}
	rows := e.spooled()
	if len(rows) != 6 {
		t.Fatalf("rows: %d", len(rows))
	}
	str, obj := decodeRow(t, rows[0]), decodeRow(t, rows[1])
	if str["Timestamp"] != "1792152000.250000000" || str["Body"] != `10.0.0.1 - - [16/Oct/2026:12:00:00 +0000] "GET / HTTP/1.1" 200 612` || str["ServiceName"] != "web-2" || attr(str, "ResourceAttributes", "host.name") != "web-2" {
		t.Fatalf("string event row: %s", rows[0])
	}
	for k, v := range map[string]string{"splunk.source": "/var/log/nginx/access.log", "splunk.sourcetype": "nginx:access", "splunk.index": "main", "env": "prod", "region": `["eu","us"]`} {
		if got := attr(str, "LogAttributes", k); got != v {
			t.Errorf("attribute %s = %v, want %s", k, got, v)
		}
	}
	if obj["Timestamp"] != "1792152001.000000000" || obj["Body"] != "user login" || obj["SeverityText"] != "WARNING" || obj["SeverityNumber"] != float64(13) || attr(obj, "LogAttributes", "user.id") != "42" || attr(obj, "LogAttributes", "splunk.sourcetype") != "_json" || obj["ServiceName"] != "" {
		t.Fatalf("object event row: %s", rows[1])
	}
	if v := e.hub.View(e.pid, e.clock); v.Received.Requests != 3 || v.Received.Records != 6 {
		t.Fatalf("stats: %+v", v)
	}

	// Newline separated works too, and gzip.
	rec := e.send(http.MethodPost, "/ingest/services/collector", gz([]byte("{\"event\":\"a\"}\n{\"event\":\"b\"}\n")), map[string]string{"Authorization": "Splunk " + e.key, "Content-Encoding": "gzip"})
	expectHEC(t, rec, 200, 0)
	rows = e.spooled()
	if len(rows) != 2 || decodeRow(t, rows[1])["Body"] != "b" || decodeRow(t, rows[0])["Timestamp"] != "1792152000.000000000" {
		t.Fatalf("gzip rows: %v", rows)
	}
}

func TestHECEventsSignal(t *testing.T) {
	e := newTestEnv(t, "events", spool.Options{})
	body := `{"time":1792152000,"host":"h","sourcetype":"st","event":{"action":"buy","host":"own"},"fields":{"env":"prod"}}{"event":"plain","index":"main"}`
	expectHEC(t, e.hec("/ingest/services/collector/event", body, nil), 200, 0)
	got := strings.Join(e.spooled(), "|")
	if got != `{"action":"buy","env":"prod","host":"own","sourcetype":"st","time":1792152000}|{"event":"plain","index":"main"}` {
		t.Fatalf("event rows: %s", got)
	}
}

func TestHECAuth(t *testing.T) {
	e := newTestEnv(t, "logs", spool.Options{})
	revoked, revokedID := e.newKey()
	if ok, err := e.db.RevokeIngestKey(e.pid, revokedID); err != nil || !ok {
		t.Fatal(err)
	}
	body := `{"event":"x"}`
	for _, h := range []map[string]string{
		{"Authorization": "Splunk " + e.key},
		{"Authorization": "splunk " + e.key},
		{"Authorization": "Bearer " + e.key},
		{"Authorization": basic("x", e.key)},
		{"X-CHUI-Ingest-Key": e.key},
	} {
		expectHEC(t, e.hec("/ingest/services/collector", body, h), 200, 0)
	}
	expectHEC(t, e.hec("/ingest/services/collector", body, map[string]string{}), 401, hecTokenRequired)
	// The token in the query string is never read.
	expectHEC(t, e.hec("/ingest/services/collector?token="+e.key, body, map[string]string{}), 401, hecTokenRequired)
	expectHEC(t, e.hec("/ingest/services/collector", body, map[string]string{"Authorization": "Token " + e.key}), 401, hecInvalidAuth)
	expectHEC(t, e.hec("/ingest/services/collector", body, map[string]string{"Authorization": "Splunk 00000000-0000-0000-0000-000000000000"}), 403, hecInvalidToken)
	rec := e.hec("/ingest/services/collector", body, map[string]string{"Authorization": "Splunk " + revoked})
	expectHEC(t, rec, 403, hecInvalidToken)
	if strings.Contains(rec.Body.String(), e.pid) {
		t.Fatal("response names the pipeline")
	}
	if v := e.hub.View(e.pid, e.clock); v.Rejected.Auth != 1 || v.Received.Requests != 5 {
		t.Fatalf("stats: %+v", v)
	}
}

func TestHECErrors(t *testing.T) {
	e := newTestEnv(t, "logs", spool.Options{})
	cases := []struct {
		name, body string
		code       int
		event      int
	}{
		{"no event field", `{"event":"ok"}{"host":"h"}`, hecEventRequired, 1},
		{"null event", `{"event":null}`, hecEventRequired, 0},
		{"blank event", `{"event":"ok"}{"event":"ok"}{"event":"  "}`, hecEventBlank, 2},
		{"broken json", `{"event":"ok"}{"event":`, hecInvalidFormat, 1},
		{"not an object", `{"event":"ok"} [1]`, hecInvalidFormat, 1},
		{"bad time", `{"event":"ok","time":"noon"}`, hecInvalidFormat, 0},
		{"fields not an object", `{"event":"ok","fields":[1]}`, hecBadIndexedFields, 0},
	}
	for _, c := range cases {
		r := expectHEC(t, e.hec("/ingest/services/collector", c.body, nil), 400, c.code)
		if r.Invalid == nil || *r.Invalid != c.event {
			t.Errorf("%s: invalid-event-number %v, want %d", c.name, r.Invalid, c.event)
		}
	}
	// Nothing of a refused request is stored.
	if rows := e.spooled(); len(rows) != 0 {
		t.Fatalf("refused requests spooled %v", rows)
	}
	for _, body := range []string{"", " \n\t "} {
		if r := expectHEC(t, e.hec("/ingest/services/collector", body, nil), 400, hecNoData); r.Invalid != nil {
			t.Fatalf("no data names an event: %+v", r)
		}
	}
	if v := e.hub.View(e.pid, e.clock); v.Rejected.Malformed != int64(len(cases)+2) {
		t.Fatalf("stats: %+v", v)
	}

	e.rv.Limits.MaxRecords = 2
	expectHEC(t, e.hec("/ingest/services/collector", `{"event":1}{"event":2}{"event":3}`, nil), 413, hecInvalidFormat)
	e.rv.Limits.MaxRecords = 0

	e.ep.RateLimitRPS = 1
	e.hec("/ingest/services/collector", `{"event":1}`, nil)
	rec := e.hec("/ingest/services/collector", `{"event":1}`, nil)
	expectHEC(t, rec, 503, hecServerBusy)
	if rec.Header().Get("Retry-After") != "1" {
		t.Fatalf("Retry-After %q", rec.Header().Get("Retry-After"))
	}

	e.hub.Unregister(e.ep)
	expectHEC(t, e.hec("/ingest/services/collector", `{"event":1}`, nil), 503, hecServerBusy)

	tr := newTestEnv(t, "traces", spool.Options{})
	rec = tr.hec("/ingest/services/collector", `{"event":1}`, nil)
	expectHEC(t, rec, 400, hecInvalidFormat)
	if !strings.Contains(rec.Body.String(), "/ingest/v1/traces") {
		t.Fatalf("traces refusal: %s", rec.Body)
	}
	expectHEC(t, tr.hec("/ingest/services/collector/raw", "line", nil), 400, hecInvalidFormat)
}

func TestHECRaw(t *testing.T) {
	e := newTestEnv(t, "logs", spool.Options{})
	body := "first line\r\n\n  \nsecond line {\"not\":\"parsed\"}\nthird"
	for _, p := range []string{"/ingest/services/collector/raw?sourcetype=app&host=box&time=1792151000&channel=abc", "/ingest/services/collector/raw/1.0"} {
		expectHEC(t, e.hec(p, body, nil), 200, 0)
	}
	rows := e.spooled()
	if len(rows) != 6 {
		t.Fatalf("rows: %v", rows)
	}
	r0, r1 := decodeRow(t, rows[0]), decodeRow(t, rows[1])
	if r0["Body"] != "first line" || r0["Timestamp"] != "1792151000.000000000" || r0["ServiceName"] != "box" || attr(r0, "LogAttributes", "splunk.sourcetype") != "app" || attr(r0, "LogAttributes", "channel") != nil {
		t.Fatalf("raw row: %s", rows[0])
	}
	if r1["Body"] != `second line {"not":"parsed"}` {
		t.Fatalf("raw row: %s", rows[1])
	}
	if r3 := decodeRow(t, rows[3]); r3["Timestamp"] != "1792152000.000000000" || r3["ServiceName"] != "" {
		t.Fatalf("raw row without metadata: %s", rows[3])
	}
	expectHEC(t, e.hec("/ingest/services/collector/raw", "\r\n\n", nil), 400, hecNoData)
	expectHEC(t, e.hec("/ingest/services/collector/raw?time=later", "x", nil), 400, hecInvalidFormat)

	ev := newTestEnv(t, "events", spool.Options{})
	expectHEC(t, ev.hec("/ingest/services/collector/raw?sourcetype=app&time=1792151000", "a\nb", nil), 200, 0)
	if got := strings.Join(ev.spooled(), "|"); got != `{"event":"a","sourcetype":"app","time":1792151000}|{"event":"b","sourcetype":"app","time":1792151000}` {
		t.Fatalf("raw event rows: %s", got)
	}
}

func TestHECHealth(t *testing.T) {
	e := newTestEnv(t, "logs", spool.Options{})
	get := func(path string, h map[string]string) *httptest.ResponseRecorder {
		return e.send(http.MethodGet, path, nil, h)
	}
	expectHEC(t, get("/ingest/services/collector/health", map[string]string{}), 200, hecHealthy)
	expectHEC(t, get("/ingest/services/collector/health/1.0", map[string]string{"Authorization": "Splunk " + e.key}), 200, hecHealthy)
	expectHEC(t, get("/ingest/services/collector/health", map[string]string{"Authorization": "Splunk chi_nope"}), 403, hecInvalidToken)
	e.hub.Unregister(e.ep)
	expectHEC(t, get("/ingest/services/collector/health", map[string]string{"Authorization": "Splunk " + e.key}), 503, hecUnhealthy)
}
