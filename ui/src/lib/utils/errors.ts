// Turn a failed request into copy for people: what happened and what to do
// next. The original text stays available as detail (toast description or a
// "Details:" tail) so nothing useful is lost. Query editor result errors do
// not go through here: the exact ClickHouse error is the point there.

import { ApiError } from '../api/api-error'
import { parseCHError } from './ch-error'

export interface FriendlyError {
  /** One to three short sentences: what happened, then the next step. */
  message: string
  /** The original server or browser text, when it adds something. */
  detail: string | null
}

const DETAIL_MAX = 400

// Text that came from Go, SQLite, the network stack or a JSON parser rather
// than from a person writing a message for the screen.
const INTERNAL_RE =
  /UNIQUE constraint|FOREIGN KEY constraint|no rows in result set|database is locked|context deadline exceeded|context canceled|connection refused|connection reset|broken pipe|no such host|i\/o timeout|unexpected EOF|invalid character|cannot unmarshal|unexpected end of JSON|nil pointer|goroutine|%!|DB::\w*Exception|Code:\s*\d+\./
// Go wraps errors as "verb noun: cause"; a human message starts with a capital.
const GO_WRAP_RE = /^[a-z][a-z0-9_ .-]*: /
const UNREACHABLE_RE =
  /connection refused|connection reset|no such host|i\/o timeout|context deadline exceeded|unexpected EOF|tunnel.*offline|not connected/i

interface Copy {
  lead: string
  next: string
}

const CH_CODES: Record<number, Copy> = {
  47: { lead: 'ClickHouse does not know a column used here.', next: 'Check the column names against the table.' },
  60: { lead: 'That table does not exist in ClickHouse.', next: 'Refresh the explorer and check the name.' },
  62: { lead: 'ClickHouse could not parse the SQL.', next: 'Check the statement and try again.' },
  81: { lead: 'That database does not exist in ClickHouse.', next: 'Refresh the explorer and check the name.' },
  159: { lead: 'ClickHouse took too long to answer.', next: 'Narrow the time range or add filters, then try again.' },
  164: { lead: 'This ClickHouse user is read only.', next: 'Ask an admin for write access.' },
  192: { lead: 'ClickHouse rejected the credentials.', next: 'Check the username and password for this connection.' },
  193: { lead: 'ClickHouse rejected the credentials.', next: 'Check the username and password for this connection.' },
  202: { lead: 'ClickHouse is running too many queries right now.', next: 'Wait a moment and try again.' },
  209: { lead: 'ClickHouse took too long to answer.', next: 'Try again in a moment.' },
  210: { lead: 'Could not reach ClickHouse.', next: 'Check that the server is up and try again.' },
  241: { lead: 'ClickHouse ran out of memory.', next: 'Narrow the time range or add filters, then try again.' },
  394: { lead: 'The query was cancelled.', next: 'Run it again if you still need it.' },
  396: { lead: 'The result is over the row or byte limit.', next: 'Add a LIMIT or filters and try again.' },
  497: { lead: 'Your ClickHouse user is not allowed to do this.', next: 'Ask an admin to grant access.' },
  516: { lead: 'ClickHouse rejected the credentials.', next: 'Check the username and password for this connection.' },
}

const CH_OTHER: Copy = { lead: 'ClickHouse returned an error.', next: 'Check the details, then try again.' }

function statusCopy(status: number, retryAfter: number): Copy {
  if (status === 401) return { lead: 'Your session ended.', next: 'Sign in again to continue.' }
  if (status === 402) return { lead: 'This needs a Pro license.', next: 'Activate one under Instance.' }
  if (status === 403) return { lead: 'Your role cannot do this.', next: 'Ask an admin for access.' }
  if (status === 404) return { lead: 'That item no longer exists.', next: 'Refresh the page and try again.' }
  if (status === 409) return { lead: 'This changed in the meantime.', next: 'Refresh the page and try again.' }
  if (status === 413) return { lead: 'That is too large to send.', next: 'Use a smaller file or fewer rows.' }
  if (status === 429) {
    return {
      lead: 'Too many requests.',
      next: retryAfter > 0 ? `Wait ${retryAfter} seconds and try again.` : 'Wait a moment and try again.',
    }
  }
  if (status === 502 || status === 503 || status === 504) {
    return { lead: 'Could not reach ClickHouse.', next: 'Check that the server and its connection are up, then try again.' }
  }
  if (status >= 500) {
    return { lead: 'The server hit an unexpected error.', next: 'Try again. If it keeps happening, check the CH-UI server logs.' }
  }
  return { lead: 'The request was not accepted.', next: 'Check the values and try again.' }
}

