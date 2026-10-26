package database

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// savedQueryVersionsKept is how many versions each saved query keeps.
const savedQueryVersionsKept = 50

// SavedQuerySnapshot is the content of a saved query at one point in time.
type SavedQuerySnapshot struct {
	Name         string `json:"name"`
	Description  string `json:"description"`
	Query        string `json:"query"`
	Parameters   string `json:"parameters"`
	ConnectionID string `json:"connection_id"`
	Verified     bool   `json:"verified"`
}

// SavedQueryVersion is one recorded state of a saved query.
type SavedQueryVersion struct {
	ID           string              `json:"id"`
	SavedQueryID string              `json:"saved_query_id"`
	Version      int                 `json:"version"`
	Snapshot     *SavedQuerySnapshot `json:"snapshot,omitempty"`
	CreatedBy    *string             `json:"created_by"`
	CreatedAt    string              `json:"created_at"`
	Note         string              `json:"note"`
}

func snapshotOf(q *SavedQuery) SavedQuerySnapshot {
	s := SavedQuerySnapshot{Name: q.Name, Query: q.Query, Verified: q.Verified}
	if q.Description != nil {
		s.Description = *q.Description
	}
	if q.Parameters != nil {
		s.Parameters = *q.Parameters
	}
	if q.ConnectionID != nil {
		s.ConnectionID = *q.ConnectionID
	}
	return s
}

// RecordSavedQueryVersion saves the current state of a saved query as a new
// version. Nothing is written when the state equals the latest version.
// Only the latest 50 versions are kept. It returns the version number
// written (0 when nothing changed or the query does not exist).
func (db *DB) RecordSavedQueryVersion(savedQueryID, createdBy, note string) (int, error) {
	q, err := db.GetSavedQueryByID(savedQueryID)
	if err != nil || q == nil {
		return 0, err
	}
	raw, err := json.Marshal(snapshotOf(q))
	if err != nil {
		return 0, fmt.Errorf("encode snapshot: %w", err)
	}

	var latest string
	err = db.conn.QueryRow(
		`SELECT snapshot FROM saved_query_versions WHERE saved_query_id = ? ORDER BY version DESC LIMIT 1`, savedQueryID,
	).Scan(&latest)
	if err != nil && err != sql.ErrNoRows {
		return 0, fmt.Errorf("get latest saved query version: %w", err)
	}
	if err == nil && latest == string(raw) {
		return 0, nil
	}

	now := time.Now().UTC().Format(time.RFC3339)
	// One statement computes the next number, so concurrent saves cannot
	// pick the same one.
	if _, err := db.conn.Exec(
		`INSERT INTO saved_query_versions (id, saved_query_id, version, snapshot, created_by, created_at, note)
		 SELECT ?, ?, COALESCE(MAX(version), 0) + 1, ?, ?, ?, ? FROM saved_query_versions WHERE saved_query_id = ?`,
		uuid.NewString(), savedQueryID, string(raw), nilIfEmpty(createdBy), now, note, savedQueryID,
	); err != nil {
		return 0, fmt.Errorf("insert saved query version: %w", err)
	}
	var version int
	if err := db.conn.QueryRow("SELECT MAX(version) FROM saved_query_versions WHERE saved_query_id = ?", savedQueryID).Scan(&version); err != nil {
		return 0, fmt.Errorf("read saved query version: %w", err)
	}
	if _, err := db.conn.Exec("DELETE FROM saved_query_versions WHERE saved_query_id = ? AND version <= ?", savedQueryID, version-savedQueryVersionsKept); err != nil {
		return version, fmt.Errorf("prune saved query versions: %w", err)
	}
	return version, nil
}

