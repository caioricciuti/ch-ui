package handlers

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"strings"
	"time"

	braincore "github.com/caioricciuti/ch-ui/internal/brain"
	"github.com/caioricciuti/ch-ui/internal/database"
)

// Brain settings live in the settings table under these keys.
const (
	settingBrainAgentModel  = "brain.agent_model_id"
	settingBrainQuickModel  = "brain.quick_model_id"
	settingBrainDailyBudget = "brain.daily_token_budget"
	settingBrainMaxOutput   = "brain.max_output_tokens"
	settingBrainPrices      = "brain.prices"
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
	if raw, ok := all[settingBrainPrices]; ok {
		s.Prices = braincore.ParsePriceTable(raw)
		if s.Prices == nil {
			s.Prices = braincore.PriceTable{}
		}
	}
	return s
}

// startOfDayUTC is when today's budget started.
func startOfDayUTC(now time.Time) time.Time {
	y, m, d := now.UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// budgetExceeded returns a message for the person when their daily token
// budget is used up, or "" when they may start a turn.
func budgetExceeded(db *database.DB, username string, s brainSettings) string {
	if s.DailyTokenBudget <= 0 || db == nil {
		return ""
	}
	used, err := db.BrainTokensUsedSince(username, startOfDayUTC(time.Now()))
	if err != nil {
		slog.Warn("Failed to check Brain token budget", "error", err)
		return ""
	}
	if used < s.DailyTokenBudget {
		return ""
	}
	return fmt.Sprintf("You have used your daily Brain budget of %s tokens (%s used today). It resets at 00:00 UTC; an admin can raise it in Admin > Brain.",
		formatTokenCount(s.DailyTokenBudget), formatTokenCount(used))
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

// usagePayload is the usage the UI shows under a message.
func usagePayload(provider, model string, u braincore.ChatResult, prices braincore.PriceTable) map[string]any {
	out := map[string]any{
		"provider":           provider,
		"model":              model,
		"input_tokens":       u.InputTokens,
		"output_tokens":      u.OutputTokens,
		"cache_read_tokens":  u.CacheReadTokens,
		"cache_write_tokens": u.CacheWriteTokens,
	}
	if cost, ok := prices.Cost(model, u); ok {
		out["cost_usd"] = roundCost(cost)
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
	u := *t.usage
	provider := providerLabel(t.runtimeModel)
	model := t.runtimeModel.ModelName
	if err := h.DB.SetBrainMessageUsage(t.assistantMessageID, provider, model, u.InputTokens, u.OutputTokens, u.CacheReadTokens, u.CacheWriteTokens); err != nil {
		slog.Warn("Failed to store Brain message usage", "error", err)
	}
	if u.InputTokens+u.OutputTokens+u.CacheReadTokens+u.CacheWriteTokens == 0 {
		return
	}
	if err := h.DB.RecordBrainUsage(database.BrainUsageRecord{
		Username:         t.username,
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

// recordAskAIUsage stores one Ask AI request in the ledger.
func (h *BrainHandler) recordAskAIUsage(username string, rt *database.BrainModelRuntime, u *braincore.ChatResult) {
	if h.DB == nil || rt == nil || u == nil || u.InputTokens+u.OutputTokens+u.CacheReadTokens+u.CacheWriteTokens == 0 {
		return
	}
	if err := h.DB.RecordBrainUsage(database.BrainUsageRecord{
		Username:         username,
		Provider:         providerLabel(rt),
		Model:            rt.ModelName,
		Purpose:          "ask_ai",
		InputTokens:      u.InputTokens,
		OutputTokens:     u.OutputTokens,
		CacheReadTokens:  u.CacheReadTokens,
		CacheWriteTokens: u.CacheWriteTokens,
	}); err != nil {
		slog.Warn("Failed to record Ask AI usage", "error", err)
	}
}

// messageUsage is the stored usage of one message for the UI, or nil.
func messageUsage(m database.BrainMessage, prices braincore.PriceTable) map[string]any {
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
	}, prices)
}

// marshalPrices is the stored form of a price table.
func marshalPrices(t braincore.PriceTable) (string, error) {
	if t == nil {
		t = braincore.PriceTable{}
	}
	b, err := json.Marshal(t)
	return string(b), err
}
