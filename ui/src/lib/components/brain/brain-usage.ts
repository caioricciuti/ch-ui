import type { BrainUsage } from '../../types/brain'

/** 950, 1.2k, 34k, 1.5M: compact token counts for footers and tables. */
export function formatTokens(n: number): string {
  if (!Number.isFinite(n) || n < 1000) return String(Math.max(0, Math.round(n || 0)))
  if (n < 10_000) return `${(n / 1000).toFixed(1).replace(/\.0$/, '')}k`
  if (n < 1_000_000) return `${Math.round(n / 1000)}k`
  return `${(n / 1_000_000).toFixed(1).replace(/\.0$/, '')}M`
}

/** $0.0042, $1.27, $1,240: enough precision for small per-message costs. */
export function formatCost(usd: number): string {
  if (!Number.isFinite(usd) || usd <= 0) return '$0'
  if (usd < 0.01) return `$${usd.toFixed(4)}`
  if (usd < 100) return `$${usd.toFixed(2)}`
  return `$${Math.round(usd).toLocaleString('en-US')}`
}

/** The parts of a message footer: model, tokens and, when priced, cost. */
export function usageParts(u: BrainUsage): string[] {
  const parts = [u.model]
  parts.push(`${formatTokens(u.input_tokens + u.cache_write_tokens)} in`)
  parts.push(`${formatTokens(u.output_tokens)} out`)
  if (u.cache_read_tokens > 0) parts.push(`${formatTokens(u.cache_read_tokens)} cached`)
  if (typeof u.cost_usd === 'number') parts.push(formatCost(u.cost_usd))
  return parts
}

/** Full numbers for the footer's hover text. */
export function usageTitle(u: BrainUsage): string {
  const lines = [
    `Provider: ${u.provider || 'unknown'}`,
    `Model: ${u.model}`,
    `Input: ${u.input_tokens.toLocaleString('en-US')} tokens`,
    `Output: ${u.output_tokens.toLocaleString('en-US')} tokens`,
    `Cache read: ${u.cache_read_tokens.toLocaleString('en-US')} tokens`,
    `Cache write: ${u.cache_write_tokens.toLocaleString('en-US')} tokens`,
  ]
  if (typeof u.cost_usd === 'number') lines.push(`Estimated cost: ${formatCost(u.cost_usd)}`)
  return lines.join('\n')
}
