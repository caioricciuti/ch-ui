import { describe, expect, it } from 'vitest'
import { CANVAS_MIN_ZOOM, FIT_VIEW_OPTIONS, otelDefaultSink, storedViewport, BRANCH_GAP, NODE_GAP, NODE_HEIGHT, NODE_WIDTH, bridgeDeleted, chainFromSource, connectionProblem, insertIntoEdge, layoutBranches, makeRoom, sinksFromSource, staleRouteEdges, transformsUpTo, type ChainEdge, type ChainNode } from './graph'

const node = (id: string, node_type: string): ChainNode => ({ id, data: { label: id.toUpperCase(), node_type } })
const edge = (source: string, target: string): ChainEdge => ({ id: `${source}-${target}`, source, target })

const src = node('s', 'source_ingest')
const parse = node('p', 'transform_parse')
const filter = node('f', 'transform_filter')
const sink = node('k', 'sink_clickhouse')
const nodes = [sink, filter, parse, src]
const chained = [edge('s', 'p'), edge('p', 'f'), edge('f', 'k')]

let n = 0
const ids = () => `e${++n}`

describe('chainFromSource', () => {
  it('follows edges from the source to the sink', () => {
    expect(chainFromSource(nodes, chained).map((x) => x.id)).toEqual(['s', 'p', 'f', 'k'])
  })

  it('stops at a gap', () => {
    expect(chainFromSource(nodes, [edge('s', 'p')]).map((x) => x.id)).toEqual(['s', 'p'])
  })

  it('stops at a loop', () => {
    expect(chainFromSource(nodes, [edge('s', 'p'), edge('p', 'f'), edge('f', 'p')]).map((x) => x.id)).toEqual(['s', 'p', 'f'])
  })
})

describe('transformsUpTo', () => {
  it('returns the transforms up to and including the node, in order', () => {
    expect(transformsUpTo(nodes, chained, 'f')).toEqual({ chain: [parse, filter], branch: '', error: '' })
    expect(transformsUpTo(nodes, chained, 'p')).toEqual({ chain: [parse], branch: '', error: '' })
  })

  it('still returns the upstream part of an unconnected node, with a note', () => {
    const r = transformsUpTo(nodes, [edge('p', 'f')], 'f')
    expect(r.chain).toEqual([parse, filter])
    expect(r.error).toBe('Connect P to a source to preview with live data')
  })
})

describe('connectionProblem', () => {
  it('accepts the next link of a chain', () => {
    expect(connectionProblem(nodes, [edge('s', 'p')], 'p', 'f')).toBe('')
  })

  it('refuses branches, naming the nodes', () => {
    expect(connectionProblem(nodes, chained, 's', 'f')).toMatch(/^S already feeds P\. Only a Route node has several outputs/)
    expect(connectionProblem(nodes, [edge('p', 'k')], 'f', 'k')).toMatch(/^K already gets input from P/)
  })

  it('refuses wrong directions, self links and loops', () => {
    expect(connectionProblem(nodes, [], 'k', 'p')).toMatch(/K is a sink/)
    expect(connectionProblem(nodes, [], 'p', 's')).toMatch(/S is a source/)
    expect(connectionProblem(nodes, [], 'p', 'p')).toMatch(/itself/)
    expect(connectionProblem(nodes, [edge('p', 'f')], 'f', 'p')).toMatch(/loop/)
  })
})

describe('insertIntoEdge', () => {
  it('splits the edge around the new node', () => {
    const t = node('m', 'transform_map')
    const out = insertIntoEdge([edge('s', 'k')], 's-k', t.id, ids)
    expect(out.map((e) => [e.source, e.target])).toEqual([['s', 'm'], ['m', 'k']])
    expect(new Set(out.map((e) => e.id)).size).toBe(2)
  })

  it('leaves unknown edges alone', () => {
    const list = [edge('s', 'k')]
    expect(insertIntoEdge(list, 'nope', 'm', ids)).toBe(list)
  })
})

