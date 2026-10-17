// SPDX-License-Identifier: BUSL-1.1
import { apiGet, apiPut } from './client'

export type CostsRange = '24h' | '7d' | '30d'

export type CostsSection = 'summary' | 'trend' | 'users' | 'queries' | 'storage'

export interface CostsResult {
  cluster: string
  is_cluster: boolean
  /** False when system.query_log (or the ProfileEvents map) is unavailable. */
  supported: boolean
  /** True when the cluster-wide query failed and only the local node answered. */
  degraded?: boolean
  range: string
  currency?: string
  data: Record<string, unknown>[]
}

/** A row of the users and trend sections. `user` is the actor of the
 * query_log rows: the person CH-UI's query tag named (SSO email, job owner)
 * when present, the ClickHouse account otherwise. */
export interface CostsActorRow extends Record<string, unknown> {
  user: string
  /** Team the actor resolved to, attached by the handler. */
  team?: string
  /** ClickHouse accounts the actor's queries ran as (users section only). */
  accounts?: string[]
}

export interface TeamRule {
  name: string
  /** Exact names, or prefixes ending with '*' (e.g. "etl_*"). They match the
   * actor: an SSO email or job owner for queries CH-UI ran, the ClickHouse
   * user otherwise. */
  users: string[]
}

export interface CostsConfig {
  currency: string
  cpuPerCoreHour: number
  storageGBMonth: number
  teams: TeamRule[]
}

export interface CostsConfigResult {
  config: CostsConfig
  /** True when nothing was saved yet and the backend answered with defaults. */
  is_default: boolean
}

export function fetchCosts(
  section: CostsSection,
  range: CostsRange,
  cluster?: string,
): Promise<CostsResult> {
  const qs = new URLSearchParams({ range })
  if (cluster) qs.set('cluster', cluster)
  return apiGet<CostsResult>(`/api/costs/${section}?${qs.toString()}`)
}

export function getCostsConfig(): Promise<CostsConfigResult> {
  return apiGet<CostsConfigResult>('/api/costs/config')
}

export function saveCostsConfig(config: CostsConfig): Promise<CostsConfigResult> {
  return apiPut<CostsConfigResult>('/api/costs/config', config)
}
