import { apiGet, apiPost, apiPut, apiDel } from './client'
import type {
  TelemetrySource, SourceInput, SourceTestResult,
  LogSearchRequest, LogSearchResponse, LogQuery, LogHistogramResponse, LogFacets, LogContextResponse, LogRow,
  TraceSearchRequest, TraceSearchResponse, TraceHistogramResponse, TraceDetail, TraceFacets,
  MetricCatalogEntry, MetricQueryRequest, MetricQueryResponse, MetricAttributes, MetricType,
  SavedSearch, SavedSearchInput, Monitor, MonitorInput, ServiceMap,
} from '../types/telemetry'

const BASE = '/api/telemetry'

// ── Sources ──────────────────────────────────────────────────

export async function listSources(): Promise<TelemetrySource[]> {
  const res = await apiGet<{ sources: TelemetrySource[] }>(`${BASE}/sources`)
  return res.sources ?? []
}

export async function createSource(input: SourceInput): Promise<TelemetrySource> {
  const res = await apiPost<{ source: TelemetrySource }>(`${BASE}/sources`, input)
  return res.source
}

export async function updateSource(id: string, input: SourceInput): Promise<TelemetrySource> {
  const res = await apiPut<{ source: TelemetrySource }>(`${BASE}/sources/${id}`, input)
  return res.source
}

export async function deleteSource(id: string): Promise<void> {
  await apiDel(`${BASE}/sources/${id}`)
}

export async function detectSources(): Promise<TelemetrySource[]> {
  const res = await apiPost<{ proposals: TelemetrySource[] }>(`${BASE}/sources/detect`, {})
  return res.proposals ?? []
}

export function testSource(id: string): Promise<SourceTestResult> {
  return apiPost<SourceTestResult>(`${BASE}/sources/${id}/test`, {})
}

// ── Logs ─────────────────────────────────────────────────────

export function searchLogs(req: LogSearchRequest): Promise<LogSearchResponse> {
  return apiPost<LogSearchResponse>(`${BASE}/logs/search`, req)
}

export function logsHistogram(req: LogQuery): Promise<LogHistogramResponse> {
  return apiPost<LogHistogramResponse>(`${BASE}/logs/histogram`, req)
}

export function logsFacets(req: LogQuery & { keys?: string[] }): Promise<LogFacets> {
  return apiPost<LogFacets>(`${BASE}/logs/facets`, req)
}

export function logsContext(req: {
  source_id: string
  timestamp_ns: string
  service: string
  before?: number
  after?: number
}): Promise<LogContextResponse> {
  return apiPost<LogContextResponse>(`${BASE}/logs/context`, req)
}

export async function logsByTrace(sourceId: string, traceId: string): Promise<LogRow[]> {
  const res = await apiGet<{ rows: LogRow[] }>(
    `${BASE}/logs/by-trace/${encodeURIComponent(traceId)}?source_id=${encodeURIComponent(sourceId)}`,
  )
  return res.rows ?? []
}

// ── Traces (phase 2) ─────────────────────────────────────────

export function searchTraces(req: TraceSearchRequest): Promise<TraceSearchResponse> {
  return apiPost<TraceSearchResponse>(`${BASE}/traces/search`, req)
}

export function tracesHistogram(req: { source_id: string; from: string; to: string; q?: string }): Promise<TraceHistogramResponse> {
  return apiPost<TraceHistogramResponse>(`${BASE}/traces/histogram`, req)
}

export function tracesFacets(req: { source_id: string; from: string; to: string; q?: string }): Promise<TraceFacets> {
  return apiPost<TraceFacets>(`${BASE}/traces/facets`, req)
}

export function getTrace(sourceId: string, traceId: string): Promise<TraceDetail> {
  return apiGet<TraceDetail>(`${BASE}/traces/${encodeURIComponent(traceId)}?source_id=${encodeURIComponent(sourceId)}`)
}

// ── Metrics (phase 3) ────────────────────────────────────────

export async function metricsCatalog(sourceId: string, from: string, to: string): Promise<MetricCatalogEntry[]> {
  const qs = new URLSearchParams({ source_id: sourceId, from, to })
  const res = await apiGet<{ metrics: MetricCatalogEntry[] }>(`${BASE}/metrics/catalog?${qs}`)
  return res.metrics ?? []
}

export function metricsQuery(req: MetricQueryRequest): Promise<MetricQueryResponse> {
  return apiPost<MetricQueryResponse>(`${BASE}/metrics/query`, req)
}

export function metricsAttributes(sourceId: string, metric: string, type: MetricType, from: string, to: string): Promise<MetricAttributes> {
  const qs = new URLSearchParams({ source_id: sourceId, metric, type, from, to })
  return apiGet<MetricAttributes>(`${BASE}/metrics/attributes?${qs}`)
}

// ── Saved searches + monitors (phase 4) ──────────────────────

export async function listSavedSearches(): Promise<SavedSearch[]> {
  const res = await apiGet<{ searches: SavedSearch[] }>(`${BASE}/saved-searches`)
  return res.searches ?? []
}

export async function createSavedSearch(input: SavedSearchInput): Promise<SavedSearch> {
  const res = await apiPost<{ search: SavedSearch }>(`${BASE}/saved-searches`, input)
  return res.search
}

export async function updateSavedSearch(id: string, input: SavedSearchInput): Promise<SavedSearch> {
  const res = await apiPut<{ search: SavedSearch }>(`${BASE}/saved-searches/${id}`, input)
  return res.search
}

export async function deleteSavedSearch(id: string): Promise<void> {
  await apiDel(`${BASE}/saved-searches/${id}`)
}

export async function listMonitors(): Promise<Monitor[]> {
  const res = await apiGet<{ monitors: Monitor[] }>(`${BASE}/monitors`)
  return res.monitors ?? []
}

export async function createMonitor(input: MonitorInput): Promise<Monitor> {
  const res = await apiPost<{ monitor: Monitor }>(`${BASE}/monitors`, input)
  return res.monitor
}

export async function updateMonitor(id: string, input: MonitorInput): Promise<Monitor> {
  const res = await apiPut<{ monitor: Monitor }>(`${BASE}/monitors/${id}`, input)
  return res.monitor
}

export async function deleteMonitor(id: string): Promise<void> {
  await apiDel(`${BASE}/monitors/${id}`)
}

export function runMonitor(id: string): Promise<{ value: number; firing: boolean; monitor: Monitor }> {
  return apiPost(`${BASE}/monitors/${id}/run`, {})
}

// ── Service map (phase 4) ────────────────────────────────────

export function serviceMap(req: { source_id: string; from: string; to: string }): Promise<ServiceMap> {
  return apiPost<ServiceMap>(`${BASE}/service-map`, req)
}
