package ingest

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/caioricciuti/ch-ui/internal/ingest/spool"
)

// send issues any method; nil headers send Basic auth with the env's key,
// the way Elasticsearch clients do.
func (e *testEnv) send(method, path string, body []byte, headers map[string]string) *httptest.ResponseRecorder {
	e.t.Helper()
	hr := httptest.NewRequest(method, path, bytes.NewReader(body))
	if headers == nil {
		hr.SetBasicAuth("elastic", e.key)
		hr.Header.Set("Content-Type", "application/x-ndjson")
	}
	for k, v := range headers {
		hr.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, hr)
	return rec
}

func (e *testEnv) bulk(path, body string) *httptest.ResponseRecorder {
	return e.send(http.MethodPost, path, []byte(body), nil)
}

func basic(user, pass string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+pass))
}

type bulkResponse struct {
	Took   int64                       `json:"took"`
	Errors bool                        `json:"errors"`
	Items  []map[string]map[string]any `json:"items"`
}

func decodeBulk(t *testing.T, rec *httptest.ResponseRecorder) bulkResponse {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("bulk: %d %s", rec.Code, rec.Body)
	}
	var br bulkResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &br); err != nil {
		t.Fatalf("bulk body %s: %v", rec.Body, err)
	}
	return br
}

// item returns the only action of a bulk response item.
func item(t *testing.T, it map[string]map[string]any) (string, map[string]any) {
	t.Helper()
	if len(it) != 1 {
		t.Fatalf("item %v", it)
	}
	for a, f := range it {
		return a, f
	}
	return "", nil
}

func decodeRow(t *testing.T, line string) map[string]any {
	t.Helper()
	var row map[string]any
	if err := json.Unmarshal([]byte(line), &row); err != nil {
		t.Fatalf("row %s: %v", line, err)
	}
	return row
}

func attr(row map[string]any, col, key string) any {
	m, _ := row[col].(map[string]any)
	return m[key]
}

// What Filebeat 8 sends for a filestream input into its default data
// stream: create actions, ECS fields, no _id.
const filebeatBulk = `{"create":{"_index":"filebeat-8.15.0"}}
{"@timestamp":"2026-10-16T11:59:58.123Z","log":{"offset":1024,"file":{"path":"/var/log/app/app.log","inode":"1234"}},"message":"GET /api/users 200 12ms","input":{"type":"filestream"},"ecs":{"version":"8.0.0"},"host":{"name":"web-1","hostname":"web-1","architecture":"x86_64","os":{"family":"debian","kernel":"6.8.0"},"ip":["10.0.0.5"]},"agent":{"type":"filebeat","version":"8.15.0","id":"a1","name":"web-1"},"cloud":{"provider":"aws","region":"eu-west-1"},"fields":{"team":"payments"}}
{"create":{"_index":"filebeat-8.15.0"}}
{"@timestamp":"2026-10-16T11:59:59.000Z","log":{"level":"error","offset":2048,"file":{"path":"/var/log/app/app.log"}},"message":"payment declined","service":{"name":"payments-api"},"host":{"name":"web-1"},"error":{"type":"CardDeclined"}}
`

// What Fluent Bit's es output sends with Logstash_Format On and the
// kubernetes filter, without Suppress_Type_Name (so _type is there).
const fluentBitBulk = `{"index":{"_index":"logstash-2026.10.16","_type":"_doc"}}
{"@timestamp":"2026-10-16T12:00:01.500Z","log":"2026-10-16 12:00:01 WARN slow query\n","stream":"stderr","time":"2026-10-16T12:00:01.499999999Z","kubernetes":{"pod_name":"api-7d9","namespace_name":"prod","container_name":"api","labels":{"app":"api"},"host":"node-3"}}
`

// What Vector's elasticsearch sink sends in bulk mode.
const vectorBulk = `{"index":{"_index":"vector-2026.10.16"}}
{"host":"vec-1","message":"hello from vector","source_type":"demo_logs","timestamp":"2026-10-16T12:00:02.000000001Z","level":"info"}
`

