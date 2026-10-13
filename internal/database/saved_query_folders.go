package database

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// SavedQueryFolder groups saved queries; folders nest through parent_id and
// are global (every user sees the same tree).
type SavedQueryFolder = DashboardFolder

// ListSavedQueryFolders returns every folder ordered by name.
func (db *DB) ListSavedQueryFolders() ([]SavedQueryFolder, error) {
	rows, err := db.conn.Query("SELECT " + folderColumns + " FROM saved_query_folders ORDER BY name COLLATE NOCASE")
	if err != nil {
		return nil, fmt.Errorf("list saved query folders: %w", err)
	}
	defer rows.Close()
	out := make([]SavedQueryFolder, 0)
	for rows.Next() {
		f, err := scanFolder(rows)
		if err != nil {
			return nil, fmt.Errorf("scan saved query folder: %w", err)
		}
		out = append(out, *f)
	}
	return out, rows.Err()
}

// GetSavedQueryFolder returns one folder or nil.
func (db *DB) GetSavedQueryFolder(id string) (*SavedQueryFolder, error) {
	row := db.conn.QueryRow("SELECT "+folderColumns+" FROM saved_query_folders WHERE id = ?", id)
	f, err := scanFolder(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get saved query folder: %w", err)
	}
	return f, nil
}

// CreateSavedQueryFolder creates a folder under parentID ("" for the root).
// A sibling with the same name returns ErrFolderNameTaken.
func (db *DB) CreateSavedQueryFolder(name, parentID, createdBy string) (string, error) {
	id := uuid.NewString()
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.conn.Exec(
		"INSERT INTO saved_query_folders (id, name, parent_id, created_by, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)",
		id, strings.TrimSpace(name), nilIfEmpty(parentID), nilIfEmpty(createdBy), now, now,
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return "", ErrFolderNameTaken
		}
		return "", fmt.Errorf("create saved query folder: %w", err)
	}
	return id, nil
}

// UpdateSavedQueryFolder renames a folder and/or moves it under a new parent
// (nil parent: keep; empty string: root).
func (db *DB) UpdateSavedQueryFolder(id string, name *string, parentID *string) error {
	existing, err := db.GetSavedQueryFolder(id)
	if err != nil {
		return err
	}
	if existing == nil {
		return sql.ErrNoRows
	}
	newName := existing.Name
	if name != nil && strings.TrimSpace(*name) != "" {
		newName = strings.TrimSpace(*name)
	}
	var parent interface{}
	if existing.ParentID != nil {
		parent = *existing.ParentID
	}
	if parentID != nil {
		if *parentID == "" {
			parent = nil
		} else {
			if *parentID == id {
				return ErrFolderCycle
			}
			ids, err := db.savedQueryFolderSubtree(id)
			if err != nil {
				return err
			}
			for _, d := range ids {
				if d == *parentID {
					return ErrFolderCycle
				}
			}
			parent = *parentID
		}
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = db.conn.Exec("UPDATE saved_query_folders SET name = ?, parent_id = ?, updated_at = ? WHERE id = ?", newName, parent, now, id)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return ErrFolderNameTaken
		}
		return fmt.Errorf("update saved query folder: %w", err)
	}
	return nil
}

// savedQueryFolderSubtree returns the id of a folder and of every folder
// below it.
func (db *DB) savedQueryFolderSubtree(id string) ([]string, error) {
	rows, err := db.conn.Query(
		`WITH RECURSIVE sub(id) AS (
			SELECT id FROM saved_query_folders WHERE id = ?
			UNION SELECT f.id FROM saved_query_folders f JOIN sub ON f.parent_id = sub.id
		) SELECT id FROM sub`, id,
	)
	if err != nil {
		return nil, fmt.Errorf("walk saved query folders: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, fmt.Errorf("scan saved query folder id: %w", err)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// DeleteSavedQueryFolder removes a folder and its subfolders. The saved
// queries inside any of them move to the root; none are deleted. It returns
// how many queries moved. folder_id is cleared here rather than left to the
// foreign key, which ALTER TABLE ADD COLUMN cannot always enforce.
func (db *DB) DeleteSavedQueryFolder(id string) (int64, error) {
	ids, err := db.savedQueryFolderSubtree(id)
	if err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, sql.ErrNoRows
	}
	tx, err := db.conn.Begin()
	if err != nil {
		return 0, fmt.Errorf("begin delete folder: %w", err)
	}
	defer tx.Rollback()

	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]interface{}, 0, len(ids))
	for _, s := range ids {
		args = append(args, s)
	}
	res, err := tx.Exec("UPDATE saved_queries SET folder_id = NULL WHERE folder_id IN ("+placeholders+")", args...)
	if err != nil {
		return 0, fmt.Errorf("move saved queries to root: %w", err)
	}
	moved, _ := res.RowsAffected()
	// The IN list covers the subfolders too, so this works with or without
	// the ON DELETE CASCADE foreign key.
	if _, err := tx.Exec("DELETE FROM saved_query_folders WHERE id IN ("+placeholders+")", args...); err != nil {
		return 0, fmt.Errorf("delete saved query folder: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit delete folder: %w", err)
	}
	return moved, nil
}
