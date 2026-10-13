package brain

import (
	"encoding/json"
	"strings"
)

// ModelPrice is a price in US dollars per million tokens. A nil field means
// no price is set, and then no cost is shown.
type ModelPrice struct {
	Input      *float64 `json:"input"`
	Output     *float64 `json:"output"`
	CacheRead  *float64 `json:"cacheRead"`
	CacheWrite *float64 `json:"cacheWrite"`
}

// PriceTable maps a model id to its price.
type PriceTable map[string]ModelPrice

func usd(v float64) *float64 { return &v }

// DefaultPrices are Anthropic's list prices for the current Claude models
// (first-party API, prompts up to the standard tier; cache writes at the
// 5-minute rate). Admins can change or clear every value.
func DefaultPrices() PriceTable {
	return PriceTable{
		"claude-sonnet-5-5": {Input: usd(2), Output: usd(10), CacheRead: usd(0.20), CacheWrite: usd(2.50)},
		"claude-haiku-5-5":  {Input: usd(0.10), Output: usd(0.50), CacheRead: usd(0.01), CacheWrite: usd(0.125)},
		"claude-opus-5-5":   {Input: usd(4), Output: usd(20), CacheRead: usd(0.20), CacheWrite: usd(5)},
		"claude-fable-5-1":  {Input: usd(10), Output: usd(50), CacheRead: usd(0.25), CacheWrite: usd(12.50)},
	}
}

// ParsePriceTable reads the stored JSON; empty or invalid input gives nil.
func ParsePriceTable(raw string) PriceTable {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var t PriceTable
	if json.Unmarshal([]byte(raw), &t) != nil {
		return nil
	}
	return t
}

// Lookup finds the price for a model id: an exact match first, then the
// model id without a provider prefix ("anthropic/claude-..." or
// "us.anthropic.claude-...").
func (t PriceTable) Lookup(model string) (ModelPrice, bool) {
	if len(t) == 0 {
		return ModelPrice{}, false
	}
	m := strings.TrimSpace(model)
	if p, ok := t[m]; ok {
		return p, true
	}
	short := m
	if i := strings.LastIndex(short, "/"); i >= 0 {
		short = short[i+1:]
	}
	if i := strings.Index(short, "claude-"); i > 0 {
		short = short[i:]
	}
	p, ok := t[short]
	return p, ok
}

// Cost estimates the dollar cost of the given usage. ok is false when the
// model has no input or output price.
func (t PriceTable) Cost(model string, u ChatResult) (float64, bool) {
	p, found := t.Lookup(model)
	if !found || p.Input == nil || p.Output == nil {
		return 0, false
	}
	cost := float64(u.InputTokens)**p.Input + float64(u.OutputTokens)**p.Output
	if p.CacheRead != nil {
		cost += float64(u.CacheReadTokens) * *p.CacheRead
	} else {
		cost += float64(u.CacheReadTokens) * *p.Input
	}
	if p.CacheWrite != nil {
		cost += float64(u.CacheWriteTokens) * *p.CacheWrite
	} else {
		cost += float64(u.CacheWriteTokens) * *p.Input
	}
	return cost / 1e6, true
}
