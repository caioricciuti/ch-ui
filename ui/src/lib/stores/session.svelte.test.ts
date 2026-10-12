import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { Session } from '../types/api'

// vitest runs without the Svelte compiler; a pass-through $state keeps the
// module's plain state logic testable.
vi.stubGlobal('$state', <T>(v: T): T => v)

const checkSession = vi.fn<() => Promise<Session | null>>()
const login = vi.fn<(p: unknown) => Promise<{ session: Session }>>()
vi.mock('../api/auth', () => ({
  checkSession: () => checkSession(),
  login: (p: unknown) => login(p),
  logout: () => Promise.resolve(),
}))
vi.mock('svelte-sonner', () => ({ toast: { warning: vi.fn() } }))

type Store = typeof import('./session.svelte')

async function freshStore(): Promise<Store> {
  vi.resetModules()
  return import('./session.svelte')
}

const session = { user: 'alice', role: 'admin' } as Session

describe('session store', () => {
  beforeEach(() => {
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
})
