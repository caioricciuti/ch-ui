import { apiGet, apiPut } from './client'

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
}

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
