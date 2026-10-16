// Graph rules for the pipeline canvas: one source, transforms in a line, then
// either one sink or one Route node whose outputs each lead to a branch
// (transforms then a sink). Other branches are refused.

import { readRoute, routeOutputs, UNMATCHED_HANDLE } from '../../../types/transforms'

export interface ChainNode {
  id: string
  data: Record<string, unknown>
}

export interface ChainEdge {
  id: string
  source: string
  target: string
  sourceHandle?: string | null
  targetHandle?: string | null
  animated?: boolean
}

function kind(n: ChainNode | undefined): 'source' | 'transform' | 'sink' | 'unknown' {
  const t = String(n?.data.node_type ?? '')
  if (t.startsWith('source_')) return 'source'
  if (t.startsWith('transform_')) return 'transform'
  if (t.startsWith('sink_')) return 'sink'
  return 'unknown'
}

export function isRouteNode(n: ChainNode | undefined): boolean {
  return n?.data.node_type === 'transform_route'
}

function nameOf(n: ChainNode | undefined): string {
  return String(n?.data.label ?? n?.id ?? 'node')
}

function configOf(n: ChainNode): Record<string, unknown> {
  const c = n.data.config
  return c && typeof c === 'object' ? (c as Record<string, unknown>) : {}
}

/** The outputs (handle and label) of a route node, in order. */
export function outputsOf(n: ChainNode): { handle: string; label: string }[] {
  return routeOutputs(readRoute(configOf(n)))
}

/** How an output of a route node reads in messages: Route "errors". */
function outputName(n: ChainNode, handle: string | null | undefined): string {
  const label = outputsOf(n).find((o) => o.handle === handle)?.label ?? (handle === UNMATCHED_HANDLE ? 'unmatched' : String(handle ?? ''))
  return `${nameOf(n)} output "${label}"`
}

const sameHandle = (a: string | null | undefined, b: string | null | undefined) => (a ?? null) === (b ?? null)

/** Follows edges from the first source. Stops at the end, a branch or a loop. */
export function chainFromSource(nodes: ChainNode[], edges: ChainEdge[]): ChainNode[] {
  const byId = new Map(nodes.map((n) => [n.id, n]))
  const start = nodes.find((n) => kind(n) === 'source')
  if (!start) return []
  const chain: ChainNode[] = [start]
  const seen = new Set([start.id])
  let cur = start
  for (;;) {
    const outs = edges.filter((e) => e.source === cur.id)
    if (outs.length !== 1) break
    const next = byId.get(outs[0].target)
    if (!next || seen.has(next.id)) break
    chain.push(next)
    seen.add(next.id)
    cur = next
  }
  return chain
}

/**
 * The transforms from the source up to and including nodeId, in chain order.
 * Past a route node, branch is the route output the node hangs off ('' before
 * or on the route). Returns an error when nodeId is not reached from the source.
 */
export function transformsUpTo(nodes: ChainNode[], edges: ChainEdge[], nodeId: string): { chain: ChainNode[]; branch: string; error: string } {
  const target = nodes.find((n) => n.id === nodeId)
  if (!target) return { chain: [], branch: '', error: 'Node not found' }
  // Walk backwards so a node that is not connected to the source still previews its own upstream part.
  const back: ChainNode[] = []
  const byId = new Map(nodes.map((n) => [n.id, n]))
  const seen = new Set<string>()
  let branch = ''
  let cur: ChainNode | undefined = target
  while (cur && !seen.has(cur.id)) {
    seen.add(cur.id)
    back.push(cur)
    const id: string = cur.id
    const ins = edges.filter((e) => e.target === id)
    if (ins.length === 0) break
    if (ins.length > 1) return { chain: [], branch: '', error: `${nameOf(cur)} has more than one input` }
    const parent = byId.get(ins[0].source)
    if (isRouteNode(parent) && !branch) {
      if (!ins[0].sourceHandle) return { chain: [], branch: '', error: `Connect ${nameOf(cur)} to one of the outputs of ${nameOf(parent)}` }
      branch = ins[0].sourceHandle
    }
    cur = parent
  }
  const ordered = back.reverse()
  const transforms = ordered.filter((n) => kind(n) === 'transform')
  if (kind(ordered[0]) !== 'source') {
    return { chain: transforms, branch, error: `Connect ${nameOf(ordered[0])} to a source to preview with live data` }
  }
  return { chain: transforms, branch, error: '' }
}