func TestESHandshake(t *testing.T) {
	e := newTestEnv(t, "logs", spool.Options{})
	// The handshake needs a valid key but not a running pipeline.
	e.hub.Unregister(e.ep)
	for _, p := range []string{"/ingest/es", "/ingest/es/"} {
		rec := e.send(http.MethodGet, p, nil, nil)
		var info struct {
			Version struct {
				Number      string `json:"number"`
				BuildFlavor string `json:"build_flavor"`
			} `json:"version"`
			Tagline string `json:"tagline"`
		}
		if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &info) != nil || !strings.HasPrefix(info.Version.Number, "8.") || info.Version.BuildFlavor != "default" || info.Tagline != "You Know, for Search" {
			t.Fatalf("GET %s: %d %s", p, rec.Code, rec.Body)
		}
		if rec.Header().Get("X-Elastic-Product") != "Elasticsearch" {
			t.Fatalf("GET %s: no product header", p)
		}
	}
	if rec := e.send(http.MethodHead, "/ingest/es/", nil, nil); rec.Code != 200 || rec.Body.Len() != 0 {
		t.Fatalf("HEAD /: %d %q", rec.Code, rec.Body)
	}

	get := func(path string) map[string]any {
		t.Helper()
		rec := e.send(http.MethodGet, path, nil, nil)
		var body map[string]any
		if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &body) != nil {
			t.Fatalf("GET %s: %d %s", path, rec.Code, rec.Body)
		}
		return body
	}
	if lic := get("/ingest/es/_license?human=false")["license"].(map[string]any); lic["status"] != "active" || lic["type"] != "basic" {
		t.Fatalf("license: %v", lic)
	}
	if ilm := get("/ingest/es/_xpack")["features"].(map[string]any)["ilm"].(map[string]any); ilm["available"] != false {
		t.Fatalf("xpack ilm: %v", ilm)
	}
	if h := get("/ingest/es/_cluster/health"); h["status"] != "green" {
		t.Fatalf("health: %v", h)
	}
	get("/ingest/es/_cluster/health/logs")
	if n := get("/ingest/es/_nodes/http"); len(n["nodes"].(map[string]any)) != 0 {
		t.Fatalf("nodes: %v", n)
	}
	if tpl := get("/ingest/es/_index_template/filebeat-8.15.0")["index_templates"].([]any); len(tpl) != 1 {
		t.Fatalf("index template: %v", tpl)
	}
	if _, ok := get("/ingest/es/_template/logstash")["logstash"]; !ok {
		t.Fatal("legacy template not echoed")
	}
	if _, ok := get("/ingest/es/_ilm/policy/filebeat")["filebeat"]; !ok {
		t.Fatal("ilm policy not echoed")
	}
	if ds := get("/ingest/es/_data_stream/logs-generic-default")["data_streams"].([]any); ds[0].(map[string]any)["name"] != "logs-generic-default" {
		t.Fatalf("data stream: %v", ds)
	}
	for _, p := range []string{"/ingest/es/_index_template/x", "/ingest/es/_template/x", "/ingest/es/_ilm/policy/x", "/ingest/es/_data_stream/x"} {
		if rec := e.send(http.MethodHead, p, nil, nil); rec.Code != 200 {
			t.Fatalf("HEAD %s: %d", p, rec.Code)
		}
		if rec := e.send(http.MethodPut, p, []byte(`{"index_patterns":["x-*"]}`), nil); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"acknowledged":true`) {
			t.Fatalf("PUT %s: %d %s", p, rec.Code, rec.Body)
		}
	}

	// Unknown endpoints and methods answer Elasticsearch errors.
	rec := e.send(http.MethodGet, "/ingest/es/_cat/indices", nil, nil)
	if rec.Code != 404 || !strings.Contains(rec.Body.String(), `"type":"resource_not_found_exception"`) {
		t.Fatalf("unknown: %d %s", rec.Code, rec.Body)
	}
	if rec := e.send(http.MethodDelete, "/ingest/es/_bulk", nil, nil); rec.Code != 405 {
		t.Fatalf("DELETE _bulk: %d", rec.Code)
	}

	// No credentials: 401 with the challenge headers, nothing about pipelines.
	rec = e.send(http.MethodGet, "/ingest/es/", nil, map[string]string{})
	if rec.Code != 401 || !strings.Contains(rec.Body.String(), `"security_exception"`) || len(rec.Header().Values("WWW-Authenticate")) != 2 || strings.Contains(rec.Body.String(), e.pid) {
		t.Fatalf("no auth: %d %s %v", rec.Code, rec.Body, rec.Header())
	}
}

func TestESAuthSchemes(t *testing.T) {
	e := newTestEnv(t, "logs", spool.Options{})
	revoked, revokedID := e.newKey()
	if ok, err := e.db.RevokeIngestKey(e.pid, revokedID); err != nil || !ok {
		t.Fatal(err)
	}
	apiKey := base64.StdEncoding.EncodeToString([]byte("my-id:" + e.key))
	cases := []struct {
		name string
		auth string
		want int
	}{
		{"basic any user", basic("anyone", e.key), 200},
		{"basic empty user", basic("", e.key), 200},
		{"apikey encoded", "ApiKey " + apiKey, 200},
		{"apikey raw", "ApiKey " + e.key, 200},
		{"bearer", "Bearer " + e.key, 200},
		{"basic key as user", basic(e.key, "x"), 401},
		{"basic no colon", "Basic " + base64.StdEncoding.EncodeToString([]byte(e.key)), 401},
		{"basic not base64", "Basic !!!", 401},
		{"apikey wrong", "ApiKey " + base64.StdEncoding.EncodeToString([]byte("id:chi_nope")), 401},
		{"revoked", basic("elastic", revoked), 401},
		{"missing", "", 401},
	}
	for _, c := range cases {
		h := map[string]string{"Content-Type": "application/x-ndjson"}
		if c.auth != "" {
			h["Authorization"] = c.auth
		}
		if rec := e.send(http.MethodPost, "/ingest/es/_bulk", []byte(vectorBulk), h); rec.Code != c.want {
			t.Errorf("%s: %d %s, want %d", c.name, rec.Code, rec.Body, c.want)
		}
	}
	// Never from the query string.
	if rec := e.send(http.MethodPost, "/ingest/es/_bulk?api_key="+e.key+"&token="+e.key, []byte(vectorBulk), map[string]string{"Content-Type": "application/json"}); rec.Code != 401 {
		t.Fatalf("query string key: %d", rec.Code)
	}
	if v := e.hub.View(e.pid, e.clock); v.Rejected.Auth != 1 || v.Received.Requests != 5 {
		t.Fatalf("stats: %+v", v)
	}
}

func TestESBulkFilebeat(t *testing.T) {
	e := newTestEnv(t, "logs", spool.Options{})
	rec := e.bulk("/ingest/es/_bulk?filter_path=", filebeatBulk)
	br := decodeBulk(t, rec)
	if br.Errors || len(br.Items) != 2 {
		t.Fatalf("response: %s", rec.Body)
	}
	for i, it := range br.Items {
		action, f := item(t, it)
		if action != "create" || f["status"] != float64(201) || f["result"] != "created" || f["_index"] != "filebeat-8.15.0" || len(f["_id"].(string)) != 20 || f["_seq_no"] != float64(i) {
			t.Fatalf("item %d: %v", i, it)
		}
	}
	if rec.Header().Get("X-Elastic-Product") != "Elasticsearch" || rec.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("headers: %v", rec.Header())
	}
	rows := e.spooled()
	if len(rows) != 2 {
		t.Fatalf("rows: %v", rows)
	}
	first, second := decodeRow(t, rows[0]), decodeRow(t, rows[1])
	if first["Timestamp"] != "1792151998.123000000" || first["Body"] != "GET /api/users 200 12ms" || first["ServiceName"] != "web-1" || first["SeverityText"] != "" {
		t.Fatalf("first row: %s", rows[0])
	}
	for k, v := range map[string]string{"host.name": "web-1", "host.os.family": "debian", "host.ip": `["10.0.0.5"]`, "agent.type": "filebeat", "cloud.region": "eu-west-1"} {
		if got := attr(first, "ResourceAttributes", k); got != v {
			t.Errorf("resource %s = %v, want %s", k, got, v)
		}
	}
	for k, v := range map[string]string{"log.offset": "1024", "log.file.path": "/var/log/app/app.log", "input.type": "filestream", "ecs.version": "8.0.0", "fields.team": "payments", "es.index": "filebeat-8.15.0"} {
		if got := attr(first, "LogAttributes", k); got != v {
			t.Errorf("attribute %s = %v, want %s", k, got, v)
		}
	}
	if attr(first, "LogAttributes", "message") != nil || attr(first, "LogAttributes", "@timestamp") != nil {
		t.Fatalf("mapped fields left in attributes: %s", rows[0])
	}
	if second["SeverityText"] != "error" || second["SeverityNumber"] != float64(17) || second["ServiceName"] != "payments-api" || attr(second, "LogAttributes", "log.level") != nil || attr(second, "LogAttributes", "error.type") != "CardDeclined" || attr(second, "ResourceAttributes", "service.name") != "payments-api" {
		t.Fatalf("second row: %s", rows[1])
	}
	if v := e.hub.View(e.pid, e.clock); v.Received.Requests != 1 || v.Received.Records != 2 || v.Rejected.Malformed != 0 {
		t.Fatalf("stats: %+v", v)
	}
}

func TestESBulkFluentBitAndVector(t *testing.T) {
	e := newTestEnv(t, "logs", spool.Options{})
	if br := decodeBulk(t, e.bulk("/ingest/es/_bulk", fluentBitBulk)); br.Errors {
		t.Fatalf("fluent bit: %+v", br)
	}
	if br := decodeBulk(t, e.bulk("/ingest/es/_bulk", vectorBulk)); br.Errors {
		t.Fatalf("vector: %+v", br)
	}
	rows := e.spooled()
	if len(rows) != 2 {
		t.Fatalf("rows: %v", rows)
	}
	fb, vec := decodeRow(t, rows[0]), decodeRow(t, rows[1])
	if fb["Timestamp"] != "1792152001.500000000" || fb["Body"] != "2026-10-16 12:00:01 WARN slow query\n" || attr(fb, "ResourceAttributes", "kubernetes.pod_name") != "api-7d9" || attr(fb, "ResourceAttributes", "kubernetes.labels.app") != "api" || attr(fb, "LogAttributes", "stream") != "stderr" || attr(fb, "LogAttributes", "time") != "2026-10-16T12:00:01.499999999Z" || attr(fb, "LogAttributes", "es.index") != "logstash-2026.10.16" {
		t.Fatalf("fluent bit row: %s", rows[0])
	}
	if vec["Timestamp"] != "1792152002.000000001" || vec["Body"] != "hello from vector" || vec["ServiceName"] != "vec-1" || vec["SeverityNumber"] != float64(9) || attr(vec, "ResourceAttributes", "host.name") != "vec-1" || attr(vec, "LogAttributes", "source_type") != "demo_logs" {
		t.Fatalf("vector row: %s", rows[1])
	}
}

// update and delete are answered per item, never dropped; a broken document
// fails its item only; delete has no document line, so the pairing after it
// stays right.
func TestESBulkItemErrors(t *testing.T) {
	e := newTestEnv(t, "logs", spool.Options{})
	body := strings.Join([]string{
		`{"index":{"_index":"app","_id":"given-1"}}`,
		`{"message":"one"}`,
		`{"update":{"_index":"app","_id":"u1"}}`,
		`{"doc":{"message":"changed"}}`,
		`{"delete":{"_index":"app","_id":"d1"}}`,
		`{"create":{"_index":"app"}}`,
		`{"message": "broken`,
		`{"index":{"_index":"app"}}`,
		`[1,2,3]`,
		`{"index":{}}`,
		`{"message":"no index"}`,
		``,
		`{"index":{"_index":"app"}}`,
		`{"message":"two"}`,
		`{"index":{"_index":"app"}}`,
	}, "\r\n")
	rec := e.bulk("/ingest/es/_bulk", body)
	br := decodeBulk(t, rec)
	want := []struct {
		action string
		status float64
		typ    string
	}{
		{"index", 201, ""},
		{"update", 400, "action_request_validation_exception"},
		{"delete", 400, "action_request_validation_exception"},
		{"create", 400, "document_parsing_exception"},
		{"index", 400, "document_parsing_exception"},
		{"index", 400, "action_request_validation_exception"},
		{"index", 201, ""},
		{"index", 400, "action_request_validation_exception"},
	}
	if !br.Errors || len(br.Items) != len(want) {
		t.Fatalf("response: %s", rec.Body)
	}
	for i, w := range want {
		action, f := item(t, br.Items[i])
		errType := ""
		if eo, ok := f["error"].(map[string]any); ok {
			errType, _ = eo["type"].(string)
			if eo["reason"] == "" {
				t.Errorf("item %d: no reason", i)
			}
		}
		if action != w.action || f["status"] != w.status || errType != w.typ {
			t.Errorf("item %d: %v, want %+v", i, br.Items[i], w)
		}
	}
	if _, f := item(t, br.Items[0]); f["_id"] != "given-1" {
		t.Fatalf("given id: %v", f)
	}
	if _, f := item(t, br.Items[2]); f["_id"] != "d1" || f["_index"] != "app" {
		t.Fatalf("delete item: %v", f)
	}
	rows := e.spooled()
	if len(rows) != 2 || decodeRow(t, rows[0])["Body"] != "one" || decodeRow(t, rows[1])["Body"] != "two" {
		t.Fatalf("rows: %v", rows)
	}
	if v := e.hub.View(e.pid, e.clock); v.Received.Records != 2 || v.Rejected.Malformed != 1 {
		t.Fatalf("stats: %+v", v)
	}

	// All items refused: 200 with errors, nothing spooled.
	br = decodeBulk(t, e.bulk("/ingest/es/_bulk", "{\"delete\":{\"_index\":\"a\",\"_id\":\"1\"}}\n"))
	if !br.Errors || len(br.Items) != 1 || len(e.spooled()) != 0 {
		t.Fatalf("delete only: %+v", br)
	}
}

