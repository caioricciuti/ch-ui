// SPDX-License-Identifier: BUSL-1.1
import { apiGet, apiPost, apiPut } from './client'

export interface ReportSettings {
  connection_id: string; enabled: boolean; weekday: number; hour: number
  channel_id: string; recipients: string[]; next_run_at: string; last_attempt_at: string; last_error: string
}
export interface OperationsReport {
  id: string; connection_id: string; created_at: string; created_by: string
  body: string; delivery_status: string; delivery_error: string; attempts: number
  payload: { connection_name: string; start: string; end: string; regression_count: number; failures: number; resolved_investigations: number }
}
const path = '/api/operations-reports'
export const getReports = () => apiGet<{ reports: OperationsReport[] }>(path + '/')
export const getReportSettings = () => apiGet<ReportSettings>(path + '/settings')
export const getReportChannels = () => apiGet<{ channels: { id: string; name: string; type: string }[] }>(path + '/channels')
export const saveReportSettings = (settings: ReportSettings) => apiPut<ReportSettings>(path + '/settings', settings)
export const generateReport = () => apiPost<OperationsReport>(path + '/generate')
export const sendReport = (id: string) => apiPost(path + '/' + encodeURIComponent(id) + '/send')
