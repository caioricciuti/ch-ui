import { decodeAbsoluteDashboardRange, resolveNamedPreset } from '../../utils/dashboard-time'

const UNIT_MS: Record<string, number> = { s: 1000, m: 60_000, h: 3_600_000, d: 86_400_000, w: 604_800_000 }

/**
 * Resolve a TimeRangeSelector value ("1h", "preset:today", "abs:iso|iso",
 * "now-2h") into absolute ISO bounds, evaluated now.
 */
export function resolveRange(value: string): { from: string; to: string } {
  const now = new Date()
  const v = (value || '1h').trim()
  const abs = decodeAbsoluteDashboardRange(v)
  if (abs) return abs
  if (v.startsWith('preset:')) {
    const r = resolveNamedPreset(v)
    if (r) return r
  }
  const m = v.replace(/^now-/, '').match(/^(\d+)\s*([smhdw])$/)
  const ms = m ? Number(m[1]) * (UNIT_MS[m[2]] ?? UNIT_MS.h) : UNIT_MS.h
  return { from: new Date(now.getTime() - ms).toISOString(), to: now.toISOString() }
}

/** Short clock for a list row; the full timestamp goes on the title. */
export function formatClock(iso: string): string {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  const hh = String(d.getHours()).padStart(2, '0')
  const mm = String(d.getMinutes()).padStart(2, '0')
  const ss = String(d.getSeconds()).padStart(2, '0')
  const ms = String(d.getMilliseconds()).padStart(3, '0')
  return `${hh}:${mm}:${ss}.${ms}`
}

export function formatFull(iso: string): string {
  const d = new Date(iso)
  return Number.isNaN(d.getTime()) ? iso : d.toLocaleString()
}
