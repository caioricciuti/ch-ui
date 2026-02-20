package database

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
)

const (
	systemDashboardName        = "System Overview"
	systemDashboardDescription = "Built-in operational dashboard for ClickHouse health and query performance."
	systemDashboardCreatedBy   = "system"
)

// Dashboard represents a dashboard record.
type Dashboard struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Description *string `json:"description"`
	CreatedBy   *string `json:"created_by"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
}

// Panel represents a dashboard panel.
type Panel struct {
	ID           string  `json:"id"`
	DashboardID  string  `json:"dashboard_id"`
	Name         string  `json:"name"`
	PanelType    string  `json:"panel_type"`
	Query        string  `json:"query"`
	ConnectionID *string `json:"connection_id"`
	Config       string  `json:"config"`
	LayoutX      int     `json:"layout_x"`
	LayoutY      int     `json:"layout_y"`
	LayoutW      int     `json:"layout_w"`
	LayoutH      int     `json:"layout_h"`
	CreatedAt    string  `json:"created_at"`
	UpdatedAt    string  `json:"updated_at"`
}

// GetDashboards retrieves all dashboards.
func (db *DB) GetDashboards() ([]Dashboard, error) {
	rows, err := db.conn.Query(
		`SELECT id, name, description, created_by, created_at, updated_at
		 FROM dashboards ORDER BY updated_at DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("get dashboards: %w", err)
	}
	defer rows.Close()

	var dashboards []Dashboard
	for rows.Next() {
		var d Dashboard
		var desc, createdBy sql.NullString
		if err := rows.Scan(&d.ID, &d.Name, &desc, &createdBy, &d.CreatedAt, &d.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan dashboard: %w", err)
		}
		d.Description = nullStringToPtr(desc)
		d.CreatedBy = nullStringToPtr(createdBy)
		dashboards = append(dashboards, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate dashboard rows: %w", err)
	}
	return dashboards, nil
}

// GetDashboardByID retrieves a dashboard by ID.
func (db *DB) GetDashboardByID(id string) (*Dashboard, error) {
	row := db.conn.QueryRow(
		`SELECT id, name, description, created_by, created_at, updated_at
		 FROM dashboards WHERE id = ?`, id,
	)

	var d Dashboard
	var desc, createdBy sql.NullString
	err := row.Scan(&d.ID, &d.Name, &desc, &createdBy, &d.CreatedAt, &d.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get dashboard by id: %w", err)
	}
	d.Description = nullStringToPtr(desc)
	d.CreatedBy = nullStringToPtr(createdBy)
	return &d, nil
}

