import { apiGet, apiPost, apiPut, apiDel } from './client'
import type {
  Dashboard,
  DashboardFolder,
  DashboardTemplate,
  DashboardVariable,
  DashboardVariableOption,
  DashboardVersion,
  Panel,
} from '../types/api'

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

/** Sentinel value for "All" on a variable with include_all. */
export const VARIABLE_ALL = '$__all'

/** Full dashboard as returned by detail, import, restore, template and duplicate. */
export interface DashboardDetail {
  dashboard: Dashboard
  panels: Panel[]
  variables: DashboardVariable[]
}

function normalizeDetail(res: Partial<DashboardDetail>): DashboardDetail {
  return {
    dashboard: res.dashboard as Dashboard,
    panels: res.panels ?? [],
    variables: res.variables ?? [],
  }
}

export async function saveDashboardVariables(id: string, variables: DashboardVariable[]): Promise<DashboardVariable[]> {
  const res = await apiPut<{ variables: DashboardVariable[] }>(`${BASE}/${id}/variables`, { variables })
  return res.variables ?? []
}

export interface VariableOptionsRequest {
  /** Current values of the variables (parents are taken from here). */
  variables?: Record<string, string[]>
  time_range?: unknown
  /** Preview an unsaved definition (editor). */
  definition?: DashboardVariable
}

export async function fetchVariableOptions(
  dashboardId: string,
  name: string,
  body: VariableOptionsRequest,
): Promise<{ options: DashboardVariableOption[]; truncated: boolean }> {
  const res = await apiPost<{ options: DashboardVariableOption[]; truncated: boolean }>(
    `${BASE}/${dashboardId}/variables/${encodeURIComponent(name)}/options`,
    body,
  )
  return { options: res.options ?? [], truncated: !!res.truncated }
}

export async function fetchPublicVariableOptions(
  token: string,
  name: string,
  body: VariableOptionsRequest,
  key?: string | null,
): Promise<{ options: DashboardVariableOption[]; truncated: boolean }> {
  const qs = key ? `?key=${encodeURIComponent(key)}` : ''
  const res = await apiPost<{ options: DashboardVariableOption[]; truncated: boolean }>(
    `/api/public/dashboards/${token}/variables/${encodeURIComponent(name)}/options${qs}`,
    body,
  )
  return { options: res.options ?? [], truncated: !!res.truncated }
}

export async function listDashboardVersions(id: string): Promise<DashboardVersion[]> {
  const res = await apiGet<{ versions: DashboardVersion[] }>(`${BASE}/${id}/versions`)
  return res.versions ?? []
}

export async function getDashboardVersion(id: string, version: number): Promise<DashboardVersion> {
  const res = await apiGet<{ version: DashboardVersion }>(`${BASE}/${id}/versions/${version}`)
  return res.version
}

export async function restoreDashboardVersion(id: string, version: number): Promise<DashboardDetail> {
  const res = await apiPost<Partial<DashboardDetail>>(`${BASE}/${id}/versions/${version}/restore`, {})
  return normalizeDetail(res)
}

export async function listDashboardTemplates(): Promise<DashboardTemplate[]> {
  const res = await apiGet<{ templates: DashboardTemplate[] }>(`${BASE}/templates`)
  return res.templates ?? []
}

export async function createDashboardFromTemplate(input: {
  template_id: string
  name?: string
  folder_id?: string | null
}): Promise<DashboardDetail> {
  const res = await apiPost<Partial<DashboardDetail>>(`${BASE}/from-template`, {
    template_id: input.template_id,
    name: input.name ?? '',
    folder_id: input.folder_id ?? '',
  })
  return normalizeDetail(res)
}

export async function duplicateDashboard(id: string, name?: string): Promise<DashboardDetail> {
  const res = await apiPost<Partial<DashboardDetail>>(`${BASE}/${id}/duplicate`, { name: name ?? '' })
  return normalizeDetail(res)
}
