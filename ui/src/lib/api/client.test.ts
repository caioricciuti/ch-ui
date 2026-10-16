import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

const warning = vi.fn()
vi.mock('svelte-sonner', () => ({ toast: { warning: (m: string) => warning(m) } }))

import { ApiError, apiGet, safeNextPath } from './client'
import { CONNECTION_HEADER, setWorkspaceScope } from '../stores/workspace-scope'

const origin = 'https://ch-ui.example.com'

describe('safeNextPath', () => {
  it('keeps a same-origin path and query', () => {
    expect(safeNextPath('/dashboards/abc?range=1h', origin)).toBe('/dashboards/abc?range=1h')
    expect(safeNextPath('/?tab=123', origin)).toBe('/?tab=123')
  })

  it('drops the fragment', () => {
    expect(safeNextPath('/admin?section=users#top', origin)).toBe('/admin?section=users')
  })

  it.each([
    null,
    '',
    'https://evil.example/x',
    '//evil.example/x',
    '/\\evil.example',
    'javascript:alert(1)',
    'dashboards',
    '/login',
    '/login?next=/x',
    '/\u0000x',
    '/' + 'a'.repeat(3000),
  ])('rejects %s', (raw) => {
    expect(safeNextPath(raw, origin)).toBeNull()
  })
})


describe('request connection guard', () => {
  const reload = vi.fn()

  beforeEach(() => {
    vi.useFakeTimers()
    vi.stubGlobal('window', { location: { reload, pathname: '/', search: '', origin: 'https://ch-ui.example.com' } })
    reload.mockReset()
    warning.mockReset()
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
    setWorkspaceScope(null)
  })

  function json(body: unknown, status: number): Response {
    return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
  }

  it('sends the session connection on every request, none when signed out', async () => {
    const fetchMock = vi.fn(async () => json({ ok: true }, 200))
    vi.stubGlobal('fetch', fetchMock)
    await apiGet('/api/auth/config')
    let init = fetchMock.mock.calls[0] as unknown as [string, RequestInit]
    expect((init[1].headers as Record<string, string>)[CONNECTION_HEADER]).toBeUndefined()

    setWorkspaceScope({ user: 'alice', connectionId: 'conn-1' })
    await apiGet('/api/query/schema')
    init = fetchMock.mock.calls[1] as unknown as [string, RequestInit]
    const headers = init[1].headers as Record<string, string>
    expect(headers[CONNECTION_HEADER]).toBe('conn-1')
    expect(headers['Content-Type']).toBe('application/json')
  })

  it('a 409 connection_changed warns once and reloads', async () => {
    setWorkspaceScope({ user: 'alice', connectionId: 'conn-1' })
    vi.stubGlobal('fetch', vi.fn(async () => json({ success: false, error: 'connection_changed' }, 409)))
    await expect(apiGet('/api/query/schema')).rejects.toBeInstanceOf(ApiError)
    await expect(apiGet('/api/query/schema')).rejects.toBeInstanceOf(ApiError)
    expect(warning).toHaveBeenCalledTimes(1)
    expect(reload).not.toHaveBeenCalled()
    vi.runAllTimers()
    expect(reload).toHaveBeenCalledTimes(1)
  })

  it('other 409s are ordinary errors carrying the body', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => json({ success: false, error: 'sso_not_configured', message: 'SSO is not set up' }, 409)))
    const err = await apiGet('/api/auth/switch-connection').catch((e: unknown) => e)
    expect(err).toBeInstanceOf(ApiError)
    expect((err as ApiError).message).toBe('SSO is not set up')
    expect((err as ApiError).body).toEqual({ success: false, error: 'sso_not_configured', message: 'SSO is not set up' })
    expect(warning).not.toHaveBeenCalled()
  })

  it('a 402 keeps the message and the body', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => json({ error: 'Sampling needs CH-UI Pro', node_id: 'n1' }, 402)))
    const err = (await apiGet('/x').catch((e: unknown) => e)) as ApiError
    expect(err).toBeInstanceOf(ApiError)
    expect(err.status).toBe(402)
    expect(err.message).toBe('Sampling needs CH-UI Pro')
    expect(err.body).toEqual({ error: 'Sampling needs CH-UI Pro', node_id: 'n1' })
  })

  it('a 429 carries retryAfter from the body or the Retry-After header', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => json({ success: false, error: 'Too many', retryAfter: 42 }, 429)))
    expect(((await apiGet('/x').catch((e: unknown) => e)) as ApiError).retryAfter).toBe(42)
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify({ success: false, error: 'Rate limit' }), { status: 429, headers: { 'Content-Type': 'application/json', 'Retry-After': '60' } })))
    expect(((await apiGet('/x').catch((e: unknown) => e)) as ApiError).retryAfter).toBe(60)
  })
})
