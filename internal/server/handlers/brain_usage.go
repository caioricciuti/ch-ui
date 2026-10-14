package handlers

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"strings"

	braincore "github.com/caioricciuti/ch-ui/internal/brain"
	"github.com/caioricciuti/ch-ui/internal/brain/pricing"
	"github.com/caioricciuti/ch-ui/internal/database"
)

// Brain settings live in the settings table under these keys.
const (
	settingBrainAgentModel  = "brain.agent_model_id"
	settingBrainQuickModel  = "brain.quick_model_id"
	settingBrainDailyBudget = "brain.daily_token_budget"
	settingBrainMaxOutput   = "brain.max_output_tokens"
	settingBrainPrices      = pricing.SettingKey
)

// brainSettings are the admin's Brain choices: which model answers chat
// (agent) and quick tasks (Ask AI), the per-person daily token budget, the
// output cap and the price table used to estimate cost.
type brainSettings struct {
	AgentModelID     string               `json:"agentModelId"`
	QuickModelID     string               `json:"quickModelId"`
	DailyTokenBudget int                  `json:"dailyTokenBudget"`
	MaxOutputTokens  int                  `json:"maxOutputTokens"`
	Prices           braincore.PriceTable `json:"prices"`
}

// loadBrainSettings reads the settings. An install that never saved prices
// gets the default Claude prices; once saved, the stored table is used as is.
func loadBrainSettings(db *database.DB) brainSettings {
	s := brainSettings{Prices: braincore.DefaultPrices()}
	if db == nil {
		return s
	}
	all, err := db.GetAllSettings()
	if err != nil {
		slog.Warn("Failed to load Brain settings", "error", err)
		return s
	}
	s.AgentModelID = strings.TrimSpace(all[settingBrainAgentModel])
	s.QuickModelID = strings.TrimSpace(all[settingBrainQuickModel])
	s.DailyTokenBudget, _ = strconv.Atoi(strings.TrimSpace(all[settingBrainDailyBudget]))
	s.MaxOutputTokens, _ = strconv.Atoi(strings.TrimSpace(all[settingBrainMaxOutput]))
	s.Prices = pricing.FromSettings(all)
	return s
}

// Stop reasons the agent loop sets itself, next to the provider's "length"
// and "refusal".
const (
	stopReasonToolLimit = "tool_limit"
	stopReasonBudget    = "budget"
)

// turnBudgetStop records what the running turn used so far and returns why
// it must stop, or "": the person's stored usage, this turn and their other
// running turns reached an applicable limit. The turn's own usage reaches
// the ledger only when it ends.
func (h *BrainHandler) turnBudgetStop(t brainTurn) string {
	if h.DB == nil || t.usage == nil || t.runtimeModel == nil {
		return ""
	}
	h.live.set(t.liveID, spendOf(*t.usage, t.runtimeModel.ModelName, t.prices))
	return h.checkBudget(t.ident, "", t.prices, h.live.others(t.ident.subject, ""))
}

// addTurnUsage sums one provider call into the turn and publishes the
// running total to the other turns' budget checks.
func (h *BrainHandler) addTurnUsage(t brainTurn, res *braincore.ChatResult) {
	if t.usage == nil || res == nil {
		return
	}
	t.usage.Add(res)
	if t.runtimeModel != nil {
		h.live.set(t.liveID, spendOf(*t.usage, t.runtimeModel.ModelName, t.prices))
	}
}

func formatTokenCount(n int) string {
	s := strconv.Itoa(n)
	var out []byte
	for i := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, s[i])
	}
	return string(out)
}

// usagePayload is the usage the UI shows under a message. cost is the
// stored cost; nil means unpriced and no cost is shown.
func usagePayload(provider, model string, u braincore.ChatResult, cost *float64) map[string]any {
	out := map[string]any{
		"provider":           provider,
		"model":              model,
		"input_tokens":       u.InputTokens,
		"output_tokens":      u.OutputTokens,
		"cache_read_tokens":  u.CacheReadTokens,
		"cache_write_tokens": u.CacheWriteTokens,
	}
	if cost != nil {
		out["cost_usd"] = roundCost(*cost)
	}
	return out
}

func roundCost(v float64) float64 {
	return math.Round(v*1e6) / 1e6
}

