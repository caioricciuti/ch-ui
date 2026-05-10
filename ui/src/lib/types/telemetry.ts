export type SeverityLevel = 'TRACE' | 'DEBUG' | 'INFO' | 'WARN' | 'ERROR' | 'FATAL'

export interface TelemetryTable {
  database: string
  name: string
  engine: string
  total_rows: number
}

export interface TelemetrySchema {
  hasLogs: boolean
  hasTraces: boolean
  hasMetrics: boolean
  tables: TelemetryTable[]
  logsTable?: string
  logsDatabase?: string
  tracesTable?: string
  tracesDatabase?: string
}

export interface TelemetryConfig {
  logsDatabase: string
  logsTable: string
  tracesDatabase: string
  tracesTable: string
  metricsDatabase: string
  metricsPrefix: string
}

export interface LogEntry {
  Timestamp: string
  TraceId: string
  SpanId: string
  SeverityText: string
  SeverityNumber: number
  ServiceName: string
  Body: string
  ResourceAttributes: Record<string, string>
  LogAttributes: Record<string, string>
}

export interface LogFilters {
  database: string
  table: string
  timeFrom: string
  timeTo: string
  severity: SeverityLevel[]
  services: string[]
  search: string
  limit: number
  offset: number
}

export interface LogHistogramBucket {
  bucket_time: string
  count: number
}

export const SEVERITY_COLORS: Record<SeverityLevel, string> = {
  TRACE: '#6b7280',
  DEBUG: '#06b6d4',
  INFO: '#3b82f6',
  WARN: '#f59e0b',
  ERROR: '#ef4444',
  FATAL: '#d946ef',
}

export const SEVERITY_LEVELS: SeverityLevel[] = ['TRACE', 'DEBUG', 'INFO', 'WARN', 'ERROR', 'FATAL']
