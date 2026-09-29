// SPDX-License-Identifier: BUSL-1.1
import { apiGet, apiPut } from './client'

const BASE = '/api/admin/sso'

/** Secret-free view of the SSO (OIDC) config. The client secret is never returned. */
export interface SSOConfigView {
  enabled: boolean
  issuer_url: string
  client_id: string
  has_secret: boolean
  redirect_base_url: string
  redirect_url: string
  admin_groups: string[]
  analyst_groups: string[]
  viewer_groups: string[]
  allowed_domains: string[]
}

export interface SSOSettings {
  /** True when OIDC is set via env/YAML — env wins, the UI is read-only. */
  env_managed: boolean
  /** True when an OIDC provider is currently initialized and serving logins. */
  active: boolean
  config: SSOConfigView
  /** Set when a save persisted but the provider could not be re-initialized. */
  reload_error?: string
}

/** PUT body. Leave client_secret blank to keep the stored secret. */
export interface SSOUpdateRequest {
  enabled: boolean
  issuer_url: string
  client_id: string
  client_secret?: string
  redirect_base_url: string
  admin_groups: string[]
  analyst_groups: string[]
  viewer_groups: string[]
  allowed_domains: string[]
}

export function fetchSSOSettings() {
  return apiGet<SSOSettings>(BASE)
}

export function updateSSOSettings(req: SSOUpdateRequest) {
  return apiPut<SSOSettings>(BASE, req)
}
