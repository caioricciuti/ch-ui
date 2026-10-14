package handlers

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	braincore "github.com/caioricciuti/ch-ui/internal/brain"
	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/database/dbtest"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
)

func i64(v int64) *int64                             { return &v }
func f64(v float64) *float64                         { return &v }
func proAccess(a config.ProAccess) *config.ProAccess { return &a }

// The most specific rule wins in each period on its own: the person's,
// then their role's, then the global one. Without Pro only global rules
// apply, without their dollar limits.
func TestPickBudgetRules(t *testing.T) {
	rules := []database.BrainBudget{
		{ID: "g-day", Scope: "global", Period: "day", TokenLimit: i64(1000), USDLimit: f64(1)},
		{ID: "g-month", Scope: "global", Period: "month", TokenLimit: i64(9000)},
		{ID: "r-day", Scope: "role", Subject: "analyst", Period: "day", USDLimit: f64(5), BlockUnpriced: true},
		{ID: "u-month", Scope: "user", Subject: "sso:ana@x.io", Period: "month", TokenLimit: i64(50)},
		{ID: "u-exempt", Scope: "user", Subject: "bob", Period: "day"},
	}
	ana := budgetIdentity{actor: "ana@x.io", subject: "sso:ana@x.io", role: "analyst"}
	cases := []struct {
		name   string
		id     budgetIdentity
		legacy int
		pro    bool
		want   string
	}{
		{"user and role over global", ana, 0, true, "r-day,u-month"},
		{"role only", budgetIdentity{actor: "cy", subject: "cy", role: "analyst"}, 0, true, "r-day,g-month"},
		{"global only", budgetIdentity{actor: "di", subject: "di", role: "viewer"}, 0, true, "g-day,g-month"},
		{"an exemption removes the day limit", budgetIdentity{actor: "bob", subject: "bob", role: "viewer"}, 0, true, "g-month"},
		{"the email alone is not the SSO key", budgetIdentity{actor: "ana@x.io", subject: "ana@x.io", role: "viewer"}, 0, true, "g-day,g-month"},
		{"license off: global only", ana, 0, false, "g-day,g-month"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := pickBudgetRules(rules, tc.id, tc.legacy, tc.pro)
			ids := make([]string, len(got))
			for i, r := range got {
				ids[i] = r.ID
				if !tc.pro && (r.USDLimit != nil || r.BlockUnpriced) {
					t.Errorf("license off keeps a dollar limit: %+v", r)
				}
			}
			if strings.Join(ids, ",") != tc.want {
				t.Errorf("got %v, want %s", ids, tc.want)
			}
		})
	}

	// The old single setting stands in for a missing global daily rule.
	got := pickBudgetRules(nil, ana, 300, false)
	if len(got) != 1 || got[0].Period != "day" || *got[0].TokenLimit != 300 {
		t.Errorf("legacy fallback %+v", got)
	}
	if got := pickBudgetRules(rules[:1], ana, 300, false); *got[0].TokenLimit != 1000 {
		t.Errorf("a global daily rule replaces the setting: %+v", got)
	}
}

// Days and months are UTC and reset at the next boundary.
func TestBudgetPeriodBoundaries(t *testing.T) {
	now := time.Date(2026, 12, 31, 23, 59, 59, 0, time.FixedZone("x", 3*3600))
	cases := []struct {
		period      string
		start, next string
	}{
		{"day", "2026-12-31T00:00:00Z", "2027-01-01T00:00:00Z"},
		{"month", "2026-12-01T00:00:00Z", "2027-01-01T00:00:00Z"},
	}
	for _, tc := range cases {
		if got := periodStart(tc.period, now).Format(time.RFC3339); got != tc.start {
			t.Errorf("%s start %s, want %s", tc.period, got, tc.start)
		}
		if got := periodReset(tc.period, now).Format(time.RFC3339); got != tc.next {
			t.Errorf("%s reset %s, want %s", tc.period, got, tc.next)
		}
	}
	if !strings.Contains(resetPhrase("month", now), "Jan 1") {
		t.Errorf("month reset phrase %q", resetPhrase("month", now))
	}
}