func TestESBulkRequestErrors(t *testing.T) {
	e := newTestEnv(t, "logs", spool.Options{})
	esErr := func(rec *httptest.ResponseRecorder, status int, typ string) {
		t.Helper()
		var body struct {
			Error struct {
				RootCause []struct {
					Type string `json:"type"`
				} `json:"root_cause"`
				Type   string `json:"type"`
				Reason string `json:"reason"`
			} `json:"error"`
			Status int `json:"status"`
		}
		if rec.Code != status || json.Unmarshal(rec.Body.Bytes(), &body) != nil || body.Status != status || body.Error.Type != typ || len(body.Error.RootCause) != 1 || body.Error.Reason == "" {
			t.Fatalf("want %d %s, got %d %s", status, typ, rec.Code, rec.Body)
		}
	}
	esErr(e.bulk("/ingest/es/_bulk", "not json\n{}\n"), 400, "illegal_argument_exception")
	esErr(e.bulk("/ingest/es/_bulk", `{"upsert":{"_index":"a"}}`+"\n{}\n"), 400, "illegal_argument_exception")
	esErr(e.bulk("/ingest/es/_bulk", `{"index":{},"create":{}}`+"\n{}\n"), 400, "illegal_argument_exception")
	esErr(e.bulk("/ingest/es/_bulk", `{"index":"a"}`+"\n{}\n"), 400, "illegal_argument_exception")
	esErr(e.bulk("/ingest/es/_bulk", "\n\n"), 400, "illegal_argument_exception")
	esErr(e.send(http.MethodPost, "/ingest/es/_bulk", []byte(vectorBulk), map[string]string{"Authorization": basic("u", e.key), "Content-Type": "application/xml"}), 415, "illegal_argument_exception")
	if v := e.hub.View(e.pid, e.clock); v.Rejected.Malformed != 6 || len(e.spooled()) != 0 {
		t.Fatalf("stats: %+v", v)
	}
	// Content types the clients send, and none.
	for _, ct := range []string{"", "application/json", "application/x-ndjson", "application/vnd.elasticsearch+x-ndjson; compatible-with=8", "text/plain"} {
		h := map[string]string{"Authorization": basic("u", e.key)}
		if ct != "" {
			h["Content-Type"] = ct
		}
		if rec := e.send(http.MethodPost, "/ingest/es/_bulk", []byte(vectorBulk), h); rec.Code != 200 {
			t.Fatalf("content type %q: %d", ct, rec.Code)
		}
	}

	e.ep.RateLimitRPS = 1
	e.bulk("/ingest/es/_bulk", vectorBulk)
	rec := e.bulk("/ingest/es/_bulk", vectorBulk)
	esErr(rec, 429, "es_rejected_execution_exception")
	if rec.Header().Get("Retry-After") != "1" {
		t.Fatalf("rate limit Retry-After %q", rec.Header().Get("Retry-After"))
	}
	e.ep.RateLimitRPS = 1000

	e.hub.Unregister(e.ep)
	rec = e.bulk("/ingest/es/_bulk", vectorBulk)
	esErr(rec, 503, "unavailable_shards_exception")
	if rec.Header().Get("Retry-After") == "" {
		t.Fatal("stopped pipeline without Retry-After")
	}

	// A traces key is refused for the whole request.
	tr := newTestEnv(t, "traces", spool.Options{})
	rec = tr.bulk("/ingest/es/_bulk", vectorBulk)
	esErr(rec, 400, "illegal_argument_exception")
	if !strings.Contains(rec.Body.String(), "/ingest/v1/traces") {
		t.Fatalf("traces refusal does not say where traces go: %s", rec.Body)
	}
}

