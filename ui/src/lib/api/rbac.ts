// SPDX-License-Identifier: BUSL-1.1
import { apiGet, apiPost } from './client'
import type { RbacApplyResponse, RbacEntityKind, RbacEntityText, RbacExplainQuery, RbacExplanation, RbacHistoryDiff, RbacHistoryPage, RbacHistoryRow, RbacOp, RbacPlan, RbacRevertPlan, RbacSnapshot, RbacWhoCanResult } from '../types/rbac'

const BASE = '/api/rbac'

export function fetchRbacSnapshot(): Promise<RbacSnapshot> {
  return apiGet<{ snapshot: RbacSnapshot }>(`${BASE}/snapshot`).then((r) => r.snapshot)
}

export function fetchRbacEntity(kind: RbacEntityKind, name: string): Promise<RbacEntityText> {
  return apiGet<RbacEntityText>(`${BASE}/entity/${kind}/${encodeURIComponent(name)}`)
}

/** revertOf names the access history row a revert goes back to. */
export function planRbac(ops: RbacOp[], cluster: string, revertOf = ''): Promise<RbacPlan> {
  return apiPost<{ plan: RbacPlan }>(`${BASE}/plan`, { ops, cluster, ...(revertOf ? { revert_of: revertOf } : {}) }).then((r) => r.plan)
}

export function applyRbac(ops: RbacOp[], cluster: string, planHash: string, acknowledged: string[], revertOf = ''): Promise<RbacApplyResponse> {
  return apiPost<RbacApplyResponse>(`${BASE}/apply`, { ops, cluster, plan_hash: planHash, acknowledged, ...(revertOf ? { revert_of: revertOf } : {}) })
}

export function fetchClusterNames(): Promise<string[]> {
  return apiGet<{ clusters: string[] }>('/api/query/clusters').then((r) => r.clusters ?? [])
}

function explainParams(q: Partial<RbacExplainQuery>): string {
  const p = new URLSearchParams()
  for (const key of ['user', 'database', 'table', 'column', 'privilege'] as const) {
    const v = q[key]
    if (v) p.set(key, v)
  }
  return p.toString()
}

/** What one user can do on an object, and why. */
export function fetchEffectiveAccess(q: Partial<RbacExplainQuery>): Promise<RbacExplanation> {
  return apiGet<RbacExplanation>(`${BASE}/effective?${explainParams(q)}`)
}

/** Every user that can use a privilege on an object. */
export function fetchWhoCan(q: Partial<RbacExplainQuery>): Promise<RbacWhoCanResult> {
  return apiGet<RbacWhoCanResult>(`${BASE}/who-can?${explainParams({ ...q, user: '' })}`)
}

// ── Access history ───────────────────────────────────────────────────

export function fetchRbacHistory(opts: { driftOnly?: boolean; limit?: number; cursor?: string } = {}): Promise<RbacHistoryPage> {
  const p = new URLSearchParams()
  if (opts.driftOnly) p.set('drift', 'only')
  if (opts.limit) p.set('limit', String(opts.limit))
  if (opts.cursor) p.set('cursor', opts.cursor)
  const qs = p.toString()
  return apiGet<RbacHistoryPage>(`${BASE}/history${qs ? `?${qs}` : ''}`)
}

/** against: 'previous', 'live' or another snapshot id. */
export function fetchRbacHistoryDiff(id: string, against: string): Promise<RbacHistoryDiff> {
  return apiGet<RbacHistoryDiff>(`${BASE}/history/${encodeURIComponent(id)}/diff?against=${encodeURIComponent(against)}`)
}

export function takeRbacSnapshot(): Promise<{ snapshot: RbacHistoryRow; created: boolean }> {
  return apiPost<{ snapshot: RbacHistoryRow; created: boolean }>(`${BASE}/history/snapshot`, {})
}

/** only limits the revert to these entity names. */
export function planRbacRevert(id: string, only: string[] = []): Promise<RbacRevertPlan> {
  return apiPost<RbacRevertPlan>(`${BASE}/history/${encodeURIComponent(id)}/revert-plan`, only.length ? { only } : {})
}
