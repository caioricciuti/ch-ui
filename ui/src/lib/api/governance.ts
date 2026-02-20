import { apiGet, apiPost, apiPut, apiDel } from './client'
import type {
  GovernanceOverview,
  SyncResult,
  SyncState,
  GovDatabase,
  GovTable,
  GovColumn,
  SchemaChange,
  QueryLogEntry,
  TopQuery,
  LineageGraph,
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
    .then((res: any) => res?.overview ?? res)
}

export function triggerSync() {
  return apiPost<SyncResult>(`${BASE}/sync`)
}

export function triggerSingleSync(type: 'metadata' | 'query_log' | 'access') {
  return apiPost<SyncResult>(`${BASE}/sync/${type}`)
}

export function fetchSyncStatus() {
  return apiGet<{ sync_states: SyncState[] }>(`${BASE}/sync/status`)
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

export function fetchTableDetail(database: string, table: string) {
  return apiGet<{
    table: GovTable
    columns: GovColumn[]
    tags: TagEntry[]
    recent_queries?: QueryLogEntry[]
    queries?: QueryLogEntry[]
    incoming: any[]
    outgoing: any[]
  }>(`${BASE}/tables/${encodeURIComponent(database)}/${encodeURIComponent(table)}`)
    .then((res: any) => ({
      ...res,
      recent_queries: res?.recent_queries ?? res?.queries ?? [],
    }))
}

export function fetchTableNotes(database: string, table: string) {
  return apiGet<{ notes: GovernanceObjectComment[] }>(`${BASE}/tables/${encodeURIComponent(database)}/${encodeURIComponent(table)}/notes`)
}

export function fetchColumnNotes(database: string, table: string, column: string) {
  return apiGet<{ notes: GovernanceObjectComment[] }>(`${BASE}/tables/${encodeURIComponent(database)}/${encodeURIComponent(table)}/columns/${encodeURIComponent(column)}/notes`)
}

export function createTableNote(database: string, table: string, commentText: string) {
  return apiPost<{ id: string }>(`${BASE}/tables/${encodeURIComponent(database)}/${encodeURIComponent(table)}/notes`, { comment_text: commentText })
}

export function createColumnNote(database: string, table: string, column: string, commentText: string) {
  return apiPost<{ id: string }>(`${BASE}/tables/${encodeURIComponent(database)}/${encodeURIComponent(table)}/columns/${encodeURIComponent(column)}/notes`, { comment_text: commentText })
}

export function deleteObjectNote(id: string) {
  return apiDel(`${BASE}/notes/${encodeURIComponent(id)}`)
}

export function fetchSchemaChanges(limit = 50) {
  return apiGet<{ changes: SchemaChange[] }>(`${BASE}/schema-changes?limit=${limit}`)
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

export function fetchTopQueries(limit = 20) {
  return apiGet<{ queries?: TopQuery[]; top_queries?: any[] }>(`${BASE}/query-log/top?limit=${limit}`)
    .then((res: any) => {
      const normalized = (res?.queries ?? res?.top_queries ?? []).map((q: any) => ({
        normalized_hash: q?.normalized_hash ?? '',
        count: Number(q?.count ?? q?.execution_count ?? 0),
        avg_duration_ms: Number(q?.avg_duration_ms ?? q?.avg_duration ?? 0),
        total_read_rows: Number(q?.total_read_rows ?? 0),
        sample_query: q?.sample_query ?? q?.normalized_query ?? '',
        last_seen: q?.last_seen ?? '',
      })) as TopQuery[]
      return { queries: normalized }
    })
}

// ── Lineage ─────────────────────────────────────────────────────

export function fetchLineage(database: string, table: string) {
  return apiGet<{ graph?: LineageGraph } | LineageGraph>(`${BASE}/lineage?database=${encodeURIComponent(database)}&table=${encodeURIComponent(table)}`)
    .then((res: any) => res?.graph ?? res)
}

export function fetchLineageGraph() {
  return apiGet<{ graph?: LineageGraph } | LineageGraph>(`${BASE}/lineage/graph`)
    .then((res: any) => res?.graph ?? res)
}

// ── Tags ────────────────────────────────────────────────────────

export function fetchTags(params?: { database?: string; table?: string }) {
  const qs = new URLSearchParams()
  if (params?.database) qs.set('database', params.database)
  if (params?.table) qs.set('table', params.table)
  const q = qs.toString()
  return apiGet<{ tags: TagEntry[] }>(`${BASE}/tags${q ? '?' + q : ''}`)
}

export function createTag(data: { object_type: string; database_name: string; table_name: string; column_name?: string; tag: string }) {
  return apiPost<{ id: string }>(`${BASE}/tags`, data)
}

export function deleteTag(id: string) {
  return apiDel(`${BASE}/tags/${id}`)
}

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

export function getPolicy(id: string) {
  return apiGet<Policy>(`${BASE}/policies/${id}`)
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
