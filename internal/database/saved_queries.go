package database

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Saved query visibility levels.
const (
	// SavedQueryPrivate is visible to its owner only.
	SavedQueryPrivate = "private"
	// SavedQueryShared is visible to everyone (the default, and what every
	// query created before visibility existed became).
	SavedQueryShared = "shared"
	// SavedQueryAdmins is visible to admins and the owner.
	SavedQueryAdmins = "admins"
)

// ValidSavedQueryVisibility reports whether v is a known visibility level.
func ValidSavedQueryVisibility(v string) bool {
	return v == SavedQueryPrivate || v == SavedQueryShared || v == SavedQueryAdmins
}

// SavedQuery represents a saved SQL query.
type SavedQuery struct {
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	Description  *string `json:"description"`
	Query        string  `json:"query"`
	Parameters   *string `json:"parameters"` // JSON object of default {name: value} bindings
	ConnectionID *string `json:"connection_id"`
	CreatedBy    *string `json:"created_by"`
	// Verified marks a query a human has reviewed as correct. AI clients
	// (MCP run_saved_query) surface it so the model prefers vetted SQL.
	Verified   bool     `json:"verified"`
	FolderID   *string  `json:"folder_id"`
	Tags       []string `json:"tags"`
	Visibility string   `json:"visibility"`
	// Owner is the creator, or "" for legacy rows created before owners
	// were recorded.
	Owner string `json:"owner"`
	// CanEdit is filled in per request: whether the caller may change it.
	CanEdit   bool   `json:"can_edit"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// SavedQueryViewer is who is looking at saved queries: the person (or MCP
// key) and their CH-UI role. AllOwners lets an admin see other people's
// private queries (offboarding); it has no effect for other roles.
type SavedQueryViewer struct {
	Actor     string
	Role      string
	AllOwners bool
}

// savedQueryVisibleTo is the one visibility rule for saved queries, used by
// the REST API, the scheduler form, Brain tools and the MCP server:
// shared is visible to everyone, private to its owner, admins to admins and
// the owner. An admin with AllOwners sees everything.
func savedQueryVisibleTo(q *SavedQuery, v SavedQueryViewer) bool {
	if q == nil {
		return false
	}
	if q.CreatedBy != nil && *q.CreatedBy != "" && *q.CreatedBy == v.Actor {
		return true
	}
	switch q.Visibility {
	case SavedQueryPrivate:
	case SavedQueryAdmins:
		if v.Role == "admin" {
			return true
		}
	default:
		return true
	}
	return v.Role == "admin" && v.AllOwners
}

// SavedQueryVisibleTo reports whether the viewer may see the query.
func SavedQueryVisibleTo(q *SavedQuery, v SavedQueryViewer) bool {
	return savedQueryVisibleTo(q, v)
}

// CanEditSavedQuery reports whether the viewer may edit, move, tag, delete,
// change the visibility of, or restore a query: admins always, analysts when
// they own it or when it is a legacy row without an owner. Viewers never.
func CanEditSavedQuery(q *SavedQuery, v SavedQueryViewer) bool {
	if q == nil {
		return false
	}
	switch v.Role {
	case "admin":
		return true
	case "analyst":
		return q.CreatedBy == nil || *q.CreatedBy == "" || *q.CreatedBy == v.Actor
	default:
		return false
	}
}

// CanVerifySavedQuery reports whether the viewer may change the verified
// flag: the owner or an admin.
func CanVerifySavedQuery(q *SavedQuery, v SavedQueryViewer) bool {
	if q == nil {
		return false
	}
	if v.Role == "admin" {
		return true
	}
	return v.Role == "analyst" && q.CreatedBy != nil && *q.CreatedBy != "" && *q.CreatedBy == v.Actor
}

// CreateSavedQueryParams holds parameters for creating a saved query.
type CreateSavedQueryParams struct {
	Name         string
	Description  string
	Query        string
	Parameters   string
	ConnectionID string
	CreatedBy    string
	Verified     bool
	FolderID     string
	Tags         []string
	// Visibility defaults to shared when empty.
	Visibility string
}

// UpdateSavedQueryParams holds the updatable content fields of a saved query.
type UpdateSavedQueryParams struct {
	Name         string
	Description  string
	Query        string
	Parameters   string
	ConnectionID string
	Verified     bool
}

const savedQueryColumns = `id, name, description, query, parameters, connection_id, created_by, verified,
	folder_id, COALESCE(tags, '[]'), COALESCE(visibility, 'shared'), created_at, updated_at`

func scanSavedQuery(row interface {
	Scan(dest ...interface{}) error
}) (*SavedQuery, error) {
	var q SavedQuery
	var desc, params, connID, createdBy, folderID sql.NullString
	var tags string
	if err := row.Scan(&q.ID, &q.Name, &desc, &q.Query, &params, &connID, &createdBy, &q.Verified,
		&folderID, &tags, &q.Visibility, &q.CreatedAt, &q.UpdatedAt); err != nil {
		return nil, err
	}
	q.Description = nullStringToPtr(desc)
	q.Parameters = nullStringToPtr(params)
	q.ConnectionID = nullStringToPtr(connID)
	q.CreatedBy = nullStringToPtr(createdBy)
	q.FolderID = nullStringToPtr(folderID)
	q.Tags = decodeTags(tags)
	if q.CreatedBy != nil {
		q.Owner = *q.CreatedBy
	}
	return &q, nil
}

// GetSavedQueries retrieves every saved query regardless of visibility. Use
// it only for internal work (search indexes, tests); anything shown to a
// person or an AI client goes through ListSavedQueriesVisibleTo.
func (db *DB) GetSavedQueries() ([]SavedQuery, error) {
	rows, err := db.conn.Query(`SELECT ` + savedQueryColumns + ` FROM saved_queries ORDER BY updated_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("get saved queries: %w", err)
	}
	defer rows.Close()

	var queries []SavedQuery
	for rows.Next() {
		q, err := scanSavedQuery(rows)
		if err != nil {
			return nil, fmt.Errorf("scan saved query: %w", err)
		}
		queries = append(queries, *q)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate saved query rows: %w", err)
	}
	return queries, nil
}

