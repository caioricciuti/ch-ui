// SPDX-License-Identifier: BUSL-1.1
import { apiGet, apiPost, apiPut } from './client'

// ── Types ────────────────────────────────────────────────────────────────────

export interface NodeSample {
  node: string
  captured_at: string
  replication_max_delay: number
  replication_queue_total: number
  replicas_readonly: number
  merges_running: number
  mutations_pending: number
  parts_max_active: number
  parts_pressure_pct: number
  long_queries: number
  /** Live only: age of the oldest unfinished mutation. */
  mutations_oldest_age_seconds?: number
  /** Live only: fullest disk on the node, absent when unknown. */
  disk_used_pct?: number
  /** Metric groups whose collection failed; their numbers read 0 but are unknown. */
  unknown?: MetricGroup[]
  /** Listed in system.clusters but returned no data. */
  down?: boolean
}

export type MetricGroup = 'replication' | 'merges' | 'mutations' | 'parts' | 'long_queries' | 'disk'

export interface PartsLimits {
  parts_to_throw_insert: number
  parts_to_delay_insert: number
}

export interface HealthSummary {
  cluster: string
  is_cluster: boolean
  degraded: boolean
  captured_at: string
  threshold_seconds: number
  parts_limits: PartsLimits
  nodes: NodeSample[]
  /** Error per failed metric group (plus "nodes" when the fan-out failed). */
  errors?: Record<string, string>
}

export interface LiveResult {
  cluster: string
  is_cluster: boolean
  supported: boolean
  degraded?: boolean
  threshold_seconds?: number
  data: Record<string, unknown>[]
}

export interface HistoryResult {
  since: string
  data: NodeSample[]
}

export interface ClusterHealthSettings {
  connection_id: string
  enabled: boolean
  retention_days: number
  poll_interval_seconds: number
  long_query_threshold_seconds: number
  updated_at: string
}

// Detail-list endpoints, keyed by the section they drive.
export type LiveSection =
  | 'replication'
  | 'replication-queue'
  | 'merges'
  | 'mutations'
  | 'parts'
  | 'disks'
  | 'keeper'
  | 'backups'
  | 'long-queries'

// ── API ──────────────────────────────────────────────────────────────────────

const base = '/api/cluster-health'

function clusterParam(cluster?: string): string {
  return cluster ? `?cluster=${encodeURIComponent(cluster)}` : ''
}

export function fetchSummary(cluster?: string): Promise<HealthSummary> {
  return apiGet<HealthSummary>(`${base}/summary${clusterParam(cluster)}`)
}

export function fetchLive(section: LiveSection, cluster?: string): Promise<LiveResult> {
  return apiGet<LiveResult>(`${base}/${section}${clusterParam(cluster)}`)
}

export function fetchHistory(range = '6h'): Promise<HistoryResult> {
  return apiGet<HistoryResult>(`${base}/history?range=${encodeURIComponent(range)}`)
}

export function fetchSettings(): Promise<ClusterHealthSettings> {
  return apiGet<ClusterHealthSettings>(`${base}/settings`)
}

export function saveSettings(settings: Partial<ClusterHealthSettings>): Promise<ClusterHealthSettings> {
  return apiPut<ClusterHealthSettings>(`${base}/settings`, settings)
}

export interface KillResult {
  success: boolean
  /** cluster: sent ON CLUSTER; local: the connected node only. */
  scope: 'cluster' | 'local'
}

export function killQuery(queryId: string): Promise<KillResult> {
  return apiPost<KillResult>(`${base}/kill-query`, { query_id: queryId })
}

export function killMutation(database: string, table: string, mutationId: string): Promise<KillResult> {
  return apiPost<KillResult>(`${base}/kill-mutation`, { database, table, mutation_id: mutationId })
}
