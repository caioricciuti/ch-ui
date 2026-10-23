// SPDX-License-Identifier: BUSL-1.1
import type { AlertChannel, AlertChannelType } from '../../types/alerts'
import type { BadgeTone } from './alerting-format'

// Shared by the Alerts, Alert rules and Alert channels sections.

export const isWebhook = (type: string | undefined): boolean => type === 'webhook'

/** Whether CH-UI can send through a channel of this type (SMTP or webhook). */
export const isSupportedChannelType = (type: string | undefined): type is AlertChannelType =>
  type === 'smtp' || type === 'webhook'

export const REMOVED_CHANNEL_HINT = 'Resend and Brevo were removed. Recreate this channel as SMTP or webhook.'

export function severityTone(severity: string | null | undefined): BadgeTone {
  if (severity === 'critical' || severity === 'error') return 'danger'
  if (severity === 'warn') return 'warning'
  if (severity === 'info') return 'info'
  return 'neutral'
}

export function statusTone(status: string): BadgeTone {
  if (status === 'sent' || status === 'delivered' || status === 'processed') return 'success'
  if (status === 'failed' || status === 'error') return 'danger'
  if (status === 'pending' || status === 'queued') return 'warning'
  if (status === 'acknowledged') return 'info'
  return 'neutral'
}

/** Where a channel sends from: webhook host and format, or sender via SMTP host. */
export function channelTarget(ch: AlertChannel): string {
  if (isWebhook(ch.channel_type)) {
    try {
      return `${new URL(String(ch.config?.url ?? '')).host} (${String(ch.config?.format || 'generic')})`
    } catch {
      return String(ch.config?.url ?? '')
    }
  }
  const from = String(ch.config?.from_email ?? '')
  const host = ch.channel_type === 'smtp' ? String(ch.config?.host ?? '') : ''
  return [from, host].filter(Boolean).join(' via ')
}
