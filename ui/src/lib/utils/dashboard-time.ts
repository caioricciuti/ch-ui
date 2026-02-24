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
  const lower = trimmed.toLowerCase()

  if (lower === 'now') return 'now'

  // normalize forms like "5min", "now-5 minutes", "2hrs"
  const match = lower.match(/^(now-)?\s*(\d+)\s*([a-z]+)$/)
  if (!match) {
    return fallback
  }

  const prefix = match[1] ? 'now-' : ''
  const amount = match[2]
  const rawUnit = match[3]
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
