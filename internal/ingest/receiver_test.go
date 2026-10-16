package ingest

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/database/dbtest"
	"github.com/caioricciuti/ch-ui/internal/ingest/spool"
	"github.com/caioricciuti/ch-ui/internal/pipelines"
)

const logsJSON = `{"resourceLogs":[{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"svc"}}]},"scopeLogs":[{"scope":{"name":"s"},"logRecords":[{"severityText":"INFO","body":{"stringValue":"hello"}},{"timeUnixNano":"1700000000000000000","body":{"stringValue":"dated"}}]}]}]}`

type testEnv struct {
	t      *testing.T
	db     *database.DB
	sp     *spool.Spool
	hub    *pipelines.Hub
	rv     *Receiver
	router http.Handler
	pid    string
	key    string
	keyID  string
	ep     *pipelines.IngestEndpoint
	clock  time.Time
}

func newTestEnv(t *testing.T, signal string, opts spool.Options) *testEnv {
	t.Helper()
	db, err := dbtest.Open(t, "ingest.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	conn, err := db.CreateConnection(database.CreateConnectionParams{Name: "c", TunnelToken: "tok"})
	if err != nil {
		t.Fatal(err)
	}
	pid, err := db.CreatePipeline("p", "", conn, "tester")
	if err != nil {
		t.Fatal(err)
	}
	opts.SyncMode = spool.SyncNone
	if opts.SegmentMaxAge == 0 {
		opts.SegmentMaxAge = 10 * time.Millisecond
	}
	sp, err := spool.Open(t.TempDir(), opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sp.Close() })
	hub := pipelines.NewHub(sp)
	e := &testEnv{t: t, db: db, sp: sp, hub: hub, pid: pid, clock: time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)}
	e.rv = NewReceiver(db, hub)
	e.rv.now = func() time.Time { return e.clock }
	r := chi.NewRouter()
	r.Route("/ingest", e.rv.Routes)
	e.router = r
	e.key, e.keyID = e.newKey()
	e.ep = &pipelines.IngestEndpoint{PipelineID: pid, Signal: signal, RateLimitRPS: 1000}
	hub.Register(e.ep)
	return e
}

func (e *testEnv) newKey() (string, string) {
	e.t.Helper()
	plain, hash, prefix, err := GenerateKey()
	if err != nil {
		e.t.Fatal(err)
	}
	k, err := e.db.CreateIngestKey(e.pid, "k", hash, prefix, "tester")
	if err != nil {
		e.t.Fatal(err)
	}
	return plain, k.ID
}

type req struct {
	path, body, ct, encoding string
	headers                  map[string]string
	raw                      []byte
}

func (e *testEnv) do(r req) *httptest.ResponseRecorder {
	e.t.Helper()
	body := r.raw
	if body == nil {
		body = []byte(r.body)
	}
	hr := httptest.NewRequest(http.MethodPost, r.path, bytes.NewReader(body))
	if r.ct != "" {
		hr.Header.Set("Content-Type", r.ct)
	}
	if r.encoding != "" {
		hr.Header.Set("Content-Encoding", r.encoding)
	}
	if r.headers == nil {
		hr.Header.Set("Authorization", "Bearer "+e.key)
	}
	for k, v := range r.headers {
		hr.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, hr)
	return rec
}

func (e *testEnv) logs(body string) *httptest.ResponseRecorder {
	return e.do(req{path: "/ingest/v1/logs", body: body, ct: "application/json"})
}

// spooled claims every sealed segment and returns the rows, in order.
func (e *testEnv) spooled() []string {
	e.t.Helper()
	var rows []string
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		seg, err := e.sp.Claim(ctx, e.pid)
		cancel()
		if err != nil {
			return rows
		}
		for _, rec := range seg.Records() {
			for _, line := range strings.Split(strings.TrimRight(string(rec), "\n"), "\n") {
				rows = append(rows, line)
			}
		}
		if err := e.sp.Ack(seg); err != nil {
			e.t.Fatal(err)
		}
	}
}

func gz(b []byte) []byte {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	zw.Write(b)
	zw.Close()
	return buf.Bytes()
}

