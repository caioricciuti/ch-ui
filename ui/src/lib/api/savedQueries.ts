import { apiGet, apiPost, apiPut, apiDel } from './client'
import { folderPath } from './dashboards'
import type { SavedQuery, SavedQueryFolder, SavedQueryVersion, SavedQueryVisibility } from '../types/api'

const BASE = '/api/saved-queries'

/** The page sections, driven by `?section=` (routes.ts PAGE_SECTIONS). */
export type SavedQueryScope = 'all' | 'mine' | 'shared' | 'verified'

const SCOPES: readonly SavedQueryScope[] = ['all', 'mine', 'shared', 'verified']

/** The scope a `?section=` value selects; anything unknown is All. */
export function scopeForSection(section: string | null | undefined): SavedQueryScope {
  return SCOPES.find((s) => s === section) ?? 'all'
}

export async function listSavedQueries(opts: { scope?: SavedQueryScope; allOwners?: boolean } = {}): Promise<SavedQuery[]> {
  const params = new URLSearchParams()
  if (opts.scope && opts.scope !== 'all') params.set('scope', opts.scope)
  if (opts.allOwners) params.set('owners', 'all')
  const qs = params.toString()
  const res = await apiGet<{ saved_queries: SavedQuery[] }>(`${BASE}/${qs ? `?${qs}` : ''}`)
  return res.saved_queries ?? []
}

/** Fields of the details form; also accepted by create. */
export interface SavedQueryDetails {
  name: string
  description: string
  folder_id: string | null
  tags: string[]
  visibility: SavedQueryVisibility
}

export async function updateSavedQuery(id: string, patch: Partial<SavedQueryDetails> & { verified?: boolean }): Promise<SavedQuery> {
  const body: Record<string, unknown> = { ...patch }
  if (patch.folder_id !== undefined) body.folder_id = patch.folder_id ?? ''
  return apiPut<SavedQuery>(`${BASE}/${id}`, body)
}

export async function duplicateSavedQuery(id: string): Promise<SavedQuery> {
  return apiPost<SavedQuery>(`${BASE}/${id}/duplicate`)
}

export async function deleteSavedQuery(id: string): Promise<void> {
  await apiDel(`${BASE}/${id}`)
}

export async function listSavedQueryFolders(): Promise<SavedQueryFolder[]> {
  const res = await apiGet<{ folders: SavedQueryFolder[] }>(`${BASE}/folders`)
  return res.folders ?? []
}

export async function createSavedQueryFolder(name: string, parentId: string | null): Promise<SavedQueryFolder> {
  const res = await apiPost<{ folder: SavedQueryFolder }>(`${BASE}/folders`, { name, parent_id: parentId ?? '' })
  return res.folder
}

export async function renameSavedQueryFolder(id: string, name: string): Promise<SavedQueryFolder> {
  const res = await apiPut<{ folder: SavedQueryFolder }>(`${BASE}/folders/${id}`, { name })
  return res.folder
}

/** Deletes a folder and its subfolders; returns how many queries moved to the root. */
export async function deleteSavedQueryFolder(id: string): Promise<number> {
  const res = await apiDel<{ moved?: number }>(`${BASE}/folders/${id}`)
  return res?.moved ?? 0
}

export async function listSavedQueryVersions(id: string): Promise<SavedQueryVersion[]> {
  const res = await apiGet<{ versions: SavedQueryVersion[] }>(`${BASE}/${id}/versions`)
  return res.versions ?? []
}

export async function getSavedQueryVersion(id: string, version: number): Promise<SavedQueryVersion> {
  const res = await apiGet<{ version: SavedQueryVersion }>(`${BASE}/${id}/versions/${version}`)
  return res.version
}

export async function restoreSavedQueryVersion(id: string, version: number): Promise<void> {
  await apiPost(`${BASE}/${id}/versions/${version}/restore`)
}

// ── Pure helpers (shared by the page, the sheets and the save form) ──

export const VISIBILITY_LABELS: Record<SavedQueryVisibility, string> = {
  private: 'Only me',
  shared: 'Everyone',
  admins: 'Admins',
}

/** Visibility choices; only admins may limit a query to admins. */
export function visibilityOptions(isAdmin: boolean, current?: SavedQueryVisibility): { value: SavedQueryVisibility; label: string }[] {
  const opts: { value: SavedQueryVisibility; label: string }[] = [
    { value: 'shared', label: 'Everyone' },
    { value: 'private', label: 'Only me' },
  ]
  if (isAdmin || current === 'admins') opts.push({ value: 'admins', label: 'Admins and me' })
  return opts
}

/** Select options for a folder picker, nested paths in tree order. */
export function folderOptions(folders: SavedQueryFolder[], rootLabel = 'No folder (root)'): { value: string; label: string }[] {
  const byParent = new Map<string | null, SavedQueryFolder[]>()
  for (const f of folders) {
    const list = byParent.get(f.parent_id ?? null) ?? []
    list.push(f)
    byParent.set(f.parent_id ?? null, list)
  }
  for (const list of byParent.values()) list.sort((a, b) => a.name.localeCompare(b.name))
  const out: { value: string; label: string }[] = [{ value: '', label: rootLabel }]
  const walk = (parentId: string | null, prefix: string, depth: number) => {
    if (depth > 64) return
    for (const f of byParent.get(parentId) ?? []) {
      const label = prefix ? `${prefix} / ${f.name}` : f.name
      out.push({ value: f.id, label })
      walk(f.id, label, depth + 1)
    }
  }
  walk(null, '', 0)
  return out
}

export function folderLabel(folders: SavedQueryFolder[], folderId: string | null | undefined): string {
  return folderPath(folders, folderId).join(' / ')
}

/** Splits a comma-separated tag field. */
export function parseTags(raw: string): string[] {
  const seen = new Set<string>()
  const out: string[] = []
  for (const t of raw.split(',')) {
    const tag = t.trim()
    if (!tag || seen.has(tag.toLowerCase())) continue
    seen.add(tag.toLowerCase())
    out.push(tag)
  }
  return out
}

/** Validation of the details sheet; null when the form can be saved. */
export function validateSavedQueryDetails(form: { name: string; tags: string }, isAdmin: boolean, visibility: SavedQueryVisibility, previous?: SavedQueryVisibility): string | null {
  if (!form.name.trim()) return 'Name is required'
  const tags = parseTags(form.tags)
  if (tags.length > 20) return 'At most 20 tags'
  if (tags.some((t) => t.length > 40)) return 'Tags are limited to 40 characters'
  if (visibility === 'admins' && !isAdmin && previous !== 'admins') return 'Only admins can limit a query to admins'
  return null
}