func TestESBulkIndexFromPath(t *testing.T) {
	e := newTestEnv(t, "logs", spool.Options{})
	body := "{\"index\":{}}\n{\"message\":\"a\"}\n{\"index\":{\"_index\":\"other\"}}\n{\"message\":\"b\"}"
	br := decodeBulk(t, e.bulk("/ingest/es/my%2Dlogs/_bulk", body))
	if br.Errors {
		t.Fatalf("%+v", br)
	}
	if _, f := item(t, br.Items[0]); f["_index"] != "my-logs" {
		t.Fatalf("path index: %v", f)
	}
	rows := e.spooled()
	if attr(decodeRow(t, rows[0]), "LogAttributes", "es.index") != "my-logs" || attr(decodeRow(t, rows[1]), "LogAttributes", "es.index") != "other" {
		t.Fatalf("rows: %v", rows)
	}
	// PUT works like POST.
	if rec := e.send(http.MethodPut, "/ingest/es/x/_bulk", []byte(body), nil); rec.Code != 200 {
		t.Fatalf("PUT: %d", rec.Code)
	}
}

func TestESBulkEventsSignal(t *testing.T) {
	e := newTestEnv(t, "events", spool.Options{})
	body := "{\"index\":{\"_index\":\"orders\"}}\n{ \"order\": 1, \"total\": 12.50, \"big\": 12345678901234567890 }\n{\"create\":{\"_index\":\"orders\"}}\n{\"order\":2}\n"
	if br := decodeBulk(t, e.bulk("/ingest/es/_bulk", body)); br.Errors {
		t.Fatalf("%+v", br)
	}
	if got := strings.Join(e.spooled(), "|"); got != `{"order":1,"total":12.50,"big":12345678901234567890}|{"order":2}` {
		t.Fatalf("event rows: %s", got)
	}
}

