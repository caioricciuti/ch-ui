// SPDX-License-Identifier: BUSL-1.1
import { apiGet, apiPost } from './client'
import type { RbacApplyResponse, RbacEntityKind, RbacEntityText, RbacOp, RbacPlan, RbacSnapshot } from '../types/rbac'

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
