// SPDX-License-Identifier: BUSL-1.1
/** ClickHouse access control editor (Governance > Access control). Mirrors internal/rbac. */

export type RbacOpKind =
  | 'create_role'
  | 'drop_role'
  | 'set_role_profile'
  | 'grant_privilege'
  | 'revoke_privilege'
  | 'grant_role'
  | 'revoke_role'
  | 'set_default_roles'
  | 'create_row_policy'
  | 'alter_row_policy'
  | 'drop_row_policy'
  | 'create_quota'
  | 'alter_quota'
  | 'drop_quota'
  | 'create_settings_profile'
  | 'alter_settings_profile'
  | 'drop_settings_profile'

export type RbacEntityKind = 'role' | 'user' | 'row_policy' | 'quota' | 'settings_profile'

export interface RbacTarget {
  type: 'role' | 'user'
  name: string
}

export interface RbacAssignees {
  all?: boolean
  names?: string[]
  except?: string[]
}

export interface RbacRowPolicySpec {
  name: string
  database: string
  table: string
  using: string
  restrictive?: boolean
  to: RbacAssignees
}

export interface RbacQuotaInterval {
  duration: number
  unit: string
  randomized?: boolean
  limits: Record<string, number>
}

export interface RbacQuotaSpec {
  name: string
  keyed_by?: string
  intervals: RbacQuotaInterval[]
  to: RbacAssignees
}

export interface RbacProfileSetting {
  name: string
  value?: string | null
  min?: string | null
  max?: string | null
  writability?: string
}

export interface RbacProfileSpec {
  name: string
  settings: RbacProfileSetting[]
  inherit?: string[]
  to: RbacAssignees
}

export interface RbacOp {
  kind: RbacOpKind
  name?: string
  profile?: string
  target?: RbacTarget
  access?: string[]
  database?: string
  table?: string
  columns?: string[]
  grant_option?: boolean
  roles?: string[]
  admin_option?: boolean
  default_all?: boolean
  default_except?: string[]
  row_policy?: RbacRowPolicySpec
  quota?: RbacQuotaSpec
  settings_profile?: RbacProfileSpec
}

export interface RbacEntity {
  name: string
  storage: string
}

export interface RbacUser {
  name: string
  storage: string
  default_roles_all: boolean
  default_roles: string[]
  default_roles_except: string[]
}

export interface RbacRoleGrant {
  user_name?: string
  role_name?: string
  granted_role: string
  is_default: boolean
  admin_option: boolean
}

export interface RbacGrantRow {
  user_name?: string
  role_name?: string
  access: string
  database?: string
  table?: string
  column?: string
  partial_revoke?: boolean
  grant_option?: boolean
}

export interface RbacRowPolicy extends RbacRowPolicySpec {
  full_name: string
  storage: string
}

export interface RbacQuota extends RbacQuotaSpec {
  storage: string
}

export interface RbacSettingsProfile extends RbacProfileSpec {
  storage: string
}

export interface RbacCapabilities {
  user: string
  roles: string[] | null
  access_management: boolean
  manage_roles: boolean
  manage_row_policies: boolean
  manage_quotas: boolean
  manage_settings_profiles: boolean
  role_admin: boolean
  show_access: boolean
  cluster: boolean
  grant_option: boolean
  grants: string[] | null
}

export interface RbacPrivilege {
  name: string
  level: string
}

export interface RbacSnapshot {
  roles: RbacEntity[] | null
  users: RbacUser[] | null
  role_grants: RbacRoleGrant[] | null
  grants: RbacGrantRow[] | null
  row_policies: RbacRowPolicy[] | null
  quotas: RbacQuota[] | null
  settings_profiles: RbacSettingsProfile[] | null
  role_profiles: Record<string, string[]> | null
  capabilities: RbacCapabilities
  privileges: RbacPrivilege[] | null
  errors?: string[]
}

export interface RbacStatement {
  seq: number
  sql: string
  op_index: number
  inverse_sql: string
}

export interface RbacDiff {
  kind: RbacEntityKind
  entity: string
  current: string
  proposed: string
}

export interface RbacWarning {
  code: string
  severity: 'block' | 'ack'
  message: string
}

