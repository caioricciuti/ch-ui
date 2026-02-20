package database

import (
	"database/sql"
	"fmt"
	"time"
)

// RateLimitEntry represents a rate limit record.
type RateLimitEntry struct {
	Identifier     string  `json:"identifier"`
	Type           string  `json:"type"`
	Attempts       int     `json:"attempts"`
	FirstAttemptAt string  `json:"first_attempt_at"`
	LockedUntil    *string `json:"locked_until"`
	CreatedAt      string  `json:"created_at"`
	UpdatedAt      string  `json:"updated_at"`
}

// GetRateLimit retrieves a rate limit entry by identifier.
func (db *DB) GetRateLimit(identifier string) (*RateLimitEntry, error) {
	row := db.conn.QueryRow(
		"SELECT identifier, type, attempts, first_attempt_at, locked_until, created_at, updated_at FROM rate_limits WHERE identifier = ?",
		identifier,
	)

	var r RateLimitEntry
	var lockedUntil sql.NullString

	err := row.Scan(
		&r.Identifier, &r.Type, &r.Attempts,
		&r.FirstAttemptAt, &lockedUntil,
		&r.CreatedAt, &r.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get rate limit: %w", err)
	}

	r.LockedUntil = nullStringToPtr(lockedUntil)
	return &r, nil
}

// UpsertRateLimit inserts or updates a rate limit entry.
func (db *DB) UpsertRateLimit(identifier, limitType string, attempts int, firstAttempt time.Time, lockedUntil *time.Time) error {
	now := time.Now().UTC().Format(time.RFC3339)
	firstAttemptStr := firstAttempt.UTC().Format(time.RFC3339)

	var lockedUntilVal interface{}
	if lockedUntil != nil {
		lockedUntilVal = lockedUntil.UTC().Format(time.RFC3339)
	}

	_, err := db.conn.Exec(`
		INSERT INTO rate_limits (identifier, type, attempts, first_attempt_at, locked_until, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(identifier) DO UPDATE SET
			attempts = excluded.attempts,
			first_attempt_at = excluded.first_attempt_at,
			locked_until = excluded.locked_until,
			updated_at = excluded.updated_at
	`, identifier, limitType, attempts, firstAttemptStr, lockedUntilVal, now, now)
	if err != nil {
		return fmt.Errorf("upsert rate limit: %w", err)
	}
	return nil
}

// DeleteRateLimit deletes a rate limit entry by identifier.
func (db *DB) DeleteRateLimit(identifier string) error {
	_, err := db.conn.Exec("DELETE FROM rate_limits WHERE identifier = ?", identifier)
	if err != nil {
		return fmt.Errorf("delete rate limit: %w", err)
	}
	return nil
}

// CleanupExpiredRateLimits removes rate limit entries that have expired based on the window.
// Returns the number of entries deleted.
func (db *DB) CleanupExpiredRateLimits(windowMs int64) (int64, error) {
	cutoff := time.Now().UTC().Add(-time.Duration(windowMs) * time.Millisecond).Format(time.RFC3339)
	now := time.Now().UTC().Format(time.RFC3339)

	result, err := db.conn.Exec(`
		DELETE FROM rate_limits
		WHERE (first_attempt_at < ? AND (locked_until IS NULL OR locked_until < ?))
		   OR (locked_until IS NOT NULL AND locked_until < ?)
	`, cutoff, now, now)
	if err != nil {
		return 0, fmt.Errorf("cleanup expired rate limits: %w", err)
	}

	count, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("get rows affected: %w", err)
	}
	return count, nil
}
