// SPDX-License-Identifier: BUSL-1.1
import { apiGet, apiPost, apiPut, apiDel } from './client'
import type {
  GovernanceOverview,
  GovernanceSettings,
  SyncResult,
  GovDatabase,
  GovTable,
  GovColumn,
  QueryLogEntry,
  QueryHarvestMode,
  QueryHarvestSettings,
  TagEntry,
  ChUser,
  ChRole,
  AccessMatrixEntry,
  OverPermission,
  Policy,
  PolicyViolation,
  GovernanceObjectComment,
  GovernanceIncident,
  GovernanceIncidentComment,
} from '../types/governance'

const BASE = '/api/governance'

// ── Overview / Sync ─────────────────────────────────────────────

export function fetchOverview() {
  return apiGet<{ overview?: GovernanceOverview } | GovernanceOverview>(`${BASE}/overview`)
    .then((res) => ('overview' in res && res.overview ? res.overview : res as GovernanceOverview))
}

export function triggerSync() {
  return apiPost<SyncResult>(`${BASE}/sync`)
}

// ── Settings (admin) ────────────────────────────────────────────

export function fetchGovernanceSettings() {
  return apiGet<GovernanceSettings>('/api/admin/governance/settings')
}

export function updateGovernanceSettings(payload: {
  sync_enabled?: boolean
  banner_dismissed?: boolean
}) {
  return apiPut<GovernanceSettings>('/api/admin/governance/settings', payload)
}

// ── Metadata ────────────────────────────────────────────────────

export function fetchDatabases() {
  return apiGet<{ databases: GovDatabase[] }>(`${BASE}/databases`)
}

export function fetchTables(params?: { database?: string; tag?: string; search?: string }) {
  const qs = new URLSearchParams()
  if (params?.database) qs.set('database', params.database)
  if (params?.tag) qs.set('tag', params.tag)
  if (params?.search) qs.set('search', params.search)
  const q = qs.toString()
  return apiGet<{ tables: GovTable[] }>(`${BASE}/tables${q ? '?' + q : ''}`)
}

interface TableDetailResponse {
  table: GovTable
  columns: GovColumn[]
  tags: TagEntry[]
  recent_queries?: QueryLogEntry[]
  queries?: QueryLogEntry[]
}

export function fetchTableDetail(database: string, table: string) {
  return apiGet<TableDetailResponse>(`${BASE}/tables/${encodeURIComponent(database)}/${encodeURIComponent(table)}`)
    .then((res) => ({
      ...res,
      recent_queries: res?.recent_queries ?? res?.queries ?? [],
    }))
}

export function fetchTableNotes(database: string, table: string) {
  return apiGet<{ notes: GovernanceObjectComment[] }>(`${BASE}/tables/${encodeURIComponent(database)}/${encodeURIComponent(table)}/notes`)
}

export function createTableNote(database: string, table: string, commentText: string) {
  return apiPost<{ id: string }>(`${BASE}/tables/${encodeURIComponent(database)}/${encodeURIComponent(table)}/notes`, { comment_text: commentText })
}

export function deleteObjectNote(id: string) {
  return apiDel(`${BASE}/notes/${encodeURIComponent(id)}`)
}

// ── Query Log ───────────────────────────────────────────────────

export function fetchQueryLog(params?: { user?: string; table?: string; limit?: number; offset?: number }) {
  const qs = new URLSearchParams()
  if (params?.user) qs.set('user', params.user)
  if (params?.table) qs.set('table', params.table)
  if (params?.limit) qs.set('limit', String(params.limit))
  if (params?.offset) qs.set('offset', String(params.offset))
  const q = qs.toString()
  return apiGet<{ entries: QueryLogEntry[]; total: number }>(`${BASE}/query-log${q ? '?' + q : ''}`)
}

export function fetchQueryHarvestSettings() {
  return apiGet<QueryHarvestSettings>(`${BASE}/query-harvest`)
}

