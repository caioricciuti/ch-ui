/**
 * The server answers 409 connection_changed when a request names a
 * connection the session no longer has: another tab switched or signed in.
 * Every fetch path (client, streams, Brain) routes that answer through here,
 * so the tab shows one notice instead of the raw error code.
 */

export const CONNECTION_CHANGED_MESSAGE = 'The connection changed in another tab'
/** Thrown instead while this tab is itself switching; the page is about to go. */
export const CONNECTION_SWITCHING_MESSAGE = 'Switching connection'

let switching = false
let listener: (() => void) | null = null

/** Set by the session store around its own switch or sign-in. */
export function setConnectionSwitching(value: boolean): void {
  switching = value
}

export function isConnectionSwitching(): boolean {
  return switching
}

/** The session store shows its stale-tab notice from here. */
export function onConnectionChanged(fn: (() => void) | null): void {
  listener = fn
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null
}

export function isConnectionChangedResponse(status: number, body: unknown): boolean {
  return status === 409 && isRecord(body) && body.error === 'connection_changed'
}

/**
 * When status and body are a connection_changed answer, raises the notice
 * (unless this tab is the one switching) and returns the message for the
 * caller's error. Returns null for any other answer.
 */
export function connectionChangedMessage(status: number, body: unknown): string | null {
  if (!isConnectionChangedResponse(status, body)) return null
  if (switching) return CONNECTION_SWITCHING_MESSAGE
  listener?.()
  return CONNECTION_CHANGED_MESSAGE
}
