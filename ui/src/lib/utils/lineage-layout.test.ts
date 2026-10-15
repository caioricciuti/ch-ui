import { describe, expect, it, vi } from 'vitest'

// parseLineageGraph lives in the api module, which loads the toast library.
vi.mock('svelte-sonner', () => ({ toast: { warning: () => {} } }))

import fixture from './__fixtures__/lineage.json?raw'
import { parseLineageGraph } from '../api/lineage'
import type { LineageConfidence, LineageGraph, LineageNodeKind } from '../types/lineage'
import {
  compactCount,
  groupId,
  isGroupId,
  isolatedCount,
  layoutLineage,
  neighborhood,
  placeNodes,
  type LayoutEdge,
  type LayoutNode,
  type LineageLayout,
} from './lineage-layout'

type EdgeSpec = [string, string, LineageConfidence?]

function graphOf(ids: (string | [string, LineageNodeKind])[], edges: EdgeSpec[]): LineageGraph {
  return {
    nodes: ids.map((spec) => {
      const [id, kind] = typeof spec === 'string' ? [spec, 'table' as const] : spec
      const dot = id.indexOf('.')
      return { id, database: id.slice(0, dot), name: id.slice(dot + 1), kind, engine: 'MergeTree', rows: 1, bytes: 1, refresh: null }
    }),
    edges: edges.map(([from, to, confidence]) => ({ from, to, kind: 'reads', confidence: confidence ?? 'system' })),
    meta: { host: '', multi_node: false, generated_at: '', cached: false, node_count: ids.length, warnings: [] },
  }
}

function laidOut(layout: LineageLayout): { nodes: LayoutNode[]; edges: LayoutEdge[] } {
  if (layout.tooLarge) throw new Error(`unexpected tooLarge (${layout.count})`)
  return layout
}

function ids(layout: LineageLayout): string[] {
  return laidOut(layout).nodes.map((n) => n.id).sort()
}

function byId(layout: LineageLayout): Map<string, LayoutNode> {
  return new Map(laidOut(layout).nodes.map((n) => [n.id, n]))
}

/** Pairs of edges between adjacent layers that cross. */
function crossings(layout: LineageLayout): number {
  const { nodes, edges } = laidOut(layout)
  const at = new Map(nodes.map((n) => [n.id, n]))
  const spans = edges
    .map((e) => [at.get(e.from)!, at.get(e.to)!] as const)
    .filter(([a, b]) => b.layer === a.layer + 1)
  let count = 0
  for (let i = 0; i < spans.length; i++) {
    for (let j = i + 1; j < spans.length; j++) {
      const [a1, b1] = spans[i]
      const [a2, b2] = spans[j]
      if (a1.layer !== a2.layer) continue
      if ((a1.y - a2.y) * (b1.y - b2.y) < 0) count++
    }
  }
  return count
}

/** Seeded generator so the benchmark graph is the same every run. */
function rng(seed: number): () => number {
  let s = seed >>> 0
  return () => {
    s = (s * 1664525 + 1013904223) >>> 0
    return s / 2 ** 32
  }
}

function bigGraph(nodeCount: number, edgeCount: number, databases: number, seed = 7): LineageGraph {
  const rand = rng(seed)
  const names: string[] = []
  for (let i = 0; i < nodeCount; i++) names.push(`db${i % databases}.t${i}`)
  const edges: EdgeSpec[] = []
  const seen = new Set<string>()
  while (edges.length < edgeCount) {
    // Mostly forward edges with locality, a few backwards to make cycles.
    const a = Math.floor(rand() * nodeCount)
    const span = 1 + Math.floor(rand() * 40)
    const b = rand() < 0.97 ? Math.min(nodeCount - 1, a + span) : Math.max(0, a - span)
    const key = `${a}:${b}`
    if (a === b || seen.has(key)) continue
    seen.add(key)
    edges.push([names[a], names[b], rand() < 0.3 ? 'parsed' : 'system'])
  }
  return graphOf(names, edges)
}

const chain = graphOf(['a.n1', 'a.n2', 'a.n3', 'a.n4', 'a.n5', 'a.x'], [
  ['a.n1', 'a.n2'],
  ['a.n2', 'a.n3'],
  ['a.n3', 'a.n4'],
  ['a.n4', 'a.n5'],
  ['a.x', 'a.n4'],
])

