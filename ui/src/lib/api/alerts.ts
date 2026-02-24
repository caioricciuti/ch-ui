import { apiDel, apiGet, apiPost, apiPut } from './client'
import type { AlertChannel, AlertEvent, AlertRule } from '../types/alerts'

const BASE = '/api/governance/alerts'

export type AlertRuleRoutePayload = {
  channel_id: string
  recipients: string[]
  is_active: boolean
  delivery_mode?: 'immediate' | 'digest'
  digest_window_minutes?: number
  escalation_channel_id?: string
  escalation_recipients?: string[]
  escalation_after_failures?: number
}

export async function adminListAlertChannels(): Promise<AlertChannel[]> {
  const res = await apiGet<{ channels: AlertChannel[] }>(`${BASE}/channels`)
  return res.channels ?? []
}

export async function adminCreateAlertChannel(payload: {
  name: string
  channel_type: 'smtp' | 'resend' | 'brevo'
  is_active: boolean
  config: Record<string, unknown>
}): Promise<void> {
  await apiPost(`${BASE}/channels`, payload)
}

export async function adminUpdateAlertChannel(id: string, payload: {
  name?: string
  channel_type?: 'smtp' | 'resend' | 'brevo'
  is_active?: boolean
  config?: Record<string, unknown>
}): Promise<void> {
  await apiPut(`${BASE}/channels/${encodeURIComponent(id)}`, payload)
}

export async function adminDeleteAlertChannel(id: string): Promise<void> {
  await apiDel(`${BASE}/channels/${encodeURIComponent(id)}`)
}

export async function adminTestAlertChannel(id: string, payload: {
  recipients: string[]
  subject?: string
  message?: string
}): Promise<{ provider_message_id?: string }> {
  return apiPost<{ provider_message_id?: string }>(`${BASE}/channels/${encodeURIComponent(id)}/test`, payload)
}

export async function adminListAlertRules(): Promise<AlertRule[]> {
  const res = await apiGet<{ rules: AlertRule[] }>(`${BASE}/rules`)
  return res.rules ?? []
}

export async function adminCreateAlertRule(payload: {
  name: string
  event_type: string
  severity_min: string
  enabled: boolean
  cooldown_seconds: number
  max_attempts: number
  subject_template?: string
  body_template?: string
  routes: AlertRuleRoutePayload[]
}): Promise<void> {
  await apiPost(`${BASE}/rules`, payload)
}

export async function adminUpdateAlertRule(id: string, payload: {
  name?: string
  event_type?: string
  severity_min?: string
  enabled?: boolean
  cooldown_seconds?: number
  max_attempts?: number
  subject_template?: string
  body_template?: string
  routes?: AlertRuleRoutePayload[]
}): Promise<void> {
  await apiPut(`${BASE}/rules/${encodeURIComponent(id)}`, payload)
}

export async function adminDeleteAlertRule(id: string): Promise<void> {
  await apiDel(`${BASE}/rules/${encodeURIComponent(id)}`)
}

export async function adminListAlertEvents(params: { limit?: number; eventType?: string; status?: string } = {}): Promise<AlertEvent[]> {
  const q = new URLSearchParams()
  if (params.limit) q.set('limit', String(params.limit))
  if (params.eventType) q.set('event_type', params.eventType)
  if (params.status) q.set('status', params.status)
  const url = `${BASE}/events${q.toString() ? `?${q.toString()}` : ''}`
  const res = await apiGet<{ events: AlertEvent[] }>(url)
  return res.events ?? []
}
