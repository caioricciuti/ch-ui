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
  /** Accept ID tokens with no email_verified claim. An explicit false is always refused. */
  accept_unverified_email: boolean
  /** Turn off password sign-in while SSO is up. */
  sso_only: boolean
}

export interface SSOSettings {
  /** True when OIDC is set via env/YAML — env wins, the UI is read-only. */
  env_managed: boolean
  /** True when an OIDC provider is currently initialized and serving logins. */
  active: boolean
  config: SSOConfigView
  /** Set when a save persisted but the provider could not be re-initialized. */
  reload_error?: string
  /** OIDC_SSO_ONLY / oidc_sso_only turns SSO only on from the server config; the toggle is locked. */
  sso_only_forced: boolean
  /** Password sign-in is off right now (SSO only set, provider loaded, no break-glass). */
  sso_only_enforced: boolean
  /** CHUI_ALLOW_PASSWORD_LOGIN keeps password sign-in on whatever SSO only says. */
  break_glass: boolean
  /** The caller signed in through SSO, which turning SSO only on requires. */
  via_sso: boolean
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
  accept_unverified_email: boolean
  sso_only: boolean
}

export function fetchSSOSettings() {
  return apiGet<SSOSettings>(BASE)
}

export function updateSSOSettings(req: SSOUpdateRequest) {
  return apiPut<SSOSettings>(BASE, req)
}