describe('neighborhood', () => {
  const names = (set: Set<number>) => [...set].map((i) => chain.nodes[i].id).sort()

  it('walks up and down separately', () => {
    expect(names(neighborhood(chain, 'a.n3', 1, 1))).toEqual(['a.n2', 'a.n3', 'a.n4'])
    expect(names(neighborhood(chain, 'a.n3', 2, 0))).toEqual(['a.n1', 'a.n2', 'a.n3'])
    expect(names(neighborhood(chain, 'a.n3', 0, 2))).toEqual(['a.n3', 'a.n4', 'a.n5'])
  })

  it('does not pull in siblings (upstream of a downstream node)', () => {
    expect(names(neighborhood(chain, 'a.n3', 0, 1))).not.toContain('a.x')
  })

  it('is empty for an unknown focus', () => {
    expect(neighborhood(chain, 'a.nope', 2, 2).size).toBe(0)
  })
})

describe('layoutLineage subgraph', () => {
  it('defaults to two hops each way', () => {
    expect(ids(layoutLineage(chain, { focus: 'a.n3' }))).toEqual(['a.n1', 'a.n2', 'a.n3', 'a.n4', 'a.n5'])
    expect(ids(layoutLineage(chain, { focus: 'a.n1' }))).toEqual(['a.n1', 'a.n2', 'a.n3'])
  })

  it('honours separate up and down hop counts', () => {
    expect(ids(layoutLineage(chain, { focus: 'a.n4', up: 1, down: 0 }))).toEqual(['a.n3', 'a.n4', 'a.x'])
  })

  it('shows every node when there is no focus', () => {
    expect(laidOut(layoutLineage(chain)).nodes).toHaveLength(6)
  })

  it('can leave out nodes without edges when there is no focus', () => {
    const g = graphOf(['a.a', 'a.b', 'a.lonely'], [['a.a', 'a.b']])
    expect(isolatedCount(g)).toBe(1)
    expect(ids(layoutLineage(g, { hideIsolated: true }))).toEqual(['a.a', 'a.b'])
    expect(ids(layoutLineage(g, { focus: 'a.lonely', hideIsolated: true }))).toEqual(['a.lonely'])
  })

  it('marks the focus', () => {
    const nodes = byId(layoutLineage(chain, { focus: 'a.n3' }))
    expect(nodes.get('a.n3')?.focus).toBe(true)
    expect(nodes.get('a.n2')?.focus).toBe(false)
  })

  it('filters kinds but always keeps the focus', () => {
    const graph = parseLineageGraph(JSON.parse(fixture))
    const kinds = new Set<LineageNodeKind>(['table'])
    const layout = layoutLineage(graph, { focus: 'shop.orders_mv', kinds, collapse: false })
    const shown = ids(layout)
    expect(shown).toContain('shop.orders_mv')
    expect(shown).not.toContain('shop.customers_dict')
    expect(shown).not.toContain('shop.legacy_orders')
  })
})

