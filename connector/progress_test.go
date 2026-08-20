package connector

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestIsSafeQueryID(t *testing.T) {
	tests := []struct {
		id   string
		want bool
	}{
		{"3f2a1c8e-0b4d-4e7a-9c11-8d2f6a5b1e90", true},
		{"ch_ui-stream-1", true},
		{"", false},
		{"' OR 1=1 --", false},
		{"id with space", false},
		{"id'; SELECT 1", false},
		{strings.Repeat("a", 65), false},
	}
	for _, tt := range tests {
		if got := isSafeQueryID(tt.id); got != tt.want {
			t.Errorf("isSafeQueryID(%q) = %v, want %v", tt.id, got, tt.want)
		}
	}
}

func TestJSONNumberHelpers(t *testing.T) {
	// ClickHouse quotes UInt64 values; both forms must parse.
	if got := jsonUint(json.RawMessage(`"18446744073709551615"`)); got != 18446744073709551615 {
		t.Errorf("jsonUint quoted = %d", got)
	}
	if got := jsonUint(json.RawMessage(`42`)); got != 42 {
		t.Errorf("jsonUint bare = %d", got)
	}
	if got := jsonUint(json.RawMessage(`null`)); got != 0 {
		t.Errorf("jsonUint invalid = %d, want 0", got)
	}
	if got := jsonFloat(json.RawMessage(`1.5`)); got != 1.5 {
		t.Errorf("jsonFloat = %v", got)
	}
	if got := jsonFloat(json.RawMessage(`"oops"`)); got != 0 {
		t.Errorf("jsonFloat invalid = %v, want 0", got)
	}
}

func TestParseSummary(t *testing.T) {
	h := http.Header{}
	if _, ok := parseSummary(h); ok {
		t.Fatal("expected no summary for empty header")
	}

	h.Set("X-ClickHouse-Summary", `{"read_rows":"3000000000","read_bytes":"24000000000","elapsed_ns":"210233459"}`)
	s, ok := parseSummary(h)
	if !ok {
		t.Fatal("expected summary")
	}
	if s.ReadRows != 3_000_000_000 || s.ReadBytes != 24_000_000_000 || s.ElapsedNS != 210_233_459 {
		t.Errorf("unexpected summary: %+v", s)
	}

	h.Set("X-ClickHouse-Summary", "not json")
	if _, ok := parseSummary(h); ok {
		t.Fatal("expected malformed summary to be ignored")
	}
}

func TestFetchProgress(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, r.ContentLength)
		r.Body.Read(body)
		gotQuery = string(body)
		w.Write([]byte(`{"meta":[],"data":[["1024","8192","4096","512","1.25"]],"rows":1}`))
	}))
	defer srv.Close()

	c := NewCHClient(srv.URL, false)
	p, ok := c.fetchProgress(context.Background(), "abc-123", "default", "")
	if !ok {
		t.Fatal("expected a progress snapshot")
	}
	want := QueryProgress{ReadRows: 1024, ReadBytes: 8192, TotalRows: 4096, MemoryUsage: 512, Elapsed: 1.25}
	if p != want {
		t.Errorf("progress = %+v, want %+v", p, want)
	}
	if !strings.Contains(gotQuery, "system.processes") || !strings.Contains(gotQuery, "'abc-123'") {
		t.Errorf("unexpected progress query: %s", gotQuery)
	}
}

