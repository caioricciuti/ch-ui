package database

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// BrainProvider stores global AI provider configuration.
type BrainProvider struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	Kind      string  `json:"kind"`
	BaseURL   *string `json:"base_url"`
	HasAPIKey bool    `json:"has_api_key"`
	IsActive  bool    `json:"is_active"`
	IsDefault bool    `json:"is_default"`
	CreatedBy *string `json:"created_by"`
	CreatedAt string  `json:"created_at"`
	UpdatedAt string  `json:"updated_at"`
}

// BrainProviderSecret returns provider config with secret for runtime use.
type BrainProviderSecret struct {
	BrainProvider
	EncryptedAPIKey *string `json:"-"`
}

// BrainModel stores available models per provider.
type BrainModel struct {
	ID          string  `json:"id"`
	ProviderID  string  `json:"provider_id"`
	Name        string  `json:"name"`
	DisplayName *string `json:"display_name"`
	IsActive    bool    `json:"is_active"`
	IsDefault   bool    `json:"is_default"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
}

// BrainModelRuntime is used for chat execution.
type BrainModelRuntime struct {
	ModelID              string
	ModelName            string
	ProviderID           string
	ProviderName         string
	ProviderKind         string
	ProviderBaseURL      *string
	ProviderEncryptedKey *string
	ModelActive          bool
	ProviderActive       bool
}

// BrainChat stores a user chat thread.
type BrainChat struct {
	ID              string  `json:"id"`
	ConnectionID    string  `json:"connection_id"`
	Username        string  `json:"username"`
	Title           string  `json:"title"`
	ProviderID      *string `json:"provider_id"`
	ModelID         *string `json:"model_id"`
	Archived        bool    `json:"archived"`
	LastMessageAt   *string `json:"last_message_at"`
	ContextDatabase *string `json:"context_database"`
	ContextTable    *string `json:"context_table"`
	ContextTables   *string `json:"context_tables"`
	CreatedAt       string  `json:"created_at"`
	UpdatedAt       string  `json:"updated_at"`
}

// BrainMessage stores one chat turn.
type BrainMessage struct {
	ID        string  `json:"id"`
	ChatID    string  `json:"chat_id"`
	Role      string  `json:"role"`
	Content   string  `json:"content"`
	Status    string  `json:"status"`
	Error     *string `json:"error"`
	CreatedAt string  `json:"created_at"`
	UpdatedAt string  `json:"updated_at"`
}

// BrainArtifact stores generated artifacts linked to chats/messages.
type BrainArtifact struct {
	ID        string  `json:"id"`
	ChatID    string  `json:"chat_id"`
	MessageID *string `json:"message_id"`
	Type      string  `json:"type"`
	Title     string  `json:"title"`
	Content   string  `json:"content"`
	CreatedBy *string `json:"created_by"`
	CreatedAt string  `json:"created_at"`
}

// BrainToolCall stores tool invocation traces.
type BrainToolCall struct {
	ID         string  `json:"id"`
	ChatID     string  `json:"chat_id"`
	MessageID  string  `json:"message_id"`
	ToolName   string  `json:"tool_name"`
	InputJSON  string  `json:"input_json"`
	OutputJSON string  `json:"output_json"`
	Status     string  `json:"status"`
	Error      *string `json:"error"`
	CreatedAt  string  `json:"created_at"`
}

// BrainSkill stores admin-managed assistant instructions.
type BrainSkill struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	Content   string  `json:"content"`
	IsActive  bool    `json:"is_active"`
	IsDefault bool    `json:"is_default"`
	CreatedBy *string `json:"created_by"`
	CreatedAt string  `json:"created_at"`
	UpdatedAt string  `json:"updated_at"`
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func intToBool(v int) bool {
	return v != 0
}

func nullableString(value string) interface{} {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return strings.TrimSpace(value)
}

// GetBrainProviders returns all providers.
func (db *DB) GetBrainProviders() ([]BrainProvider, error) {
	rows, err := db.conn.Query(`SELECT id, name, kind, base_url, encrypted_api_key, is_active, is_default, created_by, created_at, updated_at FROM brain_providers ORDER BY is_default DESC, name ASC`)
	if err != nil {
		return nil, fmt.Errorf("get brain providers: %w", err)
	}
	defer rows.Close()

	providers := make([]BrainProvider, 0)
	for rows.Next() {
		var p BrainProvider
		var baseURL, encrypted, createdBy sql.NullString
		var active, def int
		if err := rows.Scan(&p.ID, &p.Name, &p.Kind, &baseURL, &encrypted, &active, &def, &createdBy, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan brain provider: %w", err)
		}
		p.BaseURL = nullStringToPtr(baseURL)
		p.HasAPIKey = encrypted.Valid && strings.TrimSpace(encrypted.String) != ""
		p.IsActive = intToBool(active)
		p.IsDefault = intToBool(def)
		p.CreatedBy = nullStringToPtr(createdBy)
		providers = append(providers, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate brain providers: %w", err)
	}
	return providers, nil
}

// GetBrainProviderByID returns provider config including encrypted key.
func (db *DB) GetBrainProviderByID(id string) (*BrainProviderSecret, error) {
	row := db.conn.QueryRow(`SELECT id, name, kind, base_url, encrypted_api_key, is_active, is_default, created_by, created_at, updated_at FROM brain_providers WHERE id = ?`, id)
	var p BrainProviderSecret
	var baseURL, encrypted, createdBy sql.NullString
	var active, def int
	if err := row.Scan(&p.ID, &p.Name, &p.Kind, &baseURL, &encrypted, &active, &def, &createdBy, &p.CreatedAt, &p.UpdatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get brain provider by id: %w", err)
	}
	p.BaseURL = nullStringToPtr(baseURL)
	p.EncryptedAPIKey = nullStringToPtr(encrypted)
	p.HasAPIKey = encrypted.Valid && strings.TrimSpace(encrypted.String) != ""
	p.IsActive = intToBool(active)
	p.IsDefault = intToBool(def)
	p.CreatedBy = nullStringToPtr(createdBy)
	return &p, nil
}

// CreateBrainProvider creates a provider.
func (db *DB) CreateBrainProvider(name, kind, baseURL string, encryptedAPIKey *string, isActive, isDefault bool, createdBy string) (string, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	id := uuid.NewString()

	tx, err := db.conn.Begin()
	if err != nil {
		return "", fmt.Errorf("begin create brain provider: %w", err)
	}
	defer tx.Rollback()

	if isDefault {
		if _, err := tx.Exec(`UPDATE brain_providers SET is_default = 0, updated_at = ?`, now); err != nil {
			return "", fmt.Errorf("clear default provider: %w", err)
		}
	}

	if _, err := tx.Exec(
		`INSERT INTO brain_providers (id, name, kind, base_url, encrypted_api_key, is_active, is_default, created_by, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id,
		strings.TrimSpace(name),
		strings.TrimSpace(kind),
		nullableString(baseURL),
		encryptedAPIKey,
		boolToInt(isActive),
		boolToInt(isDefault),
		nullableString(createdBy),
		now,
		now,
	); err != nil {
		return "", fmt.Errorf("insert brain provider: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("commit create brain provider: %w", err)
	}

	return id, nil
}

// UpdateBrainProvider updates a provider.
func (db *DB) UpdateBrainProvider(id, name, kind, baseURL string, encryptedAPIKey *string, updateAPIKey bool, isActive, isDefault bool) error {
	now := time.Now().UTC().Format(time.RFC3339)

	tx, err := db.conn.Begin()
	if err != nil {
		return fmt.Errorf("begin update brain provider: %w", err)
	}
	defer tx.Rollback()

	if isDefault {
		if _, err := tx.Exec(`UPDATE brain_providers SET is_default = 0, updated_at = ?`, now); err != nil {
			return fmt.Errorf("clear default provider: %w", err)
		}
	}

	if updateAPIKey {
		if _, err := tx.Exec(
			`UPDATE brain_providers
			 SET name = ?, kind = ?, base_url = ?, encrypted_api_key = ?, is_active = ?, is_default = ?, updated_at = ?
			 WHERE id = ?`,
			strings.TrimSpace(name),
			strings.TrimSpace(kind),
			nullableString(baseURL),
			encryptedAPIKey,
			boolToInt(isActive),
			boolToInt(isDefault),
			now,
			id,
		); err != nil {
			return fmt.Errorf("update brain provider: %w", err)
		}
	} else {
		if _, err := tx.Exec(
			`UPDATE brain_providers
			 SET name = ?, kind = ?, base_url = ?, is_active = ?, is_default = ?, updated_at = ?
			 WHERE id = ?`,
			strings.TrimSpace(name),
			strings.TrimSpace(kind),
			nullableString(baseURL),
			boolToInt(isActive),
			boolToInt(isDefault),
			now,
			id,
		); err != nil {
			return fmt.Errorf("update brain provider: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit update brain provider: %w", err)
	}
	return nil
}

// DeleteBrainProvider removes a provider and cascades dependent rows.
func (db *DB) DeleteBrainProvider(id string) error {
	if _, err := db.conn.Exec(`DELETE FROM brain_providers WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete brain provider: %w", err)
	}
	return nil
}

// GetBrainModels returns models, optionally for one provider.
func (db *DB) GetBrainModels(providerID string) ([]BrainModel, error) {
	query := `SELECT id, provider_id, name, display_name, is_active, is_default, created_at, updated_at FROM brain_models`
	args := []interface{}{}
	if strings.TrimSpace(providerID) != "" {
		query += ` WHERE provider_id = ?`
		args = append(args, providerID)
	}
	query += ` ORDER BY is_default DESC, name ASC`

	rows, err := db.conn.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("get brain models: %w", err)
	}
	defer rows.Close()

	models := make([]BrainModel, 0)
	for rows.Next() {
		var m BrainModel
		var display sql.NullString
		var active, def int
		if err := rows.Scan(&m.ID, &m.ProviderID, &m.Name, &display, &active, &def, &m.CreatedAt, &m.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan brain model: %w", err)
		}
		m.DisplayName = nullStringToPtr(display)
		m.IsActive = intToBool(active)
		m.IsDefault = intToBool(def)
		models = append(models, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate brain models: %w", err)
	}
	return models, nil
}

// GetBrainModelByID returns one model by id.
func (db *DB) GetBrainModelByID(id string) (*BrainModel, error) {
	row := db.conn.QueryRow(`SELECT id, provider_id, name, display_name, is_active, is_default, created_at, updated_at FROM brain_models WHERE id = ?`, id)
	var m BrainModel
	var display sql.NullString
	var active, def int
	if err := row.Scan(&m.ID, &m.ProviderID, &m.Name, &display, &active, &def, &m.CreatedAt, &m.UpdatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get brain model by id: %w", err)
	}
	m.DisplayName = nullStringToPtr(display)
	m.IsActive = intToBool(active)
	m.IsDefault = intToBool(def)
	return &m, nil
}

// EnsureBrainModel inserts or updates a model by provider+name and returns its id.
func (db *DB) EnsureBrainModel(providerID, name, displayName string) (string, error) {
	now := time.Now().UTC().Format(time.RFC3339)

	var existingID string
	row := db.conn.QueryRow(`SELECT id FROM brain_models WHERE provider_id = ? AND name = ?`, providerID, name)
	if err := row.Scan(&existingID); err != nil && err != sql.ErrNoRows {
		return "", fmt.Errorf("lookup brain model: %w", err)
	}

	if existingID != "" {
		if _, err := db.conn.Exec(`UPDATE brain_models SET display_name = ?, updated_at = ? WHERE id = ?`, nullableString(displayName), now, existingID); err != nil {
			return "", fmt.Errorf("update brain model: %w", err)
		}
		return existingID, nil
	}

	id := uuid.NewString()
	if _, err := db.conn.Exec(
		`INSERT INTO brain_models (id, provider_id, name, display_name, is_active, is_default, created_at, updated_at)
		 VALUES (?, ?, ?, ?, 0, 0, ?, ?)`,
		id, providerID, strings.TrimSpace(name), nullableString(displayName), now, now,
	); err != nil {
		return "", fmt.Errorf("insert brain model: %w", err)
	}

	return id, nil
}

// UpdateBrainModel updates model flags and display name.
func (db *DB) UpdateBrainModel(id string, displayName string, isActive, isDefault bool) error {
	now := time.Now().UTC().Format(time.RFC3339)

	tx, err := db.conn.Begin()
	if err != nil {
		return fmt.Errorf("begin update brain model: %w", err)
	}
	defer tx.Rollback()

	var providerID string
	if err := tx.QueryRow(`SELECT provider_id FROM brain_models WHERE id = ?`, id).Scan(&providerID); err != nil {
		if err == sql.ErrNoRows {
			return nil
		}
		return fmt.Errorf("load model provider: %w", err)
	}

	if isDefault {
		if _, err := tx.Exec(`UPDATE brain_models SET is_default = 0, updated_at = ? WHERE provider_id = ?`, now, providerID); err != nil {
			return fmt.Errorf("clear default model: %w", err)
		}
	}

	if _, err := tx.Exec(
		`UPDATE brain_models SET display_name = ?, is_active = ?, is_default = ?, updated_at = ? WHERE id = ?`,
		nullableString(displayName), boolToInt(isActive), boolToInt(isDefault), now, id,
	); err != nil {
		return fmt.Errorf("update brain model: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit update brain model: %w", err)
	}
	return nil
}

// SetBrainModelActive updates active flag for a model without touching default flag.
func (db *DB) SetBrainModelActive(id string, isActive bool) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.conn.Exec(
		`UPDATE brain_models SET is_active = ?, updated_at = ? WHERE id = ?`,
		boolToInt(isActive), now, id,
	); err != nil {
		return fmt.Errorf("set brain model active: %w", err)
	}
	return nil
}

// ClearDefaultBrainModelsByProvider clears default flag for all models under one provider.
func (db *DB) ClearDefaultBrainModelsByProvider(providerID string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.conn.Exec(
		`UPDATE brain_models SET is_default = 0, updated_at = ? WHERE provider_id = ?`,
		now, providerID,
	); err != nil {
		return fmt.Errorf("clear default brain models by provider: %w", err)
	}
	return nil
}

// ClearDefaultBrainModelByProviderExcept keeps one default and clears others for the same provider.
func (db *DB) ClearDefaultBrainModelByProviderExcept(providerID, keepModelID string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.conn.Exec(
		`UPDATE brain_models SET is_default = 0, updated_at = ? WHERE provider_id = ? AND id <> ?`,
		now, providerID, keepModelID,
	); err != nil {
		return fmt.Errorf("clear default brain model except: %w", err)
	}
	return nil
}

// GetDefaultBrainModelRuntime returns the default active model and provider.
func (db *DB) GetDefaultBrainModelRuntime() (*BrainModelRuntime, error) {
	row := db.conn.QueryRow(`
		SELECT
			m.id,
			m.name,
			p.id,
			p.name,
			p.kind,
			p.base_url,
			p.encrypted_api_key,
			m.is_active,
			p.is_active
		FROM brain_models m
		JOIN brain_providers p ON p.id = m.provider_id
		WHERE m.is_active = 1 AND p.is_active = 1
		ORDER BY m.is_default DESC, p.is_default DESC, m.created_at ASC
		LIMIT 1
	`)

	var rt BrainModelRuntime
	var baseURL, encrypted sql.NullString
	var mActive, pActive int
	if err := row.Scan(&rt.ModelID, &rt.ModelName, &rt.ProviderID, &rt.ProviderName, &rt.ProviderKind, &baseURL, &encrypted, &mActive, &pActive); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get default brain model runtime: %w", err)
	}
	if baseURL.Valid {
		rt.ProviderBaseURL = &baseURL.String
	}
	if encrypted.Valid {
		rt.ProviderEncryptedKey = &encrypted.String
	}
	rt.ModelActive = intToBool(mActive)
	rt.ProviderActive = intToBool(pActive)
	return &rt, nil
}

// GetBrainModelRuntimeByID returns model/provider runtime config.
func (db *DB) GetBrainModelRuntimeByID(modelID string) (*BrainModelRuntime, error) {
	row := db.conn.QueryRow(`
		SELECT
			m.id,
			m.name,
			p.id,
			p.name,
			p.kind,
			p.base_url,
			p.encrypted_api_key,
			m.is_active,
			p.is_active
		FROM brain_models m
		JOIN brain_providers p ON p.id = m.provider_id
		WHERE m.id = ?
		LIMIT 1
	`, modelID)

	var rt BrainModelRuntime
	var baseURL, encrypted sql.NullString
	var mActive, pActive int
	if err := row.Scan(&rt.ModelID, &rt.ModelName, &rt.ProviderID, &rt.ProviderName, &rt.ProviderKind, &baseURL, &encrypted, &mActive, &pActive); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get brain model runtime by id: %w", err)
	}
	if baseURL.Valid {
		rt.ProviderBaseURL = &baseURL.String
	}
	if encrypted.Valid {
		rt.ProviderEncryptedKey = &encrypted.String
	}
	rt.ModelActive = intToBool(mActive)
	rt.ProviderActive = intToBool(pActive)
	return &rt, nil
}

// GetBrainSkills lists all skills.
func (db *DB) GetBrainSkills() ([]BrainSkill, error) {
	rows, err := db.conn.Query(`SELECT id, name, content, is_active, is_default, created_by, created_at, updated_at FROM brain_skills ORDER BY is_default DESC, updated_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("get brain skills: %w", err)
	}
	defer rows.Close()

	skills := make([]BrainSkill, 0)
	for rows.Next() {
		var s BrainSkill
		var active, def int
		var createdBy sql.NullString
		if err := rows.Scan(&s.ID, &s.Name, &s.Content, &active, &def, &createdBy, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan brain skill: %w", err)
		}
		s.IsActive = intToBool(active)
		s.IsDefault = intToBool(def)
		s.CreatedBy = nullStringToPtr(createdBy)
		skills = append(skills, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate brain skills: %w", err)
	}
	return skills, nil
}

// GetActiveBrainSkill returns the active skill content.
func (db *DB) GetActiveBrainSkill() (*BrainSkill, error) {
	row := db.conn.QueryRow(`SELECT id, name, content, is_active, is_default, created_by, created_at, updated_at FROM brain_skills WHERE is_active = 1 ORDER BY is_default DESC, updated_at DESC LIMIT 1`)
	var s BrainSkill
	var active, def int
	var createdBy sql.NullString
	if err := row.Scan(&s.ID, &s.Name, &s.Content, &active, &def, &createdBy, &s.CreatedAt, &s.UpdatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get active brain skill: %w", err)
	}
	s.IsActive = intToBool(active)
	s.IsDefault = intToBool(def)
	s.CreatedBy = nullStringToPtr(createdBy)
	return &s, nil
}

// GetBrainSkillByID returns one skill by id.
func (db *DB) GetBrainSkillByID(id string) (*BrainSkill, error) {
	row := db.conn.QueryRow(`SELECT id, name, content, is_active, is_default, created_by, created_at, updated_at FROM brain_skills WHERE id = ?`, id)
	var s BrainSkill
	var active, def int
	var createdBy sql.NullString
	if err := row.Scan(&s.ID, &s.Name, &s.Content, &active, &def, &createdBy, &s.CreatedAt, &s.UpdatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get brain skill by id: %w", err)
	}
	s.IsActive = intToBool(active)
	s.IsDefault = intToBool(def)
	s.CreatedBy = nullStringToPtr(createdBy)
	return &s, nil
}

// UpsertDefaultBrainSkill stores a default skill, creating one if needed.
func (db *DB) UpsertDefaultBrainSkill(name, content, createdBy string) (string, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	var existingID string
	err := db.conn.QueryRow(`SELECT id FROM brain_skills WHERE is_default = 1 LIMIT 1`).Scan(&existingID)
	if err != nil && err != sql.ErrNoRows {
		return "", fmt.Errorf("lookup default brain skill: %w", err)
	}
	if existingID == "" {
		id := uuid.NewString()
		if _, err := db.conn.Exec(`INSERT INTO brain_skills (id, name, content, is_active, is_default, created_by, created_at, updated_at) VALUES (?, ?, ?, 1, 1, ?, ?, ?)`, id, name, content, nullableString(createdBy), now, now); err != nil {
			return "", fmt.Errorf("insert default brain skill: %w", err)
		}
		return id, nil
	}

	if _, err := db.conn.Exec(`UPDATE brain_skills SET name = ?, content = ?, is_active = 1, updated_at = ? WHERE id = ?`, name, content, now, existingID); err != nil {
		return "", fmt.Errorf("update default brain skill: %w", err)
	}
	return existingID, nil
}

// UpdateBrainSkill updates skill content and active/default flags.
func (db *DB) UpdateBrainSkill(id, name, content string, isActive, isDefault bool) error {
	now := time.Now().UTC().Format(time.RFC3339)
	tx, err := db.conn.Begin()
	if err != nil {
		return fmt.Errorf("begin update brain skill: %w", err)
	}
	defer tx.Rollback()

	if isDefault {
		if _, err := tx.Exec(`UPDATE brain_skills SET is_default = 0, updated_at = ?`, now); err != nil {
			return fmt.Errorf("clear default brain skill: %w", err)
		}
	}
	if isActive {
		if _, err := tx.Exec(`UPDATE brain_skills SET is_active = 0, updated_at = ?`, now); err != nil {
			return fmt.Errorf("clear active brain skill: %w", err)
		}
	}

	if _, err := tx.Exec(`UPDATE brain_skills SET name = ?, content = ?, is_active = ?, is_default = ?, updated_at = ? WHERE id = ?`, name, content, boolToInt(isActive), boolToInt(isDefault), now, id); err != nil {
		return fmt.Errorf("update brain skill: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit update brain skill: %w", err)
	}
	return nil
}

// CreateBrainSkill creates a new skill.
func (db *DB) CreateBrainSkill(name, content, createdBy string, isActive, isDefault bool) (string, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	id := uuid.NewString()
	tx, err := db.conn.Begin()
	if err != nil {
		return "", fmt.Errorf("begin create brain skill: %w", err)
	}
	defer tx.Rollback()

	if isDefault {
		if _, err := tx.Exec(`UPDATE brain_skills SET is_default = 0, updated_at = ?`, now); err != nil {
			return "", fmt.Errorf("clear default brain skill: %w", err)
		}
	}
	if isActive {
		if _, err := tx.Exec(`UPDATE brain_skills SET is_active = 0, updated_at = ?`, now); err != nil {
			return "", fmt.Errorf("clear active brain skill: %w", err)
		}
	}

	if _, err := tx.Exec(`INSERT INTO brain_skills (id, name, content, is_active, is_default, created_by, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, id, name, content, boolToInt(isActive), boolToInt(isDefault), nullableString(createdBy), now, now); err != nil {
		return "", fmt.Errorf("insert brain skill: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("commit create brain skill: %w", err)
	}
	return id, nil
}

// GetBrainChatsByUser returns chats for a user scoped to connection.
func (db *DB) GetBrainChatsByUser(username, connectionID string, includeArchived bool) ([]BrainChat, error) {
	query := `SELECT id, connection_id, username, title, provider_id, model_id, archived, last_message_at, context_database, context_table, context_tables, created_at, updated_at FROM brain_chats WHERE username = ? AND connection_id = ?`
	args := []interface{}{username, connectionID}
	if !includeArchived {
		query += ` AND archived = 0`
	}
	query += ` ORDER BY COALESCE(last_message_at, updated_at) DESC`

	rows, err := db.conn.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("get brain chats by user: %w", err)
	}
	defer rows.Close()

	chats := make([]BrainChat, 0)
	for rows.Next() {
		chat, err := scanBrainChat(rows)
		if err != nil {
			return nil, err
		}
		chats = append(chats, chat)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate brain chats: %w", err)
	}
	return chats, nil
}

func scanBrainChat(scanner interface {
	Scan(dest ...interface{}) error
}) (BrainChat, error) {
	var c BrainChat
	var providerID, modelID, lastMessageAt, ctxDB, ctxTable, ctxTables sql.NullString
	var archived int
	if err := scanner.Scan(&c.ID, &c.ConnectionID, &c.Username, &c.Title, &providerID, &modelID, &archived, &lastMessageAt, &ctxDB, &ctxTable, &ctxTables, &c.CreatedAt, &c.UpdatedAt); err != nil {
		return BrainChat{}, fmt.Errorf("scan brain chat: %w", err)
	}
	c.ProviderID = nullStringToPtr(providerID)
	c.ModelID = nullStringToPtr(modelID)
	c.LastMessageAt = nullStringToPtr(lastMessageAt)
	c.ContextDatabase = nullStringToPtr(ctxDB)
	c.ContextTable = nullStringToPtr(ctxTable)
	c.ContextTables = nullStringToPtr(ctxTables)
	c.Archived = intToBool(archived)
	return c, nil
}

// GetBrainChatByIDForUser loads one chat if owned by user.
func (db *DB) GetBrainChatByIDForUser(chatID, username string) (*BrainChat, error) {
	row := db.conn.QueryRow(`SELECT id, connection_id, username, title, provider_id, model_id, archived, last_message_at, context_database, context_table, context_tables, created_at, updated_at FROM brain_chats WHERE id = ? AND username = ?`, chatID, username)
	var c BrainChat
	var providerID, modelID, lastMessageAt, ctxDB, ctxTable, ctxTables sql.NullString
	var archived int
	if err := row.Scan(&c.ID, &c.ConnectionID, &c.Username, &c.Title, &providerID, &modelID, &archived, &lastMessageAt, &ctxDB, &ctxTable, &ctxTables, &c.CreatedAt, &c.UpdatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get brain chat by id for user: %w", err)
	}
	c.ProviderID = nullStringToPtr(providerID)
	c.ModelID = nullStringToPtr(modelID)
	c.LastMessageAt = nullStringToPtr(lastMessageAt)
	c.ContextDatabase = nullStringToPtr(ctxDB)
	c.ContextTable = nullStringToPtr(ctxTable)
	c.ContextTables = nullStringToPtr(ctxTables)
	c.Archived = intToBool(archived)
	return &c, nil
}

// CreateBrainChat creates a chat thread.
func (db *DB) CreateBrainChat(username, connectionID, title, providerID, modelID, contextDatabase, contextTable, contextTables string) (string, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	id := uuid.NewString()
	if strings.TrimSpace(title) == "" {
		title = "New Chat"
	}

	if _, err := db.conn.Exec(
		`INSERT INTO brain_chats (id, connection_id, username, title, provider_id, model_id, archived, last_message_at, context_database, context_table, context_tables, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, 0, NULL, ?, ?, ?, ?, ?)`,
		id, connectionID, username, strings.TrimSpace(title), nullableString(providerID), nullableString(modelID), nullableString(contextDatabase), nullableString(contextTable), nullableString(contextTables), now, now,
	); err != nil {
		return "", fmt.Errorf("create brain chat: %w", err)
	}
	return id, nil
}

// UpdateBrainChat updates mutable chat properties.
func (db *DB) UpdateBrainChat(chatID, title, providerID, modelID string, archived bool, contextDatabase, contextTable, contextTables string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.conn.Exec(`UPDATE brain_chats SET title = ?, provider_id = ?, model_id = ?, archived = ?, context_database = ?, context_table = ?, context_tables = ?, updated_at = ? WHERE id = ?`,
		strings.TrimSpace(title), nullableString(providerID), nullableString(modelID), boolToInt(archived), nullableString(contextDatabase), nullableString(contextTable), nullableString(contextTables), now, chatID); err != nil {
		return fmt.Errorf("update brain chat: %w", err)
	}
	return nil
}

// TouchBrainChat updates last activity timestamp.
func (db *DB) TouchBrainChat(chatID string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.conn.Exec(`UPDATE brain_chats SET last_message_at = ?, updated_at = ? WHERE id = ?`, now, now, chatID); err != nil {
		return fmt.Errorf("touch brain chat: %w", err)
	}
	return nil
}

// DeleteBrainChat deletes a chat.
func (db *DB) DeleteBrainChat(chatID string) error {
	if _, err := db.conn.Exec(`DELETE FROM brain_chats WHERE id = ?`, chatID); err != nil {
		return fmt.Errorf("delete brain chat: %w", err)
	}
	return nil
}

// GetBrainMessages lists all messages in a chat.
func (db *DB) GetBrainMessages(chatID string) ([]BrainMessage, error) {
	rows, err := db.conn.Query(`SELECT id, chat_id, role, content, status, error, created_at, updated_at FROM brain_messages WHERE chat_id = ? ORDER BY created_at ASC`, chatID)
	if err != nil {
		return nil, fmt.Errorf("get brain messages: %w", err)
	}
	defer rows.Close()

	messages := make([]BrainMessage, 0)
	for rows.Next() {
		var m BrainMessage
		var msgErr sql.NullString
		if err := rows.Scan(&m.ID, &m.ChatID, &m.Role, &m.Content, &m.Status, &msgErr, &m.CreatedAt, &m.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan brain message: %w", err)
		}
		m.Error = nullStringToPtr(msgErr)
		messages = append(messages, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate brain messages: %w", err)
	}
	return messages, nil
}

// CreateBrainMessage creates one message.
func (db *DB) CreateBrainMessage(chatID, role, content, status, errorText string) (string, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	id := uuid.NewString()
	if strings.TrimSpace(status) == "" {
		status = "complete"
	}
	if _, err := db.conn.Exec(`INSERT INTO brain_messages (id, chat_id, role, content, status, error, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, id, chatID, strings.TrimSpace(role), content, strings.TrimSpace(status), nullableString(errorText), now, now); err != nil {
		return "", fmt.Errorf("create brain message: %w", err)
	}
	return id, nil
}

// UpdateBrainMessage updates generated content/state.
func (db *DB) UpdateBrainMessage(id, content, status, errorText string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.conn.Exec(`UPDATE brain_messages SET content = ?, status = ?, error = ?, updated_at = ? WHERE id = ?`, content, strings.TrimSpace(status), nullableString(errorText), now, id); err != nil {
		return fmt.Errorf("update brain message: %w", err)
	}
	return nil
}

// CreateBrainArtifact stores a generated artifact.
func (db *DB) CreateBrainArtifact(chatID, messageID, artifactType, title, content, createdBy string) (string, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	id := uuid.NewString()
	if _, err := db.conn.Exec(`INSERT INTO brain_artifacts (id, chat_id, message_id, artifact_type, title, content, created_by, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, id, chatID, nullableString(messageID), strings.TrimSpace(artifactType), strings.TrimSpace(title), content, nullableString(createdBy), now); err != nil {
		return "", fmt.Errorf("create brain artifact: %w", err)
	}
	return id, nil
}

// GetBrainArtifacts lists artifacts for a chat.
func (db *DB) GetBrainArtifacts(chatID string) ([]BrainArtifact, error) {
	rows, err := db.conn.Query(`SELECT id, chat_id, message_id, artifact_type, title, content, created_by, created_at FROM brain_artifacts WHERE chat_id = ? ORDER BY created_at DESC`, chatID)
	if err != nil {
		return nil, fmt.Errorf("get brain artifacts: %w", err)
	}
	defer rows.Close()

	artifacts := make([]BrainArtifact, 0)
	for rows.Next() {
		var a BrainArtifact
		var messageID, createdBy sql.NullString
		if err := rows.Scan(&a.ID, &a.ChatID, &messageID, &a.Type, &a.Title, &a.Content, &createdBy, &a.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan brain artifact: %w", err)
		}
		a.MessageID = nullStringToPtr(messageID)
		a.CreatedBy = nullStringToPtr(createdBy)
		artifacts = append(artifacts, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate brain artifacts: %w", err)
	}
	return artifacts, nil
}

// CreateBrainToolCall stores a tool execution trace.
func (db *DB) CreateBrainToolCall(chatID, messageID, toolName, inputJSON, outputJSON, status, errorText string) (string, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	id := uuid.NewString()
	if _, err := db.conn.Exec(`INSERT INTO brain_tool_calls (id, chat_id, message_id, tool_name, input_json, output_json, status, error, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, id, chatID, messageID, toolName, inputJSON, outputJSON, status, nullableString(errorText), now); err != nil {
		return "", fmt.Errorf("create brain tool call: %w", err)
	}
	return id, nil
}

// GetBrainModelsWithProvider returns active models and provider metadata for UI pickers.
func (db *DB) GetBrainModelsWithProvider(activeOnly bool) ([]map[string]interface{}, error) {
	query := `
		SELECT
			m.id,
			m.name,
			COALESCE(m.display_name, ''),
			m.provider_id,
			p.name,
			p.kind,
			m.is_active,
			m.is_default,
			p.is_active,
			p.is_default
		FROM brain_models m
		JOIN brain_providers p ON p.id = m.provider_id
	`
	if activeOnly {
		query += ` WHERE m.is_active = 1 AND p.is_active = 1`
	}
	query += ` ORDER BY m.is_default DESC, p.is_default DESC, p.name ASC, m.name ASC`

	rows, err := db.conn.Query(query)
	if err != nil {
		return nil, fmt.Errorf("get brain models with provider: %w", err)
	}
	defer rows.Close()

	items := make([]map[string]interface{}, 0)
	for rows.Next() {
		var modelID, modelName, display, providerID, providerName, providerKind string
		var modelActive, modelDefault, providerActive, providerDefault int
		if err := rows.Scan(&modelID, &modelName, &display, &providerID, &providerName, &providerKind, &modelActive, &modelDefault, &providerActive, &providerDefault); err != nil {
			return nil, fmt.Errorf("scan brain model picker row: %w", err)
		}
		items = append(items, map[string]interface{}{
			"id":               modelID,
			"name":             modelName,
			"display_name":     display,
			"provider_id":      providerID,
			"provider_name":    providerName,
			"provider_kind":    providerKind,
			"is_active":        intToBool(modelActive),
			"is_default":       intToBool(modelDefault),
			"provider_active":  intToBool(providerActive),
			"provider_default": intToBool(providerDefault),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate brain model picker rows: %w", err)
	}
	return items, nil
}
