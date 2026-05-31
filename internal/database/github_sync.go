package database

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// GitHubSyncLog records the result of a GitHub → CH-UI model sync.
type GitHubSyncLog struct {
	ID              string  `json:"id"`
	ConnectionID    string  `json:"connection_id"`
	Status          string  `json:"status"`
	ModelsCreated   int     `json:"models_created"`
	ModelsUpdated   int     `json:"models_updated"`
	ModelsDeleted   int     `json:"models_deleted"`
	ModelsUnchanged int     `json:"models_unchanged"`
	Error           *string `json:"error"`
	TriggeredBy     *string `json:"triggered_by"`
	StartedAt       string  `json:"started_at"`
	FinishedAt      *string `json:"finished_at"`
	CommitSHA       *string `json:"commit_sha"`
	CreatedAt       string  `json:"created_at"`
}

// GitHubIntegration is the read-safe representation of a connection's GitHub config.
type GitHubIntegration struct {
	Enabled          bool   `json:"enabled"`
	Repo             string `json:"repo"`
	Branch           string `json:"branch"`
	Path             string `json:"path"`
	HasPAT           bool   `json:"has_pat"`
	HasWebhookSecret bool   `json:"has_webhook_secret"`
	LastSyncSHA      string `json:"last_sync_sha"`
}

func ghKey(connectionID, key string) string {
	return fmt.Sprintf("github.%s.%s", connectionID, key)
}

// GetGitHubIntegration reads all github.* settings for a connection.
func (db *DB) GetGitHubIntegration(connectionID string) (*GitHubIntegration, error) {
	repo, _ := db.GetSetting(ghKey(connectionID, "repo"))
	if repo == "" {
		return nil, nil
	}
	enabled, _ := db.GetSetting(ghKey(connectionID, "enabled"))
	branch, _ := db.GetSetting(ghKey(connectionID, "branch"))
	path, _ := db.GetSetting(ghKey(connectionID, "path"))
	pat, _ := db.GetSetting(ghKey(connectionID, "encrypted_pat"))
	secret, _ := db.GetSetting(ghKey(connectionID, "webhook_secret"))
	sha, _ := db.GetSetting(ghKey(connectionID, "last_sync_sha"))

	if branch == "" {
		branch = "main"
	}
	if path == "" {
		path = "models/"
	}

	return &GitHubIntegration{
		Enabled:          enabled == "true",
		Repo:             repo,
		Branch:           branch,
		Path:             path,
		HasPAT:           pat != "",
		HasWebhookSecret: secret != "",
		LastSyncSHA:      sha,
	}, nil
}

// GetGitHubRawSettings returns the raw encrypted values for internal use.
func (db *DB) GetGitHubRawSettings(connectionID string) (repo, branch, path, encryptedPAT, encryptedSecret, lastSHA string) {
	repo, _ = db.GetSetting(ghKey(connectionID, "repo"))
	branch, _ = db.GetSetting(ghKey(connectionID, "branch"))
	path, _ = db.GetSetting(ghKey(connectionID, "path"))
	encryptedPAT, _ = db.GetSetting(ghKey(connectionID, "encrypted_pat"))
	encryptedSecret, _ = db.GetSetting(ghKey(connectionID, "webhook_secret"))
	lastSHA, _ = db.GetSetting(ghKey(connectionID, "last_sync_sha"))
	if branch == "" {
		branch = "main"
	}
	if path == "" {
		path = "models/"
	}
	return
}

// SaveGitHubIntegration persists the GitHub integration settings for a connection.
func (db *DB) SaveGitHubIntegration(connectionID string, fields map[string]string) error {
	for k, v := range fields {
		if err := db.SetSetting(ghKey(connectionID, k), v); err != nil {
			return fmt.Errorf("save github setting %s: %w", k, err)
		}
	}
	return nil
}

// DeleteGitHubIntegration removes all github.* settings for a connection.
func (db *DB) DeleteGitHubIntegration(connectionID string) error {
	keys := []string{"enabled", "repo", "branch", "path", "encrypted_pat", "webhook_secret", "last_sync_sha"}
	for _, k := range keys {
		if err := db.DeleteSetting(ghKey(connectionID, k)); err != nil && !strings.Contains(err.Error(), "no rows") {
			return fmt.Errorf("delete github setting %s: %w", k, err)
		}
	}
	return nil
}

// SetGitHubLastSyncSHA updates the last synced commit SHA.
func (db *DB) SetGitHubLastSyncSHA(connectionID, sha string) error {
	return db.SetSetting(ghKey(connectionID, "last_sync_sha"), sha)
}

// CreateGitHubSyncLog starts a new sync log entry.
func (db *DB) CreateGitHubSyncLog(connectionID, triggeredBy string) (string, error) {
	id := uuid.NewString()
	now := time.Now().UTC().Format(time.RFC3339)
	var trig interface{}
	if triggeredBy != "" {
		trig = triggeredBy
	}
	_, err := db.conn.Exec(
		`INSERT INTO github_sync_logs (id, connection_id, status, triggered_by, started_at, created_at)
		 VALUES (?, ?, 'running', ?, ?, ?)`,
		id, connectionID, trig, now, now,
	)
	if err != nil {
		return "", fmt.Errorf("create github sync log: %w", err)
	}
	return id, nil
}

// FinalizeGitHubSyncLog marks a sync log as complete.
func (db *DB) FinalizeGitHubSyncLog(id, status string, created, updated, deleted, unchanged int, commitSHA, errMsg string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	var sha, errStr interface{}
	if commitSHA != "" {
		sha = commitSHA
	}
	if errMsg != "" {
		errStr = errMsg
	}
	_, err := db.conn.Exec(
		`UPDATE github_sync_logs SET status = ?, models_created = ?, models_updated = ?,
		        models_deleted = ?, models_unchanged = ?, commit_sha = ?, error = ?, finished_at = ?
		 WHERE id = ?`,
		status, created, updated, deleted, unchanged, sha, errStr, now, id,
	)
	if err != nil {
		return fmt.Errorf("finalize github sync log: %w", err)
	}
	return nil
}

// GetGitHubSyncLogs returns recent sync logs for a connection.
func (db *DB) GetGitHubSyncLogs(connectionID string, limit int) ([]GitHubSyncLog, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := db.conn.Query(
		`SELECT id, connection_id, status, models_created, models_updated, models_deleted,
		        models_unchanged, error, triggered_by, started_at, finished_at, commit_sha, created_at
		 FROM github_sync_logs WHERE connection_id = ? ORDER BY started_at DESC LIMIT ?`,
		connectionID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("get github sync logs: %w", err)
	}
	defer rows.Close()

	var logs []GitHubSyncLog
	for rows.Next() {
		var l GitHubSyncLog
		var errMsg, triggeredBy, finishedAt, commitSHA sql.NullString
		if err := rows.Scan(&l.ID, &l.ConnectionID, &l.Status,
			&l.ModelsCreated, &l.ModelsUpdated, &l.ModelsDeleted, &l.ModelsUnchanged,
			&errMsg, &triggeredBy, &l.StartedAt, &finishedAt, &commitSHA, &l.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan github sync log: %w", err)
		}
		l.Error = nullStringToPtr(errMsg)
		l.TriggeredBy = nullStringToPtr(triggeredBy)
		l.FinishedAt = nullStringToPtr(finishedAt)
		l.CommitSHA = nullStringToPtr(commitSHA)
		logs = append(logs, l)
	}
	return logs, rows.Err()
}
