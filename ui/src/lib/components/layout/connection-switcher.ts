import { ApiError } from '../../api/client'
import type { Connection, Session } from '../../types/api'

export interface SwitchTarget {
  connection: Connection
  current: boolean
  /** Why the connection cannot be picked; null when it can. */
  disabledReason: string | null
}

/** One row of the switcher: current, offline and SSO-not-ready rows are not pickable. */
export function switchTarget(connection: Connection, session: Session | null): SwitchTarget {
  const current = connection.id === session?.connectionId
  let disabledReason: string | null = null
  if (current) disabledReason = 'Current connection'
  else if (!connection.online) disabledReason = 'Connection is offline'
  else if (session?.viaSSO && connection.sso_ready === false) disabledReason = 'SSO not set up'
  return { connection, current, disabledReason }
}

export interface SwitchErrorView {
  message: string
  /** Show the user and password fields (the current password was refused or cannot be used there). */
  needsPassword: boolean
}

function bodyField(e: ApiError, field: string): unknown {
  return typeof e.body === 'object' && e.body !== null ? (e.body as Record<string, unknown>)[field] : undefined
}

/** What the switcher shows for a failed switch. */
export function switchErrorView(e: unknown): SwitchErrorView {
  if (!(e instanceof ApiError)) {
    return { message: e instanceof Error && e.message ? e.message : 'Switch failed', needsPassword: false }
  }
  if (e.status === 401) {
    // The server only reuses the current password on the same ClickHouse
    // server; anywhere else it asks for that server's password.
    if (bodyField(e, 'reason') === 'different_server') {
      const message = bodyField(e, 'message')
      return { message: typeof message === 'string' && message ? message : 'Different server, enter the password for this user.', needsPassword: true }
    }
    return { message: 'Invalid credentials', needsPassword: true }
  }
  if (e.status === 429) {
    return { message: e.retryAfter > 0 ? `Too many attempts. Retry in ${e.retryAfter}s.` : 'Too many attempts. Try again later.', needsPassword: false }
  }
  if (e.status === 409 && bodyField(e, 'error') === 'session_changed') {
    return { message: 'Your session changed in another tab. Reload and try again.', needsPassword: false }
  }
  const message = bodyField(e, 'message')
  return { message: typeof message === 'string' && message ? message : e.message, needsPassword: false }
}
