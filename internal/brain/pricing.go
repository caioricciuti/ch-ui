package brain

import "github.com/caioricciuti/ch-ui/internal/brain/pricing"

// The price table lives in the pricing package so the database migrations
// can use it without importing brain; these names keep brain's API.
type (
	ModelPrice = pricing.ModelPrice
	PriceTable = pricing.PriceTable
)

// DefaultPrices are the built-in Claude list prices.
func DefaultPrices() PriceTable { return pricing.DefaultPrices() }

// ParsePriceTable reads the stored JSON; empty or invalid input gives nil.
func ParsePriceTable(raw string) PriceTable { return pricing.Parse(raw) }

// PriceUsage is the part of a result a cost is computed from.
func (r ChatResult) PriceUsage() pricing.Usage {
	return pricing.Usage{
		InputTokens:      r.InputTokens,
		OutputTokens:     r.OutputTokens,
		CacheReadTokens:  r.CacheReadTokens,
		CacheWriteTokens: r.CacheWriteTokens,
	}
}
