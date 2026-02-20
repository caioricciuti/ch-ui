package governance

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/google/uuid"
)

// syncMetadata harvests database/table/column metadata from ClickHouse system tables,
// diffs against existing SQLite state, and records schema changes.
func (s *Syncer) syncMetadata(ctx context.Context, creds CHCredentials) (*MetadataSyncResult, error) {
	connID := creds.ConnectionID
	now := time.Now().UTC().Format(time.RFC3339)

	// Update sync state to running
	if err := s.store.UpsertSyncState(connID, string(SyncMetadata), "running", nil, nil, 0); err != nil {
		slog.Error("Failed to update sync state", "error", err)
	}

	result := &MetadataSyncResult{}
	var syncErr error

	defer func() {
		status := "idle"
		var errMsg *string
		if syncErr != nil {
			status = "error"
			e := syncErr.Error()
			errMsg = &e
		}
		rowCount := result.DatabasesSynced + result.TablesSynced + result.ColumnsSynced
		if err := s.store.UpsertSyncState(connID, string(SyncMetadata), status, &now, errMsg, rowCount); err != nil {
			slog.Error("Failed to update sync state after metadata sync", "error", err)
		}
	}()

	// ── Phase 1: Databases ──────────────────────────────────────────────────
	dbRows, err := s.executeQuery(creds,
		`SELECT name, engine FROM system.databases
		 WHERE name NOT IN ('system', 'INFORMATION_SCHEMA', 'information_schema')
		 ORDER BY name`)
	if err != nil {
		slog.Warn("Metadata sync: failed to query databases", "connection", connID, "error", err)
		syncErr = fmt.Errorf("databases query failed: %w", err)
		return result, syncErr
	}

	existingDBs, err := s.store.GetDatabases(connID)
	if err != nil {
		syncErr = fmt.Errorf("failed to load existing databases: %w", err)
		return result, syncErr
	}
	existingDBMap := make(map[string]*GovDatabase, len(existingDBs))
	for i := range existingDBs {
		existingDBMap[existingDBs[i].Name] = &existingDBs[i]
	}

	seenDBs := make(map[string]bool)
	for _, row := range dbRows {
		name := fmt.Sprintf("%v", row["name"])
		engine := fmt.Sprintf("%v", row["engine"])
		seenDBs[name] = true

		_, found := existingDBMap[name]
		if err := s.store.UpsertDatabase(GovDatabase{
			ID:           uuid.NewString(),
			ConnectionID: connID,
			Name:         name,
			Engine:       engine,
			FirstSeen:    now,
			LastUpdated:  now,
		}); err != nil {
			slog.Error("Failed to upsert database", "name", name, "error", err)
			continue
		}

		if !found {
			s.store.CreateSchemaChange(connID, ChangeDatabaseAdded, name, "", "", "", name)
			result.SchemaChanges++
		}
		result.DatabasesSynced++
	}

	// Mark removed databases
	for name, existing := range existingDBMap {
		if !seenDBs[name] && !existing.IsDeleted {
			if err := s.store.MarkDatabaseDeleted(connID, name); err != nil {
				slog.Error("Failed to mark database deleted", "name", name, "error", err)
			}
			s.store.CreateSchemaChange(connID, ChangeDatabaseRemoved, name, "", "", name, "")
			result.SchemaChanges++
		}
	}

	// ── Phase 2: Tables with stats ──────────────────────────────────────────
	tableRows, err := s.executeQuery(creds,
		`SELECT
			t.database AS database_name,
			t.name AS table_name,
			t.engine AS engine,
			t.uuid AS table_uuid,
			COALESCE(sum(p.rows), 0) AS total_rows,
			COALESCE(sum(p.bytes_on_disk), 0) AS total_bytes,
			COALESCE(count(DISTINCT p.partition), 0) AS partition_count
		 FROM system.tables t
		 LEFT JOIN system.parts p ON p.database = t.database AND p.table = t.name AND p.active = 1
		 WHERE t.database NOT IN ('system', 'INFORMATION_SCHEMA', 'information_schema')
		 GROUP BY t.database, t.name, t.engine, t.uuid
		 ORDER BY t.database, t.name`)
	if err != nil {
		slog.Warn("Metadata sync: failed to query tables", "connection", connID, "error", err)
		// Continue — tables query failure is non-fatal
	} else {
		existingTables, err := s.store.GetTables(connID)
		if err != nil {
			slog.Error("Failed to load existing tables", "error", err)
		}
		existingTableMap := make(map[string]*GovTable)
		for i := range existingTables {
			key := existingTables[i].DatabaseName + "." + existingTables[i].TableName
			existingTableMap[key] = &existingTables[i]
		}

		seenTables := make(map[string]bool)
		for _, row := range tableRows {
			dbName := fmt.Sprintf("%v", row["database_name"])
			tableName := fmt.Sprintf("%v", row["table_name"])
			engine := fmt.Sprintf("%v", row["engine"])
			tableUUID := fmt.Sprintf("%v", row["table_uuid"])
			totalRows := toInt64(row["total_rows"])
			totalBytes := toInt64(row["total_bytes"])
			partCount := int(toInt64(row["partition_count"]))
			key := dbName + "." + tableName
			seenTables[key] = true

			_, found := existingTableMap[key]
			if err := s.store.UpsertTable(GovTable{
				ID:             uuid.NewString(),
				ConnectionID:   connID,
				DatabaseName:   dbName,
				TableName:      tableName,
				Engine:         engine,
				TableUUID:      tableUUID,
				TotalRows:      totalRows,
				TotalBytes:     totalBytes,
				PartitionCount: partCount,
				FirstSeen:      now,
				LastUpdated:    now,
			}); err != nil {
				slog.Error("Failed to upsert table", "table", key, "error", err)
				continue
			}

			if !found {
				s.store.CreateSchemaChange(connID, ChangeTableAdded, dbName, tableName, "", "", tableName)
				result.SchemaChanges++
			}
			result.TablesSynced++
		}

		// Mark removed tables
		for key, existing := range existingTableMap {
			if !seenTables[key] && !existing.IsDeleted {
				if err := s.store.MarkTableDeleted(connID, existing.DatabaseName, existing.TableName); err != nil {
					slog.Error("Failed to mark table deleted", "table", key, "error", err)
				}
				s.store.CreateSchemaChange(connID, ChangeTableRemoved, existing.DatabaseName, existing.TableName, "", existing.TableName, "")
				result.SchemaChanges++
			}
		}
	}

	// ── Phase 3: Columns ────────────────────────────────────────────────────
	colRows, err := s.executeQuery(creds,
		`SELECT
			database AS database_name,
			table AS table_name,
			name AS column_name,
			type AS column_type,
			position AS column_position,
			default_kind,
			default_expression,
			comment
		 FROM system.columns
		 WHERE database NOT IN ('system', 'INFORMATION_SCHEMA', 'information_schema')
		 ORDER BY database, table, position`)
	if err != nil {
		slog.Warn("Metadata sync: failed to query columns", "connection", connID, "error", err)
	} else {
		existingColMap := make(map[string]*GovColumn)
		tables, tblErr := s.store.GetTables(connID)
		if tblErr == nil {
			for _, tbl := range tables {
				cols, colErr := s.store.GetColumns(connID, tbl.DatabaseName, tbl.TableName)
				if colErr != nil {
					continue
				}
				for i := range cols {
					key := cols[i].DatabaseName + "." + cols[i].TableName + "." + cols[i].ColumnName
					existingColMap[key] = &cols[i]
				}
			}
		}

		seenCols := make(map[string]bool)
		for _, row := range colRows {
			dbName := fmt.Sprintf("%v", row["database_name"])
			tableName := fmt.Sprintf("%v", row["table_name"])
			colName := fmt.Sprintf("%v", row["column_name"])
			colType := fmt.Sprintf("%v", row["column_type"])
			position := int(toInt64(row["column_position"]))
			key := dbName + "." + tableName + "." + colName
			seenCols[key] = true

			defaultKind := toStringPtr(row["default_kind"])
			defaultExpr := toStringPtr(row["default_expression"])
			comment := toStringPtr(row["comment"])

			existing, found := existingColMap[key]
			if err := s.store.UpsertColumn(GovColumn{
				ID:                uuid.NewString(),
				ConnectionID:      connID,
				DatabaseName:      dbName,
				TableName:         tableName,
				ColumnName:        colName,
				ColumnType:        colType,
				ColumnPosition:    position,
				DefaultKind:       defaultKind,
				DefaultExpression: defaultExpr,
				Comment:           comment,
				FirstSeen:         now,
				LastUpdated:       now,
			}); err != nil {
				slog.Error("Failed to upsert column", "column", key, "error", err)
				continue
			}

			if !found {
				s.store.CreateSchemaChange(connID, ChangeColumnAdded, dbName, tableName, colName, "", colName)
				result.SchemaChanges++
			} else if existing.ColumnType != colType {
				s.store.CreateSchemaChange(connID, ChangeColumnTypeChanged, dbName, tableName, colName, existing.ColumnType, colType)
				result.SchemaChanges++
			}
			result.ColumnsSynced++
		}

		// Mark removed columns
		for key, existing := range existingColMap {
			if !seenCols[key] && !existing.IsDeleted {
				if err := s.store.MarkColumnDeleted(connID, existing.DatabaseName, existing.TableName, existing.ColumnName); err != nil {
					slog.Error("Failed to mark column deleted", "column", key, "error", err)
				}
				s.store.CreateSchemaChange(connID, ChangeColumnRemoved, existing.DatabaseName, existing.TableName, existing.ColumnName, existing.ColumnName, "")
				result.SchemaChanges++
			}
		}
	}

	slog.Info("Metadata sync completed",
		"connection", connID,
		"databases", result.DatabasesSynced,
		"tables", result.TablesSynced,
		"columns", result.ColumnsSynced,
		"changes", result.SchemaChanges,
	)

	return result, nil
}

// toInt64 converts interface{} values (float64, string, json.Number) to int64.
func toInt64(v interface{}) int64 {
	if v == nil {
		return 0
	}
	switch val := v.(type) {
	case float64:
		return int64(val)
	case int64:
		return val
	case int:
		return int64(val)
	case json.Number:
		n, _ := val.Int64()
		return n
	case string:
		n, _ := strconv.ParseInt(val, 10, 64)
		return n
	default:
		s := fmt.Sprintf("%v", v)
		n, _ := strconv.ParseInt(s, 10, 64)
		return n
	}
}

// toStringPtr converts interface{} to *string. Returns nil for nil or empty strings.
func toStringPtr(v interface{}) *string {
	if v == nil {
		return nil
	}
	s := fmt.Sprintf("%v", v)
	if s == "" || s == "<nil>" {
		return nil
	}
	return &s
}
