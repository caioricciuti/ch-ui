// SPDX-License-Identifier: BUSL-1.1
import type { HealthAlertState, HealthCheckKind, HealthCheckUnit, SqlAlertComparator, SqlAlertMode } from '../../types/alerts'

export type BadgeTone = 'neutral' | 'success' | 'warning' | 'danger' | 'info' | 'brand'

export const HEALTH_CHECK_LABELS: Record<HealthCheckKind, string> = {
  replication_lag: 'Replication lag',
  replica_readonly: 'Replica read-only',
  disk: 'Disk usage',
  parts: 'Parts pressure',
  mutation_stuck: 'Stuck mutation',
  node_unreachable: 'Node unreachable',
  connection_unreachable: 'Connection unreachable',
}

/** Display order of the built-in checks. */
export const HEALTH_CHECK_ORDER: HealthCheckKind[] = [
  'replication_lag', 'replica_readonly', 'disk', 'parts', 'mutation_stuck', 'node_unreachable', 'connection_unreachable',
]

/** Connection-level checks report on node "*". */
export function nodeLabel(node: string | null | undefined): string {
  if (node === '*') return 'Connection'
  return node || '-'
}

export const COMPARATOR_SYMBOLS: Record<SqlAlertComparator, string> = {
  gt: '>', gte: '>=', lt: '<', lte: '<=', eq: '=', ne: '!=',
}

/** 45 -> "45s", 300 -> "5m", 7200 -> "2h", 5400 -> "1.5h". */
export function formatSeconds(n: number): string {
  if (!Number.isFinite(n)) return '-'
  if (n < 60) return `${n}s`
  if (n < 3600) return n % 60 === 0 ? `${n / 60}m` : `${Math.round(n / 60)}m`
  const h = n / 3600
  return Number.isInteger(h) ? `${h}h` : `${Number(h.toFixed(1))}h`
}

/** A plain number, up to 4 decimals. */
export function formatNumber(n: number | null | undefined): string {
  if (n == null || !Number.isFinite(n)) return '-'
  return n.toLocaleString(undefined, { maximumFractionDigits: 4 })
}

/** A threshold or measured value in the check's unit. */
export function formatHealthValue(value: number | null | undefined, unit: HealthCheckUnit): string {
  if (value == null || !Number.isFinite(value)) return '-'
  if (unit === 'seconds') return formatSeconds(value)
  if (unit === 'percent') return `${Number(value.toFixed(1))}%`
  if (unit === 'down') return value > 0 ? 'Down' : 'Up'
  return formatNumber(value)
}

export function conditionLabel(mode: SqlAlertMode, comparator: SqlAlertComparator, threshold: number): string {
  return `${mode === 'rows' ? 'rows' : 'value'} ${COMPARATOR_SYMBOLS[comparator] ?? comparator} ${formatNumber(threshold)}`
}

export interface LiveSummary {
  firing: number
  pending: number
}

/** Count firing and pending nodes per check kind. */
export function summarizeStates(states: HealthAlertState[]): Map<HealthCheckKind, LiveSummary> {
  const out = new Map<HealthCheckKind, LiveSummary>()
  for (const s of states) {
    const cur = out.get(s.check_kind) ?? { firing: 0, pending: 0 }
    if (s.state === 'firing') cur.firing++
    else if (s.state === 'pending') cur.pending++
    out.set(s.check_kind, cur)
  }
  return out
}

export function stateTone(state: string | null | undefined): BadgeTone {
  if (state === 'firing' || state === 'error' || state === 'critical') return 'danger'
  if (state === 'pending' || state === 'warn' || state === 'breach') return 'warning'
  return 'neutral'
}

export function severityTone(severity: string | null | undefined): BadgeTone {
  if (severity === 'critical' || severity === 'error') return 'danger'
  if (severity === 'warn') return 'warning'
  if (severity === 'info') return 'info'
  return 'neutral'
}

const GIB = 1024 ** 3

export function bytesToGb(bytes: number | null): string {
  return bytes == null ? '' : String(Number((bytes / GIB).toFixed(2)))
}

export function gbToBytes(gb: number | null): number | null {
  return gb == null ? null : Math.round(gb * GIB)
}

/** Form input to a number; empty means null. */
export function parseOptionalNumber(v: unknown): number | null {
  if (v == null) return null
  if (typeof v === 'number') return Number.isFinite(v) ? v : null
  const s = String(v).trim()
  if (s === '') return null
  const n = Number(s)
  return Number.isFinite(n) ? n : null
}
