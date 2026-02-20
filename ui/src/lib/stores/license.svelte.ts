import type { LicenseInfo } from '../types/api'
import { apiGet } from '../api/client'

let license = $state<LicenseInfo | null>(null)
let loading = $state(false)
let loadPromise: Promise<void> | null = null

export function getLicense(): LicenseInfo | null {
  return license
}

export function isLicenseLoading(): boolean {
  return loading
}

export function isProActive(): boolean {
  return !!(license?.valid && license?.edition?.toLowerCase() === 'pro')
}

export async function loadLicense(force = false): Promise<void> {
  if (!force && license) return
  if (loadPromise) {
    await loadPromise
    return
  }

  loading = true
  loadPromise = apiGet<LicenseInfo>('/api/license')
    .then((res) => {
      license = res
    })
    .catch(() => {
      license = null
    })
    .finally(() => {
      loading = false
      loadPromise = null
    })

  await loadPromise
}
