/** Session info returned by /api/auth/session */
export interface Session {
  user: string
  role: string
  /** Explanation when the effective role is surprising (implicit admin ignored, degraded detection) */
  roleNote?: string
  /** Signed in through SSO; user is the person's email. */
  viaSSO?: boolean
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
  /** direct: CH-UI connects itself; agent: a ch-ui connect agent does. */
  kind?: 'direct' | 'agent'
  /** Only for SSO sessions: the connection has an SSO service account. */
  sso_ready?: boolean
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
/** Who can see a saved query: the owner only, everyone, or admins and the owner. */
export type SavedQueryVisibility = 'private' | 'shared' | 'admins'

export interface SavedQuery {
  id: string
  name: string
  query: string
  description?: string | null
  parameters?: string | null // JSON object of default {name: value} bind params
  connection_id?: string | null
  /** Reviewed by a human; AI clients prefer verified queries. */
  verified?: boolean
  folder_id: string | null
  tags: string[]
  visibility: SavedQueryVisibility
  /** Creator; "" for queries saved before owners were recorded. */
  owner: string
  /** Whether the current user may edit, move, delete or restore it. */
  can_edit: boolean
  created_at: string
  updated_at: string
}

/** Saved query folders share the dashboard folder shape. */
export type SavedQueryFolder = DashboardFolder

/** The content of a saved query at one point in its history. */
export interface SavedQuerySnapshot {
  name: string
  description: string
  query: string
  parameters: string
  connection_id: string
  verified: boolean
}

export interface SavedQueryVersion {
  id: string
  saved_query_id: string
  version: number
  /** Only present when one version is fetched. */
  snapshot?: SavedQuerySnapshot
  created_by: string | null
  created_at: string
  note: string
}

/** Dashboard */
export interface Dashboard {
  id: string
  name: string
  description: string | null
  /** Folder the dashboard lives in; null is the root. */
  folder_id: string | null
  tags: string[]
  /** Starred by the current user. */
  starred: boolean
  created_by: string
  created_at: string
  updated_at: string
}

/** Dashboard variable types. */
export type DashboardVariableType = 'query' | 'custom' | 'text' | 'constant' | 'interval'

/**
 * Dashboard variable definition. Panels reference it as $name or ${name};
 * the server quotes every value. ${name:number} emits bare numbers.
 */
export interface DashboardVariable {
  name: string
  label?: string
  type: DashboardVariableType
  /** type query: first column is the value, optional second the label. */
  query?: string
  /** types custom and interval: comma separated list. */
  options?: string
  multi?: boolean
  include_all?: boolean
  default?: string[]
}

/** One selectable value of a variable. */
export interface DashboardVariableOption {
  value: string
  label: string
  /** The value as a UTC ISO instant, when it is a date and time. */
  instant?: string
}

/** One saved state of a dashboard (snapshot only on the single-version endpoint). */
export interface DashboardVersion {
  id: string
  dashboard_id: string
  version: number
  snapshot?: unknown
  created_by: string | null
  created_at: string
  note: string
  panel_count: number
}

/** Built-in dashboard template. */
export interface DashboardTemplate {
  id: string
  name: string
  description: string
  tags: string[]
  panel_count: number
  variables: string[]
}

/** Dashboard folder; folders nest through parent_id. */
export interface DashboardFolder {
  id: string
  name: string
  parent_id: string | null
  created_by: string | null
  created_at: string
  updated_at: string
}

/** Dashboard panel */
export interface Panel {
  id: string
  dashboard_id: string
  name: string
  description: string
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
  /** Name of the saved query, also when it is private to someone else. */
  saved_query_name?: string
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

export interface StatThreshold {
  value: number
  color: string
}

/** What a click on a data point does. */
export type PanelLinkKind = 'setVariable' | 'dashboard' | 'sql' | 'url'

/**
 * A drilldown link of a panel. Templates take ${__value}, ${__field},
 * ${__series}, ${__row.<column>}, ${<variable>}, and ${__value:raw} (sql only).
 */
export interface PanelLink {
  kind: PanelLinkKind
  label?: string
  /** setVariable: the dashboard variable that takes the clicked value. */
  variable?: string
  /** dashboard: the dashboard to open. */
  dashboardId?: string
  /** dashboard: pass the current variable values along. */
  carryVariables?: boolean
  /** dashboard: variable values for the target, as templates. */
  params?: Record<string, string>
  /** sql and url: the text to interpolate. */
  template?: string
}

/** Panel visualization config (stored as JSON in panel.config) */
export interface PanelConfig {
  chartType: 'table' | 'stat' | 'timeseries' | 'bar' | 'text' | 'gauge' | 'pie' | 'histogram' | 'scatter' | 'heatmap'
  xColumn?: string
  yColumns?: string[]
  /** Column whose values split the first y column into one series each (long to wide). */
  seriesColumn?: string
  colors?: string[]
  legendPosition?: 'bottom' | 'right' | 'none'
  content?: string
  barMode?: 'grouped' | 'stacked'
  /** Time series: a line with a light tint (the default), or a filled area. */
  fill?: 'line' | 'area'
  /** Time series: each series sits on top of the ones before it. */
  stacked?: boolean
  /** Histogram: the column whose values are bucketed; first numeric when unset. */
  histogramColumn?: string
  /** Histogram: number of equal-width buckets, 30 when unset. */
  histogramBuckets?: number
  scatterX?: string
  scatterY?: string
  /** Scatter: column that scales each point. */
  scatterSize?: string
  /** Scatter: column whose values split points into colored groups. */
  scatterSeries?: string
  heatmapX?: string
  heatmapY?: string
  /** Heatmap: column summed per cell; rows are counted when there is no numeric column. */
  heatmapValue?: string
  gaugeMin?: number
  gaugeMax?: number
  pieDonut?: boolean
  pieLabelColumn?: string
  pieValueColumn?: string
  statField?: string
  statCalculation?: 'last' | 'first' | 'mean' | 'sum' | 'min' | 'max' | 'count' | 'range'
  statUnit?: 'none' | 'percent' | 'short' | 'bytes' | 'bps' | 'duration' | 'durationMs'
  statPrefix?: string
  statSuffix?: string
  statDecimals?: number
  statColorMode?: 'none' | 'value' | 'background'
  statThresholds?: StatThreshold[]
  /** Drilldown: what a click on a data point does. At most 10. */
  links?: PanelLink[]
}

/** Audit log entry */
export interface AuditLog {
  id: string
  action: string
  username: string | null
  /** ClickHouse account the action ran as; omitted when it equals username */
  ch_user?: string | null
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
