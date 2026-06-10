package database

import (
	"fmt"
	"testing"
)

func insertHistoryAt(t *testing.T, db *DB, id, user, conn, query, status, createdAtExpr string) {
	t.Helper()
	_, err := db.conn.Exec(
		`INSERT INTO query_history (id, connection_id, clickhouse_user, query_text, status, elapsed_ms, rows_returned, created_at)
		 VALUES (?, ?, ?, ?, ?, 10, 5, `+createdAtExpr+`)`,
		id, conn, user, query, status,
	)
	if err != nil {
		t.Fatalf("insert query history: %v", err)
	}
}

func TestQueryHistory_CreateAndList(t *testing.T) {
	db := openTestDB(t)

	err := db.CreateQueryHistoryEntry(CreateQueryHistoryParams{
		ConnectionID: "conn-1",
		User:         "alice",
		QueryText:    "SELECT * FROM events",
		Status:       "success",
		ElapsedMS:    42,
		RowsReturned: 100,
	})
	if err != nil {
		t.Fatalf("CreateQueryHistoryEntry: %v", err)
	}

	entries, err := db.GetQueryHistory("alice", "conn-1", "", "", 50, 0)
	if err != nil {
		t.Fatalf("GetQueryHistory: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	e := entries[0]
	if e.QueryText != "SELECT * FROM events" || e.Status != "success" {
		t.Fatalf("unexpected entry: %+v", e)
	}
	if e.ElapsedMS == nil || *e.ElapsedMS != 42 || e.RowsReturned == nil || *e.RowsReturned != 100 {
		t.Fatalf("metrics not persisted: %+v", e)
	}
}

func TestQueryHistory_ScopedToUserAndConnection(t *testing.T) {
	db := openTestDB(t)

	insertHistoryAt(t, db, "h1", "alice", "conn-1", "SELECT 1", "success", "datetime('now', '-1 minute')")
	insertHistoryAt(t, db, "h2", "bob", "conn-1", "SELECT 2", "success", "datetime('now', '-2 minutes')")
	insertHistoryAt(t, db, "h3", "alice", "conn-2", "SELECT 3", "success", "datetime('now', '-3 minutes')")

	entries, err := db.GetQueryHistory("alice", "conn-1", "", "", 50, 0)
	if err != nil {
		t.Fatalf("GetQueryHistory: %v", err)
	}
	if len(entries) != 1 || entries[0].ID != "h1" {
		t.Fatalf("expected only alice@conn-1 entry, got %+v", entries)
	}
}

func TestQueryHistory_SearchAndStatusFilter(t *testing.T) {
	db := openTestDB(t)

	insertHistoryAt(t, db, "h1", "alice", "conn-1", "SELECT * FROM Events WHERE id = 1", "success", "datetime('now', '-1 minute')")
	insertHistoryAt(t, db, "h2", "alice", "conn-1", "SELECT count() FROM users", "error", "datetime('now', '-2 minutes')")

	entries, err := db.GetQueryHistory("alice", "conn-1", "events", "", 50, 0)
	if err != nil {
		t.Fatalf("GetQueryHistory search: %v", err)
	}
	if len(entries) != 1 || entries[0].ID != "h1" {
		t.Fatalf("case-insensitive search failed: %+v", entries)
	}

	entries, err = db.GetQueryHistory("alice", "conn-1", "", "error", 50, 0)
	if err != nil {
		t.Fatalf("GetQueryHistory status: %v", err)
	}
	if len(entries) != 1 || entries[0].ID != "h2" {
		t.Fatalf("status filter failed: %+v", entries)
	}
}

func TestQueryHistory_SearchEscapesLikeWildcards(t *testing.T) {
	db := openTestDB(t)

	insertHistoryAt(t, db, "h1", "alice", "conn-1", "SELECT user_id FROM t", "success", "datetime('now', '-1 minute')")
	insertHistoryAt(t, db, "h2", "alice", "conn-1", "SELECT userXid FROM t", "success", "datetime('now', '-2 minutes')")
	insertHistoryAt(t, db, "h3", "alice", "conn-1", "SELECT '100%' FROM t", "success", "datetime('now', '-3 minutes')")

	// _ must match literally, not as a single-char wildcard.
	entries, err := db.GetQueryHistory("alice", "conn-1", "user_id", "", 50, 0)
	if err != nil {
		t.Fatalf("GetQueryHistory underscore: %v", err)
	}
	if len(entries) != 1 || entries[0].ID != "h1" {
		t.Fatalf("underscore must be literal, got %+v", entries)
	}

	// % must match literally, not as any-string wildcard.
	entries, err = db.GetQueryHistory("alice", "conn-1", "100%", "", 50, 0)
	if err != nil {
		t.Fatalf("GetQueryHistory percent: %v", err)
	}
	if len(entries) != 1 || entries[0].ID != "h3" {
		t.Fatalf("percent must be literal, got %+v", entries)
	}
}

func TestQueryHistory_OrderAndPagination(t *testing.T) {
	db := openTestDB(t)

	insertHistoryAt(t, db, "old", "alice", "conn-1", "SELECT 1", "success", "datetime('now', '-2 hours')")
	insertHistoryAt(t, db, "mid", "alice", "conn-1", "SELECT 2", "success", "datetime('now', '-1 hour')")
	insertHistoryAt(t, db, "new", "alice", "conn-1", "SELECT 3", "success", "datetime('now', '-1 minute')")

	entries, err := db.GetQueryHistory("alice", "conn-1", "", "", 2, 0)
	if err != nil {
		t.Fatalf("GetQueryHistory page 1: %v", err)
	}
	if len(entries) != 2 || entries[0].ID != "new" || entries[1].ID != "mid" {
		t.Fatalf("expected [new, mid], got %+v", entries)
	}

	entries, err = db.GetQueryHistory("alice", "conn-1", "", "", 2, 2)
	if err != nil {
		t.Fatalf("GetQueryHistory page 2: %v", err)
	}
	if len(entries) != 1 || entries[0].ID != "old" {
		t.Fatalf("expected [old], got %+v", entries)
	}
}

func TestQueryHistory_PrunesBeyondRetention(t *testing.T) {
	db := openTestDB(t)

	// Pre-seed retention-many old entries with distinct timestamps, then one
	// more via the public API to trigger the prune.
	for i := 0; i < queryHistoryRetention; i++ {
		insertHistoryAt(t, db, fmt.Sprintf("h%04d", i), "alice", "conn-1",
			fmt.Sprintf("SELECT %d", i), "success",
			fmt.Sprintf("datetime('now', '-%d seconds')", queryHistoryRetention-i+10))
	}

	err := db.CreateQueryHistoryEntry(CreateQueryHistoryParams{
		ConnectionID: "conn-1",
		User:         "alice",
		QueryText:    "SELECT 'newest'",
		Status:       "success",
	})
	if err != nil {
		t.Fatalf("CreateQueryHistoryEntry: %v", err)
	}

	var count int
	if err := db.conn.QueryRow(
		`SELECT COUNT(*) FROM query_history WHERE clickhouse_user = 'alice'`,
	).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != queryHistoryRetention {
		t.Fatalf("expected %d entries after prune, got %d", queryHistoryRetention, count)
	}

	// The oldest entry must be the one that was pruned.
	entries, err := db.GetQueryHistory("alice", "conn-1", "", "", 1, 0)
	if err != nil {
		t.Fatalf("GetQueryHistory: %v", err)
	}
	if len(entries) != 1 || entries[0].QueryText != "SELECT 'newest'" {
		t.Fatalf("expected newest entry first, got %+v", entries)
	}
	var oldest int
	if err := db.conn.QueryRow(
		`SELECT COUNT(*) FROM query_history WHERE id = 'h0000'`,
	).Scan(&oldest); err != nil {
		t.Fatalf("oldest count: %v", err)
	}
	if oldest != 0 {
		t.Fatalf("expected oldest entry pruned")
	}
}

func TestQueryHistory_DeleteAndClear(t *testing.T) {
	db := openTestDB(t)

	insertHistoryAt(t, db, "h1", "alice", "conn-1", "SELECT 1", "success", "datetime('now', '-1 minute')")
	insertHistoryAt(t, db, "h2", "alice", "conn-1", "SELECT 2", "success", "datetime('now', '-2 minutes')")
	insertHistoryAt(t, db, "h3", "bob", "conn-1", "SELECT 3", "success", "datetime('now', '-3 minutes')")

	// Deleting someone else's entry is a silent no-op.
	if err := db.DeleteQueryHistoryEntry("h3", "alice", "conn-1"); err != nil {
		t.Fatalf("DeleteQueryHistoryEntry foreign: %v", err)
	}
	if entries, _ := db.GetQueryHistory("bob", "conn-1", "", "", 50, 0); len(entries) != 1 {
		t.Fatalf("bob's entry should survive alice's delete")
	}

	// Deleting own entry from a different connection is also a no-op.
	if err := db.DeleteQueryHistoryEntry("h1", "alice", "conn-2"); err != nil {
		t.Fatalf("DeleteQueryHistoryEntry wrong conn: %v", err)
	}
	if entries, _ := db.GetQueryHistory("alice", "conn-1", "", "", 50, 0); len(entries) != 2 {
		t.Fatalf("delete must be connection-scoped")
	}

	if err := db.DeleteQueryHistoryEntry("h1", "alice", "conn-1"); err != nil {
		t.Fatalf("DeleteQueryHistoryEntry: %v", err)
	}
	if entries, _ := db.GetQueryHistory("alice", "conn-1", "", "", 50, 0); len(entries) != 1 || entries[0].ID != "h2" {
		t.Fatalf("expected only h2 left, got %+v", entries)
	}

	if err := db.ClearQueryHistory("alice", "conn-1"); err != nil {
		t.Fatalf("ClearQueryHistory: %v", err)
	}
	if entries, _ := db.GetQueryHistory("alice", "conn-1", "", "", 50, 0); len(entries) != 0 {
		t.Fatalf("expected empty history after clear, got %+v", entries)
	}
	if entries, _ := db.GetQueryHistory("bob", "conn-1", "", "", 50, 0); len(entries) != 1 {
		t.Fatalf("clear must not touch other users")
	}
}
