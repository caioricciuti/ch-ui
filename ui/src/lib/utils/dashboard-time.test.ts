import { describe, expect, it } from 'vitest'
import {
  decodeAbsoluteDashboardRange,
  encodeAbsoluteDashboardRange,
  formatDashboardTimeRangeLabel,
  toDashboardTimeRangePayload,
} from './dashboard-time'

describe('dashboard-time', () => {
  it('encodes and decodes absolute ranges', () => {
    const from = '2026-01-01T00:00:00.000Z'
    const to = '2026-01-01T01:00:00.000Z'
    const encoded = encodeAbsoluteDashboardRange(from, to)
    expect(encoded).toBe(`abs:${from}|${to}`)
    expect(decodeAbsoluteDashboardRange(encoded)).toEqual({ from, to })
  })

  it('parses shorthand relative tokens', () => {
    expect(toDashboardTimeRangePayload('5min')).toEqual({
      type: 'relative',
      from: '5m',
      to: 'now',
    })
  })

  it('parses explicit relative ranges', () => {
    expect(toDashboardTimeRangePayload('now-2h to now')).toEqual({
      type: 'relative',
      from: 'now-2h',
      to: 'now',
    })
  })

  it('parses absolute range strings', () => {
    const res = toDashboardTimeRangePayload('2026-01-01T00:00:00Z to 2026-01-01T01:00:00Z')
    expect(res.type).toBe('absolute')
    expect(res.from).toBe('2026-01-01T00:00:00.000Z')
    expect(res.to).toBe('2026-01-01T01:00:00.000Z')
  })

  it('formats common labels', () => {
    expect(formatDashboardTimeRangeLabel('1h')).toBe('Last 1h')
    expect(formatDashboardTimeRangeLabel('7d')).toBe('Last 7d')
  })
})
