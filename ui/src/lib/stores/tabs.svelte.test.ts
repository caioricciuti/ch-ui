import { beforeEach, describe, expect, it, vi } from 'vitest'

// vitest runs without the Svelte compiler; a pass-through $state keeps the
// module's plain state logic testable.
vi.stubGlobal('$state', <T>(v: T): T => v)
vi.mock('./router.svelte', () => ({ pushTabRouteForTab: () => {} }))
vi.mock('../routes', () => ({ isPageRouteType: () => false }))

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

const alice = { user: 'alice', connectionId: 'conn-1' }
const bob = { user: 'bob', connectionId: 'conn-1' }

function savedTabs(name: string): string {
  const tab = { id: `t-${name}`, type: 'query', name, sql: `SELECT '${name}'`, dirty: false, baseSql: `SELECT '${name}'` }
  return JSON.stringify({
    tabs: [{ id: 'home', type: 'home', name: 'Home' }, tab],
    groups: [{ id: 'left', tabIds: ['home', tab.id], activeTabId: tab.id }],
    focusedGroupId: 'left',
  })
}

async function load() {
  vi.resetModules()
  const ws = await import('./workspace-scope')
  return { ws, tabs: async () => import('./tabs.svelte') }
}

describe('tabs per scope', () => {
  beforeEach(() => {
    vi.stubGlobal('localStorage', memoryStorage())
    vi.stubGlobal('sessionStorage', memoryStorage())
  })

  it('loads the signed-in scope and rehydrates on a scope change', async () => {
    const { ws, tabs: importTabs } = await load()
    localStorage.setItem(`ch-ui-tabs:${ws.scopeKey(alice)}`, savedTabs('Alice query'))
    localStorage.setItem(`ch-ui-tabs:${ws.scopeKey(bob)}`, savedTabs('Bob query'))
    ws.setWorkspaceScope(alice)
    const tabs = await importTabs()
    expect(tabs.getTabs().map((t) => t.name)).toEqual(['Home', 'Alice query'])

    ws.setWorkspaceScope(bob)
    expect(tabs.getTabs().map((t) => t.name)).toEqual(['Home', 'Bob query'])

    ws.setWorkspaceScope(null)
    expect(tabs.getTabs().map((t) => t.name)).toEqual(['Home'])
  })

  it('starts empty without a scope and picks the workspace up at sign-in', async () => {
    const { ws, tabs: importTabs } = await load()
    localStorage.setItem('ch-ui-tabs', savedTabs('Legacy query'))
    const tabs = await importTabs()
    expect(tabs.getTabs().map((t) => t.name)).toEqual(['Home'])

    ws.setWorkspaceScope(alice)
    expect(tabs.getTabs().map((t) => t.name)).toEqual(['Home', 'Legacy query'])
    expect(localStorage.getItem('ch-ui-tabs')).toBeNull()
  })

  it('saves under the active scope only', async () => {
    const { ws, tabs: importTabs } = await load()
    ws.setWorkspaceScope(alice)
    const tabs = await importTabs()
    tabs.openQueryTab('SELECT 1')
    await Promise.resolve()
    const saved = JSON.parse(localStorage.getItem(`ch-ui-tabs:${ws.scopeKey(alice)}`) ?? '{}')
    expect(saved.tabs).toHaveLength(2)
    expect(localStorage.getItem(`ch-ui-tabs:${ws.scopeKey(bob)}`)).toBeNull()
  })
})
