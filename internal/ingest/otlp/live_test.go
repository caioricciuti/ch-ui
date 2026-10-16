package otlp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

// liveClickHouse returns a function that runs one statement, with an
// optional body, against the disposable ClickHouse named by
// CHUI_TEST_CLICKHOUSE_URL, or skips.
func liveClickHouse(t *testing.T) func(query string, body []byte) ([]map[string]any, error) {
	t.Helper()
	endpoint := os.Getenv("CHUI_TEST_CLICKHOUSE_URL")
	if endpoint == "" {
		t.Skip("set CHUI_TEST_CLICKHOUSE_URL to a disposable ClickHouse")
	}
	client := &http.Client{Timeout: 30 * time.Second}
	return func(query string, body []byte) ([]map[string]any, error) {
		u, err := url.Parse(endpoint)
		if err != nil {
			return nil, err
		}
		q := u.Query()
		// A non-UTC session zone proves the timestamp encoding is absolute.
		q.Set("session_timezone", "America/New_York")
		var reader io.Reader = strings.NewReader(query)
		if body != nil {
			q.Set("query", query)
			reader = bytes.NewReader(body)
		}
		u.RawQuery = q.Encode()
		req, err := http.NewRequest(http.MethodPost, u.String(), reader)
		if err != nil {
			return nil, err
		}
		user := os.Getenv("CHUI_TEST_CLICKHOUSE_USER")
		if user == "" {
			user = "default"
		}
		req.SetBasicAuth(user, os.Getenv("CHUI_TEST_CLICKHOUSE_PASSWORD"))
		res, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer res.Body.Close()
		out, err := io.ReadAll(res.Body)
		if err != nil {
			return nil, err
		}
		if res.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("HTTP %d: %s", res.StatusCode, out)
		}
		if len(out) == 0 {
			return nil, nil
		}
		var payload struct {
			Data []map[string]any `json:"data"`
		}
		err = json.Unmarshal(out, &payload)
		return payload.Data, err
	}
}

func TestLiveRoundTrip(t *testing.T) {
	exec := liveClickHouse(t)
	db := fmt.Sprintf("chui_otlp_%d", time.Now().UnixNano())
	if _, err := exec("CREATE DATABASE "+db, nil); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := exec("DROP DATABASE IF EXISTS "+db, nil); err != nil {
			t.Errorf("drop %s: %v", db, err)
		}
	})
	// The fixtures are from 2023, so the round-trip tables have no TTL: a
	// TTL would drop their parts in the background. The _ttl pair only
	// proves the TTL variants are accepted.
	stmts := append([]string{LogsTableDDL(db, "otel_logs", 0, ""), LogsTableDDL(db, "otel_logs_ttl", 3, "")},
		TracesTableDDL(db, "otel_traces", 0, "")...)
	stmts = append(stmts, TracesTableDDL(db, "otel_traces_ttl", 3, "")...)
	for _, s := range stmts {
		if _, err := exec(s, nil); err != nil {
			t.Fatalf("%v\n%s", err, s)
		}
	}

	// Logs: the protobuf rows plus one with characters that need escaping.
	logs, err := DecodeLogsProto(testLogsProto(), Limits{})
	if err != nil {
		t.Fatal(err)
	}
	logs[1].Body = "tab\there \"q\" \\ \x01 bad\xff é"
	var buf []byte
	for i := range logs {
		buf = AppendLogJSON(buf, &logs[i])
	}
	if _, err := exec("INSERT INTO "+db+".otel_logs FORMAT JSONEachRow", buf); err != nil {
		t.Fatal(err)
	}
	got, err := exec("SELECT toString(toUnixTimestamp64Nano(Timestamp)) AS ns, TraceId, SpanId, TraceFlags, SeverityNumber, ServiceName, Body, "+
		"LogAttributes['kv'] AS kv, ResourceAttributes['host'] AS host, ScopeAttributes['sa'] AS sa, ScopeName, EventName, "+
		"toString(TimestampTime) AS tt FROM "+db+".otel_logs ORDER BY Timestamp FORMAT JSON", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d log rows: %v", len(got), got)
	}
	r := got[0]
	for k, want := range map[string]any{
		"ns": "1700000000123456789", "TraceId": testTraceHex, "SpanId": testSpanHex, "TraceFlags": 1.0, "SeverityNumber": 17.0,
		"ServiceName": "checkout", "Body": "boom", "kv": `{"a":"q\"","b":2}`, "host": "h1", "sa": "-3", "ScopeName": "lib", "EventName": "ev",
	} {
		if r[k] != want {
			t.Errorf("log %s = %#v, want %#v", k, r[k], want)
		}
	}
	if got[1]["Body"] != "tab\there \"q\" \\ \x01 bad� é" || got[1]["ns"] != "1700000001000000000" {
		t.Errorf("escaped row read back as %#v", got[1])
	}

	// Traces, decoded from JSON this time.
	spans, err := DecodeTracesJSON([]byte(testTracesJSON), Limits{})
	if err != nil {
		t.Fatal(err)
	}
	buf = buf[:0]
	for i := range spans {
		buf = AppendSpanJSON(buf, &spans[i])
	}
	if _, err := exec("INSERT INTO "+db+".otel_traces FORMAT JSONEachRow", buf); err != nil {
		t.Fatal(err)
	}
	got, err = exec("SELECT toString(toUnixTimestamp64Nano(Timestamp)) AS ns, SpanKind, StatusCode, StatusMessage, toString(Duration) AS dur, "+
		"Events.Name AS en, arrayMap(x -> toString(toUnixTimestamp64Nano(x)), Events.Timestamp) AS et, Events.Attributes[1]['exception.type'] AS ea, "+
		"Links.SpanId AS ls, Links.Attributes[1]['l'] AS la, SpanAttributes['http.status_code'] AS sc "+
		"FROM "+db+".otel_traces WHERE TraceId = '"+testTraceHex+"' FORMAT JSON", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d spans: %v", len(got), got)
	}
	s := got[0]
	for k, want := range map[string]any{
		"ns": "1700000000000000000", "SpanKind": "Server", "StatusCode": "Error", "StatusMessage": "boom", "dur": "1250000000",
		"ea": "IOError", "la": "1", "sc": "500",
	} {
		if s[k] != want {
			t.Errorf("span %s = %#v, want %#v", k, s[k], want)
		}
	}
	if fmt.Sprint(s["en"]) != "[exception]" || fmt.Sprint(s["et"]) != "[1700000000500000000]" || fmt.Sprint(s["ls"]) != "["+testSpanHex+"]" {
		t.Errorf("nested columns read back as %v %v %v", s["en"], s["et"], s["ls"])
	}

	// The materialized view filled the lookup table Telemetry reads; the
	// span without a trace id is not in it.
	got, err = exec("SELECT TraceId, toUnixTimestamp(Start) AS s FROM "+db+".otel_traces_trace_id_ts FORMAT JSON", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0]["TraceId"] != testTraceHex || got[0]["s"] != 1700000000.0 {
		t.Errorf("trace id lookup = %v", got)
	}
}
