package middleware

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/caioricciuti/ch-ui/internal/database"
)

// RateLimiter provides rate limiting backed by SQLite.
//
// Callers check first and record a failure later, after the slow credential
// test. Without coordination, N parallel attempts all pass the check before
// any of them is recorded. So every allowed check reserves a slot: the
// reservation counts against the limit until RecordAttempt turns it into a
// stored attempt, ResetLimit clears it, Release drops it, or it expires after
// reservationTTL (for callers that return early without recording).
type RateLimiter struct {
	db *database.DB

	mu       sync.Mutex
	inFlight map[string][]time.Time
}

// reservationTTL bounds how long an allowed check holds a slot without being
// recorded. It covers the login path's tunnel wait and 15 s credential test.
const reservationTTL = 20 * time.Second

// NewRateLimiter creates a new rate limiter.
func NewRateLimiter(db *database.DB) *RateLimiter {
	return &RateLimiter{db: db, inFlight: map[string][]time.Time{}}
}

// liveReservations drops expired reservations for identifier and returns how
// many are left. Callers hold rl.mu.
func (rl *RateLimiter) liveReservations(identifier string, now time.Time) int {
	res := rl.inFlight[identifier]
	kept := res[:0]
	for _, at := range res {
		if now.Sub(at) < reservationTTL {
			kept = append(kept, at)
		}
	}
	if len(kept) == 0 {
		delete(rl.inFlight, identifier)
		return 0
	}
	rl.inFlight[identifier] = kept
	return len(kept)
}

// takeReservation removes the oldest reservation for identifier, if any.
// Callers hold rl.mu.
func (rl *RateLimiter) takeReservation(identifier string) {
	res := rl.inFlight[identifier]
	if len(res) <= 1 {
		delete(rl.inFlight, identifier)
		return
	}
	rl.inFlight[identifier] = res[1:]
}

// reserve records an in-flight attempt and returns the allowed result.
// Callers hold rl.mu.
func (rl *RateLimiter) reserve(identifier string, now time.Time, maxAttempts int) RateLimitResult {
	if rl.inFlight == nil {
		rl.inFlight = map[string][]time.Time{}
	}
	rl.inFlight[identifier] = append(rl.inFlight[identifier], now)
	return RateLimitResult{Allowed: true, MaxAttempts: maxAttempts}
}

// Release gives back the slot reserved by an allowed CheckAuthRateLimit when
// the request ends without RecordAttempt or ResetLimit (for example a 400
// before the credential test). Optional: unreleased slots expire on their own.
func (rl *RateLimiter) Release(identifier string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	rl.takeReservation(identifier)
}

// RateLimitResult holds the result of a rate limit check.
type RateLimitResult struct {
	Allowed     bool
	RetryAfter  time.Duration
	Attempts    int
	MaxAttempts int
}

var lockoutSchedule = []time.Duration{
	3 * time.Minute,
	5 * time.Minute,
	10 * time.Minute,
}

// CheckAuthRateLimit checks if a login attempt is allowed.
// Returns whether the attempt is allowed, and if not, how long to wait.
// Check and reservation happen under one lock, so parallel attempts cannot
// all pass before any of them is recorded.
func (rl *RateLimiter) CheckAuthRateLimit(identifier, limitType string, maxAttempts int, windowDuration time.Duration) RateLimitResult {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	pending := rl.liveReservations(identifier, now)

	entry, err := rl.db.GetRateLimit(identifier)
	if err != nil {
		// On error, allow the request
		return RateLimitResult{Allowed: true, MaxAttempts: maxAttempts}
	}

	if entry != nil {
		baseType, lockLevel := parseLimitTypeAndLockLevel(entry.Type, limitType)
		entryType := formatLimitTypeWithLockLevel(baseType, lockLevel)

		// Check if locked out
		if entry.LockedUntil != nil {
			lockedUntil, err := time.Parse(time.RFC3339, *entry.LockedUntil)
			if err == nil && now.Before(lockedUntil) {
				// Compatibility: normalize legacy long locks to the new capped schedule.
				activeLevel := lockLevel
				if activeLevel <= 0 {
					activeLevel = 1
				}
				maxDuration := lockoutDurationForLevel(activeLevel)
				if remaining := time.Until(lockedUntil); remaining > maxDuration {
					normalizedUntil := now.Add(maxDuration)
					lockedUntil = normalizedUntil
					rl.db.UpsertRateLimit(
						identifier,
						formatLimitTypeWithLockLevel(baseType, activeLevel),
						entry.Attempts,
						now,
						&normalizedUntil,
					)
				}
				return RateLimitResult{
					Allowed:     false,
					RetryAfter:  time.Until(lockedUntil),
					Attempts:    entry.Attempts,
					MaxAttempts: maxAttempts,
				}
			}
			// Lock expired: keep escalation level, reset attempt window.
			rl.db.UpsertRateLimit(identifier, entryType, 0, now, nil)
			return rl.reserveIfRoom(identifier, now, 0, pending, maxAttempts)
		}

		// Check if window expired
		firstAttempt, err := time.Parse(time.RFC3339, entry.FirstAttemptAt)
		if err == nil && now.Sub(firstAttempt) > windowDuration {
			// Window expired, reset
			// Escalation is reset once the attempts window is clean.
			rl.db.UpsertRateLimit(identifier, limitType, 0, now, nil)
			return rl.reserveIfRoom(identifier, now, 0, pending, maxAttempts)
		}

		// Check attempts
		if entry.Attempts >= maxAttempts {
			// Lock out
			nextLevel := nextLockoutLevel(lockLevel)
			lockoutDuration := lockoutDurationForLevel(nextLevel)
			lockedUntil := now.Add(lockoutDuration)
			rl.db.UpsertRateLimit(
				identifier,
				formatLimitTypeWithLockLevel(baseType, nextLevel),
				entry.Attempts,
				firstAttempt,
				&lockedUntil,
			)
			return RateLimitResult{
				Allowed:     false,
				RetryAfter:  lockoutDuration,
				Attempts:    entry.Attempts,
				MaxAttempts: maxAttempts,
			}
		}
		return rl.reserveIfRoom(identifier, now, entry.Attempts, pending, maxAttempts)
	}

	return rl.reserveIfRoom(identifier, now, 0, pending, maxAttempts)
}

