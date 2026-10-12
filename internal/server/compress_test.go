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
