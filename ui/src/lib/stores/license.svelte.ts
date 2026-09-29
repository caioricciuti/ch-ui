import type { LicenseInfo } from '../types/api'
import { apiGet, apiPost } from '../api/client'

/**
 * License status as returned by /api/license, /api/license/activate and
 * /api/license/deactivate. The grace fields come from internal/license
 * (LicenseInfo.InGrace / GraceUntil): set when a Pro license has expired but
 * is still inside the read-only grace window.
 */
export interface LicenseStatus extends LicenseInfo {
  in_grace?: boolean
  grace_until?: string
}

// Single source of truth for the license. Every screen (router gating,
// sidebar, palette, Settings) reads this; nothing keeps a private copy.
let license = $state<LicenseStatus | null>(null)
let loaded = $state(false)
let loadPromise: Promise<void> | null = null
// Bumped on every write. A load that started before a write (activation,
// deactivation, a newer load) must not overwrite the newer state.
let generation = 0

export function getLicense(): LicenseStatus | null {
  return license
}

/** True once the first license request has settled (success or failure). */
export function isLicenseLoaded(): boolean {
  return loaded
}

export function isProActive(): boolean {
  return !!(license?.valid && license?.edition?.toLowerCase() === 'pro')
}

/** Expired Pro license still inside the backend's read-only grace window. */
export function isLicenseInGrace(): boolean {
  return !!(license && !license.valid && license.in_grace)
}

/** Replace the shared license state with a server response. */
export function setLicense(next: LicenseStatus | null): void {
  generation++
  license = next
  loaded = true
}

export async function loadLicense(force = false): Promise<void> {
  if (!force && license) return
  if (!force && loadPromise) {
    await loadPromise
    return
  }

  const gen = ++generation
  const p: Promise<void> = apiGet<LicenseStatus>('/api/license')
    .then((res) => {
      if (gen === generation) license = res
    })
    .catch(() => {
      if (gen === generation) license = null
    })
    .finally(() => {
      loaded = true
      if (loadPromise === p) loadPromise = null
    })
  loadPromise = p

  await p
}

/** Activate a signed license. Throws the API error on failure. */
export async function activateLicense(licenseText: string): Promise<LicenseStatus> {
  const res = await apiPost<LicenseStatus>('/api/license/activate', { license: licenseText })
  setLicense(res)
  return res
}

/** Remove the stored license. Throws the API error on failure. */
export async function deactivateLicense(): Promise<LicenseStatus> {
  const res = await apiPost<LicenseStatus>('/api/license/deactivate')
  setLicense(res)
  return res
}
