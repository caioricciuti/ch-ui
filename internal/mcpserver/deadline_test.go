package mcpserver

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// A response that outlives the server's WriteTimeout reaches the client
// once the deadline is cleared, and is cut when it is not.
func TestClearWriteDeadline(t *testing.T) {
	const writeTimeout = 150 * time.Millisecond
	serve := func(clear bool) *httptest.Server {
		srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if clear {
				clearWriteDeadline(w)
			}
			time.Sleep(2 * writeTimeout)
			_, _ = io.WriteString(w, "ok")
		}))
		srv.Config.WriteTimeout = writeTimeout
		srv.Start()
		return srv
	}

	cleared := serve(true)
	defer cleared.Close()
	res, err := http.Get(cleared.URL)
	if err != nil {
		t.Fatalf("cleared deadline: %v", err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if string(body) != "ok" {
		t.Fatalf("cleared deadline: body %q, want ok", body)
	}

	kept := serve(false)
	defer kept.Close()
	res, err = http.Get(kept.URL)
	if err == nil {
		body, _ = io.ReadAll(res.Body)
		res.Body.Close()
		if string(body) == "ok" {
			t.Fatal("server WriteTimeout did not cut the slow response; the test proves nothing")
		}
	}
}
