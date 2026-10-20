/**
 * Error carrying the HTTP status so callers can branch on 403/502/503 etc.
 * body is the parsed response; retryAfter is the server's wait in seconds on
 * a 429 (0 when it gave none).
 */
export class ApiError extends Error {
  readonly status: number
  readonly body: unknown
  readonly retryAfter: number
  constructor(message: string, status: number, body: unknown = null, retryAfter = 0) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.body = body
    this.retryAfter = retryAfter
  }
}
