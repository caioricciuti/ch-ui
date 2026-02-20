package governance

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ── Object comments ──────────────────────────────────────────────────────────

func (s *Store) CreateObjectComment(connectionID, objectType, dbName, tableName, columnName, commentText, createdBy string) (string, error) {
	id := uuid.NewString()
	now := time.Now().UTC().Format(time.RFC3339)
	if objectType == "table" {
		columnName = ""
	}
	_, err := s.conn().Exec(
		`INSERT INTO gov_object_comments (id, connection_id, object_type, database_name, table_name, column_name, comment_text, created_by, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id,
		connectionID,
		strings.ToLower(strings.TrimSpace(objectType)),
		strings.TrimSpace(dbName),
		strings.TrimSpace(tableName),
		strings.TrimSpace(columnName),
		strings.TrimSpace(commentText),
		nullableValue(createdBy),
		now,
		now,
	)
	if err != nil {
		return "", fmt.Errorf("create object comment: %w", err)
	}
	return id, nil
}

func (s *Store) ListObjectComments(connectionID, objectType, dbName, tableName, columnName string, limit int) ([]ObjectComment, error) {
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}

	where := []string{"connection_id = ?"}
	args := []interface{}{connectionID}
	if ot := strings.TrimSpace(strings.ToLower(objectType)); ot != "" {
		where = append(where, "object_type = ?")
		args = append(args, ot)
	}
	if db := strings.TrimSpace(dbName); db != "" {
		where = append(where, "database_name = ?")
		args = append(args, db)
	}
	if tbl := strings.TrimSpace(tableName); tbl != "" {
		where = append(where, "table_name = ?")
		args = append(args, tbl)
	}
	if col := strings.TrimSpace(columnName); col != "" {
		where = append(where, "column_name = ?")
		args = append(args, col)
	}
	args = append(args, limit)

	query := fmt.Sprintf(
		`SELECT id, connection_id, object_type, database_name, table_name, column_name, comment_text, created_by, created_at, updated_at
		 FROM gov_object_comments
		 WHERE %s
		 ORDER BY created_at DESC
		 LIMIT ?`,
		strings.Join(where, " AND "),
	)

	rows, err := s.conn().Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list object comments: %w", err)
	}
	defer rows.Close()

	out := make([]ObjectComment, 0)
	for rows.Next() {
		var c ObjectComment
		var createdBy sql.NullString
		if err := rows.Scan(
			&c.ID, &c.ConnectionID, &c.ObjectType, &c.DatabaseName, &c.TableName, &c.ColumnName,
			&c.CommentText, &createdBy, &c.CreatedAt, &c.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan object comment: %w", err)
		}
		c.CreatedBy = nullStringToPtr(createdBy)
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate object comments: %w", err)
	}
	return out, nil
}

func (s *Store) DeleteObjectComment(connectionID, id string) error {
	res, err := s.conn().Exec(
		`DELETE FROM gov_object_comments WHERE id = ? AND connection_id = ?`,
		id, connectionID,
	)
	if err != nil {
		return fmt.Errorf("delete object comment: %w", err)
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// ── Incidents ────────────────────────────────────────────────────────────────

func (s *Store) ListIncidents(connectionID, status, severity string, limit int) ([]Incident, error) {
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	where := []string{"connection_id = ?"}
	args := []interface{}{connectionID}
	if v := strings.TrimSpace(strings.ToLower(status)); v != "" {
		where = append(where, "status = ?")
		args = append(args, v)
	}
	if v := strings.TrimSpace(strings.ToLower(severity)); v != "" {
		where = append(where, "severity = ?")
		args = append(args, v)
	}
	args = append(args, limit)

	query := fmt.Sprintf(
		`SELECT id, connection_id, source_type, source_ref, dedupe_key, title, severity, status, assignee, details, resolution_note,
		        occurrence_count, first_seen_at, last_seen_at, resolved_at, created_by, created_at, updated_at
		 FROM gov_incidents
		 WHERE %s
		 ORDER BY last_seen_at DESC
		 LIMIT ?`,
		strings.Join(where, " AND "),
	)
	rows, err := s.conn().Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list incidents: %w", err)
	}
	defer rows.Close()

	out := make([]Incident, 0)
	for rows.Next() {
		item, err := scanIncident(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate incidents: %w", err)
	}
	return out, nil
}

func (s *Store) GetIncidentByID(id string) (*Incident, error) {
	row := s.conn().QueryRow(
		`SELECT id, connection_id, source_type, source_ref, dedupe_key, title, severity, status, assignee, details, resolution_note,
		        occurrence_count, first_seen_at, last_seen_at, resolved_at, created_by, created_at, updated_at
		 FROM gov_incidents
		 WHERE id = ?`,
		id,
	)
	item, err := scanIncident(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (s *Store) CreateIncident(connectionID, sourceType, sourceRef, dedupeKey, title, severity, status, assignee, details, createdBy string) (string, error) {
	id := uuid.NewString()
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.conn().Exec(
		`INSERT INTO gov_incidents
		 (id, connection_id, source_type, source_ref, dedupe_key, title, severity, status, assignee, details, occurrence_count, first_seen_at, last_seen_at, created_by, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?, ?, ?, ?)`,
		id,
		connectionID,
		nullableValue(strings.ToLower(strings.TrimSpace(sourceType))),
		nullableValue(strings.TrimSpace(sourceRef)),
		nullableValue(strings.TrimSpace(dedupeKey)),
		strings.TrimSpace(title),
		strings.ToLower(strings.TrimSpace(severity)),
		strings.ToLower(strings.TrimSpace(status)),
		nullableValue(strings.TrimSpace(assignee)),
		nullableValue(strings.TrimSpace(details)),
		now,
		now,
		nullableValue(strings.TrimSpace(createdBy)),
		now,
		now,
	)
	if err != nil {
		return "", fmt.Errorf("create incident: %w", err)
	}
	return id, nil
}

func (s *Store) UpdateIncident(id, title, severity, status, assignee, details, resolutionNote string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	var resolvedAt interface{}
	if status == "resolved" || status == "dismissed" {
		resolvedAt = now
	}
	if _, err := s.conn().Exec(
		`UPDATE gov_incidents
		 SET title = ?, severity = ?, status = ?, assignee = ?, details = ?, resolution_note = ?, resolved_at = ?, updated_at = ?
		 WHERE id = ?`,
		strings.TrimSpace(title),
		strings.ToLower(strings.TrimSpace(severity)),
		strings.ToLower(strings.TrimSpace(status)),
		nullableValue(strings.TrimSpace(assignee)),
		nullableValue(strings.TrimSpace(details)),
		nullableValue(strings.TrimSpace(resolutionNote)),
		resolvedAt,
		now,
		id,
	); err != nil {
		return fmt.Errorf("update incident: %w", err)
	}
	return nil
}

func (s *Store) UpsertIncidentFromViolation(connectionID, sourceRef, policyName, user, severity, detail string) (string, bool, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	dedupeKey := strings.ToLower(strings.TrimSpace(fmt.Sprintf("violation:%s:%s:%s", policyName, user, severity)))
	row := s.conn().QueryRow(
		`SELECT id
		 FROM gov_incidents
		 WHERE connection_id = ?
		   AND dedupe_key = ?
		   AND status IN ('open', 'triaged', 'in_progress')
		 ORDER BY last_seen_at DESC
		 LIMIT 1`,
		connectionID, dedupeKey,
	)
	var incidentID string
	switch err := row.Scan(&incidentID); err {
	case nil:
		if _, err := s.conn().Exec(
			`UPDATE gov_incidents
			 SET occurrence_count = occurrence_count + 1,
			     last_seen_at = ?,
			     details = COALESCE(?, details),
			     updated_at = ?
			 WHERE id = ?`,
			now,
			nullableValue(strings.TrimSpace(detail)),
			now,
			incidentID,
		); err != nil {
			return "", false, fmt.Errorf("update existing incident from violation: %w", err)
		}
		return incidentID, false, nil
	case sql.ErrNoRows:
		title := fmt.Sprintf("Policy violation: %s (%s)", strings.TrimSpace(policyName), strings.TrimSpace(user))
		if strings.TrimSpace(policyName) == "" {
			title = fmt.Sprintf("Policy violation (%s)", strings.TrimSpace(user))
		}
		id, err := s.CreateIncident(
			connectionID,
			"violation",
			sourceRef,
			dedupeKey,
			title,
			strings.ToLower(strings.TrimSpace(severity)),
			"open",
			"",
			detail,
			"system",
		)
		if err != nil {
			return "", false, err
		}
		return id, true, nil
	default:
		return "", false, fmt.Errorf("find existing incident from violation: %w", err)
	}
}

func (s *Store) GetViolationByID(id string) (*PolicyViolation, error) {
	row := s.conn().QueryRow(
		`SELECT v.id, v.connection_id, v.policy_id, v.query_log_id, v.ch_user, v.violation_detail, v.severity, v.detected_at, v.created_at, COALESCE(p.name, '')
		 FROM gov_policy_violations v
		 LEFT JOIN gov_policies p ON p.id = v.policy_id
		 WHERE v.id = ?`,
		id,
	)
	var v PolicyViolation
	err := row.Scan(&v.ID, &v.ConnectionID, &v.PolicyID, &v.QueryLogID, &v.User, &v.ViolationDetail, &v.Severity, &v.DetectedAt, &v.CreatedAt, &v.PolicyName)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get violation by id: %w", err)
	}
	return &v, nil
}

func (s *Store) ListIncidentComments(incidentID string, limit int) ([]IncidentComment, error) {
	if limit <= 0 {
		limit = 200
	}
	if limit > 2000 {
		limit = 2000
	}
	rows, err := s.conn().Query(
		`SELECT id, incident_id, comment_text, created_by, created_at
		 FROM gov_incident_comments
		 WHERE incident_id = ?
		 ORDER BY created_at ASC
		 LIMIT ?`,
		incidentID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list incident comments: %w", err)
	}
	defer rows.Close()

	out := make([]IncidentComment, 0)
	for rows.Next() {
		var item IncidentComment
		var createdBy sql.NullString
		if err := rows.Scan(&item.ID, &item.IncidentID, &item.CommentText, &createdBy, &item.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan incident comment: %w", err)
		}
		item.CreatedBy = nullStringToPtr(createdBy)
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate incident comments: %w", err)
	}
	return out, nil
}

func (s *Store) CreateIncidentComment(incidentID, commentText, createdBy string) (string, error) {
	id := uuid.NewString()
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := s.conn().Exec(
		`INSERT INTO gov_incident_comments (id, incident_id, comment_text, created_by, created_at)
		 VALUES (?, ?, ?, ?, ?)`,
		id,
		incidentID,
		strings.TrimSpace(commentText),
		nullableValue(strings.TrimSpace(createdBy)),
		now,
	); err != nil {
		return "", fmt.Errorf("create incident comment: %w", err)
	}
	if _, err := s.conn().Exec(
		`UPDATE gov_incidents
		 SET updated_at = ?
		 WHERE id = ?`,
		now, incidentID,
	); err != nil {
		return "", fmt.Errorf("touch incident after comment: %w", err)
	}
	return id, nil
}

func scanIncident(scanner interface {
	Scan(dest ...interface{}) error
}) (Incident, error) {
	var item Incident
	var sourceRef, dedupeKey, assignee, details, resolutionNote, resolvedAt, createdBy sql.NullString
	err := scanner.Scan(
		&item.ID,
		&item.ConnectionID,
		&item.SourceType,
		&sourceRef,
		&dedupeKey,
		&item.Title,
		&item.Severity,
		&item.Status,
		&assignee,
		&details,
		&resolutionNote,
		&item.OccurrenceCount,
		&item.FirstSeenAt,
		&item.LastSeenAt,
		&resolvedAt,
		&createdBy,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	if err != nil {
		return item, err
	}
	item.SourceRef = nullStringToPtr(sourceRef)
	item.DedupeKey = nullStringToPtr(dedupeKey)
	item.Assignee = nullStringToPtr(assignee)
	item.Details = nullStringToPtr(details)
	item.ResolutionNote = nullStringToPtr(resolutionNote)
	item.ResolvedAt = nullStringToPtr(resolvedAt)
	item.CreatedBy = nullStringToPtr(createdBy)
	return item, nil
}

func nullableValue(v string) interface{} {
	trimmed := strings.TrimSpace(v)
	if trimmed == "" {
		return nil
	}
	return trimmed
}
