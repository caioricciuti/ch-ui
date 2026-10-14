// SPDX-License-Identifier: BUSL-1.1
import type { AlertChannel, AlertChannelType } from '../../types/alerts'
import type { BadgeTone } from './alerting-format'

// Shared by the Alerts, Alert rules and Alert channels sections.

export const isWebhook = (type: AlertChannelType | undefined): boolean => type === 'webhook'

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

/** Where a channel sends from: webhook host and format, or sender via host/provider. */
export function channelTarget(ch: AlertChannel): string {
  if (isWebhook(ch.channel_type)) {
    try {
      return `${new URL(String(ch.config?.url ?? '')).host} (${String(ch.config?.format || 'generic')})`
    } catch {
      return String(ch.config?.url ?? '')
    }
  }
  const from = String(ch.config?.from_email ?? '')
  const host = ch.channel_type === 'smtp'
    ? String(ch.config?.host ?? '')
    : String(ch.config?.base_url || `${ch.channel_type} API`)
  return [from, host].filter(Boolean).join(' via ')
}
