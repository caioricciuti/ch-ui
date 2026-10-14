package database

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// BrainUsageRecord is one entry of the Brain token ledger: one chat turn
// (all its provider calls summed) or one Ask AI request.
type BrainUsageRecord struct {
	// Username is the actor shown in reports: the SSO email or the
	// ClickHouse user. Subject is the person's role key ("sso:<email>" or
	// the ClickHouse user), the form user_roles and budget rules use.
	Username         string
	Subject          string
	Role             string
	Provider         string
	Model            string
	Purpose          string // "chat" | "ask_ai" | "fix_sql" | "ask_ai_repair" | "fix_sql_repair"
	ChatID           string
	MessageID        string
	InputTokens      int
	OutputTokens     int
	CacheReadTokens  int
	CacheWriteTokens int
	// CostUSD is the cost at the prices in effect when the row was written;
	// nil when the model has no price.
	CostUSD *float64
}

// RecordBrainUsage appends one ledger row. Its cost is stored as given and
// never recomputed, so a later price change leaves it as it was.
func (db *DB) RecordBrainUsage(rec BrainUsageRecord) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.conn.Exec(`INSERT INTO brain_usage
		(id, username, subject, role, provider, model, purpose, chat_id, message_id, input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, cost_usd, price_source, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'table', ?)`,
		uuid.NewString(), strings.TrimSpace(rec.Username), nullableString(rec.Subject), nullableString(rec.Role),
		rec.Provider, rec.Model, rec.Purpose,
		nullableString(rec.ChatID), nullableString(rec.MessageID),
		rec.InputTokens, rec.OutputTokens, rec.CacheReadTokens, rec.CacheWriteTokens, rec.CostUSD, now)
	if err != nil {
		return fmt.Errorf("record brain usage: %w", err)
	}
	return nil
}

// SetBrainMessageUsage stores the model, token counts and cost (nil when
// unpriced) of one assistant message.
func (db *DB) SetBrainMessageUsage(messageID, provider, model string, input, output, cacheRead, cacheWrite int, cost *float64) error {
	_, err := db.conn.Exec(`UPDATE brain_messages SET provider = ?, model = ?, input_tokens = ?, output_tokens = ?,
		cache_read_tokens = ?, cache_write_tokens = ?, cost_usd = ? WHERE id = ?`,
		nullableString(provider), nullableString(model), input, output, cacheRead, cacheWrite, cost, messageID)
	if err != nil {
		return fmt.Errorf("set brain message usage: %w", err)
	}
	return nil
}

// BrainSpend is what one person used in a window: budgeted tokens (input,
// output and cache writes) and the dollars of the priced rows.
type BrainSpend struct {
	Tokens  int64   `json:"tokens"`
	CostUSD float64 `json:"cost_usd"`
}

// BrainSpendSince sums what one person used since the given time. username
// is the actor and subject the role key. Rows written before the role key
// was stored match on username alone, so an old SSO row still counts.
func (db *DB) BrainSpendSince(username, subject string, since time.Time) (BrainSpend, error) {
	var s BrainSpend
	err := db.conn.QueryRow(`SELECT COALESCE(SUM(input_tokens + output_tokens + cache_write_tokens), 0), COALESCE(SUM(cost_usd), 0)
		FROM brain_usage WHERE username = ? AND (subject = ? OR subject IS NULL) AND created_at >= ?`,
		strings.TrimSpace(username), strings.TrimSpace(subject), since.UTC().Format(time.RFC3339)).Scan(&s.Tokens, &s.CostUSD)
	if err != nil {
		return BrainSpend{}, fmt.Errorf("sum brain usage: %w", err)
	}
	return s, nil
}

// BrainPersonSpend is one person's use in a window, with the role their
// latest row recorded.
type BrainPersonSpend struct {
	Username string
	Subject  string
	Role     string
	BrainSpend
}

