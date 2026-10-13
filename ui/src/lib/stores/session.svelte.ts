import { toast } from 'svelte-sonner'
import type { Session } from '../types/api'
import { checkSession, login as apiLogin, logout as apiLogout } from '../api/auth'

let session = $state<Session | null>(null)
// Startup only: the first session check. App shows its spinner while this
// is true. A sign-in must never flip it, or App unmounts the Login form (or
// the whole Shell, for Fleet's sign-in sheet) and the typed values are lost.
let initializing = $state(true)
// A sign-in request in flight. Forms show their own busy state from this.
let submitting = $state(false)
let error = $state<string | null>(null)

export function getSession(): Session | null {
  return session
}

/** True until the startup session check finishes. */
export function isInitializing(): boolean {
  return initializing
}

/** True while login() is waiting for the server. */
export function isSubmitting(): boolean {
  return submitting
}

export function getError(): string | null {
  return error
}

export function isAuthenticated(): boolean {
  return session !== null
}

/** Initialize session from server cookie */
export async function initSession(): Promise<void> {
  initializing = true
  error = null
  try {
    session = await checkSession()
  } catch {
    session = null
  } finally {
    initializing = false
  }
}

/** Log in and set session */
export async function login(connectionId: string, username: string, password: string, clickhouseUrl?: string): Promise<void> {
  error = null
  submitting = true
  try {
    const res = await apiLogin({ connectionId, username, password, clickhouseUrl })
    session = res.session
    if (session?.roleNote) {
      toast.warning(`Signed in as ${session.role}`, { description: session.roleNote, duration: 12000 })
    }
  } catch (e: unknown) {
    error = e instanceof Error && e.message ? e.message : 'Login failed'
    throw e
  } finally {
    submitting = false
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