// ListSavedQueriesVisibleTo returns the saved queries the viewer may see,
// newest first.
func (db *DB) ListSavedQueriesVisibleTo(v SavedQueryViewer) ([]SavedQuery, error) {
	all, err := db.GetSavedQueries()
	if err != nil {
		return nil, err
	}
	out := make([]SavedQuery, 0, len(all))
	for i := range all {
		if savedQueryVisibleTo(&all[i], v) {
			out = append(out, all[i])
		}
	}
	return out, nil
}

// GetSavedQueryByID retrieves a saved query by ID regardless of visibility
// (schedules keep running a query that became private).
func (db *DB) GetSavedQueryByID(id string) (*SavedQuery, error) {
	row := db.conn.QueryRow(`SELECT `+savedQueryColumns+` FROM saved_queries WHERE id = ?`, id)
	q, err := scanSavedQuery(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get saved query by id: %w", err)
	}
	return q, nil
}

// GetSavedQueryVisibleTo returns a saved query when the viewer may see it,
// else nil (callers answer 404, never 403, so private names do not leak).
func (db *DB) GetSavedQueryVisibleTo(id string, v SavedQueryViewer) (*SavedQuery, error) {
	q, err := db.GetSavedQueryByID(id)
	if err != nil || q == nil {
		return nil, err
	}
	if !savedQueryVisibleTo(q, v) {
		return nil, nil
	}
	return q, nil
}

