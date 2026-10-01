package middleware

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"net/http"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// metricsState holds lightweight, dependency-free server metrics exported in
// Prometheus text exposition format at /metrics. It is intentionally minimal —
// HTTP request counters, in-flight gauge, latency sum, and Go runtime stats —
// so an SRE team can scrape liveness, traffic, error rate, and latency without
// pulling in a metrics client library.
type metricsState struct {
	startTime     time.Time
	requestsTotal sync.Map // statusClass (string "2xx".."5xx") -> *int64
	inFlight      int64
	durationNanos int64
	requestCount  int64
}

var serverMetrics = &metricsState{startTime: time.Now()}

func (m *metricsState) classOf(status int) string {
	switch {
	case status < 200:
		return "1xx"
	case status < 300:
		return "2xx"
	case status < 400:
		return "3xx"
	case status < 500:
		return "4xx"
	default:
		return "5xx"
	}
}

func (m *metricsState) observe(status int, d time.Duration) {
	class := m.classOf(status)
	ctr, _ := m.requestsTotal.LoadOrStore(class, new(int64))
	atomic.AddInt64(ctr.(*int64), 1)
	atomic.AddInt64(&m.requestCount, 1)
	atomic.AddInt64(&m.durationNanos, int64(d))
}

// metricsResponseWriter captures the status code for metrics.
type metricsResponseWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (w *metricsResponseWriter) WriteHeader(code int) {
	if !w.wroteHeader {
		w.status = code
		w.wroteHeader = true
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *metricsResponseWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.status = http.StatusOK
		w.wroteHeader = true
	}
	return w.ResponseWriter.Write(b)
}

// Flush implements http.Flusher so SSE/streaming responses keep working.
func (w *metricsResponseWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Hijack implements http.Hijacker so WebSocket upgrades (the tunnel gateway at
// /connect) keep working when this wrapper is in the chain. Without this, the
// upgrade fails with "feature not supported".
func (w *metricsResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hj, ok := w.ResponseWriter.(http.Hijacker); ok {
		return hj.Hijack()
	}
	return nil, nil, errors.New("underlying ResponseWriter does not support hijacking")
}

// Metrics records per-request counters for the /metrics endpoint.
func Metrics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip the /metrics scrape itself and the long-lived WebSocket tunnel
		// (which is hijacked and would pollute the latency metric).
		if r.URL.Path == "/metrics" || r.URL.Path == "/connect" {
			next.ServeHTTP(w, r)
			return
		}
		atomic.AddInt64(&serverMetrics.inFlight, 1)
		defer atomic.AddInt64(&serverMetrics.inFlight, -1)

		start := time.Now()
		mw := &metricsResponseWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(mw, r)
		serverMetrics.observe(mw.status, time.Since(start))
	})
}

// MetricsHandler serves the Prometheus text exposition of server metrics.
func MetricsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var mem runtime.MemStats
		runtime.ReadMemStats(&mem)
		m := serverMetrics

		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")

		// No version or commit: /metrics is unauthenticated, and naming the
		// exact build tells a scanner which advisories apply (same reason
		// /health dropped it in #177).
		fmt.Fprintf(w, "# HELP ch_ui_build_info Build information.\n")
		fmt.Fprintf(w, "# TYPE ch_ui_build_info gauge\n")
		fmt.Fprintf(w, "ch_ui_build_info{go_version=%q} 1\n", runtime.Version())

		fmt.Fprintf(w, "# HELP ch_ui_uptime_seconds Seconds since the server started.\n")
		fmt.Fprintf(w, "# TYPE ch_ui_uptime_seconds gauge\n")
		fmt.Fprintf(w, "ch_ui_uptime_seconds %d\n", int64(time.Since(m.startTime).Seconds()))

		fmt.Fprintf(w, "# HELP ch_ui_http_requests_total Total HTTP requests by status class.\n")
		fmt.Fprintf(w, "# TYPE ch_ui_http_requests_total counter\n")
		for _, class := range []string{"1xx", "2xx", "3xx", "4xx", "5xx"} {
			var v int64
			if ctr, ok := m.requestsTotal.Load(class); ok {
				v = atomic.LoadInt64(ctr.(*int64))
			}
			fmt.Fprintf(w, "ch_ui_http_requests_total{class=%q} %d\n", class, v)
		}

		fmt.Fprintf(w, "# HELP ch_ui_http_requests_in_flight In-flight HTTP requests.\n")
		fmt.Fprintf(w, "# TYPE ch_ui_http_requests_in_flight gauge\n")
		fmt.Fprintf(w, "ch_ui_http_requests_in_flight %d\n", atomic.LoadInt64(&m.inFlight))

		fmt.Fprintf(w, "# HELP ch_ui_http_request_duration_seconds_sum Cumulative request duration.\n")
		fmt.Fprintf(w, "# TYPE ch_ui_http_request_duration_seconds_sum counter\n")
		fmt.Fprintf(w, "ch_ui_http_request_duration_seconds_sum %f\n",
			float64(atomic.LoadInt64(&m.durationNanos))/1e9)
		fmt.Fprintf(w, "# HELP ch_ui_http_request_duration_seconds_count Completed request count.\n")
		fmt.Fprintf(w, "# TYPE ch_ui_http_request_duration_seconds_count counter\n")
		fmt.Fprintf(w, "ch_ui_http_request_duration_seconds_count %d\n", atomic.LoadInt64(&m.requestCount))

		fmt.Fprintf(w, "# HELP go_goroutines Number of goroutines.\n")
		fmt.Fprintf(w, "# TYPE go_goroutines gauge\n")
		fmt.Fprintf(w, "go_goroutines %d\n", runtime.NumGoroutine())

		fmt.Fprintf(w, "# HELP go_memstats_alloc_bytes Bytes of allocated heap objects.\n")
		fmt.Fprintf(w, "# TYPE go_memstats_alloc_bytes gauge\n")
		fmt.Fprintf(w, "go_memstats_alloc_bytes %d\n", mem.Alloc)

		fmt.Fprintf(w, "# HELP go_memstats_sys_bytes Bytes of memory obtained from the OS.\n")
		fmt.Fprintf(w, "# TYPE go_memstats_sys_bytes gauge\n")
		fmt.Fprintf(w, "go_memstats_sys_bytes %d\n", mem.Sys)

		fmt.Fprintf(w, "# HELP go_gc_runs_total Completed GC cycles.\n")
		fmt.Fprintf(w, "# TYPE go_gc_runs_total counter\n")
		fmt.Fprintf(w, "go_gc_runs_total %s\n", strconv.FormatUint(uint64(mem.NumGC), 10))
	}
}
