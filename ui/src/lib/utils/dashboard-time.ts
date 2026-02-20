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

export function formatDashboardTimeRangeLabel(value: string): string {
  const absolute = decodeAbsoluteDashboardRange(value)
  if (absolute) {
    const from = new Date(absolute.from).toLocaleString()
    const to = new Date(absolute.to).toLocaleString()
    return `${from} -> ${to}`
  }

  const trimmed = value.trim()
  if (!trimmed) return 'Last 1h'
  if (trimmed.includes(' to ')) return trimmed
  if (trimmed === '5m') return 'Last 5m'
  if (trimmed === '15m') return 'Last 15m'
  if (trimmed === '1h') return 'Last 1h'
  if (trimmed === '6h') return 'Last 6h'
  if (trimmed === '24h') return 'Last 24h'
  if (trimmed === '7d') return 'Last 7d'
  if (trimmed === '30d') return 'Last 30d'
  return trimmed
}

export function toDashboardTimeRangePayload(value: string): DashboardTimeRangePayload {
  const trimmed = value.trim()

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
