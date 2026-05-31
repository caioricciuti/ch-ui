import { apiGet, apiPost, apiPut, apiDel } from './client'
import type { GitHubIntegration, GitHubSyncLog, GitHubSyncResult } from '../types/models'

const BASE = '/api/admin/github'

export async function getGitHubIntegration(connectionId: string): Promise<GitHubIntegration | null> {
  const res = await apiGet<{ integration: GitHubIntegration | null }>(`${BASE}/${encodeURIComponent(connectionId)}`)
  return res.integration ?? null
}

export async function saveGitHubIntegration(connectionId: string, data: {
  repo: string; branch: string; path: string; pat?: string
}): Promise<void> {
  await apiPut(`${BASE}/${encodeURIComponent(connectionId)}`, data)
}

export async function deleteGitHubIntegration(connectionId: string): Promise<void> {
  await apiDel(`${BASE}/${encodeURIComponent(connectionId)}`)
}

export async function testGitHubConnection(connectionId: string): Promise<{ success: boolean; error?: string }> {
  return apiPost<{ success: boolean; error?: string }>(`${BASE}/${encodeURIComponent(connectionId)}/test`)
}

export async function triggerGitHubSync(connectionId: string): Promise<GitHubSyncResult> {
  const res = await apiPost<{ result: GitHubSyncResult }>(`${BASE}/${encodeURIComponent(connectionId)}/sync`)
  return res.result
}

export async function getGitHubSyncLogs(connectionId: string, limit = 20): Promise<GitHubSyncLog[]> {
  const res = await apiGet<{ logs: GitHubSyncLog[] }>(`${BASE}/${encodeURIComponent(connectionId)}/logs?limit=${limit}`)
  return res.logs ?? []
}
