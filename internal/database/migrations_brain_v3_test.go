package database

import (
	"database/sql"
	"math"
	"testing"
	"time"
)

func insertLegacyUsage(t *testing.T, db *DB, id, username, model string, input, output int, at time.Time) {
	t.Helper()
	if _, err := db.conn.Exec(`INSERT INTO brain_usage (id, username, provider, model, purpose, input_tokens, output_tokens, created_at)
		VALUES (?, ?, 'Claude', ?, 'chat', ?, ?, ?)`, id, username, model, input, output, at.UTC().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
}

func usageCost(t *testing.T, db *DB, id string) (sql.NullFloat64, string) {
	t.Helper()
	var cost sql.NullFloat64
	var source sql.NullString
	if err := db.conn.QueryRow(`SELECT cost_usd, price_source FROM brain_usage WHERE id = ?`, id).Scan(&cost, &source); err != nil {
		t.Fatal(err)
	}
	return cost, source.String
}

// The upgrade prices old ledger rows and messages once from the current
// table, seeds the global daily rule from the old setting, and reruns
// without changing anything: not the costs after a price edit, and not a
// rule the admin deleted.
func TestMigrateBrainV3BackfillSeedIdempotent(t *testing.T) {
	db := openTestDB(t)
	now := time.Now()
	insertLegacyUsage(t, db, "priced", "ana", "claude-sonnet-5-5", 1_000_000, 100_000, now)
	insertLegacyUsage(t, db, "unpriced", "ana", "llama", 10, 10, now)
	connID, err := db.CreateConnection(CreateConnectionParams{Name: "c", TunnelToken: "tok"})
	if err != nil {
		t.Fatal(err)
	}
	chatID, err := db.CreateBrainChat("ana", connID, "c", "", "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	msgID, err := db.CreateBrainMessage(chatID, "assistant", "hi", "complete", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.conn.Exec(`UPDATE brain_messages SET model = 'claude-sonnet-5-5', input_tokens = 1000000 WHERE id = ?`, msgID); err != nil {
		t.Fatal(err)
	}
	if err := db.SetSetting(legacyDailyBudgetSetting, "5000"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.conn.Exec(`DELETE FROM settings WHERE key = ?`, brainV3Marker); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 2; i++ {
		if err := db.migrateBrainV3(); err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
	}
	if err := db.runMigrations(); err != nil {
		t.Fatalf("full rerun: %v", err)
	}

	cost, source := usageCost(t, db, "priced")
	if !cost.Valid || math.Abs(cost.Float64-3.0) > 1e-9 || source != "backfill" {
		t.Errorf("priced row cost %v source %q, want 3.00 backfill", cost, source)
	}
	cost, source = usageCost(t, db, "unpriced")
	if cost.Valid || source != "backfill" {
		t.Errorf("unpriced row cost %v source %q, want NULL backfill", cost, source)
	}
	msgs, err := db.GetBrainMessages(chatID)
	if err != nil || len(msgs) != 1 || msgs[0].CostUSD == nil || math.Abs(*msgs[0].CostUSD-2.0) > 1e-9 {
		t.Fatalf("message cost %+v %v", msgs, err)
	}

	rules, err := db.ListBrainBudgets()
	if err != nil || len(rules) != 1 || rules[0].Scope != "global" || rules[0].Period != "day" ||
		rules[0].TokenLimit == nil || *rules[0].TokenLimit != 5000 {
		t.Fatalf("seeded rules %+v %v", rules, err)
	}

	// A price edit and a deleted rule survive another start.
	if err := db.SetSetting("brain.prices", `{"claude-sonnet-5-5":{"input":100,"output":100},"llama":{"input":1,"output":1}}`); err != nil {
		t.Fatal(err)
	}
	if err := db.DeleteBrainBudget(rules[0].ID); err != nil {
		t.Fatal(err)
	}
	if err := db.migrateBrainV3(); err != nil {
		t.Fatal(err)
	}
	if cost, _ := usageCost(t, db, "priced"); math.Abs(cost.Float64-3.0) > 1e-9 {
		t.Errorf("a price edit changed a past cost: %v", cost)
	}
	if cost, _ := usageCost(t, db, "unpriced"); cost.Valid {
		t.Errorf("a price added later backfilled an old unpriced row: %v", cost)
	}
	if rules, _ := db.ListBrainBudgets(); len(rules) != 0 {
		t.Errorf("a deleted rule was seeded again: %+v", rules)
	}
}

// A row stores the cost it was given and keeps it.
func TestRecordBrainUsageStoresCost(t *testing.T) {
	db := openTestDB(t)
	c := 0.25
	if err := db.RecordBrainUsage(BrainUsageRecord{Username: "ana", Subject: "ana", Role: "analyst", Provider: "Claude", Model: "m", Purpose: "chat", InputTokens: 5, CostUSD: &c}); err != nil {
		t.Fatal(err)
	}
	if err := db.RecordBrainUsage(BrainUsageRecord{Username: "ana", Subject: "ana", Role: "analyst", Provider: "Claude", Model: "x", Purpose: "chat", InputTokens: 7}); err != nil {
		t.Fatal(err)
	}
	rows, err := db.BrainUsageSummary(time.Now().Add(-time.Hour), time.Now().Add(time.Hour), "role")
	if err != nil || len(rows) != 2 {
		t.Fatalf("rows %+v %v", rows, err)
	}
	for _, r := range rows {
		if r.Group != "analyst" || r.Role != "analyst" || r.Estimated {
			t.Errorf("row %+v", r)
		}
		switch r.Model {
		case "m":
			if r.CostUSD == nil || *r.CostUSD != 0.25 || r.UnpricedRequests != 0 {
				t.Errorf("priced row %+v", r)
			}
		case "x":
			if r.CostUSD != nil || r.UnpricedRequests != 1 {
				t.Errorf("unpriced row %+v", r)
			}
		}
	}
}

// An SSO person's spend joins their "sso:" key, also for rows written
// before the key was stored, and a password user with the same name as
// the email does not pick up the SSO person's new rows.
func TestBrainSpendJoinsSSOKey(t *testing.T) {
	db := openTestDB(t)
	now := time.Now()
	insertLegacyUsage(t, db, "old", "ana@x.io", "m", 10, 0, now)
	one := 1.0
	for _, rec := range []BrainUsageRecord{
		{Username: "ana@x.io", Subject: "sso:ana@x.io", Role: "viewer", Model: "m", Purpose: "chat", InputTokens: 100, CostUSD: &one},
		{Username: "ana@x.io", Subject: "ana@x.io", Role: "admin", Model: "m", Purpose: "chat", InputTokens: 1000},
		{Username: "bob", Subject: "bob", Role: "analyst", Model: "m", Purpose: "chat", InputTokens: 1},
	} {
		if err := db.RecordBrainUsage(rec); err != nil {
			t.Fatal(err)
		}
	}
	since := now.Add(-time.Hour)
	sso, err := db.BrainSpendSince("ana@x.io", "sso:ana@x.io", since)
	if err != nil || sso.Tokens != 110 || sso.CostUSD != 1 {
		t.Errorf("sso spend %+v %v", sso, err)
	}
	pw, _ := db.BrainSpendSince("ana@x.io", "ana@x.io", since)
	if pw.Tokens != 1010 {
		t.Errorf("password user spend %+v (old unkeyed rows count for both, new rows only for their key)", pw)
	}

	people, err := db.BrainSpendByPerson(since)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]BrainPersonSpend{}
	for _, p := range people {
		got[p.Subject] = p
	}
	if len(got) != 3 || got["sso:ana@x.io"].Tokens != 110 || got["sso:ana@x.io"].Role != "viewer" || got["bob"].Role != "analyst" {
		t.Errorf("by person %+v", people)
	}
}

// The spend window starts at the given time: a row a second before it is
// left out.
func TestBrainSpendWindow(t *testing.T) {
	db := openTestDB(t)
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	insertLegacyUsage(t, db, "before", "ana", "m", 1, 0, start.Add(-time.Second))
	insertLegacyUsage(t, db, "at", "ana", "m", 10, 0, start)
	s, err := db.BrainSpendSince("ana", "ana", start)
	if err != nil || s.Tokens != 10 {
		t.Errorf("spend %+v %v", s, err)
	}
}

// Rules are unique per scope, subject and period; the old daily setting
// follows the global daily rule.
func TestBrainBudgetCRUDAndLegacySync(t *testing.T) {
	db := openTestDB(t)
	limit := int64(100)
	id, err := db.CreateBrainBudget(BrainBudget{Scope: "global", Period: "day", TokenLimit: &limit})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateBrainBudget(BrainBudget{Scope: "global", Period: "day", TokenLimit: &limit}); err != ErrBrainBudgetExists {
		t.Fatalf("duplicate: %v", err)
	}
	if err := db.SyncLegacyDailyBudget(); err != nil {
		t.Fatal(err)
	}
	if v, _ := db.GetSetting(legacyDailyBudgetSetting); v != "100" {
		t.Errorf("setting %q", v)
	}
	if err := db.SetGlobalDailyTokenBudget(250); err != nil {
		t.Fatal(err)
	}
	b, _ := db.GetBrainBudget(id)
	if b == nil || *b.TokenLimit != 250 {
		t.Fatalf("rule %+v", b)
	}
	if err := db.SetGlobalDailyTokenBudget(0); err != nil {
		t.Fatal(err)
	}
	if b, _ := db.GetBrainBudget(id); b != nil {
		t.Errorf("a token-only rule cleared to 0 is removed: %+v", b)
	}
	if err := db.DeleteBrainBudget(id); err != nil {
		t.Fatal(err)
	}
	if err := db.SyncLegacyDailyBudget(); err != nil {
		t.Fatal(err)
	}
	if v, _ := db.GetSetting(legacyDailyBudgetSetting); v != "0" {
		t.Errorf("setting after delete %q", v)
	}
}
