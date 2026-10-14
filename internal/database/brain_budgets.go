package database

import (
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// BrainBudget is one Brain budget rule. Every rule caps each person it
// applies to on their own: scope "global" applies to everyone, "role" to
// people with the role in Subject, "user" to the person whose role key is
// Subject. A nil limit is no limit, so a rule with neither limit exempts
// the people it matches from broader rules.
type BrainBudget struct {
	ID         string   `json:"id"`
	Scope      string   `json:"scope"`
	Subject    string   `json:"subject"`
	Period     string   `json:"period"`
	TokenLimit *int64   `json:"token_limit"`
	USDLimit   *float64 `json:"usd_limit"`
	// BlockUnpriced refuses models without a price while a dollar limit
	// applies, since their spend cannot be counted against it.
	BlockUnpriced bool   `json:"block_unpriced"`
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
}

// ErrBrainBudgetExists is returned when a rule for the same scope, subject
// and period already exists.
var ErrBrainBudgetExists = errors.New("a budget for this scope, subject and period already exists")

const brainBudgetColumns = `id, scope, subject, period, token_limit, usd_limit, block_unpriced, created_at, updated_at`

func scanBrainBudget(row interface{ Scan(...any) error }) (BrainBudget, error) {
	var b BrainBudget
	var tokens sql.NullInt64
	var usd sql.NullFloat64
	var block int
	if err := row.Scan(&b.ID, &b.Scope, &b.Subject, &b.Period, &tokens, &usd, &block, &b.CreatedAt, &b.UpdatedAt); err != nil {
		return BrainBudget{}, err
	}
	if tokens.Valid {
		v := tokens.Int64
		b.TokenLimit = &v
	}
	if usd.Valid {
		v := usd.Float64
		b.USDLimit = &v
	}
	b.BlockUnpriced = block == 1
	return b, nil
}

// ListBrainBudgets returns every rule: global first, then roles, then
// people, each by subject and period.
func (db *DB) ListBrainBudgets() ([]BrainBudget, error) {
	rows, err := db.conn.Query(`SELECT ` + brainBudgetColumns + ` FROM brain_budgets
		ORDER BY CASE scope WHEN 'global' THEN 0 WHEN 'role' THEN 1 ELSE 2 END, subject, period`)
	if err != nil {
		return nil, fmt.Errorf("list brain budgets: %w", err)
	}
	defer rows.Close()
	out := make([]BrainBudget, 0)
	for rows.Next() {
		b, err := scanBrainBudget(rows)
		if err != nil {
			return nil, fmt.Errorf("scan brain budget: %w", err)
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// GetBrainBudget returns one rule, or nil when it does not exist.
func (db *DB) GetBrainBudget(id string) (*BrainBudget, error) {
	b, err := scanBrainBudget(db.conn.QueryRow(`SELECT `+brainBudgetColumns+` FROM brain_budgets WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get brain budget: %w", err)
	}
	return &b, nil
}

func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

// CreateBrainBudget stores a new rule and returns its id.
func (db *DB) CreateBrainBudget(b BrainBudget) (string, error) {
	id := uuid.NewString()
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.conn.Exec(`INSERT INTO brain_budgets (`+brainBudgetColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, b.Scope, b.Subject, b.Period, b.TokenLimit, b.USDLimit, boolInt(b.BlockUnpriced), now, now)
	if isUniqueViolation(err) {
		return "", ErrBrainBudgetExists
	}
	if err != nil {
		return "", fmt.Errorf("create brain budget: %w", err)
	}
	return id, nil
}

// UpdateBrainBudget replaces a rule's scope, subject, period and limits.
func (db *DB) UpdateBrainBudget(b BrainBudget) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.conn.Exec(`UPDATE brain_budgets SET scope = ?, subject = ?, period = ?, token_limit = ?, usd_limit = ?,
		block_unpriced = ?, updated_at = ? WHERE id = ?`,
		b.Scope, b.Subject, b.Period, b.TokenLimit, b.USDLimit, boolInt(b.BlockUnpriced), now, b.ID)
	if isUniqueViolation(err) {
		return ErrBrainBudgetExists
	}
	if err != nil {
		return fmt.Errorf("update brain budget: %w", err)
	}
	return nil
}

// DeleteBrainBudget removes a rule.
func (db *DB) DeleteBrainBudget(id string) error {
	if _, err := db.conn.Exec(`DELETE FROM brain_budgets WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete brain budget: %w", err)
	}
	return nil
}

// SyncLegacyDailyBudget writes the global daily rule's token limit (0 when
// there is none) to the older daily token setting, so the setting never
// disagrees with the rules or comes back as the fallback after the rule
// is removed.
func (db *DB) SyncLegacyDailyBudget() error {
	var limit sql.NullInt64
	err := db.conn.QueryRow(`SELECT token_limit FROM brain_budgets WHERE scope = 'global' AND subject = '' AND period = 'day'`).Scan(&limit)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("read global brain budget: %w", err)
	}
	return db.SetSetting(legacyDailyBudgetSetting, strconv.FormatInt(limit.Int64, 10))
}

// SetGlobalDailyTokenBudget keeps the older daily token setting and the
// global daily rule in step: n > 0 sets the rule's token limit, creating
// the rule if needed; 0 clears the token limit and drops the rule when it
// has no dollar limit left.
func (db *DB) SetGlobalDailyTokenBudget(n int) error {
	if err := db.SetSetting(legacyDailyBudgetSetting, strconv.Itoa(n)); err != nil {
		return err
	}
	var existing *BrainBudget
	b, err := scanBrainBudget(db.conn.QueryRow(`SELECT ` + brainBudgetColumns + ` FROM brain_budgets WHERE scope = 'global' AND subject = '' AND period = 'day'`))
	switch {
	case errors.Is(err, sql.ErrNoRows):
	case err != nil:
		return fmt.Errorf("get global brain budget: %w", err)
	default:
		existing = &b
	}
	if n <= 0 {
		if existing == nil {
			return nil
		}
		if existing.USDLimit == nil {
			_, err := db.conn.Exec(`DELETE FROM brain_budgets WHERE id = ?`, existing.ID)
			return err
		}
		existing.TokenLimit = nil
		return db.UpdateBrainBudget(*existing)
	}
	limit := int64(n)
	if existing == nil {
		_, err := db.CreateBrainBudget(BrainBudget{Scope: "global", Period: "day", TokenLimit: &limit})
		return err
	}
	existing.TokenLimit = &limit
	return db.UpdateBrainBudget(*existing)
}