// reserveIfRoom allows the attempt if stored plus in-flight attempts are
// under the limit. A refusal caused only by in-flight attempts is transient:
// nothing is persisted and RetryAfter is when the oldest one expires.
// Callers hold rl.mu.
func (rl *RateLimiter) reserveIfRoom(identifier string, now time.Time, stored, pending, maxAttempts int) RateLimitResult {
	if stored+pending < maxAttempts {
		return rl.reserve(identifier, now, maxAttempts)
	}
	retry := time.Second
	if res := rl.inFlight[identifier]; len(res) > 0 {
		if d := reservationTTL - now.Sub(res[0]); d > retry {
			retry = d
		}
	}
	return RateLimitResult{
		Allowed:     false,
		RetryAfter:  retry,
		Attempts:    stored + pending,
		MaxAttempts: maxAttempts,
	}
}

// RecordAttempt records a failed login attempt.
// It consumes the slot reserved by the matching CheckAuthRateLimit.
func (rl *RateLimiter) RecordAttempt(identifier, limitType string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	rl.takeReservation(identifier)

	entry, _ := rl.db.GetRateLimit(identifier)

	now := time.Now()
	if entry == nil {
		rl.db.UpsertRateLimit(identifier, limitType, 1, now, nil)
		return
	}

	baseType, lockLevel := parseLimitTypeAndLockLevel(entry.Type, limitType)
	firstAttempt, err := time.Parse(time.RFC3339, entry.FirstAttemptAt)
	if err != nil {
		firstAttempt = now
	}
	rl.db.UpsertRateLimit(
		identifier,
		formatLimitTypeWithLockLevel(baseType, lockLevel),
		entry.Attempts+1,
		firstAttempt,
		nil,
	)
}

// ResetLimit resets the rate limit for an identifier.
func (rl *RateLimiter) ResetLimit(identifier string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	delete(rl.inFlight, identifier)
	rl.db.DeleteRateLimit(identifier)
}

func parseLimitTypeAndLockLevel(storedType, fallback string) (string, int) {
	trimmed := strings.TrimSpace(storedType)
	if trimmed == "" {
		return fallback, 0
	}

	parts := strings.SplitN(trimmed, ":", 2)
	base := strings.TrimSpace(parts[0])
	if base == "" {
		base = fallback
	}
	if len(parts) == 1 {
		return base, 0
	}

	level, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil || level < 0 {
		return base, 0
	}
	return base, level
}

func formatLimitTypeWithLockLevel(base string, level int) string {
	if level <= 0 {
		return base
	}
	return fmt.Sprintf("%s:%d", base, level)
}

func nextLockoutLevel(current int) int {
	next := current + 1
	if next < 1 {
		next = 1
	}
	if next > len(lockoutSchedule) {
		next = len(lockoutSchedule)
	}
	return next
}

func lockoutDurationForLevel(level int) time.Duration {
	if level <= 1 {
		return lockoutSchedule[0]
	}
	if level > len(lockoutSchedule) {
		return lockoutSchedule[len(lockoutSchedule)-1]
	}
	return lockoutSchedule[level-1]
}
