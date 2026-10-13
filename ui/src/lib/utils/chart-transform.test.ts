import { describe, it, expect } from 'vitest'
import { computeStat, isDateType, isNumericType, parseTimeValue, pivotSeries, resolveChartColumns, resolveThresholdColor, suggestChart, toUPlotData } from './chart-transform'

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

  it('leaves a gap (null) for NULL and non-numeric y values, never a fake zero', () => {
    const rows = [
      { t: '2026-01-01T00:00:00Z', v: 'n/a' },
      { t: '2026-01-01T00:01:00Z', v: null },
      { t: '2026-01-01T00:02:00Z', v: 0 },
      { t: '2026-01-01T00:03:00Z', v: '7' },
    ]
    const [, y] = toUPlotData(rows, meta, { chartType: 'timeseries', xColumn: 't', yColumns: ['v'] })
    expect(Array.from(y as ArrayLike<number | null>)).toEqual([null, null, 0, 7])
  })

  it('keeps a numeric x of 0 instead of replacing it with the row index', () => {
    const numMeta = [{ name: 'x', type: 'Int32' }, { name: 'v', type: 'UInt8' }]
    const rows = [{ x: 5, v: 1 }, { x: 0, v: 2 }, { x: -3, v: 3 }]
    const [x, y] = toUPlotData(rows, numMeta, { chartType: 'bar', xColumn: 'x', yColumns: ['v'] })
    expect(Array.from(x)).toEqual([-3, 0, 5])
    expect(Array.from(y as ArrayLike<number>)).toEqual([3, 2, 1])
  })

  it('places categorical x values by row index, in row order', () => {
    const catMeta = [{ name: 'k', type: 'String' }, { name: 'v', type: 'UInt8' }]
    const rows = [{ k: '10', v: 1 }, { k: '2', v: 2 }]
    const [x] = toUPlotData(rows, catMeta, { chartType: 'bar', xColumn: 'k', yColumns: ['v'] })
    expect(Array.from(x)).toEqual([0, 1])
  })

  it('drops rows whose time is missing', () => {
    const rows = [{ t: null, v: 1 }, { t: '2026-01-01T00:00:00Z', v: 2 }]
    const [x, y] = toUPlotData(rows, meta, { chartType: 'timeseries', xColumn: 't', yColumns: ['v'] })
    expect(x).toHaveLength(1)
    expect(Array.from(y as ArrayLike<number>)).toEqual([2])
  })
})

