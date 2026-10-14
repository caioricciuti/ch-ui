package handlers

import (
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	braincore "github.com/caioricciuti/ch-ui/internal/brain"
	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
	"github.com/google/uuid"
)

// Budget periods, in UTC. Each period is resolved and checked on its own.
var budgetPeriods = []string{"day", "month"}

// budgetWarnPercent is the share of a limit from which answers carry a
// budget warning.
const budgetWarnPercent = 80

// proActive reports whether Pro features are on. Budget rules per role or
// person and dollar limits are Pro; without it only global token limits
// apply.
func proActive(cfg *config.Config) bool {
	return cfg != nil && cfg.IsPro()
}

// budgetIdentity is who a budget check is for: the ledger username (the
// actor), the role key user_roles and user rules use, and the app role.
type budgetIdentity struct {
	actor   string
	subject string
	role    string
}

func identityOf(s *middleware.SessionInfo) budgetIdentity {
	if s == nil {
		return budgetIdentity{}
	}
	return budgetIdentity{
		actor:   middleware.Actor(s),
		subject: database.RoleKey(s.ClickhouseUser, s.AuthSubject),
		role:    s.UserRole,
	}
}

func periodStart(period string, now time.Time) time.Time {
	y, m, d := now.UTC().Date()
	if period == "month" {
		return time.Date(y, m, 1, 0, 0, 0, 0, time.UTC)
	}
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func periodReset(period string, now time.Time) time.Time {
	if period == "month" {
		return periodStart(period, now).AddDate(0, 1, 0)
	}
	return periodStart(period, now).AddDate(0, 0, 1)
}

// chooseBudgetRule picks the rule that applies to a person in one period:
// their own rule, else their role's, else the global one (user > role >
// global). legacyDaily is the older single token budget, used as the
// global daily rule when no such rule exists. Without Pro only global
// rules count. The result may have no limits (an exemption) or be nil.
func chooseBudgetRule(rules []database.BrainBudget, id budgetIdentity, period string, legacyDaily int, pro bool) *database.BrainBudget {
	var user, role, global *database.BrainBudget
	for i := range rules {
		r := &rules[i]
		if r.Period != period {
			continue
		}
		switch r.Scope {
		case "user":
			if id.subject != "" && r.Subject == id.subject {
				user = r
			}
		case "role":
			if id.role != "" && r.Subject == id.role {
				role = r
			}
		case "global":
			global = r
		}
	}
	if global == nil && period == "day" && legacyDaily > 0 {
		limit := int64(legacyDaily)
		global = &database.BrainBudget{Scope: "global", Period: "day", TokenLimit: &limit}
	}
	switch {
	case pro && user != nil:
		return user
	case pro && role != nil:
		return role
	default:
		return global
	}
}

// pickBudgetRules is the rule to enforce in each period, without the
// exemptions. Without Pro dollar limits are ignored.
func pickBudgetRules(rules []database.BrainBudget, id budgetIdentity, legacyDaily int, pro bool) []database.BrainBudget {
	out := make([]database.BrainBudget, 0, len(budgetPeriods))
	for _, period := range budgetPeriods {
		chosen := chooseBudgetRule(rules, id, period, legacyDaily, pro)
		if chosen == nil {
			continue
		}
		rule := *chosen
		if !pro {
			rule.USDLimit = nil
			rule.BlockUnpriced = false
		}
		if rule.TokenLimit == nil && rule.USDLimit == nil {
			continue
		}
		out = append(out, rule)
	}
	return out
}

// legacyDailyBudget reads the older single token budget.
func legacyDailyBudget(db *database.DB) int {
	v, err := db.GetSetting(settingBrainDailyBudget)
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimSpace(v))
	return n
}

// resolveBudget loads the rules and picks those that apply to the person.
func resolveBudget(db *database.DB, id budgetIdentity, pro bool) ([]database.BrainBudget, error) {
	rules, err := db.ListBrainBudgets()
	if err != nil {
		return nil, err
	}
	return pickBudgetRules(rules, id, legacyDailyBudget(db), pro), nil
}

// budgetPeriodStatus is one applicable rule with what the person used of it.
type budgetPeriodStatus struct {
	Period        string   `json:"period"`
	Scope         string   `json:"scope"`
	Subject       string   `json:"subject,omitempty"`
	Rule          string   `json:"rule"`
	TokenLimit    *int64   `json:"token_limit"`
	USDLimit      *float64 `json:"usd_limit"`
	BlockUnpriced bool     `json:"block_unpriced"`
	UsedTokens    int64    `json:"used_tokens"`
	UsedUSD       float64  `json:"used_usd"`
	// Percent is the larger share used of the two limits.
	Percent  float64 `json:"percent"`
	Exceeded bool    `json:"exceeded"`
	ResetAt  string  `json:"reset_at"`
}

