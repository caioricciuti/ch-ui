package governance

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/google/uuid"
)

// nullStringToPtr converts a sql.NullString to a *string (nil if not valid).
func nullStringToPtr(ns sql.NullString) *string {
	if ns.Valid {
		return &ns.String
	}
	return nil
}

// nullIntToPtr converts a sql.NullInt64 to an *int (nil if not valid).
func nullIntToPtr(ni sql.NullInt64) *int {
	if ni.Valid {
		v := int(ni.Int64)
		return &v
	}
	return nil
}

// ptrToNullString converts a *string to a sql.NullString.
func ptrToNullString(s *string) sql.NullString {
	if s == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: *s, Valid: true}
}

// Store provides all governance CRUD operations against SQLite.
type Store struct {
	db *database.DB
}

// NewStore creates a new governance Store.
func NewStore(db *database.DB) *Store {
	return &Store{db: db}
}

// Ensure Store satisfies the PolicyStore interface used by the policy engine.
var _ PolicyStore = (*Store)(nil)

// conn returns the underlying *sql.DB for running queries.
func (s *Store) conn() *sql.DB {
	return s.db.Conn()
}

// ── Sync State ───────────────────────────────────────────────────────────────

// GetSyncStates returns all sync states for a connection.
func (s *Store) GetSyncStates(connectionID string) ([]SyncState, error) {
	rows, err := s.conn().Query(
		`SELECT id, connection_id, sync_type, last_synced_at, watermark, status, last_error, row_count, created_at, updated_at
		 FROM gov_sync_state WHERE connection_id = ? ORDER BY sync_type`, connectionID,
	)
	if err != nil {
		return nil, fmt.Errorf("get sync states: %w", err)
	}
	defer rows.Close()

	var results []SyncState
	for rows.Next() {
		var ss SyncState
		var lastSynced, watermark, lastError sql.NullString
		if err := rows.Scan(&ss.ID, &ss.ConnectionID, &ss.SyncType, &lastSynced, &watermark, &ss.Status, &lastError, &ss.RowCount, &ss.CreatedAt, &ss.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan sync state: %w", err)
		}
		ss.LastSyncedAt = nullStringToPtr(lastSynced)
		ss.Watermark = nullStringToPtr(watermark)
		ss.LastError = nullStringToPtr(lastError)
		results = append(results, ss)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sync state rows: %w", err)
	}
	return results, nil
}

// GetSyncState returns a specific sync state for a connection and sync type.
func (s *Store) GetSyncState(connectionID string, syncType string) (*SyncState, error) {
	row := s.conn().QueryRow(
		`SELECT id, connection_id, sync_type, last_synced_at, watermark, status, last_error, row_count, created_at, updated_at
		 FROM gov_sync_state WHERE connection_id = ? AND sync_type = ?`, connectionID, syncType,
	)

	var ss SyncState
	var lastSynced, watermark, lastError sql.NullString
	err := row.Scan(&ss.ID, &ss.ConnectionID, &ss.SyncType, &lastSynced, &watermark, &ss.Status, &lastError, &ss.RowCount, &ss.CreatedAt, &ss.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get sync state: %w", err)
	}
	ss.LastSyncedAt = nullStringToPtr(lastSynced)
	ss.Watermark = nullStringToPtr(watermark)
	ss.LastError = nullStringToPtr(lastError)
	return &ss, nil
}

// UpsertSyncState inserts or updates a sync state record.
func (s *Store) UpsertSyncState(connectionID string, syncType string, status string, watermark *string, lastError *string, rowCount int) error {
	now := time.Now().UTC().Format(time.RFC3339)
	id := uuid.NewString()

	_, err := s.conn().Exec(
		`INSERT INTO gov_sync_state (id, connection_id, sync_type, last_synced_at, watermark, status, last_error, row_count, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(connection_id, sync_type) DO UPDATE SET
		   last_synced_at = excluded.last_synced_at,
		   watermark = COALESCE(excluded.watermark, gov_sync_state.watermark),
		   status = excluded.status,
		   last_error = excluded.last_error,
		   row_count = excluded.row_count,
		   updated_at = excluded.updated_at`,
		id, connectionID, syncType, now, ptrToNullString(watermark), status, ptrToNullString(lastError), rowCount, now, now,
	)
	if err != nil {
		return fmt.Errorf("upsert sync state: %w", err)
	}
	return nil
}

// UpdateSyncWatermark updates only the watermark for a specific sync state.
func (s *Store) UpdateSyncWatermark(connectionID string, syncType string, watermark string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.conn().Exec(
		`UPDATE gov_sync_state SET watermark = ?, updated_at = ? WHERE connection_id = ? AND sync_type = ?`,
		watermark, now, connectionID, syncType,
	)
	if err != nil {
		return fmt.Errorf("update sync watermark: %w", err)
	}
	return nil
}

// ── Databases ────────────────────────────────────────────────────────────────

// GetDatabases returns all databases for a connection.
func (s *Store) GetDatabases(connectionID string) ([]GovDatabase, error) {
	rows, err := s.conn().Query(
		`SELECT id, connection_id, name, engine, first_seen, last_updated, is_deleted
		 FROM gov_databases WHERE connection_id = ? ORDER BY name`, connectionID,
	)
	if err != nil {
		return nil, fmt.Errorf("get databases: %w", err)
	}
	defer rows.Close()

	var results []GovDatabase
	for rows.Next() {
		var d GovDatabase
		if err := rows.Scan(&d.ID, &d.ConnectionID, &d.Name, &d.Engine, &d.FirstSeen, &d.LastUpdated, &d.IsDeleted); err != nil {
			return nil, fmt.Errorf("scan database: %w", err)
		}
		results = append(results, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate database rows: %w", err)
	}
	return results, nil
}

// UpsertDatabase inserts or updates a database record from a GovDatabase struct.
func (s *Store) UpsertDatabase(d GovDatabase) error {
	isDeleted := 0
	if d.IsDeleted {
		isDeleted = 1
	}

	_, err := s.conn().Exec(
		`INSERT INTO gov_databases (id, connection_id, name, engine, first_seen, last_updated, is_deleted)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(connection_id, name) DO UPDATE SET
		   engine = excluded.engine,
		   last_updated = excluded.last_updated,
		   is_deleted = excluded.is_deleted`,
		d.ID, d.ConnectionID, d.Name, d.Engine, d.FirstSeen, d.LastUpdated, isDeleted,
	)
	if err != nil {
		return fmt.Errorf("upsert database: %w", err)
	}
	return nil
}

// MarkDatabaseDeleted soft-deletes a database record.
func (s *Store) MarkDatabaseDeleted(connectionID, name string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.conn().Exec(
		`UPDATE gov_databases SET is_deleted = 1, last_updated = ? WHERE connection_id = ? AND name = ?`,
		now, connectionID, name,
	)
	if err != nil {
		return fmt.Errorf("mark database deleted: %w", err)
	}
	return nil
}

// ── Tables ───────────────────────────────────────────────────────────────────

// GetTables returns all non-deleted tables for a connection.
func (s *Store) GetTables(connectionID string) ([]GovTable, error) {
	rows, err := s.conn().Query(
		`SELECT id, connection_id, database_name, table_name, engine, table_uuid, total_rows, total_bytes, partition_count, first_seen, last_updated, is_deleted
		 FROM gov_tables WHERE connection_id = ? AND is_deleted = 0 ORDER BY database_name, table_name`, connectionID,
	)
	if err != nil {
		return nil, fmt.Errorf("get tables: %w", err)
	}
	defer rows.Close()

	return scanTables(rows)
}

// GetTablesByDatabase returns all non-deleted tables for a specific database.
func (s *Store) GetTablesByDatabase(connectionID, databaseName string) ([]GovTable, error) {
	rows, err := s.conn().Query(
		`SELECT id, connection_id, database_name, table_name, engine, table_uuid, total_rows, total_bytes, partition_count, first_seen, last_updated, is_deleted
		 FROM gov_tables WHERE connection_id = ? AND database_name = ? AND is_deleted = 0 ORDER BY table_name`,
		connectionID, databaseName,
	)
	if err != nil {
		return nil, fmt.Errorf("get tables by database: %w", err)
	}
	defer rows.Close()

	return scanTables(rows)
}

// GetTableByName returns a single table by connection, database, and table name.
func (s *Store) GetTableByName(connectionID, dbName, tableName string) (*GovTable, error) {
	row := s.conn().QueryRow(
		`SELECT id, connection_id, database_name, table_name, engine, table_uuid, total_rows, total_bytes, partition_count, first_seen, last_updated, is_deleted
		 FROM gov_tables WHERE connection_id = ? AND database_name = ? AND table_name = ?`,
		connectionID, dbName, tableName,
	)

	var t GovTable
	err := row.Scan(&t.ID, &t.ConnectionID, &t.DatabaseName, &t.TableName, &t.Engine, &t.TableUUID, &t.TotalRows, &t.TotalBytes, &t.PartitionCount, &t.FirstSeen, &t.LastUpdated, &t.IsDeleted)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get table by name: %w", err)
	}
	return &t, nil
}

