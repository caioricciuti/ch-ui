import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { Session } from '../types/api'

// vitest runs without the Svelte compiler; a pass-through $state keeps the
// module's plain state logic testable.
function stubGlobals(): void {
  vi.stubGlobal('$state', <T>(v: T): T => v)
  // Node's real BroadcastChannel would deliver between test module copies.
  vi.stubGlobal('BroadcastChannel', FakeChannel)
}

const posted: unknown[] = []
const channels: FakeChannel[] = []
class FakeChannel {
  onmessage: ((e: MessageEvent) => void) | null = null
  constructor(readonly name: string) { channels.push(this) }
  postMessage(m: unknown): void { posted.push(m) }
  close(): void {}
}
stubGlobals()

const checkSession = vi.fn<() => Promise<Session | null>>()
const login = vi.fn<(p: unknown) => Promise<{ session: Session }>>()
const switchConnection = vi.fn<(p: unknown) => Promise<unknown>>()
vi.mock('../api/auth', () => ({
  checkSession: () => checkSession(),
  login: (p: unknown) => login(p),
  logout: () => Promise.resolve(),
  switchConnection: (p: unknown) => switchConnection(p),
}))
vi.mock('svelte-sonner', () => ({ toast: { warning: vi.fn() } }))

type Store = typeof import('./session.svelte')

async function freshStore(): Promise<Store> {
  vi.resetModules()
  return import('./session.svelte')
}

const session = { user: 'alice', role: 'admin', connectionId: 'conn-1' } as Session

