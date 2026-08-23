/** Format a number with locale-aware separators */
export function formatNumber(n: number): string {
  return n.toLocaleString()
}

/**
 * Format a count compactly with a short scale suffix ("491.09B"), for progress
 * readouts where the exact digits do not matter but the magnitude does.
 */
export function formatCompactNumber(n: number): string {
  const abs = Math.abs(n)
  if (abs < 1_000) return abs < 10 ? n.toFixed(abs % 1 === 0 ? 0 : 2) : n.toFixed(0)
  const units: [number, string][] = [
    [1e12, 'T'],
    [1e9, 'B'],
    [1e6, 'M'],
    [1e3, 'K'],
  ]
  for (const [scale, suffix] of units) {
    if (abs >= scale) return `${(n / scale).toFixed(2)}${suffix}`
  }
  return n.toFixed(0)
}

/** Format bytes to human readable (KB, MB, GB). Optional fixed decimal places. */
export function formatBytes(bytes: number, decimals?: number): string {
  if (bytes === 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB', 'PB']
  const i = Math.min(Math.max(Math.floor(Math.log(Math.abs(bytes)) / Math.log(1024)), 0), units.length - 1)
  const value = bytes / Math.pow(1024, i)
  return `${value.toFixed(decimals ?? (i === 0 ? 0 : 1))} ${units[i]}`
}

/** Format elapsed seconds to human readable */
export function formatElapsed(seconds: number): string {
  if (seconds < 0.001) return '<1ms'
  if (seconds < 1) return `${(seconds * 1000).toFixed(0)}ms`
  if (seconds < 60) return `${seconds.toFixed(2)}s`
  const mins = Math.floor(seconds / 60)
  const secs = seconds % 60
  return `${mins}m ${secs.toFixed(0)}s`
}

/** Format a duration in milliseconds */
export function formatDuration(ms: number): string {
  return formatElapsed(ms / 1000)
}

function toDate(value: unknown): Date | null {
  if (value === null || value === undefined || value === '') return null
  if (value instanceof Date) return value
  return new Date(typeof value === 'number' ? value : String(value))
}

/** Format a timestamp as a local date + time string. Empty values render as an em dash. */
export function formatDate(value: unknown): string {
  const date = toDate(value)
  if (!date) return '—'
  if (Number.isNaN(date.getTime())) return String(value)
  return date.toLocaleString()
}

/** Format a timestamp as local time of day. Pass `{ ms: true }` for millisecond precision (log views). */
export function formatTime(value: unknown, opts?: { ms?: boolean }): string {
  const date = toDate(value)
  if (!date) return '—'
  if (Number.isNaN(date.getTime())) return String(value)
  if (opts?.ms) {
    return date.toLocaleTimeString('en-US', {
      hour12: false,
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit',
      fractionalSecondDigits: 3,
    })
  }
  return date.toLocaleTimeString()
}

/** Format a timestamp as relative time ("just now", "5m ago"), falling back to the local date after a week. */
export function formatRelativeTime(value: unknown): string {
  const date = toDate(value)
  if (!date) return ''
  if (Number.isNaN(date.getTime())) return String(value)
  const diff = Math.max(0, Date.now() - date.getTime())
  if (diff < 60_000) return 'just now'
  if (diff < 3_600_000) return `${Math.floor(diff / 60_000)}m ago`
  if (diff < 86_400_000) return `${Math.floor(diff / 3_600_000)}h ago`
  if (diff < 604_800_000) return `${Math.floor(diff / 86_400_000)}d ago`
  return date.toLocaleDateString()
}
