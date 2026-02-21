import { apiGet, apiPost } from './client'
import type {
  LegacyQueryResult,
  ExplorerDataResponse,
  QueryPlanResult,
  QueryProfileResult,
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

/** Format a SQL query */
export async function formatSQL(query: string): Promise<string> {
  const res = await apiPost<{ formatted: string }>('/api/query/format', { query })
  return res.formatted
}

/** Get EXPLAIN output for a query */
export function explainQuery(query: string): Promise<LegacyQueryResult> {
  return apiPost<LegacyQueryResult>('/api/query/explain', { query })
}

/** Get parsed query plan (tree + raw lines) */
export function fetchQueryPlan(query: string): Promise<QueryPlanResult> {
  return apiPost<QueryPlanResult>('/api/query/plan', { query })
}

/** Get inline profiling row from system.query_log for a query */
export function fetchQueryProfile(query: string): Promise<QueryProfileResult> {
  return apiPost<QueryProfileResult>('/api/query/profile', { query })
}

/** Execute sampling query: first N rows per shard with fallback to global sample */
export function runSampleQuery(params: {
  query: string
  per_shard?: number
  shard_by?: string
  timeout?: number
}): Promise<SampleQueryResult> {
  return apiPost<SampleQueryResult>('/api/query/sample', {
    query: params.query,
    per_shard: params.per_shard ?? 25,
    shard_by: params.shard_by ?? '_shard_num',
    timeout: params.timeout ?? 45,
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
  const res = await apiGet<{ tables: string[] }>(`/api/query/tables?database=${encodeURIComponent(database)}`)
  return res.tables ?? []
}

/** List columns for a table */
export async function listColumns(database: string, table: string): Promise<Column[]> {
  const res = await apiGet<{ columns: Column[] }>(
    `/api/query/columns?database=${encodeURIComponent(database)}&table=${encodeURIComponent(table)}`,
  )
  return res.columns ?? []
}

/** Fetch table metadata from system.tables */
export async function fetchTableInfo(database: string, table: string): Promise<Record<string, any>> {
  const db = escapeLiteral(database)
  const tbl = escapeLiteral(table)
  const query = `SELECT database, name, engine, total_rows, total_bytes, lifetime_rows, lifetime_bytes, metadata_modification_time, create_table_query, partition_key, sorting_key, primary_key, sampling_key FROM system.tables WHERE database = '${db}' AND name = '${tbl}'`
  const res = await runQuery({ query })
  if (res.data?.length > 0) {
    const row = res.data[0]
    if (Array.isArray(row)) {
      const obj: Record<string, any> = {}
      res.meta.forEach((col: any, i: number) => { obj[col.name] = row[i] })
      return obj
    }
    return row as Record<string, any>
  }
  return {}
}

/** Fetch table schema via DESCRIBE */
export async function fetchTableSchema(database: string, table: string): Promise<LegacyQueryResult> {
  return runQuery({ query: `DESCRIBE TABLE ${escapeIdentifier(database)}.${escapeIdentifier(table)}` })
}

/** Fetch database metadata and aggregate stats */
export async function fetchDatabaseInfo(database: string): Promise<Record<string, any>> {
  const db = escapeLiteral(database)
  const query = `SELECT d.name, d.engine, d.data_path, d.metadata_path, count(t.name) AS table_count, sumOrNull(t.total_rows) AS total_rows, sumOrNull(t.total_bytes) AS total_bytes, maxOrNull(t.metadata_modification_time) AS last_modified FROM system.databases d LEFT JOIN system.tables t ON t.database = d.name WHERE d.name = '${db}' GROUP BY d.name, d.engine, d.data_path, d.metadata_path`
  const res = await runQuery({ query })
  if (res.data?.length > 0) {
    const row = res.data[0]
    if (Array.isArray(row)) {
      const obj: Record<string, any> = {}
      res.meta.forEach((col: any, i: number) => { obj[col.name] = row[i] })
      return obj
    }
    return row as Record<string, any>
  }
  return {}
}

/** Fetch tables list and table-level stats for a database */
export async function fetchDatabaseTables(database: string): Promise<LegacyQueryResult> {
  const db = escapeLiteral(database)
  const query = `SELECT name, engine, total_rows, total_bytes, metadata_modification_time FROM system.tables WHERE database = '${db}' ORDER BY name`
  return runQuery({ query })
}
