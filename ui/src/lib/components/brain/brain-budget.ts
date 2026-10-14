import type {
  BrainBudgetPeriod,
  BrainBudgetRule,
  BrainBudgetScope,
  BrainBudgetStatus,
  BrainModelPrice,
} from '../../types/brain'
import type { BrainBudgetInput } from '../../api/brain'
import { formatCost } from './brain-usage'

/** "Nov 1" for a reset time, in UTC. */
function utcDay(iso: string): string {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return ''
  return d.toLocaleDateString('en-US', { month: 'short', day: 'numeric', timeZone: 'UTC' })
}

/** When a budget period starts over, as a sentence. */
export function resetText(period: BrainBudgetPeriod, resetAt: string): string {
  if (period === 'month') {
    const day = utcDay(resetAt)
    return day ? `It resets on ${day} at 00:00 UTC.` : 'It resets at the start of next month (UTC).'
  }
  return 'It resets at 00:00 UTC.'
}

/** What was used of the limits a rule has: "$4.20 and 81,000 tokens". */
export function usedText(b: Pick<BrainBudgetStatus, 'usd_limit' | 'token_limit' | 'used_usd' | 'used_tokens'>): string {
  const parts: string[] = []
  if (b.usd_limit !== null) parts.push(formatCost(b.used_usd))
  if (b.token_limit !== null) parts.push(`${b.used_tokens.toLocaleString('en-US')} tokens`)
  return parts.join(' and ')
}

/**
 * The quiet warning shown under an answer once a budget is at 80% or more:
 * "84% of your role's daily Brain budget of $5.00 used ($4.20). It resets at 00:00 UTC."
 */
export function budgetWarningText(b: BrainBudgetStatus): string {
  const share = b.exceeded ? 'All' : `${Math.min(99, Math.floor(b.percent))}%`
  return `${share} of ${b.rule} used (${usedText(b)}). ${resetText(b.period, b.reset_at)}`
}

/** The budget closest to its limit once it reaches 80%, as the server picks it for a done event. */
export function nearestBudget(list: BrainBudgetStatus[]): BrainBudgetStatus | null {
  let top: BrainBudgetStatus | null = null
  for (const b of list) {
    if (b.percent < 80) continue
    if (!top || b.percent > top.percent) top = b
  }
  return top
}

// ── Rule editor ──────────────────────────────────────────────

/** The rule editor's fields; limits stay text while typed. */
export interface BudgetRuleDraft {
  scope: BrainBudgetScope
  subject: string
  period: BrainBudgetPeriod
  tokenLimit: string
  usdLimit: string
  blockUnpriced: boolean
}

export const ROLES = ['admin', 'analyst', 'viewer'] as const

export function emptyRuleDraft(pro: boolean): BudgetRuleDraft {
  return { scope: pro ? 'role' : 'global', subject: pro ? 'analyst' : '', period: 'day', tokenLimit: '', usdLimit: '', blockUnpriced: false }
}

export function draftFromRule(r: BrainBudgetRule): BudgetRuleDraft {
  return {
    scope: r.scope,
    subject: r.subject,
    period: r.period,
    tokenLimit: r.token_limit === null ? '' : String(r.token_limit),
    usdLimit: r.usd_limit === null ? '' : String(r.usd_limit),
    blockUnpriced: r.block_unpriced,
  }
}

export type DraftResult = { ok: true; input: BrainBudgetInput } | { ok: false; error: string }

/**
 * Checks a draft the way the server does and builds the request body. An
 * empty limit is no limit; a person or role rule may have none, which
 * exempts them from broader rules. Without Pro only global token limits
 * can be saved.
 */
export function parseRuleDraft(d: BudgetRuleDraft, pro: boolean): DraftResult {
  const subject = d.scope === 'global' ? '' : d.subject.trim()
  if (d.scope === 'role' && !(ROLES as readonly string[]).includes(subject)) return { ok: false, error: 'Pick a role.' }
  if (d.scope === 'user' && !subject) return { ok: false, error: 'Pick a person.' }

  const tokenText = String(d.tokenLimit ?? '').trim()
  const usdText = String(d.usdLimit ?? '').trim()
  let token_limit: number | null = null
  let usd_limit: number | null = null
  if (tokenText) {
    const n = Number(tokenText)
    if (!Number.isInteger(n) || n <= 0) return { ok: false, error: 'The token limit must be a whole number above 0, or empty for none.' }
    token_limit = n
  }
  if (usdText) {
    const n = Number(usdText)
    if (!Number.isFinite(n) || n <= 0) return { ok: false, error: 'The dollar limit must be above 0, or empty for none.' }
    usd_limit = n
  }
  if (d.scope === 'global' && token_limit === null && usd_limit === null) {
    return { ok: false, error: 'A global budget needs a token or a dollar limit.' }
  }
  const block_unpriced = usd_limit !== null && d.blockUnpriced
  if (!pro && (d.scope !== 'global' || usd_limit !== null)) {
    return { ok: false, error: 'Budgets per role or person and dollar limits need a Pro license.' }
  }
  return { ok: true, input: { scope: d.scope, subject, period: d.period, token_limit, usd_limit, block_unpriced } }
}

/** "$5.00/day", "100,000 tokens/month", "$5.00 and 100,000 tokens/day", "No limit". */
export function limitsText(r: Pick<BrainBudgetRule, 'token_limit' | 'usd_limit' | 'period'>): string {
  const parts: string[] = []
  if (r.usd_limit !== null) parts.push(`$${r.usd_limit.toFixed(2)}`)
  if (r.token_limit !== null) parts.push(`${r.token_limit.toLocaleString('en-US')} tokens`)
  if (parts.length === 0) return 'No limit'
  return `${parts.join(' and ')}/${r.period}`
}

/** The person a user key names: "sso:ana@x.io" reads as "ana@x.io". */
export function subjectLabel(scope: BrainBudgetScope, subject: string): string {
  if (scope === 'global') return 'Everyone'
  if (scope === 'user' && subject.startsWith('sso:')) return subject.slice(4)
  return subject
}

// ── Prices ───────────────────────────────────────────────────

/** Finds a model's price like the server: exact id, then without a provider prefix. */
export function lookupPrice(prices: Record<string, BrainModelPrice> | null, model: string): BrainModelPrice | undefined {
  if (!prices) return undefined
  const m = model.trim()
  if (prices[m]) return prices[m]
  let short = m
  const slash = short.lastIndexOf('/')
  if (slash >= 0) short = short.slice(slash + 1)
  const claude = short.indexOf('claude-')
  if (claude > 0) short = short.slice(claude)
  return prices[short]
}

/** A model is priced when it has an input and an output price. */
export function isPriced(prices: Record<string, BrainModelPrice> | null, model: string): boolean {
  const p = lookupPrice(prices, model)
  return !!p && p.input !== null && p.output !== null
}