export interface RbacPlan {
  statements: RbacStatement[]
  diffs: RbacDiff[]
  warnings: RbacWarning[]
  plan_hash: string
  cluster?: string
}

export interface RbacHostResult {
  host: string
  status: 'applied' | 'failed' | 'pending'
  error?: string
}

export interface RbacResult {
  seq: number
  sql: string
  status: 'applied' | 'failed' | 'skipped'
  error?: string
  code?: string
  hosts?: RbacHostResult[]
}

export interface RbacApplyResponse {
  results: RbacResult[]
  rollback_ops: RbacOp[]
  failed: number
  plan_hash: string
}

export interface RbacEntityText {
  create: string
  grants: string
}

export const QUOTA_RESOURCES = [
  'queries', 'query_selects', 'query_inserts', 'errors', 'result_rows', 'result_bytes',
  'read_rows', 'read_bytes', 'execution_time', 'written_bytes', 'failed_sequential_authentications',
] as const

export const QUOTA_UNITS = ['second', 'minute', 'hour', 'day', 'week', 'month', 'quarter', 'year'] as const

export const QUOTA_KEYS: Array<{ value: string; label: string }> = [
  { value: '', label: 'Not keyed (shared)' },
  { value: 'user_name', label: 'Per user' },
  { value: 'ip_address', label: 'Per IP address' },
  { value: 'forwarded_ip_address', label: 'Per forwarded IP' },
  { value: 'client_key', label: 'Per client key' },
  { value: 'client_key,user_name', label: 'Client key, else user' },
  { value: 'client_key,ip_address', label: 'Client key, else IP' },
]

export const WRITABILITIES: Array<{ value: string; label: string }> = [
  { value: '', label: 'Default' },
  { value: 'WRITABLE', label: 'Writable' },
  { value: 'CONST', label: 'Const' },
  { value: 'CHANGEABLE_IN_READONLY', label: 'Changeable in readonly' },
]

// ── Effective access explainer (Governance > Effective access) ─────────

export type RbacVerdictStatus = 'allowed' | 'partial' | 'denied' | 'blocked' | 'conditional' | 'unknown'
export type RbacEffect = 'allows' | 'removes' | 'filters' | 'limits' | 'blocks'

export interface RbacExplainQuery {
  user: string
  database: string
  table: string
  column: string
  privilege: string
}

export interface RbacVerdict {
  status: RbacVerdictStatus
  summary: string
  only?: string[]
  except?: string[]
  grant_option: boolean
  row_filtered: boolean
  roles: string[]
  via?: string[]
}

export interface RbacEvidence {
  step: number
  mode: 'both' | 'all_roles'
  source: string
  statement: string
  effect: RbacEffect
  detail?: string
  grant_option?: boolean
  local?: boolean
}

export interface RbacNode {
  host: string
  multi_node: boolean
  local_entities: string[]
}

export interface RbacRowPolicyOutcome {
  table_has_policies: boolean
  permissive: string[]
  restrictive: string[]
  filter?: string
  no_permissive: boolean
  if_setting_on?: string
  if_setting_off?: string
  filters: boolean
  setting_dependent: boolean
}

export interface RbacProfileElementResult {
  setting: string
  value?: string
  min?: string
  max?: string
  writability?: string
  source: string
  local?: boolean
}

export interface RbacQuotaResult {
  name: string
  keyed_by: string
  key_note: string
  storage: string
  local?: boolean
  intervals: Array<{ interval: string; limits: Record<string, number>; used?: Record<string, number>; end_time?: string }>
  usage_visible: boolean
}

export interface RbacExplanation {
  query: RbacExplainQuery
  node: RbacNode
  storage: string
  verdict: { default: RbacVerdict; all_roles: RbacVerdict }
  evidence: RbacEvidence[]
  row_policies: { applicable: boolean; note?: string; default: RbacRowPolicyOutcome; all_roles: RbacRowPolicyOutcome }
  profile: { elements: RbacProfileElementResult[]; readonly: string; allow_ddl: boolean; const: string[]; verdicts: string[]; blocks?: string }
  quotas: RbacQuotaResult[]
  verification: { status: 'match' | 'mismatch' | 'unavailable'; statement?: string; clickhouse?: RbacVerdict; detail?: string }
  warnings: string[]
  incomplete: boolean
  incomplete_reasons: string[] | null
}

