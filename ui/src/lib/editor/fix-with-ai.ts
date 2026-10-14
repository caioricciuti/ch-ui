// SPDX-License-Identifier: BUSL-1.1
// Where an executed query sits in the editor, and when Fix with AI may
// rewrite it there. Shared by "Go to error" and "Fix with AI".

import { parseCHError } from '../utils/ch-error'

/** ClickHouse errors a model can fix by editing the SQL (matches the server). */
export const REPAIRABLE_CODES: ReadonlySet<number> = new Set([62, 47, 46, 60, 81, 43, 42, 215, 184, 53, 386, 352, 10, 36])

/** The parts of a result view that tie it to the editor text. */
export interface ExecutedText {
  /** The text sent to ClickHouse. */
  executedSql: string
  /** The plain editor statement the result came from. */
  baseSql: string
  /** Document offset of baseSql at run time, -1 when unknown. */
  executedFrom: number
}

/**
 * Document offset of the executed SQL, or -1. Only a plain run of the editor
 * text counts (not a wrapped or marked grid re-query). The offset it was run
 * from wins, so duplicated fragments resolve correctly. Otherwise the text is
 * searched for; with `unique` only a single occurrence is accepted, because
 * a replacement must not land on the wrong copy.
 */
export function locateExecuted(doc: string, view: ExecutedText, unique = false): number {
  if (!view.executedSql || view.executedSql !== view.baseSql) return -1
  if (view.executedFrom >= 0 && doc.startsWith(view.executedSql, view.executedFrom)) return view.executedFrom
  const at = doc.indexOf(view.executedSql)
  if (unique && at >= 0 && doc.indexOf(view.executedSql, at + 1) >= 0) return -1
  return at
}

/** What Fix with AI would rewrite, or null when it may not be offered. */
export function fixTarget(opts: { pro: boolean; error: string | null | undefined; doc: string; view: ExecutedText }): { from: number; sql: string } | null {
  if (!opts.pro || !opts.error) return null
  const code = parseCHError(opts.error).code
  if (code === null || !REPAIRABLE_CODES.has(code)) return null
  const from = locateExecuted(opts.doc, opts.view, true)
  if (from < 0) return null
  return { from, sql: opts.view.executedSql }
}

/**
 * The range to replace when the fix is accepted, or null when the original
 * text is no longer at `from` (the person edited it while the fix was open).
 */
export function fixRange(doc: string, from: number, original: string): { from: number; to: number } | null {
  if (from < 0 || !original || !doc.startsWith(original, from)) return null
  return { from, to: from + original.length }
}
