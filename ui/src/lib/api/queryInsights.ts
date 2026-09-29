// SPDX-License-Identifier: BUSL-1.1
import { apiGet } from './client'

export type InsightsRange = '1h' | '6h' | '24h' | '7d' | '30d'

export type InsightsSection =
  | 'summary'
  | 'volume'
  | 'latency'
  | 'slow'
  | 'memory'
  | 'frequent'
  | 'errors'
  | 'users'
  | 'tables'

export interface InsightsResult {
  cluster: string
  is_cluster: boolean
  /** False when system.query_log (or a column the section needs) is
   * unavailable on this deployment. */
  supported: boolean
  /** True when the cluster-wide query failed and only the local node answered. */
  degraded?: boolean
  range: string
  data: Record<string, unknown>[]
}

export interface InsightsFilters {
  /** Exact ClickHouse user. */
  user?: string
  /** query_kind: Select | Insert | Create | Alter | Drop | Show | System | Other. */
  kind?: string
  /** Case-insensitive substring of the query text. */
  search?: string
  /** Only queries touching this table (database.table). */
  table?: string
  /** Only queries at least this slow. */
  minMs?: number
}

export function fetchInsights(
  section: InsightsSection,
  range: InsightsRange,
  cluster?: string,
  filters: InsightsFilters = {},
): Promise<InsightsResult> {
  const qs = new URLSearchParams({ range })
  if (cluster) qs.set('cluster', cluster)
  if (filters.user) qs.set('user', filters.user)
  if (filters.kind) qs.set('kind', filters.kind)
  if (filters.search) qs.set('search', filters.search)
  if (filters.table) qs.set('table', filters.table)
  if (filters.minMs) qs.set('min_ms', String(filters.minMs))
  return apiGet<InsightsResult>(`/api/query-insights/${section}?${qs.toString()}`)
}