func TestGenerateKeyShape(t *testing.T) {
	plain, hash, prefix, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	if !wellFormed(plain) || len(plain) != 44 || prefix != plain[4:12] || hash != HashKey(plain) || len(hash) != 64 {
		t.Fatalf("key %q prefix %q hash %q", plain, prefix, hash)
	}
	other, _, _, _ := GenerateKey()
	if other == plain {
		t.Fatal("two keys are equal")
	}
	for _, bad := range []string{"", "chi_", "chm_" + plain[4:], plain + "a", strings.ToUpper(plain), plain[:43] + "1"} {
		if wellFormed(bad) {
			t.Errorf("wellFormed(%q)", bad)
		}
	}
}

func TestReceiverAuth(t *testing.T) {
	e := newTestEnv(t, "logs", spool.Options{})
	revoked, revokedID := e.newKey()
	if ok, err := e.db.RevokeIngestKey(e.pid, revokedID); err != nil || !ok {
		t.Fatal(err)
	}
	unknown, _, _, _ := GenerateKey()
	cases := []struct {
		name    string
		path    string
		headers map[string]string
		want    int
	}{
		{"bearer", "/ingest/v1/logs", nil, 200},
		{"x-chui-ingest-key", "/ingest/v1/logs", map[string]string{"X-CHUI-Ingest-Key": e.key}, 200},
		{"lowercase bearer", "/ingest/v1/logs", map[string]string{"Authorization": "bearer " + e.key}, 200},
		{"missing", "/ingest/v1/logs", map[string]string{}, 401},
		{"unknown", "/ingest/v1/logs", map[string]string{"Authorization": "Bearer " + unknown}, 401},
		{"garbage", "/ingest/v1/logs", map[string]string{"Authorization": "Bearer nope"}, 401},
		{"revoked", "/ingest/v1/logs", map[string]string{"Authorization": "Bearer " + revoked}, 401},
		{"query string is ignored", "/ingest/v1/logs?key=" + e.key + "&token=" + e.key, map[string]string{}, 401},
		{"wrong signal", "/ingest/v1/traces", nil, 404},
		{"wrong signal events", "/ingest/v1/events", nil, 404},
	}
	for _, c := range cases {
		ct := "application/json"
		rec := e.do(req{path: c.path, body: logsJSON, ct: ct, headers: c.headers})
		if rec.Code != c.want {
			t.Errorf("%s: %d %s, want %d", c.name, rec.Code, rec.Body, c.want)
		}
		if c.want == 401 && strings.Contains(rec.Body.String(), e.pid) {
			t.Errorf("%s: response names the pipeline", c.name)
		}
	}
	// Unknown keys cannot be attributed; the revoked one can.
	if v := e.hub.View(e.pid, time.Now()); v.Rejected.Auth != 1 || v.Received.Requests != 3 || v.Received.Records != 6 {
		t.Fatalf("stats: %+v", v)
	}
	key, _ := e.db.GetIngestKeyByHash(HashKey(e.key))
	if key.LastUsedAt == nil {
		t.Fatal("last_used_at not recorded")
	}

	// An OTLP JSON error body is a google.rpc.Status.
	rec := e.do(req{path: "/ingest/v1/logs", body: logsJSON, ct: "application/json", headers: map[string]string{}})
	var st struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil || st.Code != 16 {
		t.Fatalf("401 body %q: %v", rec.Body, err)
	}
}

func TestReceiverStoppedPipelineIs503(t *testing.T) {
	e := newTestEnv(t, "logs", spool.Options{})
	e.hub.Unregister(e.ep)
	rec := e.logs(logsJSON)
	if rec.Code != http.StatusServiceUnavailable || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("stopped: %d %q", rec.Code, rec.Header().Get("Retry-After"))
	}
}

// 200 comes after the spool append: the rows are in the spool, a record
// without timestamps got the receive time, and the response is OTLP.
func TestReceiverLogsSpooledBeforeOK(t *testing.T) {
	e := newTestEnv(t, "logs", spool.Options{})
	rec := e.logs(logsJSON)
	if rec.Code != 200 || rec.Body.String() != "{}" || rec.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("json: %d %q %q", rec.Code, rec.Body, rec.Header().Get("Content-Type"))
	}
	if st := e.sp.Stats(e.pid); st.Records != 1 {
		t.Fatalf("spool records after 200 = %d", st.Records)
	}
	rows := e.spooled()
	if len(rows) != 2 {
		t.Fatalf("rows: %v", rows)
	}
	var first, second map[string]interface{}
	json.Unmarshal([]byte(rows[0]), &first)
	json.Unmarshal([]byte(rows[1]), &second)
	if first["Timestamp"] != "1792152000.000000000" || first["ServiceName"] != "svc" || first["Body"] != "hello" {
		t.Fatalf("undated row: %s", rows[0])
	}
	if second["Timestamp"] != "1700000000.000000000" {
		t.Fatalf("dated row: %s", rows[1])
	}

	// Protobuf in, protobuf out (an empty message).
	rec = e.do(req{path: "/ingest/v1/logs", raw: otlpLogsProto("svc", 1, nil, "a", "b", "c"), ct: "application/x-protobuf"})
	if rec.Code != 200 || rec.Body.Len() != 0 || rec.Header().Get("Content-Type") != "application/x-protobuf" {
		t.Fatalf("protobuf: %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if rows := e.spooled(); len(rows) != 3 {
		t.Fatalf("protobuf rows: %d", len(rows))
	}
}