describe('parseTimeValue', () => {
  it('reads ISO instants from ClickHouse exactly', () => {
    expect(parseTimeValue('2026-01-01T10:00:00Z')).toBe(Date.UTC(2026, 0, 1, 10))
    expect(parseTimeValue('2026-01-01T10:00:00.250Z')).toBe(Date.UTC(2026, 0, 1, 10, 0, 0, 250))
  })

  it('puts Date values and zone-less DateTimes at local time', () => {
    expect(parseTimeValue('2026-03-05')).toBe(new Date(2026, 2, 5).getTime())
    expect(parseTimeValue('2026-03-05 06:07:08')).toBe(new Date(2026, 2, 5, 6, 7, 8).getTime())
    expect(parseTimeValue('2026-03-05 06:07:08.123456')).toBe(new Date(2026, 2, 5, 6, 7, 8, 123).getTime())
  })

  it('reads epoch numbers as seconds or milliseconds', () => {
    expect(parseTimeValue(1767225600)).toBe(1767225600000)
    expect(parseTimeValue(1767225600000)).toBe(1767225600000)
  })

  it('returns null for empty or unparseable values', () => {
    expect(parseTimeValue(null)).toBeNull()
    expect(parseTimeValue('')).toBeNull()
    expect(parseTimeValue('soon')).toBeNull()
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

describe('panel saved without axes (ISO DateTime and UInt64 from ClickHouse JSON)', () => {
  const meta = [
    { name: 'ts', type: 'DateTime' },
    { name: 'a', type: 'UInt64' },
    { name: 'b', type: 'UInt64' },
  ]
  // ClickHouse JSON may quote UInt64 values.
  const rows = [
    { a: '34', b: '68', ts: '2026-10-12T20:30:00Z' },
    { a: 34, b: 68, ts: '2026-10-12T20:29:00Z' },
  ]

  it('picks the date column for x and the numeric columns for y', () => {
    expect(resolveChartColumns(meta, {})).toEqual({ xColumn: 'ts', yColumns: ['a', 'b'] })
    expect(resolveChartColumns(meta, { xColumn: 'gone', yColumns: ['gone'] })).toEqual({ xColumn: 'ts', yColumns: ['a', 'b'] })
    expect(resolveChartColumns(meta, { xColumn: 'ts', yColumns: ['b'] })).toEqual({ xColumn: 'ts', yColumns: ['b'] })
  })

  it('plots every row with finite x values and numeric y values', () => {
    const out = toUPlotData(rows, meta, { chartType: 'timeseries', legendPosition: 'bottom' })
    expect(out).toHaveLength(3)
    const start = Date.UTC(2026, 9, 12, 20, 29) / 1000
    expect(Array.from(out[0])).toEqual([start, start + 60])
    expect(Array.from(out[1] as ArrayLike<number>)).toEqual([34, 34])
    expect(Array.from(out[2] as ArrayLike<number>)).toEqual([68, 68])
  })
})

describe('suggestChart', () => {
  it('suggests a stat for one row with a single numeric column', () => {
    expect(suggestChart([{ name: 'total', type: 'UInt64' }], 1)).toEqual({ chartType: 'stat', statField: 'total' })
    expect(suggestChart([{ name: 'db', type: 'String' }, { name: 'n', type: 'UInt64' }], 1))
      .toEqual({ chartType: 'stat', statField: 'n' })
  })

  it('suggests a time series for a date column plus numeric columns', () => {
    const meta = [
      { name: 'host', type: 'String' },
      { name: 'day', type: 'Date' },
      { name: 'hits', type: 'UInt64' },
      { name: 'bytes', type: 'Nullable(Float64)' },
    ]
    expect(suggestChart(meta, 30)).toEqual({ chartType: 'timeseries', xColumn: 'day', yColumns: ['hits', 'bytes'] })
  })

  it('suggests a bar chart for text plus numeric columns', () => {
    const meta = [{ name: 'database', type: 'LowCardinality(String)' }, { name: 'tables', type: 'UInt64' }]
    expect(suggestChart(meta, 5)).toEqual({ chartType: 'bar', xColumn: 'database', yColumns: ['tables'] })
  })

  it('falls back to a table when there is no numeric column', () => {
    expect(suggestChart([{ name: 'name', type: 'String' }, { name: 'at', type: 'DateTime' }], 10)).toEqual({ chartType: 'table' })
    expect(suggestChart([], 0)).toEqual({ chartType: 'table' })
  })

  it('never suggests a pie', () => {
    const shapes = [
      [{ name: 'k', type: 'String' }, { name: 'v', type: 'UInt8' }],
      [{ name: 'a', type: 'Int32' }, { name: 'b', type: 'Int32' }],
      [{ name: 'v', type: 'Float64' }],
    ]
    for (const meta of shapes) {
      for (const rows of [1, 3, 500]) expect(suggestChart(meta, rows).chartType).not.toBe('pie')
    }
  })

  it('picks the text column for x on new charts, while saved panels keep the first column', () => {
    const meta = [{ name: 'n', type: 'UInt32' }, { name: 'label', type: 'String' }, { name: 'v', type: 'Float64' }]
    expect(suggestChart(meta, 4)).toEqual({ chartType: 'bar', xColumn: 'label', yColumns: ['n', 'v'] })
    // A saved panel without axes renders as before: first column as x.
    expect(resolveChartColumns(meta, {})).toEqual({ xColumn: 'n', yColumns: ['v'] })
  })
})

describe('pivotSeries', () => {
  const meta = [
    { name: 'day', type: 'Date' },
    { name: 'host', type: 'String' },
    { name: 'hits', type: 'UInt64' },
  ]

  it('turns long rows into one column per series value, with null gaps', () => {
    const rows = [
      { day: '2026-01-01', host: 'a', hits: 1 },
      { day: '2026-01-01', host: 'b', hits: 2 },
      { day: '2026-01-02', host: 'a', hits: 3 },
    ]
    const out = pivotSeries(rows, meta, 'day', 'hits', 'host')
    expect(out.yColumns).toEqual(['a', 'b'])
    expect(out.rows).toEqual([
      { day: '2026-01-01', a: 1, b: 2 },
      { day: '2026-01-02', a: 3, b: null },
    ])
    expect(out.meta.map(m => m.name)).toEqual(['day', 'a', 'b'])
    expect(out.dropped).toBe(0)
  })

  it('keeps the largest series up to the cap and counts the rest', () => {
    const rows = Array.from({ length: 12 }, (_, i) => ({ day: '2026-01-01', host: `h${i}`, hits: i + 1 }))
    const out = pivotSeries(rows, meta, 'day', 'hits', 'host', 10)
    expect(out.yColumns).toHaveLength(10)
    expect(out.yColumns).not.toContain('h0')
    expect(out.yColumns).not.toContain('h1')
    expect(out.dropped).toBe(2)
  })

  it('groups numeric x values by number and sums repeats', () => {
    const numMeta = [{ name: 'bucket', type: 'UInt32' }, { name: 'kind', type: 'String' }, { name: 'n', type: 'UInt64' }]
    const rows = [
      { bucket: '10', kind: 'x', n: '4' },
      { bucket: 10, kind: 'x', n: 1 },
      { bucket: 20, kind: 'y', n: 2 },
    ]
    const out = pivotSeries(rows, numMeta, 'bucket', 'n', 'kind')
    expect(out.rows).toEqual([
      { bucket: '10', x: 5, y: null },
      { bucket: 20, x: null, y: 2 },
    ])
    const [x, xs, ys] = toUPlotData(out.rows, out.meta, { chartType: 'bar', xColumn: 'bucket', yColumns: out.yColumns })
    expect(Array.from(x)).toEqual([10, 20])
    expect(Array.from(xs as ArrayLike<number | null>)).toEqual([5, null])
    expect(Array.from(ys as ArrayLike<number | null>)).toEqual([null, 2])
  })

  it('labels NULL series values', () => {
    const out = pivotSeries([{ day: '2026-01-01', host: null, hits: 1 }], meta, 'day', 'hits', 'host')
    expect(out.yColumns).toEqual(['NULL'])
  })
})
