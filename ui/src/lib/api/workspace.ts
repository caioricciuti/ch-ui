import { apiGet } from './client'
import type { Dashboard, SavedQuery } from '../types/api'

export async function listWorkspaceDashboards(): Promise<Dashboard[]> {
  const res = await apiGet<{ success: boolean; dashboards: Dashboard[] }>('/api/dashboards/')
  return res.dashboards ?? []
}

export async function listWorkspaceSavedQueries(): Promise<SavedQuery[]> {
  const res = await apiGet<{ saved_queries: SavedQuery[] }>('/api/saved-queries/')
  return res.saved_queries ?? []
}