export function updateQueryHarvestMode(mode: QueryHarvestMode) {
  return apiPut<QueryHarvestSettings>(`${BASE}/query-harvest`, { mode })
}

export function updateQueryHarvestMinDuration(minDurationMs: number) {
  return apiPut<QueryHarvestSettings>(`${BASE}/query-harvest`, { min_duration_ms: minDurationMs })
}

// ── Tags ────────────────────────────────────────────────────────

// ── Access ──────────────────────────────────────────────────────

export function fetchAccessUsers() {
  return apiGet<{ users: ChUser[] }>(`${BASE}/access/users`)
}

export function fetchAccessRoles() {
  return apiGet<{ roles: ChRole[] }>(`${BASE}/access/roles`)
}

export function fetchAccessMatrix(user?: string) {
  const q = user ? `?user=${encodeURIComponent(user)}` : ''
  return apiGet<{ matrix: AccessMatrixEntry[] }>(`${BASE}/access/matrix${q}`)
}

export function fetchOverPermissions(days = 30) {
  return apiGet<{ over_permissions: OverPermission[] }>(`${BASE}/access/over-permissions?days=${days}`)
}

// ── Policies ────────────────────────────────────────────────────

export function fetchPolicies() {
  return apiGet<{ policies: Policy[] }>(`${BASE}/policies`)
}

export function createPolicy(data: Partial<Policy>) {
  return apiPost<{ id: string }>(`${BASE}/policies`, data)
}

export function updatePolicy(id: string, data: Partial<Policy>) {
  return apiPut(`${BASE}/policies/${id}`, data)
}

export function deletePolicy(id: string) {
  return apiDel(`${BASE}/policies/${id}`)
}

// ── Violations ──────────────────────────────────────────────────

export function fetchViolations(params?: { policy_id?: string; limit?: number }) {
  const qs = new URLSearchParams()
  if (params?.policy_id) qs.set('policy_id', params.policy_id)
  if (params?.limit) qs.set('limit', String(params.limit))
  const q = qs.toString()
  return apiGet<{ violations: PolicyViolation[] }>(`${BASE}/violations${q ? '?' + q : ''}`)
}

export function promoteViolationToIncident(id: string) {
  return apiPost<{ incident_id: string; created: boolean }>(`${BASE}/violations/${encodeURIComponent(id)}/incident`)
}

export function fetchIncidents(params?: { status?: string; severity?: string; limit?: number }) {
  const qs = new URLSearchParams()
  if (params?.status) qs.set('status', params.status)
  if (params?.severity) qs.set('severity', params.severity)
  if (params?.limit) qs.set('limit', String(params.limit))
  const q = qs.toString()
  return apiGet<{ incidents: GovernanceIncident[] }>(`${BASE}/incidents${q ? '?' + q : ''}`)
}

export function getIncident(id: string) {
  return apiGet<{ incident: GovernanceIncident }>(`${BASE}/incidents/${encodeURIComponent(id)}`)
}

export function createIncident(data: {
  source_type?: string
  source_ref?: string
  title: string
  severity?: string
  status?: string
  assignee?: string
  details?: string
}) {
  return apiPost<{ id: string }>(`${BASE}/incidents`, data)
}

export function updateIncident(id: string, data: {
  title?: string
  severity?: string
  status?: string
  assignee?: string
  details?: string
  resolution_note?: string
}) {
  return apiPut(`${BASE}/incidents/${encodeURIComponent(id)}`, data)
}

export function fetchIncidentComments(id: string) {
  return apiGet<{ comments: GovernanceIncidentComment[] }>(`${BASE}/incidents/${encodeURIComponent(id)}/comments`)
}

export function createIncidentComment(id: string, commentText: string) {
  return apiPost<{ id: string }>(`${BASE}/incidents/${encodeURIComponent(id)}/comments`, { comment_text: commentText })
}
