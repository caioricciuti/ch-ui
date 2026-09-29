// SPDX-License-Identifier: BUSL-1.1
/** Pro telemetry types: traces, metrics, monitors and the service map. */

import type { FacetValue, LogRow, SearchKind } from './telemetry'

// ── Phase 2: traces ───────────────────────────────────────────

export type SpanStatus = 'Ok' | 'Error' | 'Unset' | string

export interface TraceSummary {
  trace_id: string
  root_span: string
  root_service: string
  start: string
  start_ns: string
  duration_ms: number
  span_count: number
  error_count: number
  services: string[]
  status: SpanStatus
}

export interface TraceSearchRequest {
  source_id: string
  from: string
  to: string
  q?: string
  limit?: number
  cursor?: string | null
}

export interface TraceSearchResponse {
  traces: TraceSummary[]
  next_cursor: string | null
  took_ms: number
}

export interface TraceHistogramBucket {
  t: string
  count: number
  errors: number
  p50_ms: number
  p95_ms: number
}

export interface TraceHistogramResponse {
  bucket_seconds: number
  buckets: TraceHistogramBucket[]
}

export interface SpanEvent {
  time: string
  name: string
  attributes: Record<string, string>
}

export interface SpanLink {
  trace_id: string
  span_id: string
  attributes: Record<string, string>
}

export interface Span {
  span_id: string
  parent_span_id: string
  name: string
  kind: string
  service: string
  start: string
  start_ns: string
  duration_ms: number
  status: SpanStatus
  status_message: string
  attributes: Record<string, string>
  resource: Record<string, string>
  events: SpanEvent[]
  links: SpanLink[]
  depth: number
  order: number
}

export interface TraceServiceSummary {
  name: string
  spans: number
  errors: number
  duration_ms: number
}

export interface TraceDetail {
  trace: {
    trace_id: string
    start: string
    duration_ms: number
    span_count: number
    error_count: number
    services: TraceServiceSummary[]
  }
  spans: Span[]
  logs: LogRow[]
}

export interface TraceFacets {
  services: FacetValue[]
  span_names: FacetValue[]
  status: FacetValue[]
  kinds: FacetValue[]
}

// ── Phase 3: metrics ──────────────────────────────────────────

export type MetricType = 'gauge' | 'sum' | 'histogram' | 'exponential_histogram' | 'summary'
export type MetricAggregation = 'avg' | 'sum' | 'min' | 'max' | 'last' | 'count' | 'rate' | 'p50' | 'p95' | 'p99'

export interface MetricCatalogEntry {
  name: string
  type: MetricType
  unit: string
  description: string
  services: number
  series: number
}

export interface MetricQueryRequest {
  source_id: string
  from: string
  to: string
  metric: string
  type: MetricType
  aggregation: MetricAggregation
  group_by?: string[]
  q?: string
  bucket_seconds?: number
  limit_series?: number
}

export interface MetricSeries {
  labels: Record<string, string>
  name: string
  points: Array<[number, number | null]>
  last: number | null
  min: number | null
  max: number | null
  avg: number | null
}

export interface MetricQueryResponse {
  bucket_seconds: number
  unit: string
  series: MetricSeries[]
  truncated?: boolean
  took_ms: number
}

export interface MetricAttributeKey {
  key: string
  values: FacetValue[]
}

export interface MetricAttributes {
  keys: MetricAttributeKey[]
}

// ── Phase 4: monitors, service map ───────────────────────────

export type MonitorComparator = 'gt' | 'gte' | 'lt' | 'lte'
export type MonitorSeverity = 'info' | 'warn' | 'error' | 'critical'
export type MonitorState = 'ok' | 'firing' | 'error'

export interface Monitor {
  id: string
  connection_id: string
  name: string
  kind: SearchKind
  source_id: string
  query: string
  window_seconds: number
  interval_seconds: number
  comparator: MonitorComparator
  threshold: number
  severity: MonitorSeverity
  enabled: boolean
  last_run_at: string | null
  last_value: number | null
  last_state: MonitorState
  last_error: string | null
  created_by: string | null
  created_at: string
  updated_at: string
}

export interface MonitorInput {
  name: string
  kind: SearchKind
  source_id: string
  query: string
  window_seconds: number
  interval_seconds: number
  comparator: MonitorComparator
  threshold: number
  severity: MonitorSeverity
  enabled: boolean
}

export interface ServiceMapNode {
  service: string
  spans: number
  errors: number
  p50_ms: number
  p95_ms: number
}

export interface ServiceMapEdge {
  from: string
  to: string
  calls: number
  errors: number
  p50_ms: number
  p95_ms: number
}

export interface ServiceMap {
  nodes: ServiceMapNode[]
  edges: ServiceMapEdge[]
}
