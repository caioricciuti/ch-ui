import { describe, expect, it, vi } from 'vitest'

vi.mock('svelte-sonner', () => ({ toast: { warning: vi.fn() } }))

import { ApiError } from '../../api/client'
import type { Connection, Session } from '../../types/api'
import { switchErrorView, switchTarget } from './connection-switcher'

const conn = (id: string, online = true, extra: Partial<Connection> = {}): Connection =>
  ({ id, name: id, status: 'connected', online, created_at: '', ...extra })
const password = { user: 'alice', role: 'analyst', connectionId: 'a', connectionName: 'a', connectionOnline: true, expiresAt: '' } as Session
const sso = { ...password, user: 'alice@example.com', viaSSO: true } as Session

describe('switchTarget', () => {
  it('marks the current connection and blocks offline ones', () => {
    expect(switchTarget(conn('a'), password)).toMatchObject({ current: true, disabledReason: 'Current connection' })
    expect(switchTarget(conn('b', false), password).disabledReason).toBe('Connection is offline')
    expect(switchTarget(conn('b'), password)).toMatchObject({ current: false, disabledReason: null })
  })

  it('blocks connections without an SSO service account for SSO people only', () => {
    expect(switchTarget(conn('b', true, { sso_ready: false }), sso).disabledReason).toBe('SSO not set up')
    expect(switchTarget(conn('b', true, { sso_ready: true }), sso).disabledReason).toBeNull()
    expect(switchTarget(conn('b', true, { sso_ready: false }), password).disabledReason).toBeNull()
  })
})

describe('switchErrorView', () => {
  it('401 asks for the password', () => {
    expect(switchErrorView(new ApiError('Authentication failed', 401, { needs_password: true }))).toEqual({ message: 'Invalid credentials', needsPassword: true })
  })

  it('429 shows the wait', () => {
    expect(switchErrorView(new ApiError('Too many', 429, null, 37)).message).toBe('Too many attempts. Retry in 37s.')
  })

  it('503 shows the connection message', () => {
    const e = new ApiError('Connection error', 503, { error: 'Connection error', message: 'Connection to ClickHouse failed: could not reach x' })
    expect(switchErrorView(e)).toEqual({ message: 'Connection to ClickHouse failed: could not reach x', needsPassword: false })
  })

  it('409 session_changed asks for a reload', () => {
    expect(switchErrorView(new ApiError('x', 409, { error: 'session_changed' })).message).toMatch(/Reload/)
  })
})
