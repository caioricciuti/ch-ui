import { apiGet, apiPut } from './client'

const BASE = '/api/admin/retention'

/** Per-table retention windows in days. 0 = keep forever (pruning disabled). */
export interface RetentionConfig {
  audit_logs: number
  alert_events: number
  alert_dispatch_jobs: number
  schedule_runs: number
  pipeline_runs: number
  pipeline_run_logs: number
  model_runs: number
  model_run_results: number
  github_sync_logs: number
  gov_schema_changes: number
  operations_reports: number
  incident_annotations: number
  resolved_investigations: number
}

export interface RetentionLastRun {
  last_run_at: string
  duration_ms: number
  rows_deleted: Record<string, number>
  total_deleted: number
  last_error?: string
}

export interface RetentionSettings {
  config: RetentionConfig
  defaults: RetentionConfig
  last_run: RetentionLastRun | null
}

export function fetchRetentionSettings() {
  return apiGet<RetentionSettings>(BASE)
}

export function updateRetentionSettings(config: RetentionConfig) {
  return apiPut<RetentionSettings>(BASE, config)
}
