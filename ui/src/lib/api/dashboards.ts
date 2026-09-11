import { apiGet, apiPost, apiPut, apiDel } from './client'
import type { Dashboard, DashboardFolder } from '../types/api'

const BASE = '/api/dashboards'

export async function listDashboards(): Promise<Dashboard[]> {
  const res = await apiGet<{ dashboards: Dashboard[] }>(`${BASE}/`)
  return res.dashboards ?? []
}

export async function listDashboardFolders(): Promise<DashboardFolder[]> {
  const res = await apiGet<{ folders: DashboardFolder[] }>(`${BASE}/folders`)
  return res.folders ?? []
}

export async function createDashboardFolder(name: string, parentId: string | null): Promise<DashboardFolder> {
  const res = await apiPost<{ folder: DashboardFolder }>(`${BASE}/folders`, { name, parent_id: parentId ?? '' })
  return res.folder
}

export async function updateDashboardFolder(
  id: string,
  patch: { name?: string; parent_id?: string | null },
): Promise<DashboardFolder> {
  const body: Record<string, string> = {}
  if (patch.name !== undefined) body.name = patch.name
  if (patch.parent_id !== undefined) body.parent_id = patch.parent_id ?? ''
  const res = await apiPut<{ folder: DashboardFolder }>(`${BASE}/folders/${id}`, body)
  return res.folder
}

export async function deleteDashboardFolder(id: string): Promise<void> {
  await apiDel(`${BASE}/folders/${id}`)
}

export async function createDashboard(input: {
  name: string
  description?: string
  folder_id?: string | null
  tags?: string[]
}): Promise<Dashboard> {
  const res = await apiPost<{ dashboard: Dashboard }>(`${BASE}/`, {
    name: input.name,
    description: input.description ?? '',
    folder_id: input.folder_id ?? '',
    tags: input.tags ?? [],
  })
  return res.dashboard
}

export async function moveDashboard(id: string, folderId: string | null): Promise<void> {
  await apiPut(`${BASE}/${id}/move`, { folder_id: folderId ?? '' })
}

export async function setDashboardTags(id: string, tags: string[]): Promise<Dashboard> {
  const res = await apiPut<{ dashboard: Dashboard }>(`${BASE}/${id}/tags`, { tags })
  return res.dashboard
}

export async function setDashboardStar(id: string, starred: boolean): Promise<void> {
  if (starred) await apiPost(`${BASE}/${id}/star`, {})
  else await apiDel(`${BASE}/${id}/star`)
}

export async function deleteDashboard(id: string): Promise<void> {
  await apiDel(`${BASE}/${id}`)
}

export async function renameDashboard(id: string, name: string): Promise<void> {
  await apiPut(`${BASE}/${id}`, { name })
}

/** "Ops / Cluster" style path for a folder id; empty for the root. */
export function folderPath(folders: DashboardFolder[], folderId: string | null | undefined): string[] {
  const byId = new Map(folders.map((f) => [f.id, f]))
  const path: string[] = []
  let current = folderId ? byId.get(folderId) : undefined
  for (let i = 0; current && i < 64; i++) {
    path.unshift(current.name)
    current = current.parent_id ? byId.get(current.parent_id) : undefined
  }
  return path
}