// CreateDashboard creates a new dashboard and returns its ID.
func (db *DB) CreateDashboard(name, description, createdBy string) (string, error) {
	id := uuid.NewString()
	now := time.Now().UTC().Format(time.RFC3339)

	var desc, creator interface{}
	if description != "" {
		desc = description
	}
	if createdBy != "" {
		creator = createdBy
	}

	_, err := db.conn.Exec(
		`INSERT INTO dashboards (id, name, description, created_by, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		id, name, desc, creator, now, now,
	)
	if err != nil {
		return "", fmt.Errorf("create dashboard: %w", err)
	}
	return id, nil
}

// UpdateDashboard updates a dashboard's name and description.
func (db *DB) UpdateDashboard(id, name, description string) error {
	now := time.Now().UTC().Format(time.RFC3339)

	var desc interface{}
	if description != "" {
		desc = description
	}

	_, err := db.conn.Exec(
		"UPDATE dashboards SET name = ?, description = ?, updated_at = ? WHERE id = ?",
		name, desc, now, id,
	)
	if err != nil {
		return fmt.Errorf("update dashboard: %w", err)
	}
	return nil
}

// DeleteDashboard deletes a dashboard and all its panels (cascade).
func (db *DB) DeleteDashboard(id string) error {
	_, err := db.conn.Exec("DELETE FROM dashboards WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete dashboard: %w", err)
	}
	return nil
}

// GetPanelsByDashboard retrieves all panels for a dashboard.
func (db *DB) GetPanelsByDashboard(dashboardID string) ([]Panel, error) {
	rows, err := db.conn.Query(
		`SELECT id, dashboard_id, name, panel_type, query, connection_id, config, layout_x, layout_y, layout_w, layout_h, created_at, updated_at
		 FROM panels WHERE dashboard_id = ? ORDER BY layout_y ASC, layout_x ASC`,
		dashboardID,
	)
	if err != nil {
		return nil, fmt.Errorf("get panels by dashboard: %w", err)
	}
	defer rows.Close()

	var panels []Panel
	for rows.Next() {
		var p Panel
		var connID sql.NullString
		if err := rows.Scan(&p.ID, &p.DashboardID, &p.Name, &p.PanelType, &p.Query, &connID, &p.Config, &p.LayoutX, &p.LayoutY, &p.LayoutW, &p.LayoutH, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan panel: %w", err)
		}
		p.ConnectionID = nullStringToPtr(connID)
		panels = append(panels, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate panel rows: %w", err)
	}
	return panels, nil
}

// GetPanelByID retrieves a panel by ID.
func (db *DB) GetPanelByID(id string) (*Panel, error) {
	row := db.conn.QueryRow(
		`SELECT id, dashboard_id, name, panel_type, query, connection_id, config, layout_x, layout_y, layout_w, layout_h, created_at, updated_at
		 FROM panels WHERE id = ?`, id,
	)

	var p Panel
	var connID sql.NullString
	err := row.Scan(&p.ID, &p.DashboardID, &p.Name, &p.PanelType, &p.Query, &connID, &p.Config, &p.LayoutX, &p.LayoutY, &p.LayoutW, &p.LayoutH, &p.CreatedAt, &p.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get panel by id: %w", err)
	}
	p.ConnectionID = nullStringToPtr(connID)
	return &p, nil
}

// CreatePanel creates a new panel and returns its ID.
func (db *DB) CreatePanel(dashboardID, name, panelType, query, connectionID, config string, x, y, w, h int) (string, error) {
	id := uuid.NewString()
	now := time.Now().UTC().Format(time.RFC3339)

	var connID interface{}
	if connectionID != "" {
		connID = connectionID
	}
	if config == "" {
		config = "{}"
	}

	_, err := db.conn.Exec(
		`INSERT INTO panels (id, dashboard_id, name, panel_type, query, connection_id, config, layout_x, layout_y, layout_w, layout_h, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, dashboardID, name, panelType, query, connID, config, x, y, w, h, now, now,
	)
	if err != nil {
		return "", fmt.Errorf("create panel: %w", err)
	}
	return id, nil
}

// UpdatePanel updates a panel.
func (db *DB) UpdatePanel(id, name, panelType, query, connectionID, config string, x, y, w, h int) error {
	now := time.Now().UTC().Format(time.RFC3339)

	var connID interface{}
	if connectionID != "" {
		connID = connectionID
	}

	_, err := db.conn.Exec(
		`UPDATE panels SET name = ?, panel_type = ?, query = ?, connection_id = ?, config = ?, layout_x = ?, layout_y = ?, layout_w = ?, layout_h = ?, updated_at = ? WHERE id = ?`,
		name, panelType, query, connID, config, x, y, w, h, now, id,
	)
	if err != nil {
		return fmt.Errorf("update panel: %w", err)
	}
	return nil
}

// DeletePanel deletes a panel by ID.
func (db *DB) DeletePanel(id string) error {
	_, err := db.conn.Exec("DELETE FROM panels WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete panel: %w", err)
	}
	return nil
}

type seededPanel struct {
	Name      string
	PanelType string
	Query     string
	Config    string
	X         int
	Y         int
	W         int
	H         int
}

