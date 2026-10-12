package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// Both wrappers must let http.ResponseController reach the real writer, or a
// per-request deadline set by a streaming handler silently does nothing.
func TestWrappersExposeUnderlyingWriter(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := http.NewResponseController(w).SetWriteDeadline(time.Now().Add(time.Minute)); err != nil {
			t.Errorf("SetWriteDeadline through wrappers: %v", err)
		}
	})
	srv := httptest.NewServer(Logger(Metrics(handler)))
	defer srv.Close()
	res, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
}