export interface RbacWhoCanUser {
  user: string
  storage: string
  default: RbacVerdict
  all_roles: RbacVerdict
  via: string[]
  row_filtered: boolean
  columns: string[]
  local?: boolean
}

export interface RbacWhoCanResult {
  query: RbacExplainQuery
  node: RbacNode
  users: RbacWhoCanUser[]
  incomplete: boolean
  incomplete_reasons: string[] | null
}

// ── Access history ───────────────────────────────────────────────────

export type RbacHistorySource = 'apply' | 'revert' | 'sync' | 'view' | 'manual'
export type RbacHistoryDrift = 'none' | 'outside' | 'mixed'
export type RbacChangeKind = 'grant' | 'role_grant' | 'default_roles' | 'entity' | 'setting'
export type RbacChangeType = 'added' | 'removed' | 'changed'

export interface RbacHistorySummary {
  added?: Partial<Record<RbacChangeKind, number>>
  removed?: Partial<Record<RbacChangeKind, number>>
  changed?: Partial<Record<RbacChangeKind, number>>
  entities?: string[]
  truncated?: boolean
  unexplained?: string[]
  local?: string[]
  host_changed?: boolean
  previous_host?: string
  revert_of?: string
  first?: boolean
  note?: string
  incomplete?: string[]
}

export interface RbacHistoryRow {
  id: string
  connection_id: string
  hash: string
  first_seen_at: string
  last_seen_at: string
  seen_count: number
  source: RbacHistorySource
  actor: string
  ch_user: string
  host: string
  multi_node: boolean
  plan_hash: string
  complete: boolean
  drift: RbacHistoryDrift
  summary: RbacHistorySummary
}

export interface RbacHistoryPage {
  snapshots: RbacHistoryRow[]
  next_cursor: string
  outside_last_30d: number
}

export interface RbacStateChange {
  kind: RbacChangeKind
  change: RbacChangeType
  entity_kind: RbacEntityKind
  grantee: string
  detail: string
}

export interface RbacEffectiveChange {
  user: string
  change: 'gained' | 'lost'
  what: string
  via: string
}

export interface RbacStateDiff {
  changes: RbacStateChange[]
  entities: RbacDiff[]
  effective: RbacEffectiveChange[]
}

export interface RbacHistorySide {
  id?: string
  live?: boolean
  at?: string
  label: string
}

export interface RbacHistoryDiff {
  from: RbacHistorySide
  to: RbacHistorySide
  diff: RbacStateDiff
}

export interface RbacUnrevertable {
  kind: string
  name: string
  reason: string
}

export interface RbacRevertPlan {
  snapshot_id: string
  ops: RbacOp[]
  unrevertable: RbacUnrevertable[]
  over_cap: boolean
  max_ops: number
  entities: Array<{ name: string; ops: number }>
}

// ── Access as code ─────────────────────────────────────────────────

/** Parts of an access file an import can apply. Users means default roles. */
export type RbacImportKind = 'roles' | 'users' | 'grants' | 'role_grants' | 'row_policies' | 'quotas' | 'profiles'

export type RbacPruneKind = Exclude<RbacImportKind, 'users'>

export type RbacPrune = Partial<Record<RbacPruneKind, boolean>>

export interface RbacRename {
  principals?: Record<string, string>
  databases?: Record<string, string>
}

export interface RbacImportRequest {
  file: string
  prune: RbacPrune
  rename: RbacRename
  kinds?: RbacImportKind[]
}

export interface RbacSkip {
  kind: string
  name: string
  reason: string
}

export interface RbacFileSource {
  connection?: string
  host?: string
  exported_at?: string
}

export interface RbacImportPlan {
  ops: RbacOp[]
  skipped: RbacSkip[]
  summary: Partial<Record<RbacImportKind, number>>
  over_cap: boolean
  message?: string
  source?: RbacFileSource
}

export type RbacExportFormat = 'yaml' | 'json' | 'sql'
