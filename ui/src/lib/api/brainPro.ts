// SPDX-License-Identifier: BUSL-1.1
import { apiPost } from './client'

export interface GenerateSQLResult {
  success: boolean
  sql: string
  tables_used: string[]
  model: string
}

// Text-to-SQL (Pro): turn a plain-English question into a ClickHouse query,
// grounded in the active connection's schema + documented models.
export async function generateSQL(question: string, modelId?: string): Promise<GenerateSQLResult> {
  return apiPost<GenerateSQLResult>('/api/brain/generate-sql', { question, model_id: modelId })
}

export async function approveBrainToolCall(approvalId: string): Promise<void> {
  await apiPost(`/api/brain/approvals/${encodeURIComponent(approvalId)}/approve`)
}

export async function declineBrainToolCall(approvalId: string): Promise<void> {
  await apiPost(`/api/brain/approvals/${encodeURIComponent(approvalId)}/decline`)
}
