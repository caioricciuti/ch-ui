/**
 * Lineage layout without the DOM: pick the subgraph around a focus node,
 * collapse other databases into one node each, then place it in layers
 * left to right (upstream on the left). Only the result goes to xyflow, so
 * the cost here is the cost of the page.
 */
import type {
  LineageConfidence,
  LineageEdgeKind,
  LineageGraph,
  LineageNode,
  LineageNodeKind,
} from '../types/lineage'

export const NODE_WIDTH = 220
export const NODE_HEIGHT = 58
export const LAYER_GAP = 96
export const ROW_GAP = 22
export const DEFAULT_HOPS = 2
export const MAX_VISIBLE_NODES = 400
const SWEEPS = 4

const GROUP_PREFIX = 'db:'

/** Node id of a collapsed database. Table ids are `db.name`, so no clash. */
export function groupId(database: string): string {
  return GROUP_PREFIX + database
}

export function isGroupId(id: string): boolean {
  return id.startsWith(GROUP_PREFIX)
}

// ── Index ───────────────────────────────────────────────────────

export interface LineageIndex {
  nodes: LineageNode[]
  byId: Map<string, number>
  /** node index -> indices into graph.edges leaving it */
  out: number[][]
  /** node index -> indices into graph.edges entering it */
  inc: number[][]
}

const indexCache = new WeakMap<LineageGraph, LineageIndex>()

/** Adjacency for a response, built once and reused for every refocus. */
export function indexLineage(graph: LineageGraph): LineageIndex {
  const cached = indexCache.get(graph)
  if (cached) return cached
  const n = graph.nodes.length
  const byId = new Map<string, number>()
  for (let i = 0; i < n; i++) byId.set(graph.nodes[i].id, i)
  const out: number[][] = Array.from({ length: n }, () => [])
  const inc: number[][] = Array.from({ length: n }, () => [])
  for (let e = 0; e < graph.edges.length; e++) {
    const a = byId.get(graph.edges[e].from)
    const b = byId.get(graph.edges[e].to)
    if (a === undefined || b === undefined) continue
    out[a].push(e)
    inc[b].push(e)
  }
  const index = { nodes: graph.nodes, byId, out, inc }
  indexCache.set(graph, index)
  return index
}

// ── Subgraph ────────────────────────────────────────────────────

/**
 * Node indices within `up` hops upstream and `down` hops downstream of
 * the focus, the focus included. The two directions are walked apart, so a
 * sibling (upstream of a downstream node) is not pulled in.
 */
export function neighborhood(graph: LineageGraph, focus: string, up: number, down: number): Set<number> {
  const index = indexLineage(graph)
  const start = index.byId.get(focus)
  const result = new Set<number>()
  if (start === undefined) return result
  result.add(start)
  walk(index, graph, start, up, 'up', result)
  walk(index, graph, start, down, 'down', result)
  return result
}

function walk(index: LineageIndex, graph: LineageGraph, start: number, hops: number, dir: 'up' | 'down', result: Set<number>): void {
  if (hops <= 0) return
  const seen = new Set<number>([start])
  let frontier = [start]
  for (let depth = 0; depth < hops && frontier.length > 0; depth++) {
    const next: number[] = []
    for (const v of frontier) {
      const list = dir === 'up' ? index.inc[v] : index.out[v]
      for (const e of list) {
        const edge = graph.edges[e]
        const u = index.byId.get(dir === 'up' ? edge.from : edge.to)
        if (u === undefined || seen.has(u)) continue
        seen.add(u)
        result.add(u)
        next.push(u)
      }
    }
    frontier = next
  }
}

/** Nodes with no edge in or out: objects lineage has nothing to say about. */
export function isolatedCount(graph: LineageGraph): number {
  const index = indexLineage(graph)
  let count = 0
  for (let i = 0; i < index.nodes.length; i++) if (index.out[i].length === 0 && index.inc[i].length === 0) count++
  return count
}

// ── Layout ──────────────────────────────────────────────────────

