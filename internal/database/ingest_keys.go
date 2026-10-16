package database

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// IngestKey is a credential a shipper sends to /ingest for one pipeline. The
// plaintext is shown once at creation; only its SHA-256 hex digest is stored.
type IngestKey struct {
	ID         string  `json:"id"`
	PipelineID string  `json:"-"`
	Name       string  `json:"name"`
	KeyHash    string  `json:"-"`
	Prefix     string  `json:"prefix"`
	CreatedBy  string  `json:"created_by"`
	CreatedAt  string  `json:"created_at"`
	LastUsedAt *string `json:"last_used_at"`
	RevokedAt  *string `json:"revoked_at"`
}

const ingestKeyColumns = `id, pipeline_id, name, key_hash, key_prefix, created_by, created_at, last_used_at, revoked_at`

// CreateIngestKey stores a new key for a pipeline.
func (db *DB) CreateIngestKey(pipelineID, name, keyHash, prefix, createdBy string) (*IngestKey, error) {
	k := &IngestKey{
		ID:         uuid.New().String(),
		PipelineID: pipelineID,
		Name:       name,
		KeyHash:    keyHash,
		Prefix:     prefix,
		CreatedBy:  createdBy,
		CreatedAt:  time.Now().UTC().Format(time.RFC3339),
	}
	_, err := db.conn.Exec(
		`INSERT INTO ingest_keys (id, pipeline_id, name, key_hash, key_prefix, created_by, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		k.ID, k.PipelineID, k.Name, k.KeyHash, k.Prefix, k.CreatedBy, k.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("create ingest key: %w", err)
	}
	return k, nil
}

// ListIngestKeys returns a pipeline's keys, revoked ones included, newest first.
func (db *DB) ListIngestKeys(pipelineID string) ([]IngestKey, error) {
	rows, err := db.conn.Query(`SELECT `+ingestKeyColumns+` FROM ingest_keys WHERE pipeline_id = ? ORDER BY created_at DESC, id`, pipelineID)
	if err != nil {
		return nil, fmt.Errorf("list ingest keys: %w", err)
	}
	defer rows.Close()
	out := []IngestKey{}
	for rows.Next() {
		k, err := scanIngestKey(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *k)
	}
	return out, rows.Err()
}

// GetIngestKeyByHash returns the key with this hash, revoked or not, or nil.
// Callers check RevokedAt, so a revoked key can still be attributed to its
// pipeline in the rejection counters.
func (db *DB) GetIngestKeyByHash(keyHash string) (*IngestKey, error) {
	k, err := scanIngestKey(db.conn.QueryRow(`SELECT `+ingestKeyColumns+` FROM ingest_keys WHERE key_hash = ?`, keyHash))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return k, err
}

// GetIngestKey returns one key of a pipeline, or nil.
func (db *DB) GetIngestKey(pipelineID, id string) (*IngestKey, error) {
	k, err := scanIngestKey(db.conn.QueryRow(`SELECT `+ingestKeyColumns+` FROM ingest_keys WHERE id = ? AND pipeline_id = ?`, id, pipelineID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return k, err
}

// RevokeIngestKey sets revoked_at. It reports false when the key does not
// exist on that pipeline or was already revoked.
func (db *DB) RevokeIngestKey(pipelineID, id string) (bool, error) {
	res, err := db.conn.Exec(
		`UPDATE ingest_keys SET revoked_at = ? WHERE id = ? AND pipeline_id = ? AND revoked_at IS NULL`,
		time.Now().UTC().Format(time.RFC3339), id, pipelineID,
	)
	if err != nil {
		return false, fmt.Errorf("revoke ingest key: %w", err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// TouchIngestKey records key usage. Best effort; callers throttle it.
func (db *DB) TouchIngestKey(id string, at time.Time) error {
	_, err := db.conn.Exec(`UPDATE ingest_keys SET last_used_at = ? WHERE id = ?`, at.UTC().Format(time.RFC3339), id)
	return err
}

func scanIngestKey(row rowScanner) (*IngestKey, error) {
	var k IngestKey
	err := row.Scan(&k.ID, &k.PipelineID, &k.Name, &k.KeyHash, &k.Prefix, &k.CreatedBy, &k.CreatedAt, &k.LastUsedAt, &k.RevokedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		return nil, fmt.Errorf("scan ingest key: %w", err)
	}
	return &k, nil
}
