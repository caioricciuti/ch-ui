// Result tabs: every query tab keeps a short list of results, one per run
// statement, plus the index of the one on screen. This module is the pure
// part (no Svelte state, no storage): adding with eviction, pinning, and the
// compact form pinned results are saved in across reloads.

import type { ColumnMeta, QueryStats } from '../types/query'
import { maskNonCode } from './statements'

/** Most results one query tab keeps. */
export const MAX_RESULTS_PER_TAB = 10
/** Most pinned results one query tab keeps. */
export const MAX_PINNED_PER_TAB = 5
/** Rows of a pinned result saved for a page reload. */
export const MAX_PERSISTED_ROWS = 1000
/** A pinned result whose saved form is larger than this keeps only metadata. */
export const MAX_PERSISTED_CHARS = 1_500_000

export interface ResultEntry {
  id: string
  /** Short name shown on the result tab, e.g. "2 · SELECT count". */
  label: string
  /** The statement that produced this result (the base SQL, not a grid re-query). */
  sql: string
  /** Bind parameters it ran with. */
  params?: Record<string, string>
  meta: ColumnMeta[]
  data: unknown[][]
  stats: QueryStats | null
  elapsedMs: number
  error: string | null
  running: boolean
  pinned: boolean
  createdAt: number
  /** Restored from storage with fewer rows than it had: re-run to reload. */
  rowsDropped?: boolean
  /** Row count before rows were dropped for storage. */
  savedRowCount?: number
}

export interface TabResults {
  results: ResultEntry[]
  /** Index into results of the result on screen; -1 when there is none. */
  activeResult: number
}

export const EMPTY_TAB_RESULTS: TabResults = { results: [], activeResult: -1 }

/** "n · FIRST KEYWORDS" for a statement, n being its position in the editor. */
export function resultLabel(sql: string, statementNumber: number | null): string {
  const words = maskNonCode(sql).match(/[A-Za-z_][A-Za-z0-9_.]*|\*/g) ?? []
  let head = ''
  for (const w of words.slice(0, 3)) {
    const next = head ? `${head} ${w}` : w
    if (next.length > 28) break
    head = next
  }
  const kw = head ? head.replace(/^[a-z_]+/i, (m) => m.toUpperCase()) : 'Query'
  return statementNumber !== null ? `${statementNumber} · ${kw}` : kw
}

/**
 * Adds entry and makes it active. When the tab is over MAX_RESULTS_PER_TAB,
 * the oldest unpinned results (never entry itself) are evicted first.
 */
export function addResult(state: TabResults, entry: ResultEntry): TabResults {
  const results = [...state.results, entry]
  while (results.length > MAX_RESULTS_PER_TAB) {
    let oldest = -1
    for (let i = 0; i < results.length; i++) {
      const r = results[i]
      if (r.pinned || r.id === entry.id) continue
      if (oldest < 0 || r.createdAt < results[oldest].createdAt) oldest = i
    }
    if (oldest < 0) break
    results.splice(oldest, 1)
  }
  return { results, activeResult: results.findIndex((r) => r.id === entry.id) }
}

/**
 * Puts entry in place of the active result, or appends it when the active
 * result is pinned (pinned results are never replaced) or there is none.
 */
export function replaceActiveResult(state: TabResults, entry: ResultEntry): TabResults {
  const active = state.results[state.activeResult]
  if (!active || active.pinned) return addResult(state, entry)
  const results = state.results.map((r, i) => (i === state.activeResult ? entry : r))
  return { results, activeResult: state.activeResult }
}

/** Drops every unpinned result: a run of several statements starts clean. */
export function dropUnpinned(state: TabResults): TabResults {
  const active = state.results[state.activeResult]
  const results = state.results.filter((r) => r.pinned)
  const idx = active ? results.findIndex((r) => r.id === active.id) : -1
  return { results, activeResult: idx >= 0 ? idx : results.length - 1 }
}

export function updateResult(state: TabResults, id: string, partial: Partial<ResultEntry>): TabResults {
  let changed = false
  const results = state.results.map((r) => {
    if (r.id !== id) return r
    changed = true
    return { ...r, ...partial }
  })
  return changed ? { ...state, results } : state
}

