import { apiGet, apiPost, apiPut, apiDel } from './client'
import type { Pipeline, PipelineGraph, PipelineRun, PipelineRunLog } from '../types/pipelines'

const BASE = '/api/pipelines'

export function listPipelines() {
  return apiGet<{ pipelines: Pipeline[] }>(BASE)
}

export function createPipeline(data: { name: string; description?: string; connection_id?: string }) {
  return apiPost<{ pipeline: Pipeline }>(BASE, data)
}

export function getPipeline(id: string) {
  return apiGet<{ pipeline: Pipeline; graph: PipelineGraph }>(`${BASE}/${id}`)
}

export function updatePipeline(id: string, data: { name: string; description?: string }) {
  return apiPut<{ pipeline: Pipeline }>(`${BASE}/${id}`, data)
}

export function deletePipeline(id: string) {
  return apiDel(`${BASE}/${id}`)
}

export function saveGraph(id: string, graph: {
  nodes: { id: string; node_type: string; label: string; position_x: number; position_y: number; config: Record<string, unknown> }[]
  edges: { id: string; source_node_id: string; target_node_id: string; source_handle?: string; target_handle?: string }[]
  viewport?: { x: number; y: number; zoom: number }
}) {
  return apiPut<{ success: string }>(`${BASE}/${id}/graph`, graph)
}

export function startPipeline(id: string) {
  return apiPost<{ success: string }>(`${BASE}/${id}/start`)
}

export function stopPipeline(id: string) {
  return apiPost<{ success: string }>(`${BASE}/${id}/stop`)
}

export function getPipelineStatus(id: string) {
  return apiGet<{
    pipeline_id: string
    status: string
    last_error: string | null
    rows_ingested?: number
    bytes_ingested?: number
    batches_sent?: number
    errors_count?: number
  }>(`${BASE}/${id}/status`)
}

export function listRuns(id: string, limit = 20, offset = 0) {
  return apiGet<{ runs: PipelineRun[] }>(`${BASE}/${id}/runs?limit=${limit}&offset=${offset}`)
}

export function getRunLogs(id: string, runId: string, limit = 200) {
  return apiGet<{ logs: PipelineRunLog[] }>(`${BASE}/${id}/runs/${runId}/logs?limit=${limit}`)
}
