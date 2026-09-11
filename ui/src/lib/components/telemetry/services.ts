/**
 * Stable colors per service for the waterfall and service strips. Ten
 * hues that read on both themes; orange is left out because it is the
 * accent, red because it means error.
 */
const PALETTE = ['#3b82f6', '#10b981', '#8b5cf6', '#f59e0b', '#ec4899', '#14b8a6', '#eab308', '#6366f1', '#84cc16', '#06b6d4']

export function serviceColor(name: string, services: string[]): string {
  const sorted = [...services].sort()
  let idx = sorted.indexOf(name)
  if (idx < 0) {
    // Not in the known list: hash the name so it still gets a fixed hue.
    let h = 0
    for (const ch of name) h = (h * 31 + ch.charCodeAt(0)) >>> 0
    idx = h
  }
  return PALETTE[idx % PALETTE.length]
}

export function formatDuration(ms: number): string {
  if (!Number.isFinite(ms)) return '—'
  if (ms < 1) return `${(ms * 1000).toFixed(0)} µs`
  if (ms < 1000) return `${ms < 10 ? ms.toFixed(2) : ms.toFixed(1)} ms`
  if (ms < 60_000) return `${(ms / 1000).toFixed(2)} s`
  return `${(ms / 60_000).toFixed(1)} min`
}
