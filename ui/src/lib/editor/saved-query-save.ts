// Saving the editor over its linked saved query. The tab remembers the
// version it loaded; the save sends it as if_version and the server answers
// 409 with the stored query when someone saved or restored it since.

import type { SavedQuery } from '../types/api'

export interface LinkedSaveBody {
  query: string
  parameters: Record<string, string>
  if_version?: number
}

/** The PUT body; without a known version (tabs from older builds) nothing is checked. */
export function linkedSaveBody(query: string, parameters: Record<string, string>, version: number | undefined): LinkedSaveBody {
  return version === undefined ? { query, parameters } : { query, parameters, if_version: version }
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null
}

/** The stored saved query when a save was refused because it changed meanwhile, else null. */
export function savedQueryConflict(e: unknown): SavedQuery | null {
  if (!isRecord(e) || e.status !== 409 || !isRecord(e.body)) return null
  const body = e.body
  if (body.code !== 'saved_query_conflict' || !isRecord(body.saved_query)) return null
  const q = body.saved_query
  if (typeof q.id !== 'string' || typeof q.query !== 'string' || typeof q.version !== 'number') return null
  return q as unknown as SavedQuery
}
