package database

import (
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"sync"

	_ "modernc.org/sqlite"
)

// nullStringToPtr converts a sql.NullString to a *string (nil if not valid).
func nullStringToPtr(ns sql.NullString) *string {
	if ns.Valid {
		return &ns.String
	}
	return nil
}

// DB wraps the SQLite connection.
type DB struct {
	conn *sql.DB
	path string

	// OnAudit, if set, is invoked after each audit log row is written. The
	// server uses it to forward events to external sinks (SIEM). It must not
	// block; forwarding is handled asynchronously inside the callback.
	OnAudit func(AuditLogParams)

	// retentionStats holds the outcome of the most recent data retention run
	// (see retention.go). Guarded by retentionMu.
	retentionMu    sync.Mutex
	retentionStats RetentionStats

	backgroundAuditMu sync.Mutex
	backgroundAudits  map[backgroundAuditKey]backgroundAuditEntry
}

// Open opens the SQLite database at the given path, runs migrations, and returns a DB.
func Open(path string) (*DB, error) {
	// Ensure directory exists
	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			slog.Warn("Could not create database directory", "dir", dir, "error", err)
		}
	}

	// auto_vacuum=INCREMENTAL lets the retention job reclaim freed pages via
	// PRAGMA incremental_vacuum. It only takes effect on newly created
	// databases (or after a full VACUUM); on existing databases it is a no-op.
	// It must be set before the first table is created, hence in the DSN.
	dsn := fmt.Sprintf("%s?_pragma=auto_vacuum(INCREMENTAL)&_pragma=foreign_keys(1)&_pragma=journal_mode(wal)&_pragma=busy_timeout(5000)", path)
	conn, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	// SQLite is single-writer, but WAL allows concurrent readers.
	// Keep a small pool so reads (session/token checks) are not blocked by long sync writes.
	maxOpenConns := 8
	if raw := os.Getenv("CHUI_SQLITE_MAX_OPEN_CONNS"); raw != "" {
		if parsed, parseErr := strconv.Atoi(raw); parseErr == nil && parsed > 0 {
			maxOpenConns = parsed
		}
	}
	conn.SetMaxOpenConns(maxOpenConns)
	conn.SetMaxIdleConns(maxOpenConns)

	// Verify connection
	if err := conn.Ping(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}

	db := &DB{conn: conn, path: path}

	// Run migrations
	if err := db.runMigrations(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("migrations: %w", err)
	}

	slog.Info("Database initialized", "path", path)
	return db, nil
}

// Close closes the database connection.
func (db *DB) Close() error {
	return db.conn.Close()
}

// Conn returns the underlying sql.DB for advanced usage.
func (db *DB) Conn() *sql.DB {
	return db.conn
}
