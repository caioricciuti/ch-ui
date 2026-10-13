// SPDX-License-Identifier: BUSL-1.1
/** Pure helpers for Governance > Effective access. */
import type { RbacEffect, RbacExplainQuery, RbacVerdict } from '../../../types/rbac'

export type Tone = 'neutral' | 'success' | 'warning' | 'danger' | 'info'

export const EXPLAIN_SECTION = 'effective-access'

const VERDICT_TONES: Record<RbacVerdict['status'], Tone> = {
  allowed: 'success',
  partial: 'warning',
  conditional: 'warning',
  blocked: 'danger',
  denied: 'danger',
  unknown: 'neutral',
}

const VERDICT_LABELS: Record<RbacVerdict['status'], string> = {
  allowed: 'Allowed',
  partial: 'Partly',
  conditional: 'Depends',
  blocked: 'Blocked',
  denied: 'No access',
  unknown: 'Unknown',
}

export function verdictTone(status: RbacVerdict['status']): Tone {
  return VERDICT_TONES[status] ?? 'neutral'
}

export function verdictLabel(status: RbacVerdict['status']): string {
  return VERDICT_LABELS[status] ?? status
}

const EFFECTS: Record<RbacEffect, { label: string; tone: Tone }> = {
  allows: { label: 'Allows', tone: 'success' },
  removes: { label: 'Removes', tone: 'warning' },
  filters: { label: 'Filters rows', tone: 'info' },
  limits: { label: 'Limits', tone: 'neutral' },
  blocks: { label: 'Blocks', tone: 'danger' },
}

export function effectBadge(effect: RbacEffect): { label: string; tone: Tone } {
  return EFFECTS[effect] ?? { label: effect, tone: 'neutral' }
}

/** Reads the explainer question from a URL query string. */
export function queryFromSearch(search: string): RbacExplainQuery {
  const p = new URLSearchParams(search)
  return {
    user: p.get('user') ?? '',
    database: p.get('database') ?? '',
    table: p.get('table') ?? '',
    column: p.get('column') ?? '',
    privilege: p.get('privilege') || 'SELECT',
  }
}

/**
 * Writes the question into a URL query string, keeping unrelated params
 * (section). Empty fields and the default privilege are dropped.
 */
export function searchWithQuery(search: string, q: RbacExplainQuery): string {
  const p = new URLSearchParams(search)
  const set = (key: string, value: string, skip = '') => {
    if (value && value !== skip) p.set(key, value)
    else p.delete(key)
  }
  set('user', q.user)
  set('database', q.database)
  set('table', q.database ? q.table : '')
  set('column', q.database && q.table ? q.column : '')
  set('privilege', q.privilege, 'SELECT')
  return p.toString()
}

/** The Governance path that explains one user's access. */
export function explainPath(user: string): string {
  const p = new URLSearchParams({ section: EXPLAIN_SECTION, user })
  return `/governance?${p.toString()}`
}

/** Human object name for the toolbar question. */
export function objectName(q: Pick<RbacExplainQuery, 'database' | 'table' | 'column'>): string {
  if (!q.database) return '*.*'
  if (!q.table) return `${q.database}.*`
  return q.column ? `${q.database}.${q.table}(${q.column})` : `${q.database}.${q.table}`
}
