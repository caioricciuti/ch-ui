// SPDX-License-Identifier: BUSL-1.1
import { apiGet, apiPost } from './client'
import type { RbacApplyResponse, RbacEntityKind, RbacEntityText, RbacExplainQuery, RbacExplanation, RbacOp, RbacPlan, RbacSnapshot, RbacWhoCanResult } from '../types/rbac'

const BASE = '/api/rbac'

export function fetchRbacSnapshot(): Promise<RbacSnapshot> {
  return apiGet<{ snapshot: RbacSnapshot }>(`${BASE}/snapshot`).then((r) => r.snapshot)
}

export function fetchRbacEntity(kind: RbacEntityKind, name: string): Promise<RbacEntityText> {
  return apiGet<RbacEntityText>(`${BASE}/entity/${kind}/${encodeURIComponent(name)}`)
}

export function planRbac(ops: RbacOp[], cluster: string): Promise<RbacPlan> {
  return apiPost<{ plan: RbacPlan }>(`${BASE}/plan`, { ops, cluster }).then((r) => r.plan)
}

export function applyRbac(ops: RbacOp[], cluster: string, planHash: string, acknowledged: string[]): Promise<RbacApplyResponse> {
  return apiPost<RbacApplyResponse>(`${BASE}/apply`, { ops, cluster, plan_hash: planHash, acknowledged })
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
