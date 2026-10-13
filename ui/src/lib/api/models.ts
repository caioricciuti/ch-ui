import { apiGet, apiPost, apiPut, apiDel } from './client'
import type {
  CompileResult,
  Materialization,
  Model,
  ModelDAG,
  ModelPipeline,
  ModelRun,
  ModelRunResult,
  ModelTest,
  ModelTestInput,
  PipelineInput,
  PreviewResult,
  RunOutcome,
  RunScope,
} from '../types/models'

const BASE = '/api/models'

export function listModels() {
  return apiGet<{ models: Model[] }>(BASE)
}

export interface ModelInput {
  name: string
  description?: string
  target_database: string
  materialization: Materialization
  sql_body: string
  table_engine?: string
  order_by?: string
  partition_by?: string
  engine_args?: string
  incremental_strategy?: string
  unique_key?: string
  watermark_column?: string
}

export function createModel(data: ModelInput) {
  return apiPost<{ model: Model }>(BASE, data)
}

export function getModel(id: string) {
  return apiGet<{ model: Model }>(`${BASE}/${id}`)
}

/** Option fields left out keep their stored value; send '' to clear one. */
export function updateModel(id: string, data: Partial<ModelInput>) {
  return apiPut<{ model: Model }>(`${BASE}/${id}`, data)
}

export function deleteModel(id: string) {
  return apiDel(`${BASE}/${id}`)
}

export function getDAG() {
  return apiGet<ModelDAG>(`${BASE}/dag`)
}

/** Runs every model, or the models a selector picks. */
export function runAllModels(selector?: string) {
  return apiPost<RunOutcome>(`${BASE}/run`, selector ? { selector } : undefined)
}

/**
 * Runs a model with its upstream models (default), its downstream models or
 * alone. fullRefresh rebuilds this model from scratch.
 */
export function runSingleModel(id: string, opts: { scope?: RunScope; fullRefresh?: boolean } = {}) {
  const body: { scope?: RunScope; full_refresh?: boolean } = {}
  if (opts.scope) body.scope = opts.scope
  if (opts.fullRefresh) body.full_refresh = true
  return apiPost<RunOutcome>(`${BASE}/${id}/run`, body)
}

export function listModelRuns(limit = 20, offset = 0) {
  return apiGet<{ runs: ModelRun[] }>(`${BASE}/runs?limit=${limit}&offset=${offset}`)
}

export function getModelRun(runId: string) {
  return apiGet<{ run: ModelRun; results: ModelRunResult[] }>(`${BASE}/runs/${runId}`)
}

// ── Compile and preview ────────────────────────────────────────────

/** Resolved SQL and planned statements; sqlBody compiles unsaved SQL. */
export function compileModel(id: string, sqlBody?: string) {
  return apiPost<CompileResult>(`${BASE}/${id}/compile`, sqlBody === undefined ? {} : { sql_body: sqlBody })
}

/** First 100 rows of the SQL, run read-only with your own credentials. */
export function previewModel(sqlBody: string, modelId?: string) {
  return apiPost<PreviewResult>(`${BASE}/preview`, modelId ? { sql_body: sqlBody, model_id: modelId } : { sql_body: sqlBody })
}

// ── Data tests ─────────────────────────────────────────────────────

export function listModelTests(modelId: string) {
  return apiGet<{ tests: ModelTest[] }>(`${BASE}/${modelId}/tests`)
}

export function createModelTest(modelId: string, data: ModelTestInput) {
  return apiPost<{ test: ModelTest }>(`${BASE}/${modelId}/tests`, data)
}

export function updateModelTest(modelId: string, testId: string, data: ModelTestInput) {
  return apiPut<{ test: ModelTest }>(`${BASE}/${modelId}/tests/${testId}`, data)
}

export function deleteModelTest(modelId: string, testId: string) {
  return apiDel(`${BASE}/${modelId}/tests/${testId}`)
}

// ── Named pipelines ────────────────────────────────────────────────

export function listPipelines() {
  return apiGet<{ pipelines: ModelPipeline[]; ungrouped_model_ids: string[] }>(`${BASE}/pipelines`)
}

export function createPipeline(data: PipelineInput) {
  return apiPost<{ pipeline: ModelPipeline }>(`${BASE}/pipelines`, data)
}

/** enabled omitted keeps the current state. */
export function updatePipeline(id: string, data: PipelineInput) {
  return apiPut<{ pipeline: ModelPipeline }>(`${BASE}/pipelines/${id}`, data)
}

export function deletePipeline(id: string) {
  return apiDel(`${BASE}/pipelines/${id}`)
}

export function runPipeline(id: string) {
  return apiPost<RunOutcome>(`${BASE}/pipelines/${id}/run`)
}

/** Next three run times (UTC ISO) for a cron read in timezone. */
export function previewSchedule(cron: string, timezone: string) {
  const q = new URLSearchParams({ cron, timezone })
  return apiGet<{ next_runs: string[] }>(`${BASE}/schedule-preview?${q.toString()}`)
}
