/**
 * Client for the CH-UI self-serve license server.
 *
 * These endpoints are cross-origin (CORS open) and credential-free, so we use
 * plain fetch here instead of the app's apiClient.
 */
export const LICENSE_SERVER_URL = 'https://license.ch-ui.com'

export class LicenseServerError extends Error {
  readonly status: number

  constructor(status: number, message: string) {
    super(message)
    this.name = 'LicenseServerError'
    this.status = status
  }
}

export interface TrialResponse {
  success: boolean
  message: string
  /**
   * Signed license file, ready to activate via POST /api/license/activate.
   * Current servers deliver by email only and omit this field.
   */
  license?: Record<string, unknown>
  expires_at: string
}

export interface CheckoutResponse {
  url: string
}

export interface StatusMessageResponse {
  status: string
  message: string
}

async function postJson<T>(path: string, body: Record<string, string>): Promise<T> {
  let res: Response
  try {
    res = await fetch(`${LICENSE_SERVER_URL}${path}`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    })
  } catch {
    throw new LicenseServerError(0, 'Could not reach the license server. Check your connection and try again.')
  }

  const data: unknown = await res.json().catch(() => null)

  if (!res.ok) {
    if (res.status === 429) {
      throw new LicenseServerError(429, 'Too many requests. Wait a few minutes and try again.')
    }
    const message =
      data !== null &&
      typeof data === 'object' &&
      'error' in data &&
      typeof (data as { error: unknown }).error === 'string'
        ? (data as { error: string }).error
        : `The license server returned an error (HTTP ${res.status}). Try again in a few minutes.`
    throw new LicenseServerError(res.status, message)
  }

  return data as T
}

/** POST /trial — start a free 30-day Pro trial (one per email). */
export function startTrial(email: string, name?: string): Promise<TrialResponse> {
  const body: Record<string, string> = { email }
  if (name) body.name = name
  return postJson<TrialResponse>('/trial', body)
}

/** POST /checkout — create a Stripe Checkout session; open the returned URL in a new tab. */
export function createCheckout(email?: string): Promise<CheckoutResponse> {
  const body: Record<string, string> = {}
  if (email) body.email = email
  return postJson<CheckoutResponse>('/checkout', body)
}

/** POST /license/resend — re-send the license file to the purchase email (generic response). */
export function resendLicense(email: string): Promise<StatusMessageResponse> {
  return postJson<StatusMessageResponse>('/license/resend', { email })
}

/** POST /portal — email a Stripe billing-portal link. */
export function requestBillingPortal(email: string): Promise<StatusMessageResponse> {
  return postJson<StatusMessageResponse>('/portal', { email })
}