// UpsertTable inserts or updates a table record from a GovTable struct.
func (s *Store) UpsertTable(t GovTable) error {
	isDeleted := 0
	if t.IsDeleted {
		isDeleted = 1
	}

	_, err := s.conn().Exec(
		`INSERT INTO gov_tables (id, connection_id, database_name, table_name, engine, table_uuid, total_rows, total_bytes, partition_count, first_seen, last_updated, is_deleted)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(connection_id, database_name, table_name) DO UPDATE SET
		   engine = excluded.engine,
		   table_uuid = excluded.table_uuid,
		   total_rows = excluded.total_rows,
		   total_bytes = excluded.total_bytes,
		   partition_count = excluded.partition_count,
		   last_updated = excluded.last_updated,
		   is_deleted = excluded.is_deleted`,
		t.ID, t.ConnectionID, t.DatabaseName, t.TableName, t.Engine, t.TableUUID,
		t.TotalRows, t.TotalBytes, t.PartitionCount, t.FirstSeen, t.LastUpdated, isDeleted,
	)
	if err != nil {
		return fmt.Errorf("upsert table: %w", err)
	}
	return nil
}

// MarkTableDeleted soft-deletes a table record.
func (s *Store) MarkTableDeleted(connectionID, dbName, tableName string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.conn().Exec(
		`UPDATE gov_tables SET is_deleted = 1, last_updated = ? WHERE connection_id = ? AND database_name = ? AND table_name = ?`,
		now, connectionID, dbName, tableName,
	)
	if err != nil {
		return fmt.Errorf("mark table deleted: %w", err)
	}
	return nil
}