describe('bridgeDeleted', () => {
  it('rejoins the chain around a removed middle node', () => {
    const out = bridgeDeleted(chained, new Set(['p']), ids)
    expect(out.map((e) => [e.source, e.target])).toEqual([['f', 'k'], ['s', 'f']])
  })

  it('rejoins around a run of removed nodes', () => {
    const out = bridgeDeleted(chained, new Set(['p', 'f']), ids)
    expect(out.map((e) => [e.source, e.target])).toEqual([['s', 'k']])
  })

  it('does not bridge an end of the chain', () => {
    expect(bridgeDeleted(chained, new Set(['k']), ids).map((e) => e.id)).toEqual(['s-p', 'p-f'])
    expect(bridgeDeleted(chained, new Set(['s']), ids).map((e) => e.id)).toEqual(['p-f', 'f-k'])
  })
})

describe('makeRoom', () => {
  const step = NODE_WIDTH + NODE_GAP
  const at = (id: string, node_type: string, x: number) => ({ ...node(id, node_type), position: { x, y: 0 } })

  it('places the inserted node after its upstream node and pushes the rest right', () => {
    const list = [at('s', 'source_ingest', 0), at('m', 'transform_map', 150), at('k', 'sink_clickhouse', 300)]
    const out = makeRoom(list, [edge('s', 'm'), edge('m', 'k')], 'm')
    expect(out.map((x) => x.position.x)).toEqual([0, step, 2 * step])
  })

  it('keeps nodes that already have room, and pushes everything after an insert in a long chain', () => {
    const list = [at('s', 'source_ingest', 0), at('a', 'transform_map', step), at('b', 'transform_map', step + 10), at('c', 'transform_map', 3 * step), at('k', 'sink_clickhouse', 5 * step)]
    const out = makeRoom(list, [edge('s', 'a'), edge('a', 'b'), edge('b', 'c'), edge('c', 'k')], 'b')
    expect(out.map((x) => x.position.x)).toEqual([0, step, 2 * step, 3 * step, 5 * step])
    expect(out[0]).toBe(list[0])
  })

  it('uses measured widths when known', () => {
    const list = [{ ...at('s', 'source_ingest', 0), measured: { width: 100 } }, at('m', 'transform_map', 0)]
    expect(makeRoom(list, [edge('s', 'm')], 'm')[1].position.x).toBe(100 + NODE_GAP)
  })
})

// ---- Routing ----

const routeNode = (id: string): ChainNode => ({
  id,
  data: {
    label: id.toUpperCase(),
    node_type: 'transform_route',
    config: {
      mode: 'first',
      routes: [
        { id: 'r1', name: 'errors', match: 'all', rules: [] },
        { id: 'r2', name: 'slow', match: 'all', rules: [] },
      ],
      unmatched: 'output',
    },
  },
})
const hedge = (source: string, target: string, sourceHandle: string): ChainEdge => ({ id: `${source}-${target}`, source, target, sourceHandle })

// s -> p -> R -> (r1) m -> k1, (r2) k2, (unmatched) k3
const route = routeNode('r')
const map = node('m', 'transform_map')
const k1 = node('k1', 'sink_clickhouse')
const k2 = node('k2', 'sink_clickhouse')
const k3 = node('k3', 'sink_clickhouse')
const tree = [src, parse, route, map, k1, k2, k3]
const treeEdges = [edge('s', 'p'), edge('p', 'r'), hedge('r', 'm', 'r1'), edge('m', 'k1'), hedge('r', 'k2', 'r2'), hedge('r', 'k3', 'unmatched')]

describe('routing: preview chains', () => {
  it('previews the route node with the shared chain and no branch', () => {
    expect(transformsUpTo(tree, treeEdges, 'r')).toEqual({ chain: [parse, route], branch: '', error: '' })
  })

  it('previews a branch node with the route and its branch handle', () => {
    expect(transformsUpTo(tree, treeEdges, 'm')).toEqual({ chain: [parse, route, map], branch: 'r1', error: '' })
  })

  it('asks for an output when a route edge has no handle', () => {
    expect(transformsUpTo(tree, [edge('s', 'p'), edge('p', 'r'), edge('r', 'm')], 'm').error).toBe('Connect M to one of the outputs of R')
  })

  it('finds every sink of the tree', () => {
    expect(sinksFromSource(tree, treeEdges).map((x) => x.id)).toEqual(['k1', 'k2', 'k3'])
    // Output order, not edge order.
    expect(sinksFromSource(tree, [...treeEdges].reverse()).map((x) => x.id)).toEqual(['k1', 'k2', 'k3'])
    expect(sinksFromSource(nodes, chained).map((x) => x.id)).toEqual(['k'])
  })
})

