package middleware

import (
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/database/dbtest"
)

func TestProgressiveLockoutSchedule(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "rate-limit.db")
	db, err := database.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	rl := NewRateLimiter(db)
	identifier := "user:test"
	maxAttempts := 3
	window := 15 * time.Minute

	expectLocked := func(expectedMin time.Duration, expectedMax time.Duration, expectedType string) {
		t.Helper()
		res := rl.CheckAuthRateLimit(identifier, "user", maxAttempts, window)
		if res.Allowed {
			t.Fatalf("expected blocked result for %s", expectedType)
		}
		if res.RetryAfter < expectedMin || res.RetryAfter > expectedMax {
			t.Fatalf("retryAfter out of range: got=%s want=[%s,%s]", res.RetryAfter, expectedMin, expectedMax)
		}
		entry, err := db.GetRateLimit(identifier)
		if err != nil {
			t.Fatalf("get rate limit: %v", err)
		}
		if entry == nil {
			t.Fatalf("expected persisted rate limit entry")
		}
		if entry.Type != expectedType {
			t.Fatalf("unexpected entry type: got=%q want=%q", entry.Type, expectedType)
		}
		if entry.LockedUntil == nil {
			t.Fatalf("expected locked_until to be set")
		}
	}

	expireLock := func(expectedType string) {
		t.Helper()
		now := time.Now()
		expired := now.Add(-1 * time.Second)
		if err := db.UpsertRateLimit(identifier, expectedType, maxAttempts, now.Add(-2*time.Minute), &expired); err != nil {
			t.Fatalf("upsert expired lock: %v", err)
		}
		res := rl.CheckAuthRateLimit(identifier, "user", maxAttempts, window)
		if !res.Allowed {
			t.Fatalf("expected allowed result after lock expiry, got blocked retryAfter=%s", res.RetryAfter)
		}
		entry, err := db.GetRateLimit(identifier)
		if err != nil {
			t.Fatalf("get rate limit after expiry: %v", err)
		}
		if entry == nil {
			t.Fatalf("expected rate limit entry after lock expiry")
		}
		if entry.Type != expectedType {
			t.Fatalf("unexpected type after expiry: got=%q want=%q", entry.Type, expectedType)
		}
		if entry.Attempts != 0 {
			t.Fatalf("attempts should reset after expiry: got=%d", entry.Attempts)
		}
		if entry.LockedUntil != nil {
			t.Fatalf("expected lock to be cleared after expiry")
		}
	}

	if err := db.UpsertRateLimit(identifier, "user", maxAttempts, time.Now(), nil); err != nil {
		t.Fatalf("seed first lock: %v", err)
	}
	expectLocked(3*time.Minute-5*time.Second, 3*time.Minute+5*time.Second, "user:1")

	expireLock("user:1")
	if err := db.UpsertRateLimit(identifier, "user:1", maxAttempts, time.Now(), nil); err != nil {
		t.Fatalf("seed second lock: %v", err)
	}
	expectLocked(5*time.Minute-5*time.Second, 5*time.Minute+5*time.Second, "user:2")

	expireLock("user:2")
	if err := db.UpsertRateLimit(identifier, "user:2", maxAttempts, time.Now(), nil); err != nil {
		t.Fatalf("seed third lock: %v", err)
	}
	expectLocked(10*time.Minute-5*time.Second, 10*time.Minute+5*time.Second, "user:3")

	expireLock("user:3")
	if err := db.UpsertRateLimit(identifier, "user:3", maxAttempts, time.Now(), nil); err != nil {
		t.Fatalf("seed capped lock: %v", err)
	}
	expectLocked(10*time.Minute-5*time.Second, 10*time.Minute+5*time.Second, "user:3")
}

