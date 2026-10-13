// SPDX-License-Identifier: BUSL-1.1
import { apiDel, apiGet, apiPost, apiPut } from './client'
import type {
  AlertEvaluation,
  HealthAlertState,
  HealthCheckSetting,
  HealthCheckSettingInput,
  SqlAlert,
  SqlAlertInput,
  SqlAlertRunResult,
  SqlAlertTestInput,
  SqlAlertTestResult,
  SqlAlertWorkerMode,
} from '../types/alerts'

const BASE = '/api/alerting'

export interface HealthSettingsResponse {
  settings: HealthCheckSetting[]
  /** Health checks only run while Cluster Health collection is on for this connection. */
  collection_enabled: boolean
}

export async function fetchHealthSettings(): Promise<HealthSettingsResponse> {
  const res = await apiGet<Partial<HealthSettingsResponse>>(`${BASE}/health/settings`)
  return { settings: res.settings ?? [], collection_enabled: res.collection_enabled ?? false }
}

export async function saveHealthSettings(settings: HealthCheckSettingInput[]): Promise<HealthSettingsResponse> {
  const res = await apiPut<Partial<HealthSettingsResponse>>(`${BASE}/health/settings`, { settings })
  return { settings: res.settings ?? [], collection_enabled: res.collection_enabled ?? false }
}

/** Pending and firing rows only. */
export async function fetchHealthStates(): Promise<HealthAlertState[]> {
  const res = await apiGet<{ states?: HealthAlertState[] }>(`${BASE}/health/state`)
  return res.states ?? []
}

export interface SqlAlertsResponse {
  alerts: SqlAlert[]
  /** Background runs only happen with a dedicated account. */
  worker_mode: SqlAlertWorkerMode
}

export async function fetchSqlAlerts(): Promise<SqlAlertsResponse> {
  const res = await apiGet<Partial<SqlAlertsResponse>>(`${BASE}/sql`)
  return { alerts: res.alerts ?? [], worker_mode: res.worker_mode ?? 'disabled' }
}

export async function createSqlAlert(input: SqlAlertInput): Promise<SqlAlert> {
  const res = await apiPost<{ alert: SqlAlert }>(`${BASE}/sql`, input)
  return res.alert
}

export async function updateSqlAlert(id: string, input: SqlAlertInput): Promise<SqlAlert> {
  const res = await apiPut<{ alert: SqlAlert }>(`${BASE}/sql/${encodeURIComponent(id)}`, input)
  return res.alert
}

export async function deleteSqlAlert(id: string): Promise<void> {
  await apiDel(`${BASE}/sql/${encodeURIComponent(id)}`)
}

/** Runs with the caller's own session; nothing is stored. */
export async function testSqlAlert(input: SqlAlertTestInput): Promise<SqlAlertTestResult> {
  const res = await apiPost<SqlAlertTestResult>(`${BASE}/sql/test`, input)
  return { ...res, rows_preview: res.rows_preview ?? [] }
}

/** Runs now with the caller's session and records the result. */
export async function runSqlAlert(id: string): Promise<SqlAlertRunResult> {
  return apiPost<SqlAlertRunResult>(`${BASE}/sql/${encodeURIComponent(id)}/run`)
}

export async function fetchAlertEvaluations(source: 'health' | 'sql', id: string, limit = 50): Promise<AlertEvaluation[]> {
  const q = new URLSearchParams({ source, id, limit: String(limit) })
  const res = await apiGet<{ evaluations?: AlertEvaluation[] }>(`${BASE}/evaluations?${q.toString()}`)
  return res.evaluations ?? []
}
