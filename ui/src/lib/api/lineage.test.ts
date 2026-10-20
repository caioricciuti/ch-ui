import { describe, expect, it, vi } from 'vitest'

vi.mock('svelte-sonner', () => ({ toast: { warning: () => {} } }))

import fixture from '../utils/__fixtures__/lineage.json?raw'
import { ApiError } from './client'
import { lineageFailure, lineageNodeUrl, lineageUrl, parseLineageGraph, parseLineageNodeDetail } from './lineage'
import { LINEAGE_NODE_KINDS } from '../types/lineage'

describe('parseLineageGraph', () => {
  const graph = parseLineageGraph(JSON.parse(fixture))

  it('keeps every node and edge of a well formed response', () => {
    expect(graph.nodes).toHaveLength(18)
    expect(graph.edges).toHaveLength(15)
    expect(new Set(graph.nodes.map((n) => n.kind))).toEqual(new Set(LINEAGE_NODE_KINDS))
    expect(new Set(graph.edges.map((e) => e.confidence))).toEqual(new Set(['system', 'parsed']))
  })

  it('reads numbers sent as strings and keeps unknown sizes null', () => {
    const orders = graph.nodes.find((n) => n.id === 'shop.orders')
    expect(orders?.rows).toBe(48_000_000)
    const view = graph.nodes.find((n) => n.id === 'analytics.revenue_view')
    expect(view?.rows).toBeNull()
    expect(view?.bytes).toBeNull()
  })

  it('reads refresh state and the external target', () => {
    const rmv = graph.nodes.find((n) => n.kind === 'refreshable_mv')
    expect(rmv?.refresh).toEqual({ status: 'Scheduled', last_success: '2026-10-15T06:00:00Z', next: '2026-10-16T06:00:00Z' })
    expect(graph.nodes.find((n) => n.id === 'shop.orders_s3')?.external).toBe('s3://acme-landing/orders/*.parquet')
  })

  it('reads empty refresh times as none', () => {
    const weekly = graph.nodes.find((n) => n.id === 'analytics.weekly_rmv')
    expect(weekly?.refresh).toEqual({ status: 'Disabled', last_success: null, next: null })
  })

  it('keeps external nodes without a database and the edges to them', () => {
    const id = "external:url('https://api.acme.io/fx.csv')"
    const ext = graph.nodes.find((n) => n.id === id)
    expect(ext).toMatchObject({ database: '', kind: 'external', name: "url('https://api.acme.io/fx.csv')", external: "url('https://api.acme.io/fx.csv')" })
    expect(graph.edges.some((e) => e.from === id && e.to === 'analytics.daily_revenue_rmv')).toBe(true)
  })

  it('reads meta', () => {
    expect(graph.meta).toEqual({
      host: 'ch-node-1',
      multi_node: true,
      generated_at: '2026-10-15T09:30:00Z',
      cached: false,
      node_count: 18,
      warnings: ['Could not parse the definition of analytics.revenue_view', '1 object is not visible to you'],
    })
  })

  it('drops malformed entries, duplicates and dangling edges', () => {
    const parsed = parseLineageGraph({
      nodes: [
        { id: 'a.x', database: 'a', name: 'x', kind: 'table' },
        { id: 'a.x', database: 'a', name: 'dup', kind: 'view' },
        { database: 'a', name: 'no id' },
        'junk',
        { id: 'a.y', database: 'a', name: 'y', kind: 'something-new', rows: 'NaN' },
      ],
      edges: [
        { from: 'a.x', to: 'a.y', kind: 'writes', confidence: 'parsed' },
        { from: 'a.x', to: 'a.gone', kind: 'reads', confidence: 'system' },
        { from: 'a.y', to: 'a.x', kind: 'odd', confidence: 'maybe' },
        null,
      ],
    })
    expect(parsed.nodes.map((n) => n.id)).toEqual(['a.x', 'a.y'])
    expect(parsed.nodes[0].name).toBe('x')
    expect(parsed.nodes[1].kind).toBe('table')
    expect(parsed.nodes[1].rows).toBeNull()
    expect(parsed.edges).toEqual([
      { from: 'a.x', to: 'a.y', kind: 'writes', confidence: 'parsed' },
      { from: 'a.y', to: 'a.x', kind: 'reads', confidence: 'system' },
    ])
    expect(parsed.meta.node_count).toBe(2)
    expect(parsed.meta.warnings).toEqual([])
  })

  it('returns an empty graph for an empty or broken body', () => {
    for (const body of [null, {}, 'x', []]) {
      const g = parseLineageGraph(body)
      expect(g.nodes).toEqual([])
      expect(g.edges).toEqual([])
      expect(g.meta.multi_node).toBe(false)
    }
  })
})

describe('parseLineageNodeDetail', () => {
  it('reads the definition and sizes', () => {
    const d = parseLineageNodeDetail({
      id: 'shop.orders',
      database: 'shop',
      name: 'orders',
      kind: 'table',
      engine: 'MergeTree',
      engine_full: 'MergeTree ORDER BY id',
      ddl: 'CREATE TABLE shop.orders (id UInt64) ENGINE = MergeTree ORDER BY id',
      rows: '10',
      bytes: null,
    })
    expect(d.rows).toBe(10)
    expect(d.bytes).toBeNull()
    expect(d.ddl).toContain('CREATE TABLE')
  })
})

describe('lineageNodeUrl', () => {
  it('sends database and name apart, so dots in a database name survive', () => {
    const url = new URL(lineageNodeUrl('team.eu', 'orders.v2'), 'http://x')
    expect(url.pathname).toBe('/api/lineage/node')
    expect(url.searchParams.get('database')).toBe('team.eu')
    expect(url.searchParams.get('name')).toBe('orders.v2')
    expect(url.searchParams.has('id')).toBe(false)
  })
})

describe('lineageFailure', () => {
  it('turns 422 into a prompt to pick databases and keeps the message', () => {
    expect(lineageFailure(new ApiError('More than 50000 objects: choose databases', 422))).toEqual({
      kind: 'pick',
      message: 'More than 50000 objects: choose databases',
    })
  })

  it('names offline tunnels and unreadable metadata', () => {
    expect(lineageFailure(new ApiError('tunnel offline', 503)).kind).toBe('offline')
    expect(lineageFailure(new ApiError('cannot read system.tables', 502)).kind).toBe('unreadable')
  })

  it('falls back to a plain error with human copy', () => {
    expect(lineageFailure(new ApiError('boom', 500))).toEqual({
      kind: 'error',
      message: 'The server hit an unexpected error. Try again. If it keeps happening, check the CH-UI server logs. Details: boom',
    })
    expect(lineageFailure(new Error('Graph is empty'))).toEqual({ kind: 'error', message: 'Graph is empty' })
    expect(lineageFailure('odd')).toEqual({ kind: 'error', message: 'odd' })
  })
})

describe('lineageUrl', () => {
  it('joins databases and adds refresh only when asked', () => {
    expect(lineageUrl(['a', 'b'])).toBe('/api/lineage?databases=a%2Cb')
    expect(lineageUrl(['a'], true)).toBe('/api/lineage?databases=a&refresh=1')
  })
})