describe('routing: connections', () => {
  const partial = [edge('s', 'p'), edge('p', 'r'), hedge('r', 'm', 'r1')]

  it('allows one edge per route output', () => {
    expect(connectionProblem(tree, partial, 'r', 'k2', 'r2')).toBe('')
    expect(connectionProblem(tree, partial, 'r', 'k3', 'unmatched')).toBe('')
    expect(connectionProblem(tree, partial, 'r', 'k2', 'r1')).toMatch(/^R output "errors" already feeds M\. Each route output leads to one branch/)
  })

  it('refuses an unknown or missing route output', () => {
    expect(connectionProblem(tree, partial, 'r', 'k2', 'r9')).toBe('Connect from one of the outputs of R')
    expect(connectionProblem(tree, partial, 'r', 'k2')).toBe('Connect from one of the outputs of R')
    const noUnmatched = { ...route, data: { ...route.data, config: { ...(route.data.config as object), unmatched: 'drop' } } }
    expect(connectionProblem([src, parse, noUnmatched, map, k1, k2], partial, 'r', 'k2', 'unmatched')).toBe('Connect from one of the outputs of R')
  })

  it('still refuses other branches and second inputs', () => {
    expect(connectionProblem(tree, partial, 'p', 'k2')).toMatch(/^P already feeds R\./)
    expect(connectionProblem(tree, [...partial, edge('m', 'k1')], 'r', 'k1', 'r2')).toMatch(/^K1 already gets input from M/)
  })

  it('refuses a route inside a branch', () => {
    const r2 = { ...routeNode('q') }
    expect(connectionProblem([...tree, r2], partial, 'm', 'q')).toMatch(/^Q cannot go after R/)
  })
})

describe('routing: edits', () => {
  it('finds edges of removed outputs', () => {
    const fewer = { ...route, data: { ...route.data, config: { mode: 'first', routes: [{ id: 'r1', name: 'errors', rules: [] }], unmatched: 'drop' } } }
    expect(staleRouteEdges([src, parse, fewer, map, k1, k2, k3], treeEdges).map((e) => e.id)).toEqual(['r-k2', 'r-k3'])
    expect(staleRouteEdges(tree, treeEdges)).toEqual([])
  })

  it('keeps the route output when inserting into a branch edge', () => {
    const out = insertIntoEdge(treeEdges, 'r-k2', 'x', ids)
    expect(out.find((e) => e.target === 'x')).toMatchObject({ source: 'r', sourceHandle: 'r2' })
    expect(out.find((e) => e.source === 'x')).toMatchObject({ target: 'k2', sourceHandle: undefined })
  })

  it('gives an inserted route its first output downstream', () => {
    const out = insertIntoEdge([edge('p', 'k')], 'p-k', 'r', ids, 'r1')
    expect(out.find((e) => e.source === 'r')).toMatchObject({ target: 'k', sourceHandle: 'r1' })
  })

  it('bridges a removed branch transform on the same route output', () => {
    const out = bridgeDeleted(treeEdges, new Set(['m']), ids)
    expect(out.find((e) => e.target === 'k1')).toMatchObject({ source: 'r', sourceHandle: 'r1' })
  })
})