func TestESBulkGzipAndLimits(t *testing.T) {
	e := newTestEnv(t, "logs", spool.Options{})
	e.rv.MaxBody = 64 << 10
	h := map[string]string{"Authorization": basic("u", e.key), "Content-Type": "application/x-ndjson", "Content-Encoding": "gzip"}
	if rec := e.send(http.MethodPost, "/ingest/es/_bulk", gz([]byte(filebeatBulk)), h); rec.Code != 200 {
		t.Fatalf("gzip: %d %s", rec.Code, rec.Body)
	}
	if len(e.spooled()) != 2 {
		t.Fatal("gzip rows not spooled")
	}
	bomb := gz(bytes.Repeat([]byte{'\n'}, 16<<20))
	if rec := e.send(http.MethodPost, "/ingest/es/_bulk", bomb, h); rec.Code != http.StatusRequestEntityTooLarge || !strings.Contains(rec.Body.String(), `"status":413`) {
		t.Fatalf("gzip bomb: %d %s", rec.Code, rec.Body)
	}
	e.rv.Limits.MaxRecords = 1
	if rec := e.bulk("/ingest/es/_bulk", filebeatBulk); rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("too many items: %d %s", rec.Code, rec.Body)
	}
	if v := e.hub.View(e.pid, e.clock); v.Rejected.TooLarge != 2 || len(e.spooled()) != 0 {
		t.Fatalf("stats: %+v", v)
	}
}

