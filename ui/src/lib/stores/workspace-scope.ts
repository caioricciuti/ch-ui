// Client-side state that belongs to one person on one connection (open tabs,
// pinned results, recent commands, ...) is stored under a per-scope key, so a
// connection switch or a different person signing in on the same browser never
// shows or overwrites someone else's workspace. UI preferences (theme, split
// sizes, row limit) stay global.

export interface ScopeIdentity {
  /** The display user: the email for SSO people, else the ClickHouse user. */
  user: string
  connectionId: string
}

/** localStorage keys whose values are kept per scope. */
export const SCOPED_KEYS = [
  'ch-ui-tabs',
  'ch-ui-pinned-results',
  'ch-ui-palette-recent',
  'ch-ui-dash-folders-open',
  'ch-ui-telemetry-log-columns',
] as const

export type ScopedKey = (typeof SCOPED_KEYS)[number]

/** Index of known scopes: scope key to the last time it was used (ms). */
export const SCOPES_INDEX_KEY = 'ch-ui-scopes'
/** Scopes kept on this browser; the least recently used one beyond is removed. */
export const MAX_SCOPES = 10
/** Brain prompt handed from the command palette; never crosses a switch. */
export const BRAIN_SEED_KEY = 'ch-ui-brain-prompt-seed'

/** user@connection. The connection id, never its name, so a rename keeps the scope. */
export function scopeKey(s: ScopeIdentity): string {
  return `${encodeURIComponent(s.user)}@${s.connectionId}`
}

let current: ScopeIdentity | null = null
let currentKey: string | null = null
const listeners = new Set<() => void>()

/** The active scope key, or null while nobody is signed in. */
export function getWorkspaceScope(): string | null {
  return currentKey
}

/** Header the server checks against the session's connection (409 connection_changed on a mismatch). */
export const CONNECTION_HEADER = 'X-CHUI-Connection'

/** The connection header for the signed-in session; empty while nobody is. */
export function connectionHeaders(): Record<string, string> {
  return current ? { [CONNECTION_HEADER]: current.connectionId } : {}
}

/** base:scope for the active scope, or null while nobody is signed in. */
export function scoped(base: ScopedKey): string | null {
  return currentKey ? `${base}:${currentKey}` : null
}

/** Runs fn whenever the active scope changes. Returns the unsubscribe. */
export function onScopeChange(fn: () => void): () => void {
  listeners.add(fn)
  return () => listeners.delete(fn)
}

/**
 * Sets the active scope from the session (null on sign-out). On a change it
 * moves pre-scope (legacy) values into the new scope, records it in the index
 * (evicting the least recently used scope past MAX_SCOPES), clears the Brain
 * prompt seed, and notifies listeners.
 */
export function setWorkspaceScope(s: ScopeIdentity | null): void {
  const next = s ? scopeKey(s) : null
  current = s
  if (next === currentKey) return
  const hadScope = currentKey !== null
  currentKey = next
  if (next) {
    migrateLegacy(next)
    touchScope(next)
  }
  if (hadScope) {
    try { sessionStorage.removeItem(BRAIN_SEED_KEY) } catch { /* unavailable */ }
  }
  for (const fn of listeners) fn()
}

/** Reads a scoped value for the active scope. */
export function readScoped(base: ScopedKey): string | null {
  const key = scoped(base)
  if (!key) return null
  try {
    return localStorage.getItem(key)
  } catch {
    return null
  }
}

/** Writes a scoped value for the active scope. False when nothing was stored. */
export function writeScoped(base: ScopedKey, value: string): boolean {
  const key = scoped(base)
  if (!key) return false
  try {
    localStorage.setItem(key, value)
    return true
  } catch {
    return false
  }
}

/** Removes a scoped value for the active scope. */
export function removeScoped(base: ScopedKey): void {
  const key = scoped(base)
  if (!key) return
  try { localStorage.removeItem(key) } catch { /* unavailable */ }
}

/**
 * Values written before scoping existed belong to whoever signs in first
 * after the upgrade: move each legacy key into scope unless the scope
 * already has its own value, then drop the legacy key.
 */
export function migrateLegacy(scope: string): void {
  try {
    for (const base of SCOPED_KEYS) {
      const legacy = localStorage.getItem(base)
      if (legacy === null) continue
      const key = `${base}:${scope}`
      if (localStorage.getItem(key) === null) localStorage.setItem(key, legacy)
      localStorage.removeItem(base)
    }
  } catch {
    // storage unavailable: nothing to migrate
  }
}

function readIndex(): Record<string, number> {
  try {
    const parsed: unknown = JSON.parse(localStorage.getItem(SCOPES_INDEX_KEY) ?? '{}')
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) return {}
    const out: Record<string, number> = {}
    for (const [k, v] of Object.entries(parsed as Record<string, unknown>)) {
      if (typeof v === 'number') out[k] = v
    }
    return out
  } catch {
    return {}
  }
}

/** Marks scope as used now and evicts the oldest scopes beyond MAX_SCOPES. */
export function touchScope(scope: string, now: number = Date.now()): void {
  const index = readIndex()
  index[scope] = now
  const byAge = Object.keys(index).sort((a, b) => index[b] - index[a])
  for (const old of byAge.slice(MAX_SCOPES)) {
    if (old === scope) continue
    delete index[old]
    try {
      for (const base of SCOPED_KEYS) localStorage.removeItem(`${base}:${old}`)
    } catch { /* unavailable */ }
  }
  try {
    localStorage.setItem(SCOPES_INDEX_KEY, JSON.stringify(index))
  } catch {
    // quota: the index is rebuilt on the next successful write
  }
}
