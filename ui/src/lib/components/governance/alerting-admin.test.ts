import { describe, expect, it } from 'vitest'
import type { AlertChannel } from '../../types/alerts'
import { channelTarget, severityTone, statusTone } from './alerting-admin'

const channel = (over: Partial<AlertChannel>): AlertChannel => ({
  id: 'c1', name: 'Ops', channel_type: 'smtp', is_active: true, created_at: '', updated_at: '', config: {}, has_secret: false,
  ...over,
})

describe('channelTarget', () => {
  it('shows sender and SMTP host', () => {
    expect(channelTarget(channel({ config: { from_email: 'a@b.com', host: 'smtp.b.com' } }))).toBe('a@b.com via smtp.b.com')
  })
  it('falls back to the provider API', () => {
    expect(channelTarget(channel({ channel_type: 'resend', config: { from_email: 'a@b.com' } }))).toBe('a@b.com via resend API')
  })
  it('shows webhook host and format', () => {
    expect(channelTarget(channel({ channel_type: 'webhook', config: { url: 'https://hooks.slack.com/x', format: 'slack' } }))).toBe('hooks.slack.com (slack)')
    expect(channelTarget(channel({ channel_type: 'webhook', config: { url: 'not a url' } }))).toBe('not a url')
  })
})

describe('tones', () => {
  it('maps severity and status', () => {
    expect(severityTone('critical')).toBe('danger')
    expect(severityTone('warn')).toBe('warning')
    expect(severityTone(undefined)).toBe('neutral')
    expect(statusTone('processed')).toBe('success')
    expect(statusTone('failed')).toBe('danger')
    expect(statusTone('acknowledged')).toBe('info')
  })
})
