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
