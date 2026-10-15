// SPDX-License-Identifier: BUSL-1.1
import { apiDel, apiGet, apiPost, apiPut } from './client'
import type { AlertAck, AlertChannel, AlertChannelType, AlertEvent, AlertRule, FiringIncident, Silence, SilenceInput } from '../types/alerts'

const BASE = '/api/governance/alerts'

export type AlertRuleChannelPayload = {
  channel_id: string
  recipients: string[]
  is_active: boolean
}

export async function adminListAlertChannels(): Promise<AlertChannel[]> {
  const res = await apiGet<{ channels: AlertChannel[] }>(`${BASE}/channels`)
  return res.channels ?? []
}

export async function adminCreateAlertChannel(payload: {
  name: string
  channel_type: AlertChannelType
  is_active: boolean
  config: Record<string, unknown>
}): Promise<void> {
  await apiPost(`${BASE}/channels`, payload)
}

export async function adminUpdateAlertChannel(id: string, payload: {
  name?: string
  channel_type?: AlertChannelType
  is_active?: boolean
  config?: Record<string, unknown>
}): Promise<void> {
  await apiPut(`${BASE}/channels/${encodeURIComponent(id)}`, payload)
}

export async function adminDeleteAlertChannel(id: string): Promise<void> {
  await apiDel(`${BASE}/channels/${encodeURIComponent(id)}`)
}

export async function adminTestAlertChannel(id: string, payload: {
  /** Required for email channels; webhooks ignore it. */
  recipients?: string[]
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
  channels: AlertRuleChannelPayload[]
  /** null = every connection. */
  connection_ids?: string[] | null
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
  channels?: AlertRuleChannelPayload[]
  connection_ids?: string[] | null
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

/** Newest first. */
export async function listSilences(): Promise<Silence[]> {
  const res = await apiGet<{ silences?: Silence[] }>(`${BASE}/silences`)
  return res.silences ?? []
}

export async function createSilence(input: SilenceInput): Promise<string> {
  const res = await apiPost<{ id: string }>(`${BASE}/silences`, input)
  return res.id
}

export async function updateSilence(id: string, input: SilenceInput): Promise<void> {
  await apiPut(`${BASE}/silences/${encodeURIComponent(id)}`, input)
}

/** Ends the silence now. */
export async function expireSilence(id: string): Promise<void> {
  await apiPost(`${BASE}/silences/${encodeURIComponent(id)}/expire`)
}

/** Open incidents across health checks, SQL alerts and (admins only) access drift, with their ack. */
export async function listFiring(): Promise<FiringIncident[]> {
  const res = await apiGet<{ incidents?: FiringIncident[] }>(`${BASE}/firing`)
  return res.incidents ?? []
}

export async function createAck(input: { event_id: string; comment?: string; expires_in?: number }): Promise<AlertAck> {
  const res = await apiPost<{ ack: AlertAck }>(`${BASE}/acks`, input)
  return res.ack
}

export async function deleteAck(id: string): Promise<void> {
  await apiDel(`${BASE}/acks/${encodeURIComponent(id)}`)
}