/**
 * Every sink reached from the first source, through the chain and any route
 * branches, in branch order (route outputs as configured).
 */
export function sinksFromSource(nodes: ChainNode[], edges: ChainEdge[]): ChainNode[] {
  const byId = new Map(nodes.map((n) => [n.id, n]))
  const start = nodes.find((n) => kind(n) === 'source')
  if (!start) return []
  const out: ChainNode[] = []
  const seen = new Set<string>()
  const stack = [start.id]
  while (stack.length) {
    const id = stack.pop() as string
    if (seen.has(id)) continue
    seen.add(id)
    const n = byId.get(id)
    if (!n) continue
    if (kind(n) === 'sink') out.push(n)
    let outs = edges.filter((e) => e.source === id)
    if (isRouteNode(n)) {
      const order = outputsOf(n).map((o) => o.handle)
      const rank = (h: string | null | undefined) => {
        const i = order.indexOf(String(h ?? ''))
        return i < 0 ? order.length : i
      }
      outs = [...outs].sort((x, y) => rank(x.sourceHandle) - rank(y.sourceHandle))
    }
    // Reversed onto the stack so the first output is walked first.
    for (let i = outs.length - 1; i >= 0; i--) stack.push(outs[i].target)
  }
  return out
}

/**
 * The one sink that gets the OTel table default (table): the reachable sink
 * already holding it, else the first reachable sink. Others are left for the
 * user to name, so two branches never share a table by accident.
 */
export function otelDefaultSink(nodes: ChainNode[], edges: ChainEdge[], table: string): string | null {
  const sinks = sinksFromSource(nodes, edges)
  const holder = sinks.find((n) => String(configOf(n).table ?? '').trim() === table)
  return (holder ?? sinks[0])?.id ?? null
}

/** Edges leaving a route node through outputs it no longer has. */
export function staleRouteEdges(nodes: ChainNode[], edges: ChainEdge[]): ChainEdge[] {
  const routes = new Map(nodes.filter(isRouteNode).map((n) => [n.id, new Set(outputsOf(n).map((o) => o.handle))]))
  return edges.filter((e) => {
    const handles = routes.get(e.source)
    return !!handles && !handles.has(String(e.sourceHandle ?? ''))
  })
}

/** Why a new connection is refused, or '' when it fits the graph. sourceHandle is the route output, for route nodes. */
export function connectionProblem(nodes: ChainNode[], edges: ChainEdge[], sourceId: string, targetId: string, sourceHandle?: string | null): string {
  const from = nodes.find((n) => n.id === sourceId)
  const to = nodes.find((n) => n.id === targetId)
  if (!from || !to) return 'Both ends must be nodes on the canvas'
  if (sourceId === targetId) return 'A node cannot connect to itself'
  if (kind(from) === 'sink') return `${nameOf(from)} is a sink; nothing comes out of it`
  if (kind(to) === 'source') return `${nameOf(to)} is a source; it takes no input`
  if (edges.some((e) => e.source === sourceId && e.target === targetId)) return `${nameOf(from)} already feeds ${nameOf(to)}`
  if (isRouteNode(from)) {
    if (!sourceHandle || !outputsOf(from).some((o) => o.handle === sourceHandle)) return `Connect from one of the outputs of ${nameOf(from)}`
    const used = edges.find((e) => e.source === sourceId && sameHandle(e.sourceHandle, sourceHandle))
    if (used) return `${outputName(from, sourceHandle)} already feeds ${nameOf(nodes.find((n) => n.id === used.target))}. Each route output leads to one branch; drop a transform onto the edge to insert it.`
  } else {
    const out = edges.find((e) => e.source === sourceId)
    if (out) {
      const next = nodes.find((n) => n.id === out.target)
      return `${nameOf(from)} already feeds ${nameOf(next)}. Only a Route node has several outputs; drop a transform onto the edge to insert it.`
    }
  }
  const inc = edges.find((e) => e.target === targetId)
  if (inc) {
    const prev = nodes.find((n) => n.id === inc.source)
    return `${nameOf(to)} already gets input from ${nameOf(prev)}. Each node has one input.`
  }
  if (isRouteNode(to)) {
    // Walk up from the new edge's source: a route there would put this one inside a branch.
    const seenUp = new Set<string>()
    let up: string | undefined = sourceId
    while (up && !seenUp.has(up)) {
      seenUp.add(up)
      const n = nodes.find((x) => x.id === up)
      if (isRouteNode(n)) return `${nameOf(to)} cannot go after ${nameOf(n)}; a pipeline has one route, outside its branches`
      up = edges.find((e) => e.target === up)?.source
    }
  }
  // A loop: targetId already reaches sourceId.
  const seen = new Set<string>()
  const stack = [targetId]
  while (stack.length) {
    const id = stack.pop() as string
    if (id === sourceId) return `Connecting ${nameOf(from)} to ${nameOf(to)} would make a loop`
    if (seen.has(id)) continue
    seen.add(id)
    for (const e of edges) if (e.source === id) stack.push(e.target)
  }
  return ''
}

