package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
)

// appRoles are the roles a role budget can name.
var appRoles = map[string]bool{"admin": true, "analyst": true, "viewer": true}

// budgetRuleUsage is how the people a rule governs used it this period.
type budgetRuleUsage struct {
	// People counts who used Brain this period with this rule applying.
	People int `json:"people"`
	// Top is the person closest to the limit and what they used.
	TopSubject string  `json:"top_subject,omitempty"`
	TopTokens  int64   `json:"top_tokens"`
	TopUSD     float64 `json:"top_usd"`
	TopPercent float64 `json:"top_percent"`
	// OverLimit counts the people who reached the limit.
	OverLimit int    `json:"over_limit"`
	ResetAt   string `json:"reset_at"`
}

type budgetRuleView struct {
	database.BrainBudget
	Usage budgetRuleUsage `json:"usage"`
	// Inactive is true when the license does not cover the rule (a role or
	// person rule without Pro); IgnoredUSD when only its dollar limit is
	// not covered.
	Inactive   bool `json:"inactive"`
	IgnoredUSD bool `json:"ignored_usd"`
}

// ListBrainBudgets returns every budget rule with this period's usage of
// the people it governs.
func (h *AdminHandler) ListBrainBudgets(w http.ResponseWriter, r *http.Request) {
	rules, err := h.DB.ListBrainBudgets()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to load Brain budgets")
		return
	}
	pro := proActive(h.Config)
	now := time.Now()
	legacy := legacyDailyBudget(h.DB)

	views := make([]budgetRuleView, len(rules))
	index := map[string]int{}
	for i, rule := range rules {
		views[i] = budgetRuleView{
			BrainBudget: rule,
			Inactive:    !pro && rule.Scope != "global",
			IgnoredUSD:  !pro && rule.USDLimit != nil,
		}
		views[i].Usage.ResetAt = periodReset(rule.Period, now).Format(time.RFC3339)
		index[rule.ID] = i
	}
	for _, period := range budgetPeriods {
		people, err := h.DB.BrainSpendByPerson(periodStart(period, now))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "Failed to load Brain usage")
			return
		}
		for _, p := range people {
			rule := chooseBudgetRule(rules, budgetIdentity{actor: p.Username, subject: p.Subject, role: p.Role}, period, legacy, pro)
			if rule == nil || rule.ID == "" {
				continue
			}
			v := &views[index[rule.ID]]
			effective := *rule
			if !pro {
				effective.USDLimit = nil
			}
			st := ruleStatus(effective, p.BrainSpend, now)
			v.Usage.People++
			if st.Exceeded && (effective.TokenLimit != nil || effective.USDLimit != nil) {
				v.Usage.OverLimit++
			}
			if v.Usage.TopSubject == "" || st.Percent > v.Usage.TopPercent ||
				(st.Percent == v.Usage.TopPercent && p.Tokens > v.Usage.TopTokens) {
				v.Usage.TopSubject = p.Subject
				v.Usage.TopTokens = p.Tokens
				v.Usage.TopUSD = roundCost(p.CostUSD)
				v.Usage.TopPercent = st.Percent
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"budgets": views,
		"pro":     pro,
		// legacyDailyTokens is the older single budget, still enforced as
		// the global daily rule when no such rule exists.
		"legacyDailyTokens": legacy,
	})
}

type budgetRuleBody struct {
	Scope         string   `json:"scope"`
	Subject       string   `json:"subject"`
	Period        string   `json:"period"`
	TokenLimit    *int64   `json:"token_limit"`
	USDLimit      *float64 `json:"usd_limit"`
	BlockUnpriced bool     `json:"block_unpriced"`
}

