// SPDX-License-Identifier: BUSL-1.1
import type { AlertAck, FiringIncident, HealthCheckKind, Silence, SilenceStatus } from '../../types/alerts'
import { HEALTH_CHECK_LABELS, nodeLabel, type BadgeTone } from './alerting-format'

export interface Option {
  value: string
  label: string
}

/** Event types a rule or a silence can match. */
export const EVENT_TYPE_OPTIONS: Option[] = [
  { value: 'policy.violation', label: 'Policy violation' },
  { value: 'schedule.failed', label: 'Schedule failed' },
  { value: 'schedule.slow', label: 'Schedule slow' },
  { value: 'telemetry.monitor', label: 'Telemetry monitor' },
  { value: 'health.replication_lag', label: 'Health: replication lag' },
  { value: 'health.replica_readonly', label: 'Health: replica readonly' },
  { value: 'health.disk', label: 'Health: disk' },
  { value: 'health.parts', label: 'Health: parts pressure' },
  { value: 'health.mutation_stuck', label: 'Health: stuck mutation' },
  { value: 'health.node_unreachable', label: 'Health: node unreachable' },
  { value: 'health.connection_unreachable', label: 'Health: connection unreachable' },
  { value: 'health.*', label: 'All health checks' },
  { value: 'sql.alert', label: 'SQL alert' },
  { value: 'sql.*', label: 'All SQL alerts' },
  { value: 'report.failed', label: 'Report failed' },
  { value: 'report.disabled', label: 'Report disabled' },
  { value: 'report.*', label: 'All report events' },
  { value: '*', label: 'All events' },
]

export function eventTypeLabel(v: string): string {
  return EVENT_TYPE_OPTIONS.find((o) => o.value === v)?.label ?? v
}

export const SILENCE_DURATION_OPTIONS: Option[] = [
  { value: '3600', label: '1 hour' },
  { value: '14400', label: '4 hours' },
  { value: '86400', label: '24 hours' },
  { value: 'custom', label: 'Custom end time' },
]

export const ACK_EXPIRY_OPTIONS: Option[] = [
  { value: '', label: 'Until it resolves' },
  { value: '3600', label: '1 hour' },
  { value: '14400', label: '4 hours' },
  { value: '86400', label: '24 hours' },
]

export function silenceStatusTone(status: SilenceStatus | string): BadgeTone {
  if (status === 'active') return 'info'
  if (status === 'scheduled') return 'warning'
  return 'neutral'
}

export function silenceStatusLabel(status: SilenceStatus | string): string {
  if (status === 'active') return 'Active'
  if (status === 'scheduled') return 'Scheduled'
  if (status === 'expired') return 'Expired'
  return status
}

export function healthSourceRef(kind: HealthCheckKind): string {
  return `health:${kind}`
}

/** "health:disk" -> "Health: Disk usage"; a SQL alert id -> its name. */
export function sourceRefLabel(ref: string, sqlAlertName: (id: string) => string | undefined): string {
  if (ref.startsWith('health:')) {
    const kind = ref.slice('health:'.length) as HealthCheckKind
    return `Health: ${HEALTH_CHECK_LABELS[kind] ?? kind}`
  }
  return sqlAlertName(ref) ?? `SQL alert ${ref}`
}

export type SilenceMatchers = Pick<Silence, 'connection_id' | 'event_type' | 'source_ref' | 'node'>

export function hasMatcher(m: SilenceMatchers): boolean {
  return !!(m.connection_id || m.event_type || m.source_ref || m.node)
}

export interface MatchLookup {
  connectionName: (id: string) => string | undefined
  sqlAlertName: (id: string) => string | undefined
}

/** One line describing what a silence holds back. */
export function silenceMatchSummary(m: SilenceMatchers, lookup: MatchLookup): string {
  const parts: string[] = []
  if (m.connection_id) parts.push(lookup.connectionName(m.connection_id) ?? 'Deleted connection')
  if (m.event_type) parts.push(eventTypeLabel(m.event_type))
  if (m.source_ref) parts.push(sourceRefLabel(m.source_ref, lookup.sqlAlertName))
  if (m.node) parts.push(`node ${nodeLabel(m.node)}`)
  return parts.length ? parts.join(', ') : 'Everything'
}

const pad = (n: number) => String(n).padStart(2, '0')

/** A Date as the value of an <input type="datetime-local">, in local time. */
export function toLocalInput(d: Date): string {
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`
}

/** datetime-local value (local time) to RFC3339 UTC; null when empty or invalid. */
export function localInputToIso(v: string): string | null {
  if (!v.trim()) return null
  const d = new Date(v)
  if (Number.isNaN(d.getTime())) return null
  return d.toISOString().replace(/\.\d{3}Z$/, 'Z')
}

/** The preset matching a window exactly, else "custom". */
export function durationPreset(startIso: string, endIso: string): string {
  const secs = Math.round((new Date(endIso).getTime() - new Date(startIso).getTime()) / 1000)
  return SILENCE_DURATION_OPTIONS.find((o) => o.value === String(secs))?.value ?? 'custom'
}

const windowFormat: Intl.DateTimeFormatOptions = { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' }

/** Start to end in local time. */
export function formatWindow(startIso: string, endIso: string): string {
  const fmt = (v: string) => {
    const d = new Date(v)
    return Number.isNaN(d.getTime()) ? v : d.toLocaleString(undefined, windowFormat)
  }
  return `${fmt(startIso)} to ${fmt(endIso)}`
}

/** Hover text for an Acknowledged badge. */
export function ackTitle(ack: AlertAck): string {
  const who = ack.acked_by ? `Acknowledged by ${ack.acked_by}` : 'Acknowledged'
  return ack.comment ? `${who}: ${ack.comment}` : who
}

export function healthFiringKey(connectionId: string, kind: string, node: string): string {
  return `${connectionId}|${kind}|${node}`
}

export interface FiringIndex {
  health: Map<string, FiringIncident>
  sql: Map<string, FiringIncident>
}

/** Look up firing incidents by health row (connection, check, node) or SQL alert id. */
export function indexFiring(incidents: FiringIncident[]): FiringIndex {
  const health = new Map<string, FiringIncident>()
  const sql = new Map<string, FiringIncident>()
  for (const inc of incidents) {
    if (inc.source === 'health' && inc.check_kind) {
      health.set(healthFiringKey(inc.connection_id, inc.check_kind, inc.node ?? ''), inc)
    } else if (inc.source === 'sql' && inc.sql_alert_id) {
      sql.set(inc.sql_alert_id, inc)
    }
  }
  return { health, sql }
}
