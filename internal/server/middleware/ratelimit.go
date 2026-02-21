package middleware

import (
	"fmt"
	"strconv"
	"strings"
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

var lockoutSchedule = []time.Duration{
	3 * time.Minute,
	5 * time.Minute,
	10 * time.Minute,
}

// CheckAuthRateLimit checks if a login attempt is allowed.
// Returns whether the attempt is allowed, and if not, how long to wait.
func (rl *RateLimiter) CheckAuthRateLimit(identifier, limitType string, maxAttempts int, windowDuration time.Duration) RateLimitResult {
	entry, err := rl.db.GetRateLimit(identifier)
	if err != nil {
		// On error, allow the request
		return RateLimitResult{Allowed: true, MaxAttempts: maxAttempts}
	}

	now := time.Now()

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
			return RateLimitResult{Allowed: true, MaxAttempts: maxAttempts}
		}

		// Check if window expired
		firstAttempt, err := time.Parse(time.RFC3339, entry.FirstAttemptAt)
		if err == nil && now.Sub(firstAttempt) > windowDuration {
			// Window expired, reset
			// Escalation is reset once the attempts window is clean.
			rl.db.UpsertRateLimit(identifier, limitType, 0, now, nil)
			return RateLimitResult{Allowed: true, MaxAttempts: maxAttempts}
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
