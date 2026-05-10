import { apiGet, apiPost, apiPut } from './client'
import type { TelemetryConfig, LogFilters, LogEntry, LogHistogramBucket } from '../types/telemetry'

interface SchemaResponse {
  tables: Array<{ database: string; name: string; engine: string; total_rows: number }>
  meta: Array<{ name: string; type: string }>
}

interface LogsResponse {
  data: LogEntry[]
  meta: Array<{ name: string; type: string }>
}

interface HistogramResponse {
  data: LogHistogramBucket[]
  meta: Array<{ name: string; type: string }>
}

interface ServicesResponse {
  data: Array<{ ServiceName: string }>
  meta: Array<{ name: string; type: string }>
}

interface ConfigResponse {
  config: { config_json: string } | null
}

export function fetchTelemetrySchema(): Promise<SchemaResponse> {
  return apiGet<SchemaResponse>('/api/telemetry/schema')
}

export function queryLogs(filters: Partial<LogFilters>): Promise<LogsResponse> {
  return apiPost<LogsResponse>('/api/telemetry/logs', filters)
}

export function fetchLogServices(database?: string, table?: string): Promise<ServicesResponse> {
  const params = new URLSearchParams()
  if (database) params.set('database', database)
  if (table) params.set('table', table)
  const qs = params.toString()
  return apiGet<ServicesResponse>(`/api/telemetry/logs/services${qs ? '?' + qs : ''}`)
}

export function fetchLogHistogram(filters: Partial<LogFilters>): Promise<HistogramResponse> {
  return apiPost<HistogramResponse>('/api/telemetry/logs/histogram', filters)
}

export function getTelemetryConfig(): Promise<ConfigResponse> {
  return apiGet<ConfigResponse>('/api/telemetry/config')
}

export function saveTelemetryConfig(config: TelemetryConfig): Promise<void> {
  return apiPut('/api/telemetry/config', config)
}
