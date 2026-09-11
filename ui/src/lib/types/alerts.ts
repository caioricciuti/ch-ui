export type AlertChannelType = 'smtp' | 'resend' | 'brevo'
export type AlertSeverity = 'info' | 'warn' | 'error' | 'critical'
export type AlertEventType = 'policy.violation' | 'schedule.failed' | 'schedule.slow' | 'telemetry.monitor' | '*'

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
  status: string
  created_at: string
  processed_at?: string | null
}
