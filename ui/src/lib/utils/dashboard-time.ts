export interface DashboardTimeRangePayload {
  type: 'relative' | 'absolute'
  from: string
  to: string
}

const relativeToken = /^(?:now-)?\s*\d+\s*[a-zA-Z]+$/

export function encodeAbsoluteDashboardRange(fromISO: string, toISO: string): string {
  return `abs:${fromISO}|${toISO}`
}

export function decodeAbsoluteDashboardRange(value: string): { from: string; to: string } | null {
  const trimmed = value.trim()
  if (!trimmed.startsWith('abs:')) return null
  const payload = trimmed.slice(4)
  const sep = payload.indexOf('|')
  if (sep <= 0) return null
  const from = payload.slice(0, sep).trim()
  const to = payload.slice(sep + 1).trim()
  if (!from || !to) return null
  return { from, to }
}

function normalizeRelative(value: string, fallback: string): string {
  const trimmed = value.trim()
  if (!trimmed) return fallback

  if (trimmed.toLowerCase() === 'now') return 'now'

  // normalize forms like "5min", "now-5 minutes", "2hrs"
  const match = trimmed.match(/^((?:now|NOW|Now)-)?\s*(\d+)\s*([a-zA-Z]+)$/)
  if (!match) {
    return fallback
  }

  const prefix = match[1] ? 'now-' : ''
  const amount = match[2]
  // "M" alone is months and "m" alone is minutes, as in Grafana; every
  // longer unit is matched without regard to case.
  if (match[3] === 'M') return `${prefix}${amount}M`
  const rawUnit = match[3].toLowerCase()
  let unit = 'm'

  if (rawUnit === 's' || rawUnit === 'sec' || rawUnit === 'secs' || rawUnit === 'second' || rawUnit === 'seconds') {
    unit = 's'
  } else if (rawUnit === 'm' || rawUnit === 'min' || rawUnit === 'mins' || rawUnit === 'minute' || rawUnit === 'minutes') {
    unit = 'm'
  } else if (rawUnit === 'h' || rawUnit === 'hr' || rawUnit === 'hrs' || rawUnit === 'hour' || rawUnit === 'hours') {
    unit = 'h'
  } else if (rawUnit === 'd' || rawUnit === 'day' || rawUnit === 'days') {
    unit = 'd'
  } else if (rawUnit === 'w' || rawUnit === 'week' || rawUnit === 'weeks') {
    unit = 'w'
  } else if (rawUnit === 'mo' || rawUnit === 'mon' || rawUnit === 'month' || rawUnit === 'months' || rawUnit === 'mth') {
    unit = 'M'
  } else if (rawUnit === 'y' || rawUnit === 'yr' || rawUnit === 'yrs' || rawUnit === 'year' || rawUnit === 'years') {
    unit = 'y'
  }

  return `${prefix}${amount}${unit}`
}

function isAbsoluteToken(value: string): boolean {
  if (!value) return false
  if (value.toLowerCase().startsWith('now')) return false
  return !Number.isNaN(Date.parse(value))
}

// ── Named preset resolution ────────────────────────────────

const PRESET_LABELS: Record<string, string> = {
  'preset:today': 'Today',
  'preset:yesterday': 'Yesterday',
  'preset:this-week': 'This Week',
  'preset:last-week': 'Last Week',
  'preset:this-month': 'This Month',
  'preset:last-month': 'Last Month',
  'preset:last-3-months': 'Last 3 Months',
  'preset:last-6-months': 'Last 6 Months',
}

const DURATION_LABELS: Record<string, string> = {
  '5m': 'Last 5m',
  '15m': 'Last 15m',
  '30m': 'Last 30m',
  '1h': 'Last 1h',
  '3h': 'Last 3h',
  '6h': 'Last 6h',
  '12h': 'Last 12h',
  '24h': 'Last 24h',
  '7d': 'Last 7d',
  '30d': 'Last 30d',
}

export function resolveNamedPreset(name: string): { from: string; to: string } | null {
  const now = new Date()
  const startOfDay = new Date(now.getFullYear(), now.getMonth(), now.getDate())

  switch (name) {
    case 'preset:today':
      return { from: startOfDay.toISOString(), to: now.toISOString() }
    case 'preset:yesterday': {
      const yd = new Date(startOfDay)
      yd.setDate(yd.getDate() - 1)
      return { from: yd.toISOString(), to: startOfDay.toISOString() }
    }
    case 'preset:this-week': {
      const dow = now.getDay()
      const startOfWeek = new Date(startOfDay)
      startOfWeek.setDate(startOfWeek.getDate() - dow)
      return { from: startOfWeek.toISOString(), to: now.toISOString() }
    }
    case 'preset:last-week': {
      const dow = now.getDay()
      const endOfLastWeek = new Date(startOfDay)
      endOfLastWeek.setDate(endOfLastWeek.getDate() - dow)
      const startOfLastWeek = new Date(endOfLastWeek)
      startOfLastWeek.setDate(startOfLastWeek.getDate() - 7)
      return { from: startOfLastWeek.toISOString(), to: endOfLastWeek.toISOString() }
    }
    case 'preset:this-month': {
      const startOfMonth = new Date(now.getFullYear(), now.getMonth(), 1)
      return { from: startOfMonth.toISOString(), to: now.toISOString() }
    }
    case 'preset:last-month': {
      const startOfThisMonth = new Date(now.getFullYear(), now.getMonth(), 1)
      const startOfLastMonth = new Date(now.getFullYear(), now.getMonth() - 1, 1)
      return { from: startOfLastMonth.toISOString(), to: startOfThisMonth.toISOString() }
    }
    case 'preset:last-3-months': {
      const d = new Date(now)
      d.setMonth(d.getMonth() - 3)
      return { from: d.toISOString(), to: now.toISOString() }
    }
    case 'preset:last-6-months': {
      const d = new Date(now)
      d.setMonth(d.getMonth() - 6)
      return { from: d.toISOString(), to: now.toISOString() }
    }
    default:
      return null
  }
}

