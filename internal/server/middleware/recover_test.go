package middleware

import (
	"bufio"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeHijacker is a ResponseWriter that supports hijacking, like a real server
// connection (needed for WebSocket upgrades).
type fakeHijacker struct {
	http.ResponseWriter
	hijacked bool
}

func (f *fakeHijacker) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	f.hijacked = true
	return nil, nil, nil
}

// Regression test: the Metrics wrapper must preserve http.Hijacker so the
// WebSocket tunnel (/connect) can upgrade. Losing it returns 500
// "feature not supported".
func TestMetrics_PreservesHijacker(t *testing.T) {
	reached := false
	h := Metrics(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Fatal("Metrics wrapper dropped http.Hijacker — WebSocket upgrades would fail")
		}
		_, _, _ = hj.Hijack()
		reached = true
	}))
	fw := &fakeHijacker{ResponseWriter: httptest.NewRecorder()}
	h.ServeHTTP(fw, httptest.NewRequest(http.MethodGet, "/api/x", nil))
	if !reached || !fw.hijacked {
		t.Fatal("hijack did not reach the underlying writer")
	}
}

func TestRecoverer_TurnsPanicInto500(t *testing.T) {
	h := Recoverer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	}))

	rec := httptest.NewRecorder()
	// Must not propagate the panic to the caller.
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
}

func TestRecoverer_PassesThroughNormalResponses(t *testing.T) {
	h := Recoverer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))

	if rec.Code != http.StatusTeapot {
		t.Fatalf("expected 418 passthrough, got %d", rec.Code)
	}
}

func TestMetrics_ClassOf(t *testing.T) {
	m := &metricsState{}
	cases := map[int]string{
		100: "1xx", 200: "2xx", 204: "2xx", 301: "3xx",
		400: "4xx", 404: "4xx", 500: "5xx", 503: "5xx",
	}
	for status, want := range cases {
		if got := m.classOf(status); got != want {
			t.Errorf("classOf(%d) = %q, want %q", status, got, want)
		}
	}
}

func TestMetrics_RecordsAndExports(t *testing.T) {
	// Exercise the middleware, then confirm the handler exposes the build_info line.
	h := Metrics(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/x", nil))

	rec := httptest.NewRecorder()
	MetricsHandler()(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("metrics endpoint returned %d", rec.Code)
	}
	for _, want := range []string{"ch_ui_build_info", "ch_ui_http_requests_total", "go_goroutines"} {
		if !strings.Contains(body, want) {
			t.Errorf("metrics output missing %q", want)
		}
	}
	// Unauthenticated: the build must not be identifiable.
	for _, banned := range []string{"version=", "commit="} {
		if strings.Contains(body, "ch_ui_build_info{"+banned) || strings.Contains(body, ","+banned) {
			t.Errorf("metrics output exposes %q", banned)
		}
	}
}
