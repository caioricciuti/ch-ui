import { withBase, stripBase } from '../basePath'
import { safeParse } from '../utils/safe-json'
import { connectionHeaders } from '../stores/workspace-scope'
import { ApiError } from './api-error'
import { connectionChangedMessage } from './connection-changed'

export { ApiError }

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null
}

/** Base fetch wrapper with credentials and error handling */
async function parseResponseBody(res: Response): Promise<unknown> {
  const contentType = (res.headers.get('content-type') || '').toLowerCase()
  if (contentType.includes('application/json')) {
    try {
      const text = await res.text()
      return safeParse<unknown>(text)
    } catch {
      return null
    }
  }

  const text = await res.text().catch(() => '')
  if (!text) return null
  return { message: text }
}

function retryAfterSeconds(res: Response, body: unknown): number {
  const fromBody = isRecord(body) ? Number(body.retryAfter || body.retry_after || 0) : 0
  if (fromBody > 0) return fromBody
  const fromHeader = Number(res.headers.get('retry-after') || 0)
  return Number.isFinite(fromHeader) && fromHeader > 0 ? fromHeader : 0
}

function buildErrorMessage(status: number, body: unknown): string {
  const raw = isRecord(body)
    ? String(body.message || body.error || '').trim()
    : ''
  if (status === 429) {
    const retryAfter = isRecord(body) ? Number(body.retryAfter || body.retry_after || 0) : 0
    if (raw && retryAfter > 0) return `${raw} (retry in ${retryAfter}s)`
    if (raw) return raw
    return 'Too many requests'
  }
  if (raw) return raw
  return `Request failed (${status})`
}

/**
 * Validates a ?next= value from the login URL. Only a same-origin path plus
 * query string is accepted (no scheme, no host, no protocol-relative "//",
 * no backslash tricks, not /login itself); anything else returns null.
 */
export function safeNextPath(raw: string | null | undefined, origin: string = window.location.origin): string | null {
  if (!raw || raw.length > 2048) return null
  if (!raw.startsWith('/') || raw.startsWith('//') || raw.includes('\\')) return null
  if (/[\u0000-\u001f]/.test(raw)) return null
  let url: URL
  try {
    url = new URL(raw, origin)
  } catch {
    return null
  }
  if (url.origin !== origin) return null
  if (url.pathname === '/login') return null
  return url.pathname + url.search
}

/** /login, carrying the current page as ?next= so sign-in returns there. */
function loginUrlWithNext(): string {
  const path = stripBase(window.location.pathname)
  if (path === '/login') return withBase('/login') + window.location.search
  const next = safeNextPath(path + window.location.search)
  return withBase('/login') + (next && next !== '/' ? `?next=${encodeURIComponent(next)}` : '')
}

async function request<T = unknown>(
  url: string,
  options: RequestInit = {},
): Promise<T> {
  const isFormDataBody =
    typeof FormData !== 'undefined' && options.body instanceof FormData

  const { headers, ...rest } = options
  const res = await fetch(withBase(url), {
    credentials: 'include',
    ...rest,
    headers: {
      ...(isFormDataBody ? {} : { 'Content-Type': 'application/json' }),
      ...connectionHeaders(),
      ...headers,
    },
  })

  const body = await parseResponseBody(res)
  const isAuthEndpoint = url.startsWith('/api/auth/')

  const changed = connectionChangedMessage(res.status, body)
  if (changed) throw new ApiError(changed, res.status, body)

  if (res.status === 401 && !isAuthEndpoint) {
    // Session expired: sign in again, then come back to this page.
    window.location.href = loginUrlWithNext()
    throw new Error('Session expired')
  }

  if (res.status === 402) {
    // Body kept: Pro errors may name the part that needs Pro (e.g. a pipeline node_id).
    throw new ApiError(buildErrorMessage(res.status, body) || 'Pro license required', res.status, body)
  }

  if (!res.ok || (isRecord(body) && body.success === false)) {
    throw new ApiError(buildErrorMessage(res.status, body), res.status, body, res.status === 429 ? retryAfterSeconds(res, body) : 0)
  }

  return body as T
}

export function apiGet<T = unknown>(url: string): Promise<T> {
  return request<T>(url)
}

export function apiPost<T = unknown>(url: string, data?: unknown): Promise<T> {
  return request<T>(url, {
    method: 'POST',
    body: data != null ? JSON.stringify(data) : undefined,
  })
}

export function apiPostForm<T = unknown>(url: string, data: FormData): Promise<T> {
  return request<T>(url, {
    method: 'POST',
    body: data,
  })
}

export function apiPut<T = unknown>(url: string, data?: unknown): Promise<T> {
  return request<T>(url, {
    method: 'PUT',
    body: data != null ? JSON.stringify(data) : undefined,
  })
}

export function apiDel<T = unknown>(url: string): Promise<T> {
  return request<T>(url, { method: 'DELETE' })
}