// A turn is blocked when either limit of a rule is reached; an unpriced
// model counts toward tokens only, unless the dollar rule blocks unpriced
// models.
func TestBudgetBlockLimits(t *testing.T) {
	now := time.Now()
	prices := braincore.PriceTable{"priced": {Input: f64(1), Output: f64(1)}}
	both := database.BrainBudget{Scope: "role", Subject: "analyst", Period: "day", TokenLimit: i64(100), USDLimit: f64(5)}
	cases := []struct {
		name    string
		rule    database.BrainBudget
		used    database.BrainSpend
		model   string
		blocked string
	}{
		{"under both", both, database.BrainSpend{Tokens: 99, CostUSD: 4.99}, "priced", ""},
		{"tokens reached", both, database.BrainSpend{Tokens: 100, CostUSD: 1}, "priced", "your role's daily Brain budget of $5.00 and 100 tokens"},
		{"dollars reached", both, database.BrainSpend{Tokens: 1, CostUSD: 5}, "priced", "($5.00 and 1 tokens used)"},
		{"unpriced allowed under a dollar cap", both, database.BrainSpend{Tokens: 50}, "llama", ""},
		{"unpriced blocked by the rule", func() database.BrainBudget { r := both; r.BlockUnpriced = true; return r }(), database.BrainSpend{}, "llama", "llama has no price"},
		{"block unpriced lets priced models through", func() database.BrainBudget { r := both; r.BlockUnpriced = true; return r }(), database.BrainSpend{}, "priced", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st := []budgetPeriodStatus{ruleStatus(tc.rule, tc.used, now)}
			got := budgetBlock(st, tc.model, prices, now)
			if (tc.blocked == "") != (got == "") || !strings.Contains(got, tc.blocked) {
				t.Errorf("got %q, want containing %q", got, tc.blocked)
			}
		})
	}
	if w := budgetWarning([]budgetPeriodStatus{ruleStatus(both, database.BrainSpend{Tokens: 80}, now)}); w == nil || w.Percent != 80 {
		t.Errorf("warning at 80%%: %+v", w)
	}
	if w := budgetWarning([]budgetPeriodStatus{ruleStatus(both, database.BrainSpend{Tokens: 79}, now)}); w != nil {
		t.Errorf("no warning below 80%%: %+v", w)
	}
}

// Running turns publish their usage to each other, from many goroutines.
func TestBudgetLiveSpendParallelTurns(t *testing.T) {
	var l liveSpend
	ids := make([]string, 8)
	for i := range ids {
		ids[i] = l.start("ana")
	}
	other := l.start("bob")
	l.set(other, database.BrainSpend{Tokens: 1000})
	var wg sync.WaitGroup
	for _, id := range ids {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			for n := int64(1); n <= 10; n++ {
				l.set(id, database.BrainSpend{Tokens: n, CostUSD: float64(n) / 10})
				_ = l.others("ana", id)
			}
		}(id)
	}
	wg.Wait()
	if got := l.others("ana", ""); got.Tokens != 80 {
		t.Errorf("all of ana's turns %+v", got)
	}
	if got := l.others("ana", ids[0]); got.Tokens != 70 {
		t.Errorf("ana's other turns %+v", got)
	}
	for _, id := range ids {
		l.end(id)
	}
	if got := l.others("ana", ""); got.Tokens != 0 {
		t.Errorf("ended turns still count: %+v", got)
	}
}