func TestFetchProgressNoRunningQuery(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"meta":[],"data":[],"rows":0}`))
	}))
	defer srv.Close()

	c := NewCHClient(srv.URL, false)
	if _, ok := c.fetchProgress(context.Background(), "gone", "default", ""); ok {
		t.Error("expected no snapshot when the query is not running")
	}
}

func TestFetchProgressDeniedIsSilent(t *testing.T) {
	// A user without access to system.processes must not break the stream.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte("Code: 497. DB::Exception: Not enough privileges."))
	}))
	defer srv.Close()

	c := NewCHClient(srv.URL, false)
	if _, ok := c.fetchProgress(context.Background(), "denied", "reader", ""); ok {
		t.Error("expected no snapshot when system.processes is denied")
	}
}

func TestExecuteStreamingReportsProgress(t *testing.T) {
	var mu sync.Mutex
	sampled := 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, r.ContentLength)
		r.Body.Read(body)
		query := string(body)

		switch {
		case strings.Contains(query, "system.processes"):
			mu.Lock()
			sampled++
			n := sampled
			mu.Unlock()
			w.Write([]byte(`{"meta":[],"data":[["` +
				strconv.Itoa(n*1000) + `","` + strconv.Itoa(n*8000) + `","10000","0","0.5"]],"rows":1}`))

		case strings.Contains(query, "LIMIT 0"):
			w.Write([]byte(`{"meta":[{"name":"number","type":"UInt64"}],"data":[],"rows":0}`))

		default:
			if r.URL.Query().Get("query_id") == "" {
				t.Error("expected the streamed query to be tagged with a query_id")
			}
			w.Header().Set("X-ClickHouse-Summary", `{"read_rows":"10000","read_bytes":"80000","elapsed_ns":"1000"}`)
			flusher := w.(http.Flusher)
			w.Write([]byte("[1]\n"))
			flusher.Flush()
			// Hold the response open long enough for a few progress samples.
			time.Sleep(900 * time.Millisecond)
			w.Write([]byte("[2]\n"))
		}
	}))
	defer srv.Close()

	c := NewCHClient(srv.URL, false)

	var progressMu sync.Mutex
	var snapshots []QueryProgress

	summary, rows, err := c.ExecuteStreaming(
		context.Background(),
		"stream-1",
		"SELECT number FROM numbers(2)",
		"default", "",
		1,
		nil,
		func(json.RawMessage) error { return nil },
		func(int, json.RawMessage) error { return nil },
		func(p QueryProgress) {
			progressMu.Lock()
			snapshots = append(snapshots, p)
			progressMu.Unlock()
		},
	)
	if err != nil {
		t.Fatalf("ExecuteStreaming: %v", err)
	}
	if rows != 2 {
		t.Errorf("rows = %d, want 2", rows)
	}

	progressMu.Lock()
	defer progressMu.Unlock()
	if len(snapshots) == 0 {
		t.Fatal("expected at least one progress snapshot")
	}
	first := snapshots[0]
	if first.ReadRows == 0 || first.TotalRows != 10000 {
		t.Errorf("unexpected first snapshot: %+v", first)
	}

	if summary == nil {
		t.Fatal("expected the ClickHouse summary header to be parsed")
	}
	if summary.ReadRows != 10000 || summary.ReadBytes != 80000 {
		t.Errorf("unexpected summary: %+v", summary)
	}
}

func TestExecuteStreamingWithoutProgressCallbackIsUntagged(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, r.ContentLength)
		r.Body.Read(body)
		if strings.Contains(string(body), "system.processes") {
			t.Error("progress must not be sampled without an onProgress callback")
		}
		if strings.Contains(string(body), "LIMIT 0") {
			w.Write([]byte(`{"meta":[{"name":"number","type":"UInt64"}],"data":[],"rows":0}`))
			return
		}
		if r.URL.Query().Get("query_id") != "" {
			t.Error("query_id must not be set when progress is not tracked")
		}
		w.Write([]byte("[1]\n"))
	}))
	defer srv.Close()

	c := NewCHClient(srv.URL, false)
	_, rows, err := c.ExecuteStreaming(
		context.Background(),
		"stream-2",
		"SELECT number FROM numbers(1)",
		"default", "",
		1,
		nil,
		func(json.RawMessage) error { return nil },
		func(int, json.RawMessage) error { return nil },
		nil,
	)
	if err != nil {
		t.Fatalf("ExecuteStreaming: %v", err)
	}
	if rows != 1 {
		t.Errorf("rows = %d, want 1", rows)
	}
}
