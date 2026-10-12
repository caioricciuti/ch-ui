import { describe, it, expect } from 'vitest'
import { computeStat, isDateType, isNumericType, resolveThresholdColor, toUPlotData } from './chart-transform'

describe('column type detection', () => {
  it('recognises date and datetime types, nullable or not', () => {
    expect(isDateType('Date')).toBe(true)
    expect(isDateType("DateTime64(3, 'UTC')")).toBe(true)
    expect(isDateType('Nullable(DateTime)')).toBe(true)
    expect(isDateType('String')).toBe(false)
  })

  it('recognises numeric types, nullable or parametric', () => {
    expect(isNumericType('UInt64')).toBe(true)
    expect(isNumericType('Nullable(Float32)')).toBe(true)
    expect(isNumericType('Decimal(10, 2)')).toBe(true)
    expect(isNumericType('String')).toBe(false)
  })
})

describe('toUPlotData', () => {
  const meta = [
    { name: 't', type: 'DateTime' },
    { name: 'v', type: 'UInt64' },
  ]

  it('returns an empty x series when there is nothing to plot', () => {
    const out = toUPlotData([], meta, { chartType: 'timeseries', xColumn: 't', yColumns: ['v'] })
    expect(out).toHaveLength(1)
    expect(out[0]).toHaveLength(0)
  })

  it('sorts rows by time and converts timestamps to seconds', () => {
    const rows = [
      { t: '2026-01-01T00:01:00Z', v: 2 },
      { t: '2026-01-01T00:00:00Z', v: 1 },
    ]
    const [x, y] = toUPlotData(rows, meta, { chartType: 'timeseries', xColumn: 't', yColumns: ['v'] })
    const start = Date.parse('2026-01-01T00:00:00Z') / 1000
    expect(Array.from(x)).toEqual([start, start + 60])
    expect(Array.from(y as ArrayLike<number>)).toEqual([1, 2])
  })

  it('treats non-numeric y values as zero', () => {
    const rows = [{ t: '2026-01-01T00:00:00Z', v: 'n/a' }]
    const [, y] = toUPlotData(rows, meta, { chartType: 'timeseries', xColumn: 't', yColumns: ['v'] })
    expect(Array.from(y as ArrayLike<number>)).toEqual([0])
  })
})

describe('resolveThresholdColor', () => {
  const thresholds = [
    { value: 90, color: 'red' },
    { value: -Infinity, color: 'green' },
    { value: 80, color: 'amber' },
  ]

  it('picks the highest threshold the value reaches, regardless of input order', () => {
    expect(resolveThresholdColor(10, thresholds)).toBe('green')
    expect(resolveThresholdColor(80, thresholds)).toBe('amber')
    expect(resolveThresholdColor(95, thresholds)).toBe('red')
  })
})

describe('computeStat', () => {
  const meta = [
    { name: 'label', type: 'String' },
    { name: 'n', type: 'UInt32' },
  ]
  const rows = [
    { label: 'a', n: 10 },
    { label: 'b', n: 30 },
    { label: 'c', n: 20 },
  ]

  it('defaults to the last value of the first numeric column', () => {
    const s = computeStat(rows, meta, {})
    expect(s.field).toBe('n')
    expect(s.raw).toBe(20)
    expect(s.value).toBe('20')
  })

  it('applies the chosen calculation', () => {
    expect(computeStat(rows, meta, { statCalculation: 'sum' }).raw).toBe(60)
    expect(computeStat(rows, meta, { statCalculation: 'max' }).raw).toBe(30)
    expect(computeStat(rows, meta, { statCalculation: 'range' }).raw).toBe(20)
    expect(computeStat(rows, meta, { statCalculation: 'count' }).raw).toBe(3)
  })

  it('formats units into value and suffix', () => {
    const pct = computeStat([{ n: 12.345 }], [{ name: 'n', type: 'Float64' }], { statUnit: 'percent' })
    expect(pct.value).toBe('12.3')
    expect(pct.suffix).toBe('%')

    const bytes = computeStat([{ n: 2048 }], [{ name: 'n', type: 'UInt64' }], { statUnit: 'bytes', statDecimals: 0 })
    expect(bytes.value).toBe('2')
    expect(bytes.suffix).toBe('KB')

    const ms = computeStat([{ n: 250 }], [{ name: 'n', type: 'UInt64' }], { statUnit: 'durationMs' })
    expect(ms.value).toBe('250')
    expect(ms.suffix).toBe('ms')
  })

  it('falls back to the raw text when the field is not numeric', () => {
    const s = computeStat([{ label: 'ok' }], [{ name: 'label', type: 'String' }], {})
    expect(s.raw).toBeNull()
    expect(s.value).toBe('ok')
  })

  it('returns a placeholder for no rows', () => {
    expect(computeStat([], meta, {}).value).toBe('--')
  })
})
