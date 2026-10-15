import { describe, expect, it } from 'vitest'
import { buildLineageSearch, databaseOf, parseLineageSearch } from './lineage-url'

describe('parseLineageSearch', () => {
  it('reads databases, focus and hops', () => {
    expect(parseLineageSearch('?databases=shop,analytics&focus=shop.orders&hops=3')).toEqual({
      databases: ['shop', 'analytics'],
      focus: 'shop.orders',
      hops: 3,
    })
  })

  it('falls back to defaults and drops blanks and repeats', () => {
    expect(parseLineageSearch('')).toEqual({ databases: [], focus: null, hops: 2 })
    expect(parseLineageSearch('?databases=a,,a, b&hops=9&focus=')).toEqual({ databases: ['a', 'b'], focus: null, hops: 2 })
    expect(parseLineageSearch('?hops=1.5').hops).toBe(2)
  })
})

describe('buildLineageSearch', () => {
  it('round-trips', () => {
    const state = { databases: ['shop', 'analytics'], focus: 'shop.orders', hops: 4 }
    const search = buildLineageSearch(state)
    expect(search).toBe('?databases=shop,analytics&focus=shop.orders&hops=4')
    expect(parseLineageSearch(search)).toEqual(state)
  })

  it('leaves defaults out and keeps unrelated params', () => {
    expect(buildLineageSearch({ databases: [], focus: null, hops: 2 })).toBe('')
    expect(buildLineageSearch({ databases: ['a'], focus: null, hops: 2 }, '?focus=old.x&hops=3&tab=1')).toBe('?tab=1&databases=a')
  })
})

describe('databaseOf', () => {
  it('takes the part before the first dot', () => {
    expect(databaseOf('shop.orders')).toBe('shop')
    expect(databaseOf('shop.orders.v2')).toBe('shop')
    expect(databaseOf('orders')).toBe('')
    expect(databaseOf("external:url('https://x.io/a.csv')")).toBe('')
  })
})