// EnsureSavedQueryBaseline records the current state of a saved query before
// its first recorded change, so the state from before history existed can
// be restored. Queries created before versions existed have no history.
func (db *DB) EnsureSavedQueryBaseline(savedQueryID string) error {
	var n int
	if err := db.conn.QueryRow("SELECT COUNT(*) FROM saved_query_versions WHERE saved_query_id = ?", savedQueryID).Scan(&n); err != nil {
		return fmt.Errorf("count saved query versions: %w", err)
	}
	if n > 0 {
		return nil
	}
	_, err := db.RecordSavedQueryVersion(savedQueryID, "", "Before first recorded change")
	return err
}

// ListSavedQueryVersions returns the versions of a saved query, newest
// first, without their snapshots.
func (db *DB) ListSavedQueryVersions(savedQueryID string) ([]SavedQueryVersion, error) {
	rows, err := db.conn.Query(
		`SELECT id, saved_query_id, version, created_by, created_at, note
		 FROM saved_query_versions WHERE saved_query_id = ? ORDER BY version DESC`, savedQueryID,
	)
	if err != nil {
		return nil, fmt.Errorf("list saved query versions: %w", err)
	}
	defer rows.Close()
	out := []SavedQueryVersion{}
	for rows.Next() {
		var v SavedQueryVersion
		var createdBy sql.NullString
		if err := rows.Scan(&v.ID, &v.SavedQueryID, &v.Version, &createdBy, &v.CreatedAt, &v.Note); err != nil {
			return nil, fmt.Errorf("scan saved query version: %w", err)
		}
		v.CreatedBy = nullStringToPtr(createdBy)
		out = append(out, v)
	}
	return out, rows.Err()
}

// GetSavedQueryVersion returns one version with its snapshot, or nil.
func (db *DB) GetSavedQueryVersion(savedQueryID string, version int) (*SavedQueryVersion, error) {
	var v SavedQueryVersion
	var createdBy sql.NullString
	var raw string
	err := db.conn.QueryRow(
		`SELECT id, saved_query_id, version, snapshot, created_by, created_at, note
		 FROM saved_query_versions WHERE saved_query_id = ? AND version = ?`, savedQueryID, version,
	).Scan(&v.ID, &v.SavedQueryID, &v.Version, &raw, &createdBy, &v.CreatedAt, &v.Note)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get saved query version: %w", err)
	}
	var snap SavedQuerySnapshot
	if err := json.Unmarshal([]byte(raw), &snap); err != nil {
		return nil, fmt.Errorf("decode saved query snapshot: %w", err)
	}
	v.Snapshot = &snap
	v.CreatedBy = nullStringToPtr(createdBy)
	return &v, nil
}

// RestoreSavedQueryVersion writes the content of a version back to the
// saved query, keeping its current connection, and records the result as a new version noted
// "Restored version N". It returns the new version number (0 when the
// current state already equals that version) or sql.ErrNoRows when the
// version does not exist.
func (db *DB) RestoreSavedQueryVersion(savedQueryID string, version int, actor string) (int, error) {
	v, err := db.GetSavedQueryVersion(savedQueryID, version)
	if err != nil {
		return 0, err
	}
	if v == nil || v.Snapshot == nil {
		return 0, sql.ErrNoRows
	}
	if err := db.EnsureSavedQueryBaseline(savedQueryID); err != nil {
		return 0, err
	}
	s := *v.Snapshot
	// The query keeps its current connection: a restore must not move it
	// back to a connection the person restoring may not be signed in to.
	current, err := db.GetSavedQueryByID(savedQueryID)
	if err != nil {
		return 0, err
	}
	if current == nil {
		return 0, sql.ErrNoRows
	}
	s.ConnectionID = ""
	if current.ConnectionID != nil {
		s.ConnectionID = *current.ConnectionID
	}
	if err := db.UpdateSavedQuery(savedQueryID, UpdateSavedQueryParams{
		Name: s.Name, Description: s.Description, Query: s.Query, Parameters: s.Parameters,
		ConnectionID: s.ConnectionID, Verified: s.Verified,
	}); err != nil {
		return 0, err
	}
	return db.RecordSavedQueryVersion(savedQueryID, actor, fmt.Sprintf("Restored version %d", version))
}
