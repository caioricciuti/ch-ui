import { describe, it, expect } from 'vitest'
import { DEFAULT_COLORS, LEGACY_DEFAULT_COLORS, seriesColors, setSeriesColor } from './chart-transform'

function luminance(hex: string): number {
  const [r, g, b] = [1, 3, 5].map((i) => {
    const c = parseInt(hex.slice(i, i + 2), 16) / 255
    return c <= 0.04045 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4
  })
  return 0.2126 * r + 0.7152 * g + 0.0722 * b
}

function contrast(a: string, b: string): number {
  const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x)
  return (hi + 0.05) / (lo + 0.05)
}

// Chart backgrounds: surface and canvas in the light and dark themes (app.css).
const BACKGROUNDS = { lightSurface: '#ffffff', lightCanvas: '#f6f6f7', darkSurface: '#0a0a0a', darkCanvas: '#000000' }

describe('chart palette', () => {
  it('has 8 distinct hex colors, orange first', () => {
    expect(DEFAULT_COLORS).toHaveLength(8)
    expect(new Set(DEFAULT_COLORS.map((c) => c.toLowerCase())).size).toBe(8)
    for (const c of DEFAULT_COLORS) expect(c).toMatch(/^#[0-9a-f]{6}$/i)
    expect(DEFAULT_COLORS[0]).toBe('#ea580c')
  })

  for (const [name, bg] of Object.entries(BACKGROUNDS)) {
    it(`keeps every color at 3:1 or more on ${name}`, () => {
      for (const c of DEFAULT_COLORS) expect(contrast(c, bg), c).toBeGreaterThanOrEqual(3)
    })
  }
})

describe('seriesColors', () => {
  it('falls back to the default palette when a panel has no colors', () => {
    expect(seriesColors(undefined)).toBe(DEFAULT_COLORS)
    expect(seriesColors([])).toBe(DEFAULT_COLORS)
  })

  it('replaces the untouched legacy default, in any case', () => {
    expect(seriesColors([...LEGACY_DEFAULT_COLORS])).toBe(DEFAULT_COLORS)
    expect(seriesColors(LEGACY_DEFAULT_COLORS.map((c) => c.toLowerCase()))).toBe(DEFAULT_COLORS)
  })

  it('fills unpicked slots from the default palette', () => {
    expect(seriesColors(['', '#123456'])).toEqual([DEFAULT_COLORS[0], '#123456', ...DEFAULT_COLORS.slice(2)])
    expect(seriesColors(['#000000'])).toEqual(['#000000', ...DEFAULT_COLORS.slice(1)])
  })

  it('keeps a changed legacy list as the user left it', () => {
    const custom = [...LEGACY_DEFAULT_COLORS]
    custom[1] = '#123456'
    expect(seriesColors(custom)).toEqual(custom)
  })
})

describe('setSeriesColor', () => {
  it('saves only the picked slot', () => {
    expect(setSeriesColor(undefined, 2, '#123456')).toEqual(['', '', '#123456'])
    expect(setSeriesColor([...LEGACY_DEFAULT_COLORS], 0, '#123456')).toEqual(['#123456'])
  })

  it('clears a slot set back to its default, and saves nothing when no picks remain', () => {
    expect(setSeriesColor(['', '#123456'], 1, DEFAULT_COLORS[1].toUpperCase())).toBeUndefined()
    expect(setSeriesColor(['#123456', '#abcdef'], 1, DEFAULT_COLORS[1])).toEqual(['#123456'])
    expect(setSeriesColor(undefined, 0, DEFAULT_COLORS[0])).toBeUndefined()
  })
})
