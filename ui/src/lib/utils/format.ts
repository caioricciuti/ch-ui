/** Format a number with locale-aware separators */
export function formatNumber(n: number): string {
  return n.toLocaleString()
}

/** Format bytes to human readable (KB, MB, GB) */
export function formatBytes(bytes: number): string {
  if (bytes === 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.floor(Math.log(bytes) / Math.log(1024))
  const value = bytes / Math.pow(1024, i)
  return `${value.toFixed(i === 0 ? 0 : 1)} ${units[i]}`
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