// budgetStatus is what a person used against each applicable rule. extra
// adds spend not in the ledger yet: the person's running turns.
func budgetStatus(db *database.DB, id budgetIdentity, pro bool, extra database.BrainSpend, now time.Time) ([]budgetPeriodStatus, error) {
	if db == nil {
		return nil, nil
	}
	rules, err := resolveBudget(db, id, pro)
	if err != nil {
		return nil, err
	}
	out := make([]budgetPeriodStatus, 0, len(rules))
	for _, rule := range rules {
		used, err := db.BrainSpendSince(id.actor, id.subject, periodStart(rule.Period, now))
		if err != nil {
			return nil, err
		}
		out = append(out, ruleStatus(rule, database.BrainSpend{
			Tokens:  used.Tokens + extra.Tokens,
			CostUSD: used.CostUSD + extra.CostUSD,
		}, now))
	}
	return out, nil
}

func ruleStatus(rule database.BrainBudget, used database.BrainSpend, now time.Time) budgetPeriodStatus {
	st := budgetPeriodStatus{
		Period:        rule.Period,
		Scope:         rule.Scope,
		Subject:       rule.Subject,
		Rule:          describeBudget(rule),
		TokenLimit:    rule.TokenLimit,
		USDLimit:      rule.USDLimit,
		BlockUnpriced: rule.BlockUnpriced,
		UsedTokens:    used.Tokens,
		UsedUSD:       roundCost(used.CostUSD),
		ResetAt:       periodReset(rule.Period, now).Format(time.RFC3339),
	}
	if rule.TokenLimit != nil {
		if *rule.TokenLimit <= 0 || used.Tokens >= *rule.TokenLimit {
			st.Exceeded = true
		}
		if *rule.TokenLimit > 0 {
			st.Percent = math.Max(st.Percent, float64(used.Tokens)/float64(*rule.TokenLimit)*100)
		}
	}
	if rule.USDLimit != nil {
		if *rule.USDLimit <= 0 || used.CostUSD >= *rule.USDLimit {
			st.Exceeded = true
		}
		if *rule.USDLimit > 0 {
			st.Percent = math.Max(st.Percent, used.CostUSD / *rule.USDLimit * 100)
		}
	}
	if st.Exceeded {
		st.Percent = math.Max(st.Percent, 100)
	}
	st.Percent = math.Round(st.Percent*10) / 10
	return st
}

// describeBudget names a rule the way the person reads it, such as "your
// role's daily Brain budget of $5.00".
func describeBudget(rule database.BrainBudget) string {
	owner := "the"
	switch rule.Scope {
	case "user":
		owner = "your"
	case "role":
		owner = "your role's"
	}
	period := "daily"
	if rule.Period == "month" {
		period = "monthly"
	}
	var limits []string
	if rule.USDLimit != nil {
		limits = append(limits, formatUSD(*rule.USDLimit))
	}
	if rule.TokenLimit != nil {
		limits = append(limits, formatTokenCount(int(*rule.TokenLimit))+" tokens")
	}
	return fmt.Sprintf("%s %s Brain budget of %s", owner, period, strings.Join(limits, " and "))
}

func formatUSD(v float64) string {
	if v != 0 && math.Abs(v) < 0.01 {
		return "$" + strconv.FormatFloat(v, 'f', 4, 64)
	}
	return "$" + strconv.FormatFloat(v, 'f', 2, 64)
}

// resetPhrase says when a period starts over.
func resetPhrase(period string, now time.Time) string {
	if period == "month" {
		return "It resets on " + periodReset(period, now).Format("Jan 2") + " at 00:00 UTC"
	}
	return "It resets at 00:00 UTC"
}

