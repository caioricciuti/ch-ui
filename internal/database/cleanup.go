package database

import (
	"log/slog"
	"time"
)

// StartCleanupJobs launches background goroutines that periodically clean up
// expired sessions and expired rate limits.
func (db *DB) StartCleanupJobs() {
	slog.Info("Starting periodic cleanup jobs...")

	// Cleanup expired sessions (every 1 hour)
	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			db.cleanupExpiredSessions()
		}
	}()

	// Cleanup expired rate limits (every 10 minutes)
	go func() {
		ticker := time.NewTicker(10 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			db.cleanupRateLimits()
		}
	}()

	slog.Info("Cleanup jobs scheduled")
}

// cleanupExpiredSessions removes sessions that have passed their expiration time.
func (db *DB) cleanupExpiredSessions() {
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := db.conn.Exec("DELETE FROM sessions WHERE expires_at < ?", now)
	if err != nil {
		slog.Error("Failed to cleanup expired sessions", "error", err)
		return
	}
	if affected, _ := result.RowsAffected(); affected > 0 {
		slog.Info("Cleaned up expired sessions", "count", affected)
	}
}

// cleanupRateLimits removes expired rate limit entries (15-minute window).
func (db *DB) cleanupRateLimits() {
	const windowMs int64 = 15 * 60 * 1000 // 15 minutes
	cleaned, err := db.CleanupExpiredRateLimits(windowMs)
	if err != nil {
		slog.Error("Failed to cleanup rate limits", "error", err)
		return
	}
	if cleaned > 0 {
		slog.Info("Cleaned up expired rate limits", "count", cleaned)
	}
}
