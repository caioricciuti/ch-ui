// Chain rules for the pipeline canvas: one source, transforms in a line, one sink.
// Branches are refused until routing exists.

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

function nameOf(n: ChainNode | undefined): string {
  return String(n?.data.label ?? n?.id ?? 'node')
}

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
 * Returns an error when nodeId is not reached from the source.
 */
export function transformsUpTo(nodes: ChainNode[], edges: ChainEdge[], nodeId: string): { chain: ChainNode[]; error: string } {
  const target = nodes.find((n) => n.id === nodeId)
  if (!target) return { chain: [], error: 'Node not found' }
  // Walk backwards so a node that is not connected to the source still previews its own upstream part.
  const back: ChainNode[] = []
  const byId = new Map(nodes.map((n) => [n.id, n]))
  const seen = new Set<string>()
  let cur: ChainNode | undefined = target
  while (cur && !seen.has(cur.id)) {
    seen.add(cur.id)
    back.push(cur)
    const id: string = cur.id
    const ins = edges.filter((e) => e.target === id)
    if (ins.length === 0) break
    if (ins.length > 1) return { chain: [], error: `${nameOf(cur)} has more than one input` }
    cur = byId.get(ins[0].source)
  }
  const ordered = back.reverse()
  const transforms = ordered.filter((n) => kind(n) === 'transform')
  if (kind(ordered[0]) !== 'source') {
    return { chain: transforms, error: `Connect ${nameOf(ordered[0])} to a source to preview with live data` }
  }
  return { chain: transforms, error: '' }
}

/** Why a new connection is refused, or '' when it fits the chain. */
export function connectionProblem(nodes: ChainNode[], edges: ChainEdge[], sourceId: string, targetId: string): string {
  const from = nodes.find((n) => n.id === sourceId)
  const to = nodes.find((n) => n.id === targetId)
  if (!from || !to) return 'Both ends must be nodes on the canvas'
  if (sourceId === targetId) return 'A node cannot connect to itself'
  if (kind(from) === 'sink') return `${nameOf(from)} is a sink; nothing comes out of it`
  if (kind(to) === 'source') return `${nameOf(to)} is a source; it takes no input`
  if (edges.some((e) => e.source === sourceId && e.target === targetId)) return `${nameOf(from)} already feeds ${nameOf(to)}`
  const out = edges.find((e) => e.source === sourceId)
  if (out) {
    const next = nodes.find((n) => n.id === out.target)
    return `${nameOf(from)} already feeds ${nameOf(next)}. A pipeline is one chain for now (routing comes later); drop a transform onto the edge to insert it.`
  }
  const inc = edges.find((e) => e.target === targetId)
  if (inc) {
    const prev = nodes.find((n) => n.id === inc.source)
    return `${nameOf(to)} already gets input from ${nameOf(prev)}. A pipeline is one chain for now (routing comes later).`
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

/** Splits edgeId into edge -> nodeId -> edge. Unknown edges leave the list as is. */
export function insertIntoEdge(edges: ChainEdge[], edgeId: string, nodeId: string, newId: (n: number) => string): ChainEdge[] {
  const e = edges.find((x) => x.id === edgeId)
  if (!e || e.source === nodeId || e.target === nodeId) return edges
  return [
    ...edges.filter((x) => x.id !== edgeId),
    { id: newId(0), source: e.source, target: nodeId, sourceHandle: e.sourceHandle ?? undefined, animated: true },
    { id: newId(1), source: nodeId, target: e.target, targetHandle: e.targetHandle ?? undefined, animated: true },
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
    let from = ins[0].source
    const guard = new Set<string>([id])
    while (deletedIds.has(from) && !guard.has(from)) {
      guard.add(from)
      const up = edges.filter((e) => e.target === from)
      if (up.length !== 1) break
      from = up[0].source
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
    if (all.some((e) => e.source === from || e.target === to)) continue
    added.push({ id: newId(added.length), source: from, target: to, animated: true })
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

/**
 * After nodeId is inserted into the chain: puts it right of its upstream node
 * and pushes every downstream node right as far as needed, so none overlap.
 */
export function makeRoom<T extends Positioned>(nodes: T[], edges: ChainEdge[], nodeId: string): T[] {
  const byId = new Map(nodes.map((n) => [n.id, n]))
  const pos = new Map(nodes.map((n) => [n.id, { ...n.position }]))
  const width = (id: string) => byId.get(id)?.measured?.width ?? NODE_WIDTH
  const inc = edges.find((e) => e.target === nodeId)
  let prev = inc && byId.has(inc.source) ? inc.source : null
  let cur: string | null = nodeId
  const seen = new Set<string>()
  if (prev) {
    const p = pos.get(prev)!
    pos.set(nodeId, { x: p.x + width(prev) + NODE_GAP, y: p.y })
  }
  while (cur && !seen.has(cur)) {
    seen.add(cur)
    if (prev && cur !== nodeId) {
      const p = pos.get(prev)!
      const c = pos.get(cur)!
      const needed = p.x + width(prev) + NODE_GAP
      if (c.x < needed) pos.set(cur, { x: needed, y: c.y })
    }
    const outs = edges.filter((e) => e.source === cur)
    if (outs.length !== 1 || !byId.has(outs[0].target)) break
    prev = cur
    cur = outs[0].target
  }
  return nodes.map((n) => {
    const p = pos.get(n.id)!
    return p.x === n.position.x && p.y === n.position.y ? n : { ...n, position: p }
  })
}