// budgetBlock returns why a person may not start a call on model, or "":
// a used-up limit, or a dollar rule that refuses unpriced models.
func budgetBlock(statuses []budgetPeriodStatus, model string, prices braincore.PriceTable, now time.Time) string {
	for _, st := range statuses {
		if st.USDLimit != nil && st.BlockUnpriced && model != "" && !prices.Priced(model) {
			return fmt.Sprintf("%s counts dollars, and %s has no price, so it cannot be used. Pick a priced model, or ask an admin to set its price in Admin > Brain.",
				capitalize(st.Rule), model)
		}
	}
	for _, st := range statuses {
		if !st.Exceeded {
			continue
		}
		return fmt.Sprintf("You have used %s (%s). %s; an admin can change it in Admin > Brain budgets.",
			st.Rule, usedPhrase(st), resetPhrase(st.Period, now))
	}
	return ""
}

func usedPhrase(st budgetPeriodStatus) string {
	var parts []string
	if st.USDLimit != nil {
		parts = append(parts, formatUSD(st.UsedUSD))
	}
	if st.TokenLimit != nil {
		parts = append(parts, formatTokenCount(int(st.UsedTokens))+" tokens")
	}
	return strings.Join(parts, " and ") + " used"
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// budgetWarning returns the status closest to its limit once it reaches
// budgetWarnPercent, for the quiet warning under an answer, or nil.
func budgetWarning(statuses []budgetPeriodStatus) *budgetPeriodStatus {
	var top *budgetPeriodStatus
	for i := range statuses {
		st := &statuses[i]
		if st.Percent < budgetWarnPercent {
			continue
		}
		if top == nil || st.Percent > top.Percent {
			top = st
		}
	}
	return top
}

// liveSpend holds what running chat turns used so far. A turn's usage
// reaches the ledger only when it ends, so the checks between agent steps
// add the person's other running turns from here.
type liveSpend struct {
	mu    sync.Mutex
	turns map[string]liveTurn
}

type liveTurn struct {
	subject string
	spend   database.BrainSpend
}

// start registers a running turn and returns its id.
func (l *liveSpend) start(subject string) string {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.turns == nil {
		l.turns = map[string]liveTurn{}
	}
	id := uuid.NewString()
	l.turns[id] = liveTurn{subject: subject}
	return id
}

// set records what a running turn used so far.
func (l *liveSpend) set(id string, s database.BrainSpend) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if t, ok := l.turns[id]; ok {
		t.spend = s
		l.turns[id] = t
	}
}

// end forgets a turn once its usage is in the ledger.
func (l *liveSpend) end(id string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.turns, id)
}

// others sums the running turns of subject, leaving out exclude.
func (l *liveSpend) others(subject, exclude string) database.BrainSpend {
	l.mu.Lock()
	defer l.mu.Unlock()
	var sum database.BrainSpend
	for id, t := range l.turns {
		if id == exclude || t.subject != subject {
			continue
		}
		sum.Tokens += t.spend.Tokens
		sum.CostUSD += t.spend.CostUSD
	}
	return sum
}

// spendOf is the budgeted tokens and the cost of one usage on model; an
// unpriced model counts toward tokens only.
func spendOf(u braincore.ChatResult, model string, prices braincore.PriceTable) database.BrainSpend {
	s := database.BrainSpend{Tokens: int64(u.Tokens())}
	if c, ok := prices.Cost(model, u.PriceUsage()); ok {
		s.CostUSD = c
	}
	return s
}

// costPtr is the cost to store for a usage: nil when the model is unpriced.
func costPtr(u braincore.ChatResult, model string, prices braincore.PriceTable) *float64 {
	c, ok := prices.Cost(model, u.PriceUsage())
	if !ok {
		return nil
	}
	c = roundCost(c)
	return &c
}

// checkBudget returns why the person may not start a call on model now,
// or "". Running turns of the person count, except the one asking.
func (h *BrainHandler) checkBudget(id budgetIdentity, model string, prices braincore.PriceTable, extra database.BrainSpend) string {
	if h.DB == nil {
		return ""
	}
	now := time.Now()
	statuses, err := budgetStatus(h.DB, id, proActive(h.Config), extra, now)
	if err != nil {
		slog.Warn("Failed to check Brain budget", "error", err)
		return ""
	}
	return budgetBlock(statuses, model, prices, now)
}

// budgetWarningFor is the budget field for an answer: the rule closest to
// its limit at or above budgetWarnPercent, or nil.
func (h *BrainHandler) budgetWarningFor(id budgetIdentity) *budgetPeriodStatus {
	if h.DB == nil {
		return nil
	}
	statuses, err := budgetStatus(h.DB, id, proActive(h.Config), h.live.others(id.subject, ""), time.Now())
	if err != nil {
		slog.Warn("Failed to read Brain budget", "error", err)
		return nil
	}
	return budgetWarning(statuses)
}
