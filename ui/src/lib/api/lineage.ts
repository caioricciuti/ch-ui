import { ApiError, apiGet } from './client'
import {
  LINEAGE_EDGE_KINDS,
  LINEAGE_NODE_KINDS,
  type LineageEdge,
  type LineageEdgeKind,
  type LineageGraph,
  type LineageMeta,
  type LineageNode,
  type LineageNodeDetail,
  type LineageNodeKind,
  type LineageRefresh,
} from '../types/lineage'

const BASE = '/api/lineage'

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function str(value: unknown): string {
  return typeof value === 'string' ? value : value == null ? '' : String(value)
}

function strOrNull(value: unknown): string | null {
  return typeof value === 'string' && value ? value : null
}

/** ClickHouse UInt64 can arrive as a string; null stays null (unknown size). */
function numOrNull(value: unknown): number | null {
  if (value == null || value === '') return null
  const n = typeof value === 'number' ? value : Number(value)
  return Number.isFinite(n) ? n : null
}

function nodeKind(value: unknown): LineageNodeKind {
  const s = str(value)
  return (LINEAGE_NODE_KINDS as readonly string[]).includes(s) ? (s as LineageNodeKind) : 'table'
}

function edgeKind(value: unknown): LineageEdgeKind {
  const s = str(value)
  return (LINEAGE_EDGE_KINDS as readonly string[]).includes(s) ? (s as LineageEdgeKind) : 'reads'
}

function parseRefresh(value: unknown): LineageRefresh | null {
  if (!isRecord(value)) return null
  return { status: str(value.status), last_success: strOrNull(value.last_success), next: strOrNull(value.next) }
}

function parseNode(value: unknown): LineageNode | null {
  if (!isRecord(value)) return null
  const id = str(value.id)
  if (!id) return null
  const node: LineageNode = {
    id,
    database: str(value.database),
    name: str(value.name) || id,
    kind: nodeKind(value.kind),
    engine: str(value.engine),
    rows: numOrNull(value.rows),
    bytes: numOrNull(value.bytes),
    refresh: parseRefresh(value.refresh),
  }
  if (typeof value.external === 'string' && value.external) node.external = value.external
  return node
}

function parseEdge(value: unknown): LineageEdge | null {
  if (!isRecord(value)) return null
  const from = str(value.from)
  const to = str(value.to)
  if (!from || !to) return null
  return { from, to, kind: edgeKind(value.kind), confidence: value.confidence === 'parsed' ? 'parsed' : 'system' }
}

/**
 * Normalizes a /api/lineage response: drops malformed entries, dedupes
 * nodes by id, drops edges whose ends are not in the node list, and fills
 * meta defaults. The layout code can then trust every id it sees.
 */
export function parseLineageGraph(raw: unknown): LineageGraph {
  const body = isRecord(raw) ? raw : {}
  const nodes: LineageNode[] = []
  const seen = new Set<string>()
  for (const item of Array.isArray(body.nodes) ? body.nodes : []) {
    const node = parseNode(item)
    if (!node || seen.has(node.id)) continue
    seen.add(node.id)
    nodes.push(node)
  }
  const edges: LineageEdge[] = []
  for (const item of Array.isArray(body.edges) ? body.edges : []) {
    const edge = parseEdge(item)
    if (edge && seen.has(edge.from) && seen.has(edge.to)) edges.push(edge)
  }
  const m = isRecord(body.meta) ? body.meta : {}
  const meta: LineageMeta = {
    host: str(m.host),
    multi_node: m.multi_node === true,
    generated_at: str(m.generated_at),
    cached: m.cached === true,
    node_count: numOrNull(m.node_count) ?? nodes.length,
    warnings: Array.isArray(m.warnings) ? m.warnings.map(str).filter(Boolean) : [],
  }
  return { nodes, edges, meta }
}

export function parseLineageNodeDetail(raw: unknown): LineageNodeDetail {
  const body = isRecord(raw) ? raw : {}
  return {
    id: str(body.id),
    database: str(body.database),
    name: str(body.name),
    kind: nodeKind(body.kind),
    engine: str(body.engine),
    engine_full: str(body.engine_full),
    ddl: str(body.ddl),
    rows: numOrNull(body.rows),
    bytes: numOrNull(body.bytes),
  }
}

/** The lineage URL for a set of databases; refresh skips the server cache. */
export function lineageUrl(databases: readonly string[], refresh = false): string {
  const params = new URLSearchParams()
  params.set('databases', databases.join(','))
  if (refresh) params.set('refresh', '1')
  return `${BASE}?${params.toString()}`
}

export async function fetchLineage(databases: readonly string[], refresh = false): Promise<LineageGraph> {
  return parseLineageGraph(await apiGet<unknown>(lineageUrl(databases, refresh)))
}

/** Database and name go separately: database names can contain dots. */
export function lineageNodeUrl(database: string, name: string): string {
  const params = new URLSearchParams({ database, name })
  return `${BASE}/node?${params.toString()}`
}

/** 404 for hidden, missing and external nodes: callers should not ask for those. */
export async function fetchLineageNode(database: string, name: string): Promise<LineageNodeDetail> {
  return parseLineageNodeDetail(await apiGet<unknown>(lineageNodeUrl(database, name)))
}

/**
 * How a failed /api/lineage call should read. pick: too many objects
 * without a filter (422), the fix is choosing databases. offline: the
 * tunnel is down (503). unreadable: system.tables could not be read (502).
 * The server message is kept verbatim.
 */
export type LineageFailure = { kind: 'pick' | 'offline' | 'unreadable' | 'error'; message: string }

export const LINEAGE_FAILURE_TITLES: Record<LineageFailure['kind'], string> = {
  pick: 'Pick databases',
  offline: 'Connection offline',
  unreadable: 'Could not read table metadata',
  error: 'Could not load lineage',
}

export function lineageFailure(e: unknown): LineageFailure {
  const message = e instanceof Error ? e.message : String(e)
  if (e instanceof ApiError) {
    if (e.status === 422) return { kind: 'pick', message }
    if (e.status === 503) return { kind: 'offline', message }
    if (e.status === 502) return { kind: 'unreadable', message }
  }
  return { kind: 'error', message }
}