describe('layoutLineage collapse', () => {
  const graph = parseLineageGraph(JSON.parse(fixture))

  it('folds other databases into one node with a count', () => {
    const layout = layoutLineage(graph, { focus: 'shop.orders' })
    const nodes = byId(layout)
    const group = nodes.get(groupId('analytics'))
    expect(group?.group).toEqual({ database: 'analytics', count: 2 })
    expect(group?.node).toBeNull()
    expect(nodes.has('analytics.daily_revenue')).toBe(false)
    expect(nodes.has('shop.orders_mv')).toBe(true)
    // Nodes without a database (external) are never folded.
    expect(isGroupId('shop.orders')).toBe(false)
  })

  it('never folds external nodes, even beside folded databases', () => {
    const ext = "external:url('https://api.acme.io/fx.csv')"
    const nodes = byId(layoutLineage(graph, { focus: 'analytics.daily_revenue_rmv' }))
    expect(nodes.get(ext)?.node?.kind).toBe('external')
    expect(nodes.get(ext)?.group).toBeNull()
    expect(nodes.get(groupId('shop'))?.group).toEqual({ database: 'shop', count: 2 })
    expect(nodes.has(groupId(''))).toBe(false)
  })

  it('drops edges inside a folded database and keeps the ones into it', () => {
    const { edges } = laidOut(layoutLineage(graph, { focus: 'shop.orders' }))
    const intoGroup = edges.filter((e) => e.to === groupId('analytics') || e.from === groupId('analytics'))
    expect(intoGroup).toEqual([
      { id: 'shop.orders->db:analytics', from: 'shop.orders', to: 'db:analytics', kinds: ['reads'], confidence: 'system', count: 1, back: false },
    ])
  })

  it('shows an expanded database node by node', () => {
    const nodes = byId(layoutLineage(graph, { focus: 'shop.orders', expanded: new Set(['analytics']) }))
    expect(nodes.has(groupId('analytics'))).toBe(false)
    expect(nodes.has('analytics.daily_revenue')).toBe(true)
  })

  it('aggregates parallel edges: count, kinds, and system wins over parsed', () => {
    const g = graphOf(['a.f', 'b.x', 'b.y', 'c.z', 'c.w'], [
      ['a.f', 'b.x', 'parsed'],
      ['a.f', 'b.y', 'system'],
      ['c.z', 'a.f', 'parsed'],
      ['c.w', 'a.f', 'parsed'],
    ])
    g.edges[1].kind = 'writes'
    const { nodes, edges } = laidOut(layoutLineage(g, { focus: 'a.f' }))
    expect(nodes.map((n) => n.id).sort()).toEqual(['a.f', 'db:b', 'db:c'])
    const out = edges.find((e) => e.to === 'db:b')
    expect(out).toMatchObject({ from: 'a.f', count: 2, kinds: ['reads', 'writes'], confidence: 'system' })
    const inc = edges.find((e) => e.from === 'db:c')
    expect(inc).toMatchObject({ to: 'a.f', count: 2, confidence: 'parsed' })
  })

  it('does not collapse without a focus or when turned off', () => {
    expect(ids(layoutLineage(graph))).toContain('analytics.daily_revenue')
    expect(ids(layoutLineage(graph, { focus: 'shop.orders', collapse: false }))).toContain('analytics.daily_revenue')
  })
})

describe('cycles and layers', () => {
  it('marks the edge that closes a cycle and keeps it', () => {
    const g = graphOf(['a.s', 'a.a', 'a.b', 'a.c'], [
      ['a.s', 'a.a'],
      ['a.a', 'a.b'],
      ['a.b', 'a.c'],
      ['a.c', 'a.a'],
    ])
    const layout = layoutLineage(g)
    const { edges } = laidOut(layout)
    expect(edges).toHaveLength(4)
    expect(edges.filter((e) => e.back).map((e) => e.id)).toEqual(['a.c->a.a'])
    const nodes = byId(layout)
    expect(['a.s', 'a.a', 'a.b', 'a.c'].map((id) => nodes.get(id)?.layer)).toEqual([0, 1, 2, 3])
  })

  it('handles a cycle with no entry and a self loop', () => {
    const g = graphOf(['a.a', 'a.b', 'a.c'], [
      ['a.a', 'a.b'],
      ['a.b', 'a.a'],
      ['a.c', 'a.c'],
    ])
    const { nodes, edges } = laidOut(layoutLineage(g))
    expect(edges.filter((e) => e.back)).toHaveLength(2)
    expect(edges.find((e) => e.id === 'a.c->a.c')?.back).toBe(true)
    expect(nodes.every((n) => Number.isFinite(n.x) && Number.isFinite(n.y))).toBe(true)
  })

  it('layers by longest path', () => {
    const g = graphOf(['a.a', 'a.b', 'a.c', 'a.d'], [
      ['a.a', 'a.b'],
      ['a.b', 'a.c'],
      ['a.a', 'a.c'],
      ['a.d', 'a.c'],
    ])
    const nodes = byId(layoutLineage(g))
    expect(nodes.get('a.c')?.layer).toBe(2)
    expect(nodes.get('a.d')?.layer).toBe(0)
    expect(nodes.get('a.c')!.x).toBeGreaterThan(nodes.get('a.b')!.x)
  })

  it('never overlaps two nodes', () => {
    const { nodes } = laidOut(layoutLineage(bigGraph(300, 450, 5)))
    const spots = new Set(nodes.map((n) => `${n.x}:${n.y}`))
    expect(spots.size).toBe(nodes.length)
  })
})