// stopNote tells the person why an answer ended early.
func stopNote(reason string) string {
	switch reason {
	case "length":
		return "\n\n_The answer was cut off because it reached the output token limit. Ask me to continue for the rest._"
	case "refusal":
		return "\n\n_The model declined to answer this request._"
	case stopReasonToolLimit:
		return fmt.Sprintf("\n\n_Stopped after %d steps in one turn. Ask me to continue if there is more to do._", brainMaxIterations)
	default:
		return ""
	}
}

// providerLabel is how the ledger names a provider: its admin name.
func providerLabel(rt *database.BrainModelRuntime) string {
	if rt == nil {
		return ""
	}
	if strings.TrimSpace(rt.ProviderName) != "" {
		return rt.ProviderName
	}
	return rt.ProviderKind
}

// saveTurnUsage stores the summed usage of one chat turn on its assistant
// message and in the ledger. It runs once the turn ended, however it ended.
func (h *BrainHandler) saveTurnUsage(t *brainTurn) {
	if t == nil || t.usage == nil || h.DB == nil || t.runtimeModel == nil {
		return
	}
	if t.usageSaved != nil {
		if *t.usageSaved {
			return
		}
		*t.usageSaved = true
	}
	// The running total leaves the live map once the row is stored; until
	// then it keeps counting for the person's other turns.
	defer h.live.end(t.liveID)
	u := *t.usage
	provider := providerLabel(t.runtimeModel)
	model := t.runtimeModel.ModelName
	cost := costPtr(u, model, t.prices)
	if err := h.DB.SetBrainMessageUsage(t.assistantMessageID, provider, model, u.InputTokens, u.OutputTokens, u.CacheReadTokens, u.CacheWriteTokens, cost); err != nil {
		slog.Warn("Failed to store Brain message usage", "error", err)
	}
	if u.InputTokens+u.OutputTokens+u.CacheReadTokens+u.CacheWriteTokens == 0 {
		return
	}
	if err := h.DB.RecordBrainUsage(database.BrainUsageRecord{
		Username:         t.ident.actor,
		Subject:          t.ident.subject,
		Role:             t.ident.role,
		CostUSD:          cost,
		Provider:         provider,
		Model:            model,
		Purpose:          "chat",
		ChatID:           t.chatID,
		MessageID:        t.assistantMessageID,
		InputTokens:      u.InputTokens,
		OutputTokens:     u.OutputTokens,
		CacheReadTokens:  u.CacheReadTokens,
		CacheWriteTokens: u.CacheWriteTokens,
	}); err != nil {
		slog.Warn("Failed to record Brain usage", "error", err)
	}
}

// recordQuickUsage stores one Ask AI or Fix with AI call in the ledger.
// purpose is ask_ai, fix_sql, or ask_ai_repair / fix_sql_repair for a call
// that repairs SQL ClickHouse rejected.
func (h *BrainHandler) recordQuickUsage(id budgetIdentity, rt *database.BrainModelRuntime, purpose string, u *braincore.ChatResult, prices braincore.PriceTable) {
	if h.DB == nil || rt == nil || u == nil || u.InputTokens+u.OutputTokens+u.CacheReadTokens+u.CacheWriteTokens == 0 {
		return
	}
	if err := h.DB.RecordBrainUsage(database.BrainUsageRecord{
		Username:         id.actor,
		Subject:          id.subject,
		Role:             id.role,
		CostUSD:          costPtr(*u, rt.ModelName, prices),
		Provider:         providerLabel(rt),
		Model:            rt.ModelName,
		Purpose:          purpose,
		InputTokens:      u.InputTokens,
		OutputTokens:     u.OutputTokens,
		CacheReadTokens:  u.CacheReadTokens,
		CacheWriteTokens: u.CacheWriteTokens,
	}); err != nil {
		slog.Warn("Failed to record quick Brain usage", "error", err)
	}
}

// messageUsage is the stored usage and cost of one message for the UI, or
// nil.
func messageUsage(m database.BrainMessage) map[string]any {
	if m.Role != "assistant" || m.Model == nil {
		return nil
	}
	provider := ""
	if m.Provider != nil {
		provider = *m.Provider
	}
	return usagePayload(provider, *m.Model, braincore.ChatResult{
		InputTokens:      m.InputTokens,
		OutputTokens:     m.OutputTokens,
		CacheReadTokens:  m.CacheReadTokens,
		CacheWriteTokens: m.CacheWriteTokens,
	}, m.CostUSD)
}

// marshalPrices is the stored form of a price table.
func marshalPrices(t braincore.PriceTable) (string, error) {
	if t == nil {
		t = braincore.PriceTable{}
	}
	b, err := json.Marshal(t)
	return string(b), err
}
