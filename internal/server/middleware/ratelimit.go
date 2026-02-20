package middleware

import (
	"time"

	"github.com/caioricciuti/ch-ui/internal/database"
)

// RateLimiter provides rate limiting backed by SQLite.
type RateLimiter struct {
	db *database.DB
}

// NewRateLimiter creates a new rate limiter.
func NewRateLimiter(db *database.DB) *RateLimiter {
	return &RateLimiter{db: db}
}

// RateLimitResult holds the result of a rate limit check.
type RateLimitResult struct {
	Allowed     bool
	RetryAfter  time.Duration
	Attempts    int
	MaxAttempts int
}

// CheckAuthRateLimit checks if a login attempt is allowed.
// Returns whether the attempt is allowed, and if not, how long to wait.
func (rl *RateLimiter) CheckAuthRateLimit(identifier, limitType string, maxAttempts int, windowDuration, lockoutDuration time.Duration) RateLimitResult {
	entry, err := rl.db.GetRateLimit(identifier)
	if err != nil {
		// On error, allow the request
		return RateLimitResult{Allowed: true, MaxAttempts: maxAttempts}
	}

	now := time.Now()

	if entry != nil {
		// Check if locked out
		if entry.LockedUntil != nil {
			lockedUntil, err := time.Parse(time.RFC3339, *entry.LockedUntil)
			if err == nil && now.Before(lockedUntil) {
				return RateLimitResult{
					Allowed:     false,
					RetryAfter:  time.Until(lockedUntil),
					Attempts:    entry.Attempts,
					MaxAttempts: maxAttempts,
				}
			}
			// Lock expired, reset
			rl.db.DeleteRateLimit(identifier)
			return RateLimitResult{Allowed: true, MaxAttempts: maxAttempts}
		}

		// Check if window expired
		firstAttempt, err := time.Parse(time.RFC3339, entry.FirstAttemptAt)
		if err == nil && now.Sub(firstAttempt) > windowDuration {
			// Window expired, reset
			rl.db.DeleteRateLimit(identifier)
			return RateLimitResult{Allowed: true, MaxAttempts: maxAttempts}
		}

		// Check attempts
		if entry.Attempts >= maxAttempts {
			// Lock out
			lockedUntil := now.Add(lockoutDuration)
			rl.db.UpsertRateLimit(identifier, limitType, entry.Attempts, firstAttempt, &lockedUntil)
			return RateLimitResult{
				Allowed:     false,
				RetryAfter:  lockoutDuration,
				Attempts:    entry.Attempts,
				MaxAttempts: maxAttempts,
			}
		}
	}

	return RateLimitResult{Allowed: true, MaxAttempts: maxAttempts}
}

// RecordAttempt records a failed login attempt.
func (rl *RateLimiter) RecordAttempt(identifier, limitType string) {
	entry, _ := rl.db.GetRateLimit(identifier)

	now := time.Now()
	if entry == nil {
		rl.db.UpsertRateLimit(identifier, limitType, 1, now, nil)
		return
	}

	firstAttempt, _ := time.Parse(time.RFC3339, entry.FirstAttemptAt)
	rl.db.UpsertRateLimit(identifier, limitType, entry.Attempts+1, firstAttempt, nil)
}

// ResetLimit resets the rate limit for an identifier.
func (rl *RateLimiter) ResetLimit(identifier string) {
	rl.db.DeleteRateLimit(identifier)
}
