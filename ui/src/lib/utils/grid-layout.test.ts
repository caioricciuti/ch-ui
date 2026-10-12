import { describe, expect, it } from 'vitest'
import { COLS, compact, nextFreeRow, stackLayout } from './grid-layout'

describe('stackLayout', () => {
  it('puts panels in one full-width column in reading order', () => {
    const out = stackLayout([
      { id: 'b', x: 6, y: 0, w: 6, h: 3 },
      { id: 'c', x: 0, y: 4, w: 12, h: 2 },
      { id: 'a', x: 0, y: 0, w: 6, h: 4 },
    ])
    expect(out).toEqual([
      { id: 'a', x: 0, y: 0, w: COLS, h: 4 },
      { id: 'b', x: 0, y: 4, w: COLS, h: 3 },
      { id: 'c', x: 0, y: 7, w: COLS, h: 2 },
    ])
  })
})

describe('nextFreeRow', () => {
  it('is below the lowest panel, and 0 for an empty dashboard', () => {
    expect(nextFreeRow([])).toBe(0)
    expect(nextFreeRow([{ y: 0, h: 4 }, { y: 2, h: 6 }])).toBe(8)
  })

  it('places a new panel where compact keeps it clear of the others', () => {
    const items = [{ id: 'a', x: 0, y: 0, w: 6, h: 4 }, { id: 'b', x: 6, y: 0, w: 6, h: 4 }]
    const added = { id: 'new', x: 0, y: nextFreeRow(items), w: 6, h: 4 }
    const out = compact([...items, added])
    expect(out.find(i => i.id === 'new')).toEqual({ id: 'new', x: 0, y: 4, w: 6, h: 4 })
  })
})
