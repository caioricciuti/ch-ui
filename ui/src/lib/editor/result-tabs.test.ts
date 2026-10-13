import { describe, expect, it } from 'vitest'
import {
  EMPTY_TAB_RESULTS,
  MAX_PERSISTED_ROWS,
  MAX_PINNED_PER_TAB,
  MAX_RESULTS_PER_TAB,
  addResult,
  capRows,
  dropUnpinned,
  removeResult,
  replaceActiveResult,
  restorePinned,
  resultLabel,
  serializePinned,
  setActiveResult,
  togglePin,
  type ResultEntry,
  type TabResults,
} from './result-tabs'

let clock = 0
function entry(id: string, over: Partial<ResultEntry> = {}): ResultEntry {
  return {
    id,
    label: id,
    sql: `SELECT '${id}'`,
    meta: [{ name: 'x', type: 'UInt8' }],
    data: [[1]],
    stats: null,
    elapsedMs: 5,
    error: null,
    running: false,
    pinned: false,
    createdAt: ++clock,
    ...over,
  }
}

const ids = (s: TabResults) => s.results.map((r) => r.id)

describe('resultLabel', () => {
  it('numbers the statement and keeps the first keywords', () => {
    expect(resultLabel('select count() from system.tables', 2)).toBe('2 · SELECT count from')
    expect(resultLabel('-- note\n/* x */ SHOW TABLES', 1)).toBe('1 · SHOW TABLES')
    expect(resultLabel("SELECT 'a b c'", null)).toBe('SELECT')
    expect(resultLabel('', 3)).toBe('3 · Query')
  })
})

describe('result list', () => {
  it('replaces only the active result', () => {
    let s = addResult(EMPTY_TAB_RESULTS, entry('a'))
    s = addResult(s, entry('b'))
    s = setActiveResult(s, 'a')
    s = replaceActiveResult(s, entry('c'))
    expect(ids(s)).toEqual(['c', 'b'])
    expect(s.activeResult).toBe(0)
  })

  it('never replaces a pinned result', () => {
    let s = addResult(EMPTY_TAB_RESULTS, entry('a', { pinned: true }))
    s = replaceActiveResult(s, entry('b'))
    expect(ids(s)).toEqual(['a', 'b'])
    expect(s.activeResult).toBe(1)
  })

  it('evicts the oldest unpinned results past the cap', () => {
    let s: TabResults = EMPTY_TAB_RESULTS
    s = addResult(s, entry('p', { pinned: true }))
    for (let i = 0; i < MAX_RESULTS_PER_TAB + 2; i++) s = addResult(s, entry(`r${i}`))
    expect(s.results).toHaveLength(MAX_RESULTS_PER_TAB)
    expect(ids(s)).toContain('p')
    expect(ids(s)).not.toContain('r0')
    expect(ids(s)).not.toContain('r1')
    expect(s.results[s.activeResult].id).toBe(`r${MAX_RESULTS_PER_TAB + 1}`)
  })

  it('a run of several statements keeps only pinned results', () => {
    let s = addResult(EMPTY_TAB_RESULTS, entry('a'))
    s = addResult(s, entry('b', { pinned: true }))
    s = addResult(s, entry('c'))
    s = dropUnpinned(s)
    expect(ids(s)).toEqual(['b'])
    expect(s.activeResult).toBe(0)
  })

  it('keeps the active index right when a result is removed', () => {
    let s = addResult(EMPTY_TAB_RESULTS, entry('a'))
    s = addResult(s, entry('b'))
    s = addResult(s, entry('c'))
    s = removeResult(s, 'a')
    expect(s.results[s.activeResult].id).toBe('c')
    s = removeResult(s, 'c')
    expect(s.results[s.activeResult].id).toBe('b')
    s = removeResult(s, 'b')
    expect(s.activeResult).toBe(-1)
  })

  it(`allows at most ${MAX_PINNED_PER_TAB} pinned results`, () => {
    let s: TabResults = EMPTY_TAB_RESULTS
    for (let i = 0; i < MAX_PINNED_PER_TAB + 1; i++) s = addResult(s, entry(`r${i}`))
    for (let i = 0; i < MAX_PINNED_PER_TAB; i++) {
      const res = togglePin(s, `r${i}`)
      expect(res.outcome).toBe('pinned')
      s = res.state
    }
    expect(togglePin(s, `r${MAX_PINNED_PER_TAB}`).outcome).toBe('limit')
    const off = togglePin(s, 'r0')
    expect(off.outcome).toBe('unpinned')
    expect(off.state.results[0].pinned).toBe(false)
  })

  it('caps rows to the row limit', () => {
    const rows = Array.from({ length: 5 }, (_, i) => [i])
    expect(capRows(rows, 3)).toHaveLength(3)
    expect(capRows(rows, 10)).toBe(rows)
  })
})

describe('pinned persistence', () => {
  it('saves pinned results with up to the row cap and flags dropped rows', () => {
    const big = Array.from({ length: MAX_PERSISTED_ROWS + 10 }, (_, i) => [i])
    let s = addResult(EMPTY_TAB_RESULTS, entry('small', { pinned: true }))
    s = addResult(s, entry('big', { pinned: true, data: big }))
    s = addResult(s, entry('loose'))
    s = addResult(s, entry('running', { pinned: true, running: true }))
    const saved = serializePinned(s)
    expect(saved.map((r) => r.id)).toEqual(['small', 'big'])
    expect(saved[0].rowsDropped).toBe(false)
    expect(saved[1].data).toHaveLength(MAX_PERSISTED_ROWS)
    expect(saved[1].rowsDropped).toBe(true)
    expect(saved[1].savedRowCount).toBe(MAX_PERSISTED_ROWS + 10)

    const back = restorePinned(JSON.parse(JSON.stringify(saved)))
    expect(back.map((r) => r.id)).toEqual(['small', 'big'])
    expect(back[1].rowsDropped).toBe(true)
    expect(back.every((r) => r.pinned && !r.running)).toBe(true)
  })

  it('drops the rows of a result too large to store', () => {
    const wide = Array.from({ length: 200 }, () => ['x'.repeat(10_000)])
    const saved = serializePinned(addResult(EMPTY_TAB_RESULTS, entry('wide', { pinned: true, data: wide })))
    expect(saved[0].data).toEqual([])
    expect(saved[0].rowsDropped).toBe(true)
  })

  it('ignores malformed saved data', () => {
    expect(restorePinned(null)).toEqual([])
    expect(restorePinned({ a: 1 })).toEqual([])
    expect(restorePinned([{ id: 'x' }, { id: 1, label: 'l', sql: '', meta: [], data: [] }])).toEqual([])
    expect(restorePinned([{ id: 'x', label: 'l', sql: 's', meta: [{ name: 'a' }], data: [] }])).toEqual([])
    expect(restorePinned([{ id: 'x', label: 'l', sql: 's', meta: [], data: [[1]] }])).toHaveLength(1)
  })
})
