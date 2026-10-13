// SPDX-License-Identifier: BUSL-1.1
export type AlertChannelType = 'smtp' | 'resend' | 'brevo' | 'webhook'
export type WebhookFormat = 'generic' | 'slack' | 'teams'
export type AlertSeverity = 'info' | 'warn' | 'error' | 'critical'
export type AlertEventType =
  | 'policy.violation' | 'schedule.failed' | 'schedule.slow' | 'telemetry.monitor'
  | 'health.replication_lag' | 'health.replica_readonly' | 'health.disk' | 'health.parts'
  | 'health.mutation_stuck' | 'health.node_unreachable' | 'health.connection_unreachable' | 'health.*'
  | 'sql.alert' | 'sql.*'
  | 'report.failed' | 'report.disabled' | 'report.*'
  | '*'

export interface AlertChannel {
  id: string
  name: string
  channel_type: AlertChannelType
  is_active: boolean
  created_by?: string | null
  created_at: string
  updated_at: string
  config: Record<string, unknown>
  has_secret: boolean
}

export interface AlertRuleChannelBinding {
  id: string
  rule_id: string
  channel_id: string
  channel_name: string
  channel_type: AlertChannelType
  recipients: string[]
  is_active: boolean
  created_at: string
  updated_at: string
}

export interface AlertRule {
  id: string
  name: string
  event_type: AlertEventType | string
  severity_min: AlertSeverity
  enabled: boolean
  cooldown_seconds: number
  max_attempts: number
  subject_template?: string | null
  body_template?: string | null
  created_by?: string | null
  created_at: string
  updated_at: string
  channels: AlertRuleChannelBinding[]
  /** null or empty = every connection. */
  connection_ids?: string[] | null
}

export interface AlertEvent {
  id: string
  connection_id?: string | null
  event_type: string
  severity: AlertSeverity | string
  title: string
  message: string
  payload_json?: string | null
  fingerprint?: string | null
  source_ref?: string | null
  /** 'new' | 'processed' | 'silenced' | 'acknowledged' */
  status: string
  created_at: string
  processed_at?: string | null
  /** Silence or acknowledgement id that held this event back. */
  suppressed_by?: string | null
}

// ── Silences and acknowledgements (/api/governance/alerts) ──

export type SilenceStatus = 'active' | 'scheduled' | 'expired'

export interface Silence {
  id: string
  reason: string
  connection_id: string | null
  event_type: string | null
  /** One SQL alert id, or "health:<check_kind>". */
  source_ref: string | null
  node: string | null
  starts_at: string
  ends_at: string
  created_by: string | null
  created_at: string
  expired_at: string | null
  expired_by: string | null
  status: SilenceStatus
}

export interface SilenceInput {
  reason: string
  connection_id: string | null
  event_type: string | null
  source_ref: string | null
  node: string | null
  /** RFC3339 UTC; the server defaults to now. */
  starts_at?: string
  duration_seconds?: number
  ends_at?: string
}

export interface AlertAck {
  id: string
  alert_key: string
  incident_id: string | null
  event_id: string | null
  connection_id: string | null
  severity: string | null
  comment: string | null
  acked_by: string | null
  created_at: string
  expires_at: string | null
}

export interface FiringIncident {
  source: 'health' | 'sql'
  alert_key: string
  incident_id: string
  connection_id: string
  check_kind: string | null
  node: string | null
  sql_alert_id: string | null
  name: string
  severity: string
  since: string
  /** null: there is no event to acknowledge. */
  event_id: string | null
  ack: AlertAck | null
}

// ── Health checks and SQL threshold alerts (/api/alerting) ──

export type HealthCheckKind =
  | 'replication_lag' | 'replica_readonly' | 'disk' | 'parts' | 'mutation_stuck' | 'node_unreachable' | 'connection_unreachable'
export type HealthCheckUnit = 'seconds' | 'count' | 'percent' | 'down'
export type AlertState = 'ok' | 'pending' | 'firing'

export interface HealthCheckSetting {
  check_kind: HealthCheckKind
  enabled: boolean
  /** null = no warn level */
  warn_threshold: number | null
  /** null = no critical level */
  crit_threshold: number | null
  /** Disk only: critical when the emptiest disk has fewer free bytes than this; null = off. */
  free_bytes_floor: number | null
  /** Consecutive breaches before firing (1-20). */
  for_evaluations: number
  /** Consecutive OKs before resolving (1-20). */
  resolve_evaluations: number
  unit: HealthCheckUnit
  /** false for replica_readonly, node_unreachable and connection_unreachable */
  supports_warn: boolean
  updated_at: string | null
}

export type HealthCheckSettingInput = Pick<
  HealthCheckSetting,
  'check_kind' | 'enabled' | 'warn_threshold' | 'crit_threshold' | 'free_bytes_floor' | 'for_evaluations' | 'resolve_evaluations'
>

export interface HealthAlertState {
  connection_id: string
  check_kind: HealthCheckKind
  /** "*" for connection-level checks. */
  node: string
  state: AlertState
  severity: 'warn' | 'critical' | null
  breach_count: number
  ok_count: number
  last_value: number | null
  since: string | null
  last_event_id: string | null
  updated_at: string | null
}

export type SqlAlertMode = 'value' | 'rows'
export type SqlAlertComparator = 'gt' | 'gte' | 'lt' | 'lte' | 'eq' | 'ne'
export type SqlAlertState = AlertState | 'error'
export type SqlAlertWorkerMode = 'session' | 'service_account' | 'disabled'

export interface SqlAlert {
  id: string
  connection_id: string
  name: string
  sql: string
  mode: SqlAlertMode
  comparator: SqlAlertComparator
  threshold: number
  severity: AlertSeverity
  /** 60..86400 */
  interval_seconds: number
  /** 5..60 */
  timeout_seconds: number
  /** 1..20 */
  for_evaluations: number
  /** 1..20 */
  resolve_evaluations: number
  enabled: boolean
  state: SqlAlertState
  breach_count: number
  ok_count: number
  error_count: number
  last_run_at: string | null
  last_value: number | null
  last_error: string | null
  created_by: string | null
  created_at: string
  updated_at: string
}

export interface SqlAlertInput {
  name: string
  sql: string
  mode: SqlAlertMode
  comparator: SqlAlertComparator
  threshold: number
  severity: AlertSeverity
  interval_seconds: number
  timeout_seconds: number
  for_evaluations: number
  resolve_evaluations: number
  enabled: boolean
}

export type SqlAlertTestInput = Pick<SqlAlertInput, 'sql' | 'mode' | 'comparator' | 'threshold' | 'timeout_seconds'>

export interface SqlAlertTestResult {
  value: number | null
  rows: number
  /** Up to 10 rows. */
  rows_preview: Record<string, unknown>[]
  firing: boolean
  duration_ms: number
}

export interface SqlAlertRunResult {
  value: number | null
  firing: boolean
  alert: SqlAlert
}

export interface AlertEvaluation {
  id: number
  source: 'health' | 'sql'
  source_id: string
  connection_id: string
  node: string
  value: number | null
  state: string
  error: string
  duration_ms: number
  evaluated_at: string
}
