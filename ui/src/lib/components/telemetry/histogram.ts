import type { HistogramBucket, TraceHistogramBucket } from '../../types/telemetry'
import { SEVERITY_LEVELS } from '../../types/telemetry'
import { normalizeSeverity, severityVar } from './severity'

export interface HistogramRange { from: string; to: string }

/** Bucket timestamps are UTC interval starts; chart points sit inside their intervals. */
export function histogramTimeline(timestamps: string[], bucketSeconds: number, range?: HistogramRange) {
  const step = Number.isFinite(bucketSeconds) && bucketSeconds > 0 ? bucketSeconds : 60
  const populated = [...new Set(timestamps.map((t) => Date.parse(t) / 1000).filter(Number.isFinite))].sort((a, b) => a - b)
  const requestedFrom = range ? Date.parse(range.from) / 1000 : NaN
  const requestedTo = range ? Date.parse(range.to) / 1000 : NaN
  const hasRange = Number.isFinite(requestedFrom) && Number.isFinite(requestedTo) && requestedTo > requestedFrom
  const from = hasRange ? requestedFrom : (populated[0] ?? 0)
  const to = hasRange ? requestedTo : ((populated.at(-1) ?? from) + step)
  const first = Math.floor(from / step) * step
  const count = Math.ceil((to - first) / step)
  // Normal API ranges contain a few hundred buckets. Bound allocation for
  // unusually wide custom ranges without discarding any returned observations.
  const starts = count <= 10000
    ? Array.from({ length: count }, (_, i) => first + i * step)
    : populated.filter((t) => t < to && t + step > from)
  const intervals = starts.map((start) => ({ from: Math.max(start, from), to: Math.min(start + step, to) }))
  return {
    starts,
    x: intervals.map((b) => (b.from + b.to) / 2),
    intervals,
    bounds: [from, to] as [number, number],
    step,
  }
}

/** Keep count, cumulative height, label and color together when reversing paint order. */
export function logsHistogramData(buckets: HistogramBucket[], bucketSeconds: number, range?: HistogramRange) {
  const timeline = histogramTimeline(buckets.map((b) => b.t), bucketSeconds, range)
  const byTime = new Map<number, Map<string, number>>()
  const present = new Set<string>()
  for (const bucket of buckets) {
    const time = Date.parse(bucket.t) / 1000
    const counts = byTime.get(time) ?? new Map<string, number>()
    for (const [key, value] of Object.entries(bucket.counts)) {
      const level = normalizeSeverity(key)
      present.add(level)
      counts.set(level, (counts.get(level) ?? 0) + value)
    }
    byTime.set(time, counts)
  }
  const levels: string[] = SEVERITY_LEVELS.filter((level) => present.has(level))
  levels.push(...[...present].filter((level) => !levels.includes(level)).sort())
  const cumulative = timeline.starts.map(() => 0)
  const series = levels.map((level) => {
    const counts = timeline.starts.map((t) => byTime.get(t)?.get(level) ?? 0)
    const stacked = counts.map((value, i) => (cumulative[i] += value))
    return { label: level, color: severityVar(level), counts, stacked }
  }).reverse()
  return { ...timeline, series }
}

export function tracesHistogramData(buckets: TraceHistogramBucket[], bucketSeconds: number, range?: HistogramRange) {
  const timeline = histogramTimeline(buckets.map((b) => b.t), bucketSeconds, range)
  const byTime = new Map(buckets.map((b) => [Date.parse(b.t) / 1000, b]))
  const ordered = timeline.starts.map((t) => byTime.get(t))
  return {
    ...timeline,
    total: ordered.map((b) => b?.count ?? 0),
    errors: ordered.map((b) => b?.errors ?? 0),
    p50: ordered.map((b) => b && b.count > 0 ? b.p50_ms : null),
    p95: ordered.map((b) => b && b.count > 0 ? b.p95_ms : null),
  }
}

export function histogramSelection(from: number, to: number, bounds: [number, number]): HistogramRange | null {
  const start = Math.max(bounds[0], Math.min(from, to))
  const end = Math.min(bounds[1], Math.max(from, to))
  if (!Number.isFinite(start) || !Number.isFinite(end) || end <= start) return null
  return { from: new Date(start * 1000).toISOString(), to: new Date(end * 1000).toISOString() }
}

/** uPlot's nearest-point cursor can cross a clipped bucket edge too early. */
export function histogramIndexAt(intervals: Array<{ from: number; to: number }>, time: number): number | null {
  if (!Number.isFinite(time)) return null
  let low = 0, high = intervals.length - 1
  while (low <= high) {
    const mid = (low + high) >>> 1
    const interval = intervals[mid]
    if (time < interval.from) high = mid - 1
    else if (time >= interval.to) low = mid + 1
    else return mid
  }
  return null
}

export function formatHistogramInterval(interval: { from: number; to: number }): string {
  const options: Intl.DateTimeFormatOptions = { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit', second: '2-digit' }
  const from = new Date(interval.from * 1000), to = new Date(interval.to * 1000)
  const end = from.toDateString() === to.toDateString() ? to.toLocaleTimeString() : to.toLocaleString(undefined, options)
  return `${from.toLocaleString(undefined, options)} – ${end}`
}
