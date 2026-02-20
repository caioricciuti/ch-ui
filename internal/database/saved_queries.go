package database

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// SavedQuery represents a saved SQL query.
type SavedQuery struct {
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	Description  *string `json:"description"`
	Query        string  `json:"query"`
	ConnectionID *string `json:"connection_id"`
	CreatedBy    *string `json:"created_by"`
	CreatedAt    string  `json:"created_at"`
	UpdatedAt    string  `json:"updated_at"`
}

// CreateSavedQueryParams holds parameters for creating a saved query.
type CreateSavedQueryParams struct {
	Name         string
	Description  string
	Query        string
	ConnectionID string
	CreatedBy    string
}

// GetSavedQueries retrieves all saved queries.
func (db *DB) GetSavedQueries() ([]SavedQuery, error) {
	rows, err := db.conn.Query(
		`SELECT id, name, description, query, connection_id, created_by, created_at, updated_at
		 FROM saved_queries ORDER BY updated_at DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("get saved queries: %w", err)
	}
	defer rows.Close()

	var queries []SavedQuery
	for rows.Next() {
		var q SavedQuery
		var desc, connID, createdBy sql.NullString
		if err := rows.Scan(&q.ID, &q.Name, &desc, &q.Query, &connID, &createdBy, &q.CreatedAt, &q.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan saved query: %w", err)
		}
		q.Description = nullStringToPtr(desc)
		q.ConnectionID = nullStringToPtr(connID)
		q.CreatedBy = nullStringToPtr(createdBy)
		queries = append(queries, q)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate saved query rows: %w", err)
	}
	return queries, nil
}

// GetSavedQueryByID retrieves a saved query by ID.
func (db *DB) GetSavedQueryByID(id string) (*SavedQuery, error) {
	row := db.conn.QueryRow(
		`SELECT id, name, description, query, connection_id, created_by, created_at, updated_at
		 FROM saved_queries WHERE id = ?`, id,
	)

	var q SavedQuery
	var desc, connID, createdBy sql.NullString
	err := row.Scan(&q.ID, &q.Name, &desc, &q.Query, &connID, &createdBy, &q.CreatedAt, &q.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get saved query by id: %w", err)
	}
	q.Description = nullStringToPtr(desc)
	q.ConnectionID = nullStringToPtr(connID)
	q.CreatedBy = nullStringToPtr(createdBy)
	return &q, nil
}

// CreateSavedQuery creates a new saved query and returns its ID.
func (db *DB) CreateSavedQuery(params CreateSavedQueryParams) (string, error) {
	id := uuid.NewString()
	now := time.Now().UTC().Format(time.RFC3339)

	var desc, connID, createdBy interface{}
	if params.Description != "" {
		desc = params.Description
	}
	if params.ConnectionID != "" {
		connID = params.ConnectionID
	}
	if params.CreatedBy != "" {
		createdBy = params.CreatedBy
	}

	_, err := db.conn.Exec(
		`INSERT INTO saved_queries (id, name, description, query, connection_id, created_by, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		id, params.Name, desc, params.Query, connID, createdBy, now, now,
	)
	if err != nil {
		return "", fmt.Errorf("create saved query: %w", err)
	}
	return id, nil
}

// UpdateSavedQuery updates an existing saved query.
func (db *DB) UpdateSavedQuery(id, name, description, query, connectionID string) error {
	now := time.Now().UTC().Format(time.RFC3339)

	var desc, connID interface{}
	if description != "" {
		desc = description
	}
	if connectionID != "" {
		connID = connectionID
	}

	_, err := db.conn.Exec(
		`UPDATE saved_queries SET name = ?, description = ?, query = ?, connection_id = ?, updated_at = ? WHERE id = ?`,
		name, desc, query, connID, now, id,
	)
	if err != nil {
		return fmt.Errorf("update saved query: %w", err)
	}
	return nil
}

// DeleteSavedQuery deletes a saved query by ID.
func (db *DB) DeleteSavedQuery(id string) error {
	_, err := db.conn.Exec("DELETE FROM saved_queries WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete saved query: %w", err)
	}
	return nil
}
