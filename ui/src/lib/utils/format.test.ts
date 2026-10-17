import { describe, it, expect } from 'vitest'
import { formatCompactNumber, formatDate, formatRelativeTime } from './format'

describe('formatCompactNumber', () => {
  it('leaves small counts alone', () => {
    expect(formatCompactNumber(0)).toBe('0')
    expect(formatCompactNumber(1)).toBe('1')
    expect(formatCompactNumber(999)).toBe('999')
  })

  it('scales large counts with a short suffix', () => {
    expect(formatCompactNumber(1_500)).toBe('1.50K')
    expect(formatCompactNumber(15_990_000)).toBe('15.99M')
    expect(formatCompactNumber(3_020_000_000)).toBe('3.02B')
    expect(formatCompactNumber(1_250_000_000_000)).toBe('1.25T')
  })

  it('handles fractional throughput values', () => {
    expect(formatCompactNumber(2.5)).toBe('2.50')
    expect(formatCompactNumber(3_020_500.5)).toBe('3.02M')
  })
})

describe('formatDate', () => {
  it('reads a bare "YYYY-MM-DD HH:MM:SS" value as UTC', () => {
    const utc = new Date(Date.UTC(2026, 9, 17, 20, 26, 45))
    expect(formatDate('2026-10-17 20:26:45')).toBe(utc.toLocaleString())
    expect(formatDate('2026-10-17 20:26:45.123456')).toBe(utc.toLocaleString())
    expect(formatDate('2026-10-17T20:26:45')).toBe(utc.toLocaleString())
  })

  it('leaves zoned values to Date', () => {
    const zoned = new Date('2026-10-17T20:26:45Z')
    expect(formatDate('2026-10-17T20:26:45Z')).toBe(zoned.toLocaleString())
    expect(formatDate('2026-10-17T22:26:45+02:00')).toBe(zoned.toLocaleString())
  })

  it('keeps a value it cannot parse', () => {
    expect(formatDate('yesterday')).toBe('yesterday')
    expect(formatDate('')).toBe('—')
  })
})

describe('formatRelativeTime', () => {
  it('treats a bare UTC value written just now as "just now"', () => {
    const now = new Date()
    const bare = now.toISOString().slice(0, 19).replace('T', ' ')
    expect(formatRelativeTime(bare)).toBe('just now')
  })
})