/** True when the text reads like a message written for people. */
export function isHumanMessage(text: string): boolean {
  const t = text.trim()
  if (!t || t.length > 300) return false
  return !INTERNAL_RE.test(t) && !GO_WRAP_RE.test(t)
}

function clip(text: string): string {
  const t = text.trim()
  return t.length > DETAIL_MAX ? `${t.slice(0, DETAIL_MAX).trimEnd()}...` : t
}

function compose(copy: Copy, action: string | undefined, detail: string): FriendlyError {
  const message = [action ? `Could not ${action}.` : '', copy.lead, copy.next].filter(Boolean).join(' ')
  const d = clip(detail)
  return { message, detail: d && d !== message ? d : null }
}

// A message the server or the browser already wrote for people, with the
// action in front so the reader knows what failed.
function kept(raw: string, action: string | undefined): FriendlyError {
  if (!action) return { message: raw, detail: null }
  const sentence = /[\w)'"]$/.test(raw) ? `${raw}.` : raw
  return { message: `Could not ${action}. ${sentence}`, detail: null }
}

function isNetworkFailure(e: unknown): boolean {
  return e instanceof TypeError && /fetch|network|load failed/i.test(e.message)
}

/**
 * What to show for a failed call. action reads after "Could not", e.g.
 * 'save the policy'. A message the server already wrote for people is kept,
 * except on 403, 413, 429 and 500, which always say what to do next.
 */
export function friendlyError(e: unknown, action?: string): FriendlyError {
  if (e instanceof DOMException && e.name === 'AbortError') {
    return { message: 'The request was cancelled.', detail: null }
  }
  if (isNetworkFailure(e)) {
    return compose({ lead: 'Could not reach the CH-UI server.', next: 'Check your network connection and try again.' }, action, (e as Error).message)
  }

  const raw = (e instanceof Error ? e.message : typeof e === 'string' ? e : '').trim()
  // client.ts throws this while it redirects to /login.
  if (raw === 'Session expired') return compose(statusCopy(401, 0), undefined, '')

  const ch = parseCHError(raw)
  if (ch.code !== null) {
    // Keep a prefix the server put in front ("Source: ...") ahead of the cleaned text.
    const at = raw.search(/Code:\s*\d+\./)
    return compose(CH_CODES[ch.code] ?? CH_OTHER, action, (at > 0 ? raw.slice(0, at) : '') + ch.message)
  }

  if (e instanceof ApiError) {
    if (e.status === 403) return compose(statusCopy(403, 0), action, raw)
    if (e.status === 429) return compose(statusCopy(429, e.retryAfter), action, raw)
    if (e.status === 413) return compose(statusCopy(413, 0), action, raw)
    const gateway = e.status >= 502 && e.status <= 504
    // A 500 is a server bug whatever its text says; keep that text as detail.
    if (e.status >= 500 && !gateway) return compose(statusCopy(e.status, 0), action, raw)
    if (isHumanMessage(raw)) return kept(raw, action)
    if (gateway && raw && !UNREACHABLE_RE.test(raw)) return compose(CH_OTHER, action, raw)
    return compose(statusCopy(e.status, e.retryAfter), action, raw)
  }

  if (isHumanMessage(raw)) return kept(raw, action)
  return compose({ lead: 'Something went wrong.', next: 'Try again.' }, action, raw)
}

/** friendlyError as one line for inline text: the message, then "Details: ...". */
export function errorText(e: unknown, action?: string): string {
  const { message, detail } = friendlyError(e, action)
  return detail ? `${message} Details: ${detail}` : message
}
