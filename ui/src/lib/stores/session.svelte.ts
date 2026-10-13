import { toast } from 'svelte-sonner'
import type { Session } from '../types/api'
import {
  checkSession,
  login as apiLogin,
  logout as apiLogout,
  switchConnection as apiSwitchConnection,
  type SwitchConnectionParams,
} from '../api/auth'
import { stripBase, withBase } from '../basePath'
import { BRAIN_SEED_KEY, scopeKey, setWorkspaceScope } from './workspace-scope'

let session = $state<Session | null>(null)
// Startup only: the first session check. App shows its spinner while this
// is true. A sign-in must never flip it, or App unmounts the Login form (or
// the whole Shell, for Fleet's sign-in sheet) and the typed values are lost.
let initializing = $state(true)
// A sign-in request in flight. Forms show their own busy state from this.
let submitting = $state(false)
let error = $state<string | null>(null)
// The user@connection scope this page has shown. In-memory stores (schema,
// Brain and palette lists) hold its data, so signing in to a different scope
// reloads the page instead of mixing the two.
let pageScope: string | null = null

// Tells the other tabs of this browser that the session changed under them:
// they reload, so none keeps working on a connection or person that is gone.
const CHANNEL_NAME = 'ch-ui-session'
let channel: BroadcastChannel | null = null

function sessionChannel(): BroadcastChannel | null {
  if (channel || typeof window === 'undefined' || typeof BroadcastChannel === 'undefined') return channel
  try {
    channel = new BroadcastChannel(CHANNEL_NAME)
    channel.onmessage = () => window.location.reload()
  } catch {
    channel = null
  }
  return channel
}

function announce(type: 'login' | 'logout' | 'switch'): void {
  try {
    sessionChannel()?.postMessage({ type })
  } catch {
    // closed channel: the connection header still catches stale tabs
  }
}

/** A Brain prompt handed over from the palette belongs to the old workspace. */
function dropBrainSeed(): void {
  try { sessionStorage.removeItem(BRAIN_SEED_KEY) } catch { /* unavailable */ }
}

function applyScope(s: Session | null): void {
  setWorkspaceScope(s ? { user: s.user, connectionId: s.connectionId } : null)
}

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
  sessionChannel()
  try {
    session = await checkSession()
  } catch {
    session = null
  } finally {
    if (session) pageScope = scopeKey(session)
    applyScope(session)
    initializing = false
  }
}

/**
 * Log in and set session. When this page already showed a different
 * user@connection, it reloads (at reloadTo, else the current URL) instead of
 * setting the session, so nothing of the previous workspace stays in memory.
 */
export async function login(connectionId: string, username: string, password: string, clickhouseUrl?: string, reloadTo?: string): Promise<void> {
  error = null
  submitting = true
  try {
    const res = await apiLogin({ connectionId, username, password, clickhouseUrl })
    const nextScope = scopeKey(res.session)
    announce('login')
    if (pageScope !== null && pageScope !== nextScope) {
      // Stay "submitting" until the page goes away.
      dropBrainSeed()
      window.location.assign(withBase(reloadTo ?? stripBase(window.location.pathname) + window.location.search))
      return
    }
    session = res.session
    pageScope = nextScope
    applyScope(session)
    if (session?.roleNote) {
      toast.warning(`Signed in as ${session.role}`, { description: session.roleNote, duration: 12000 })
    }
    submitting = false
  } catch (e: unknown) {
    error = e instanceof Error && e.message ? e.message : 'Login failed'
    submitting = false
    throw e
  }
}

/**
 * Move the session to another connection, then load the app fresh on it. The
 * new scope restores that connection's own tabs. Errors are the ApiError of
 * switchConnection in api/auth.
 */
export async function switchConnection(params: SwitchConnectionParams): Promise<void> {
  await apiSwitchConnection(params)
  announce('switch')
  dropBrainSeed()
  window.location.assign(withBase('/'))
}

/** Log out and clear session */
export async function logout(): Promise<void> {
  try {
    await apiLogout()
  } finally {
    session = null
    applyScope(null)
    announce('logout')
  }
}
