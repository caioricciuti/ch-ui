/** Base fetch wrapper with credentials and error handling */
async function parseResponseBody(res: Response): Promise<any> {
  const contentType = (res.headers.get('content-type') || '').toLowerCase()
  if (contentType.includes('application/json')) {
    try {
      return await res.json()
    } catch {
      return null
    }
  }

  const text = await res.text().catch(() => '')
  if (!text) return null
  return { message: text }
}

function buildErrorMessage(status: number, body: any): string {
  const raw = body && typeof body === 'object'
    ? String(body.message || body.error || '').trim()
    : ''
  if (status === 429) {
    const retryAfter = body && typeof body === 'object' ? Number(body.retryAfter || body.retry_after || 0) : 0
    if (raw && retryAfter > 0) return `${raw} (retry in ${retryAfter}s)`
    if (raw) return raw
    return 'Too many requests'
  }
  if (raw) return raw
  return `Request failed (${status})`
}

async function request<T = unknown>(
  url: string,
  options: RequestInit = {},
): Promise<T> {
  const isFormDataBody =
    typeof FormData !== 'undefined' && options.body instanceof FormData

  const res = await fetch(url, {
    credentials: 'include',
    headers: {
      ...(isFormDataBody ? {} : { 'Content-Type': 'application/json' }),
      ...options.headers,
    },
    ...options,
  })

  const body = await parseResponseBody(res)
  const isAuthEndpoint = url.startsWith('/api/auth/')

  if (res.status === 401 && !isAuthEndpoint) {
    // Session expired — redirect to login
    window.location.href = '/login'
    throw new Error('Session expired')
  }

  if (res.status === 402) {
    throw new Error(buildErrorMessage(res.status, body) || 'Pro license required')
  }

  if (!res.ok || (body && body.success === false)) {
    throw new Error(buildErrorMessage(res.status, body))
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
