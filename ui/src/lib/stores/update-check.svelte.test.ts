import { beforeEach, describe, expect, it, vi } from 'vitest'

// vitest runs without the Svelte compiler, so runes are not transformed.
vi.stubGlobal('$state', <T>(v: T): T => v)

const store = new Map<string, string>()
vi.stubGlobal('sessionStorage', {
  getItem: (k: string) => store.get(k) ?? null,
  setItem: (k: string, v: string) => void store.set(k, v),
})

const fetchMock = vi.fn<(url: string) => Promise<{ ok: boolean; json: () => Promise<unknown> }>>()
vi.stubGlobal('fetch', (url: string) => fetchMock(url))

function release(tag: string) {
  return Promise.resolve({ ok: true, json: () => Promise.resolve({ tag_name: tag }) })
}

async function freshStore() {
  vi.resetModules()
  return import('./update-check.svelte')
}

describe('update check', () => {
  beforeEach(() => {
    store.clear()
    fetchMock.mockReset()
    vi.useRealTimers()
  })

  it('uses the cached answer within the TTL', async () => {
    const m = await freshStore()
    fetchMock.mockReturnValueOnce(release('v2.24.0'))
    await m.checkForUpdate('v2.24.0')
    await m.checkForUpdate('v2.24.0')
    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(m.hasUpdate('v2.24.0')).toBe(false)
  })

  it('asks again once the cache is older than the TTL and sees the new release', async () => {
    vi.useFakeTimers()
    const m = await freshStore()
    fetchMock.mockReturnValueOnce(release('v2.24.0'))
    await m.checkForUpdate('v2.24.0')
    expect(m.hasUpdate('v2.24.0')).toBe(false)

    vi.advanceTimersByTime(m.CACHE_TTL + 1)
    fetchMock.mockReturnValueOnce(release('v2.25.0'))
    await m.checkForUpdate('v2.24.0')
    expect(fetchMock).toHaveBeenCalledTimes(2)
    expect(m.getLatestVersion()).toBe('v2.25.0')
    expect(m.hasUpdate('v2.24.0')).toBe(true)
  })

  it('keeps an hour or less between checks', async () => {
    const m = await freshStore()
    expect(m.CACHE_TTL).toBeLessThanOrEqual(60 * 60 * 1000)
  })
})
