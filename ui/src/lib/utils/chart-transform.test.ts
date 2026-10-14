import { describe, it, expect } from 'vitest'
import type uPlot from 'uplot'
import {
  computeStat, heatmapMatrix, histogramBins, isDateType, isNumericType, parseTimeValue, pivotSeries, plotPoints, resolveChartColumns,
  resolveHistogramColumn, resolveThresholdColor, stackSeries, suggestChart, toScatterData, toUPlotData,
} from './chart-transform'

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

  it('skips NULL, empty and boolean values instead of counting them as 0', () => {
    const m = [{ name: 'n', type: 'Nullable(Float64)' }]
    const rows = [{ n: 5 }, { n: '7' }, { n: null }, { n: '' }, { n: '  ' }, { n: false }]
    expect(computeStat(rows, m, { statCalculation: 'last' }).raw).toBe(7)
    expect(computeStat(rows, m, { statCalculation: 'min' }).raw).toBe(5)
    expect(computeStat(rows, m, { statCalculation: 'mean' }).raw).toBe(6)
    expect(computeStat(rows, m, { statCalculation: 'count' }).raw).toBe(2)
  })

  it('shows the placeholder when a numeric column has no numbers', () => {
    const m = [{ name: 'n', type: 'Nullable(UInt64)' }]
    for (const rows of [[{ n: null }], [{ n: null }, { n: '' }]]) {
      const s = computeStat(rows, m, {})
      expect(s.raw).toBeNull()
      expect(s.value).toBe('--')
    }
    expect(computeStat([{ label: null }], [{ name: 'label', type: 'Nullable(String)' }], {}).value).toBe('--')
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

describe('stackSeries', () => {
  const data = [new Float64Array([1, 2, 3]), [1, 2, 3], [10, null, 30], [100, 200, 300]] as unknown as uPlot.AlignedData

  it('sums each series onto the ones below it', () => {
    const out = stackSeries(data)
    expect(Array.from(out[0])).toEqual([1, 2, 3])
    expect(out[1]).toEqual([1, 2, 3])
    // A null stacks as 0: the band stays closed and adds nothing above it.
    expect(out[2]).toEqual([11, 2, 33])
    expect(out[3]).toEqual([111, 202, 333])
  })

  it('leaves skipped series out of the totals and as they are', () => {
    const out = stackSeries(data, new Set([2]))
    expect(out[2]).toBe(data[2])
    expect(out[3]).toEqual([101, 202, 303])
  })

  it('handles a chart with no points', () => {
    const out = stackSeries([new Float64Array(0), []] as unknown as uPlot.AlignedData)
    expect(out[1]).toEqual([])
  })
})

describe('histogramBins', () => {
  const rows = (values: unknown[]) => values.map(v => ({ v }))

  it('makes equal-width buckets from min to max, the max landing in the last one', () => {
    const h = histogramBins(rows([0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10]), 'v', 5)
    expect(h.edges).toEqual([0, 2, 4, 6, 8, 10])
    expect(h.counts).toEqual([2, 2, 2, 2, 3])
    expect(h.counts.reduce((a, b) => a + b, 0)).toBe(11)
    expect(h.skipped).toBe(0)
  })

  it('puts a value on an inner edge in the bucket that starts there', () => {
    const h = histogramBins(rows([0, 5, 10]), 'v', 2)
    expect(h.edges).toEqual([0, 5, 10])
    expect(h.counts).toEqual([1, 2])
  })

  it('skips and counts NULL, empty and non-numeric values', () => {
    const h = histogramBins(rows([1, null, '', 'abc', undefined, '3', 2]), 'v', 2)
    expect(h.skipped).toBe(4)
    expect(h.counts.reduce((a, b) => a + b, 0)).toBe(3)
  })

  it('gives a single value one bucket centered on it', () => {
    expect(histogramBins(rows([7]), 'v')).toEqual({ edges: [6.5, 7.5], counts: [1], skipped: 0 })
  })

  it('gives all-equal values one bucket', () => {
    expect(histogramBins(rows([3, 3, '3', 3]), 'v', 30)).toEqual({ edges: [2.5, 3.5], counts: [4], skipped: 0 })
  })

  it('returns no buckets when nothing is a number', () => {
    expect(histogramBins(rows([null, 'x']), 'v')).toEqual({ edges: [], counts: [], skipped: 2 })
  })

  it('keeps the bucket count between 1 and 200, 30 by default', () => {
    const many = rows(Array.from({ length: 1000 }, (_, i) => i))
    expect(histogramBins(many, 'v').counts).toHaveLength(30)
    expect(histogramBins(many, 'v', 0).counts).toHaveLength(1)
    expect(histogramBins(many, 'v', 5000).counts).toHaveLength(200)
    expect(histogramBins(many, 'v', Number.NaN).counts).toHaveLength(30)
  })

  it('picks the saved column, else the first numeric one', () => {
    const meta = [{ name: 's', type: 'String' }, { name: 'a', type: 'UInt64' }, { name: 'b', type: 'Float64' }]
    expect(resolveHistogramColumn(meta, { histogramColumn: 'b' })).toBe('b')
    expect(resolveHistogramColumn(meta, { histogramColumn: 'gone' })).toBe('a')
    expect(resolveHistogramColumn([{ name: 's', type: 'String' }], {})).toBeUndefined()
  })
})

describe('toScatterData', () => {
  const meta = [
    { name: 'x', type: 'Float64' },
    { name: 'y', type: 'Float64' },
    { name: 'size', type: 'UInt32' },
    { name: 'kind', type: 'String' },
  ]

  it('sorts points by x and drops rows missing a coordinate', () => {
    const rows = [
      { x: 3, y: 30, size: 1, kind: 'a' },
      { x: 1, y: 10, size: 2, kind: 'b' },
      { x: null, y: 5, size: 3, kind: 'a' },
      { x: 2, y: 'n/a', size: 4, kind: 'a' },
    ]
    const out = toScatterData(rows, meta, { scatterX: 'x', scatterY: 'y', scatterSize: 'size' })!
    expect(Array.from(out.data[0])).toEqual([1, 3])
    expect(out.data[1]).toEqual([10, 30])
    expect(out.sizes).toEqual([2, 1])
    expect(out.labels).toEqual(['y'])
    expect(out.total).toBe(2)
  })

  it('splits points into one series per group, null elsewhere', () => {
    const rows = [
      { x: 1, y: 10, kind: 'a' },
      { x: 2, y: 20, kind: 'b' },
      { x: 3, y: 30, kind: 'a' },
    ]
    const out = toScatterData(rows, meta, { scatterX: 'x', scatterY: 'y', scatterSeries: 'kind' })!
    expect(out.labels).toEqual(['a', 'b'])
    expect(out.data[1]).toEqual([10, null, 30])
    expect(out.data[2]).toEqual([null, 20, null])
    expect(out.sizes).toBeNull()
  })

  it('caps the points and counts the rest', () => {
    const rows = Array.from({ length: 50 }, (_, i) => ({ x: i, y: i }))
    const out = toScatterData(rows, meta, { scatterX: 'x', scatterY: 'y' }, 20)!
    expect(out.data[0]).toHaveLength(20)
    expect(out.total).toBe(50)
  })

  it('picks two numeric columns when none are saved, and needs two', () => {
    const out = toScatterData([{ x: 1, y: 2 }], meta, {})!
    expect([out.xColumn, out.yColumn]).toEqual(['x', 'y'])
    expect(toScatterData([], [{ name: 'x', type: 'Float64' }], {})).toBeNull()
  })
})

describe('heatmapMatrix', () => {
  const meta = [
    { name: 'day', type: 'String' },
    { name: 'hour', type: 'UInt8' },
    { name: 'n', type: 'UInt64' },
  ]

  it('sums the value per cell, numbers ascending, text in first-seen order', () => {
    const rows = [
      { day: 'Tue', hour: 2, n: 5 },
      { day: 'Mon', hour: 1, n: 1 },
      { day: 'Mon', hour: 1, n: 2 },
      { day: 'Tue', hour: 1, n: 4 },
    ]
    const m = heatmapMatrix(rows, meta, { heatmapX: 'day', heatmapY: 'hour', heatmapValue: 'n' })!
    expect(m.x).toEqual(['Tue', 'Mon'])
    expect(m.y).toEqual(['1', '2'])
    expect(m.values).toEqual([
      [4, 3],
      [5, null],
    ])
    expect([m.min, m.max]).toEqual([3, 5])
    expect(m.yAscending).toBe(true)
    expect(m.merged).toBe(false)
  })

  it('counts rows when there is no numeric value column', () => {
    const textMeta = [{ name: 'a', type: 'String' }, { name: 'b', type: 'String' }]
    const rows = [{ a: 'x', b: 'p' }, { a: 'x', b: 'p' }, { a: 'y', b: 'q' }]
    const m = heatmapMatrix(rows, textMeta, {})!
    expect(m.valueColumn).toBeUndefined()
    expect(m.values).toEqual([
      [2, null],
      [null, 1],
    ])
  })

  it('merges neighbouring values beyond the cap and sums their cells', () => {
    const rows = Array.from({ length: 10 }, (_, i) => ({ day: 'Mon', hour: i, n: 1 }))
    const m = heatmapMatrix(rows, meta, { heatmapX: 'hour', heatmapY: 'day', heatmapValue: 'n' }, 4)!
    // 10 values, 3 per bucket: 0-2, 3-5, 6-8, 9.
    expect(m.x).toEqual(['0 to 2', '3 to 5', '6 to 8', '9'])
    expect(m.values).toEqual([[3, 3, 3, 1]])
    expect(m.merged).toBe(true)
  })

  it('keeps at most the cap on each axis', () => {
    const rows = Array.from({ length: 1000 }, (_, i) => ({ day: `d${i % 500}`, hour: i, n: 1 }))
    const m = heatmapMatrix(rows, meta, { heatmapX: 'hour', heatmapY: 'day', heatmapValue: 'n' })!
    expect(m.x.length).toBeLessThanOrEqual(200)
    expect(m.y.length).toBeLessThanOrEqual(200)
    const total = m.values.flat().reduce<number>((a, v) => a + (v ?? 0), 0)
    expect(total).toBe(1000)
  })

  it('skips rows whose value is not a number', () => {
    const rows = [{ day: 'Mon', hour: 1, n: null }, { day: 'Mon', hour: 1, n: 2 }]
    const m = heatmapMatrix(rows, meta, { heatmapX: 'day', heatmapY: 'hour', heatmapValue: 'n' })!
    expect(m.values).toEqual([[2]])
  })
})

describe('click targets', () => {
  it('plotPoints lists rows in plotted order', () => {
    const meta = [{ name: 'n', type: 'UInt32' }, { name: 'v', type: 'Float64' }]
    const rows = [{ n: 3, v: 1 }, { n: null, v: 2 }, { n: 1, v: 3 }]
    const points = plotPoints(rows, meta, { chartType: 'bar', xColumn: 'n', yColumns: ['v'] })
    expect(points.map(p => p.row)).toEqual([rows[2], rows[0]])
    expect(Array.from(toUPlotData(rows, meta, { chartType: 'bar', xColumn: 'n', yColumns: ['v'] })[0])).toEqual([1, 3])
  })

  it('scatter keeps each point\'s source row', () => {
    const meta = [{ name: 'a', type: 'Float64' }, { name: 'b', type: 'Float64' }, { name: 'id', type: 'String' }]
    const rows = [{ a: 2, b: 1, id: 'x' }, { a: 1, b: 1, id: 'y' }]
    expect(toScatterData(rows, meta, {})!.rows.map(r => r.id)).toEqual(['y', 'x'])
  })

  it('heatmap keeps the first raw value of each column and row', () => {
    const meta = [{ name: 'day', type: 'String' }, { name: 'hour', type: 'UInt8' }]
    const m = heatmapMatrix([{ day: 'Tue', hour: 9 }, { day: 'Mon', hour: 2 }], meta, {})!
    expect(m.xValues).toEqual(['Tue', 'Mon'])
    expect(m.yValues).toEqual([2, 9])
  })
})