func TestLegacyLongLockIsCappedToCurrentSchedule(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "rate-limit-legacy.db")
	db, err := database.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	rl := NewRateLimiter(db)
	identifier := "user:legacy"
	now := time.Now()
	legacyUntil := now.Add(2 * time.Hour)
	if err := db.UpsertRateLimit(identifier, "user:3", 3, now.Add(-1*time.Minute), &legacyUntil); err != nil {
		t.Fatalf("seed legacy lock: %v", err)
	}

	res := rl.CheckAuthRateLimit(identifier, "user", 3, 15*time.Minute)
	if res.Allowed {
		t.Fatalf("expected request to remain blocked during capped lock window")
	}
	if res.RetryAfter > 10*time.Minute+5*time.Second {
		t.Fatalf("legacy lock should be capped to 10m, got retryAfter=%s", res.RetryAfter)
	}

	entry, err := db.GetRateLimit(identifier)
	if err != nil {
		t.Fatalf("get rate limit: %v", err)
	}
	if entry == nil || entry.LockedUntil == nil {
		t.Fatalf("expected normalized lock to be persisted")
	}

	normalizedUntil, err := time.Parse(time.RFC3339, *entry.LockedUntil)
	if err != nil {
		t.Fatalf("parse locked_until: %v", err)
	}
	if normalizedUntil.After(time.Now().Add(10*time.Minute + 5*time.Second)) {
		t.Fatalf("persisted lock should be capped near now+10m, got %s", normalizedUntil)
	}
}

func TestParallelAttemptsCannotExceedLimit(t *testing.T) {
	db, err := dbtest.Open(t, "rate-limit-parallel.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	rl := NewRateLimiter(db)
	const maxAttempts = 3
	const workers = 40
	identifier := "ip:203.0.113.7"

	// Every worker checks, waits as a slow credential test would, then
	// records a failure. Only maxAttempts may pass the check.
	var allowed atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			res := rl.CheckAuthRateLimit(identifier, "ip", maxAttempts, 15*time.Minute)
			if !res.Allowed {
				return
			}
			allowed.Add(1)
			time.Sleep(20 * time.Millisecond)
			rl.RecordAttempt(identifier, "ip")
		}()
	}
	close(start)
	wg.Wait()

	if got := allowed.Load(); got > maxAttempts {
		t.Fatalf("%d parallel attempts passed the check, want at most %d", got, maxAttempts)
	}
	entry, err := db.GetRateLimit(identifier)
	if err != nil || entry == nil {
		t.Fatalf("get rate limit: %v %v", entry, err)
	}
	if entry.Attempts != int(allowed.Load()) {
		t.Fatalf("stored attempts = %d, want %d", entry.Attempts, allowed.Load())
	}
	if res := rl.CheckAuthRateLimit(identifier, "ip", maxAttempts, 15*time.Minute); res.Allowed {
		t.Fatal("limit reached: the next attempt must be refused")
	}
}

func TestInFlightRefusalIsTransient(t *testing.T) {
	db, err := dbtest.Open(t, "rate-limit-inflight.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	rl := NewRateLimiter(db)
	id := "user:inflight"
	for i := 0; i < 2; i++ {
		if !rl.CheckAuthRateLimit(id, "user", 2, 15*time.Minute).Allowed {
			t.Fatalf("attempt %d should pass", i)
		}
	}
	res := rl.CheckAuthRateLimit(id, "user", 2, 15*time.Minute)
	if res.Allowed || res.RetryAfter > reservationTTL {
		t.Fatalf("third in-flight attempt: allowed=%v retryAfter=%s", res.Allowed, res.RetryAfter)
	}
	if entry, _ := db.GetRateLimit(id); entry != nil {
		t.Fatalf("in-flight refusal must not persist a lockout: %+v", entry)
	}
	rl.Release(id)
	if !rl.CheckAuthRateLimit(id, "user", 2, 15*time.Minute).Allowed {
		t.Fatal("a released slot must be usable again")
	}
	rl.ResetLimit(id)
	if !rl.CheckAuthRateLimit(id, "user", 2, 15*time.Minute).Allowed {
		t.Fatal("reset must clear in-flight slots")
	}
}
