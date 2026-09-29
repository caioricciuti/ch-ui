import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { LicenseStatus } from './license.svelte'

// vitest runs without the Svelte compiler, so runes are not transformed.
// A pass-through $state keeps the module's plain state logic testable.
vi.stubGlobal('$state', <T>(v: T): T => v)

const apiGet = vi.fn<(url: string) => Promise<unknown>>()
const apiPost = vi.fn<(url: string, data?: unknown) => Promise<unknown>>()
vi.mock('../api/client', () => ({
  apiGet: (url: string) => apiGet(url),
  apiPost: (url: string, data?: unknown) => apiPost(url, data),
}))

type Store = typeof import('./license.svelte')

const community: LicenseStatus = { edition: 'community', valid: false }
const pro: LicenseStatus = { edition: 'pro', valid: true, customer: 'Acme', license_id: 'lic_1', expires_at: '2099-01-01T00:00:00Z' }

async function freshStore(): Promise<Store> {
  vi.resetModules()
  return import('./license.svelte')
}

describe('license store', () => {
  beforeEach(() => {
    apiGet.mockReset()
    apiPost.mockReset()
  })

  it('activation flips the state the router reads, without a reload', async () => {
    const store = await freshStore()
    apiGet.mockResolvedValue(community)
    await store.loadLicense()
    expect(store.isLicenseLoaded()).toBe(true)
    expect(store.isProActive()).toBe(false)

    apiPost.mockResolvedValue(pro)
    await store.activateLicense('{"signed":"json"}')
    expect(apiPost).toHaveBeenCalledWith('/api/license/activate', { license: '{"signed":"json"}' })
    expect(store.isProActive()).toBe(true)
    expect(store.getLicense()).toEqual(pro)

    // A later non-forced load (another component mounting) keeps the new state.
    await store.loadLicense()
    expect(apiGet).toHaveBeenCalledTimes(1)
    expect(store.isProActive()).toBe(true)
  })

  it('deactivation locks Pro again', async () => {
    const store = await freshStore()
    apiGet.mockResolvedValue(pro)
    await store.loadLicense()
    expect(store.isProActive()).toBe(true)

    apiPost.mockResolvedValue(community)
    await store.deactivateLicense()
    expect(apiPost).toHaveBeenCalledWith('/api/license/deactivate', undefined)
    expect(store.isProActive()).toBe(false)
  })

  it('a failed activation leaves the state unchanged', async () => {
    const store = await freshStore()
    apiGet.mockResolvedValue(community)
    await store.loadLicense()

    apiPost.mockRejectedValue(new Error('Invalid or expired license'))
    await expect(store.activateLicense('bad')).rejects.toThrow('Invalid or expired license')
    expect(store.isProActive()).toBe(false)
    expect(store.getLicense()).toEqual(community)
  })

  it('a stale load that resolves after activation does not overwrite it', async () => {
    const store = await freshStore()
    let resolveGet: (v: LicenseStatus) => void = () => {}
    apiGet.mockReturnValue(new Promise<LicenseStatus>((r) => { resolveGet = r }))
    const pending = store.loadLicense()

    apiPost.mockResolvedValue(pro)
    await store.activateLicense('{}')
    resolveGet(community)
    await pending

    expect(store.isProActive()).toBe(true)
  })

  it('reports the backend grace window', async () => {
    const store = await freshStore()
    apiGet.mockResolvedValue({
      edition: 'pro',
      valid: false,
      license_id: 'lic_1',
      expires_at: '2026-09-20T00:00:00Z',
      in_grace: true,
      grace_until: '2026-10-04T00:00:00Z',
    } satisfies LicenseStatus)
    await store.loadLicense()
    expect(store.isProActive()).toBe(false)
    expect(store.isLicenseInGrace()).toBe(true)
  })
})
