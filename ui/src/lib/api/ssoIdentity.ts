// SPDX-License-Identifier: BUSL-1.1
import { apiGet, apiPut, apiPost, apiDel } from './client'

// Per-connection SSO identity: either every SSO person queries through one
// shared ClickHouse service account, or (Pro) each person gets their own
// ClickHouse user provisioned from IdP groups.

export type SSOIdentityMode = 'shared' | 'per_person'

export interface SSORoleMapping {
  group: string
  roles: string[]
}

export interface SSOIdentityConfig {
  mode: SSOIdentityMode
  /** Prefix for provisioned ClickHouse user names. Default "sso:". */
  user_prefix: string
  /** Settings profile applied to provisioned users; empty for none. */
  settings_profile: string
  /** "auto", "any", or a comma-separated list of IPs. */
  host: string
  role_mappings: SSORoleMapping[]
  /** Roles granted when no group matches. Empty refuses sign-in. */
  fallback_roles: string[]
  /** Lock a provisioned user after this many days without sign-in. 0 = never. */
  lock_after_days: number
  /** Drop a provisioned user after this many days without sign-in. 0 = never. */
  drop_after_days: number
}

export interface SSOIdentityResponse {
  config: SSOIdentityConfig
  /** ClickHouse account used to provision users (per-person) or to run queries (shared). */
  provisioner_user: string
  provisioner_set: boolean
}

export interface SSOAccountRequest {
  username: string
  password: string
}

export interface SSOSetupSQLRequest {
  config: SSOIdentityConfig
  provisioner_user: string
}

export interface SSOSetupSQLResponse {
  sql: string
}

export interface SSOVerifyRequest {
  config?: SSOIdentityConfig
  provisioner?: SSOAccountRequest
}

export interface SSOVerifyCheck {
  name: string
  ok: boolean
  detail: string
}

export interface SSOVerifyResponse {
  ok: boolean
  checks: SSOVerifyCheck[]
}

export interface SSORemoveAccessFailure {
  connection_id: string
  connection_name: string
  error: string
}

export interface SSORemoveAccessResponse {
  dropped: number
  failed: SSORemoveAccessFailure[]
  sessions_ended: number
}

/** One provisioned ClickHouse user of an SSO person, as listed by GET /api/admin/users. */
export interface SSOClickHouseUser {
  connection_id: string
  connection_name: string
  ch_user: string
  roles: string[]
  last_sync_at: string
  last_seen_at: string
  locked: boolean
}

export function defaultSSOIdentityConfig(): SSOIdentityConfig {
  return {
    mode: 'shared',
    user_prefix: 'sso:',
    settings_profile: '',
    host: 'auto',
    role_mappings: [],
    fallback_roles: [],
    lock_after_days: 0,
    drop_after_days: 0,
  }
}

function base(connectionId: string): string {
  return `/api/connections/${encodeURIComponent(connectionId)}`
}

export function fetchSSOIdentity(connectionId: string) {
  return apiGet<SSOIdentityResponse>(`${base(connectionId)}/sso-identity`)
}

export function updateSSOIdentity(connectionId: string, config: SSOIdentityConfig) {
  return apiPut<SSOIdentityResponse>(`${base(connectionId)}/sso-identity`, config)
}

/** Copies another connection's SSO settings and account (the password stays on the server). */
export function copySSOIdentity(connectionId: string, sourceId: string) {
  return apiPost<SSOIdentityResponse>(`${base(connectionId)}/sso-identity/copy-from`, { source_id: sourceId })
}

export function updateSSOAccount(connectionId: string, account: SSOAccountRequest) {
  return apiPut<{ success: boolean }>(`${base(connectionId)}/sso-account`, account)
}

export function fetchSSOSetupSQL(connectionId: string, req: SSOSetupSQLRequest) {
  return apiPost<SSOSetupSQLResponse>(`${base(connectionId)}/sso-identity/setup-sql`, req)
}

export function verifySSOIdentity(connectionId: string, req: SSOVerifyRequest) {
  return apiPost<SSOVerifyResponse>(`${base(connectionId)}/sso-identity/verify`, req)
}

/** Drops the person's ClickHouse users on every connection and ends their sessions. */
export function removeSSOAccess(email: string) {
  return apiDel<SSORemoveAccessResponse>(`/api/admin/users/sso/${encodeURIComponent(email)}/access`)
}