// parseBudgetRule validates a rule. status is 400 for a bad rule and 402
// when the rule needs Pro and the license does not cover it.
func (h *AdminHandler) parseBudgetRule(r *http.Request) (database.BrainBudget, int, string) {
	var body budgetRuleBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return database.BrainBudget{}, http.StatusBadRequest, "Invalid request body"
	}
	b := database.BrainBudget{
		Scope:         strings.TrimSpace(body.Scope),
		Subject:       strings.TrimSpace(body.Subject),
		Period:        strings.TrimSpace(body.Period),
		TokenLimit:    body.TokenLimit,
		USDLimit:      body.USDLimit,
		BlockUnpriced: body.BlockUnpriced,
	}
	switch b.Scope {
	case "global":
		b.Subject = ""
	case "role":
		b.Subject = strings.ToLower(b.Subject)
		if !appRoles[b.Subject] {
			return b, http.StatusBadRequest, "A role budget needs a role: admin, analyst or viewer"
		}
	case "user":
		if b.Subject == "" || len(b.Subject) > 320 {
			return b, http.StatusBadRequest, "A person budget needs a person"
		}
	default:
		return b, http.StatusBadRequest, "Scope must be global, role or user"
	}
	if b.Period != "day" && b.Period != "month" {
		return b, http.StatusBadRequest, "Period must be day or month"
	}
	if b.TokenLimit != nil && *b.TokenLimit <= 0 {
		return b, http.StatusBadRequest, "The token limit must be above 0; leave it empty for no token limit"
	}
	if b.USDLimit != nil && (*b.USDLimit <= 0 || math.IsInf(*b.USDLimit, 0) || math.IsNaN(*b.USDLimit)) {
		return b, http.StatusBadRequest, "The dollar limit must be above 0; leave it empty for no dollar limit"
	}
	if b.BlockUnpriced && b.USDLimit == nil {
		return b, http.StatusBadRequest, "Blocking unpriced models needs a dollar limit"
	}
	if b.Scope == "global" && b.TokenLimit == nil && b.USDLimit == nil {
		return b, http.StatusBadRequest, "A global budget needs a token or a dollar limit"
	}
	if (b.Scope != "global" || b.USDLimit != nil) && !proActive(h.Config) {
		return b, http.StatusPaymentRequired, "Budgets per role or person and dollar limits require a Pro license"
	}
	return b, 0, ""
}

// CreateBrainBudget adds a budget rule.
func (h *AdminHandler) CreateBrainBudget(w http.ResponseWriter, r *http.Request) {
	b, status, msg := h.parseBudgetRule(r)
	if status != 0 {
		writeError(w, status, msg)
		return
	}
	id, err := h.DB.CreateBrainBudget(b)
	if errors.Is(err, database.ErrBrainBudgetExists) {
		writeError(w, http.StatusConflict, "A budget for this scope, subject and period already exists; edit that one")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to save Brain budget")
		return
	}
	h.afterBudgetChange(r, "brain.budget.created", id, b)
	writeJSON(w, http.StatusCreated, map[string]any{"success": true, "id": id})
}

// UpdateBrainBudget replaces a budget rule.
func (h *AdminHandler) UpdateBrainBudget(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	existing, err := h.DB.GetBrainBudget(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to load Brain budget")
		return
	}
	if existing == nil {
		writeError(w, http.StatusNotFound, "Budget not found")
		return
	}
	b, status, msg := h.parseBudgetRule(r)
	if status != 0 {
		writeError(w, status, msg)
		return
	}
	b.ID = id
	if err := h.DB.UpdateBrainBudget(b); errors.Is(err, database.ErrBrainBudgetExists) {
		writeError(w, http.StatusConflict, "A budget for this scope, subject and period already exists; edit that one")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to save Brain budget")
		return
	}
	h.afterBudgetChange(r, "brain.budget.updated", id, b)
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

// DeleteBrainBudget removes a budget rule. Allowed without Pro, so an admin
// can clear rules the license no longer covers.
func (h *AdminHandler) DeleteBrainBudget(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	existing, err := h.DB.GetBrainBudget(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to load Brain budget")
		return
	}
	if existing == nil {
		writeError(w, http.StatusNotFound, "Budget not found")
		return
	}
	if err := h.DB.DeleteBrainBudget(id); err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to delete Brain budget")
		return
	}
	h.afterBudgetChange(r, "brain.budget.deleted", id, *existing)
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (h *AdminHandler) afterBudgetChange(r *http.Request, action, id string, b database.BrainBudget) {
	if err := h.DB.SyncLegacyDailyBudget(); err != nil {
		slog.Warn("Failed to sync the daily Brain budget setting", "error", err)
	}
	h.auditBrain(r, action, fmt.Sprintf("id=%s scope=%s subject=%s period=%s", id, b.Scope, b.Subject, b.Period))
}

// GetMyUsage returns what the signed-in person used of Brain today and this
// month (UTC), and each budget rule that applies to them.
func (h *BrainHandler) GetMyUsage(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}
	id := identityOf(session)
	now := time.Now()
	usage := map[string]any{}
	for _, period := range budgetPeriods {
		spend, err := h.DB.BrainSpendSince(id.actor, id.subject, periodStart(period, now))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "Failed to load Brain usage")
			return
		}
		usage[period] = map[string]any{
			"tokens":   spend.Tokens,
			"cost_usd": roundCost(spend.CostUSD),
			"reset_at": periodReset(period, now).Format(time.RFC3339),
		}
	}
	statuses, err := budgetStatus(h.DB, id, proActive(h.Config), h.live.others(id.subject, ""), now)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to load Brain budgets")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "usage": usage, "budgets": statuses})
}