describe('routing: layout', () => {
  const at = (n: ChainNode, x: number, y: number) => ({ ...n, position: { x, y } })
  const step = NODE_WIDTH + NODE_GAP

  function overlaps(list: { position: { x: number; y: number } }[]): boolean {
    for (let i = 0; i < list.length; i++) {
      for (let j = i + 1; j < list.length; j++) {
        const a = list[i].position
        const b = list[j].position
        if (Math.abs(a.x - b.x) < NODE_WIDTH && Math.abs(a.y - b.y) < NODE_HEIGHT) return true
      }
    }
    return false
  }

  it('stacks the branches after the route in output order, centred on it', () => {
    const list = [at(src, 0, 0), at(parse, step, 0), at(route, 2 * step, 0), at(map, 0, 0), at(k1, 0, 0), at(k2, 0, 0), at(k3, 0, 0)]
    const out = layoutBranches(list, treeEdges, 'r')
    const pos = Object.fromEntries(out.map((x) => [x.id, x.position]))
    const row = NODE_HEIGHT + BRANCH_GAP
    expect(pos.m).toEqual({ x: 3 * step, y: -row })
    expect(pos.k1).toEqual({ x: 4 * step, y: -row })
    expect(pos.k2).toEqual({ x: 3 * step, y: 0 })
    expect(pos.k3).toEqual({ x: 3 * step, y: row })
    expect(pos.s).toEqual({ x: 0, y: 0 })
    expect(overlaps(out)).toBe(false)
  })

  it('pushes the route and every branch right after an insert in the shared chain', () => {
    const laid = layoutBranches([at(src, 0, 0), at(parse, step, 0), at(route, 2 * step, 0), at(map, 0, 0), at(k1, 0, 0), at(k2, 0, 0), at(k3, 0, 0)], treeEdges, 'r')
    const x = node('x', 'transform_filter')
    const edges2 = insertIntoEdge(treeEdges, 's-p', 'x', ids)
    const out = makeRoom([...laid, at(x, 10, 10)], edges2, 'x')
    const pos = Object.fromEntries(out.map((n) => [n.id, n.position]))
    expect(pos.x).toEqual({ x: step, y: 0 })
    expect(pos.p.x).toBe(2 * step)
    expect(pos.r.x).toBe(3 * step)
    expect([pos.m.x, pos.k2.x, pos.k3.x]).toEqual([4 * step, 4 * step, 4 * step])
    expect(pos.k1.x).toBe(5 * step)
    expect(overlaps(out)).toBe(false)
  })

  it('puts a node inserted into a branch on that branch row', () => {
    const laid = layoutBranches([at(src, 0, 0), at(parse, step, 0), at(route, 2 * step, 0), at(map, 0, 0), at(k1, 0, 0), at(k2, 0, 0), at(k3, 0, 0)], treeEdges, 'r')
    const x = node('x', 'transform_filter')
    const edges2 = insertIntoEdge(treeEdges, 'r-k3', 'x', ids)
    const out = makeRoom([...laid, at(x, 0, 0)], edges2, 'x')
    const pos = Object.fromEntries(out.map((n) => [n.id, n.position]))
    expect(pos.x).toEqual({ x: 3 * step, y: NODE_HEIGHT + BRANCH_GAP })
    expect(pos.k3).toEqual({ x: 4 * step, y: NODE_HEIGHT + BRANCH_GAP })
    expect(overlaps(out)).toBe(false)
  })
})

describe('routing: OTel table default', () => {
  const withTable = (n: ChainNode, table: string): ChainNode => ({ ...n, data: { ...n.data, config: { table } } })

  it('goes to the first sink in output order', () => {
    expect(otelDefaultSink(tree, treeEdges, 'otel_logs')).toBe('k1')
    expect(otelDefaultSink(nodes, chained, 'otel_logs')).toBe('k')
  })

  it('stays with the sink that already holds the table', () => {
    const list = [src, parse, route, map, k1, withTable(k2, 'otel_logs'), k3]
    expect(otelDefaultSink(list, treeEdges, 'otel_logs')).toBe('k2')
  })

  it('is none without a reachable sink', () => {
    expect(otelDefaultSink([src, k1], [], 'otel_logs')).toBeNull()
  })
})

describe('storedViewport', () => {
  it('reads a saved viewport', () => {
    expect(storedViewport('{"x":10,"y":-5,"zoom":0.8}')).toEqual({ x: 10, y: -5, zoom: 0.8 })
  })

  it('is null without one, so the canvas fits the graph', () => {
    for (const c of ['', '{}', 'null', 'oops', '{"x":1,"y":2}', '{"x":1,"y":2,"zoom":0}', null, undefined]) expect(storedViewport(c)).toBeNull()
  })
})

describe('fit options', () => {
  it('never zooms in past 1 and can zoom out far enough for a wide graph', () => {
    expect(FIT_VIEW_OPTIONS.maxZoom).toBe(1)
    expect(FIT_VIEW_OPTIONS.minZoom).toBe(CANVAS_MIN_ZOOM)
    // Eight nodes in a row (about 2900 px with padding) in a 600 px canvas need a zoom of about 0.21.
    const width = 8 * (NODE_WIDTH + NODE_GAP) * (1 + FIT_VIEW_OPTIONS.padding)
    expect(600 / width).toBeGreaterThanOrEqual(CANVAS_MIN_ZOOM)
    expect(CANVAS_MIN_ZOOM).toBeLessThan(0.5)
  })
})
