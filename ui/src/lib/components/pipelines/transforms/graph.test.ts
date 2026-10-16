import { describe, expect, it } from 'vitest'
import { NODE_GAP, NODE_WIDTH, bridgeDeleted, chainFromSource, connectionProblem, insertIntoEdge, makeRoom, transformsUpTo, type ChainEdge, type ChainNode } from './graph'

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
    expect(transformsUpTo(nodes, chained, 'f')).toEqual({ chain: [parse, filter], error: '' })
    expect(transformsUpTo(nodes, chained, 'p')).toEqual({ chain: [parse], error: '' })
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
    expect(connectionProblem(nodes, chained, 's', 'f')).toMatch(/^S already feeds P\. A pipeline is one chain for now/)
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
