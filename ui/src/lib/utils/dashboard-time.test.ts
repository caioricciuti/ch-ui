import { describe, expect, it } from 'vitest'
import {
  decodeAbsoluteDashboardRange,
  encodeAbsoluteDashboardRange,
  formatDashboardTimeRangeLabel,
  resolveNamedPreset,
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

  it('formats new duration labels', () => {
    expect(formatDashboardTimeRangeLabel('30m')).toBe('Last 30m')
    expect(formatDashboardTimeRangeLabel('3h')).toBe('Last 3h')
    expect(formatDashboardTimeRangeLabel('12h')).toBe('Last 12h')
  })

  it('formats named preset labels', () => {
    expect(formatDashboardTimeRangeLabel('preset:today')).toBe('Today')
    expect(formatDashboardTimeRangeLabel('preset:yesterday')).toBe('Yesterday')
    expect(formatDashboardTimeRangeLabel('preset:this-week')).toBe('This Week')
    expect(formatDashboardTimeRangeLabel('preset:last-week')).toBe('Last Week')
    expect(formatDashboardTimeRangeLabel('preset:this-month')).toBe('This Month')
    expect(formatDashboardTimeRangeLabel('preset:last-month')).toBe('Last Month')
    expect(formatDashboardTimeRangeLabel('preset:last-3-months')).toBe('Last 3 Months')
    expect(formatDashboardTimeRangeLabel('preset:last-6-months')).toBe('Last 6 Months')
  })

  describe('resolveNamedPreset', () => {
    it('resolves preset:today to absolute range', () => {
      const result = resolveNamedPreset('preset:today')
      expect(result).not.toBeNull()
      const from = new Date(result!.from)
      const to = new Date(result!.to)
      expect(from.getHours() + from.getMinutes() + from.getSeconds()).toBe(0)
      expect(to.getTime()).toBeLessThanOrEqual(Date.now())
      expect(to.getTime()).toBeGreaterThan(from.getTime())
    })

    it('resolves preset:yesterday', () => {
      const result = resolveNamedPreset('preset:yesterday')
      expect(result).not.toBeNull()
      const from = new Date(result!.from)
      const to = new Date(result!.to)
      expect(to.getTime() - from.getTime()).toBe(86400000) // exactly 1 day
    })

    it('resolves preset:this-week', () => {
      const result = resolveNamedPreset('preset:this-week')
      expect(result).not.toBeNull()
      const from = new Date(result!.from)
      expect(from.getDay()).toBe(0) // starts on Sunday
    })

    it('resolves preset:last-week', () => {
      const result = resolveNamedPreset('preset:last-week')
      expect(result).not.toBeNull()
      const from = new Date(result!.from)
      const to = new Date(result!.to)
      expect(from.getDay()).toBe(0)
      expect(to.getDay()).toBe(0)
      expect(to.getTime() - from.getTime()).toBe(604800000) // exactly 7 days
    })

    it('resolves preset:this-month', () => {
      const result = resolveNamedPreset('preset:this-month')
      expect(result).not.toBeNull()
      const from = new Date(result!.from)
      expect(from.getDate()).toBe(1)
    })

    it('resolves preset:last-month', () => {
      const result = resolveNamedPreset('preset:last-month')
      expect(result).not.toBeNull()
      const from = new Date(result!.from)
      const to = new Date(result!.to)
      expect(from.getDate()).toBe(1)
      expect(to.getDate()).toBe(1)
    })

    it('resolves preset:last-3-months and preset:last-6-months', () => {
      const r3 = resolveNamedPreset('preset:last-3-months')
      const r6 = resolveNamedPreset('preset:last-6-months')
      expect(r3).not.toBeNull()
      expect(r6).not.toBeNull()
      expect(new Date(r6!.from).getTime()).toBeLessThan(new Date(r3!.from).getTime())
    })

    it('returns null for unknown presets', () => {
      expect(resolveNamedPreset('preset:unknown')).toBeNull()
      expect(resolveNamedPreset('not-a-preset')).toBeNull()
    })
  })

  describe('toDashboardTimeRangePayload with named presets', () => {
    it('resolves preset:today to absolute payload', () => {
      const result = toDashboardTimeRangePayload('preset:today')
      expect(result.type).toBe('absolute')
      expect(result.from).toBeTruthy()
      expect(result.to).toBeTruthy()
    })

    it('resolves preset:yesterday to absolute payload', () => {
      const result = toDashboardTimeRangePayload('preset:yesterday')
      expect(result.type).toBe('absolute')
    })
  })
})
