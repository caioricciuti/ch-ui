package server

import (
	"bytes"
	"compress/gzip"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync"
)

// compressibleExt lists the static asset types worth gzipping. Images, fonts
// and archives are already compressed and are passed through untouched.
var compressibleExt = map[string]bool{
	".html":        true,
	".css":         true,
	".js":          true,
	".mjs":         true,
	".svg":         true,
	".json":        true,
	".webmanifest": true,
	".txt":         true,
	".map":         true,
	".xml":         true,
}

// Below this size gzip saves less than its own framing and a round of CPU.
const minCompressSize = 1024

// Largest response CompressStatic buffers and caches. The embedded frontend
// is far below this; anything bigger is passed through.
const maxCompressSize = 16 << 20

type gzipEntry struct {
	header http.Header
	body   []byte
}

// CompressStatic gzips static frontend assets (HTML, CSS, JS, SVG, JSON
// manifests) for clients that send Accept-Encoding: gzip, and sets
// Vary: Accept-Encoding. Each file is compressed once, on its first request,
// and served from memory afterwards, so next must serve immutable content:
// the embedded frontend. Do not wrap API routes with it; it buffers whole
// responses and would break streaming.
//
// Range requests, non-GET/HEAD methods, non-200 responses and already
// compressed types (images, fonts) go straight to next.
func CompressStatic(next http.Handler) http.Handler {
	var cache sync.Map // request path -> *gzipEntry, or nil when not worth compressing

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isCompressiblePath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Add("Vary", "Accept-Encoding")
		if (r.Method != http.MethodGet && r.Method != http.MethodHead) ||
			r.Header.Get("Range") != "" || !acceptsGzip(r) {
			next.ServeHTTP(w, r)
			return
		}

		if v, ok := cache.Load(r.URL.Path); ok {
			if entry, _ := v.(*gzipEntry); entry != nil {
				writeGzipEntry(w, r, entry)
				return
			}
			next.ServeHTTP(w, r)
			return
		}

		buf := &bufferedResponse{header: http.Header{}, status: http.StatusOK}
		// Probe with a plain GET so a HEAD still fills the cache with a body.
		probe := r.Clone(r.Context())
		probe.Method = http.MethodGet
		next.ServeHTTP(buf, probe)

		if buf.status != http.StatusOK || buf.overflow || buf.body.Len() < minCompressSize ||
			buf.header.Get("Content-Encoding") != "" {
			if buf.status == http.StatusOK && !buf.overflow && buf.header.Get("Content-Encoding") == "" {
				cache.Store(r.URL.Path, (*gzipEntry)(nil))
			}
			buf.replay(w, r)
			return
		}

		var gz bytes.Buffer
		zw, _ := gzip.NewWriterLevel(&gz, gzip.BestCompression)
		_, _ = zw.Write(buf.body.Bytes())
		_ = zw.Close()

		header := buf.header.Clone()
		header.Del("Content-Length")
		header.Del("Accept-Ranges")
		header.Set("Content-Encoding", "gzip")
		entry := &gzipEntry{header: header, body: gz.Bytes()}
		actual, _ := cache.LoadOrStore(r.URL.Path, entry)
		if stored, _ := actual.(*gzipEntry); stored != nil {
			entry = stored
		}
		writeGzipEntry(w, r, entry)
	})
}

// isCompressiblePath reports whether p names a text asset. A path ending in
// "/" is a directory index (index.html), which is HTML.
func isCompressiblePath(p string) bool {
	if p == "" || strings.HasSuffix(p, "/") {
		return true
	}
	return compressibleExt[strings.ToLower(path.Ext(p))]
}

func acceptsGzip(r *http.Request) bool {
	for _, part := range strings.Split(r.Header.Get("Accept-Encoding"), ",") {
		fields := strings.Split(strings.TrimSpace(part), ";")
		if !strings.EqualFold(strings.TrimSpace(fields[0]), "gzip") {
			continue
		}
		for _, param := range fields[1:] {
			param = strings.ReplaceAll(strings.TrimSpace(param), " ", "")
			if param == "q=0" || param == "q=0.0" || param == "q=0.00" || param == "q=0.000" {
				return false
			}
		}
		return true
	}
	return false
}

func writeGzipEntry(w http.ResponseWriter, r *http.Request, entry *gzipEntry) {
	h := w.Header()
	for k, v := range entry.header {
		if k == "Vary" {
			continue // already set on w
		}
		h[k] = v
	}
	h.Set("Content-Length", strconv.Itoa(len(entry.body)))
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(entry.body)
	}
}

// bufferedResponse captures a response from the wrapped handler.
type bufferedResponse struct {
	header   http.Header
	status   int
	wrote    bool
	body     bytes.Buffer
	overflow bool
}

func (b *bufferedResponse) Header() http.Header { return b.header }

func (b *bufferedResponse) WriteHeader(status int) {
	if b.wrote {
		return
	}
	b.wrote = true
	b.status = status
}

func (b *bufferedResponse) Write(p []byte) (int, error) {
	if !b.wrote {
		b.WriteHeader(http.StatusOK)
	}
	if b.body.Len()+len(p) > maxCompressSize {
		b.overflow = true
	}
	return b.body.Write(p)
}

// replay sends the captured response unchanged.
func (b *bufferedResponse) replay(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	for k, v := range b.header {
		if k == "Vary" {
			continue
		}
		h[k] = v
	}
	w.WriteHeader(b.status)
	if r.Method != http.MethodHead {
		_, _ = w.Write(b.body.Bytes())
	}
}
