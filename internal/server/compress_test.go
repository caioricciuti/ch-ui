package server

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"
)

func staticFixture() (http.Handler, *atomic.Int32) {
	js := strings.Repeat("console.log('chui');\n", 400)
	fsys := fstest.MapFS{
		"index.html":       {Data: []byte("<!doctype html><title>CH-UI</title>" + strings.Repeat("<p>x</p>", 300))},
		"assets/app.js":    {Data: []byte(js)},
		"assets/tiny.css":  {Data: []byte("a{}")},
		"assets/logo.png":  {Data: bytes.Repeat([]byte{0x89, 'P', 'N', 'G'}, 1000)},
		"assets/icon.svg":  {Data: []byte("<svg>" + strings.Repeat("<g/>", 500) + "</svg>")},
		"manifest.json":    {Data: []byte(`{"name":"` + strings.Repeat("a", 2000) + `"}`)},
		"assets/font.woff": {Data: bytes.Repeat([]byte{1, 2, 3}, 1000)},
	}
	var calls atomic.Int32
	fileServer := http.FileServer(http.FS(fsys))
	counted := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		fileServer.ServeHTTP(w, r)
	})
	return CompressStatic(counted), &calls
}

func get(h http.Handler, path, acceptEncoding string, extra ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if acceptEncoding != "" {
		req.Header.Set("Accept-Encoding", acceptEncoding)
	}
	for i := 0; i+1 < len(extra); i += 2 {
		req.Header.Set(extra[i], extra[i+1])
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func gunzip(t *testing.T, b []byte) string {
	t.Helper()
	zr, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	out, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func TestCompressStaticGzipsTextAssets(t *testing.T) {
	h, _ := staticFixture()
	for _, path := range []string{"/", "/assets/app.js", "/assets/icon.svg", "/manifest.json"} {
		rec := get(h, path, "br, gzip;q=0.8")
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status %d", path, rec.Code)
		}
		if rec.Header().Get("Content-Encoding") != "gzip" {
			t.Fatalf("%s: not gzipped", path)
		}
		if !strings.Contains(rec.Header().Get("Vary"), "Accept-Encoding") {
			t.Fatalf("%s: missing Vary", path)
		}
		if rec.Header().Get("Accept-Ranges") != "" {
			t.Fatalf("%s: gzipped response must not advertise ranges", path)
		}
		if got := rec.Header().Get("Content-Length"); got != "" && got != strconv.Itoa(rec.Body.Len()) {
			t.Fatalf("%s: Content-Length %s for %d bytes", path, got, rec.Body.Len())
		}
		plain := get(h, path, "")
		if plain.Header().Get("Content-Encoding") != "" {
			t.Fatalf("%s: gzipped without Accept-Encoding", path)
		}
		if gunzip(t, rec.Body.Bytes()) != plain.Body.String() {
			t.Fatalf("%s: gzip body does not match the original", path)
		}
		if ct := rec.Header().Get("Content-Type"); ct == "" || ct != plain.Header().Get("Content-Type") {
			t.Fatalf("%s: content type %q vs %q", path, ct, plain.Header().Get("Content-Type"))
		}
	}
}

func TestCompressStaticCompressesOnce(t *testing.T) {
	h, calls := staticFixture()
	for i := 0; i < 5; i++ {
		if rec := get(h, "/assets/app.js", "gzip"); rec.Header().Get("Content-Encoding") != "gzip" {
			t.Fatal("not gzipped")
		}
	}
	if n := calls.Load(); n != 1 {
		t.Fatalf("file served %d times, want 1 (cached after the first)", n)
	}
}

func TestCompressStaticSkips(t *testing.T) {
	h, _ := staticFixture()
	cases := []struct {
		name, path, ae string
		extra          []string
	}{
		{"png", "/assets/logo.png", "gzip", nil},
		{"font", "/assets/font.woff", "gzip", nil},
		{"tiny", "/assets/tiny.css", "gzip", nil},
		{"refused", "/assets/app.js", "gzip;q=0, identity", nil},
		{"range", "/assets/app.js", "gzip", []string{"Range", "bytes=0-10"}},
		{"missing", "/assets/missing.js", "gzip", nil},
	}
	for _, tc := range cases {
		rec := get(h, tc.path, tc.ae, tc.extra...)
		if rec.Header().Get("Content-Encoding") != "" {
			t.Fatalf("%s: must not be gzipped", tc.name)
		}
	}
	if rec := get(h, "/assets/app.js", "gzip", "Range", "bytes=0-10"); rec.Code != http.StatusPartialContent {
		t.Fatalf("range request: %d, want 206", rec.Code)
	}
	// Tiny files are remembered as not worth compressing and still served.
	for i := 0; i < 2; i++ {
		if rec := get(h, "/assets/tiny.css", "gzip"); rec.Code != http.StatusOK || rec.Body.String() != "a{}" {
			t.Fatalf("tiny css: %d %q", rec.Code, rec.Body.String())
		}
	}
}

func TestCompressStaticHead(t *testing.T) {
	h, _ := staticFixture()
	req := httptest.NewRequest(http.MethodHead, "/assets/app.js", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Body.Len() != 0 || rec.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("HEAD: %d len=%d ce=%q", rec.Code, rec.Body.Len(), rec.Header().Get("Content-Encoding"))
	}
}

func TestCompressStaticKeepsCallerHeaders(t *testing.T) {
	inner, _ := staticFixture()
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		inner.ServeHTTP(w, r)
	})
	rec := get(h, "/assets/app.js", "gzip")
	if rec.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" {
		t.Fatalf("Cache-Control lost: %q", rec.Header().Get("Cache-Control"))
	}
}

