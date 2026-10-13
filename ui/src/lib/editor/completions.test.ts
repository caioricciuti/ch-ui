import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

let databases: { name: string }[] = []
const loadDatabases = vi.fn<() => Promise<void>>()

vi.mock('../stores/schema.svelte', () => ({
  getDatabases: () => databases,
  loadDatabases: () => loadDatabases(),
}))
const listTables = vi.fn<(db: string) => Promise<string[]>>()
const fetchCompletions = vi.fn<() => Promise<{ functions: string[]; keywords: string[] }>>()

vi.mock('../api/query', () => ({
  fetchCompletions: () => fetchCompletions(),
  listColumns: vi.fn(),
  listTables: (db: string) => listTables(db),
}))
vi.mock('../stores/session.svelte', () => ({
  getSession: () => null,
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

describe('table cache', () => {
  beforeEach(() => {
    databases = []
    listTables.mockReset()
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-10-12T10:00:00Z'))
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('does not cache a failed fetch and retries after the backoff', async () => {
    const { ensureTablesCached, findTablesForDatabase, hasTablesCached } = await freshModule()
    listTables.mockRejectedValueOnce(new Error('timeout'))
    await ensureTablesCached('db1')
    expect(hasTablesCached('db1')).toBe(false)

    await ensureTablesCached('db1')
    expect(listTables).toHaveBeenCalledTimes(1)

    vi.advanceTimersByTime(6000)
    listTables.mockResolvedValueOnce(['t1'])
    await ensureTablesCached('db1')
    expect(findTablesForDatabase('db1')).toEqual(['t1'])
  })

  it('invalidateCompletionCaches drops cached tables', async () => {
    const { ensureTablesCached, findTablesForDatabase, invalidateCompletionCaches } = await freshModule()
    listTables.mockResolvedValueOnce(['old'])
    await ensureTablesCached('db1')
    invalidateCompletionCaches()
    expect(findTablesForDatabase('db1')).toEqual([])
    listTables.mockResolvedValueOnce(['new'])
    await ensureTablesCached('db1')
    expect(findTablesForDatabase('db1')).toEqual(['new'])
  })

  it('loads all databases in parallel and stops waiting at the timeout', async () => {
    databases = [{ name: 'fast' }, { name: 'slow' }]
    const { ensureAllTablesCached, findTablesForDatabase } = await freshModule()
    listTables.mockImplementation((db: string) =>
      db === 'fast' ? Promise.resolve(['a']) : new Promise((r) => setTimeout(() => r(['b']), 10_000)),
    )
    const done = ensureAllTablesCached(1000)
    expect(listTables).toHaveBeenCalledTimes(2)
    await vi.advanceTimersByTimeAsync(1000)
    await done
    expect(findTablesForDatabase('fast')).toEqual(['a'])
    expect(findTablesForDatabase('slow')).toEqual([])
    await vi.advanceTimersByTimeAsync(10_000)
    expect(findTablesForDatabase('slow')).toEqual(['b'])
  })
})

describe('alias and CTE detection', () => {
  it('never takes a keyword for an alias', async () => {
    const { buildAliasMap } = await freshModule()
    const doc = 'SELECT * FROM db.t WHERE x = 1; SELECT * FROM a FINAL; SELECT * FROM b SAMPLE 0.1; SELECT * FROM c PREWHERE 1; SELECT * FROM d ARRAY JOIN e'
    expect([...buildAliasMap(doc).keys()]).toEqual([])
  })

  it('finds real aliases, with or without AS, and ignores strings and comments', async () => {
    const { buildAliasMap } = await freshModule()
    const doc = "SELECT 'FROM fake f' FROM db.t AS x JOIN u y ON x.id = y.id -- FROM other o"
    expect(Object.fromEntries(buildAliasMap(doc))).toEqual({ x: 'db.t', y: 'u' })
  })

  it('lists CTE names', async () => {
    const { findCteNames } = await freshModule()
    expect(findCteNames('WITH a AS (SELECT 1), b AS (SELECT 2) SELECT * FROM a')).toEqual(['a', 'b'])
    expect(findCteNames("SELECT 'WITH z AS (' ")).toEqual([])
  })
})
