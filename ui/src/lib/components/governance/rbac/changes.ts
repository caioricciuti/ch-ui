// SPDX-License-Identifier: BUSL-1.1
/** Pure helpers for the access control change set: labels, lookups, summaries. */
import type { RbacAssignees, RbacGrantRow, RbacOp, RbacSnapshot, RbacTarget } from '../../../types/rbac'
import { authLabel } from './newUsers'

export function grantObject(database?: string, table?: string): string {
  if (!database || database === '*') return '*.*'
  if (!table || table === '*') return `${database}.*`
  return `${database}.${table}`
}

export function assigneesLabel(a: RbacAssignees | undefined): string {
  if (!a) return 'none'
  if (a.all) return a.except && a.except.length ? `all except ${a.except.join(', ')}` : 'all'
  return a.names && a.names.length ? a.names.join(', ') : 'none'
}

/** One line describing an op, for the pending change list. */
export function describeOp(op: RbacOp): string {
  const who = op.target ? `${op.target.type} ${op.target.name}` : ''
  switch (op.kind) {
    case 'create_role':
      return `Create role ${op.name}${op.profile ? ` with profile ${op.profile}` : ''}`
    case 'drop_role':
      return `Drop role ${op.name}`
    case 'set_role_profile':
      return op.profile ? `Set profile ${op.profile} on role ${op.name}` : `Clear the profile of role ${op.name}`
    case 'grant_privilege': {
      const cols = op.columns && op.columns.length ? `(${op.columns.join(', ')})` : ''
      return `Grant ${(op.access ?? []).join(', ')}${cols} on ${grantObject(op.database, op.table)} to ${who}${op.grant_option ? ' with grant option' : ''}`
    }
    case 'revoke_privilege': {
      const cols = op.columns && op.columns.length ? `(${op.columns.join(', ')})` : ''
      return `Revoke ${op.grant_option ? 'grant option for ' : ''}${(op.access ?? []).join(', ')}${cols} on ${grantObject(op.database, op.table)} from ${who}`
    }
    case 'grant_role':
      return `Grant role ${(op.roles ?? []).join(', ')} to ${who}${op.admin_option ? ' with admin option' : ''}`
    case 'revoke_role':
      return `Revoke ${op.admin_option ? 'admin option for ' : ''}role ${(op.roles ?? []).join(', ')} from ${who}`
    case 'set_default_roles':
      return `Set default roles of ${who} to ${op.default_all ? (op.default_except?.length ? `ALL EXCEPT ${op.default_except.join(', ')}` : 'ALL') : op.roles && op.roles.length ? op.roles.join(', ') : 'NONE'}`
    case 'create_row_policy':
    case 'alter_row_policy': {
      const p = op.row_policy
      const verb = op.kind === 'create_row_policy' ? 'Create' : 'Change'
      return p ? `${verb} row policy ${p.name} on ${p.database}.${p.table}` : verb
    }
    case 'drop_row_policy':
      return op.row_policy ? `Drop row policy ${op.row_policy.name} on ${op.row_policy.database}.${op.row_policy.table}` : 'Drop row policy'
    case 'create_quota':
      return `Create quota ${op.quota?.name ?? ''}`
    case 'alter_quota':
      return `Change quota ${op.quota?.name ?? ''}`
    case 'drop_quota':
      return `Drop quota ${op.name}`
    case 'create_settings_profile':
      return `Create settings profile ${op.settings_profile?.name ?? ''}`
    case 'alter_settings_profile':
      return `Change settings profile ${op.settings_profile?.name ?? ''}`
    case 'drop_settings_profile':
      return `Drop settings profile ${op.name}`
    case 'create_user':
      return `Create user ${op.name}${op.auth ? ` (${authLabel(op.auth.method)})` : ''}${op.profile ? ` with profile ${op.profile}` : ''}`
    case 'drop_user':
      return `Drop user ${op.name}`
  }
}

/** One checkbox per ack warning: two warnings with the same code (two dropped users) are ticked separately. */
export function ackKey(w: { code: string; message: string }): string {
  return `${w.code}\u0000${w.message}`
}

/** Whether every ack warning has its own tick. */
export function allAcknowledged(warnings: { code: string; severity: string; message: string }[], acked: Record<string, boolean>): boolean {
  return warnings.filter((w) => w.severity === 'ack').every((w) => acked[ackKey(w)])
}

/** The name a person must type to confirm a drop, or null when op is not a drop. */
export function dropConfirmName(op: RbacOp): string | null {
  if (op.kind === 'drop_role' || op.kind === 'drop_user' || op.kind === 'drop_quota' || op.kind === 'drop_settings_profile') return op.name ?? ''
  if (op.kind === 'drop_row_policy') return op.row_policy?.name ?? ''
  return null
}

export function sameTarget(a: RbacTarget | undefined, b: RbacTarget): boolean {
  return !!a && a.type === b.type && a.name === b.name
}

/** Ops that touch the given role or user (for the entity sheet). */
export function opsForTarget(ops: RbacOp[], t: RbacTarget): Array<{ op: RbacOp; index: number }> {
  return ops
    .map((op, index) => ({ op, index }))
    .filter(
      ({ op }) =>
        sameTarget(op.target, t) ||
        (t.type === 'role' && (op.kind === 'create_role' || op.kind === 'drop_role' || op.kind === 'set_role_profile') && op.name === t.name) ||
        (t.type === 'user' && (op.kind === 'create_user' || op.kind === 'drop_user') && op.name === t.name),
    )
}

export function grantsOf(snap: RbacSnapshot | null, t: RbacTarget): RbacGrantRow[] {
  if (!snap) return []
  return (snap.grants ?? []).filter((g) => (t.type === 'user' ? g.user_name === t.name : g.role_name === t.name))
}

export function rolesHeldBy(snap: RbacSnapshot | null, t: RbacTarget): Array<{ role: string; admin: boolean }> {
  if (!snap) return []
  return (snap.role_grants ?? [])
    .filter((g) => (t.type === 'user' ? g.user_name === t.name : g.role_name === t.name))
    .map((g) => ({ role: g.granted_role, admin: g.admin_option }))
}

export function membersOf(snap: RbacSnapshot | null, role: string): string[] {
  if (!snap) return []
  return (snap.role_grants ?? []).filter((g) => g.granted_role === role).map((g) => g.user_name || g.role_name || '')
}

/** A grant row turned back into the revoke op that removes it. */
export function revokeOpFor(t: RbacTarget, g: RbacGrantRow): RbacOp {
  return {
    kind: g.partial_revoke ? 'grant_privilege' : 'revoke_privilege',
    target: { ...t },
    access: [g.access],
    database: g.database || '',
    table: g.table || '',
    columns: g.column ? [g.column] : [],
  }
}

/** Split a comma or newline separated list, trimmed, empties dropped. */
export function splitList(text: string): string[] {
  return text
    .split(/[,\n]/)
    .map((s) => s.trim())
    .filter(Boolean)
}

export function hasReplicatedStorage(snap: RbacSnapshot | null): boolean {
  if (!snap) return false
  const all = [
    ...(snap.roles ?? []),
    ...(snap.users ?? []),
    ...(snap.row_policies ?? []),
    ...(snap.quotas ?? []),
    ...(snap.settings_profiles ?? []),
  ]
  return all.some((e) => e.storage === 'replicated')
}
