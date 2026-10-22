import { describe, expect, it } from 'vitest'
import type { AlertChannel } from '../../types/alerts'
import { channelTarget, isSupportedChannelType, severityTone, statusTone } from './alerting-admin'

const channel = (over: Partial<AlertChannel>): AlertChannel => ({
  id: 'c1', name: 'Ops', channel_type: 'smtp', is_active: true, created_at: '', updated_at: '', config: {}, has_secret: false,
  ...over,
})

describe('channelTarget', () => {
  it('shows sender and SMTP host', () => {
    expect(channelTarget(channel({ config: { from_email: 'a@b.com', host: 'smtp.b.com' } }))).toBe('a@b.com via smtp.b.com')
  })
  it('shows only the sender for a removed provider', () => {
    expect(channelTarget(channel({ channel_type: 'resend', config: { from_email: 'a@b.com', base_url: 'https://api.example.com' } }))).toBe('a@b.com')
  })
  it('shows webhook host and format', () => {
    expect(channelTarget(channel({ channel_type: 'webhook', config: { url: 'https://hooks.slack.com/x', format: 'slack' } }))).toBe('hooks.slack.com (slack)')
    expect(channelTarget(channel({ channel_type: 'webhook', config: { url: 'not a url' } }))).toBe('not a url')
  })
})

describe('isSupportedChannelType', () => {
  it('accepts SMTP and webhook only', () => {
    expect(isSupportedChannelType('smtp')).toBe(true)
    expect(isSupportedChannelType('webhook')).toBe(true)
    expect(isSupportedChannelType('brevo')).toBe(false)
    expect(isSupportedChannelType(undefined)).toBe(false)
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
