import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

let databases: { name: string }[] = []
const loadDatabases = vi.fn<() => Promise<void>>()

vi.mock('../stores/schema.svelte', () => ({
  getDatabases: () => databases,
  loadDatabases: () => loadDatabases(),
}))
vi.mock('../api/query', () => ({
  fetchCompletions: vi.fn(),
  listColumns: vi.fn(),
  listTables: vi.fn(),
}))

async function freshModule() {
  vi.resetModules()
  return import('./completions')
}

describe('ensureDatabasesLoaded', () => {
  beforeEach(() => {
    databases = []
    loadDatabases.mockReset()
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-10-12T10:00:00Z'))
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('refetches after a load that left the list empty', async () => {
    const { ensureDatabasesLoaded } = await freshModule()
    loadDatabases.mockResolvedValueOnce(undefined)
    await ensureDatabasesLoaded()
    expect(loadDatabases).toHaveBeenCalledTimes(1)

    // Within the retry window: no request per keystroke.
    await ensureDatabasesLoaded()
    expect(loadDatabases).toHaveBeenCalledTimes(1)

    vi.advanceTimersByTime(6000)
    loadDatabases.mockImplementationOnce(async () => {
      databases = [{ name: 'default' }]
    })
    await ensureDatabasesLoaded()
    expect(loadDatabases).toHaveBeenCalledTimes(2)

    // Loaded now: nothing more to fetch.
    vi.advanceTimersByTime(6000)
    await ensureDatabasesLoaded()
    expect(loadDatabases).toHaveBeenCalledTimes(2)
  })

  it('recovers after a rejected load', async () => {
    const { ensureDatabasesLoaded } = await freshModule()
    loadDatabases.mockRejectedValueOnce(new Error('connection refused'))
    await ensureDatabasesLoaded()
    vi.advanceTimersByTime(6000)
    loadDatabases.mockResolvedValueOnce(undefined)
    await ensureDatabasesLoaded()
    expect(loadDatabases).toHaveBeenCalledTimes(2)
  })

  it('shares one request between concurrent callers', async () => {
    const { ensureDatabasesLoaded } = await freshModule()
    let resolve: () => void = () => {}
    loadDatabases.mockImplementationOnce(() => new Promise<void>((r) => { resolve = r }))
    const a = ensureDatabasesLoaded()
    const b = ensureDatabasesLoaded()
    resolve()
    await Promise.all([a, b])
    expect(loadDatabases).toHaveBeenCalledTimes(1)
  })
})
