package middleware

import (
	"net/http"
	"sync"
	"time"
)

// BodyLimit caps the size of request bodies to maxBytes. Handlers that need a
// stricter limit (uploads, webhooks) can still wrap the body again; this is the
// outer bound that prevents an unbounded JSON decode from exhausting memory.
func BodyLimit(maxBytes int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Body != nil {
				r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ipRateLimiter is a small in-memory fixed-window rate limiter keyed by client
// IP. It is used to protect unauthenticated endpoints (public dashboards) from
// abuse without a database round-trip.
type ipRateLimiter struct {
	mu     sync.Mutex
	counts map[string]*windowCount
	limit  int
	window time.Duration
	lastGC time.Time
}

type windowCount struct {
	count int
	reset time.Time
}

// IPRateLimit returns middleware allowing at most `limit` requests per `window`
// per client IP.
func IPRateLimit(limit int, window time.Duration) func(http.Handler) http.Handler {
	rl := &ipRateLimiter{
		counts: make(map[string]*windowCount),
		limit:  limit,
		window: window,
		lastGC: time.Now(),
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !rl.allow(ClientIP(r)) {
				w.Header().Set("Retry-After", "60")
				writeError(w, http.StatusTooManyRequests, "Rate limit exceeded. Please slow down.")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func (rl *ipRateLimiter) allow(ip string) bool {
	now := time.Now()
	rl.mu.Lock()
	defer rl.mu.Unlock()

	// Opportunistic cleanup of expired windows.
	if now.Sub(rl.lastGC) > rl.window {
		for k, wc := range rl.counts {
			if now.After(wc.reset) {
				delete(rl.counts, k)
			}
		}
		rl.lastGC = now
	}

	wc, ok := rl.counts[ip]
	if !ok || now.After(wc.reset) {
		rl.counts[ip] = &windowCount{count: 1, reset: now.Add(rl.window)}
		return true
	}
	if wc.count >= rl.limit {
		return false
	}
	wc.count++
	return true
}
