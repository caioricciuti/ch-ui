/** Telemetry sources and log search, mirroring /api/telemetry (phase 1). */

export type SourceKind = 'logs' | 'traces' | 'metrics'

export interface LogsMapping {
  timestamp: string
  body: string
  severity: string
  severity_number: string
  service: string
  trace_id: string
  span_id: string
  resource_attributes: string
  scope_attributes: string
  log_attributes: string
  scope_name: string
  event_name: string
}

export interface TracesMapping {
  timestamp: string
  trace_id: string
  span_id: string
  parent_span_id: string
  span_name: string
  span_kind: string
  service: string
  duration: string
  duration_unit: 'ns' | 'us' | 'ms' | 's'
  status_code: string
  status_message: string
  resource_attributes: string
  span_attributes: string
  events: string
  links: string
}

export interface MetricsTables {
  gauge: string
  sum: string
  histogram: string
  exponential_histogram: string
  summary: string
}

export interface TelemetrySource {
  id: string
  connection_id: string
  kind: SourceKind
  name: string
  database: string
  table: string
  tables?: MetricsTables
  logs?: LogsMapping
  traces?: TracesMapping
  correlated_logs?: string
  correlated_traces?: string
  enabled: boolean
  created_by: string
  created_at: string
  updated_at: string
}

/** What the client sends to create or update a source. */
export type SourceInput = Omit<TelemetrySource, 'id' | 'connection_id' | 'created_by' | 'created_at' | 'updated_at'>

export interface SourceTestResult {
  ok: boolean
  columns: Record<string, string>
  missing: string[]
  row_count_1h: number
  latest: string | null
  error?: string
}

export const DEFAULT_LOGS_MAPPING: LogsMapping = {
  timestamp: 'Timestamp',
  body: 'Body',
  severity: 'SeverityText',
  severity_number: 'SeverityNumber',
  service: 'ServiceName',
  trace_id: 'TraceId',
  span_id: 'SpanId',
  resource_attributes: 'ResourceAttributes',
  scope_attributes: 'ScopeAttributes',
  log_attributes: 'LogAttributes',
  scope_name: 'ScopeName',
  event_name: 'EventName',
}

export const DEFAULT_TRACES_MAPPING: TracesMapping = {
  timestamp: 'Timestamp',
  trace_id: 'TraceId',
  span_id: 'SpanId',
  parent_span_id: 'ParentSpanId',
  span_name: 'SpanName',
  span_kind: 'SpanKind',
  service: 'ServiceName',
  duration: 'Duration',
  duration_unit: 'ns',
  status_code: 'StatusCode',
  status_message: 'StatusMessage',
  resource_attributes: 'ResourceAttributes',
  span_attributes: 'SpanAttributes',
  events: 'Events',
  links: 'Links',
}

export const DEFAULT_METRICS_TABLES: MetricsTables = {
  gauge: 'otel_metrics_gauge',
  sum: 'otel_metrics_sum',
  histogram: 'otel_metrics_histogram',
  exponential_histogram: 'otel_metrics_exponential_histogram',
  summary: 'otel_metrics_summary',
}

// ── Logs ─────────────────────────────────────────────────────

export type SeverityLevel = 'TRACE' | 'DEBUG' | 'INFO' | 'WARN' | 'ERROR' | 'FATAL'

export const SEVERITY_LEVELS: SeverityLevel[] = ['TRACE', 'DEBUG', 'INFO', 'WARN', 'ERROR', 'FATAL']

export interface LogRow {
  timestamp: string
  timestamp_ns: string
  severity: string
  severity_number: number
  service: string
  body: string
  trace_id: string
  span_id: string
  resource: Record<string, string>
  scope: Record<string, string>
  attributes: Record<string, string>
  scope_name: string
  event_name: string
}

export interface LogQuery {
  source_id: string
  from: string
  to: string
  q?: string
  severity?: string[]
  services?: string[]
}

export interface LogSearchRequest extends LogQuery {
  limit?: number
  order?: 'asc' | 'desc'
  cursor?: string | null
}

export interface LogSearchResponse {
  rows: LogRow[]
  next_cursor: string | null
  took_ms: number
}

export interface HistogramBucket {
  t: string
  counts: Record<string, number>
}

export interface LogHistogramResponse {
  bucket_seconds: number
  buckets: HistogramBucket[]
}

export interface FacetValue {
  value: string
  count: number
}

export interface FacetKey {
  key: string
  count: number
  source: 'attributes' | 'resource'
}

export interface LogFacets {
  severity: FacetValue[]
  services: FacetValue[]
  attribute_keys: FacetKey[]
  attributes: Record<string, FacetValue[]>
}

export interface LogContextResponse {
  rows: LogRow[]
  anchor_index: number
}

// ── Phase 4: saved searches ──────────────────────────────────

export type SearchKind = 'logs' | 'traces'

export interface SavedSearch {
  id: string
  connection_id: string
  kind: SearchKind
  name: string
  query: string
  range_preset: string
  source_id: string | null
  created_by: string | null
  created_at: string
  updated_at: string
}

export interface SavedSearchInput {
  kind: SearchKind
  name: string
  query: string
  range_preset: string
  source_id?: string | null
}
