// SPDX-License-Identifier: BUSL-1.1
import type {
  RbacChangeKind, RbacEffectiveChange, RbacHistoryDrift, RbacHistoryRow, RbacHistorySource, RbacOp, RbacStateChange,
} from '../../../types/rbac'

/**
 * Formatting for access history: row summaries, change lines and the
 * hand-off that carries a revert into Access control's review.
 */

type Tone = 'neutral' | 'success' | 'warning' | 'danger' | 'info'

const SOURCE_LABELS: Record<RbacHistorySource, string> = {
  apply: 'Apply', revert: 'Revert', sync: 'Governance sync', view: 'Page view', manual: 'Manual',
}

export function sourceLabel(source: RbacHistorySource): string {
  return SOURCE_LABELS[source] ?? source
}

export function sourceTone(source: RbacHistorySource): Tone {
  return source === 'apply' || source === 'revert' ? 'info' : 'neutral'
}

export function driftLabel(drift: RbacHistoryDrift): string {
  return drift === 'outside' ? 'Outside change' : drift === 'mixed' ? 'Mixed' : 'None'
}

export function driftTone(drift: RbacHistoryDrift): Tone {
  return drift === 'outside' ? 'warning' : drift === 'mixed' ? 'danger' : 'neutral'
}

const KIND_NOUNS: Record<RbacChangeKind, [string, string]> = {
  grant: ['grant', 'grants'],
  role_grant: ['role grant', 'role grants'],
  default_roles: ['default roles change', 'default roles changes'],
  entity: ['entity', 'entities'],
  setting: ['setting change', 'setting changes'],
}

const KIND_ORDER: RbacChangeKind[] = ['entity', 'grant', 'role_grant', 'default_roles', 'setting']

function countPhrase(kind: RbacChangeKind, n: number, verb: string): string {
  const [one, many] = KIND_NOUNS[kind] ?? [kind, kind]
  // "default roles change" and "setting change" already say what happened.
  const suffix = kind === 'default_roles' || kind === 'setting' ? '' : ` ${verb}`
  return `${n} ${n === 1 ? one : many}${suffix}`
}

/** "1 entity added, 2 grants removed", or why there is nothing to count. */
export function summaryText(row: Pick<RbacHistoryRow, 'summary'>): string {
  const s = row.summary ?? {}
  if (s.first) return 'First snapshot'
  const parts: string[] = []
  for (const [verb, counts] of [['added', s.added], ['removed', s.removed], ['changed', s.changed]] as const) {
    if (!counts) continue
    for (const kind of KIND_ORDER) {
      const n = counts[kind]
      if (n) parts.push(countPhrase(kind, n, verb))
    }
  }
  let text = parts.length ? parts.join(', ') : 'No change'
  if (s.host_changed) text += ' (host changed)'
  if (s.incomplete?.length) text += ' (partial read)'
  return text
}

const ENTITY_LABELS: Record<string, string> = {
  role: 'role', user: 'user', row_policy: 'row policy', quota: 'quota', settings_profile: 'settings profile',
}

/** One change in words: "role analyst: GRANT SELECT ON `sales`.* TO `analyst`". */
export function changeLine(c: RbacStateChange): string {
  return `${ENTITY_LABELS[c.entity_kind] ?? c.entity_kind} ${c.grantee}: ${c.detail}`
}

export function changeTone(c: Pick<RbacStateChange, 'change'>): Tone {
  return c.change === 'added' ? 'success' : c.change === 'removed' ? 'danger' : 'info'
}

/** "alice gained SELECT ON `sales`.* via analyst". */
export function effectiveLine(e: RbacEffectiveChange): string {
  const via = e.via === 'direct' ? 'directly' : `via ${e.via}`
  return `${e.user} ${e.change} ${e.what} ${via}`
}

/** "role:analyst" to "role analyst". */
export function entityKeyLabel(key: string): string {
  const i = key.indexOf(':')
  if (i < 0) return key
  return `${ENTITY_LABELS[key.slice(0, i)] ?? key.slice(0, i)} ${key.slice(i + 1)}`
}

// ── Hand-off to Access control ───────────────────────────────────────

export interface StagedRevert {
  ops: RbacOp[]
  /** The history row the revert goes back to. */
  revertOf: string
}

let staged: StagedRevert | null = null

/** Keep a revert for Access control to pick up when it opens. */
export function stageRevert(v: StagedRevert): void {
  staged = v
}

/** Take the staged revert, once. */
export function takeStagedRevert(): StagedRevert | null {
  const v = staged
  staged = null
  return v
}
