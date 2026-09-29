// SPDX-License-Identifier: BUSL-1.1
import { apiGet, apiPost, apiPut, apiDel } from './client'
import type {
  TraceSearchRequest, TraceSearchResponse, TraceHistogramResponse, TraceDetail, TraceFacets,
  MetricCatalogEntry, MetricQueryRequest, MetricQueryResponse, MetricAttributes, MetricType,
  Monitor, MonitorInput, ServiceMap,
} from '../types/telemetryPro'

const BASE = '/api/telemetry'

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

// ── Monitors (phase 4) ───────────────────────────────────────

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
