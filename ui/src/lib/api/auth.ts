import { apiGet, apiPost } from './client'
import type { Session, Connection } from '../types/api'

interface LoginParams {
  connectionId: string
  username: string
  password: string
}

interface LoginResponse {
  success: boolean
  session: Session
}

interface ConnectionsResponse {
  success: boolean
  connections: Connection[]
}

interface SessionResponse {
  success: boolean
  session: Session
}

/** Log in to a ClickHouse connection */
export function login(params: LoginParams): Promise<LoginResponse> {
  return apiPost<LoginResponse>('/api/auth/login', params)
}

/** Log out and destroy the session */
export function logout(): Promise<void> {
  return apiPost('/api/auth/logout')
}

/** Check if a valid session exists */
export async function checkSession(): Promise<Session | null> {
  try {
    const res = await apiGet<{ authenticated: boolean; session?: Session }>('/api/auth/session')
    if (!res.authenticated) return null
    return res.session ?? null
  } catch {
    return null
  }
}

/** List all connections (with online/offline status) */
export async function listConnections(): Promise<Connection[]> {
  const res = await apiGet<ConnectionsResponse>('/api/auth/connections')
  return res.connections ?? []
}
