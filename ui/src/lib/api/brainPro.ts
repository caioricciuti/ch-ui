// SPDX-License-Identifier: BUSL-1.1
import { withBase } from '../basePath'
import { connectionHeaders } from '../stores/workspace-scope'
import { apiPost } from './client'
import type { BrainBudgetStatus, BrainUsage } from '../types/brain'

/** How generated SQL fared when ClickHouse checked it with EXPLAIN. */
export interface SQLValidation {
  /** passed, failed (ClickHouse still rejects it) or skipped (not checked). */
  status: 'passed' | 'failed' | 'skipped'
  error?: string
  /** Why the check was skipped or stopped early. */
  reason?: string
  /** How many repairs the model was asked for. */
  rounds: number
}

export interface GenerateSQLResult {
  success: boolean
  sql: string
  tables_used: string[]
  model: string
  usage?: BrainUsage
  /** The answer hit the output limit, so the SQL may be incomplete. */
  truncated?: boolean
  validation?: SQLValidation
  /** Present when an applicable budget is at 80% or more. */
  budget?: BrainBudgetStatus
}

export interface FixSQLResult extends GenerateSQLResult {
  /** The model's one-line reason for the change. */
  explanation: string
}

// Text-to-SQL (Pro): turn a plain-English question into a ClickHouse query,
// grounded in the active connection's schema + documented models.
export async function generateSQL(question: string, modelId?: string): Promise<GenerateSQLResult> {
  return apiPost<GenerateSQLResult>('/api/brain/generate-sql', { question, model_id: modelId })
}

interface SQLStreamEvent {
  type: 'delta' | 'done' | 'error' | 'validating' | 'repair' | 'reset'
  delta?: string
  error?: string
  round?: number
  sql?: string
  explanation?: string
  tables_used?: string[]
  model?: string
  usage?: BrainUsage
  truncated?: boolean
  validation?: SQLValidation
  budget?: BrainBudgetStatus
}

/** Progress callbacks of a streamed SQL request. */
export interface SQLStreamHandlers {
  /** Raw text as the model writes it. */
  ondelta: (text: string) => void
  /** The text written so far is discarded: a repair writes it again. */
  onreset?: () => void
  /** ClickHouse is checking the SQL with EXPLAIN. */
  onvalidating?: () => void
  /** ClickHouse rejected the SQL; the model is asked to repair it. */
  onrepair?: (round: number, error: string) => void
}

async function streamSQL(path: string, body: unknown, handlers: SQLStreamHandlers, signal?: AbortSignal): Promise<FixSQLResult> {
  const response = await fetch(withBase(path), {
    method: 'POST',
    credentials: 'include',
    headers: { 'Content-Type': 'application/json', ...connectionHeaders() },
    body: JSON.stringify(body),
    signal,
  })
  if (!response.ok) {
    const err = (await response.json().catch(() => ({}))) as { error?: string }
    throw new Error(err.error ?? `Request failed (${response.status})`)
  }
  const reader = response.body?.getReader()
  if (!reader) throw new Error('No response body')

  const decoder = new TextDecoder()
  let buffer = ''
  let result: FixSQLResult | null = null
  while (true) {
    const { done, value } = await reader.read()
    if (done) break
    buffer += decoder.decode(value, { stream: true })
    const lines = buffer.split('\n')
    buffer = lines.pop() ?? ''
    for (const line of lines) {
      if (!line.startsWith('data: ')) continue
      let ev: SQLStreamEvent
      try {
        ev = JSON.parse(line.slice(6)) as SQLStreamEvent
      } catch {
        continue
      }
      if (ev.type === 'delta' && ev.delta) handlers.ondelta(ev.delta)
      else if (ev.type === 'reset') handlers.onreset?.()
      else if (ev.type === 'validating') handlers.onvalidating?.()
      else if (ev.type === 'repair') handlers.onrepair?.(ev.round ?? 0, ev.error ?? '')
      else if (ev.type === 'error') throw new Error(ev.error ?? 'AI generation failed')
      else if (ev.type === 'done') {
        result = {
          success: true,
          sql: ev.sql ?? '',
          explanation: ev.explanation ?? '',
          tables_used: ev.tables_used ?? [],
          model: ev.model ?? '',
          usage: ev.usage,
          truncated: ev.truncated,
          validation: ev.validation,
          budget: ev.budget,
        }
      }
    }
  }
  if (!result) throw new Error('The AI stream ended before the answer was complete')
  return result
}

// Streaming Ask AI: ondelta gets the raw text as it is written; resolves
// with the cleaned SQL once the model is done and ClickHouse checked it.
export async function generateSQLStream(
  payload: { question: string; currentSql?: string; modelId?: string },
  handlers: SQLStreamHandlers,
  signal?: AbortSignal,
): Promise<GenerateSQLResult> {
  return streamSQL(
    '/api/brain/generate-sql/stream',
    { question: payload.question, current_sql: payload.currentSql, model_id: payload.modelId },
    handlers,
    signal,
  )
}

// Fix with AI (Pro): a minimal, meaning-preserving fix for a query that
// ClickHouse rejected, checked with EXPLAIN before it is offered.
export async function fixSQLStream(
  payload: { sql: string; error: string; params?: Record<string, string>; modelId?: string },
  handlers: SQLStreamHandlers,
  signal?: AbortSignal,
): Promise<FixSQLResult> {
  return streamSQL(
    '/api/brain/fix-sql/stream',
    { sql: payload.sql, error: payload.error, params: payload.params, model_id: payload.modelId },
    handlers,
    signal,
  )
}

export async function approveBrainToolCall(approvalId: string): Promise<void> {
  await apiPost(`/api/brain/approvals/${encodeURIComponent(approvalId)}/approve`)
}

export async function declineBrainToolCall(approvalId: string): Promise<void> {
  await apiPost(`/api/brain/approvals/${encodeURIComponent(approvalId)}/decline`)
}