func TestCompressStreamJSON(t *testing.T) {
	body := `{"rows":[` + strings.Repeat(`{"a":1,"b":"x"},`, 200) + `{}]}`
	h := CompressStream(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		_, _ = io.WriteString(w, body)
	}))

	rec := get(h, "/mcp", "gzip, deflate, br")
	if rec.Header().Get("Content-Encoding") != "gzip" || rec.Header().Get("Content-Length") != "" {
		t.Fatalf("want gzip without Content-Length, got %v", rec.Header())
	}
	if !strings.Contains(rec.Header().Get("Vary"), "Accept-Encoding") {
		t.Error("missing Vary: Accept-Encoding")
	}
	if got := gunzip(t, rec.Body.Bytes()); got != body {
		t.Error("decompressed body differs")
	}
	if rec.Body.Len() >= len(body)/4 {
		t.Errorf("compressed %d bytes, original %d", rec.Body.Len(), len(body))
	}

	plain := get(h, "/mcp", "")
	if plain.Header().Get("Content-Encoding") != "" || plain.Body.String() != body {
		t.Error("a client without Accept-Encoding: gzip must get the plain body")
	}
}

func TestCompressStreamPassesOtherResponses(t *testing.T) {
	for name, set := range map[string]func(http.Header){
		"png":            func(h http.Header) { h.Set("Content-Type", "image/png") },
		"already-zipped": func(h http.Header) { h.Set("Content-Type", "application/json"); h.Set("Content-Encoding", "br") },
		"no-type":        func(http.Header) {},
	} {
		h := CompressStream(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			set(w.Header())
			_, _ = io.WriteString(w, "payload")
		}))
		rec := get(h, "/mcp", "gzip")
		if rec.Header().Get("Content-Encoding") == "gzip" || rec.Body.String() != "payload" {
			t.Errorf("%s: should pass through, got %q %v", name, rec.Body.String(), rec.Header())
		}
	}
}

// Each flushed event must reach the client while the stream is still open,
// and the wrapper must not hide the connection from ResponseController.
func TestCompressStreamFlushesEvents(t *testing.T) {
	firstEvent := "data: " + strings.Repeat("x", 2*minCompressSize) + "\n\n"
	release := make(chan struct{})
	deadlineErr := make(chan error, 1)
	srv := httptest.NewServer(CompressStream(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		deadlineErr <- http.NewResponseController(w).SetWriteDeadline(time.Time{})
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, firstEvent)
		w.(http.Flusher).Flush()
		<-release
		_, _ = io.WriteString(w, "data: second\n\n")
	})))
	defer srv.Close()
	defer close(release)

	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	req.Header.Set("Accept-Encoding", "gzip")
	resp, err := (&http.Client{Transport: &http.Transport{DisableCompression: true}}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if err := <-deadlineErr; err != nil {
		t.Errorf("SetWriteDeadline through the wrapper: %v", err)
	}
	if resp.Header.Get("Content-Encoding") != "gzip" {
		t.Fatalf("want gzip event stream, got %v", resp.Header)
	}
	zr, err := gzip.NewReader(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	line := make(chan string, 1)
	go func() {
		buf := make([]byte, len(firstEvent))
		_, _ = io.ReadFull(zr, buf)
		line <- string(buf)
	}()
	select {
	case got := <-line:
		if got != firstEvent {
			t.Errorf("first event: %.40q", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("first event did not arrive before the stream ended: Flush is not reaching the client")
	}
}

// A response that is still small when it ends or first flushes goes out
// plain: gzip framing would make it bigger.
func TestCompressStreamSkipsSmallResponses(t *testing.T) {
	small := `{"error":"unknown or revoked token"}`
	h := CompressStream(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, small)
	}))
	rec := get(h, "/mcp", "gzip")
	if rec.Code != http.StatusUnauthorized || rec.Header().Get("Content-Encoding") != "" || rec.Body.String() != small {
		t.Errorf("small response: %d %v %q", rec.Code, rec.Header(), rec.Body.String())
	}

	flushed := CompressStream(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: hi\n\n")
		w.(http.Flusher).Flush()
		_, _ = io.WriteString(w, strings.Repeat("data: later\n\n", 500))
	}))
	rec = get(flushed, "/mcp", "gzip")
	if rec.Header().Get("Content-Encoding") != "" || !strings.HasPrefix(rec.Body.String(), "data: hi\n\n") || !strings.HasSuffix(rec.Body.String(), "data: later\n\n") {
		t.Errorf("stream small at first flush must stay plain throughout: %v %.40q", rec.Header(), rec.Body.String())
	}
}
