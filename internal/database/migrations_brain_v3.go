package database

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/caioricciuti/ch-ui/internal/brain/pricing"
	"github.com/google/uuid"
)

// brainV3Marker is set once the one-time parts of migrateBrainV3 (the
// message cost backfill and the budget seed) ran, so a deleted global rule
// is not seeded again on the next start.
const brainV3Marker = "brain.v3_migrated_at"

// legacyDailyBudgetSetting is the single token budget that came before
// budget rules. It is still read as the global daily rule when no such
// rule exists.
const legacyDailyBudgetSetting = "brain.daily_token_budget"

// migrateBrainV3 adds Brain budgets: the cost of every ledger row and
// message stored at write time, the role and role key of the person on each
// ledger row, and the brain_budgets rule table. Rows written before it get
// their cost from the current price table, marked as a backfill.
// Idempotent: safe to run on every start.
func (db *DB) migrateBrainV3() error {
	columns := []struct{ table, name, def string }{
		{"brain_usage", "cost_usd", "REAL"},
		{"brain_usage", "price_source", "TEXT"},
		{"brain_usage", "role", "TEXT"},
		{"brain_usage", "subject", "TEXT"},
		{"brain_messages", "cost_usd", "REAL"},
	}
	for _, c := range columns {
		if err := db.ensureColumn(c.table, c.name, c.def); err != nil {
			return fmt.Errorf("brain v3: %w", err)
		}
	}

	return db.inImmediateTx(func(ctx context.Context, q sqlRunner) error {
		if _, err := q.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS brain_budgets (
			id TEXT PRIMARY KEY,
			scope TEXT NOT NULL CHECK (scope IN ('global', 'role', 'user')),
			subject TEXT NOT NULL DEFAULT '',
			period TEXT NOT NULL CHECK (period IN ('day', 'month')),
			token_limit INTEGER,
			usd_limit REAL,
			block_unpriced INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			UNIQUE (scope, subject, period)
		)`); err != nil {
			return fmt.Errorf("brain v3: create brain_budgets: %w", err)
		}

		settings, err := readSettingsOn(ctx, q, pricing.SettingKey, legacyDailyBudgetSetting, brainV3Marker)
		if err != nil {
			return fmt.Errorf("brain v3: %w", err)
		}
		prices := pricing.FromSettings(settings)

		if err := backfillBrainUsageCost(ctx, q, prices); err != nil {
			return fmt.Errorf("brain v3: %w", err)
		}
		if _, done := settings[brainV3Marker]; done {
			return nil
		}
		if err := backfillBrainMessageCost(ctx, q, prices); err != nil {
			return fmt.Errorf("brain v3: %w", err)
		}
		now := time.Now().UTC().Format(time.RFC3339)
		if n, _ := strconv.Atoi(strings.TrimSpace(settings[legacyDailyBudgetSetting])); n > 0 {
			if _, err := q.ExecContext(ctx, `INSERT INTO brain_budgets (id, scope, subject, period, token_limit, created_at, updated_at)
				VALUES (?, 'global', '', 'day', ?, ?, ?) ON CONFLICT(scope, subject, period) DO NOTHING`,
				uuid.NewString(), n, now, now); err != nil {
				return fmt.Errorf("brain v3: seed global budget: %w", err)
			}
		}
		if _, err := q.ExecContext(ctx, `INSERT INTO settings (key, value, updated_at) VALUES (?, ?, ?)
			ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
			brainV3Marker, now, now); err != nil {
			return fmt.Errorf("brain v3: mark done: %w", err)
		}
		return nil
	})
}

func readSettingsOn(ctx context.Context, q sqlRunner, keys ...string) (map[string]string, error) {
	out := map[string]string{}
	for _, k := range keys {
		rows, err := q.QueryContext(ctx, `SELECT value FROM settings WHERE key = ?`, k)
		if err != nil {
			return nil, fmt.Errorf("read setting %s: %w", k, err)
		}
		for rows.Next() {
			var v string
			if err := rows.Scan(&v); err != nil {
				rows.Close()
				return nil, fmt.Errorf("read setting %s: %w", k, err)
			}
			out[k] = v
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, fmt.Errorf("read setting %s: %w", k, err)
		}
	}
	return out, nil
}

type pricedRow struct {
	id, model string
	usage     pricing.Usage
}

// backfillBrainUsageCost prices every ledger row no price was ever applied
// to. Every visited row is marked 'backfill', also the unpriced ones (cost
// stays NULL), so a price added later never rewrites the past.
func backfillBrainUsageCost(ctx context.Context, q sqlRunner, prices pricing.PriceTable) error {
	rows, err := q.QueryContext(ctx, `SELECT id, model, input_tokens, output_tokens, cache_read_tokens, cache_write_tokens
		FROM brain_usage WHERE price_source IS NULL`)
	if err != nil {
		return fmt.Errorf("read usage to backfill: %w", err)
	}
	pending, err := scanPricedRows(rows)
	if err != nil {
		return err
	}
	for _, r := range pending {
		var cost any
		if c, ok := prices.Cost(r.model, r.usage); ok {
			cost = c
		}
		if _, err := q.ExecContext(ctx, `UPDATE brain_usage SET cost_usd = ?, price_source = 'backfill' WHERE id = ?`, cost, r.id); err != nil {
			return fmt.Errorf("backfill usage cost: %w", err)
		}
	}
	return nil
}

// backfillBrainMessageCost prices the assistant messages stored before
// costs were kept, so their footers keep showing a cost.
func backfillBrainMessageCost(ctx context.Context, q sqlRunner, prices pricing.PriceTable) error {
	rows, err := q.QueryContext(ctx, `SELECT id, model, input_tokens, output_tokens, cache_read_tokens, cache_write_tokens
		FROM brain_messages WHERE model IS NOT NULL AND cost_usd IS NULL`)
	if err != nil {
		return fmt.Errorf("read messages to backfill: %w", err)
	}
	pending, err := scanPricedRows(rows)
	if err != nil {
		return err
	}
	for _, r := range pending {
		c, ok := prices.Cost(r.model, r.usage)
		if !ok {
			continue
		}
		if _, err := q.ExecContext(ctx, `UPDATE brain_messages SET cost_usd = ? WHERE id = ?`, c, r.id); err != nil {
			return fmt.Errorf("backfill message cost: %w", err)
		}
	}
	return nil
}

// scanPricedRows reads every row before any update runs: the migration
// holds one connection, which cannot write while a result set is open.
func scanPricedRows(rows *sql.Rows) ([]pricedRow, error) {
	defer rows.Close()
	var out []pricedRow
	for rows.Next() {
		var r pricedRow
		if err := rows.Scan(&r.id, &r.model, &r.usage.InputTokens, &r.usage.OutputTokens, &r.usage.CacheReadTokens, &r.usage.CacheWriteTokens); err != nil {
			return nil, fmt.Errorf("scan row to backfill: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
