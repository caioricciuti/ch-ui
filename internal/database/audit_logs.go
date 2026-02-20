package database

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// AuditLogParams holds parameters for creating an audit log entry.
type AuditLogParams struct {
	Action       string
	Username     *string
	ConnectionID *string
	Details      *string
	IPAddress    *string
}

// AuditLog represents an audit log entry.
type AuditLog struct {
	ID           string  `json:"id"`
	Action       string  `json:"action"`
	Username     *string `json:"username"`
	ConnectionID *string `json:"connection_id"`
	Details      *string `json:"details"`
	IPAddress    *string `json:"ip_address"`
	CreatedAt    string  `json:"created_at"`
}

// CreateAuditLog creates a new audit log entry.
func (db *DB) CreateAuditLog(params AuditLogParams) error {
	id := uuid.NewString()
	_, err := db.conn.Exec(
		`INSERT INTO audit_logs (id, action, username, connection_id, details, ip_address)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		id, params.Action, params.Username, params.ConnectionID, params.Details, params.IPAddress,
	)
	if err != nil {
		return fmt.Errorf("create audit log: %w", err)
	}
	return nil
}

// GetAuditLogs retrieves audit logs, most recent first.
func (db *DB) GetAuditLogs(limit int) ([]AuditLog, error) {
	return db.GetAuditLogsFiltered(limit, "", "", "", "")
}

// GetAuditLogsFiltered retrieves audit logs with optional filters, most recent first.
func (db *DB) GetAuditLogsFiltered(limit int, timeRange, action, username, search string) ([]AuditLog, error) {
	if limit <= 0 {
		limit = 100
	}

	var whereClauses []string
	args := make([]any, 0, 8)

	timeRange = strings.TrimSpace(timeRange)
	action = strings.TrimSpace(action)
	username = strings.TrimSpace(username)
	search = strings.TrimSpace(strings.ToLower(search))

	timeRangeOffsets := map[string]string{
		"15m": "-15 minutes",
		"1h":  "-1 hour",
		"6h":  "-6 hours",
		"24h": "-24 hours",
		"7d":  "-7 days",
		"30d": "-30 days",
	}
	if offset, ok := timeRangeOffsets[timeRange]; ok {
		whereClauses = append(whereClauses, "created_at >= datetime('now', ?)")
		args = append(args, offset)
	}
	if action != "" {
		whereClauses = append(whereClauses, "action = ?")
		args = append(args, action)
	}
	if username != "" {
		whereClauses = append(whereClauses, "username = ?")
		args = append(args, username)
	}
	if search != "" {
		term := "%" + search + "%"
		whereClauses = append(whereClauses,
			`(
				lower(action) LIKE ? OR
				lower(COALESCE(username, '')) LIKE ? OR
				lower(COALESCE(details, '')) LIKE ? OR
				lower(COALESCE(ip_address, '')) LIKE ?
			)`,
		)
		args = append(args, term, term, term, term)
	}

	query := strings.Builder{}
	query.WriteString(`SELECT id, action, username, connection_id, details, ip_address, created_at FROM audit_logs`)
	if len(whereClauses) > 0 {
		query.WriteString(" WHERE ")
		query.WriteString(strings.Join(whereClauses, " AND "))
	}
	query.WriteString(" ORDER BY created_at DESC LIMIT ?")
	args = append(args, limit)

	rows, err := db.conn.Query(query.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("get audit logs: %w", err)
	}
	defer rows.Close()

	var logs []AuditLog
	for rows.Next() {
		var l AuditLog
		var username, connID, details, ip sql.NullString
		if err := rows.Scan(&l.ID, &l.Action, &username, &connID, &details, &ip, &l.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan audit log: %w", err)
		}
		l.Username = nullStringToPtr(username)
		l.ConnectionID = nullStringToPtr(connID)
		l.Details = nullStringToPtr(details)
		l.IPAddress = nullStringToPtr(ip)
		logs = append(logs, l)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate audit log rows: %w", err)
	}
	return logs, nil
}