// EnsureSystemOverviewDashboard creates or updates a built-in default dashboard
// with operational ClickHouse metrics.
func (db *DB) EnsureSystemOverviewDashboard() error {
	tx, err := db.conn.Begin()
	if err != nil {
		return fmt.Errorf("begin system dashboard transaction: %w", err)
	}
	defer tx.Rollback()

	now := time.Now().UTC().Format(time.RFC3339)
	dashboardID := ""

	if err := tx.QueryRow(
		`SELECT id FROM dashboards WHERE created_by = ? LIMIT 1`,
		systemDashboardCreatedBy,
	).Scan(&dashboardID); err != nil {
		if err == sql.ErrNoRows {
			dashboardID = uuid.NewString()
			if _, err := tx.Exec(
				`INSERT INTO dashboards (id, name, description, created_by, created_at, updated_at)
				 VALUES (?, ?, ?, ?, ?, ?)`,
				dashboardID,
				systemDashboardName,
				systemDashboardDescription,
				systemDashboardCreatedBy,
				now,
				now,
			); err != nil {
				return fmt.Errorf("insert system dashboard: %w", err)
			}
		} else {
			return fmt.Errorf("get system dashboard: %w", err)
		}
	} else {
		if _, err := tx.Exec(
			`UPDATE dashboards
			 SET name = ?, description = ?, updated_at = ?
			 WHERE id = ?`,
			systemDashboardName,
			systemDashboardDescription,
			now,
			dashboardID,
		); err != nil {
			return fmt.Errorf("update system dashboard metadata: %w", err)
		}
	}

	panels := []seededPanel{
		{
			Name:      "ClickHouse Version",
			PanelType: "stat",
			Query:     `SELECT version() AS version`,
			Config:    `{"chartType":"stat"}`,
			X:         0, Y: 0, W: 2, H: 3,
		},
		{
			Name:      "Uptime (seconds)",
			PanelType: "stat",
			Query: `SELECT toUInt64(anyIf(value, metric = 'Uptime')) AS uptime_sec
FROM system.asynchronous_metrics`,
			Config: `{"chartType":"stat"}`,
			X:      2, Y: 0, W: 2, H: 3,
		},
		{
			Name:      "Active Queries",
			PanelType: "stat",
			Query:     `SELECT count() AS active_queries FROM system.processes`,
			Config:    `{"chartType":"stat"}`,
			X:         4, Y: 0, W: 2, H: 3,
		},
		{
			Name:      "Connected Users",
			PanelType: "stat",
			Query:     `SELECT uniq(user) AS connected_users FROM system.processes`,
			Config:    `{"chartType":"stat"}`,
			X:         6, Y: 0, W: 2, H: 3,
		},
		{
			Name:      "Databases",
			PanelType: "stat",
			Query:     `SELECT count() AS databases FROM system.databases`,
			Config:    `{"chartType":"stat"}`,
			X:         8, Y: 0, W: 2, H: 3,
		},
		{
			Name:      "Tables",
			PanelType: "stat",
			Query:     `SELECT count() AS tables FROM system.tables WHERE database NOT IN ('system', 'INFORMATION_SCHEMA', 'information_schema')`,
			Config:    `{"chartType":"stat"}`,
			X:         10, Y: 0, W: 2, H: 3,
		},
		{
			Name:      "Queries / interval",
			PanelType: "timeseries",
			Query: `SELECT
  toStartOfInterval(event_time, INTERVAL $__interval second) AS ts,
  count() AS queries
FROM system.query_log
WHERE type = 'QueryFinish'
  AND $__timestamp(event_time)
GROUP BY ts
ORDER BY ts`,
			Config: `{"chartType":"timeseries","xColumn":"ts","yColumns":["queries"],"colors":["#F97316"],"legendPosition":"bottom"}`,
			X:      0, Y: 3, W: 4, H: 5,
		},
		{
			Name:      "P95 Query Latency (ms) / interval",
			PanelType: "timeseries",
			Query: `SELECT
  toStartOfInterval(event_time, INTERVAL $__interval second) AS ts,
  round(quantile(0.95)(query_duration_ms), 2) AS p95_ms
FROM system.query_log
WHERE type = 'QueryFinish'
  AND $__timestamp(event_time)
GROUP BY ts
ORDER BY ts`,
			Config: `{"chartType":"timeseries","xColumn":"ts","yColumns":["p95_ms"],"colors":["#EF4444"],"legendPosition":"bottom"}`,
			X:      4, Y: 3, W: 4, H: 5,
		},
		{
			Name:      "Read MB / interval",
			PanelType: "timeseries",
			Query: `SELECT
  toStartOfInterval(event_time, INTERVAL $__interval second) AS ts,
  round(sum(read_bytes) / 1048576, 2) AS read_mb
FROM system.query_log
WHERE type = 'QueryFinish'
  AND $__timestamp(event_time)
GROUP BY ts
ORDER BY ts`,
			Config: `{"chartType":"timeseries","xColumn":"ts","yColumns":["read_mb"],"colors":["#10B981"],"legendPosition":"bottom"}`,
			X:      8, Y: 3, W: 4, H: 5,
		},
		{
			Name:      "Top Slow Queries",
			PanelType: "table",
			Query: `SELECT
  query_id,
  user,
  query_duration_ms,
  read_rows,
  formatReadableSize(read_bytes) AS read_size,
  left(query, 120) AS sample_query
FROM system.query_log
WHERE type = 'QueryFinish'
  AND $__timestamp(event_time)
ORDER BY query_duration_ms DESC
LIMIT 50`,
			Config: `{"chartType":"table"}`,
			X:      0, Y: 8, W: 7, H: 6,
		},
		{
			Name:      "Top Tables by Size",
			PanelType: "table",
			Query: `SELECT
  concat(database, '.', table) AS table_name,
  round(sum(bytes_on_disk) / 1048576, 2) AS size_mb,
  sum(rows) AS rows
FROM system.parts
WHERE active
GROUP BY table_name
ORDER BY size_mb DESC
LIMIT 50`,
			Config: `{"chartType":"table"}`,
			X:      7, Y: 8, W: 5, H: 6,
		},
		{
			Name:      "Disk Space",
			PanelType: "table",
			Query: `SELECT
  name,
  path,
  round(total_space / 1073741824, 2) AS total_gb,
  round(free_space / 1073741824, 2) AS free_gb,
  round((total_space - free_space) / 1073741824, 2) AS used_gb
FROM system.disks
ORDER BY used_gb DESC`,
			Config: `{"chartType":"table"}`,
			X:      0, Y: 14, W: 6, H: 5,
		},
		{
			Name:      "Background Operations",
			PanelType: "table",
			Query: `SELECT 'merges_running' AS metric, count() AS value FROM system.merges
UNION ALL
SELECT 'mutations_pending' AS metric, countIf(is_done = 0) AS value FROM system.mutations
UNION ALL
SELECT 'replication_queue' AS metric, sum(queue_size) AS value FROM system.replicas`,
			Config: `{"chartType":"table"}`,
			X:      6, Y: 14, W: 6, H: 5,
		},
	}

	existing := map[string]string{}
	rows, err := tx.Query(`SELECT id, name FROM panels WHERE dashboard_id = ?`, dashboardID)
	if err != nil {
		return fmt.Errorf("list existing system panels: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var panelID, name string
		if err := rows.Scan(&panelID, &name); err != nil {
			return fmt.Errorf("scan existing system panel: %w", err)
		}
		existing[name] = panelID
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate existing system panels: %w", err)
	}

	for _, p := range panels {
		if panelID, ok := existing[p.Name]; ok {
			if _, err := tx.Exec(
				`UPDATE panels
				 SET panel_type = ?, query = ?, connection_id = NULL, config = ?,
				     layout_x = ?, layout_y = ?, layout_w = ?, layout_h = ?, updated_at = ?
				 WHERE id = ?`,
				p.PanelType,
				p.Query,
				p.Config,
				p.X,
				p.Y,
				p.W,
				p.H,
				now,
				panelID,
			); err != nil {
				return fmt.Errorf("update system panel %q: %w", p.Name, err)
			}
		} else {
			if _, err := tx.Exec(
				`INSERT INTO panels (
					id, dashboard_id, name, panel_type, query, connection_id, config,
					layout_x, layout_y, layout_w, layout_h, created_at, updated_at
				)
				VALUES (?, ?, ?, ?, ?, NULL, ?, ?, ?, ?, ?, ?, ?)`,
				uuid.NewString(),
				dashboardID,
				p.Name,
				p.PanelType,
				p.Query,
				p.Config,
				p.X,
				p.Y,
				p.W,
				p.H,
				now,
				now,
			); err != nil {
				return fmt.Errorf("insert system panel %q: %w", p.Name, err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit system dashboard seed: %w", err)
	}

	return nil
}