export interface LineageLayoutOptions {
  /** Node to center on. Without one, every node of the response is shown. */
  focus?: string | null
  /** Hops upstream of the focus. */
  up?: number
  /** Hops downstream of the focus. */
  down?: number
  /** Fold databases other than the focus database into one node each. Default true. */
  collapse?: boolean
  /** Databases shown node by node even when collapse is on. */
  expanded?: ReadonlySet<string>
  /** Show only these kinds (the focus always shows). Null or empty shows all. */
  kinds?: ReadonlySet<LineageNodeKind> | null
  /** Without a focus, leave out nodes that have no edges at all. */
  hideIsolated?: boolean
  /** Above this many nodes the layout gives up and reports tooLarge. */
  maxNodes?: number
}

export interface LayoutGroup {
  database: string
  count: number
}

export interface LayoutNode {
  id: string
  x: number
  y: number
  layer: number
  /** The object, or null for a collapsed database. */
  node: LineageNode | null
  group: LayoutGroup | null
  focus: boolean
}

export interface LayoutEdge {
  id: string
  from: string
  to: string
  kinds: LineageEdgeKind[]
  /** system when any folded edge is system, parsed only when all are. */
  confidence: LineageConfidence
  /** How many raw edges this one stands for (more than 1 after collapsing). */
  count: number
  /** Closes a cycle: ignored for layering, drawn marked. */
  back: boolean
}

export type LineageLayout =
  | { tooLarge: false; nodes: LayoutNode[]; edges: LayoutEdge[]; total: number }
  | { tooLarge: true; count: number; total: number }

interface WorkEdge {
  a: number
  b: number
  kinds: Set<LineageEdgeKind>
  system: boolean
  count: number
  back: boolean
}

export function layoutLineage(graph: LineageGraph, options: LineageLayoutOptions = {}): LineageLayout {
  const index = indexLineage(graph)
  const total = graph.nodes.length
  const maxNodes = options.maxNodes ?? MAX_VISIBLE_NODES
  const focusIdx = options.focus ? index.byId.get(options.focus) : undefined
  const focusNode = focusIdx === undefined ? null : graph.nodes[focusIdx]
  const kinds = options.kinds && options.kinds.size > 0 ? options.kinds : null
  const collapse = (options.collapse ?? true) && focusNode !== null
  const expanded = options.expanded

  // 1. Which raw nodes are in view.
  let picked: Iterable<number>
  if (focusIdx !== undefined) {
    picked = neighborhood(graph, focusNode!.id, options.up ?? DEFAULT_HOPS, options.down ?? DEFAULT_HOPS)
  } else {
    picked = graph.nodes.keys()
  }
  const visible = new Uint8Array(total)
  const visibleList: number[] = []
  const dropIsolated = options.hideIsolated === true && focusIdx === undefined
  for (const i of picked) {
    if (kinds && i !== focusIdx && !kinds.has(graph.nodes[i].kind)) continue
    if (dropIsolated && index.out[i].length === 0 && index.inc[i].length === 0) continue
    visible[i] = 1
    visibleList.push(i)
  }

  // 2. Representatives: the node itself, or its database when folded.
  const repKey: string[] = new Array(total)
  const repIndex = new Map<string, number>()
  const repNode: (LineageNode | null)[] = []
  const repGroup: (LayoutGroup | null)[] = []
  const repOf = new Int32Array(total).fill(-1)
  for (const i of visibleList) {
    const node = graph.nodes[i]
    const fold = collapse && i !== focusIdx && node.database !== '' && node.database !== focusNode!.database && !expanded?.has(node.database)
    const key = fold ? groupId(node.database) : node.id
    let r = repIndex.get(key)
    if (r === undefined) {
      r = repNode.length
      repIndex.set(key, r)
      repNode.push(fold ? null : node)
      repGroup.push(fold ? { database: node.database, count: 0 } : null)
    }
    const group = repGroup[r]
    if (group) group.count++
    repOf[i] = r
    repKey[i] = key
  }
  const count = repNode.length
  if (count > maxNodes) return { tooLarge: true, count, total }

  // 3. Edges between representatives, folded edges merged.
  const edgeByKey = new Map<string, WorkEdge>()
  for (const i of visibleList) {
    for (const e of index.out[i]) {
      const edge = graph.edges[e]
      const j = index.byId.get(edge.to)
      if (j === undefined || !visible[j]) continue
      const a = repOf[i]
      const b = repOf[j]
      if (a === b && repGroup[a]) continue // inside one folded database
      const key = repKey[i] + '\u0000' + repKey[j]
      let w = edgeByKey.get(key)
      if (!w) {
        w = { a, b, kinds: new Set(), system: false, count: 0, back: false }
        edgeByKey.set(key, w)
      }
      w.kinds.add(edge.kind)
      if (edge.confidence === 'system') w.system = true
      w.count++
    }
  }
  const work = [...edgeByKey.values()]

  const ids = [...repIndex.keys()] // insertion order, so ids[r] is representative r
  const placed = placeNodes(count, ids, work)

  const nodes: LayoutNode[] = new Array(count)
  for (let r = 0; r < count; r++) {
    nodes[r] = {
      id: ids[r],
      x: placed.x[r],
      y: placed.y[r],
      layer: placed.layer[r],
      node: repNode[r],
      group: repGroup[r],
      focus: focusIdx !== undefined && repNode[r] === focusNode,
    }
  }
  const edges: LayoutEdge[] = work.map((w) => ({
    id: `${ids[w.a]}->${ids[w.b]}`,
    from: ids[w.a],
    to: ids[w.b],
    kinds: [...w.kinds].sort(),
    confidence: w.system ? 'system' : 'parsed',
    count: w.count,
    back: w.back,
  }))
  return { tooLarge: false, nodes, edges, total }
}

