import type { Session } from '../types/api'
import { checkSession, login as apiLogin, logout as apiLogout } from '../api/auth'

let session = $state<Session | null>(null)
let loading = $state(true)
let error = $state<string | null>(null)

export function getSession(): Session | null {
  return session
}

export function isLoading(): boolean {
  return loading
}

export function getError(): string | null {
  return error
}

export function isAuthenticated(): boolean {
  return session !== null
}

/** Initialize session from server cookie */
export async function initSession(): Promise<void> {
  loading = true
  error = null
  try {
    session = await checkSession()
  } catch (e) {
    session = null
  } finally {
    loading = false
  }
}

/** Log in and set session */
export async function login(connectionId: string, username: string, password: string): Promise<void> {
  error = null
  loading = true
  try {
    const res = await apiLogin({ connectionId, username, password })
    session = res.session
  } catch (e: any) {
    error = e.message || 'Login failed'
    throw e
  } finally {
    loading = false
  }
}

/** Log out and clear session */
export async function logout(): Promise<void> {
  try {
    await apiLogout()
  } finally {
    session = null
  }
}
