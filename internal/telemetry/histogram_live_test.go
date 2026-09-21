package telemetry

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// Runs only against an explicitly supplied disposable ClickHouse endpoint.
func TestHistogramWindowLive(t *testing.T) {
	endpoint := os.Getenv("CHUI_TEST_CLICKHOUSE_URL")
	if endpoint == "" {
		t.Skip("set CHUI_TEST_CLICKHOUSE_URL to a disposable ClickHouse")
	}
	client := &http.Client{Timeout: 15 * time.Second}
	exec := func(query string) ([]map[string]interface{}, error) {
		req, err := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(query))
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
		body, err := io.ReadAll(res.Body)
		if err != nil {
			return nil, err
		}
		if res.StatusCode != 200 {
			return nil, fmt.Errorf("HTTP %d: %s", res.StatusCode, body)
		}
		if len(body) == 0 {
			return nil, nil
		}
		var payload struct {
			Data []map[string]interface{} `json:"data"`
		}
		err = json.Unmarshal(body, &payload)
		return payload.Data, err
	}
	table := fmt.Sprintf("chui_histogram_test_%d", time.Now().UnixNano())
	_, err := exec(fmt.Sprintf(`CREATE TABLE default.%s (Timestamp DateTime64(9, 'Asia/Kathmandu'), SeverityText String, Body String, ServiceName String, TraceId String, SpanName String, Duration UInt64) ENGINE=Memory`, table))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := exec("DROP TABLE IF EXISTS default." + table); err != nil {
			t.Error(err)
		}
	})
	_, err = exec(fmt.Sprintf(`INSERT INTO default.%s VALUES
	(parseDateTime64BestEffort('2026-09-21T07:02:39.999Z',9),'INFO','before end','api','before','request',1000000),
	(parseDateTime64BestEffort('2026-09-21T07:02:40Z',9),'WARN','at end','api','at','request',2000000)`, table))
	if err != nil {
		t.Fatal(err)
	}
	logs := &Source{Kind: KindLogs, Database: "default", Table: table, Logs: &LogsMapping{Timestamp: "Timestamp", Severity: "SeverityText", Body: "Body", Service: "ServiceName"}}
	traces := &Source{Kind: KindTraces, Database: "default", Table: table, Traces: &TracesMapping{Timestamp: "Timestamp", TraceID: "TraceId", SpanName: "SpanName", Service: "ServiceName", Duration: "Duration", DurationUnit: "ns"}}
	from, to := "2026-09-21T07:02:30Z", "2026-09-21T07:02:40Z"
	search, err := SearchSQL(logs, nil, LogsParams{From: from, To: to}, "asc", 10, nil)
	if err != nil {
		t.Fatal(err)
	}
	hist, err := HistogramSQL(logs, nil, LogsParams{From: from, To: to}, 10)
	if err != nil {
		t.Fatal(err)
	}
	traceSearch, err := TraceSearchSQL(traces, nil, TracesParams{From: from, To: to}, 10, nil)
	if err != nil {
		t.Fatal(err)
	}
	traceHist, err := TraceHistogramSQL(traces, nil, TracesParams{From: from, To: to}, 10)
	if err != nil {
		t.Fatal(err)
	}
	for name, query := range map[string]string{"logs search": search, "logs histogram": hist, "traces search": traceSearch, "traces histogram": traceHist} {
		rows, err := exec(query + " FORMAT JSON")
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(rows) != 1 {
			t.Fatalf("%s returned %d rows: %+v", name, len(rows), rows)
		}
		if strings.Contains(name, "histogram") && fmt.Sprint(rows[0]["c"]) != "1" {
			t.Fatalf("%s included exclusive end: %+v", name, rows)
		}
		if name == "logs search" && rows[0]["severity"] != "INFO" {
			t.Fatalf("logs search included exclusive end: %+v", rows)
		}
		if name == "traces search" && rows[0]["trace_id"] != "before" {
			t.Fatalf("traces search included exclusive end: %+v", rows)
		}
	}
	// Daily buckets must use UTC even when the timestamp column carries another timezone.
	hist, err = HistogramSQL(logs, nil, LogsParams{From: "2026-09-21T00:00:00Z", To: "2026-09-22T00:00:00Z"}, 86400)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := exec(hist + " FORMAT JSON")
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row["t"] != "2026-09-21T00:00:00Z" {
			t.Fatalf("daily bucket shifted by column timezone: %+v", row)
		}
	}
}