func budgetHandler(t *testing.T, access config.ProAccess) (*BrainHandler, *database.DB) {
	t.Helper()
	db, err := dbtest.Open(t, "budget.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	cfg := &config.Config{}
	cfg.SetProAccessForTest(func() config.ProAccess { return access })
	return &BrainHandler{DB: db, Config: cfg}, db
}

// A person rule keyed "sso:<email>" applies to the SSO session of that
// email, joined to the ledger rows recorded under the email.
func TestBrainBudgetSSOKeyJoin(t *testing.T) {
	h, db := budgetHandler(t, config.ProActive)
	if _, err := db.CreateBrainBudget(database.BrainBudget{Scope: "user", Subject: "sso:ana@x.io", Period: "day", TokenLimit: i64(100)}); err != nil {
		t.Fatal(err)
	}
	session := &middleware.SessionInfo{ClickhouseUser: "svc_sso", AuthSubject: "ana@x.io", UserRole: "analyst"}
	id := identityOf(session)
	h.recordQuickUsage(id, &database.BrainModelRuntime{ModelName: "m", ProviderKind: "anthropic"}, "ask_ai", &braincore.ChatResult{InputTokens: 100}, nil)
	msg := h.checkBudget(id, "m", nil, database.BrainSpend{})
	if !strings.Contains(msg, "your daily Brain budget of 100 tokens") {
		t.Errorf("SSO person not held to their rule: %q", msg)
	}
	other := identityOf(&middleware.SessionInfo{ClickhouseUser: "svc_sso", AuthSubject: "bo@x.io", UserRole: "analyst"})
	if msg := h.checkBudget(other, "m", nil, database.BrainSpend{}); msg != "" {
		t.Errorf("another SSO person on the same account is blocked: %q", msg)
	}
}

// The cost is stored when a request runs; editing prices later changes
// neither the ledger nor the message footer.
func TestBrainCostFixedAfterPriceEdit(t *testing.T) {
	db, router := brainUsageRouterWith(t, nil)
	h := &BrainHandler{DB: db}
	id := budgetIdentity{actor: "ana", subject: "ana", role: "analyst"}
	rt := &database.BrainModelRuntime{ModelName: "claude-sonnet-5-5", ProviderKind: "anthropic"}
	h.recordQuickUsage(id, rt, "ask_ai", &braincore.ChatResult{InputTokens: 1_000_000}, loadBrainSettings(db).Prices)

	if code, out := doJSON(t, router, http.MethodPut, "/brain/settings", `{"prices":{"claude-sonnet-5-5":{"input":100,"output":100}}}`); code != http.StatusOK {
		t.Fatalf("save prices %d %v", code, out)
	}
	_, out := doJSON(t, router, http.MethodGet, "/brain/usage?days=1&group_by=role", "")
	row := out["rows"].([]any)[0].(map[string]any)
	if row["cost_usd"] != float64(2) || row["role"] != "analyst" {
		t.Errorf("row after a price edit %v", row)
	}

	cost := 2.0
	m := database.BrainMessage{Role: "assistant", Model: &rt.ModelName, InputTokens: 1_000_000, CostUSD: &cost}
	if u := messageUsage(m); u["cost_usd"] != 2.0 {
		t.Errorf("footer %v", u)
	}
	m.CostUSD = nil
	if u := messageUsage(m); u["cost_usd"] != nil {
		t.Errorf("an unpriced message shows no cost, not $0: %v", u)
	}
}

// Rules per role or person and dollar limits need Pro: writing one without
// it is 402, and an existing one stays listed but inactive.
func TestBrainBudgetAdminProGate(t *testing.T) {
	db, h := brainUsageRouterWith(t, proAccess(config.ProNone))
	for _, body := range []string{
		`{"scope":"role","subject":"analyst","period":"day","token_limit":10}`,
		`{"scope":"user","subject":"ana","period":"month","token_limit":10}`,
		`{"scope":"global","period":"day","usd_limit":5}`,
	} {
		if code, out := doJSON(t, h, http.MethodPost, "/brain/budgets", body); code != http.StatusPaymentRequired {
			t.Errorf("%s: %d %v", body, code, out)
		}
	}
	if code, out := doJSON(t, h, http.MethodPost, "/brain/budgets", `{"scope":"global","period":"day","token_limit":500}`); code != http.StatusCreated {
		t.Fatalf("global token rule without Pro: %d %v", code, out)
	}
	if v, _ := db.GetSetting(settingBrainDailyBudget); v != "500" {
		t.Errorf("the old setting follows the global daily rule: %q", v)
	}
	roleID, err := db.CreateBrainBudget(database.BrainBudget{Scope: "role", Subject: "viewer", Period: "day", TokenLimit: i64(1)})
	if err != nil {
		t.Fatal(err)
	}
	if code, _ := doJSON(t, h, http.MethodPut, "/brain/budgets/"+roleID, `{"scope":"role","subject":"viewer","period":"day","token_limit":2}`); code != http.StatusPaymentRequired {
		t.Errorf("editing a role rule without Pro: %d", code)
	}
	_, out := doJSON(t, h, http.MethodGet, "/brain/budgets", "")
	found := false
	for _, b := range out["budgets"].([]any) {
		v := b.(map[string]any)
		if v["id"] == roleID {
			found = v["inactive"] == true
		}
	}
	if !found || out["pro"] != false {
		t.Errorf("role rule should be listed inactive: %v", out)
	}
	if code, _ := doJSON(t, h, http.MethodDelete, "/brain/budgets/"+roleID, ""); code != http.StatusOK {
		t.Errorf("deleting an inactive rule without Pro: %d", code)
	}
}

// With Pro, rules are created, validated, unique, and listed with the
// usage of the people they govern.
func TestBrainBudgetAdminCRUDAndUsage(t *testing.T) {
	db, h := brainUsageRouterWith(t, proAccess(config.ProActive))
	for _, bad := range []string{
		`{"scope":"role","subject":"owner","period":"day","token_limit":1}`,
		`{"scope":"user","subject":"","period":"day","token_limit":1}`,
		`{"scope":"global","period":"week","token_limit":1}`,
		`{"scope":"global","period":"day","token_limit":0}`,
		`{"scope":"global","period":"day","token_limit":5,"block_unpriced":true}`,
		`{"scope":"global","period":"day"}`,
	} {
		if code, _ := doJSON(t, h, http.MethodPost, "/brain/budgets", bad); code != http.StatusBadRequest {
			t.Errorf("%s: %d", bad, code)
		}
	}
	code, out := doJSON(t, h, http.MethodPost, "/brain/budgets", `{"scope":"role","subject":"Analyst","period":"day","usd_limit":5,"block_unpriced":true}`)
	if code != http.StatusCreated {
		t.Fatalf("create %d %v", code, out)
	}
	id := out["id"].(string)
	if code, _ := doJSON(t, h, http.MethodPost, "/brain/budgets", `{"scope":"role","subject":"analyst","period":"day","token_limit":5}`); code != http.StatusConflict {
		t.Errorf("duplicate rule: %d", code)
	}
	if code, out := doJSON(t, h, http.MethodPut, "/brain/budgets/"+id, `{"scope":"role","subject":"analyst","period":"day","usd_limit":4}`); code != http.StatusOK {
		t.Fatalf("update %d %v", code, out)
	}
	for i, spend := range []float64{3.2, 1} {
		c := spend
		if err := db.RecordBrainUsage(database.BrainUsageRecord{Username: fmt.Sprint("p", i), Subject: fmt.Sprint("p", i), Role: "analyst", Model: "m", Purpose: "chat", InputTokens: 10, CostUSD: &c}); err != nil {
			t.Fatal(err)
		}
	}
	_, out = doJSON(t, h, http.MethodGet, "/brain/budgets", "")
	rule := out["budgets"].([]any)[0].(map[string]any)
	usage := rule["usage"].(map[string]any)
	if rule["usd_limit"] != float64(4) || usage["people"] != float64(2) || usage["top_subject"] != "p0" || usage["top_percent"] != float64(80) {
		t.Errorf("rule %v", rule)
	}
	if code, _ := doJSON(t, h, http.MethodDelete, "/brain/budgets/"+id, ""); code != http.StatusOK {
		t.Errorf("delete %d", code)
	}
	if code, _ := doJSON(t, h, http.MethodDelete, "/brain/budgets/"+id, ""); code != http.StatusNotFound {
		t.Errorf("delete again %d", code)
	}
}
