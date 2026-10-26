import { toast } from 'svelte-sonner'
import type { Session } from '../types/api'
import {
  checkSession,
  login as apiLogin,
  logout as apiLogout,
  switchConnection as apiSwitchConnection,
  type SwitchConnectionParams,
} from '../api/auth'
import { onConnectionChanged, setConnectionSwitching } from '../api/connection-changed'
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

type SessionEvent = 'login' | 'logout' | 'switch'

/**
 * Set when the session changed under this tab (another tab switched, signed
 * in or out, or a request came back connection_changed). The tab shows a
 * notice with a Reload button instead of reloading on its own, so unsaved
 * work (an open editor, a half-filled form) can still be copied out.
 */
export type StaleSession = 'changed' | 'logout'
let stale = $state<StaleSession | null>(null)

function markStale(reason: StaleSession): void {
  // A signed-out page has no work to lose: load the new session right away.
  if (!session) {
    if (!initializing && !submitting) window.location.reload()
    return
  }
  if (stale !== 'logout') stale = reason
}

// Tells the other tabs of this browser that the session changed under them:
// they show the stale notice, so none keeps working on a connection or person
// that is gone without knowing it.
const CHANNEL_NAME = 'ch-ui-session'
let channel: BroadcastChannel | null = null

function sessionChannel(): BroadcastChannel | null {
  if (channel || typeof window === 'undefined' || typeof BroadcastChannel === 'undefined') return channel
  try {
    channel = new BroadcastChannel(CHANNEL_NAME)
    channel.onmessage = (e: MessageEvent<{ type?: SessionEvent }>) => markStale(e.data?.type === 'logout' ? 'logout' : 'changed')
  } catch {
    channel = null
  }
  return channel
}

function announce(type: SessionEvent): void {
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

/** Why this tab's session is stale, or null while it is current. */
export function getStaleSession(): StaleSession | null {
  return stale
}

/** Hides the notice; the next stale request or message shows it again. */
export function dismissStaleSession(): void {
  stale = null
}

/**
 * Reads the session again for the connection's online state, which startup
 * only read once. A session that moved to another connection (another tab)
 * is left to the stale notice.
 */
export async function refreshConnectionOnline(): Promise<void> {
  if (!session) return
  const fresh = await checkSession()
  if (!fresh || !session || fresh.connectionId !== session.connectionId) return
  if (fresh.connectionOnline !== session.connectionOnline) session = { ...session, connectionOnline: fresh.connectionOnline }
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
  onConnectionChanged(() => markStale('changed'))
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
  // Requests still in flight from this page may answer connection_changed
  // once the new session lands; they are this tab's own doing.
  setConnectionSwitching(true)
  try {
    const res = await apiLogin({ connectionId, username, password, clickhouseUrl })
    const nextScope = scopeKey(res.session)
    announce('login')
    if (pageScope !== null && pageScope !== nextScope) {
      // Stay "submitting" (and switching) until the page goes away.
      dropBrainSeed()
      window.location.assign(withBase(reloadTo ?? stripBase(window.location.pathname) + window.location.search))
      return
    }
    setConnectionSwitching(false)
    session = res.session
    pageScope = nextScope
    applyScope(session)
    if (session?.roleNote) {
      toast.warning(`Signed in as ${session.role}`, { description: session.roleNote, duration: 12000 })
    }
    submitting = false
  } catch (e: unknown) {
    error = e instanceof Error && e.message ? e.message : 'Login failed'
    setConnectionSwitching(false)
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
  // From here until the page goes, a connection_changed answer is this tab's
  // own switch landing, not another tab's.
  setConnectionSwitching(true)
  try {
    await apiSwitchConnection(params)
  } catch (e) {
    setConnectionSwitching(false)
    throw e
  }
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