interface Placement {
  layer: Int32Array
  x: Float64Array
  y: Float64Array
}

/**
 * Layered placement over node ids 0..n-1. Marks back edges on `edges` in
 * place. Every step is deterministic: the same ids and edges give the same
 * picture whatever order they came in.
 */
export function placeNodes(n: number, ids: readonly string[], edges: { a: number; b: number; back: boolean }[]): Placement {
  const byName = Array.from({ length: n }, (_, i) => i).sort((p, q) => cmp(ids[p], ids[q]))
  const rank = new Int32Array(n)
  for (let i = 0; i < n; i++) rank[byName[i]] = i

  const out: number[][] = Array.from({ length: n }, () => [])
  const indeg = new Int32Array(n)
  for (let e = 0; e < edges.length; e++) {
    out[edges[e].a].push(e)
    indeg[edges[e].b]++
  }
  for (const list of out) list.sort((p, q) => rank[edges[p].b] - rank[edges[q].b])

  // Break cycles: an iterative DFS marks every edge that points back into
  // the current path. Roots go sources first, so a cycle hanging off a
  // source is cut at its far end, not at the entry.
  const state = new Uint8Array(n) // 0 new, 1 on the path, 2 done
  const discovery: number[] = []
  const roots = byName.filter((v) => indeg[v] === 0).concat(byName.filter((v) => indeg[v] !== 0))
  const stackNode: number[] = []
  const stackPos: number[] = []
  for (const root of roots) {
    if (state[root] !== 0) continue
    state[root] = 1
    discovery.push(root)
    stackNode.push(root)
    stackPos.push(0)
    while (stackNode.length > 0) {
      const top = stackNode.length - 1
      const v = stackNode[top]
      const list = out[v]
      if (stackPos[top] < list.length) {
        const e = list[stackPos[top]++]
        const w = edges[e].b
        if (state[w] === 1) edges[e].back = true
        else if (state[w] === 0) {
          state[w] = 1
          discovery.push(w)
          stackNode.push(w)
          stackPos.push(0)
        }
      } else {
        state[v] = 2
        stackNode.pop()
        stackPos.pop()
      }
    }
  }

  // Layers by longest path over the forward edges (Kahn order).
  const layer = new Int32Array(n)
  const fwdIn = new Int32Array(n)
  for (const e of edges) if (!e.back) fwdIn[e.b]++
  const queue: number[] = byName.filter((v) => fwdIn[v] === 0)
  for (let head = 0; head < queue.length; head++) {
    const v = queue[head]
    for (const e of out[v]) {
      const edge = edges[e]
      if (edge.back) continue
      if (layer[v] + 1 > layer[edge.b]) layer[edge.b] = layer[v] + 1
      if (--fwdIn[edge.b] === 0) queue.push(edge.b)
    }
  }

  // Start each layer in DFS discovery order, which keeps chains together.
  let layerCount = 0
  for (let v = 0; v < n; v++) if (layer[v] + 1 > layerCount) layerCount = layer[v] + 1
  const layers: number[][] = Array.from({ length: layerCount }, () => [])
  for (const v of discovery) layers[layer[v]].push(v)

  // Neighbours on each side, for the sweeps. Self loops carry no order.
  const preds: number[][] = Array.from({ length: n }, () => [])
  const succs: number[][] = Array.from({ length: n }, () => [])
  for (const e of edges) {
    if (e.a === e.b) continue
    const [lo, hi] = layer[e.a] <= layer[e.b] ? [e.a, e.b] : [e.b, e.a]
    if (layer[lo] === layer[hi]) continue
    succs[lo].push(hi)
    preds[hi].push(lo)
  }

  // Centered row of a node: index minus half the layer, so rows of layers
  // with different sizes compare.
  const row = new Float64Array(n)
  const setRows = (list: number[]) => {
    const mid = (list.length - 1) / 2
    for (let i = 0; i < list.length; i++) row[list[i]] = i - mid
  }
  for (const list of layers) setRows(list)

  const bary = new Float64Array(n)
  for (let sweep = 0; sweep < SWEEPS; sweep++) {
    const down = sweep % 2 === 0
    for (let k = 0; k < layerCount; k++) {
      const list = layers[down ? k : layerCount - 1 - k]
      if (list.length < 2) continue
      for (const v of list) {
        const nb = down ? preds[v] : succs[v]
        if (nb.length === 0) {
          bary[v] = row[v]
          continue
        }
        let sum = 0
        for (const u of nb) sum += row[u]
        bary[v] = sum / nb.length
      }
      // Ties keep the current order, so repeated sweeps settle instead of shuffling.
      list.sort((p, q) => bary[p] - bary[q] || row[p] - row[q])
      setRows(list)
    }
  }

  const x = new Float64Array(n)
  const y = new Float64Array(n)
  for (let v = 0; v < n; v++) {
    x[v] = layer[v] * (NODE_WIDTH + LAYER_GAP)
    y[v] = row[v] * (NODE_HEIGHT + ROW_GAP)
  }
  return { layer, x, y }
}

function cmp(a: string, b: string): number {
  return a < b ? -1 : a > b ? 1 : 0
}

// ── Formatting ──────────────────────────────────────────────────

export const KIND_LABELS: Record<LineageNodeKind, string> = {
  table: 'Table',
  mv: 'Materialized view',
  refreshable_mv: 'Refreshable MV',
  view: 'View',
  dictionary: 'Dictionary',
  distributed: 'Distributed',
  buffer: 'Buffer',
  merge: 'Merge',
  external: 'External',
  hidden: 'No access',
  missing: 'Missing',
}

/** "1.2M rows" style figures for a node card; null means unknown, shown as nothing. */
export function compactCount(n: number | null): string {
  if (n === null) return ''
  const abs = Math.abs(n)
  const units: [number, string][] = [[1e12, 'T'], [1e9, 'B'], [1e6, 'M'], [1e3, 'K']]
  for (const [scale, suffix] of units) {
    if (abs >= scale) {
      const v = n / scale
      return `${v >= 100 ? v.toFixed(0) : v.toFixed(1).replace(/\.0$/, '')}${suffix}`
    }
  }
  return String(n)
}
