/** Standard API response envelope */
export interface ApiResponse<T = unknown> {
  success: boolean
  error?: string
  data?: T
}

/** Session info returned by /api/auth/session */
export interface Session {
  user: string
  role: string
  connectionId: string
  connectionName: string
  connectionOnline: boolean
  expiresAt: string
  version?: string
  appVersion?: string
}

/** Connection info */
export interface Connection {
  id: string
  name: string
  status: string
  online: boolean
  created_at: string
  host_info?: HostInfo
}

/** Host machine metrics from agent */
export interface HostInfo {
  hostname: string
  os: string
  arch: string
  cpu_cores: number
  memory_total: number
  memory_free: number
  disk_total: number
  disk_free: number
  go_version: string
  agent_uptime: number
  collected_at: string
}

/** License info returned by the server */
export interface LicenseInfo {
  edition: string
  valid: boolean
  customer?: string
  expires_at?: string
  license_id?: string
}

/** Saved query */
export interface SavedQuery {
  id: string
  name: string
  query: string
  description?: string
  created_at: string
  updated_at: string
}

/** Dashboard */
export interface Dashboard {
  id: string
  name: string
  description: string | null
  created_by: string
  created_at: string
  updated_at: string
}

/** Dashboard panel */
export interface Panel {
  id: string
  dashboard_id: string
  name: string
  panel_type: string
  query: string
  connection_id: string | null
  config: string
  layout_x: number
  layout_y: number
  layout_w: number
  layout_h: number
  created_at: string
  updated_at: string
}

/** Scheduled query */
export interface Schedule {
  id: string
  name: string
  saved_query_id: string
  connection_id: string | null
  cron: string
  timezone: string
  enabled: boolean
  timeout_ms: number
  last_run_at: string | null
  next_run_at: string | null
  last_status: string | null
  last_error: string | null
  created_by: string
  created_at: string
  updated_at: string
}

/** Schedule execution run */
export interface ScheduleRun {
  id: string
  schedule_id: string
  started_at: string
  finished_at: string | null
  status: string
  rows_affected: number
  elapsed_ms: number
  error: string | null
}

/** Panel visualization config (stored as JSON in panel.config) */
export interface PanelConfig {
  chartType: 'table' | 'stat' | 'timeseries' | 'bar'
  xColumn?: string
  yColumns?: string[]
  colors?: string[]
  legendPosition?: 'bottom' | 'right' | 'none'
}

/** Audit log entry */
export interface AuditLog {
  id: string
  action: string
  username: string | null
  details: string | null
  ip_address: string | null
  created_at: string
  parsed_details?: Record<string, unknown>
}

/** Admin stats overview */
export interface AdminStats {
  users_count: number
  connections: number
  online: number
  login_count: number
  query_count: number
}
