import { apiGet, apiPost } from './client'

export interface OAuthConsentInfo {
  success: boolean
  client_name: string
  client_id: string
  redirect_host: string
  scopes: string[]
  connection: string
  user: string
  expires_at: string
}

export function getOAuthConsent(requestId: string): Promise<OAuthConsentInfo> {
  return apiGet<OAuthConsentInfo>(`/api/oauth/consent/${encodeURIComponent(requestId)}`)
}

export function approveOAuthConsent(requestId: string): Promise<{ success: boolean; redirect_url: string }> {
  return apiPost(`/api/oauth/consent/${encodeURIComponent(requestId)}/approve`)
}

export function denyOAuthConsent(requestId: string): Promise<{ success: boolean; redirect_url: string }> {
  return apiPost(`/api/oauth/consent/${encodeURIComponent(requestId)}/deny`)
}
