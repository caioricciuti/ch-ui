import { describe, expect, it } from 'vitest'
import { histogramSelection, histogramTimeline, histogramIndexAt, logsHistogramData, tracesHistogramData } from './histogram'

const timestamp = (seconds: number) => new Date(Date.UTC(2026, 8, 21, 7, 2) + seconds * 1000).toISOString()
const epoch = (seconds: number) => Date.parse(timestamp(seconds)) / 1000

describe('log histogram severity identity', () => {
  it.each<Record<string, number>>([
    { INFO: 37, WARN: 1 },
    { WARN: 1, INFO: 37 },
    { warning: 1, info: 37 },
  ])('keeps 37 INFO and 1 WARN attached to their labels and colors for %j', (counts) => {
    const plot = logsHistogramData([{ t: timestamp(30), counts }], 10)
    expect(plot.series).toEqual([
      { label: 'WARN', color: '--warning', counts: [1], stacked: [38] },
      { label: 'INFO', color: '--accent', counts: [37], stacked: [37] },
    ])
  })

  it('combines aliases and retains independent counts with more than two severities', () => {
    const plot = logsHistogramData([{ t: timestamp(30), counts: { custom: 3, ERROR: 2, warning: 1, WARN: 4, INFO: 37 } }], 10)
    expect(plot.series.map((s) => [s.label, s.counts[0], s.stacked[0]])).toEqual([
      ['CUSTOM', 3, 47], ['ERROR', 2, 44], ['WARN', 5, 42], ['INFO', 37, 37],
    ])
  })

  it('preserves the screenshot totals across separate buckets and empty intervals', () => {
    const plot = logsHistogramData([
      { t: timestamp(40), counts: { WARN: 1 } },
      { t: timestamp(30), counts: { INFO: 37 } },
    ], 10, { from: timestamp(23.892), to: timestamp(52.994) })
    expect(plot.series.map((s) => [s.label, s.counts])).toEqual([
      ['WARN', [0, 0, 1, 0]], ['INFO', [0, 37, 0, 0]],
    ])
    expect(plot.bounds).toEqual([epoch(23.892), epoch(52.994)])
    expect(plot.intervals.at(-1)?.to).toBe(epoch(52.994))
    expect(plot.x.every((t) => t > plot.bounds[0] && t < plot.bounds[1])).toBe(true)
  })
})

describe('histogram time alignment', () => {
  it('uses the containing bucket near clipped-edge midpoints and exact boundaries', () => {
    const plot = logsHistogramData([
      { t: timestamp(30), counts: { INFO: 37 } }, { t: timestamp(40), counts: { WARN: 1 } },
    ], 10, { from: timestamp(23.892), to: timestamp(52.994) })
    for (const [seconds, expected] of [[30.5, 1], [39.999, 1], [40, 2], [49, 2], [50, 3]]) {
      expect(histogramIndexAt(plot.intervals, epoch(seconds))).toBe(expected)
    }
    expect(histogramIndexAt(plot.intervals, epoch(23))).toBeNull()
    expect(histogramIndexAt(plot.intervals, epoch(52.994))).toBeNull()
  })
  it('sorts uneven bucket times and keeps empty periods at their real positions', () => {
    const plot = logsHistogramData([
      { t: timestamp(50), counts: { ERROR: 2 } },
      { t: timestamp(10), counts: { INFO: 5 } },
    ], 10, { from: timestamp(0), to: timestamp(60) })
    expect(plot.starts).toEqual([0, 10, 20, 30, 40, 50].map(epoch))
    expect(plot.x).toEqual([5, 15, 25, 35, 45, 55].map(epoch))
    expect(plot.series.find((s) => s.label === 'INFO')?.counts).toEqual([0, 5, 0, 0, 0, 0])
    expect(plot.series.find((s) => s.label === 'ERROR')?.counts).toEqual([0, 0, 0, 0, 0, 2])
  })

  it('gives a single populated bucket its full width', () => {
    const plot = histogramTimeline([timestamp(30)], 10)
    expect(plot.bounds).toEqual([epoch(30), epoch(40)])
    expect(plot.x).toEqual([epoch(35)])
  })

  it('does not extend a dragged range by another bucket and clamps to the queried window', () => {
    const bounds: [number, number] = [epoch(23.892), epoch(52.994)]
    expect(histogramSelection(epoch(30), epoch(40), bounds)).toEqual({ from: timestamp(30), to: timestamp(40) })
    expect(histogramSelection(epoch(60), epoch(0), bounds)).toEqual({ from: timestamp(23.892), to: timestamp(52.994) })
    expect(histogramSelection(epoch(30), epoch(30), bounds)).toBeNull()
  })

  it('keeps trace errors and durations aligned across gaps without inventing zero latency', () => {
    const plot = tracesHistogramData([
      { t: timestamp(40), count: 1, errors: 1, p50_ms: 9, p95_ms: 9 },
      { t: timestamp(10), count: 37, errors: 0, p50_ms: 12, p95_ms: 45 },
    ], 10, { from: timestamp(0), to: timestamp(60) })
    expect(plot.total).toEqual([0, 37, 0, 0, 1, 0])
    expect(plot.errors).toEqual([0, 0, 0, 0, 1, 0])
    expect(plot.p50).toEqual([null, 12, null, null, 9, null])
    expect(plot.p95).toEqual([null, 45, null, null, 9, null])
  })
})
