// SPDX-License-Identifier: BUSL-1.1
import { withBase } from '../basePath'
import { apiPost } from './client'
import type { BrainUsage } from '../types/brain'

export interface GenerateSQLResult {
  success: boolean
  sql: string
  tables_used: string[]
  model: string
  usage?: BrainUsage
  /** The answer hit the output limit, so the SQL may be incomplete. */
  truncated?: boolean
}

// Text-to-SQL (Pro): turn a plain-English question into a ClickHouse query,
// grounded in the active connection's schema + documented models.
export async function generateSQL(question: string, modelId?: string): Promise<GenerateSQLResult> {
  return apiPost<GenerateSQLResult>('/api/brain/generate-sql', { question, model_id: modelId })
}

interface GenerateSQLEvent {
  type: 'delta' | 'done' | 'error'
  delta?: string
  error?: string
  sql?: string
  tables_used?: string[]
  model?: string
  usage?: BrainUsage
  truncated?: boolean
}

// Streaming Ask AI: ondelta gets the raw text as it is written; resolves
// with the cleaned SQL once the model is done.
export async function generateSQLStream(
  payload: { question: string; currentSql?: string; modelId?: string },
  ondelta: (text: string) => void,
  signal?: AbortSignal,
): Promise<GenerateSQLResult> {
  const response = await fetch(withBase('/api/brain/generate-sql/stream'), {
    method: 'POST',
    credentials: 'include',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ question: payload.question, current_sql: payload.currentSql, model_id: payload.modelId }),
    signal,
  })
  if (!response.ok) {
    const body = (await response.json().catch(() => ({}))) as { error?: string }
    throw new Error(body.error ?? `Request failed (${response.status})`)
  }
  const reader = response.body?.getReader()
  if (!reader) throw new Error('No response body')

  const decoder = new TextDecoder()
  let buffer = ''
  let result: GenerateSQLResult | null = null
  while (true) {
    const { done, value } = await reader.read()
    if (done) break
    buffer += decoder.decode(value, { stream: true })
    const lines = buffer.split('\n')
    buffer = lines.pop() ?? ''
    for (const line of lines) {
      if (!line.startsWith('data: ')) continue
      let ev: GenerateSQLEvent
      try {
        ev = JSON.parse(line.slice(6)) as GenerateSQLEvent
      } catch {
        continue
      }
      if (ev.type === 'delta' && ev.delta) ondelta(ev.delta)
      else if (ev.type === 'error') throw new Error(ev.error ?? 'AI generation failed')
      else if (ev.type === 'done') {
        result = {
          success: true,
          sql: ev.sql ?? '',
          tables_used: ev.tables_used ?? [],
          model: ev.model ?? '',
          usage: ev.usage,
          truncated: ev.truncated,
        }
      }
    }
  }
  if (!result) throw new Error('The AI stream ended before the answer was complete')
  return result
}

export async function approveBrainToolCall(approvalId: string): Promise<void> {
  await apiPost(`/api/brain/approvals/${encodeURIComponent(approvalId)}/approve`)
}

export async function declineBrainToolCall(approvalId: string): Promise<void> {
  await apiPost(`/api/brain/approvals/${encodeURIComponent(approvalId)}/decline`)
}
