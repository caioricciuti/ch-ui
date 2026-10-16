import { apiGet, apiPost, apiDel } from './client'
import type { IngestInfo, IngestKey, IngestStats } from '../types/ingest'

const BASE = '/api/pipelines'

export function listIngestKeys(pipelineId: string) {
  return apiGet<{ keys: IngestKey[] }>(`${BASE}/${encodeURIComponent(pipelineId)}/ingest-keys`)
}

/** The secret is only in this response; the server keeps a hash. */
export function createIngestKey(pipelineId: string, name: string) {
  return apiPost<{ key: IngestKey; secret: string }>(`${BASE}/${encodeURIComponent(pipelineId)}/ingest-keys`, { name })
}

export function revokeIngestKey(pipelineId: string, keyId: string) {
  return apiDel(`${BASE}/${encodeURIComponent(pipelineId)}/ingest-keys/${encodeURIComponent(keyId)}`)
}

export function getIngestStats(pipelineId: string) {
  return apiGet<IngestStats>(`${BASE}/${encodeURIComponent(pipelineId)}/ingest-stats`)
}

export function getIngestInfo() {
  return apiGet<IngestInfo>('/api/ingest/info')
}
