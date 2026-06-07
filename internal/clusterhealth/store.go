package clusterhealth

import (
	"database/sql"
	"fmt"

	"github.com/caioricciuti/ch-ui/internal/database"
)

// Store provides SQLite persistence for cluster-health settings and the
// per-node time-series samples.
type Store struct {
	db *database.DB
}

// NewStore creates a new cluster-health Store.
func NewStore(db *database.DB) *Store {
	return &Store{db: db}
}

func (s *Store) conn() *sql.DB {
	return s.db.Conn()
}

// GetSettings returns the stored settings for a connection, or DefaultSettings
// when no row exists yet.
func (s *Store) GetSettings(connectionID string) (Settings, error) {
	row := s.conn().QueryRow(
		`SELECT connection_id, enabled, retention_days, poll_interval_seconds,
		        long_query_threshold_seconds, COALESCE(updated_at, '')
		 FROM ch_health_settings WHERE connection_id = ?`, connectionID,
	)
	var st Settings
	var enabled int
	err := row.Scan(&st.ConnectionID, &enabled, &st.RetentionDays, &st.PollIntervalSeconds,
		&st.LongQueryThresholdSecs, &st.UpdatedAt)
	if err == sql.ErrNoRows {
		return DefaultSettings(connectionID), nil
	}
	if err != nil {
		return DefaultSettings(connectionID), fmt.Errorf("get cluster-health settings: %w", err)
	}
	st.Enabled = enabled != 0
	return st.Clamp(), nil
}

// UpsertSettings stores the settings for a connection, clamping every field into
// its valid range first.
func (s *Store) UpsertSettings(in Settings) (Settings, error) {
	out := in.Clamp()
	enabled := 0
	if out.Enabled {
		enabled = 1
	}
	_, err := s.conn().Exec(
		`INSERT INTO ch_health_settings
		   (connection_id, enabled, retention_days, poll_interval_seconds, long_query_threshold_seconds, updated_at)
		 VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		 ON CONFLICT(connection_id) DO UPDATE SET
		   enabled = excluded.enabled,
		   retention_days = excluded.retention_days,
		   poll_interval_seconds = excluded.poll_interval_seconds,
		   long_query_threshold_seconds = excluded.long_query_threshold_seconds,
		   updated_at = CURRENT_TIMESTAMP`,
		out.ConnectionID, enabled, out.RetentionDays, out.PollIntervalSeconds, out.LongQueryThresholdSecs,
	)
	if err != nil {
		return out, fmt.Errorf("upsert cluster-health settings: %w", err)
	}
	return out, nil
}

// InsertSamples persists one poll's worth of per-node samples for a connection.
func (s *Store) InsertSamples(connectionID, cluster string, samples []Sample) error {
	if len(samples) == 0 {
		return nil
	}
	tx, err := s.conn().Begin()
	if err != nil {
		return fmt.Errorf("begin sample insert: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // rolled back unless committed

	stmt, err := tx.Prepare(
		`INSERT INTO ch_health_samples
		   (connection_id, cluster, node, captured_at, replication_max_delay, replication_queue_total,
		    replicas_readonly, merges_running, mutations_pending, parts_max_active, parts_pressure_pct, long_queries)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
	)
	if err != nil {
		return fmt.Errorf("prepare sample insert: %w", err)
	}
	defer stmt.Close()

	for _, sm := range samples {
		if _, err := stmt.Exec(connectionID, cluster, sm.Node, sm.CapturedAt,
			sm.ReplicationMaxDelay, sm.ReplicationQueueTotal, sm.ReplicasReadonly,
			sm.MergesRunning, sm.MutationsPending, sm.PartsMaxActive, sm.PartsPressurePct, sm.LongQueries,
		); err != nil {
			return fmt.Errorf("insert sample: %w", err)
		}
	}
	return tx.Commit()
}

// HistorySample is a stored sample annotated with the node it belongs to, for
// charting trends over time.
type HistorySample struct {
	Node string `json:"node"`
	Sample
}

// GetHistory returns samples for a connection captured at or after `since`
// (RFC3339), oldest first — ready to plot as a time-series.
func (s *Store) GetHistory(connectionID, since string) ([]HistorySample, error) {
	rows, err := s.conn().Query(
		`SELECT node, captured_at, replication_max_delay, replication_queue_total, replicas_readonly,
		        merges_running, mutations_pending, parts_max_active, parts_pressure_pct, long_queries
		 FROM ch_health_samples
		 WHERE connection_id = ? AND captured_at >= ?
		 ORDER BY captured_at ASC`, connectionID, since,
	)
	if err != nil {
		return nil, fmt.Errorf("get cluster-health history: %w", err)
	}
	defer rows.Close()

	var out []HistorySample
	for rows.Next() {
		var hs HistorySample
		if err := rows.Scan(&hs.Node, &hs.CapturedAt, &hs.ReplicationMaxDelay, &hs.ReplicationQueueTotal,
			&hs.ReplicasReadonly, &hs.MergesRunning, &hs.MutationsPending, &hs.PartsMaxActive,
			&hs.PartsPressurePct, &hs.LongQueries); err != nil {
			return nil, fmt.Errorf("scan history sample: %w", err)
		}
		out = append(out, hs)
	}
	return out, rows.Err()
}

// CleanupOldSamples deletes samples older than the given timestamp for a
// connection, bounding SQLite growth per the configured retention.
func (s *Store) CleanupOldSamples(connectionID, before string) (int64, error) {
	res, err := s.conn().Exec(
		`DELETE FROM ch_health_samples WHERE connection_id = ? AND captured_at < ?`,
		connectionID, before,
	)
	if err != nil {
		return 0, fmt.Errorf("cleanup old cluster-health samples: %w", err)
	}
	return res.RowsAffected()
}
