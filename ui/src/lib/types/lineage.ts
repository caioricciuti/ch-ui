/** What a lineage node is. `hidden` is an object the user cannot see; `missing` is referenced but does not exist. */
export type LineageNodeKind =
  | 'table'
  | 'mv'
  | 'refreshable_mv'
  | 'view'
  | 'dictionary'
  | 'distributed'
  | 'buffer'
  | 'merge'
  | 'external'
  | 'hidden'
  | 'missing'

export const LINEAGE_NODE_KINDS: readonly LineageNodeKind[] = [
  'table',
  'mv',
  'refreshable_mv',
  'view',
  'dictionary',
  'distributed',
  'buffer',
  'merge',
  'external',
  'hidden',
  'missing',
]

export type LineageEdgeKind = 'triggers' | 'writes' | 'reads' | 'routes' | 'sources'

export const LINEAGE_EDGE_KINDS: readonly LineageEdgeKind[] = ['triggers', 'writes', 'reads', 'routes', 'sources']

/** system: read from ClickHouse metadata. parsed: inferred from the definition text. */
export type LineageConfidence = 'system' | 'parsed'

export interface LineageRefresh {
  status: string
  last_success: string | null
  next: string | null
}

export interface LineageNode {
  /** `database.name` */
  id: string
  database: string
  name: string
  kind: LineageNodeKind
  engine: string
  rows: number | null
  bytes: number | null
  refresh: LineageRefresh | null
  /** For external nodes: what is on the other side (URL, cluster, engine target). */
  external?: string
}

/** Data flows from `from` (upstream) to `to`. */
export interface LineageEdge {
  from: string
  to: string
  kind: LineageEdgeKind
  confidence: LineageConfidence
}

export interface LineageMeta {
  host: string
  multi_node: boolean
  generated_at: string
  cached: boolean
  node_count: number
  warnings: string[]
}

export interface LineageGraph {
  nodes: LineageNode[]
  edges: LineageEdge[]
  meta: LineageMeta
}

export interface LineageNodeDetail {
  id: string
  database: string
  name: string
  kind: LineageNodeKind
  engine: string
  engine_full: string
  ddl: string
  rows: number | null
  bytes: number | null
}
