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

export interface TeamRule {
  name: string
  /** Exact ClickHouse user names, or prefixes ending with '*' (e.g. "etl_*"). */
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
