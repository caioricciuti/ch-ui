// SPDX-License-Identifier: BUSL-1.1
import { apiDel, apiGet, apiPost, apiPut, ApiError } from './client'
import { withBase } from '../basePath'
import { connectionHeaders } from '../stores/workspace-scope'

const BASE = '/api/reports'

export type ReportSource = 'saved_query' | 'dashboard'
export type ReportFormat = 'csv' | 'inline' | 'both'
export type ReportWorkerMode = 'service_account' | 'session' | 'disabled'

export interface ReportTarget {
  channel_id: string
  recipients: string[]
  channel_name?: string
  channel_type?: string
}

export interface Report {
  id: string
  name: string
  source_type: ReportSource
  saved_query_id: string | null
  dashboard_id: string | null
  connection_id: string
  cron: string
  timezone: string
  enabled: boolean
  format: ReportFormat
  row_limit: number
  inline_rows: number
  max_bytes: number
  timeout_seconds: number
  time_range: string
  variables: Record<string, string[]>
  skip_if_empty: boolean
  subject_template: string
  next_run_at: string | null
  last_run_at: string | null
  last_status: string | null
  last_error: string | null
  consecutive_failures: number
  created_by: string | null
  created_at: string
  updated_at: string
  targets: ReportTarget[]
}

export interface ReportInput {
  name: string
  source_type: ReportSource
  saved_query_id: string | null
  dashboard_id: string | null
  connection_id: string
  cron: string
  timezone: string
  enabled: boolean
  format: ReportFormat
  row_limit: number
  inline_rows: number
  max_bytes: number
  timeout_seconds: number
  time_range: string
  variables: Record<string, string[]>
  skip_if_empty: boolean
  subject_template: string
  targets: ReportTarget[]
}

export interface ReportChannel {
  id: string
  name: string
  channel_type: string
  format?: string
  is_active: boolean
}

export interface ReportDeliveryTarget {
  delivery_id: string
  channel_id: string
  channel_name: string
  status: string
  attempts: number
  next_attempt_at: string | null
  last_error: string | null
  provider_message_id: string | null
}

export interface ReportDelivery {
  id: string
  subscription_id: string
  trigger: string
  status: string
  rows: number
  bytes: number
  truncated: boolean
  duration_ms: number
  error: string | null
  has_snapshot: boolean
  ch_user: string | null
  started_at: string
  finished_at: string | null
  targets: ReportDeliveryTarget[]
}

export interface ReportsResponse {
  reports: Report[]
  channels: ReportChannel[]
  /** Report account mode per connection id. Reports only run with a dedicated account. */
  worker_modes: Record<string, ReportWorkerMode>
  recipient_domains: string[]
}

export interface ReportPreview {
  subject: string
  text: string
  html: string
  rows: number
  truncated: boolean
  empty: boolean
  csv_name: string
  csv_bytes: number
}

/** Limits enforced by the server. */
export const REPORT_LIMITS = {
  rowLimit: { def: 1000, max: 50000 },
  inlineRows: { def: 50, max: 200 },
  maxBytes: { def: 1 << 20, max: 5 << 20 },
  timeout: { def: 60, min: 5, max: 300 },
  targets: 5,
  recipients: 20,
} as const

export async function fetchReports(): Promise<ReportsResponse> {
  const res = await apiGet<Partial<ReportsResponse>>(BASE)
  return {
    reports: res.reports ?? [],
    channels: res.channels ?? [],
    worker_modes: res.worker_modes ?? {},
    recipient_domains: res.recipient_domains ?? [],
  }
}

export async function createReport(input: ReportInput): Promise<Report> {
  const res = await apiPost<{ report: Report }>(BASE, input)
  return res.report
}

export async function updateReport(id: string, input: ReportInput): Promise<Report> {
  const res = await apiPut<{ report: Report }>(`${BASE}/${encodeURIComponent(id)}`, input)
  return res.report
}

export async function deleteReport(id: string): Promise<void> {
  await apiDel(`${BASE}/${encodeURIComponent(id)}`)
}

/** Renders with the report account; nothing is sent or stored. */
export async function previewReport(id: string): Promise<ReportPreview> {
  return apiPost<ReportPreview>(`${BASE}/${encodeURIComponent(id)}/preview`)
}

/** Runs now and sends to every channel. */
export async function sendReport(id: string): Promise<ReportDelivery | null> {
  const res = await apiPost<{ delivery?: ReportDelivery | null }>(`${BASE}/${encodeURIComponent(id)}/send`)
  return res.delivery ?? null
}

export async function fetchReportDeliveries(id: string, limit = 100): Promise<ReportDelivery[]> {
  const res = await apiGet<{ deliveries?: ReportDelivery[] }>(`${BASE}/${encodeURIComponent(id)}/deliveries?limit=${limit}`)
  return res.deliveries ?? []
}

export async function saveRecipientDomains(domains: string[]): Promise<string[]> {
  const res = await apiPut<{ recipient_domains?: string[] }>(`${BASE}/settings`, { recipient_domains: domains })
  return res.recipient_domains ?? []
}

/** The CSV a delivery sent, while it is still stored (7 days). */
export async function downloadDeliveryCSV(deliveryId: string): Promise<{ blob: Blob; filename: string }> {
  const res = await fetch(withBase(`${BASE}/deliveries/${encodeURIComponent(deliveryId)}/download`), {
    credentials: 'include',
    headers: { ...connectionHeaders() },
  })
  if (!res.ok) {
    let message = `Download failed (${res.status})`
    try {
      const body: unknown = await res.json()
      if (typeof body === 'object' && body !== null && 'error' in body) message = String((body as { error: unknown }).error)
    } catch {
      // not JSON
    }
    throw new ApiError(message, res.status)
  }
  const disposition = res.headers.get('content-disposition') ?? ''
  const match = /filename="([^"]+)"/.exec(disposition)
  return { blob: await res.blob(), filename: match?.[1] ?? 'report.csv' }
}