/**
 * Splits edgeId into edge -> nodeId -> edge. Unknown edges leave the list as is.
 * outHandle is the output of nodeId the second edge leaves from (a route's first route).
 */
export function insertIntoEdge(edges: ChainEdge[], edgeId: string, nodeId: string, newId: (n: number) => string, outHandle?: string): ChainEdge[] {
  const e = edges.find((x) => x.id === edgeId)
  if (!e || e.source === nodeId || e.target === nodeId) return edges
  return [
    ...edges.filter((x) => x.id !== edgeId),
    { id: newId(0), source: e.source, target: nodeId, sourceHandle: e.sourceHandle ?? undefined, animated: true },
    { id: newId(1), source: nodeId, target: e.target, sourceHandle: outHandle, targetHandle: e.targetHandle ?? undefined, animated: true },
  ]
}

/**
 * After nodes are deleted, joins the neighbours of each removed node that sat
 * in the middle of the chain (one input, one output), so the chain stays whole.
 */
export function bridgeDeleted(edges: ChainEdge[], deletedIds: Set<string>, newId: (n: number) => string): ChainEdge[] {
  const kept = edges.filter((e) => !deletedIds.has(e.source) && !deletedIds.has(e.target))
  const added: ChainEdge[] = []
  for (const id of deletedIds) {
    const ins = edges.filter((e) => e.target === id)
    const outs = edges.filter((e) => e.source === id)
    if (ins.length !== 1 || outs.length !== 1) continue
    // Walk through runs of deleted nodes to the first kept neighbour on each side.
    // The bridge leaves the kept upstream node through the same output (route handle).
    let from = ins[0].source
    let handle = ins[0].sourceHandle ?? null
    const guard = new Set<string>([id])
    while (deletedIds.has(from) && !guard.has(from)) {
      guard.add(from)
      const up = edges.filter((e) => e.target === from)
      if (up.length !== 1) break
      from = up[0].source
      handle = up[0].sourceHandle ?? null
    }
    let to = outs[0].target
    while (deletedIds.has(to) && !guard.has(to)) {
      guard.add(to)
      const down = edges.filter((e) => e.source === to)
      if (down.length !== 1) break
      to = down[0].target
    }
    if (deletedIds.has(from) || deletedIds.has(to) || from === to) continue
    const all = [...kept, ...added]
    if (all.some((e) => (e.source === from && sameHandle(e.sourceHandle, handle)) || e.target === to)) continue
    added.push({ id: newId(added.length), source: from, target: to, sourceHandle: handle ?? undefined, animated: true })
  }
  return [...kept, ...added]
}

export interface Positioned extends ChainNode {
  position: { x: number; y: number }
  measured?: { width?: number; height?: number }
}

/** Widest a node gets (TransformNode caps at 240 px) plus the gap kept between nodes. */
export const NODE_WIDTH = 240
export const NODE_GAP = 60
/** Height assumed for a node not yet measured, and the gap between stacked branches. */
export const NODE_HEIGHT = 64
export const BRANCH_GAP = 32

/**
 * After nodeId is inserted: puts it right of its upstream node (on the row of
 * its downstream node when it went into a route branch) and pushes every node
 * downstream right as far as needed, through all branches, so none overlap.
 */
