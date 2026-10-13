package database

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// BrainUsageRecord is one entry of the Brain token ledger: one chat turn
// (all its provider calls summed) or one Ask AI request.
type BrainUsageRecord struct {
	Username         string
	Provider         string
	Model            string
	Purpose          string // "chat" | "ask_ai" | "fix_sql" | "ask_ai_repair" | "fix_sql_repair"
	ChatID           string
	MessageID        string
	InputTokens      int
	OutputTokens     int
	CacheReadTokens  int
	CacheWriteTokens int
}

// RecordBrainUsage appends one ledger row.
func (db *DB) RecordBrainUsage(rec BrainUsageRecord) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.conn.Exec(`INSERT INTO brain_usage
		(id, username, provider, model, purpose, chat_id, message_id, input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		uuid.NewString(), strings.TrimSpace(rec.Username), rec.Provider, rec.Model, rec.Purpose,
		nullableString(rec.ChatID), nullableString(rec.MessageID),
		rec.InputTokens, rec.OutputTokens, rec.CacheReadTokens, rec.CacheWriteTokens, now)
	if err != nil {
		return fmt.Errorf("record brain usage: %w", err)
	}
	return nil
}

// SetBrainMessageUsage stores the model and token counts of one assistant
// message.
func (db *DB) SetBrainMessageUsage(messageID, provider, model string, input, output, cacheRead, cacheWrite int) error {
	_, err := db.conn.Exec(`UPDATE brain_messages SET provider = ?, model = ?, input_tokens = ?, output_tokens = ?,
		cache_read_tokens = ?, cache_write_tokens = ? WHERE id = ?`,
		nullableString(provider), nullableString(model), input, output, cacheRead, cacheWrite, messageID)
	if err != nil {
		return fmt.Errorf("set brain message usage: %w", err)
	}
	return nil
}

// BrainTokensUsedSince sums the budgeted tokens (input, output and cache
// writes) one person used since the given time.
func (db *DB) BrainTokensUsedSince(username string, since time.Time) (int, error) {
	var n int
	err := db.conn.QueryRow(`SELECT COALESCE(SUM(input_tokens + output_tokens + cache_write_tokens), 0)
		FROM brain_usage WHERE username = ? AND created_at >= ?`,
		strings.TrimSpace(username), since.UTC().Format(time.RFC3339)).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("sum brain usage: %w", err)
	}
	return n, nil
}

// BrainUsageRow is the ledger summed per person, provider and model.
type BrainUsageRow struct {
	Username  string `json:"username"`
	Provider  string `json:"provider"`
	Model     string `json:"model"`
	Requests  int    `json:"requests"`
	ChatTurns int    `json:"chat_turns"`
	// AskAI counts Ask AI and Fix with AI requests; Repairs counts the
	// extra calls that repaired SQL ClickHouse rejected.
	AskAI            int    `json:"ask_ai"`
	Repairs          int    `json:"repairs"`
	InputTokens      int    `json:"input_tokens"`
	OutputTokens     int    `json:"output_tokens"`
	CacheReadTokens  int    `json:"cache_read_tokens"`
	CacheWriteTokens int    `json:"cache_write_tokens"`
	LastUsedAt       string `json:"last_used_at"`
}

// BrainUsageSummary returns the ledger between since and until, summed per
// person, provider and model, biggest spenders first.
func (db *DB) BrainUsageSummary(since, until time.Time) ([]BrainUsageRow, error) {
	rows, err := db.conn.Query(`SELECT username, provider, model, COUNT(*),
			SUM(CASE WHEN purpose = 'chat' THEN 1 ELSE 0 END),
			SUM(CASE WHEN purpose IN ('ask_ai', 'fix_sql') THEN 1 ELSE 0 END),
			SUM(CASE WHEN purpose IN ('ask_ai_repair', 'fix_sql_repair') THEN 1 ELSE 0 END),
			SUM(input_tokens), SUM(output_tokens), SUM(cache_read_tokens), SUM(cache_write_tokens), MAX(created_at)
		FROM brain_usage WHERE created_at >= ? AND created_at < ?
		GROUP BY username, provider, model
		ORDER BY SUM(input_tokens + output_tokens + cache_write_tokens) DESC, username, model`,
		since.UTC().Format(time.RFC3339), until.UTC().Format(time.RFC3339))
	if err != nil {
		return nil, fmt.Errorf("brain usage summary: %w", err)
	}
	defer rows.Close()
	out := make([]BrainUsageRow, 0)
	for rows.Next() {
		var r BrainUsageRow
		if err := rows.Scan(&r.Username, &r.Provider, &r.Model, &r.Requests, &r.ChatTurns, &r.AskAI, &r.Repairs,
			&r.InputTokens, &r.OutputTokens, &r.CacheReadTokens, &r.CacheWriteTokens, &r.LastUsedAt); err != nil {
			return nil, fmt.Errorf("scan brain usage row: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
