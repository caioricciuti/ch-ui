import { beforeEach, describe, expect, it, vi } from 'vitest'

/** Minimal Web Storage for the node test environment. */
function memoryStorage(): Storage {
  const data = new Map<string, string>()
  return {
    get length() { return data.size },
    clear: () => data.clear(),
    getItem: (k: string) => data.get(k) ?? null,
    key: (i: number) => [...data.keys()][i] ?? null,
    removeItem: (k: string) => { data.delete(k) },
    setItem: (k: string, v: string) => { data.set(k, String(v)) },
  }
}

type Scope = typeof import('./workspace-scope')

async function freshScope(): Promise<Scope> {
  vi.resetModules()
  return import('./workspace-scope')
}

const alice = { user: 'alice@example.com', connectionId: 'conn-1' }
const aliceKey = 'alice%40example.com@conn-1'

describe('workspace scope', () => {
  beforeEach(() => {
    vi.stubGlobal('localStorage', memoryStorage())
    vi.stubGlobal('sessionStorage', memoryStorage())
  })

  it('keys scopes by encoded user and connection id', async () => {
    const ws = await freshScope()
    expect(ws.scopeKey(alice)).toBe(aliceKey)
    expect(ws.scopeKey({ user: 'a@b', connectionId: 'x' })).not.toBe(ws.scopeKey({ user: 'a', connectionId: 'b@x' }))
    expect(ws.scoped('ch-ui-tabs')).toBeNull()
    expect(ws.connectionHeaders()).toEqual({})
    ws.setWorkspaceScope(alice)
    expect(ws.scoped('ch-ui-tabs')).toBe(`ch-ui-tabs:${aliceKey}`)
    expect(ws.connectionHeaders()).toEqual({ [ws.CONNECTION_HEADER]: 'conn-1' })
  })

  it('moves legacy values into the first scope and drops the legacy keys', async () => {
    localStorage.setItem('ch-ui-tabs', '{"tabs":[]}')
    localStorage.setItem('ch-ui-pinned-results', '{"t1":[]}')
    localStorage.setItem('ch-ui-theme', 'dark')
    const ws = await freshScope()
    ws.setWorkspaceScope(alice)
    expect(localStorage.getItem(`ch-ui-tabs:${aliceKey}`)).toBe('{"tabs":[]}')
    expect(localStorage.getItem(`ch-ui-pinned-results:${aliceKey}`)).toBe('{"t1":[]}')
    expect(localStorage.getItem('ch-ui-tabs')).toBeNull()
    expect(localStorage.getItem('ch-ui-pinned-results')).toBeNull()
    // Global preferences are not scoped.
    expect(localStorage.getItem('ch-ui-theme')).toBe('dark')

    // A second person does not inherit anything.
    ws.setWorkspaceScope({ user: 'bob', connectionId: 'conn-1' })
    expect(ws.readScoped('ch-ui-tabs')).toBeNull()
  })

  it('never overwrites a scope that already has its own value', async () => {
    localStorage.setItem(`ch-ui-tabs:${aliceKey}`, 'mine')
    localStorage.setItem('ch-ui-tabs', 'legacy')
    const ws = await freshScope()
    ws.setWorkspaceScope(alice)
    expect(ws.readScoped('ch-ui-tabs')).toBe('mine')
    expect(localStorage.getItem('ch-ui-tabs')).toBeNull()
  })

  it('evicts the least recently used scope past the cap with all its keys', async () => {
    const ws = await freshScope()
    for (let i = 0; i < ws.MAX_SCOPES; i++) {
      for (const base of ws.SCOPED_KEYS) localStorage.setItem(`${base}:u${i}@c`, 'x')
      ws.touchScope(`u${i}@c`, 1000 + i)
    }
    ws.touchScope('new@c', 5000)
    const index = JSON.parse(localStorage.getItem(ws.SCOPES_INDEX_KEY) ?? '{}')
    expect(Object.keys(index)).toHaveLength(ws.MAX_SCOPES)
    expect(index['u0@c']).toBeUndefined()
    for (const base of ws.SCOPED_KEYS) {
      expect(localStorage.getItem(`${base}:u0@c`)).toBeNull()
      expect(localStorage.getItem(`${base}:u1@c`)).toBe('x')
    }
  })

  it('keeps scopes with unsaved tabs past the cap and evicts the oldest clean ones', async () => {
    const ws = await freshScope()
    // u0 has a never-saved SQL tab, u1 an edited saved query; both are the oldest.
    localStorage.setItem('ch-ui-tabs:u0@c', JSON.stringify({ tabs: [{ type: 'query', sql: 'SELECT 1', dirty: false }] }))
    localStorage.setItem('ch-ui-tabs:u1@c', JSON.stringify({ tabs: [{ type: 'query', sql: 'SELECT 2', savedQueryId: 'q1', dirty: true }] }))
    localStorage.setItem('ch-ui-tabs:u2@c', JSON.stringify({ tabs: [{ type: 'query', sql: 'SELECT 3', savedQueryId: 'q2', dirty: false }, { type: 'query', sql: '  ', dirty: false }] }))
    expect(ws.hasUnsavedTabs('u0@c')).toBe(true)
    expect(ws.hasUnsavedTabs('u1@c')).toBe(true)
    expect(ws.hasUnsavedTabs('u2@c')).toBe(false)
    for (let i = 0; i < ws.MAX_SCOPES; i++) ws.touchScope(`u${i}@c`, 1000 + i)
    ws.touchScope('new@c', 5000)
    let index = JSON.parse(localStorage.getItem(ws.SCOPES_INDEX_KEY) ?? '{}')
    expect(Object.keys(index)).toHaveLength(ws.MAX_SCOPES)
    expect(index['u0@c']).toBe(1000)
    expect(index['u1@c']).toBe(1001)
    expect(index['u2@c']).toBeUndefined()
    expect(localStorage.getItem('ch-ui-tabs:u0@c')).not.toBeNull()

    // Clean scopes keep going first; unsaved ones stay up to the hard bound.
    for (let i = 0; i < ws.MAX_SCOPES_WITH_UNSAVED; i++) {
      localStorage.setItem(`ch-ui-tabs:w${i}@c`, JSON.stringify({ tabs: [{ type: 'query', sql: 'SELECT 1' }] }))
      ws.touchScope(`w${i}@c`, 6000 + i)
    }
    index = JSON.parse(localStorage.getItem(ws.SCOPES_INDEX_KEY) ?? '{}')
    expect(Object.keys(index)).toHaveLength(ws.MAX_SCOPES_WITH_UNSAVED)
    expect(index['u0@c']).toBeUndefined()
    expect(index['new@c']).toBeUndefined()
    expect(index[`w${ws.MAX_SCOPES_WITH_UNSAVED - 1}@c`]).toBeDefined()
  })

  it('notifies listeners on a change and clears the Brain seed when leaving a scope', async () => {
    const ws = await freshScope()
    const fn = vi.fn()
    ws.onScopeChange(fn)
    ws.setWorkspaceScope(alice)
    expect(fn).toHaveBeenCalledTimes(1)
    ws.setWorkspaceScope({ ...alice })
    expect(fn).toHaveBeenCalledTimes(1)

    sessionStorage.setItem(ws.BRAIN_SEED_KEY, 'explain this')
    ws.setWorkspaceScope({ user: 'alice@example.com', connectionId: 'conn-2' })
    expect(fn).toHaveBeenCalledTimes(2)
    expect(sessionStorage.getItem(ws.BRAIN_SEED_KEY)).toBeNull()

    ws.setWorkspaceScope(null)
    expect(fn).toHaveBeenCalledTimes(3)
    expect(ws.writeScoped('ch-ui-tabs', 'x')).toBe(false)
  })
})
