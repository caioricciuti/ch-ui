import { describe, expect, it } from 'vitest'
import { formatCost, formatTokens, usageParts } from './brain-usage'

describe('formatTokens', () => {
  it('compacts counts', () => {
    expect(formatTokens(950)).toBe('950')
    expect(formatTokens(1200)).toBe('1.2k')
    expect(formatTokens(2000)).toBe('2k')
    expect(formatTokens(34_400)).toBe('34k')
    expect(formatTokens(1_500_000)).toBe('1.5M')
  })
})

describe('formatCost', () => {
  it('keeps precision for small costs', () => {
    expect(formatCost(0.00421)).toBe('$0.0042')
    expect(formatCost(1.266)).toBe('$1.27')
    expect(formatCost(1240.4)).toBe('$1,240')
    expect(formatCost(0)).toBe('$0')
  })
})

describe('usageParts', () => {
  it('lists model, tokens, cache and cost', () => {
    expect(
      usageParts({ provider: 'Claude', model: 'claude-sonnet-5-5', input_tokens: 100, output_tokens: 50, cache_read_tokens: 2000, cache_write_tokens: 900, cost_usd: 0.002 }),
    ).toEqual(['claude-sonnet-5-5', '1k in', '50 out', '2k cached', '$0.0020'])
  })
  it('leaves cost out when unpriced', () => {
    expect(usageParts({ provider: 'x', model: 'm', input_tokens: 1, output_tokens: 2, cache_read_tokens: 0, cache_write_tokens: 0 })).toEqual(['m', '1 in', '2 out'])
  })
})