export function makeRoom<T extends Positioned>(nodes: T[], edges: ChainEdge[], nodeId: string): T[] {
  const byId = new Map(nodes.map((n) => [n.id, n]))
  const pos = new Map(nodes.map((n) => [n.id, { ...n.position }]))
  const width = (id: string) => byId.get(id)?.measured?.width ?? NODE_WIDTH
  const inc = edges.find((e) => e.target === nodeId)
  const prev = inc && byId.has(inc.source) ? inc.source : null
  if (prev && pos.has(nodeId)) {
    const p = pos.get(prev)!
    const down = edges.find((e) => e.source === nodeId && pos.has(e.target))
    const y = isRouteNode(byId.get(prev)) && down ? pos.get(down.target)!.y : p.y
    pos.set(nodeId, { x: p.x + width(prev) + NODE_GAP, y })
  }
  // Breadth first from the inserted node; each node has one input, so each is placed once.
  const seen = new Set<string>([nodeId])
  const queue = [nodeId]
  while (queue.length) {
    const cur = queue.shift() as string
    const c = pos.get(cur)
    if (!c) continue
    for (const e of edges) {
      if (e.source !== cur || seen.has(e.target) || !pos.has(e.target)) continue
      seen.add(e.target)
      const t = pos.get(e.target)!
      const needed = c.x + width(cur) + NODE_GAP
      if (t.x < needed) pos.set(e.target, { x: needed, y: t.y })
      queue.push(e.target)
    }
  }
  return placed(nodes, pos)
}

function placed<T extends Positioned>(nodes: T[], pos: Map<string, { x: number; y: number }>): T[] {
  return nodes.map((n) => {
    const p = pos.get(n.id)!
    return p.x === n.position.x && p.y === n.position.y ? n : { ...n, position: p }
  })
}

/**
 * Lays out the branches after a route node: one row per connected output, in
 * output order, stacked vertically and centred on the route; each branch runs
 * left to right from the route. Other nodes keep their place.
 */
export function layoutBranches<T extends Positioned>(nodes: T[], edges: ChainEdge[], routeId: string): T[] {
  const byId = new Map(nodes.map((n) => [n.id, n]))
  const route = byId.get(routeId)
  if (!route || !isRouteNode(route)) return nodes
  const width = (id: string) => byId.get(id)?.measured?.width ?? NODE_WIDTH
  const height = (id: string) => byId.get(id)?.measured?.height ?? NODE_HEIGHT
  const order = outputsOf(route).map((o) => o.handle)
  const rank = (h: string | null | undefined) => {
    const i = order.indexOf(String(h ?? ''))
    return i < 0 ? order.length : i
  }
  const outs = edges.filter((e) => e.source === routeId && byId.has(e.target)).sort((a, b) => rank(a.sourceHandle) - rank(b.sourceHandle))
  const seen = new Set<string>([routeId])
  const rows: string[][] = []
  for (const e of outs) {
    const row: string[] = []
    let cur: string | undefined = e.target
    while (cur && byId.has(cur) && !seen.has(cur)) {
      seen.add(cur)
      row.push(cur)
      const next = edges.filter((x) => x.source === cur)
      cur = next.length === 1 ? next[0].target : undefined
    }
    if (row.length) rows.push(row)
  }
  const rowHeights = rows.map((r) => Math.max(...r.map(height)))
  const total = rowHeights.reduce((a, b) => a + b, 0) + BRANCH_GAP * Math.max(0, rows.length - 1)
  let y = route.position.y + height(routeId) / 2 - total / 2
  const pos = new Map(nodes.map((n) => [n.id, { ...n.position }]))
  rows.forEach((row, i) => {
    let x = route.position.x + width(routeId) + NODE_GAP
    for (const id of row) {
      pos.set(id, { x, y })
      x += width(id) + NODE_GAP
    }
    y += rowHeights[i] + BRANCH_GAP
  })
  return placed(nodes, pos)
}

export interface StoredViewport {
  x: number
  y: number
  zoom: number
}

/** The viewport saved in a pipeline's config, or null when there is none (then the canvas fits the graph). */
export function storedViewport(config: string | null | undefined): StoredViewport | null {
  if (!config) return null
  try {
    const v: unknown = JSON.parse(config)
    if (!v || typeof v !== 'object') return null
    const { x, y, zoom } = v as Record<string, unknown>
    if (typeof x !== 'number' || typeof y !== 'number' || typeof zoom !== 'number') return null
    if (!Number.isFinite(x) || !Number.isFinite(y) || !(zoom > 0)) return null
    return { x, y, zoom }
  } catch {
    return null
  }
}

/**
 * Lowest zoom of the pipeline canvas. Svelte Flow clamps every viewport to
 * [minZoom, maxZoom], a fit included, so a wide graph needs this below the
 * default 0.5 to fit at all.
 */
export const CANVAS_MIN_ZOOM = 0.2

/** How the canvas fits a graph without a saved viewport: zoom out as needed, never in past 1. */
export const FIT_VIEW_OPTIONS = { padding: 0.2, minZoom: CANVAS_MIN_ZOOM, maxZoom: 1 } as const