// CreateSavedQuery creates a new saved query and returns its ID.
func (db *DB) CreateSavedQuery(params CreateSavedQueryParams) (string, error) {
	id := uuid.NewString()
	now := time.Now().UTC().Format(time.RFC3339)
	visibility := params.Visibility
	if visibility == "" {
		visibility = SavedQueryShared
	}
	if !ValidSavedQueryVisibility(visibility) {
		return "", fmt.Errorf("invalid visibility %q", visibility)
	}

	_, err := db.conn.Exec(
		`INSERT INTO saved_queries (id, name, description, query, parameters, connection_id, created_by, verified, folder_id, tags, visibility, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, params.Name, nilIfEmpty(params.Description), params.Query, nilIfEmpty(params.Parameters),
		nilIfEmpty(params.ConnectionID), nilIfEmpty(params.CreatedBy), params.Verified,
		nilIfEmpty(params.FolderID), encodeTags(params.Tags), visibility, now, now,
	)
	if err != nil {
		return "", fmt.Errorf("create saved query: %w", err)
	}
	return id, nil
}

// UpdateSavedQuery updates the content fields of an existing saved query.
func (db *DB) UpdateSavedQuery(id string, params UpdateSavedQueryParams) error {
	now := time.Now().UTC().Format(time.RFC3339)

	_, err := db.conn.Exec(
		`UPDATE saved_queries SET name = ?, description = ?, query = ?, parameters = ?, connection_id = ?, verified = ?, updated_at = ? WHERE id = ?`,
		params.Name, nilIfEmpty(params.Description), params.Query, nilIfEmpty(params.Parameters),
		nilIfEmpty(params.ConnectionID), params.Verified, now, id,
	)
	if err != nil {
		return fmt.Errorf("update saved query: %w", err)
	}
	return nil
}

// SetSavedQueryFolder moves a saved query into a folder ("" for the root).
func (db *DB) SetSavedQueryFolder(id, folderID string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.conn.Exec("UPDATE saved_queries SET folder_id = ?, updated_at = ? WHERE id = ?", nilIfEmpty(folderID), now, id); err != nil {
		return fmt.Errorf("move saved query: %w", err)
	}
	return nil
}

// SetSavedQueryTags replaces the tags of a saved query.
func (db *DB) SetSavedQueryTags(id string, tags []string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.conn.Exec("UPDATE saved_queries SET tags = ?, updated_at = ? WHERE id = ?", encodeTags(tags), now, id); err != nil {
		return fmt.Errorf("set saved query tags: %w", err)
	}
	return nil
}

// SetSavedQueryVisibility changes who can see a saved query. A legacy row
// without an owner that stops being shared is claimed by claimOwner, so it
// does not become invisible to everyone but admins.
func (db *DB) SetSavedQueryVisibility(id, visibility, claimOwner string) error {
	if !ValidSavedQueryVisibility(visibility) {
		return fmt.Errorf("invalid visibility %q", visibility)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if visibility != SavedQueryShared && claimOwner != "" {
		if _, err := db.conn.Exec(
			"UPDATE saved_queries SET created_by = ? WHERE id = ? AND (created_by IS NULL OR created_by = '')", claimOwner, id,
		); err != nil {
			return fmt.Errorf("claim saved query: %w", err)
		}
	}
	if _, err := db.conn.Exec("UPDATE saved_queries SET visibility = ?, updated_at = ? WHERE id = ?", visibility, now, id); err != nil {
		return fmt.Errorf("set saved query visibility: %w", err)
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

// NormalizeSavedQueryTags trims, de-duplicates and caps tags (20 of 40
// characters), the same limits dashboards use.
func NormalizeSavedQueryTags(tags []string) []string {
	out := make([]string, 0, len(tags))
	seen := map[string]bool{}
	for _, t := range tags {
		t = strings.TrimSpace(t)
		if t == "" || len(t) > 40 || seen[strings.ToLower(t)] {
			continue
		}
		seen[strings.ToLower(t)] = true
		out = append(out, t)
		if len(out) == 20 {
			break
		}
	}
	return out
}

// nilIfEmpty returns nil for an empty string so it stores SQL NULL rather than "".
func nilIfEmpty(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}
