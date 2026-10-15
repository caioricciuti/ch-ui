/**
 * The Lineage page keeps its view in the query string so a link opens the
 * same picture: ?databases=a,b&focus=db.name&hops=2.
 */
import { DEFAULT_HOPS } from './lineage-layout'

export const MIN_HOPS = 1
export const MAX_HOPS = 4

export interface LineageUrlState {
  databases: string[]
  focus: string | null
  hops: number
}

export function parseLineageSearch(search: string): LineageUrlState {
  const params = new URLSearchParams(search)
  const databases = [...new Set((params.get('databases') ?? '').split(',').map((d) => d.trim()).filter(Boolean))]
  const focus = params.get('focus')?.trim() || null
  const raw = Number(params.get('hops'))
  const hops = Number.isInteger(raw) && raw >= MIN_HOPS && raw <= MAX_HOPS ? raw : DEFAULT_HOPS
  return { databases, focus, hops }
}

/**
 * Writes the state into an existing query string, leaving other params
 * alone. Defaults are left out so the plain page keeps a clean URL.
 */
export function buildLineageSearch(state: LineageUrlState, current = ''): string {
  const params = new URLSearchParams(current)
  if (state.databases.length > 0) params.set('databases', state.databases.join(','))
  else params.delete('databases')
  if (state.focus) params.set('focus', state.focus)
  else params.delete('focus')
  if (state.hops !== DEFAULT_HOPS) params.set('hops', String(state.hops))
  else params.delete('hops')
  // Keep commas and dots readable in shared links.
  const out = params.toString().replace(/%2C/gi, ',')
  return out ? `?${out}` : ''
}

/**
 * Database part of a `db.name` id. External ids (`external:<descriptor>`)
 * and ids without a dot have none. A guess for URLs only: database names
 * can contain dots, so prefer the node's own database field.
 */
export function databaseOf(id: string): string {
  if (id.startsWith('external:')) return ''
  const dot = id.indexOf('.')
  return dot > 0 ? id.slice(0, dot) : ''
}