export function removeResult(state: TabResults, id: string): TabResults {
  const idx = state.results.findIndex((r) => r.id === id)
  if (idx < 0) return state
  const results = state.results.filter((r) => r.id !== id)
  let activeResult = state.activeResult
  if (idx < activeResult || activeResult >= results.length) activeResult--
  if (results.length > 0 && activeResult < 0) activeResult = 0
  return { results, activeResult: results.length === 0 ? -1 : activeResult }
}

export function setActiveResult(state: TabResults, id: string): TabResults {
  const idx = state.results.findIndex((r) => r.id === id)
  return idx < 0 || idx === state.activeResult ? state : { ...state, activeResult: idx }
}

export type PinOutcome = 'pinned' | 'unpinned' | 'limit'

/** Toggles a result's pin. Refuses ('limit') a pin past MAX_PINNED_PER_TAB. */
export function togglePin(state: TabResults, id: string): { state: TabResults; outcome: PinOutcome } {
  const entry = state.results.find((r) => r.id === id)
  if (!entry) return { state, outcome: 'unpinned' }
  if (entry.pinned) return { state: updateResult(state, id, { pinned: false }), outcome: 'unpinned' }
  if (state.results.filter((r) => r.pinned).length >= MAX_PINNED_PER_TAB) return { state, outcome: 'limit' }
  return { state: updateResult(state, id, { pinned: true }), outcome: 'pinned' }
}

/** Keeps at most maxRows rows (the result row limit) in memory. */
export function capRows(data: unknown[][], maxRows: number): unknown[][] {
  return maxRows > 0 && data.length > maxRows ? data.slice(0, maxRows) : data
}

/**
 * The saved form of a tab's pinned results: metadata plus up to
 * MAX_PERSISTED_ROWS rows each. A result too large to store keeps metadata
 * only. Results still running are not saved.
 */
export function serializePinned(state: TabResults): ResultEntry[] {
  const out: ResultEntry[] = []
  for (const r of state.results) {
    if (!r.pinned || r.running) continue
    const total = r.rowsDropped ? (r.savedRowCount ?? r.data.length) : r.data.length
    let data = r.data.slice(0, MAX_PERSISTED_ROWS)
    let entry: ResultEntry = { ...r, data, rowsDropped: total > data.length, savedRowCount: total }
    if (JSON.stringify(entry).length > MAX_PERSISTED_CHARS) {
      data = []
      entry = { ...entry, data, rowsDropped: total > 0 }
    }
    out.push(entry)
  }
  return out
}

function isRecord(v: unknown): v is Record<string, unknown> {
  return typeof v === 'object' && v !== null && !Array.isArray(v)
}

/** Reads saved pinned results back, skipping anything malformed. */
export function restorePinned(raw: unknown): ResultEntry[] {
  if (!Array.isArray(raw)) return []
  const out: ResultEntry[] = []
  for (const item of raw) {
    if (!isRecord(item)) continue
    const { id, label, sql, meta, data } = item
    if (typeof id !== 'string' || typeof label !== 'string' || typeof sql !== 'string') continue
    if (!Array.isArray(meta) || !Array.isArray(data) || !data.every(Array.isArray)) continue
    const metaOk = meta.every((m) => isRecord(m) && typeof m.name === 'string' && typeof m.type === 'string')
    if (!metaOk) continue
    const params = isRecord(item.params) ? Object.fromEntries(Object.entries(item.params).filter(([, v]) => typeof v === 'string')) as Record<string, string> : undefined
    out.push({
      id,
      label,
      sql,
      params,
      meta: meta as ColumnMeta[],
      data: data as unknown[][],
      stats: isRecord(item.stats) ? (item.stats as unknown as QueryStats) : null,
      elapsedMs: typeof item.elapsedMs === 'number' ? item.elapsedMs : 0,
      error: typeof item.error === 'string' ? item.error : null,
      running: false,
      pinned: true,
      createdAt: typeof item.createdAt === 'number' ? item.createdAt : 0,
      rowsDropped: item.rowsDropped === true,
      savedRowCount: typeof item.savedRowCount === 'number' ? item.savedRowCount : data.length,
    })
    if (out.length >= MAX_PINNED_PER_TAB) break
  }
  return out
}
