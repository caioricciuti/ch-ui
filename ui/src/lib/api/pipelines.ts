import { apiGet, apiPost, apiPut, apiDel } from './client'
import type { Pipeline, PipelineCheckpoint, PipelineGraph, PipelineRun, PipelineRunLog, PipelineStatusInfo, PipelineTestResult } from '../types/pipelines'
import type { PreviewRequest, PreviewResponse } from '../types/transforms'

const BASE = '/api/pipelines'

export function listPipelines() {
  return apiGet<{ pipelines: Pipeline[] }>(BASE)
}

export function createPipeline(data: { name: string; description?: string }) {
  return apiPost<{ pipeline: Pipeline }>(BASE, data)
}

export function getPipeline(id: string) {
  return apiGet<{ pipeline: Pipeline; graph: PipelineGraph }>(`${BASE}/${id}`)
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
  return apiGet<PipelineStatusInfo>(`${BASE}/${id}/status`)
}

export function listRuns(id: string, limit = 50) {
  return apiGet<{ runs: PipelineRun[] }>(`${BASE}/${id}/runs?limit=${limit}`)
}

export function getRunLogs(id: string, runId: string, limit = 500) {
  return apiGet<{ logs: PipelineRunLog[] }>(`${BASE}/${id}/runs/${encodeURIComponent(runId)}/logs?limit=${limit}`)
}

/** Reads a sample from the saved pipeline's source and checks its sink. Writes nothing. */
export function testPipeline(id: string) {
  return apiPost<PipelineTestResult>(`${BASE}/${id}/test`)
}

/** Forgets the saved source position; the next start reads from the beginning. */
export function resetPosition(id: string) {
  return apiPost<{ cleared: number }>(`${BASE}/${id}/reset-position`)
}

export function listCheckpoints(id: string) {
  return apiGet<{ checkpoints: PipelineCheckpoint[] }>(`${BASE}/${id}/checkpoints`)
}

/**
 * Runs the given transform chain (unsaved configs, in chain order) on sample
 * records. Without records the server uses recent live records or the source
 * sample, else answers 400. Writes nothing.
 */
export function previewTransforms(id: string, req: PreviewRequest) {
  return apiPost<PreviewResponse>(`${BASE}/${id}/preview-transforms`, req)
}
