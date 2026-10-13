package database

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// ---------- Variables ----------

// GetDashboardVariables returns the stored JSON array of variable
// definitions ("[]" when there are none).
func (db *DB) GetDashboardVariables(id string) (string, error) {
	var raw sql.NullString
	err := db.conn.QueryRow("SELECT variables FROM dashboards WHERE id = ?", id).Scan(&raw)
	if err == sql.ErrNoRows {
		return "[]", nil
	}
	if err != nil {
		return "", fmt.Errorf("get dashboard variables: %w", err)
	}
	if !raw.Valid || raw.String == "" {
		return "[]", nil
	}
	return raw.String, nil
}

// SetDashboardVariables replaces the variable definitions (a JSON array).
func (db *DB) SetDashboardVariables(id, variablesJSON string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.conn.Exec("UPDATE dashboards SET variables = ?, updated_at = ? WHERE id = ?", variablesJSON, now, id); err != nil {
		return fmt.Errorf("set dashboard variables: %w", err)
	}
	return nil
}

// ---------- Whole-dashboard writes ----------

// PanelInput is a validated panel to write.
type PanelInput struct {
	Name, Description, PanelType, Query, ConnectionID, Config string
	X, Y, W, H                                                int
}

// DashboardContent is everything an import, restore, template or duplicate
// writes. Variables nil keeps the current variables (replace only).
type DashboardContent struct {
	Name        string
	Description *string
	Variables   *string
	Panels      []PanelInput
}

