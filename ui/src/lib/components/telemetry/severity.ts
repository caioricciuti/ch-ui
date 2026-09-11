import type { SeverityLevel } from '../../types/telemetry'

/** Badge tone per severity; the same palette drives the histogram. */
export function severityTone(sev: string): 'neutral' | 'info' | 'brand' | 'warning' | 'danger' {
  switch (sev.toUpperCase()) {
    case 'DEBUG': return 'info'
    case 'INFO': return 'brand'
    case 'WARN': case 'WARNING': return 'warning'
    case 'ERROR': case 'FATAL': case 'CRITICAL': return 'danger'
    default: return 'neutral'
  }
}

/** CSS variable name that colors a severity in charts. */
export function severityVar(sev: string): string {
  switch (sev.toUpperCase()) {
    case 'TRACE': return '--fg-4'
    case 'DEBUG': return '--info'
    case 'INFO': return '--accent'
    case 'WARN': case 'WARNING': return '--warning'
    case 'ERROR': return '--danger'
    case 'FATAL': case 'CRITICAL': return '--danger'
    default: return '--fg-3'
  }
}

export function normalizeSeverity(sev: string): SeverityLevel | string {
  const u = sev.toUpperCase()
  if (u === 'WARNING') return 'WARN'
  if (u === 'CRITICAL') return 'FATAL'
  return u
}
