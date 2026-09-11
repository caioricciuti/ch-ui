package database

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// DashboardFolder groups dashboards; folders nest through parent_id.
type DashboardFolder struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	ParentID  *string `json:"parent_id"`
	CreatedBy *string `json:"created_by"`
	CreatedAt string  `json:"created_at"`
	UpdatedAt string  `json:"updated_at"`
}

const folderColumns = "id, name, parent_id, created_by, created_at, updated_at"

func scanFolder(row interface {
	Scan(dest ...interface{}) error
}) (*DashboardFolder, error) {
	var f DashboardFolder
	var parent, createdBy sql.NullString
	if err := row.Scan(&f.ID, &f.Name, &parent, &createdBy, &f.CreatedAt, &f.UpdatedAt); err != nil {
		return nil, err
	}
	f.ParentID = nullStringToPtr(parent)
	f.CreatedBy = nullStringToPtr(createdBy)
	return &f, nil
}

// ListDashboardFolders returns every folder, parents before children by name.
func (db *DB) ListDashboardFolders() ([]DashboardFolder, error) {
	rows, err := db.conn.Query("SELECT " + folderColumns + " FROM dashboard_folders ORDER BY name COLLATE NOCASE")
	if err != nil {
		return nil, fmt.Errorf("list dashboard folders: %w", err)
	}
	defer rows.Close()
	out := make([]DashboardFolder, 0)
	for rows.Next() {
		f, err := scanFolder(rows)
		if err != nil {
			return nil, fmt.Errorf("scan dashboard folder: %w", err)
		}
		out = append(out, *f)
	}
	return out, rows.Err()
}

// GetDashboardFolder returns one folder or nil.
func (db *DB) GetDashboardFolder(id string) (*DashboardFolder, error) {
	row := db.conn.QueryRow("SELECT "+folderColumns+" FROM dashboard_folders WHERE id = ?", id)
	f, err := scanFolder(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get dashboard folder: %w", err)
	}
	return f, nil
}

// CreateDashboardFolder creates a folder under parentID (empty: root).
func (db *DB) CreateDashboardFolder(name, parentID, createdBy string) (string, error) {
	id := uuid.NewString()
	now := time.Now().UTC().Format(time.RFC3339)
	var parent, creator interface{}
	if parentID != "" {
		parent = parentID
	}
	if createdBy != "" {
		creator = createdBy
	}
	_, err := db.conn.Exec(
		"INSERT INTO dashboard_folders (id, name, parent_id, created_by, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)",
		id, strings.TrimSpace(name), parent, creator, now, now,
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return "", ErrFolderNameTaken
		}
		return "", fmt.Errorf("create dashboard folder: %w", err)
	}
	return id, nil
}

// ErrFolderNameTaken is returned when a sibling folder already has the name.
var ErrFolderNameTaken = fmt.Errorf("a folder with that name already exists here")

// ErrFolderCycle is returned when a move would make a folder its own ancestor.
var ErrFolderCycle = fmt.Errorf("a folder cannot be moved inside itself")

// UpdateDashboardFolder renames a folder and/or moves it under a new parent
// (nil parent: keep; empty string: root).
func (db *DB) UpdateDashboardFolder(id string, name *string, parentID *string) error {
	existing, err := db.GetDashboardFolder(id)
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
			isDesc, err := db.folderIsDescendant(*parentID, id)
			if err != nil {
				return err
			}
			if isDesc {
				return ErrFolderCycle
			}
			parent = *parentID
		}
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = db.conn.Exec("UPDATE dashboard_folders SET name = ?, parent_id = ?, updated_at = ? WHERE id = ?", newName, parent, now, id)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return ErrFolderNameTaken
		}
		return fmt.Errorf("update dashboard folder: %w", err)
	}
	return nil
}

// folderIsDescendant reports whether `candidate` sits somewhere under `ancestor`.
func (db *DB) folderIsDescendant(candidate, ancestor string) (bool, error) {
	current := candidate
	for i := 0; i < 64 && current != ""; i++ {
		var parent sql.NullString
		err := db.conn.QueryRow("SELECT parent_id FROM dashboard_folders WHERE id = ?", current).Scan(&parent)
		if err == sql.ErrNoRows {
			return false, nil
		}
		if err != nil {
			return false, fmt.Errorf("walk dashboard folders: %w", err)
		}
		if !parent.Valid {
			return false, nil
		}
		if parent.String == ancestor {
			return true, nil
		}
		current = parent.String
	}
	return false, nil
}

// DeleteDashboardFolder removes a folder. Its dashboards and subfolders are
// re-parented to the folder's parent, so nothing is lost.
func (db *DB) DeleteDashboardFolder(id string) error {
	existing, err := db.GetDashboardFolder(id)
	if err != nil {
		return err
	}
	if existing == nil {
		return sql.ErrNoRows
	}
	var parent interface{}
	if existing.ParentID != nil {
		parent = *existing.ParentID
	}
	tx, err := db.conn.Begin()
	if err != nil {
		return fmt.Errorf("begin delete folder: %w", err)
	}
	defer tx.Rollback()
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := tx.Exec("UPDATE dashboards SET folder_id = ?, updated_at = ? WHERE folder_id = ?", parent, now, id); err != nil {
		return fmt.Errorf("re-parent dashboards: %w", err)
	}
	if _, err := tx.Exec("UPDATE dashboard_folders SET parent_id = ?, updated_at = ? WHERE parent_id = ?", parent, now, id); err != nil {
		return fmt.Errorf("re-parent subfolders: %w", err)
	}
	if _, err := tx.Exec("DELETE FROM dashboard_folders WHERE id = ?", id); err != nil {
		return fmt.Errorf("delete dashboard folder: %w", err)
	}
	return tx.Commit()
}
