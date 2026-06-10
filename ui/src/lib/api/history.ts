import { apiGet, apiDel } from './client'

export interface QueryHistoryEntry {
  id: string
  connection_id: string | null
  clickhouse_user: string
  query_text: string
  status: 'success' | 'error' | 'cancelled'
  error_message: string | null
  elapsed_ms: number | null
  rows_returned: number | null
  created_at: string
}

export interface FetchQueryHistoryParams {
  search?: string
  status?: 'success' | 'error' | ''
  limit?: number
  offset?: number
}

export async function fetchQueryHistory(params: FetchQueryHistoryParams = {}): Promise<QueryHistoryEntry[]> {
  const qs = new URLSearchParams()
  if (params.search) qs.set('search', params.search)
  if (params.status) qs.set('status', params.status)
  if (params.limit) qs.set('limit', String(params.limit))
  if (params.offset) qs.set('offset', String(params.offset))
  const suffix = qs.toString() ? `?${qs.toString()}` : ''
  const res = await apiGet<{ entries: QueryHistoryEntry[] }>(`/api/query-history${suffix}`)
  return res.entries ?? []
}

export async function deleteQueryHistoryEntry(id: string): Promise<void> {
  await apiDel(`/api/query-history/${id}`)
}

export async function clearQueryHistory(): Promise<void> {
  await apiDel('/api/query-history')
}
