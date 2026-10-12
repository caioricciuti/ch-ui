package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

const (
	// brainWriteWindow is how far each write pushes the response's write
	// deadline. The server-wide WriteTimeout would otherwise cut a long
	// Brain turn (approval waits, slow tools) at five minutes.
	brainWriteWindow = 10 * time.Minute
	// brainHeartbeat keeps proxies from closing an idle stream while Brain
	// waits on an approval, a tool or the provider.
	brainHeartbeat = 15 * time.Second
)

// sseStream serializes Server-Sent Event writes for one Brain response, so
// the heartbeat goroutine and the turn never interleave a frame, and keeps
// the write deadline moving while the stream is alive.
type sseStream struct {
	mu      sync.Mutex
	w       http.ResponseWriter
	flusher http.Flusher
	rc      *http.ResponseController
}

func newSSEStream(w http.ResponseWriter, flusher http.Flusher) *sseStream {
	s := &sseStream{w: w, flusher: flusher, rc: http.NewResponseController(w)}
	s.extend()
	return s
}

// extend moves the write deadline forward. It is best effort: a
// ResponseWriter wrapper without Unwrap reports ErrNotSupported, and then
// the server-wide WriteTimeout still applies.
func (s *sseStream) extend() {
	_ = s.rc.SetWriteDeadline(time.Now().Add(brainWriteWindow))
}

// send writes one data event.
func (s *sseStream) send(payload map[string]interface{}) error {
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.extend()
	if _, err := fmt.Fprintf(s.w, "data: %s\n\n", b); err != nil {
		return err
	}
	s.flusher.Flush()
	return nil
}

// ping writes an SSE comment, which clients ignore.
func (s *sseStream) ping() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.extend()
	if _, err := fmt.Fprint(s.w, ": ping\n\n"); err != nil {
		return err
	}
	s.flusher.Flush()
	return nil
}

// startHeartbeat pings every interval until ctx ends or stop is called.
// stop waits for the goroutine, so no write happens after it returns.
func (s *sseStream) startHeartbeat(ctx context.Context, interval time.Duration) (stop func()) {
	done := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-done:
				return
			case <-t.C:
				if s.ping() != nil {
					return
				}
			}
		}
	}()
	var once sync.Once
	return func() {
		once.Do(func() { close(done) })
		<-finished
	}
}