describe('ordering', () => {
  it('untangles crossings the first-seen order leaves', () => {
    // Discovery order puts a, b, c over q, p, where c -> q crosses b -> p.
    // The upward sweep pulls c next to a.
    const g = graphOf(['a.a', 'a.b', 'a.c', 'a.p', 'a.q'], [
      ['a.a', 'a.q'],
      ['a.b', 'a.p'],
      ['a.c', 'a.q'],
    ])
    const layout = layoutLineage(g)
    expect(crossings(layout)).toBe(0)
    const nodes = byId(layout)
    expect(nodes.get('a.c')!.y).toBeLessThan(nodes.get('a.b')!.y)
  })

  it('is stable: the same picture whatever the input order', () => {
    const g = bigGraph(200, 320, 4, 11)
    const shuffled: LineageGraph = {
      ...g,
      nodes: [...g.nodes].reverse(),
      edges: [...g.edges].sort((p, q) => (p.to < q.to ? -1 : p.to > q.to ? 1 : 0)),
    }
    const a = byId(layoutLineage(g))
    const b = byId(layoutLineage(shuffled))
    for (const [id, n] of a) {
      expect(b.get(id)).toMatchObject({ x: n.x, y: n.y, layer: n.layer })
    }
    // And a second run on the same object gives the same answer.
    expect(layoutLineage(g)).toEqual(layoutLineage(g))
  })

  it('placeNodes centers each layer around zero', () => {
    const edges = [{ a: 0, b: 1, back: false }, { a: 0, b: 2, back: false }]
    const p = placeNodes(3, ['r', 'x', 'y'], edges)
    expect(p.y[0]).toBe(0)
    expect(p.y[1] + p.y[2]).toBe(0)
  })
})

describe('tooLarge cap', () => {
  const big = bigGraph(500, 700, 1)

  it('refuses to lay out more than the cap', () => {
    const layout = layoutLineage(big)
    expect(layout).toEqual({ tooLarge: true, count: 500, total: 500 })
  })

  it('counts folded databases as one node', () => {
    const many = bigGraph(500, 700, 250)
    const focus = many.nodes[250].id
    const layout = layoutLineage(many, { focus, up: 4, down: 4 })
    expect(layout.tooLarge).toBe(false)
  })

  it('takes a custom cap', () => {
    expect(layoutLineage(chain, { maxNodes: 3 }).tooLarge).toBe(true)
    expect(layoutLineage(chain, { focus: 'a.n1', maxNodes: 3 }).tooLarge).toBe(false)
  })
})

describe('compactCount', () => {
  it('formats sizes for a card', () => {
    expect(compactCount(null)).toBe('')
    expect(compactCount(950)).toBe('950')
    expect(compactCount(1500)).toBe('1.5K')
    expect(compactCount(48_000_000)).toBe('48M')
    expect(compactCount(250_000_000_000)).toBe('250B')
  })
})

describe('performance', () => {
  function time(fn: () => void, runs: number): number {
    fn() // warm up the JIT
    const times: number[] = []
    for (let i = 0; i < runs; i++) {
      const t0 = performance.now()
      fn()
      times.push(performance.now() - t0)
    }
    times.sort((p, q) => p - q)
    return times[Math.floor(times.length / 2)]
  }

  it('2,000 nodes / 3,000 edges, focus + 2 hops, collapsed: under 50 ms', () => {
    const graph = bigGraph(2000, 3000, 20)
    const focus = graph.nodes[1000].id
    // Cold: a fresh response, adjacency index built inside the timed call.
    const t0 = performance.now()
    const first = layoutLineage(graph, { focus })
    const cold = performance.now() - t0
    expect(first.tooLarge).toBe(false)
    const warm = time(() => {
      layoutLineage(graph, { focus })
    }, 20)
    console.info(`[lineage perf] 2000/3000 focus+2 collapsed: cold ${cold.toFixed(2)} ms, warm median ${warm.toFixed(2)} ms`)
    expect(cold).toBeLessThan(50)
    expect(warm).toBeLessThan(50)
  })

  it('full layout of a 400-node graph: under 100 ms', () => {
    const graph = bigGraph(400, 600, 4, 3)
    const t0 = performance.now()
    const first = layoutLineage(graph)
    const cold = performance.now() - t0
    expect(laidOut(first).nodes).toHaveLength(400)
    const warm = time(() => {
      layoutLineage(graph)
    }, 10)
    console.info(`[lineage perf] 400/600 full layout: cold ${cold.toFixed(2)} ms, warm median ${warm.toFixed(2)} ms`)
    expect(cold).toBeLessThan(100)
    expect(warm).toBeLessThan(100)
  })
})
