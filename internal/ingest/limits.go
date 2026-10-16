package ingest

import (
	"sync"
	"time"
)

// limiter holds the per-key request rate and the per-pipeline daily quota.
// Both live in memory: a restart refills the buckets and resets the day's
// count.
type limiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	quotas  map[string]*quota
	lastGC  time.Time
}

// bucket is a token bucket that holds one second of requests.
type bucket struct {
	tokens float64
	last   time.Time
}

// quota counts decompressed bytes for one UTC day.
type quota struct {
	day  string
	used int64
}

func newLimiter() *limiter {
	return &limiter{buckets: map[string]*bucket{}, quotas: map[string]*quota{}}
}

// allow takes one token from the key's bucket, refilled at rps per second.
func (l *limiter) allow(keyID string, rps int, now time.Time) bool {
	if rps <= 0 {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.gcLocked(now)
	b := l.buckets[keyID]
	if b == nil {
		b = &bucket{tokens: float64(rps), last: now}
		l.buckets[keyID] = b
	}
	b.tokens = min(float64(rps), b.tokens+now.Sub(b.last).Seconds()*float64(rps))
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// gcLocked drops buckets idle for a minute (they would be full again).
func (l *limiter) gcLocked(now time.Time) {
	if now.Sub(l.lastGC) < time.Minute {
		return
	}
	l.lastGC = now
	for k, b := range l.buckets {
		if now.Sub(b.last) > time.Minute {
			delete(l.buckets, k)
		}
	}
}

func (l *limiter) quotaLocked(pipelineID string, now time.Time) *quota {
	day := now.UTC().Format(time.DateOnly)
	q := l.quotas[pipelineID]
	if q == nil || q.day != day {
		q = &quota{day: day}
		l.quotas[pipelineID] = q
	}
	return q
}

// quotaLeft reports whether the pipeline has any of today's quota left.
func (l *limiter) quotaLeft(pipelineID string, limit int64, now time.Time) bool {
	if limit <= 0 {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.quotaLocked(pipelineID, now).used < limit
}

// charge adds n bytes to today's count, or refuses them all when that would
// pass the limit.
func (l *limiter) charge(pipelineID string, limit, n int64, now time.Time) bool {
	if limit <= 0 {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	q := l.quotaLocked(pipelineID, now)
	if q.used+n > limit {
		return false
	}
	q.used += n
	return true
}

// untilMidnightUTC is the Retry-After for an exhausted daily quota.
func untilMidnightUTC(now time.Time) time.Duration {
	u := now.UTC()
	next := time.Date(u.Year(), u.Month(), u.Day()+1, 0, 0, 0, 0, time.UTC)
	return next.Sub(u)
}