func insertPanels(tx *sql.Tx, dashboardID, now string, panels []PanelInput) error {
	for _, p := range panels {
		var connID interface{}
		if p.ConnectionID != "" {
			connID = p.ConnectionID
		}
		cfg := p.Config
		if cfg == "" {
			cfg = "{}"
		}
		if _, err := tx.Exec(
			`INSERT INTO panels (id, dashboard_id, name, description, panel_type, query, connection_id, config, layout_x, layout_y, layout_w, layout_h, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			uuid.NewString(), dashboardID, p.Name, p.Description, p.PanelType, p.Query, connID, cfg, p.X, p.Y, p.W, p.H, now, now,
		); err != nil {
			return fmt.Errorf("insert panel %q: %w", p.Name, err)
		}
	}
	return nil
}

func nullable(s *string) interface{} {
	if s == nil || *s == "" {
		return nil
	}
	return *s
}

// ReplaceDashboardContent updates the dashboard row and swaps every panel
// (and the variables, when given) in one transaction.
func (db *DB) ReplaceDashboardContent(id string, c DashboardContent) error {
	tx, err := db.conn.Begin()
	if err != nil {
		return fmt.Errorf("begin replace: %w", err)
	}
	defer tx.Rollback()

	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := tx.Exec("UPDATE dashboards SET name = ?, description = ?, updated_at = ? WHERE id = ?", c.Name, nullable(c.Description), now, id); err != nil {
		return fmt.Errorf("update dashboard: %w", err)
	}
	if c.Variables != nil {
		if _, err := tx.Exec("UPDATE dashboards SET variables = ? WHERE id = ?", *c.Variables, id); err != nil {
			return fmt.Errorf("update variables: %w", err)
		}
	}
	if _, err := tx.Exec("DELETE FROM panels WHERE dashboard_id = ?", id); err != nil {
		return fmt.Errorf("delete panels: %w", err)
	}
	if err := insertPanels(tx, id, now, c.Panels); err != nil {
		return err
	}
	return tx.Commit()
}

// CreateDashboardWithContent creates a dashboard with its panels and
// variables in one transaction and returns its ID.
func (db *DB) CreateDashboardWithContent(c DashboardContent, createdBy, folderID string, tags []string) (string, error) {
	tx, err := db.conn.Begin()
	if err != nil {
		return "", fmt.Errorf("begin create: %w", err)
	}
	defer tx.Rollback()

	id := uuid.NewString()
	now := time.Now().UTC().Format(time.RFC3339)
	vars := "[]"
	if c.Variables != nil {
		vars = *c.Variables
	}
	var creator, folder interface{}
	if createdBy != "" {
		creator = createdBy
	}
	if folderID != "" {
		folder = folderID
	}
	if _, err := tx.Exec(
		`INSERT INTO dashboards (id, name, description, folder_id, tags, variables, created_by, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, c.Name, nullable(c.Description), folder, encodeTags(tags), vars, creator, now, now,
	); err != nil {
		return "", fmt.Errorf("insert dashboard: %w", err)
	}
	if err := insertPanels(tx, id, now, c.Panels); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("commit create: %w", err)
	}
	return id, nil
}

// ---------- Snapshots ----------

// SnapshotPanel is one panel in the export format.
type SnapshotPanel struct {
	Name         string          `json:"name"`
	Description  string          `json:"description"`
	PanelType    string          `json:"panel_type"`
	Query        string          `json:"query"`
	ConnectionID string          `json:"connection_id,omitempty"`
	Config       json.RawMessage `json:"config"`
	LayoutX      int             `json:"layout_x"`
	LayoutY      int             `json:"layout_y"`
	LayoutW      int             `json:"layout_w"`
	LayoutH      int             `json:"layout_h"`
}

// Snapshot is a whole dashboard in the export format (what the UI downloads
// and what import accepts).
type Snapshot struct {
	Version   int `json:"version"`
	Dashboard struct {
		Name        string  `json:"name"`
		Description *string `json:"description"`
	} `json:"dashboard"`
	Variables json.RawMessage `json:"variables"`
	Panels    []SnapshotPanel `json:"panels"`
}

// DashboardSnapshot builds the export JSON of a dashboard. It returns nil
// when the dashboard does not exist.
func (db *DB) DashboardSnapshot(id string) (*Snapshot, error) {
	d, err := db.GetDashboardByID(id)
	if err != nil || d == nil {
		return nil, err
	}
	vars, err := db.GetDashboardVariables(id)
	if err != nil {
		return nil, err
	}
	panels, err := db.GetPanelsByDashboard(id)
	if err != nil {
		return nil, err
	}
	s := &Snapshot{Version: 1, Variables: json.RawMessage(vars), Panels: make([]SnapshotPanel, 0, len(panels))}
	if !json.Valid(s.Variables) {
		s.Variables = json.RawMessage("[]")
	}
	s.Dashboard.Name = d.Name
	s.Dashboard.Description = d.Description
	for _, p := range panels {
		cfg := json.RawMessage(p.Config)
		if !json.Valid(cfg) {
			cfg = json.RawMessage("{}")
		}
		sp := SnapshotPanel{
			Name: p.Name, Description: p.Description, PanelType: p.PanelType, Query: p.Query, Config: cfg,
			LayoutX: p.LayoutX, LayoutY: p.LayoutY, LayoutW: p.LayoutW, LayoutH: p.LayoutH,
		}
		if p.ConnectionID != nil {
			sp.ConnectionID = *p.ConnectionID
		}
		s.Panels = append(s.Panels, sp)
	}
	return s, nil
}

// ---------- Versions ----------

const (
	// versionMergeWindow: a layout-only change this soon after the latest
	// version updates that version instead of adding one.
	versionMergeWindow = 30 * time.Second
	// versionsKept is how many versions each dashboard keeps.
	versionsKept = 50
)

// DashboardVersion is one saved state of a dashboard.
type DashboardVersion struct {
	ID          string          `json:"id"`
	DashboardID string          `json:"dashboard_id"`
	Version     int             `json:"version"`
	Snapshot    json.RawMessage `json:"snapshot,omitempty"`
	CreatedBy   *string         `json:"created_by"`
	CreatedAt   string          `json:"created_at"`
	Note        string          `json:"note"`
	PanelCount  int             `json:"panel_count"`
}

// withoutLayout returns the snapshot with every panel position zeroed, so two
// states that differ only in layout compare equal.
func withoutLayout(s Snapshot) string {
	panels := make([]SnapshotPanel, len(s.Panels))
	copy(panels, s.Panels)
	for i := range panels {
		panels[i].LayoutX, panels[i].LayoutY, panels[i].LayoutW, panels[i].LayoutH = 0, 0, 0, 0
	}
	s.Panels = panels
	b, _ := json.Marshal(s)
	return string(b)
}

// RecordDashboardVersion saves the current state of a dashboard as a new
// version. Nothing is written when the state equals the latest version. A
// change to the layout only, within 30 s of the latest version, updates that
// version in place, so dragging panels around does not flood the history.
// Only the latest 50 versions are kept. It returns the version number
// written or updated (0 when nothing changed).
func (db *DB) RecordDashboardVersion(dashboardID, createdBy, note string) (int, error) {
	snap, err := db.DashboardSnapshot(dashboardID)
	if err != nil || snap == nil {
		return 0, err
	}
	raw, err := json.Marshal(snap)
	if err != nil {
		return 0, fmt.Errorf("encode snapshot: %w", err)
	}

	var latestID, latestSnap, latestAt string
	var latestVersion int
	err = db.conn.QueryRow(
		`SELECT id, version, snapshot, created_at FROM dashboard_versions WHERE dashboard_id = ? ORDER BY version DESC LIMIT 1`,
		dashboardID,
	).Scan(&latestID, &latestVersion, &latestSnap, &latestAt)
	if err != nil && err != sql.ErrNoRows {
		return 0, fmt.Errorf("get latest version: %w", err)
	}
	if err == nil {
		if latestSnap == string(raw) {
			return 0, nil
		}
		var prev Snapshot
		at, perr := time.Parse(time.RFC3339, latestAt)
		if note == "" && perr == nil && time.Since(at) < versionMergeWindow &&
			json.Unmarshal([]byte(latestSnap), &prev) == nil && withoutLayout(prev) == withoutLayout(*snap) {
			if _, err := db.conn.Exec("UPDATE dashboard_versions SET snapshot = ? WHERE id = ?", string(raw), latestID); err != nil {
				return 0, fmt.Errorf("update version: %w", err)
			}
			return latestVersion, nil
		}
	}

	var creator interface{}
	if createdBy != "" {
		creator = createdBy
	}
	now := time.Now().UTC().Format(time.RFC3339)
	// One statement computes the next number, so concurrent saves cannot
	// pick the same one.
	if _, err := db.conn.Exec(
		`INSERT INTO dashboard_versions (id, dashboard_id, version, snapshot, created_by, created_at, note)
		 SELECT ?, ?, COALESCE(MAX(version), 0) + 1, ?, ?, ?, ? FROM dashboard_versions WHERE dashboard_id = ?`,
		uuid.NewString(), dashboardID, string(raw), creator, now, note, dashboardID,
	); err != nil {
		return 0, fmt.Errorf("insert version: %w", err)
	}
	var version int
	if err := db.conn.QueryRow("SELECT MAX(version) FROM dashboard_versions WHERE dashboard_id = ?", dashboardID).Scan(&version); err != nil {
		return 0, fmt.Errorf("read version: %w", err)
	}
	if _, err := db.conn.Exec("DELETE FROM dashboard_versions WHERE dashboard_id = ? AND version <= ?", dashboardID, version-versionsKept); err != nil {
		return version, fmt.Errorf("prune versions: %w", err)
	}
	return version, nil
}

// HasDashboardVersions reports whether any version exists for a dashboard.
func (db *DB) HasDashboardVersions(dashboardID string) (bool, error) {
	var n int
	if err := db.conn.QueryRow("SELECT COUNT(*) FROM dashboard_versions WHERE dashboard_id = ?", dashboardID).Scan(&n); err != nil {
		return false, fmt.Errorf("count versions: %w", err)
	}
	return n > 0, nil
}

// ListDashboardVersions returns the versions of a dashboard, newest first,
// without their snapshots.
func (db *DB) ListDashboardVersions(dashboardID string) ([]DashboardVersion, error) {
	rows, err := db.conn.Query(
		`SELECT id, dashboard_id, version, created_by, created_at, note, COALESCE(json_array_length(snapshot, '$.panels'), 0)
		 FROM dashboard_versions WHERE dashboard_id = ? ORDER BY version DESC`, dashboardID,
	)
	if err != nil {
		return nil, fmt.Errorf("list versions: %w", err)
	}
	defer rows.Close()
	out := []DashboardVersion{}
	for rows.Next() {
		var v DashboardVersion
		var createdBy sql.NullString
		if err := rows.Scan(&v.ID, &v.DashboardID, &v.Version, &createdBy, &v.CreatedAt, &v.Note, &v.PanelCount); err != nil {
			return nil, fmt.Errorf("scan version: %w", err)
		}
		v.CreatedBy = nullStringToPtr(createdBy)
		out = append(out, v)
	}
	return out, rows.Err()
}

// GetDashboardVersion returns one version with its snapshot, or nil.
func (db *DB) GetDashboardVersion(dashboardID string, version int) (*DashboardVersion, error) {
	var v DashboardVersion
	var createdBy sql.NullString
	var snap string
	err := db.conn.QueryRow(
		`SELECT id, dashboard_id, version, snapshot, created_by, created_at, note, COALESCE(json_array_length(snapshot, '$.panels'), 0)
		 FROM dashboard_versions WHERE dashboard_id = ? AND version = ?`, dashboardID, version,
	).Scan(&v.ID, &v.DashboardID, &v.Version, &snap, &createdBy, &v.CreatedAt, &v.Note, &v.PanelCount)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get version: %w", err)
	}
	v.Snapshot = json.RawMessage(snap)
	v.CreatedBy = nullStringToPtr(createdBy)
	return &v, nil
}