describe('session store', () => {
  afterEach(() => vi.unstubAllGlobals())

  beforeEach(() => {
    stubGlobals()
    posted.length = 0
    channels.length = 0
    checkSession.mockReset()
    login.mockReset()
  })

  it('initializing covers only the startup check', async () => {
    const store = await freshStore()
    expect(store.isInitializing()).toBe(true)
    checkSession.mockResolvedValue(null)
    await store.initSession()
    expect(store.isInitializing()).toBe(false)
  })

  it('a failed sign-in never flips initializing, so App keeps the Login form mounted', async () => {
    const store = await freshStore()
    checkSession.mockResolvedValue(null)
    await store.initSession()

    let release: (err: Error) => void = () => {}
    login.mockReturnValue(new Promise((_, reject) => { release = reject }))
    const pending = store.login('conn', 'alice', 'wrong')
    expect(store.isSubmitting()).toBe(true)
    expect(store.isInitializing()).toBe(false)

    release(new Error('Invalid credentials'))
    await expect(pending).rejects.toThrow('Invalid credentials')
    expect(store.isSubmitting()).toBe(false)
    expect(store.isInitializing()).toBe(false)
    expect(store.getError()).toBe('Invalid credentials')
    expect(store.isAuthenticated()).toBe(false)
  })

  it('a successful sign-in sets the session', async () => {
    const store = await freshStore()
    checkSession.mockResolvedValue(null)
    await store.initSession()
    login.mockResolvedValue({ session })
    await store.login('conn', 'alice', 'right')
    expect(store.isAuthenticated()).toBe(true)
    expect(store.isSubmitting()).toBe(false)
  })

  it('signing in to another scope on a page that showed one reloads instead', async () => {
    const assign = vi.fn()
    vi.stubGlobal('window', { location: { assign, pathname: '/login', search: '?next=%2Fx' } })
    const store = await freshStore()
    checkSession.mockResolvedValue(session)
    await store.initSession()
    await store.logout()
    login.mockResolvedValue({ session: { ...session, user: 'bob' } })
    await store.login('conn-1', 'bob', 'pw')
    expect(assign).toHaveBeenCalledWith('/login?next=%2Fx')
    expect(store.isAuthenticated()).toBe(false)
  })

  it('the same person signing back in keeps the page', async () => {
    const assign = vi.fn()
    vi.stubGlobal('window', { location: { assign, pathname: '/login', search: '' } })
    const store = await freshStore()
    checkSession.mockResolvedValue(session)
    await store.initSession()
    await store.logout()
    login.mockResolvedValue({ session })
    await store.login('conn-1', 'alice', 'pw')
    expect(assign).not.toHaveBeenCalled()
    expect(store.isAuthenticated()).toBe(true)
  })

  it('a successful switch reloads at the app root; a failed one does not', async () => {
    const assign = vi.fn()
    vi.stubGlobal('window', { location: { assign, pathname: '/', search: '' } })
    const store = await freshStore()
    switchConnection.mockRejectedValueOnce(new Error('Invalid credentials'))
    await expect(store.switchConnection({ connectionId: 'conn-2' })).rejects.toThrow('Invalid credentials')
    expect(assign).not.toHaveBeenCalled()
    switchConnection.mockResolvedValueOnce({})
    await store.switchConnection({ connectionId: 'conn-2' })
    expect(assign).toHaveBeenCalledWith('/')
    expect(posted).toContainEqual({ type: 'switch' })
  })

  it('a switch marks this tab as switching until it fails', async () => {
    vi.stubGlobal('window', { location: { assign: vi.fn(), pathname: '/', search: '' } })
    const store = await freshStore()
    const cc = await import('../api/connection-changed')
    let release: (err: Error) => void = () => {}
    switchConnection.mockReturnValueOnce(new Promise((_, reject) => { release = reject }))
    const pending = store.switchConnection({ connectionId: 'conn-2' })
    expect(cc.isConnectionSwitching()).toBe(true)
    release(new Error('offline'))
    await expect(pending).rejects.toThrow('offline')
    expect(cc.isConnectionSwitching()).toBe(false)

    switchConnection.mockResolvedValueOnce({})
    await store.switchConnection({ connectionId: 'conn-2' })
    expect(cc.isConnectionSwitching()).toBe(true)
  })

  it('another tab changing the session shows the notice instead of reloading', async () => {
    const reload = vi.fn()
    vi.stubGlobal('window', { location: { reload, pathname: '/', search: '' } })
    const store = await freshStore()
    checkSession.mockResolvedValue(session)
    await store.initSession()
    expect(channels).toHaveLength(1)
    channels[0].onmessage?.({ data: { type: 'switch' } } as MessageEvent)
    expect(store.getStaleSession()).toBe('changed')
    store.dismissStaleSession()
    expect(store.getStaleSession()).toBeNull()
    channels[0].onmessage?.({ data: { type: 'logout' } } as MessageEvent)
    expect(store.getStaleSession()).toBe('logout')
    // A later change does not hide that the session is gone.
    channels[0].onmessage?.({ data: { type: 'login' } } as MessageEvent)
    expect(store.getStaleSession()).toBe('logout')
    expect(reload).not.toHaveBeenCalled()
  })

  it('a signed-out tab still reloads, it has no work to lose', async () => {
    const reload = vi.fn()
    vi.stubGlobal('window', { location: { reload, pathname: '/', search: '' } })
    const store = await freshStore()
    checkSession.mockResolvedValue(null)
    await store.initSession()
    channels[0].onmessage?.({ data: { type: 'login' } } as MessageEvent)
    expect(reload).toHaveBeenCalledTimes(1)
    expect(store.getStaleSession()).toBeNull()
  })

  it('a connection_changed answer shows the notice', async () => {
    const store = await freshStore()
    const cc = await import('../api/connection-changed')
    checkSession.mockResolvedValue(session)
    await store.initSession()
    expect(cc.connectionChangedMessage(409, { error: 'connection_changed' })).toBe(cc.CONNECTION_CHANGED_MESSAGE)
    expect(store.getStaleSession()).toBe('changed')
  })

  it('refreshes the online state of the same connection only', async () => {
    const store = await freshStore()
    checkSession.mockResolvedValue({ ...session, connectionOnline: true })
    await store.initSession()
    checkSession.mockResolvedValue({ ...session, connectionOnline: false })
    await store.refreshConnectionOnline()
    expect(store.getSession()?.connectionOnline).toBe(false)
    checkSession.mockResolvedValue({ ...session, connectionId: 'conn-2', connectionOnline: true })
    await store.refreshConnectionOnline()
    expect(store.getSession()?.connectionOnline).toBe(false)
    expect(store.getSession()?.connectionId).toBe('conn-1')
  })
})
