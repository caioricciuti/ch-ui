// SPDX-License-Identifier: BUSL-1.1
import { apiGet, apiPost, apiPut } from './client'

export type PerformanceRange = '1h' | '6h' | '24h' | '7d'
export interface Window { start: string; end: string }
export interface Metrics {
  runs: number; failures: number; mean_ms: number; p95_ms: number
  mean_memory_bytes: number; mean_read_bytes: number; mean_cpu_ms: number
}
export interface Change { metric: keyof Metrics; before: number; after: number; percent: number | null; regressed: boolean }
export interface Pattern {
  hash: string; database: string; sample_query: string; baseline: Metrics; current: Metrics
  changes: Change[]; status: 'stable' | 'regressed' | 'insufficient_data'; extra_duration_ms: number
}
export interface PerformanceReport {
  range: PerformanceRange; baseline: Window; current: Window; cluster: string; node: string; supported: boolean
  coverage: string; truncated: boolean; min_samples: number; compared: number; insufficient: number
  regressions: Pattern[]; patterns: Pattern[]
}
export interface Snapshot { range: PerformanceRange; window: Window; cluster: string; node: string; metrics: Metrics; hash: string; database: string; sample_query: string }
export interface Comparison { baseline: Snapshot; current: Snapshot; changes: Change[]; sufficient: boolean }
export interface Investigation {
  id: string; connection_id: string; title: string; query_hash: string; database: string; sample_query: string
  owner: string; status: 'open' | 'monitoring' | 'resolved'; baseline: Snapshot
  created_by: string; created_at: string; updated_at: string; revision: number
}
export interface InvestigationEvent {
  id: string; investigation_id: string; kind: 'baseline' | 'update' | 'comparison'
  actor: string; note: string; payload: Snapshot | Comparison | { title: string; owner: string; status: string }; created_at: string
}
export interface InvestigationDetail { investigation: Investigation; events: InvestigationEvent[] }
export interface InvestigationInput {
  title: string; owner: string; note: string; status?: Investigation['status']; revision?: number
  hash?: string; database?: string; range?: PerformanceRange; cluster?: string
}
export interface PerformanceMonitor {
  connection_id: string; enabled: boolean; last_scan_at: string; last_error: string
  report: PerformanceReport | null; report_at: string
}
export const fetchRegressions = (range: PerformanceRange) => apiGet<PerformanceReport>(`/api/performance/regressions?range=${range}`)
export const fetchInvestigations = () => apiGet<{ investigations: Investigation[] }>('/api/performance/investigations')
export const fetchInvestigation = (id: string) => apiGet<InvestigationDetail>(`/api/performance/investigations/${encodeURIComponent(id)}`)
export const createInvestigation = (data: InvestigationInput) => apiPost<InvestigationDetail>('/api/performance/investigations', data)
export const updateInvestigation = (id: string, data: InvestigationInput) => apiPut<InvestigationDetail>(`/api/performance/investigations/${encodeURIComponent(id)}`, data)
export const compareInvestigation = (id: string, target?: {hash: string; database: string}) => apiPost<InvestigationDetail>(`/api/performance/investigations/${encodeURIComponent(id)}/compare`, target)
export const fetchPerformanceMonitor = () => apiGet<PerformanceMonitor>('/api/performance/monitor')
export const setPerformanceMonitor = (enabled: boolean) => apiPut<PerformanceMonitor>('/api/performance/monitor', { enabled })
