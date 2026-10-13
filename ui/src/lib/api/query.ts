import { apiGet, apiPost } from './client'
import type {
  LegacyQueryResult,
  ExplorerDataResponse,
  QueryPlanResult,
  QueryProfileResult,
  QueryEstimateResult,
  SampleQueryResult,
} from '../types/query'
import type { Column } from '../types/schema'

interface RunQueryParams {
  query: string
  timeout?: number
}

function escapeLiteral(value: string): string {
  // Reject null bytes which can truncate strings in some SQL engines
  if (value.includes('\0')) throw new Error('Invalid character in identifier')
  return value.replace(/\\/g, '\\\\').replace(/'/g, "\\'")
}

function escapeIdentifier(value: string): string {
  if (value.includes('\0')) throw new Error('Invalid character in identifier')
  return '`' + value.replace(/`/g, '``') + '`'
}

/** Execute a query (legacy JSON format) */
export function runQuery(params: RunQueryParams): Promise<LegacyQueryResult> {
  return apiPost<LegacyQueryResult>('/api/query/run', params)
}

export interface FormatResult {
  formatted: string
  /** 'clickhouse' when the server's formatQuery() did it, 'basic' for the fallback. */
  engine: 'clickhouse' | 'basic'
  /** ClickHouse's formatter does not keep comments. */
  comments_dropped?: boolean
}

/** Format a SQL query, with details on how it was formatted */
export function formatSQLDetailed(query: string): Promise<FormatResult> {
  return apiPost<FormatResult>('/api/query/format', { query })
}

/** Format a SQL query */
export async function formatSQL(query: string): Promise<string> {
  const res = await formatSQLDetailed(query)
  return res.formatted
}

/** Get EXPLAIN output for a query (bind parameters as for a run) */
export function explainQuery(query: string, params?: Record<string, string>): Promise<LegacyQueryResult> {
  return apiPost<LegacyQueryResult>('/api/query/explain', { query, params })
}

/** Get parsed query plan (tree + raw lines) */
export function fetchQueryPlan(query: string, params?: Record<string, string>): Promise<QueryPlanResult> {
  return apiPost<QueryPlanResult>('/api/query/plan', { query, params })
}

/** Get the system.query_log row of a streamed run, by its query_id */
export function fetchQueryProfile(queryId: string, query?: string): Promise<QueryProfileResult> {
  return apiPost<QueryProfileResult>('/api/query/profile', { query_id: queryId, query })
}

/** Get query cost estimate via EXPLAIN ESTIMATE (single SELECT only) */
export function estimateQuery(query: string, params?: Record<string, string>): Promise<QueryEstimateResult> {
  return apiPost<QueryEstimateResult>('/api/query/estimate', { query, params })
}

/** Execute sampling query: first N rows per shard with fallback to global sample */
export function runSampleQuery(params: {
  query: string
  per_shard?: number
  shard_by?: string
  timeout?: number
  params?: Record<string, string>
}): Promise<SampleQueryResult> {
  return apiPost<SampleQueryResult>('/api/query/sample', {
    query: params.query,
    per_shard: params.per_shard ?? 25,
    shard_by: params.shard_by ?? '_shard_num',
    timeout: params.timeout ?? 45,
    params: params.params,
  })
}

/** Fetch paginated explorer data (JSONCompact format) */
export function fetchExplorerData(params: {
  database: string
  table: string
  page?: number
  page_size?: number
  sort_column?: string
  sort_dir?: string
}): Promise<ExplorerDataResponse> {
  return apiPost<ExplorerDataResponse>('/api/query/explorer-data', {
    database: params.database,
    table: params.table,
    page: params.page ?? 0,
    page_size: params.page_size ?? 100,
    sort_column: params.sort_column ?? '',
    sort_dir: params.sort_dir ?? 'asc',
  })
}

/** List databases */
export async function listDatabases(): Promise<string[]> {
  const res = await apiGet<{ databases: string[] }>('/api/query/databases')
  return res.databases ?? []
}

/** Fetch autocomplete data (functions + keywords) */
export async function fetchCompletions(): Promise<{ functions: string[]; keywords: string[] }> {
  const res = await apiGet<{ functions: string[]; keywords: string[] }>('/api/query/completions')
  return { functions: res.functions ?? [], keywords: res.keywords ?? [] }
}

/** List tables in a database */
export async function listTables(database: string): Promise<string[]> {
  const res = await apiGet<{ tables: Array<{ name: string; engine: string }> }>(`/api/query/tables?database=${encodeURIComponent(database)}`)
  return (res.tables ?? []).map(t => t.name)
}

/** List columns for a table */
export async function listColumns(database: string, table: string): Promise<Column[]> {
  const res = await apiGet<{ columns: Column[] }>(
    `/api/query/columns?database=${encodeURIComponent(database)}&table=${encodeURIComponent(table)}`,
  )
  return res.columns ?? []
}

/** Fetch cluster topology info */
export async function fetchClusterInfo(): Promise<{
  is_cluster: boolean
  clusters: Array<{
    name: string
    shards: number
    replicas: number
    total_nodes: number
    nodes: Array<{
      shard_num: number
      replica_num: number
      host_name: string
      host_address: string
      port: number
      is_local: number
    }>
  }>
}> {
  return apiGet('/api/query/cluster-info')
}

/** Fetch current ClickHouse node identity */
export async function fetchNodeInfo(): Promise<{
  node: {
    hostname: string
    fqdn?: string
    version: string
    uptime_seconds: number
    current_database?: string
  }
}> {
  return apiGet('/api/query/node-info')
}

/** Fetch table metadata from system.tables */
export async function fetchTableInfo(database: string, table: string): Promise<Record<string, unknown>> {
  const db = escapeLiteral(database)
  const tbl = escapeLiteral(table)
  const query = `SELECT database, name, engine, total_rows, total_bytes, lifetime_rows, lifetime_bytes, metadata_modification_time, create_table_query, partition_key, sorting_key, primary_key, sampling_key FROM system.tables WHERE database = '${db}' AND name = '${tbl}'`
  const res = await runQuery({ query })
  if (res.data?.length > 0) {
    const row = res.data[0]
    if (Array.isArray(row)) {
      const obj: Record<string, unknown> = {}
      res.meta.forEach((col, i) => { obj[col.name] = row[i] })
      return obj
    }
    return row as Record<string, unknown>
  }
  return {}
}

/** Fetch table schema via DESCRIBE */
export async function fetchTableSchema(database: string, table: string): Promise<LegacyQueryResult> {
  return runQuery({ query: `DESCRIBE TABLE ${escapeIdentifier(database)}.${escapeIdentifier(table)}` })
}

/** Fetch database metadata and aggregate stats */
export async function fetchDatabaseInfo(database: string): Promise<Record<string, unknown>> {
  const db = escapeLiteral(database)
  const query = `SELECT d.name, d.engine, d.data_path, d.metadata_path, count(t.name) AS table_count, sumOrNull(t.total_rows) AS total_rows, sumOrNull(t.total_bytes) AS total_bytes, maxOrNull(t.metadata_modification_time) AS last_modified FROM system.databases d LEFT JOIN system.tables t ON t.database = d.name WHERE d.name = '${db}' GROUP BY d.name, d.engine, d.data_path, d.metadata_path`
  const res = await runQuery({ query })
  if (res.data?.length > 0) {
    const row = res.data[0]
    if (Array.isArray(row)) {
      const obj: Record<string, unknown> = {}
      res.meta.forEach((col, i) => { obj[col.name] = row[i] })
      return obj
    }
    return row as Record<string, unknown>
  }
  return {}
}

/** Fetch tables list and table-level stats for a database */
export async function fetchDatabaseTables(database: string): Promise<LegacyQueryResult> {
  const db = escapeLiteral(database)
  const query = `SELECT name, engine, total_rows, total_bytes, metadata_modification_time FROM system.tables WHERE database = '${db}' ORDER BY name`
  return runQuery({ query })
}