func TestReceiverTracesAndEvents(t *testing.T) {
	e := newTestEnv(t, "traces", spool.Options{})
	rec := e.do(req{path: "/ingest/v1/traces", raw: otlpTraceProto("api", bytes.Repeat([]byte{7}, 16), 1_700_000_000_000_000_000), ct: "application/x-protobuf"})
	if rec.Code != 200 {
		t.Fatalf("traces: %d %s", rec.Code, rec.Body)
	}
	rows := e.spooled()
	if len(rows) != 2 || !strings.Contains(rows[1], `"StatusCode":"Error"`) || !strings.Contains(rows[0], `"TraceId":"07070707070707070707070707070707"`) {
		t.Fatalf("spans: %v", rows)
	}

	ev := newTestEnv(t, "events", spool.Options{})
	for _, c := range []struct{ body, ct string }{
		{`{"a":1}`, "application/json"},
		{`[{"a":2},{"a":3}]`, "text/plain; charset=utf-8"},
		{"{\"a\":4}\n\n{\"a\": 5}\n", "application/x-ndjson"},
	} {
		rec := ev.do(req{path: "/ingest/v1/events", body: c.body, ct: c.ct})
		if rec.Code != 200 || !strings.HasPrefix(rec.Body.String(), `{"accepted":`) {
			t.Fatalf("events %q: %d %s", c.body, rec.Code, rec.Body)
		}
	}
	got := strings.Join(ev.spooled(), "|")
	if got != `{"a":1}|{"a":2}|{"a":3}|{"a":4}|{"a":5}` {
		t.Fatalf("event rows: %s", got)
	}
	for _, ct := range []string{"", "application/xml", "application/jsonl"} {
		if rec := ev.do(req{path: "/ingest/v1/events", body: `{"a":1}`, ct: ct}); rec.Code != http.StatusUnsupportedMediaType {
			t.Fatalf("events with %q: %d, want 415", ct, rec.Code)
		}
	}
	rec = ev.do(req{path: "/ingest/v1/events", body: `[1,2]`, ct: "application/json"})
	if rec.Code != 400 || !strings.Contains(rec.Body.String(), `"error"`) {
		t.Fatalf("bad events: %d %s", rec.Code, rec.Body)
	}
}

func TestReceiverRateLimitAndQuota(t *testing.T) {
	e := newTestEnv(t, "logs", spool.Options{})
	e.ep.RateLimitRPS = 2
	for i := 0; i < 2; i++ {
		if rec := e.logs(logsJSON); rec.Code != 200 {
			t.Fatalf("request %d: %d", i, rec.Code)
		}
	}
	rec := e.logs(logsJSON)
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") != "1" {
		t.Fatalf("over the rate: %d %q", rec.Code, rec.Header().Get("Retry-After"))
	}
	// Another key of the same pipeline has its own bucket.
	other, _ := e.newKey()
	if rec := e.do(req{path: "/ingest/v1/logs", body: logsJSON, ct: "application/json", headers: map[string]string{"Authorization": "Bearer " + other}}); rec.Code != 200 {
		t.Fatalf("second key: %d", rec.Code)
	}
	e.clock = e.clock.Add(time.Second)
	if rec := e.logs(logsJSON); rec.Code != 200 {
		t.Fatalf("after refill: %d", rec.Code)
	}

	// Quota: room for two bodies, the third is refused, the next UTC day
	// starts again. Refused requests are counted before decoding: an
	// invalid body is refused for quota, not as malformed.
	e.ep.RateLimitRPS = 1000
	e.clock = e.clock.Add(time.Second)
	e.ep.DailyQuotaBytes = int64(2*len(logsJSON) + 10)
	for i := 0; i < 2; i++ {
		if rec := e.logs(logsJSON); rec.Code != 200 {
			t.Fatalf("quota request %d: %d", i, rec.Code)
		}
	}
	rec = e.logs(logsJSON)
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") != "43198" {
		t.Fatalf("over quota: %d %q", rec.Code, rec.Header().Get("Retry-After"))
	}
	e.ep.DailyQuotaBytes = int64(2 * len(logsJSON))
	if rec := e.logs("not json at all"); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("exhausted quota with a bad body: %d", rec.Code)
	}
	e.clock = e.clock.Add(12 * time.Hour)
	if rec := e.logs(logsJSON); rec.Code != 200 {
		t.Fatalf("next day: %d", rec.Code)
	}
	v := e.hub.View(e.pid, e.clock)
	if v.Rejected.RateLimit != 1 || v.Rejected.Quota != 2 || v.Rejected.Malformed != 0 {
		t.Fatalf("rejections: %+v", v.Rejected)
	}
}