// BrainSpendByPerson sums the ledger since the given time per person. Rows
// from before the role key was stored join the person whose key is
// "sso:"+username when one exists, else stand for username itself.
func (db *DB) BrainSpendByPerson(since time.Time) ([]BrainPersonSpend, error) {
	rows, err := db.conn.Query(`SELECT username, COALESCE(subject, ''), COALESCE(role, ''), MAX(created_at),
			SUM(input_tokens + output_tokens + cache_write_tokens), COALESCE(SUM(cost_usd), 0)
		FROM brain_usage WHERE created_at >= ?
		GROUP BY username, subject`, since.UTC().Format(time.RFC3339))
	if err != nil {
		return nil, fmt.Errorf("brain usage by person: %w", err)
	}
	defer rows.Close()
	type part struct {
		BrainPersonSpend
		last string
	}
	var parts []part
	for rows.Next() {
		var p part
		if err := rows.Scan(&p.Username, &p.Subject, &p.Role, &p.last, &p.Tokens, &p.CostUSD); err != nil {
			return nil, fmt.Errorf("scan brain usage by person: %w", err)
		}
		parts = append(parts, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	byKey := map[string]*part{}
	order := []string{}
	add := func(key string, p part) {
		cur, ok := byKey[key]
		if !ok {
			p.Subject = key
			byKey[key] = &p
			order = append(order, key)
			return
		}
		cur.Tokens += p.Tokens
		cur.CostUSD += p.CostUSD
		if p.Role != "" && (cur.Role == "" || p.last > cur.last) {
			cur.Role, cur.last = p.Role, p.last
		}
	}
	var legacy []part
	for _, p := range parts {
		if p.Subject == "" {
			legacy = append(legacy, p)
			continue
		}
		add(p.Subject, p)
	}
	for _, p := range legacy {
		key := p.Username
		if _, ok := byKey[SSORoleKeyPrefix+p.Username]; ok {
			key = SSORoleKeyPrefix + p.Username
		}
		add(key, p)
	}
	out := make([]BrainPersonSpend, 0, len(order))
	for _, k := range order {
		out = append(out, byKey[k].BrainPersonSpend)
	}
	return out, nil
}

// BrainUsageRow is the ledger summed per group (person or role), provider
// and model.
type BrainUsageRow struct {
	// Group is the person or role the row sums; Username or Role repeats it
	// for the grouping in use. Role is "" for rows written before roles
	// were stored.
	Group     string `json:"group"`
	Username  string `json:"username,omitempty"`
	Role      string `json:"role,omitempty"`
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
	// CostUSD sums the stored cost of the priced requests; nil when none
	// was priced. UnpricedRequests had no price when written, and
	// Estimated is true when part of the cost was backfilled from the
	// prices in effect at the upgrade rather than stored at the time.
	CostUSD          *float64 `json:"cost_usd"`
	UnpricedRequests int      `json:"unpriced_requests"`
	Estimated        bool     `json:"estimated"`
}

// BrainUsageSummary returns the ledger between since and until, summed per
// group ("person" or "role"), provider and model, biggest spenders first.
func (db *DB) BrainUsageSummary(since, until time.Time, groupBy string) ([]BrainUsageRow, error) {
	group := "username"
	if groupBy == "role" {
		group = "COALESCE(role, '')"
	}
	rows, err := db.conn.Query(`SELECT `+group+`, provider, model, COUNT(*),
			SUM(CASE WHEN purpose = 'chat' THEN 1 ELSE 0 END),
			SUM(CASE WHEN purpose IN ('ask_ai', 'fix_sql') THEN 1 ELSE 0 END),
			SUM(CASE WHEN purpose IN ('ask_ai_repair', 'fix_sql_repair') THEN 1 ELSE 0 END),
			SUM(input_tokens), SUM(output_tokens), SUM(cache_read_tokens), SUM(cache_write_tokens), MAX(created_at),
			SUM(cost_usd), SUM(CASE WHEN cost_usd IS NULL THEN 1 ELSE 0 END),
			MAX(CASE WHEN cost_usd IS NOT NULL AND price_source = 'backfill' THEN 1 ELSE 0 END)
		FROM brain_usage WHERE created_at >= ? AND created_at < ?
		GROUP BY 1, provider, model
		ORDER BY SUM(input_tokens + output_tokens + cache_write_tokens) DESC, 1, model`,
		since.UTC().Format(time.RFC3339), until.UTC().Format(time.RFC3339))
	if err != nil {
		return nil, fmt.Errorf("brain usage summary: %w", err)
	}
	defer rows.Close()
	out := make([]BrainUsageRow, 0)
	for rows.Next() {
		var r BrainUsageRow
		var cost sql.NullFloat64
		var estimated int
		if err := rows.Scan(&r.Group, &r.Provider, &r.Model, &r.Requests, &r.ChatTurns, &r.AskAI, &r.Repairs,
			&r.InputTokens, &r.OutputTokens, &r.CacheReadTokens, &r.CacheWriteTokens, &r.LastUsedAt,
			&cost, &r.UnpricedRequests, &estimated); err != nil {
			return nil, fmt.Errorf("scan brain usage row: %w", err)
		}
		if groupBy == "role" {
			r.Role = r.Group
		} else {
			r.Username = r.Group
		}
		if cost.Valid {
			c := cost.Float64
			r.CostUSD = &c
		}
		r.Estimated = estimated == 1
		out = append(out, r)
	}
	return out, rows.Err()
}
