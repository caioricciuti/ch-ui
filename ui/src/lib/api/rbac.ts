// SPDX-License-Identifier: BUSL-1.1
import { ApiError, apiGet, apiPost } from './client'
import { withBase } from '../basePath'
import { connectionHeaders } from '../stores/workspace-scope'
import type {
  RbacApplyResponse, RbacEntityKind, RbacEntityText, RbacExplainQuery, RbacExplanation, RbacExportFormat, RbacImportRequest,
  RbacOp, RbacPlan, RbacSnapshot, RbacWhoCanResult,
} from '../types/rbac'

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

/** The access state of the connection as a file to download. */
export async function exportRbac(format: RbacExportFormat): Promise<{ blob: Blob; filename: string }> {
  const res = await fetch(withBase(`${BASE}/export?format=${format}`), {
    credentials: 'include',
    headers: { ...connectionHeaders() },
  })
  if (!res.ok) {
    let message = `Export failed (${res.status})`
    try {
      const body: unknown = await res.json()
      if (typeof body === 'object' && body !== null && 'error' in body) message = String((body as { error: unknown }).error)
    } catch {
      // not JSON
    }
    throw new ApiError(message, res.status)
  }
  const disposition = res.headers.get('content-disposition') ?? ''
  const match = /filename="([^"]+)"/.exec(disposition)
  return { blob: await res.blob(), filename: match?.[1] ?? `chui-access.${format}` }
}

/**
 * The ops an access file would apply. Changes nothing. The body is checked
 * by importPlanFrom before its ops are staged.
 */
export function planRbacImport(req: RbacImportRequest): Promise<unknown> {
  return apiPost<unknown>(`${BASE}/import/plan`, req)
}
