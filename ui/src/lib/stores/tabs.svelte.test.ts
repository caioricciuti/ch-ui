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

describe('running queries', () => {
  beforeEach(() => {
    vi.stubGlobal('localStorage', memoryStorage())
    vi.stubGlobal('sessionStorage', memoryStorage())
  })

  async function setup() {
    const { ws, tabs: importTabs } = await load()
    localStorage.setItem(`ch-ui-tabs:${ws.scopeKey(alice)}`, savedTabs('Alice query'))
    ws.setWorkspaceScope(alice)
    const tabs = await importTabs()
    const { queryRuns } = await import('../editor/query-runs')
    return { ws, tabs, queryRuns }
  }

  it('closing a tab stops its run', async () => {
    const { tabs, queryRuns } = await setup()
    const cancel = vi.fn()
    const other = vi.fn()
    queryRuns.start('t-Alice query', cancel, 'r1')
    queryRuns.start('elsewhere', other, 'r2')
    tabs.closeTab('t-Alice query')
    expect(cancel).toHaveBeenCalledOnce()
    expect(other).not.toHaveBeenCalled()
  })

  it('closing a result stops the run filling it, not another one', async () => {
    const { tabs, queryRuns } = await setup()
    const cancel = vi.fn()
    queryRuns.start('t-Alice query', cancel, 'r1')
    tabs.closeTabResult('t-Alice query', 'r0')
    expect(cancel).not.toHaveBeenCalled()
    tabs.closeTabResult('t-Alice query', 'r1')
    expect(cancel).toHaveBeenCalledOnce()
  })

  it('switching connection stops every run', async () => {
    const { ws, queryRuns } = await setup()
    const a = vi.fn()
    const b = vi.fn()
    queryRuns.start('t-Alice query', a, 'r1')
    queryRuns.start('t-other', b, 'r2')
    ws.setWorkspaceScope({ user: 'alice', connectionId: 'conn-2' })
    expect(a).toHaveBeenCalledOnce()
    expect(b).toHaveBeenCalledOnce()
  })
})
