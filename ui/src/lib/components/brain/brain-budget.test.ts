import { describe, expect, it } from 'vitest'
import type { BrainBudgetStatus } from '../../types/brain'
import { budgetWarningText, nearestBudget, draftFromRule, emptyRuleDraft, isPriced, limitsText, parseRuleDraft, resetText, subjectLabel } from './brain-budget'

const status = (over: Partial<BrainBudgetStatus> = {}): BrainBudgetStatus => ({
  period: 'day',
  scope: 'role',
  subject: 'analyst',
  rule: "your role's daily Brain budget of $5.00",
  token_limit: null,
  usd_limit: 5,
  block_unpriced: false,
  used_tokens: 81000,
  used_usd: 4.2,
  percent: 84,
  exceeded: false,
  reset_at: '2026-10-15T00:00:00Z',
  ...over,
})

describe('budgetWarningText', () => {
  it('names the rule, the share used and the reset', () => {
    expect(budgetWarningText(status())).toBe("84% of your role's daily Brain budget of $5.00 used ($4.20). It resets at 00:00 UTC.")
  })

  it('says all once the limit is reached and never rounds up to 100%', () => {
    expect(budgetWarningText(status({ percent: 99.9 }))).toMatch(/^99% of/)
    expect(budgetWarningText(status({ percent: 100, exceeded: true }))).toMatch(/^All of/)
  })

  it('shows both limits and the monthly reset day', () => {
    const text = budgetWarningText(status({ period: 'month', token_limit: 100000, reset_at: '2026-11-01T00:00:00Z' }))
    expect(text).toContain('($4.20 and 81,000 tokens)')
    expect(text).toContain('It resets on Nov 1 at 00:00 UTC.')
    expect(resetText('month', 'not a date')).toBe('It resets at the start of next month (UTC).')
  })
})

describe('nearestBudget', () => {
  it('picks the fullest budget at 80% or more', () => {
    expect(nearestBudget([status({ percent: 79 })])).toBeNull()
    expect(nearestBudget([status({ percent: 85 }), status({ period: 'month', percent: 92 })])?.period).toBe('month')
  })
})

describe('parseRuleDraft', () => {
  it('builds the request body and treats empty limits as none', () => {
    const r = parseRuleDraft({ scope: 'role', subject: 'analyst', period: 'month', tokenLimit: '', usdLimit: '5', blockUnpriced: true }, true)
    expect(r).toEqual({ ok: true, input: { scope: 'role', subject: 'analyst', period: 'month', token_limit: null, usd_limit: 5, block_unpriced: true } })
  })

  it('allows a person exemption without limits but not a global one', () => {
    expect(parseRuleDraft({ ...emptyRuleDraft(true), scope: 'user', subject: 'sso:ana@x.io' }, true).ok).toBe(true)
    expect(parseRuleDraft({ ...emptyRuleDraft(true), scope: 'global' }, true)).toEqual({ ok: false, error: 'A global budget needs a token or a dollar limit.' })
  })

  it('rejects bad limits and a missing subject', () => {
    expect(parseRuleDraft({ ...emptyRuleDraft(true), tokenLimit: '1.5' }, true).ok).toBe(false)
    expect(parseRuleDraft({ ...emptyRuleDraft(true), usdLimit: '0' }, true).ok).toBe(false)
    expect(parseRuleDraft({ ...emptyRuleDraft(true), scope: 'user', subject: ' ' }, true).ok).toBe(false)
    expect(parseRuleDraft({ ...emptyRuleDraft(true), subject: 'owner' }, true).ok).toBe(false)
  })

  it('drops block unpriced without a dollar limit and the subject of a global rule', () => {
    const r = parseRuleDraft({ scope: 'global', subject: 'x', period: 'day', tokenLimit: '1000', usdLimit: '', blockUnpriced: true }, true)
    expect(r).toEqual({ ok: true, input: { scope: 'global', subject: '', period: 'day', token_limit: 1000, usd_limit: null, block_unpriced: false } })
  })

  it('keeps Pro-only rules out without a license', () => {
    expect(emptyRuleDraft(false).scope).toBe('global')
    expect(parseRuleDraft({ ...emptyRuleDraft(false), tokenLimit: '10' }, false).ok).toBe(true)
    expect(parseRuleDraft({ ...emptyRuleDraft(false), usdLimit: '10' }, false).ok).toBe(false)
    expect(parseRuleDraft({ ...emptyRuleDraft(true), tokenLimit: '10' }, false).ok).toBe(false)
  })

  it('round-trips a stored rule', () => {
    const d = draftFromRule({ id: 'x', scope: 'user', subject: 'bob', period: 'day', token_limit: 500, usd_limit: null, block_unpriced: false, created_at: '', updated_at: '' })
    expect(d).toEqual({ scope: 'user', subject: 'bob', period: 'day', tokenLimit: '500', usdLimit: '', blockUnpriced: false })
  })
})

describe('labels', () => {
  it('describes limits and subjects', () => {
    expect(limitsText({ token_limit: 100000, usd_limit: 5, period: 'day' })).toBe('$5.00 and 100,000 tokens/day')
    expect(limitsText({ token_limit: null, usd_limit: null, period: 'month' })).toBe('No limit')
    expect(subjectLabel('user', 'sso:ana@x.io')).toBe('ana@x.io')
    expect(subjectLabel('global', '')).toBe('Everyone')
  })
})

describe('isPriced', () => {
  it('matches prefixed ids and needs input and output', () => {
    const prices = { 'claude-haiku-5-5': { input: 0.1, output: 0.5, cacheRead: null, cacheWrite: null }, half: { input: 1, output: null, cacheRead: null, cacheWrite: null } }
    expect(isPriced(prices, 'us.anthropic.claude-haiku-5-5')).toBe(true)
    expect(isPriced(prices, 'anthropic/claude-haiku-5-5')).toBe(true)
    expect(isPriced(prices, 'half')).toBe(false)
    expect(isPriced(null, 'x')).toBe(false)
  })
})