func scanTables(rows *sql.Rows) ([]GovTable, error) {
	var results []GovTable
	for rows.Next() {
		var t GovTable
		if err := rows.Scan(&t.ID, &t.ConnectionID, &t.DatabaseName, &t.TableName, &t.Engine, &t.TableUUID, &t.TotalRows, &t.TotalBytes, &t.PartitionCount, &t.FirstSeen, &t.LastUpdated, &t.IsDeleted); err != nil {
			return nil, fmt.Errorf("scan table: %w", err)
		}
		results = append(results, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate table rows: %w", err)
	}
	return results, nil
}

// ── Columns ──────────────────────────────────────────────────────────────────

// GetColumns returns columns for a connection, optionally filtered by database and table.
// If dbName and tableName are empty strings, all non-deleted columns for the connection are returned.
func (s *Store) GetColumns(connectionID, dbName, tableName string) ([]GovColumn, error) {
	var query string
	var args []interface{}

	if dbName == "" && tableName == "" {
		query = `SELECT id, connection_id, database_name, table_name, column_name, column_type, column_position, default_kind, default_expression, comment, first_seen, last_updated, is_deleted
			 FROM gov_columns WHERE connection_id = ? AND is_deleted = 0 ORDER BY database_name, table_name, column_position`
		args = []interface{}{connectionID}
	} else {
		query = `SELECT id, connection_id, database_name, table_name, column_name, column_type, column_position, default_kind, default_expression, comment, first_seen, last_updated, is_deleted
			 FROM gov_columns WHERE connection_id = ? AND database_name = ? AND table_name = ? AND is_deleted = 0 ORDER BY column_position`
		args = []interface{}{connectionID, dbName, tableName}
	}

	rows, err := s.conn().Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("get columns: %w", err)
	}
	defer rows.Close()

	var results []GovColumn
	for rows.Next() {
		var c GovColumn
		var defaultKind, defaultExpr, comment sql.NullString
		if err := rows.Scan(&c.ID, &c.ConnectionID, &c.DatabaseName, &c.TableName, &c.ColumnName, &c.ColumnType, &c.ColumnPosition, &defaultKind, &defaultExpr, &comment, &c.FirstSeen, &c.LastUpdated, &c.IsDeleted); err != nil {
			return nil, fmt.Errorf("scan column: %w", err)
		}
		c.DefaultKind = nullStringToPtr(defaultKind)
		c.DefaultExpression = nullStringToPtr(defaultExpr)
		c.Comment = nullStringToPtr(comment)
		results = append(results, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate column rows: %w", err)
	}
	return results, nil
}

// UpsertColumn inserts or updates a column record from a GovColumn struct.
func (s *Store) UpsertColumn(c GovColumn) error {
	isDeleted := 0
	if c.IsDeleted {
		isDeleted = 1
	}

	_, err := s.conn().Exec(
		`INSERT INTO gov_columns (id, connection_id, database_name, table_name, column_name, column_type, column_position, default_kind, default_expression, comment, first_seen, last_updated, is_deleted)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(connection_id, database_name, table_name, column_name) DO UPDATE SET
		   column_type = excluded.column_type,
		   column_position = excluded.column_position,
		   default_kind = excluded.default_kind,
		   default_expression = excluded.default_expression,
		   comment = excluded.comment,
		   last_updated = excluded.last_updated,
		   is_deleted = excluded.is_deleted`,
		c.ID, c.ConnectionID, c.DatabaseName, c.TableName, c.ColumnName, c.ColumnType, c.ColumnPosition,
		ptrToNullString(c.DefaultKind), ptrToNullString(c.DefaultExpression), ptrToNullString(c.Comment),
		c.FirstSeen, c.LastUpdated, isDeleted,
	)
	if err != nil {
		return fmt.Errorf("upsert column: %w", err)
	}
	return nil
}

// MarkColumnDeleted soft-deletes a column record.
func (s *Store) MarkColumnDeleted(connectionID, dbName, tableName, colName string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.conn().Exec(
		`UPDATE gov_columns SET is_deleted = 1, last_updated = ? WHERE connection_id = ? AND database_name = ? AND table_name = ? AND column_name = ?`,
		now, connectionID, dbName, tableName, colName,
	)
	if err != nil {
		return fmt.Errorf("mark column deleted: %w", err)
	}
	return nil
}

// ── Schema Changes ───────────────────────────────────────────────────────────

// InsertSchemaChange inserts a schema change record from a SchemaChange struct.
func (s *Store) InsertSchemaChange(sc SchemaChange) error {
	_, err := s.conn().Exec(
		`INSERT INTO gov_schema_changes (id, connection_id, change_type, database_name, table_name, column_name, old_value, new_value, detected_at, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sc.ID, sc.ConnectionID, sc.ChangeType, sc.DatabaseName, sc.TableName, sc.ColumnName, sc.OldValue, sc.NewValue, sc.DetectedAt, sc.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert schema change: %w", err)
	}
	return nil
}

// CreateSchemaChange creates a new schema change record with auto-generated ID and timestamps.
func (s *Store) CreateSchemaChange(connectionID string, changeType SchemaChangeType, dbName, tableName, colName, oldVal, newVal string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	id := uuid.NewString()

	_, err := s.conn().Exec(
		`INSERT INTO gov_schema_changes (id, connection_id, change_type, database_name, table_name, column_name, old_value, new_value, detected_at, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, connectionID, string(changeType), dbName, tableName, colName, oldVal, newVal, now, now,
	)
	if err != nil {
		return fmt.Errorf("create schema change: %w", err)
	}
	return nil
}

// GetSchemaChanges returns recent schema changes for a connection.
func (s *Store) GetSchemaChanges(connectionID string, limit int) ([]SchemaChange, error) {
	rows, err := s.conn().Query(
		`SELECT id, connection_id, change_type, database_name, table_name, column_name, old_value, new_value, detected_at, created_at
		 FROM gov_schema_changes WHERE connection_id = ? ORDER BY detected_at DESC LIMIT ?`,
		connectionID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("get schema changes: %w", err)
	}
	defer rows.Close()

	var results []SchemaChange
	for rows.Next() {
		var sc SchemaChange
		if err := rows.Scan(&sc.ID, &sc.ConnectionID, &sc.ChangeType, &sc.DatabaseName, &sc.TableName, &sc.ColumnName, &sc.OldValue, &sc.NewValue, &sc.DetectedAt, &sc.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan schema change: %w", err)
		}
		results = append(results, sc)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate schema change rows: %w", err)
	}
	return results, nil
}

// ── Query Log ────────────────────────────────────────────────────────────────

// BatchInsertQueryLog inserts a batch of query log entries using INSERT OR IGNORE
// (idempotent by connection_id + query_id).
func (s *Store) BatchInsertQueryLog(connectionID string, entries []QueryLogEntry) error {
	if len(entries) == 0 {
		return nil
	}

	tx, err := s.conn().Begin()
	if err != nil {
		return fmt.Errorf("begin query log batch: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(
		`INSERT OR IGNORE INTO gov_query_log (id, connection_id, query_id, ch_user, query_text, normalized_hash, query_kind, event_time, duration_ms, read_rows, read_bytes, result_rows, written_rows, written_bytes, memory_usage, tables_used, is_error, error_message, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
	)
	if err != nil {
		return fmt.Errorf("prepare query log insert: %w", err)
	}
	defer stmt.Close()

	for _, e := range entries {
		isError := 0
		if e.IsError {
			isError = 1
		}
		_, err := stmt.Exec(
			e.ID, e.ConnectionID, e.QueryID, e.User, e.QueryText, e.NormalizedHash, e.QueryKind,
			e.EventTime, e.DurationMs, e.ReadRows, e.ReadBytes, e.ResultRows, e.WrittenRows, e.WrittenBytes,
			e.MemoryUsage, e.TablesUsed, isError, ptrToNullString(e.ErrorMessage), e.CreatedAt,
		)
		if err != nil {
			return fmt.Errorf("insert query log entry: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit query log batch: %w", err)
	}
	return nil
}

// InsertQueryLogBatch is an alias for BatchInsertQueryLog that also returns inserted count.
func (s *Store) InsertQueryLogBatch(entries []QueryLogEntry) (int, error) {
	if len(entries) == 0 {
		return 0, nil
	}

	tx, err := s.conn().Begin()
	if err != nil {
		return 0, fmt.Errorf("begin query log batch: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(
		`INSERT OR IGNORE INTO gov_query_log (id, connection_id, query_id, ch_user, query_text, normalized_hash, query_kind, event_time, duration_ms, read_rows, read_bytes, result_rows, written_rows, written_bytes, memory_usage, tables_used, is_error, error_message, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
	)
	if err != nil {
		return 0, fmt.Errorf("prepare query log insert: %w", err)
	}
	defer stmt.Close()

	now := time.Now().UTC().Format(time.RFC3339)
	inserted := 0

	for _, e := range entries {
		id := e.ID
		if id == "" {
			id = uuid.NewString()
		}
		isError := 0
		if e.IsError {
			isError = 1
		}
		createdAt := e.CreatedAt
		if createdAt == "" {
			createdAt = now
		}
		result, err := stmt.Exec(
			id, e.ConnectionID, e.QueryID, e.User, e.QueryText, e.NormalizedHash, e.QueryKind,
			e.EventTime, e.DurationMs, e.ReadRows, e.ReadBytes, e.ResultRows, e.WrittenRows, e.WrittenBytes,
			e.MemoryUsage, e.TablesUsed, isError, ptrToNullString(e.ErrorMessage), createdAt,
		)
		if err != nil {
			return 0, fmt.Errorf("insert query log entry: %w", err)
		}
		affected, _ := result.RowsAffected()
		inserted += int(affected)
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit query log batch: %w", err)
	}
	return inserted, nil
}

// GetQueryLog returns paginated query log entries with optional user/table filters.
// Returns the entries, total count, and any error.
func (s *Store) GetQueryLog(connectionID string, limit, offset int, user, table string) ([]QueryLogEntry, int, error) {
	where := "connection_id = ?"
	args := []interface{}{connectionID}

	if user != "" {
		where += " AND ch_user = ?"
		args = append(args, user)
	}
	if table != "" {
		where += " AND tables_used LIKE ?"
		args = append(args, "%"+table+"%")
	}

	// Get total count
	var total int
	countArgs := make([]interface{}, len(args))
	copy(countArgs, args)
	err := s.conn().QueryRow("SELECT COUNT(*) FROM gov_query_log WHERE "+where, countArgs...).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("count query log: %w", err)
	}

	// Get page
	query := fmt.Sprintf(
		`SELECT id, connection_id, query_id, ch_user, query_text, normalized_hash, query_kind, event_time, duration_ms, read_rows, read_bytes, result_rows, written_rows, written_bytes, memory_usage, tables_used, is_error, error_message, created_at
		 FROM gov_query_log WHERE %s ORDER BY event_time DESC LIMIT ? OFFSET ?`, where,
	)
	args = append(args, limit, offset)

	rows, err := s.conn().Query(query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("get query log: %w", err)
	}
	defer rows.Close()

	var results []QueryLogEntry
	for rows.Next() {
		var e QueryLogEntry
		var errorMsg sql.NullString
		if err := rows.Scan(&e.ID, &e.ConnectionID, &e.QueryID, &e.User, &e.QueryText, &e.NormalizedHash, &e.QueryKind, &e.EventTime, &e.DurationMs, &e.ReadRows, &e.ReadBytes, &e.ResultRows, &e.WrittenRows, &e.WrittenBytes, &e.MemoryUsage, &e.TablesUsed, &e.IsError, &errorMsg, &e.CreatedAt); err != nil {
			return nil, 0, fmt.Errorf("scan query log entry: %w", err)
		}
		e.ErrorMessage = nullStringToPtr(errorMsg)
		results = append(results, e)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate query log rows: %w", err)
	}
	return results, total, nil
}

// GetTopQueries returns the top queries grouped by normalized_hash.
func (s *Store) GetTopQueries(connectionID string, limit int) ([]map[string]interface{}, error) {
	rows, err := s.conn().Query(
		`SELECT
			normalized_hash,
			COUNT(*) AS cnt,
			ROUND(AVG(duration_ms), 2) AS avg_duration_ms,
			COALESCE(SUM(read_rows), 0) AS total_read_rows,
			MIN(query_text) AS sample_query,
			MAX(event_time) AS last_seen
		 FROM gov_query_log
		 WHERE connection_id = ? AND normalized_hash != ''
		 GROUP BY normalized_hash
		 ORDER BY cnt DESC
		 LIMIT ?`,
		connectionID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("get top queries: %w", err)
	}
	defer rows.Close()

	var results []map[string]interface{}
	for rows.Next() {
		var hash, sampleQuery, lastSeen string
		var cnt int
		var avgDurationMs float64
		var totalReadRows int64
		if err := rows.Scan(&hash, &cnt, &avgDurationMs, &totalReadRows, &sampleQuery, &lastSeen); err != nil {
			return nil, fmt.Errorf("scan top query: %w", err)
		}
		results = append(results, map[string]interface{}{
			"normalized_hash": hash,
			"count":           cnt,
			"avg_duration_ms": avgDurationMs,
			"total_read_rows": totalReadRows,
			"sample_query":    sampleQuery,
			"last_seen":       lastSeen,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate top query rows: %w", err)
	}
	return results, nil
}

// ── Lineage ──────────────────────────────────────────────────────────────────

// InsertLineageEdge inserts a lineage edge using INSERT OR IGNORE.
func (s *Store) InsertLineageEdge(edge LineageEdge) error {
	now := time.Now().UTC().Format(time.RFC3339)

	_, err := s.conn().Exec(
		`INSERT OR IGNORE INTO gov_lineage_edges (id, connection_id, source_database, source_table, target_database, target_table, query_id, ch_user, edge_type, detected_at, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		edge.ID, edge.ConnectionID, edge.SourceDatabase, edge.SourceTable, edge.TargetDatabase, edge.TargetTable,
		edge.QueryID, edge.User, edge.EdgeType, edge.DetectedAt, now,
	)
	if err != nil {
		return fmt.Errorf("insert lineage edge: %w", err)
	}
	return nil
}

// UpsertLineageEdge is an alias for InsertLineageEdge (INSERT OR IGNORE is idempotent).
func (s *Store) UpsertLineageEdge(edge LineageEdge) error {
	return s.InsertLineageEdge(edge)
}

// GetLineageForTable returns upstream and downstream edges for a specific table.
func (s *Store) GetLineageForTable(connectionID, dbName, tableName string) ([]LineageEdge, []LineageEdge, error) {
	upstreamRows, err := s.conn().Query(
		`SELECT id, connection_id, source_database, source_table, target_database, target_table, query_id, ch_user, edge_type, detected_at
		 FROM gov_lineage_edges WHERE connection_id = ? AND target_database = ? AND target_table = ?`,
		connectionID, dbName, tableName,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("get upstream lineage: %w", err)
	}
	defer upstreamRows.Close()

	upstream, err := scanLineageEdges(upstreamRows)
	if err != nil {
		return nil, nil, err
	}

	downstreamRows, err := s.conn().Query(
		`SELECT id, connection_id, source_database, source_table, target_database, target_table, query_id, ch_user, edge_type, detected_at
		 FROM gov_lineage_edges WHERE connection_id = ? AND source_database = ? AND source_table = ?`,
		connectionID, dbName, tableName,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("get downstream lineage: %w", err)
	}
	defer downstreamRows.Close()

	downstream, err := scanLineageEdges(downstreamRows)
	if err != nil {
		return nil, nil, err
	}

	return upstream, downstream, nil
}

// GetFullLineageGraph returns all lineage edges for a connection.
func (s *Store) GetFullLineageGraph(connectionID string) ([]LineageEdge, error) {
	rows, err := s.conn().Query(
		`SELECT id, connection_id, source_database, source_table, target_database, target_table, query_id, ch_user, edge_type, detected_at
		 FROM gov_lineage_edges WHERE connection_id = ? ORDER BY detected_at DESC`,
		connectionID,
	)
	if err != nil {
		return nil, fmt.Errorf("get full lineage graph: %w", err)
	}
	defer rows.Close()

	return scanLineageEdges(rows)
}

func scanLineageEdges(rows *sql.Rows) ([]LineageEdge, error) {
	var results []LineageEdge
	for rows.Next() {
		var e LineageEdge
		if err := rows.Scan(&e.ID, &e.ConnectionID, &e.SourceDatabase, &e.SourceTable, &e.TargetDatabase, &e.TargetTable, &e.QueryID, &e.User, &e.EdgeType, &e.DetectedAt); err != nil {
			return nil, fmt.Errorf("scan lineage edge: %w", err)
		}
		results = append(results, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate lineage edge rows: %w", err)
	}
	return results, nil
}

// ── Tags ─────────────────────────────────────────────────────────────────────

// GetTags returns all tags for a connection.
func (s *Store) GetTags(connectionID string) ([]TagEntry, error) {
	rows, err := s.conn().Query(
		`SELECT id, connection_id, object_type, database_name, table_name, column_name, tag, tagged_by, created_at
		 FROM gov_tags WHERE connection_id = ? ORDER BY created_at DESC`, connectionID,
	)
	if err != nil {
		return nil, fmt.Errorf("get tags: %w", err)
	}
	defer rows.Close()

	return scanTags(rows)
}

// GetTagsForTable returns all tags for a specific table.
func (s *Store) GetTagsForTable(connectionID, dbName, tableName string) ([]TagEntry, error) {
	rows, err := s.conn().Query(
		`SELECT id, connection_id, object_type, database_name, table_name, column_name, tag, tagged_by, created_at
		 FROM gov_tags WHERE connection_id = ? AND database_name = ? AND table_name = ? ORDER BY created_at DESC`,
		connectionID, dbName, tableName,
	)
	if err != nil {
		return nil, fmt.Errorf("get tags for table: %w", err)
	}
	defer rows.Close()

	return scanTags(rows)
}

// GetTagsForColumn returns all tags for a specific column.
func (s *Store) GetTagsForColumn(connectionID, dbName, tableName, colName string) ([]TagEntry, error) {
	rows, err := s.conn().Query(
		`SELECT id, connection_id, object_type, database_name, table_name, column_name, tag, tagged_by, created_at
		 FROM gov_tags WHERE connection_id = ? AND database_name = ? AND table_name = ? AND column_name = ? ORDER BY created_at DESC`,
		connectionID, dbName, tableName, colName,
	)
	if err != nil {
		return nil, fmt.Errorf("get tags for column: %w", err)
	}
	defer rows.Close()

	return scanTags(rows)
}

// CreateTag creates a new tag entry and returns its ID.
func (s *Store) CreateTag(connectionID, objectType, dbName, tableName, colName string, tag SensitivityTag, taggedBy string) (string, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	id := uuid.NewString()

	_, err := s.conn().Exec(
		`INSERT INTO gov_tags (id, connection_id, object_type, database_name, table_name, column_name, tag, tagged_by, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, connectionID, objectType, dbName, tableName, colName, string(tag), taggedBy, now,
	)
	if err != nil {
		return "", fmt.Errorf("create tag: %w", err)
	}
	return id, nil
}

// DeleteTag deletes a tag by ID.
func (s *Store) DeleteTag(id string) error {
	_, err := s.conn().Exec("DELETE FROM gov_tags WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete tag: %w", err)
	}
	return nil
}

// GetTaggedTableCount returns the count of distinct tables that have at least one tag.
func (s *Store) GetTaggedTableCount(connectionID string) (int, error) {
	var count int
	err := s.conn().QueryRow(
		`SELECT COUNT(DISTINCT database_name || '.' || table_name) FROM gov_tags WHERE connection_id = ?`,
		connectionID,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("get tagged table count: %w", err)
	}
	return count, nil
}

func scanTags(rows *sql.Rows) ([]TagEntry, error) {
	var results []TagEntry
	for rows.Next() {
		var t TagEntry
		if err := rows.Scan(&t.ID, &t.ConnectionID, &t.ObjectType, &t.DatabaseName, &t.TableName, &t.ColumnName, &t.Tag, &t.TaggedBy, &t.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan tag: %w", err)
		}
		results = append(results, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tag rows: %w", err)
	}
	return results, nil
}

// DeleteChUsersForConnection removes all cached ClickHouse users for a connection.
func (s *Store) DeleteChUsersForConnection(connectionID string) error {
	if _, err := s.conn().Exec(`DELETE FROM gov_ch_users WHERE connection_id = ?`, connectionID); err != nil {
		return fmt.Errorf("delete ch users for connection: %w", err)
	}
	return nil
}

// DeleteChRolesForConnection removes all cached ClickHouse roles for a connection.
func (s *Store) DeleteChRolesForConnection(connectionID string) error {
	if _, err := s.conn().Exec(`DELETE FROM gov_ch_roles WHERE connection_id = ?`, connectionID); err != nil {
		return fmt.Errorf("delete ch roles for connection: %w", err)
	}
	return nil
}

// DeleteRoleGrantsForConnection removes all cached role grants for a connection.
func (s *Store) DeleteRoleGrantsForConnection(connectionID string) error {
	if _, err := s.conn().Exec(`DELETE FROM gov_role_grants WHERE connection_id = ?`, connectionID); err != nil {
		return fmt.Errorf("delete role grants for connection: %w", err)
	}
	return nil
}

// DeleteGrantsForConnection removes all cached grants for a connection.
func (s *Store) DeleteGrantsForConnection(connectionID string) error {
	if _, err := s.conn().Exec(`DELETE FROM gov_grants WHERE connection_id = ?`, connectionID); err != nil {
		return fmt.Errorf("delete grants for connection: %w", err)
	}
	return nil
}

// ── CH Users ─────────────────────────────────────────────────────────────────

// UpsertChUser inserts or replaces a ClickHouse user record from a ChUser struct.
func (s *Store) UpsertChUser(u ChUser) error {
	_, err := s.conn().Exec(
		`INSERT INTO gov_ch_users (id, connection_id, name, auth_type, host_ip, default_roles, first_seen, last_updated)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(connection_id, name) DO UPDATE SET
		   auth_type = excluded.auth_type,
		   host_ip = excluded.host_ip,
		   default_roles = excluded.default_roles,
		   last_updated = excluded.last_updated`,
		u.ID, u.ConnectionID, u.Name, ptrToNullString(u.AuthType), ptrToNullString(u.HostIP), ptrToNullString(u.DefaultRoles), u.FirstSeen, u.LastUpdated,
	)
	if err != nil {
		return fmt.Errorf("upsert ch user: %w", err)
	}
	return nil
}

// GetChUsers returns all ClickHouse users for a connection.
func (s *Store) GetChUsers(connectionID string) ([]ChUser, error) {
	rows, err := s.conn().Query(
		`SELECT id, connection_id, name, auth_type, host_ip, default_roles, first_seen, last_updated
		 FROM gov_ch_users WHERE connection_id = ? ORDER BY name`, connectionID,
	)
	if err != nil {
		return nil, fmt.Errorf("get ch users: %w", err)
	}
	defer rows.Close()

	var results []ChUser
	for rows.Next() {
		var u ChUser
		var authType, hostIP, defaultRoles sql.NullString
		if err := rows.Scan(&u.ID, &u.ConnectionID, &u.Name, &authType, &hostIP, &defaultRoles, &u.FirstSeen, &u.LastUpdated); err != nil {
			return nil, fmt.Errorf("scan ch user: %w", err)
		}
		u.AuthType = nullStringToPtr(authType)
		u.HostIP = nullStringToPtr(hostIP)
		u.DefaultRoles = nullStringToPtr(defaultRoles)
		results = append(results, u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate ch user rows: %w", err)
	}
	return results, nil
}

// ── CH Roles ─────────────────────────────────────────────────────────────────

// UpsertChRole inserts or replaces a ClickHouse role record from a ChRole struct.
func (s *Store) UpsertChRole(r ChRole) error {
	_, err := s.conn().Exec(
		`INSERT INTO gov_ch_roles (id, connection_id, name, first_seen, last_updated)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(connection_id, name) DO UPDATE SET
		   last_updated = excluded.last_updated`,
		r.ID, r.ConnectionID, r.Name, r.FirstSeen, r.LastUpdated,
	)
	if err != nil {
		return fmt.Errorf("upsert ch role: %w", err)
	}
	return nil
}

// GetChRoles returns all ClickHouse roles for a connection.
func (s *Store) GetChRoles(connectionID string) ([]ChRole, error) {
	rows, err := s.conn().Query(
		`SELECT id, connection_id, name, first_seen, last_updated
		 FROM gov_ch_roles WHERE connection_id = ? ORDER BY name`, connectionID,
	)
	if err != nil {
		return nil, fmt.Errorf("get ch roles: %w", err)
	}
	defer rows.Close()

	var results []ChRole
	for rows.Next() {
		var r ChRole
		if err := rows.Scan(&r.ID, &r.ConnectionID, &r.Name, &r.FirstSeen, &r.LastUpdated); err != nil {
			return nil, fmt.Errorf("scan ch role: %w", err)
		}
		results = append(results, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate ch role rows: %w", err)
	}
	return results, nil
}

// ── Role Grants ──────────────────────────────────────────────────────────────

// UpsertRoleGrant inserts or replaces a role grant record from a RoleGrant struct.
func (s *Store) UpsertRoleGrant(rg RoleGrant) error {
	isDefaultInt := 0
	if rg.IsDefault {
		isDefaultInt = 1
	}
	withAdminInt := 0
	if rg.WithAdminOption {
		withAdminInt = 1
	}

	_, err := s.conn().Exec(
		`INSERT INTO gov_role_grants (id, connection_id, user_name, granted_role_name, is_default, with_admin_option, first_seen, last_updated)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(connection_id, user_name, granted_role_name) DO UPDATE SET
		   is_default = excluded.is_default,
		   with_admin_option = excluded.with_admin_option,
		   last_updated = excluded.last_updated`,
		rg.ID, rg.ConnectionID, rg.UserName, rg.GrantedRoleName, isDefaultInt, withAdminInt, rg.FirstSeen, rg.LastUpdated,
	)
	if err != nil {
		return fmt.Errorf("upsert role grant: %w", err)
	}
	return nil
}

// GetRoleGrants returns all role grants for a connection.
func (s *Store) GetRoleGrants(connectionID string) ([]RoleGrant, error) {
	rows, err := s.conn().Query(
		`SELECT id, connection_id, user_name, granted_role_name, is_default, with_admin_option, first_seen, last_updated
		 FROM gov_role_grants WHERE connection_id = ? ORDER BY user_name, granted_role_name`, connectionID,
	)
	if err != nil {
		return nil, fmt.Errorf("get role grants: %w", err)
	}
	defer rows.Close()

	var results []RoleGrant
	for rows.Next() {
		var rg RoleGrant
		if err := rows.Scan(&rg.ID, &rg.ConnectionID, &rg.UserName, &rg.GrantedRoleName, &rg.IsDefault, &rg.WithAdminOption, &rg.FirstSeen, &rg.LastUpdated); err != nil {
			return nil, fmt.Errorf("scan role grant: %w", err)
		}
		results = append(results, rg)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate role grant rows: %w", err)
	}
	return results, nil
}

// ── Grants ───────────────────────────────────────────────────────────────────

// UpsertGrant inserts or replaces a grant record from a Grant struct.
func (s *Store) UpsertGrant(g Grant) error {
	isPartialRevoke := 0
	if g.IsPartialRevoke {
		isPartialRevoke = 1
	}
	grantOption := 0
	if g.GrantOption {
		grantOption = 1
	}

	_, err := s.conn().Exec(
		`INSERT OR REPLACE INTO gov_grants (id, connection_id, user_name, role_name, access_type, grant_database, grant_table, grant_column, is_partial_revoke, grant_option, first_seen, last_updated)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		g.ID, g.ConnectionID, ptrToNullString(g.UserName), ptrToNullString(g.RoleName),
		g.AccessType, ptrToNullString(g.GrantDatabase), ptrToNullString(g.GrantTable), ptrToNullString(g.GrantColumn),
		isPartialRevoke, grantOption, g.FirstSeen, g.LastUpdated,
	)
	if err != nil {
		return fmt.Errorf("upsert grant: %w", err)
	}
	return nil
}

// GetGrants returns all grants for a connection.
func (s *Store) GetGrants(connectionID string) ([]Grant, error) {
	rows, err := s.conn().Query(
		`SELECT id, connection_id, user_name, role_name, access_type, grant_database, grant_table, grant_column, is_partial_revoke, grant_option, first_seen, last_updated
		 FROM gov_grants WHERE connection_id = ? ORDER BY access_type`, connectionID,
	)
	if err != nil {
		return nil, fmt.Errorf("get grants: %w", err)
	}
	defer rows.Close()

	var results []Grant
	for rows.Next() {
		var g Grant
		var userName, roleName, grantDB, grantTable, grantCol sql.NullString
		if err := rows.Scan(&g.ID, &g.ConnectionID, &userName, &roleName, &g.AccessType, &grantDB, &grantTable, &grantCol, &g.IsPartialRevoke, &g.GrantOption, &g.FirstSeen, &g.LastUpdated); err != nil {
			return nil, fmt.Errorf("scan grant: %w", err)
		}
		g.UserName = nullStringToPtr(userName)
		g.RoleName = nullStringToPtr(roleName)
		g.GrantDatabase = nullStringToPtr(grantDB)
		g.GrantTable = nullStringToPtr(grantTable)
		g.GrantColumn = nullStringToPtr(grantCol)
		results = append(results, g)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate grant rows: %w", err)
	}
	return results, nil
}

// ── Access Matrix ────────────────────────────────────────────────────────────

// RebuildAccessMatrix deletes all access matrix entries for a connection and rebuilds
// from grants and role_grants joined data. Returns the number of entries created.
func (s *Store) RebuildAccessMatrix(connectionID string) (int, error) {
	tx, err := s.conn().Begin()
	if err != nil {
		return 0, fmt.Errorf("begin access matrix rebuild: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec("DELETE FROM gov_access_matrix WHERE connection_id = ?", connectionID); err != nil {
		return 0, fmt.Errorf("delete access matrix: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	// Preload last query timestamps per user once to avoid N extra lookups while inserting.
	lastQueryByUser := make(map[string]sql.NullString)
	lastQueryRows, err := tx.Query(
		`SELECT ch_user, MAX(event_time)
		 FROM gov_query_log
		 WHERE connection_id = ?
		 GROUP BY ch_user`,
		connectionID,
	)
	if err != nil {
		return 0, fmt.Errorf("query last query times: %w", err)
	}
	for lastQueryRows.Next() {
		var userName string
		var lastQueryTime sql.NullString
		if err := lastQueryRows.Scan(&userName, &lastQueryTime); err != nil {
			lastQueryRows.Close()
			return 0, fmt.Errorf("scan last query time: %w", err)
		}
		lastQueryByUser[userName] = lastQueryTime
	}
	if err := lastQueryRows.Err(); err != nil {
		lastQueryRows.Close()
		return 0, fmt.Errorf("iterate last query rows: %w", err)
	}
	if err := lastQueryRows.Close(); err != nil {
		return 0, fmt.Errorf("close last query rows: %w", err)
	}

	insertStmt, err := tx.Prepare(
		`INSERT INTO gov_access_matrix (id, connection_id, user_name, role_name, database_name, table_name, privilege, is_direct_grant, last_query_time, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
	)
	if err != nil {
		return 0, fmt.Errorf("prepare access matrix insert: %w", err)
	}
	defer insertStmt.Close()

	count := 0

	// Insert direct user grants
	directRows, err := tx.Query(
		`SELECT g.user_name, g.grant_database, g.grant_table, g.access_type
		 FROM gov_grants g
		 WHERE g.connection_id = ? AND g.user_name IS NOT NULL AND g.is_partial_revoke = 0`,
		connectionID,
	)
	if err != nil {
		return 0, fmt.Errorf("query direct grants: %w", err)
	}

	for directRows.Next() {
		var userName string
		var grantDB, grantTable sql.NullString
		var accessType string
		if err := directRows.Scan(&userName, &grantDB, &grantTable, &accessType); err != nil {
			directRows.Close()
			return 0, fmt.Errorf("scan direct grant: %w", err)
		}

		lastQueryTime := lastQueryByUser[userName]
		if _, err = insertStmt.Exec(
			uuid.NewString(), connectionID, userName, nil, grantDB, grantTable, accessType, 1, lastQueryTime, now,
		); err != nil {
			directRows.Close()
			return 0, fmt.Errorf("insert direct grant matrix: %w", err)
		}
		count++
	}
	if err := directRows.Err(); err != nil {
		directRows.Close()
		return 0, fmt.Errorf("iterate direct grant rows: %w", err)
	}
	if err := directRows.Close(); err != nil {
		return 0, fmt.Errorf("close direct grant rows: %w", err)
	}

	// Insert role-based grants
	roleRows, err := tx.Query(
		`SELECT rg.user_name, rg.granted_role_name, g.grant_database, g.grant_table, g.access_type
		 FROM gov_role_grants rg
		 JOIN gov_grants g ON g.connection_id = rg.connection_id AND g.role_name = rg.granted_role_name
		 WHERE rg.connection_id = ? AND g.is_partial_revoke = 0`,
		connectionID,
	)
	if err != nil {
		return 0, fmt.Errorf("query role-based grants: %w", err)
	}

	for roleRows.Next() {
		var userName, roleName, accessType string
		var grantDB, grantTable sql.NullString
		if err := roleRows.Scan(&userName, &roleName, &grantDB, &grantTable, &accessType); err != nil {
			roleRows.Close()
			return 0, fmt.Errorf("scan role grant: %w", err)
		}

		lastQueryTime := lastQueryByUser[userName]
		if _, err = insertStmt.Exec(
			uuid.NewString(), connectionID, userName, roleName, grantDB, grantTable, accessType, 0, lastQueryTime, now,
		); err != nil {
			roleRows.Close()
			return 0, fmt.Errorf("insert role grant matrix: %w", err)
		}
		count++
	}
	if err := roleRows.Err(); err != nil {
		roleRows.Close()
		return 0, fmt.Errorf("iterate role grant rows: %w", err)
	}
	if err := roleRows.Close(); err != nil {
		return 0, fmt.Errorf("close role grant rows: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit access matrix rebuild: %w", err)
	}
	return count, nil
}

// GetAccessMatrix returns all access matrix entries for a connection.
func (s *Store) GetAccessMatrix(connectionID string) ([]AccessMatrixEntry, error) {
	rows, err := s.conn().Query(
		`SELECT id, connection_id, user_name, role_name, database_name, table_name, privilege, is_direct_grant, last_query_time
		 FROM gov_access_matrix WHERE connection_id = ? ORDER BY user_name, privilege`, connectionID,
	)
	if err != nil {
		return nil, fmt.Errorf("get access matrix: %w", err)
	}
	defer rows.Close()

	return scanAccessMatrix(rows)
}

// GetAccessMatrixForUser returns access matrix entries for a specific user.
func (s *Store) GetAccessMatrixForUser(connectionID, userName string) ([]AccessMatrixEntry, error) {
	rows, err := s.conn().Query(
		`SELECT id, connection_id, user_name, role_name, database_name, table_name, privilege, is_direct_grant, last_query_time
		 FROM gov_access_matrix WHERE connection_id = ? AND user_name = ? ORDER BY privilege`,
		connectionID, userName,
	)
	if err != nil {
		return nil, fmt.Errorf("get access matrix for user: %w", err)
	}
	defer rows.Close()

	return scanAccessMatrix(rows)
}

// UserHasRole checks whether a user has been granted a specific role.
func (s *Store) UserHasRole(connectionID, userName, roleName string) (bool, error) {
	var count int
	err := s.conn().QueryRow(
		`SELECT COUNT(*) FROM gov_role_grants WHERE connection_id = ? AND user_name = ? AND granted_role_name = ?`,
		connectionID, userName, roleName,
	).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("check user role: %w", err)
	}
	return count > 0, nil
}

// GetOverPermissions finds access matrix entries where last_query_time is null
// or older than a default inactivity threshold (30 days).
func (s *Store) GetOverPermissions(connectionID string) ([]OverPermission, error) {
	return s.GetOverPermissionsWithDays(connectionID, 30)
}

// GetOverPermissionsWithDays finds access matrix entries where last_query_time is null
// or older than inactiveDays.
func (s *Store) GetOverPermissionsWithDays(connectionID string, inactiveDays int) ([]OverPermission, error) {
	cutoff := time.Now().UTC().AddDate(0, 0, -inactiveDays).Format(time.RFC3339)

	rows, err := s.conn().Query(
		`SELECT user_name, role_name, database_name, table_name, privilege, last_query_time
		 FROM gov_access_matrix
		 WHERE connection_id = ? AND (last_query_time IS NULL OR last_query_time < ?)
		 ORDER BY user_name, privilege`,
		connectionID, cutoff,
	)
	if err != nil {
		return nil, fmt.Errorf("get over permissions: %w", err)
	}
	defer rows.Close()

	var results []OverPermission
	for rows.Next() {
		var op OverPermission
		var roleName, dbName, tableName, lastQueryTime sql.NullString
		if err := rows.Scan(&op.UserName, &roleName, &dbName, &tableName, &op.Privilege, &lastQueryTime); err != nil {
			return nil, fmt.Errorf("scan over permission: %w", err)
		}
		op.RoleName = nullStringToPtr(roleName)
		op.DatabaseName = nullStringToPtr(dbName)
		op.TableName = nullStringToPtr(tableName)
		op.LastQueryTime = nullStringToPtr(lastQueryTime)

		if lastQueryTime.Valid {
			t, parseErr := time.Parse(time.RFC3339, lastQueryTime.String)
			if parseErr == nil {
				days := int(time.Since(t).Hours() / 24)
				op.DaysSinceQuery = &days
			}
			op.Reason = fmt.Sprintf("no queries in %d+ days", inactiveDays)
		} else {
			op.Reason = "never queried"
		}

		results = append(results, op)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate over permission rows: %w", err)
	}
	return results, nil
}

func scanAccessMatrix(rows *sql.Rows) ([]AccessMatrixEntry, error) {
	var results []AccessMatrixEntry
	for rows.Next() {
		var am AccessMatrixEntry
		var roleName, dbName, tableName, lastQueryTime sql.NullString
		if err := rows.Scan(&am.ID, &am.ConnectionID, &am.UserName, &roleName, &dbName, &tableName, &am.Privilege, &am.IsDirectGrant, &lastQueryTime); err != nil {
			return nil, fmt.Errorf("scan access matrix entry: %w", err)
		}
		am.RoleName = nullStringToPtr(roleName)
		am.DatabaseName = nullStringToPtr(dbName)
		am.TableName = nullStringToPtr(tableName)
		am.LastQueryTime = nullStringToPtr(lastQueryTime)
		results = append(results, am)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate access matrix rows: %w", err)
	}
	return results, nil
}

// ── Policies ─────────────────────────────────────────────────────────────────

// GetPolicies returns all policies for a connection.
func (s *Store) GetPolicies(connectionID string) ([]Policy, error) {
	return s.scanPolicies(
		`SELECT id, connection_id, name, description, object_type, object_database, object_table, object_column, required_role, severity, enforcement_mode, enabled, created_by, created_at, updated_at
		 FROM gov_policies WHERE connection_id = ? ORDER BY name`, connectionID,
	)
}

// GetEnabledPolicies returns all enabled policies for a connection.
func (s *Store) GetEnabledPolicies(connectionID string) ([]Policy, error) {
	return s.scanPolicies(
		`SELECT id, connection_id, name, description, object_type, object_database, object_table, object_column, required_role, severity, enforcement_mode, enabled, created_by, created_at, updated_at
		 FROM gov_policies WHERE connection_id = ? AND enabled = 1 ORDER BY name`, connectionID,
	)
}

func (s *Store) scanPolicies(query string, args ...interface{}) ([]Policy, error) {
	rows, err := s.conn().Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("get policies: %w", err)
	}
	defer rows.Close()

	var results []Policy
	for rows.Next() {
		var p Policy
		var desc, objDB, objTable, objCol, createdBy, enforcementMode sql.NullString
		if err := rows.Scan(&p.ID, &p.ConnectionID, &p.Name, &desc, &p.ObjectType, &objDB, &objTable, &objCol, &p.RequiredRole, &p.Severity, &enforcementMode, &p.Enabled, &createdBy, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan policy: %w", err)
		}
		p.Description = nullStringToPtr(desc)
		p.ObjectDatabase = nullStringToPtr(objDB)
		p.ObjectTable = nullStringToPtr(objTable)
		p.ObjectColumn = nullStringToPtr(objCol)
		p.EnforcementMode = normalizePolicyEnforcementMode(enforcementMode.String)
		p.CreatedBy = nullStringToPtr(createdBy)
		results = append(results, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate policy rows: %w", err)
	}
	return results, nil
}

// GetPolicyByID returns a single policy by ID.
func (s *Store) GetPolicyByID(id string) (*Policy, error) {
	row := s.conn().QueryRow(
		`SELECT id, connection_id, name, description, object_type, object_database, object_table, object_column, required_role, severity, enforcement_mode, enabled, created_by, created_at, updated_at
		 FROM gov_policies WHERE id = ?`, id,
	)

	var p Policy
	var desc, objDB, objTable, objCol, createdBy, enforcementMode sql.NullString
	err := row.Scan(&p.ID, &p.ConnectionID, &p.Name, &desc, &p.ObjectType, &objDB, &objTable, &objCol, &p.RequiredRole, &p.Severity, &enforcementMode, &p.Enabled, &createdBy, &p.CreatedAt, &p.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get policy by id: %w", err)
	}
	p.Description = nullStringToPtr(desc)
	p.ObjectDatabase = nullStringToPtr(objDB)
	p.ObjectTable = nullStringToPtr(objTable)
	p.ObjectColumn = nullStringToPtr(objCol)
	p.EnforcementMode = normalizePolicyEnforcementMode(enforcementMode.String)
	p.CreatedBy = nullStringToPtr(createdBy)
	return &p, nil
}

// CreatePolicy creates a new policy and returns its ID.
func (s *Store) CreatePolicy(connectionID, name, description, objectType, objectDB, objectTable, objectCol, requiredRole, severity, enforcementMode, createdBy string) (string, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	id := uuid.NewString()

	var desc, oDB, oTable, oCol, cBy interface{}
	if description != "" {
		desc = description
	}
	if objectDB != "" {
		oDB = objectDB
	}
	if objectTable != "" {
		oTable = objectTable
	}
	if objectCol != "" {
		oCol = objectCol
	}
	if createdBy != "" {
		cBy = createdBy
	}

	_, err := s.conn().Exec(
		`INSERT INTO gov_policies (id, connection_id, name, description, object_type, object_database, object_table, object_column, required_role, severity, enforcement_mode, enabled, created_by, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?, ?)`,
		id, connectionID, name, desc, objectType, oDB, oTable, oCol, requiredRole, severity, normalizePolicyEnforcementMode(enforcementMode), cBy, now, now,
	)
	if err != nil {
		return "", fmt.Errorf("create policy: %w", err)
	}
	return id, nil
}

// UpdatePolicy updates an existing policy.
func (s *Store) UpdatePolicy(id, name, description, requiredRole, severity, enforcementMode string, enabled bool) error {
	now := time.Now().UTC().Format(time.RFC3339)

	var desc interface{}
	if description != "" {
		desc = description
	}

	enabledInt := 0
	if enabled {
		enabledInt = 1
	}

	_, err := s.conn().Exec(
		`UPDATE gov_policies SET name = ?, description = ?, required_role = ?, severity = ?, enforcement_mode = ?, enabled = ?, updated_at = ? WHERE id = ?`,
		name, desc, requiredRole, severity, normalizePolicyEnforcementMode(enforcementMode), enabledInt, now, id,
	)
	if err != nil {
		return fmt.Errorf("update policy: %w", err)
	}
	return nil
}

// DeletePolicy deletes a policy by ID (cascades to violations).
func (s *Store) DeletePolicy(id string) error {
	_, err := s.conn().Exec("DELETE FROM gov_policies WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete policy: %w", err)
	}
	return nil
}

// ── Violations ───────────────────────────────────────────────────────────────

// InsertPolicyViolation inserts a policy violation from a PolicyViolation struct.
func (s *Store) InsertPolicyViolation(v PolicyViolation) error {
	_, err := s.conn().Exec(
		`INSERT INTO gov_policy_violations (id, connection_id, policy_id, query_log_id, ch_user, violation_detail, severity, detection_phase, request_endpoint, detected_at, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		v.ID, v.ConnectionID, v.PolicyID, nullableValue(v.QueryLogID), v.User, v.ViolationDetail, v.Severity, normalizeDetectionPhase(v.DetectionPhase), nullableValue(deref(v.RequestEndpoint)), v.DetectedAt, v.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert policy violation: %w", err)
	}
	return nil
}

// CreateViolation creates a new policy violation and returns its ID.
func (s *Store) CreateViolation(connectionID, policyID, queryLogID, user, detail, severity, detectionPhase, requestEndpoint string) (string, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	id := uuid.NewString()

	_, err := s.conn().Exec(
		`INSERT INTO gov_policy_violations (id, connection_id, policy_id, query_log_id, ch_user, violation_detail, severity, detection_phase, request_endpoint, detected_at, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, connectionID, policyID, nullableValue(queryLogID), user, detail, severity, normalizeDetectionPhase(detectionPhase), nullableValue(requestEndpoint), now, now,
	)
	if err != nil {
		return "", fmt.Errorf("create violation: %w", err)
	}
	return id, nil
}

// GetViolations returns violations for a connection with optional policyID filter,
// joined with the policy name.
func (s *Store) GetViolations(connectionID string, limit int, policyID string) ([]PolicyViolation, error) {
	where := "v.connection_id = ?"
	args := []interface{}{connectionID}

	if policyID != "" {
		where += " AND v.policy_id = ?"
		args = append(args, policyID)
	}

	args = append(args, limit)

	query := fmt.Sprintf(
		`SELECT v.id, v.connection_id, v.policy_id, v.query_log_id, v.ch_user, v.violation_detail, v.severity, v.detection_phase, v.request_endpoint, v.detected_at, v.created_at, COALESCE(p.name, '')
		 FROM gov_policy_violations v
		 LEFT JOIN gov_policies p ON p.id = v.policy_id
		 WHERE %s
		 ORDER BY v.detected_at DESC
		 LIMIT ?`, where,
	)

	rows, err := s.conn().Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("get violations: %w", err)
	}
	defer rows.Close()

	var results []PolicyViolation
	for rows.Next() {
		var v PolicyViolation
		var queryLogID, requestEndpoint sql.NullString
		if err := rows.Scan(&v.ID, &v.ConnectionID, &v.PolicyID, &queryLogID, &v.User, &v.ViolationDetail, &v.Severity, &v.DetectionPhase, &requestEndpoint, &v.DetectedAt, &v.CreatedAt, &v.PolicyName); err != nil {
			return nil, fmt.Errorf("scan violation: %w", err)
		}
		v.QueryLogID = queryLogID.String
		v.RequestEndpoint = nullStringToPtr(requestEndpoint)
		v.DetectionPhase = normalizeDetectionPhase(v.DetectionPhase)
		results = append(results, v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate violation rows: %w", err)
	}
	return results, nil
}

func normalizePolicyEnforcementMode(v string) string {
	mode := strings.ToLower(strings.TrimSpace(v))
	switch mode {
	case "block":
		return "block"
	default:
		return "warn"
	}
}

func normalizeDetectionPhase(v string) string {
	phase := strings.ToLower(strings.TrimSpace(v))
	switch phase {
	case "pre_exec_block":
		return "pre_exec_block"
	default:
		return "post_exec"
	}
}

// ── Overview ─────────────────────────────────────────────────────────────────

// GetOverview returns aggregate counts from all governance tables for a connection.
func (s *Store) GetOverview(connectionID string) (*GovernanceOverview, error) {
	o := &GovernanceOverview{}

	s.conn().QueryRow("SELECT COUNT(*) FROM gov_databases WHERE connection_id = ? AND is_deleted = 0", connectionID).Scan(&o.DatabaseCount)
	s.conn().QueryRow("SELECT COUNT(*) FROM gov_tables WHERE connection_id = ? AND is_deleted = 0", connectionID).Scan(&o.TableCount)
	s.conn().QueryRow("SELECT COUNT(*) FROM gov_columns WHERE connection_id = ? AND is_deleted = 0", connectionID).Scan(&o.ColumnCount)

	tagCount, err := s.GetTaggedTableCount(connectionID)
	if err == nil {
		o.TaggedTableCount = tagCount
	}

	s.conn().QueryRow("SELECT COUNT(*) FROM gov_ch_users WHERE connection_id = ?", connectionID).Scan(&o.UserCount)
	s.conn().QueryRow("SELECT COUNT(*) FROM gov_ch_roles WHERE connection_id = ?", connectionID).Scan(&o.RoleCount)

	cutoff24h := time.Now().UTC().Add(-24 * time.Hour).Format(time.RFC3339)
	s.conn().QueryRow("SELECT COUNT(*) FROM gov_query_log WHERE connection_id = ? AND event_time > ?", connectionID, cutoff24h).Scan(&o.QueryCount24h)

	s.conn().QueryRow("SELECT COUNT(*) FROM gov_lineage_edges WHERE connection_id = ?", connectionID).Scan(&o.LineageEdgeCount)
	s.conn().QueryRow("SELECT COUNT(*) FROM gov_policies WHERE connection_id = ?", connectionID).Scan(&o.PolicyCount)
	s.conn().QueryRow("SELECT COUNT(*) FROM gov_policy_violations WHERE connection_id = ?", connectionID).Scan(&o.ViolationCount)
	s.conn().QueryRow("SELECT COUNT(*) FROM gov_incidents WHERE connection_id = ? AND status IN ('open', 'triaged', 'in_progress')", connectionID).Scan(&o.IncidentCount)
	s.conn().QueryRow("SELECT COUNT(*) FROM gov_schema_changes WHERE connection_id = ?", connectionID).Scan(&o.SchemaChangeCount)

	syncStates, err := s.GetSyncStates(connectionID)
	if err == nil {
		o.SyncStates = syncStates
	}

	recentChanges, err := s.GetSchemaChanges(connectionID, 10)
	if err == nil {
		o.RecentChanges = recentChanges
	}

	recentViolations, err := s.GetViolations(connectionID, 10, "")
	if err == nil {
		o.RecentViolations = recentViolations
	}

	return o, nil
}

// ── Cleanup ──────────────────────────────────────────────────────────────────

// CleanupOldQueryLogs deletes query logs older than the given timestamp.
func (s *Store) CleanupOldQueryLogs(connectionID string, before string) (int64, error) {
	result, err := s.conn().Exec(
		"DELETE FROM gov_query_log WHERE connection_id = ? AND event_time < ?",
		connectionID, before,
	)
	if err != nil {
		return 0, fmt.Errorf("cleanup old query logs: %w", err)
	}
	return result.RowsAffected()
}

// CleanupOldViolations deletes violations older than the given timestamp.
func (s *Store) CleanupOldViolations(connectionID string, before string) (int64, error) {
	result, err := s.conn().Exec(
		"DELETE FROM gov_policy_violations WHERE connection_id = ? AND detected_at < ?",
		connectionID, before,
	)
	if err != nil {
		return 0, fmt.Errorf("cleanup old violations: %w", err)
	}
	return result.RowsAffected()
}
