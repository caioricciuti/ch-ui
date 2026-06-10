package database

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// queryHistoryRetention is the number of entries kept per user+connection.
const queryHistoryRetention = 500

// QueryHistoryEntry represents one recorded query execution.
type QueryHistoryEntry struct {
	ID           string  `json:"id"`
	ConnectionID *string `json:"connection_id"`
	User         string  `json:"clickhouse_user"`
	QueryText    string  `json:"query_text"`
	Status       string  `json:"status"` // "success" | "error"
	ErrorMessage *string `json:"error_message"`
	ElapsedMS    *int64  `json:"elapsed_ms"`
	RowsReturned *int64  `json:"rows_returned"`
	CreatedAt    string  `json:"created_at"`
}

// CreateQueryHistoryParams holds parameters for recording a query execution.
type CreateQueryHistoryParams struct {
	ConnectionID string
	User         string
	QueryText    string
	Status       string
	ErrorMessage string
	ElapsedMS    int64
	RowsReturned int64
}

// CreateQueryHistoryEntry records a query execution and prunes old entries
// beyond the per-user retention window.
func (db *DB) CreateQueryHistoryEntry(params CreateQueryHistoryParams) error {
	id := uuid.NewString()
	// Millisecond-precision timestamp: CURRENT_TIMESTAMP is second-resolution,
	// which makes ordering (and prune victims) unstable for rapid runs.
	_, err := db.conn.Exec(
		`INSERT INTO query_history (id, connection_id, clickhouse_user, query_text, status, error_message, elapsed_ms, rows_returned, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, strftime('%Y-%m-%d %H:%M:%f', 'now'))`,
		id,
		nilIfEmpty(params.ConnectionID),
		params.User,
		params.QueryText,
		params.Status,
		nilIfEmpty(params.ErrorMessage),
		params.ElapsedMS,
		params.RowsReturned,
	)
	if err != nil {
		return fmt.Errorf("create query history entry: %w", err)
	}

	_, err = db.conn.Exec(
		`DELETE FROM query_history
		 WHERE clickhouse_user = ? AND COALESCE(connection_id, '') = COALESCE(?, '')
		   AND id NOT IN (
		     SELECT id FROM query_history
		     WHERE clickhouse_user = ? AND COALESCE(connection_id, '') = COALESCE(?, '')
		     ORDER BY created_at DESC, id DESC LIMIT ?
		   )`,
		params.User, nilIfEmpty(params.ConnectionID),
		params.User, nilIfEmpty(params.ConnectionID),
		queryHistoryRetention,
	)
	if err != nil {
		return fmt.Errorf("prune query history: %w", err)
	}
	return nil
}

// GetQueryHistory lists a user's query history on a connection, most recent
// first. status filters to "success"/"error" when non-empty; search matches a
// case-insensitive substring of the query text.
func (db *DB) GetQueryHistory(user, connectionID, search, status string, limit, offset int) ([]QueryHistoryEntry, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	where := []string{
		"clickhouse_user = ?",
		"COALESCE(connection_id, '') = COALESCE(?, '')",
	}
	args := []any{user, nilIfEmpty(connectionID)}

	if status == "success" || status == "error" || status == "cancelled" {
		where = append(where, "status = ?")
		args = append(args, status)
	}
	if s := strings.TrimSpace(strings.ToLower(search)); s != "" {
		// Escape LIKE wildcards so searching for SQL identifiers like
		// "user_id" or literals containing % matches literally.
		s = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
		where = append(where, `lower(query_text) LIKE ? ESCAPE '\'`)
		args = append(args, "%"+s+"%")
	}

	query := fmt.Sprintf(
		`SELECT id, connection_id, clickhouse_user, query_text, status, error_message, elapsed_ms, rows_returned, created_at
		 FROM query_history WHERE %s
		 ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?`,
		strings.Join(where, " AND "),
	)
	args = append(args, limit, offset)

	rows, err := db.conn.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("get query history: %w", err)
	}
	defer rows.Close()

	var entries []QueryHistoryEntry
	for rows.Next() {
		var e QueryHistoryEntry
		var connID, errMsg sql.NullString
		var elapsed, rowsReturned sql.NullInt64
		if err := rows.Scan(&e.ID, &connID, &e.User, &e.QueryText, &e.Status, &errMsg, &elapsed, &rowsReturned, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan query history entry: %w", err)
		}
		e.ConnectionID = nullStringToPtr(connID)
		e.ErrorMessage = nullStringToPtr(errMsg)
		if elapsed.Valid {
			v := elapsed.Int64
			e.ElapsedMS = &v
		}
		if rowsReturned.Valid {
			v := rowsReturned.Int64
			e.RowsReturned = &v
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate query history rows: %w", err)
	}
	return entries, nil
}

// DeleteQueryHistoryEntry deletes one entry if it belongs to the user on the
// given connection (same scoping as List and Clear).
func (db *DB) DeleteQueryHistoryEntry(id, user, connectionID string) error {
	_, err := db.conn.Exec(
		`DELETE FROM query_history WHERE id = ? AND clickhouse_user = ? AND COALESCE(connection_id, '') = COALESCE(?, '')`,
		id, user, nilIfEmpty(connectionID),
	)
	if err != nil {
		return fmt.Errorf("delete query history entry: %w", err)
	}
	return nil
}

// ClearQueryHistory deletes all of a user's history on a connection.
func (db *DB) ClearQueryHistory(user, connectionID string) error {
	_, err := db.conn.Exec(
		`DELETE FROM query_history WHERE clickhouse_user = ? AND COALESCE(connection_id, '') = COALESCE(?, '')`,
		user, nilIfEmpty(connectionID),
	)
	if err != nil {
		return fmt.Errorf("clear query history: %w", err)
	}
	return nil
}