func TestESFilterPath(t *testing.T) {
	e := newTestEnv(t, "logs", spool.Options{})
	body := "{\"index\":{\"_index\":\"a\"}}\n{\"message\":\"x\"}\n{\"delete\":{\"_index\":\"a\",\"_id\":\"1\"}}\n"
	// Filebeat's filter.
	rec := e.bulk("/ingest/es/_bulk?filter_path=errors,items.*.error,items.*.status", body)
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(got)
	if string(b) != `{"errors":true,"items":[{"index":{"status":201}},{"delete":{"error":{"reason":"ch-ui ingest takes only index and create actions, not delete","type":"action_request_validation_exception"},"status":400}}]}` {
		t.Fatalf("filtered: %s", b)
	}
	// Errors only: success items disappear, as in Elasticsearch.
	rec = e.bulk("/ingest/es/_bulk?filter_path=took,items.index.error", body)
	got = nil
	json.Unmarshal(rec.Body.Bytes(), &got)
	if _, ok := got["took"]; !ok || got["items"] != nil || got["errors"] != nil {
		t.Fatalf("errors only: %s", rec.Body)
	}
	// Unsupported forms leave the response whole.
	rec = e.bulk("/ingest/es/_bulk?filter_path=-took", body)
	if br := decodeBulk(t, rec); len(br.Items) != 2 || !strings.Contains(rec.Body.String(), `"took"`) {
		t.Fatalf("exclusion: %s", rec.Body)
	}
}
