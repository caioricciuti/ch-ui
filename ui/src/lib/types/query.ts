/** Column metadata from ClickHouse */
export interface ColumnMeta {
  name: string
  type: string
}

/** Query result in JSONCompact format (positional arrays) */
export interface CompactResult {
  meta: ColumnMeta[]
  data: unknown[][]
  rows: number
  statistics?: QueryStats
}

/** Query execution statistics */
export interface QueryStats {
  elapsed: number
  rows_read: number
  bytes_read: number
}

/** Explorer data response (server-side paginated) */
export interface ExplorerDataResponse {
  success: boolean
  meta: ColumnMeta[]
  data: unknown[][]
  rows: number
  total_rows: number
  page: number
  page_size: number
}

/** Legacy query result (JSON format, row objects) */
export interface LegacyQueryResult {
  success: boolean
  data: Record<string, unknown>[]
  meta: ColumnMeta[]
  statistics?: QueryStats
  rows: number
  elapsed_ms: number
}

export interface SampleQueryResult extends LegacyQueryResult {
  sampling_mode?: 'per_shard' | 'global'
  warning?: string
}

export interface QueryPlanNode {
  id: string
  parent_id?: string
  level: number
  label: string
}

export interface QueryPlanResult {
  success: boolean
  source: string
  lines: string[]
  nodes: QueryPlanNode[]
}

export interface QueryProfileResult {
  success: boolean
  available: boolean
  reason?: string
  profile?: Record<string, unknown>
}

/** NDJSON stream message types */
export type StreamMessage =
  | { type: 'meta'; meta: ColumnMeta[] }
  | { type: 'chunk'; data: unknown[][]; seq: number }
  | { type: 'done'; statistics?: QueryStats; total_rows: number }
  | { type: 'error'; error: string }
