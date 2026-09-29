import { apiGet, apiPost, apiPut, apiDel } from './client'
import type {
  TelemetrySource, SourceInput, SourceTestResult,
  LogSearchRequest, LogSearchResponse, LogQuery, LogHistogramResponse, LogFacets, LogContextResponse, LogRow,
  SavedSearch, SavedSearchInput,
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

// ── Saved searches (phase 4) ─────────────────────────────────

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