func TestReceiverBodyLimitsAndEncodings(t *testing.T) {
	e := newTestEnv(t, "logs", spool.Options{})
	e.rv.MaxBody = 64 << 10

	if rec := e.do(req{path: "/ingest/v1/logs", raw: gz([]byte(logsJSON)), ct: "application/json", encoding: "gzip"}); rec.Code != 200 {
		t.Fatalf("gzip: %d %s", rec.Code, rec.Body)
	}
	// A bomb: 64 KiB of gzip that inflates to 16 MiB stops at the cap.
	bomb := gz(bytes.Repeat([]byte{'a'}, 16<<20))
	if len(bomb) > 64<<10 {
		t.Fatalf("bomb is %d bytes compressed", len(bomb))
	}
	if rec := e.do(req{path: "/ingest/v1/logs", raw: bomb, ct: "application/json", encoding: "gzip"}); rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("gzip bomb: %d", rec.Code)
	}
	big := `{"resourceLogs":[],"pad":"` + strings.Repeat("x", 70<<10) + `"}`
	if rec := e.do(req{path: "/ingest/v1/logs", body: big, ct: "application/json"}); rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("raw too large: %d", rec.Code)
	}
	if rec := e.do(req{path: "/ingest/v1/logs", raw: []byte("not gzip"), ct: "application/json", encoding: "gzip"}); rec.Code != http.StatusBadRequest {
		t.Fatalf("broken gzip: %d", rec.Code)
	}
	if rec := e.do(req{path: "/ingest/v1/logs", body: logsJSON, ct: "application/json", encoding: "br"}); rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("brotli: %d", rec.Code)
	}
	if rec := e.do(req{path: "/ingest/v1/logs", body: logsJSON, ct: "text/xml"}); rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("xml: %d", rec.Code)
	}
	if rec := e.do(req{path: "/ingest/v1/logs", body: `{"resourceLogs":7}`, ct: "application/json"}); rec.Code != http.StatusBadRequest {
		t.Fatalf("malformed: %d", rec.Code)
	}
	e.rv.Limits.MaxRecords = 1
	if rec := e.logs(logsJSON); rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("too many records: %d", rec.Code)
	}
	v := e.hub.View(e.pid, time.Now())
	if v.Rejected.TooLarge != 3 || v.Rejected.Malformed != 4 || v.Received.Requests != 1 {
		t.Fatalf("stats: %+v", v)
	}
}

func TestReceiverSpoolFull(t *testing.T) {
	block, _, err := (&Receiver{}).encode("logs", []byte(logsJSON), "application/json", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	// Room for one request: segment header, frame header, the block.
	e := newTestEnv(t, "logs", spool.Options{MaxBytes: int64(16 + 8 + len(block) + 10)})
	if rec := e.logs(logsJSON); rec.Code != 200 {
		t.Fatalf("first: %d", rec.Code)
	}
	rec := e.logs(logsJSON)
	if rec.Code != http.StatusServiceUnavailable || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("full: %d %s", rec.Code, rec.Body)
	}
	if v := e.hub.View(e.pid, time.Now()); v.Rejected.SpoolFull != 1 || v.Received.Requests != 1 || v.Spool.Segments != 1 {
		t.Fatalf("stats: %+v", v)
	}
}