// ── Public API ──────────────────────────────────────────────

export function formatDashboardTimeRangeLabel(value: string): string {
  const absolute = decodeAbsoluteDashboardRange(value)
  if (absolute) {
    const from = new Date(absolute.from).toLocaleString()
    const to = new Date(absolute.to).toLocaleString()
    return `${from} -> ${to}`
  }

  const trimmed = value.trim()
  if (!trimmed) return 'Last 1h'
  if (PRESET_LABELS[trimmed]) return PRESET_LABELS[trimmed]
  if (DURATION_LABELS[trimmed]) return DURATION_LABELS[trimmed]
  if (trimmed.includes(' to ')) return trimmed
  return trimmed
}

export function toDashboardTimeRangePayload(value: string): DashboardTimeRangePayload {
  const trimmed = value.trim()

  // Named presets — resolved at query time to absolute ranges
  if (trimmed.startsWith('preset:')) {
    const resolved = resolveNamedPreset(trimmed)
    if (resolved) {
      return { type: 'absolute', from: resolved.from, to: resolved.to }
    }
  }

  const absolute = decodeAbsoluteDashboardRange(trimmed)
  if (absolute) {
    return {
      type: 'absolute',
      from: absolute.from,
      to: absolute.to,
    }
  }

  if (!trimmed) {
    return {
      type: 'relative',
      from: '1h',
      to: 'now',
    }
  }

  if (trimmed.includes(' to ')) {
    const [rawFrom, rawTo] = trimmed.split(/\s+to\s+/i).map((part) => part.trim())
    if (isAbsoluteToken(rawFrom) && isAbsoluteToken(rawTo)) {
      return {
        type: 'absolute',
        from: new Date(rawFrom).toISOString(),
        to: new Date(rawTo).toISOString(),
      }
    }

    return {
      type: 'relative',
      from: normalizeRelative(rawFrom, '1h'),
      to: normalizeRelative(rawTo, 'now'),
    }
  }

  // Accept Grafana-style shorthand like "now-5m", "now-5min", "5m"
  if (trimmed.toLowerCase().startsWith('now-') || relativeToken.test(trimmed)) {
    return {
      type: 'relative',
      from: normalizeRelative(trimmed, '1h'),
      to: 'now',
    }
  }

  return {
    type: 'relative',
    from: normalizeRelative(trimmed, '1h'),
    to: 'now',
  }
}

// ── URL parameters ─────────────────────────────────────────

/**
 * The range as ?from=&to= values, Grafana style, so a dashboard link carries
 * its time range: "now-1h"/"now" for relative ranges, ISO instants for
 * absolute ones, and the preset name ("preset:today") for named presets.
 */
export function dashboardRangeToUrlParams(value: string): { from: string; to: string } {
  const trimmed = value.trim() || '1h'
  if (trimmed.startsWith('preset:')) return { from: trimmed, to: 'now' }
  const absolute = decodeAbsoluteDashboardRange(trimmed)
  if (absolute) return absolute
  const payload = toDashboardTimeRangePayload(trimmed)
  if (payload.type === 'absolute') return { from: payload.from, to: payload.to }
  const withNow = (v: string) => (v === 'now' || v.startsWith('now-') ? v : `now-${v}`)
  return { from: withNow(payload.from), to: withNow(payload.to) }
}

function parseUrlInstant(v: string): string | null {
  if (/^\d{10,}$/.test(v)) {
    const n = Number(v)
    return new Date(v.length <= 10 ? n * 1000 : n).toISOString()
  }
  if (!/^\d{4}-\d{2}-\d{2}/.test(v)) return null
  const t = Date.parse(v)
  return Number.isNaN(t) ? null : new Date(t).toISOString()
}

/**
 * Read ?from=&to= back into a dashboard range value, or null when the
 * parameters are missing or not understood. Accepts what
 * dashboardRangeToUrlParams writes, plus epoch milliseconds (as Grafana links
 * use) and relative values without "now-".
 */
export function dashboardRangeFromUrlParams(from: string | null, to: string | null): string | null {
  const f = (from ?? '').trim()
  const t = (to ?? '').trim() || 'now'
  if (!f) return null
  if (f.startsWith('preset:')) return PRESET_LABELS[f] ? f : null

  const fromInstant = parseUrlInstant(f)
  const toInstant = t === 'now' ? new Date().toISOString() : parseUrlInstant(t)
  if (fromInstant && toInstant) return encodeAbsoluteDashboardRange(fromInstant, toInstant)
  if (fromInstant || (toInstant && t !== 'now')) return null

  const rel = /^(?:now-)?\s*\d+\s*[a-zA-Z]+$/
  if (!rel.test(f) || (t !== 'now' && !rel.test(t))) return null
  const short = f.replace(/^now-/, '')
  if (t === 'now') return short
  return `now-${short} to ${t.startsWith('now-') ? t : `now-${t}`}`
}
