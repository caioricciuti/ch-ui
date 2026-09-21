// SPDX-License-Identifier: BUSL-1.1
package incidenttimeline

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/caioricciuti/ch-ui/internal/database"
)

type Store struct{ DB *database.DB }

func (s Store) IncidentExists(ctx context.Context, connectionID, id string) (bool, error) {
	var count int
	err := s.DB.Conn().QueryRowContext(ctx, `SELECT count(*) FROM gov_incidents WHERE connection_id = ? AND id = ?`, connectionID, id).Scan(&count)
	return count > 0, err
}

// Local sources are bounded, connection-scoped and read only after admin authorization.
func (s Store) Local(ctx context.Context, connectionID, source string, window Window) ([]Event, error) {
	var query string
	switch source {
	case "deployments":
		query = `SELECT id, occurred_at, 'deployment', title, details, 'info', '', created_by FROM incident_deployment_annotations WHERE connection_id = ? AND julianday(occurred_at) >= julianday(?) AND julianday(occurred_at) < julianday(?) ORDER BY julianday(occurred_at), id LIMIT ?`
	case "incidents":
		query = `SELECT id, first_seen_at, 'incident', title, COALESCE(details,''), severity, id, COALESCE(created_by,'') FROM gov_incidents WHERE connection_id = ? AND julianday(first_seen_at) >= julianday(?) AND julianday(first_seen_at) < julianday(?) ORDER BY julianday(first_seen_at), id LIMIT ?`
	case "comments":
		query = `SELECT c.id, c.created_at, 'comment', 'Incident comment', c.comment_text, 'info', c.incident_id, COALESCE(c.created_by,'') FROM gov_incident_comments c JOIN gov_incidents i ON i.id = c.incident_id WHERE i.connection_id = ? AND julianday(c.created_at) >= julianday(?) AND julianday(c.created_at) < julianday(?) ORDER BY julianday(c.created_at), c.id LIMIT ?`
	case "health":
		return s.health(ctx, connectionID, window)
	default:
		return nil, fmt.Errorf("unknown timeline source")
	}
	rows, err := s.DB.Conn().QueryContext(ctx, query, connectionID, window.From.Format(time.RFC3339Nano), window.To.Format(time.RFC3339Nano), Limit+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	events := []Event{}
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.ID, &e.At, &e.Kind, &e.Title, &e.Details, &e.Severity, &e.IncidentID, &e.Actor); err != nil {
			return nil, err
		}
		e.At = normalizeTime(e.At)
		e.Source = source
		if e.Kind != "deployment" {
			e.ID = source + ":" + e.ID
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

func normalizeTime(value string) string {
	for _, format := range []string{time.RFC3339Nano, "2006-01-02 15:04:05"} {
		if t, err := time.Parse(format, value); err == nil {
			return t.UTC().Format(time.RFC3339Nano)
		}
	}
	return value
}

func (s Store) health(ctx context.Context, connectionID string, window Window) ([]Event, error) {
	rows, err := s.DB.Conn().QueryContext(ctx, `SELECT node, captured_at, replication_max_delay, replication_queue_total, replicas_readonly, parts_pressure_pct, parts_max_active, mutations_pending
	FROM ch_health_samples WHERE connection_id = ? AND julianday(captured_at) >= julianday(?) AND julianday(captured_at) < julianday(?)
	AND (replication_max_delay >= 30 OR replicas_readonly > 0 OR parts_pressure_pct >= 80)
	ORDER BY julianday(captured_at), node LIMIT ?`, connectionID, window.From.Format(time.RFC3339Nano), window.To.Format(time.RFC3339Nano), Limit+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	events := []Event{}
	for rows.Next() {
		var node, at string
		var queue, readonly, parts, mutations int64
		var delay float64
		var pressure sql.NullFloat64
		if err := rows.Scan(&node, &at, &delay, &queue, &readonly, &pressure, &parts, &mutations); err != nil {
			return nil, err
		}
		events = append(events, Event{ID: "health:" + at + ":" + node, At: normalizeTime(at), Source: "health", Kind: "pressure", Title: "Cluster pressure observed on " + node, Severity: "warn",
			Details: "Retained sample: replication delay ≥ 30s, readonly replicas, or parts pressure ≥ 80%. This is an observation, not a detected cause.",
			Values:  map[string]interface{}{"replication_delay_seconds": delay, "replication_queue": queue, "readonly_replicas": readonly, "parts_pressure_pct": pressure.Float64, "active_parts": parts, "mutations_pending": mutations}})
	}
	return events, rows.Err()
}
