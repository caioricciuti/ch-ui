import { apiGet, apiPost, apiPut, apiDel } from './client'
import type { Model, ModelRun, ModelRunResult, ModelDAG, ValidationResult } from '../types/models'

const BASE = '/api/models'

export function listModels() {
  return apiGet<{ models: Model[] }>(BASE)
}

export function createModel(data: {
  name: string
  description?: string
  target_database: string
  materialization: string
  sql_body: string
  table_engine?: string
  order_by?: string
}) {
  return apiPost<{ model: Model }>(BASE, data)
}

export function getModel(id: string) {
  return apiGet<{ model: Model }>(`${BASE}/${id}`)
}

export function updateModel(id: string, data: Partial<Omit<Model, 'id' | 'connection_id' | 'created_at' | 'updated_at'>>) {
  return apiPut<{ model: Model }>(`${BASE}/${id}`, data)
}

export function deleteModel(id: string) {
  return apiDel(`${BASE}/${id}`)
}

export function getDAG() {
  return apiGet<ModelDAG>(`${BASE}/dag`)
}

export function validateModels() {
  return apiGet<ValidationResult>(`${BASE}/validate`)
}

export function runAllModels() {
  return apiPost<{ run_id: string }>(`${BASE}/run`)
}

export function runSingleModel(id: string) {
  return apiPost<{ run_id: string }>(`${BASE}/${id}/run`)
}

export function listModelRuns(limit = 20, offset = 0) {
  return apiGet<{ runs: ModelRun[] }>(`${BASE}/runs?limit=${limit}&offset=${offset}`)
}

export function getModelRun(runId: string) {
  return apiGet<{ run: ModelRun; results: ModelRunResult[] }>(`${BASE}/runs/${runId}`)
}
