// SPDX-License-Identifier: BUSL-1.1
import type { RbacImportKind, RbacImportPlan, RbacImportRequest, RbacNewUser, RbacOp, RbacOpKind, RbacPrune, RbacPruneKind, RbacSkip } from '../../../types/rbac'
import { hostsFrom } from './newUsers'

/** The parts of an access file, in file order, and whether they can prune. */
export const IMPORT_KINDS: { kind: RbacImportKind; label: string; prunable: boolean }[] = [
  { kind: 'roles', label: 'Roles', prunable: true },
  { kind: 'users', label: 'Default roles of users', prunable: false },
  { kind: 'grants', label: 'Privileges', prunable: true },
  { kind: 'role_grants', label: 'Role grants', prunable: true },
  { kind: 'row_policies', label: 'Row policies', prunable: true },
  { kind: 'quotas', label: 'Quotas', prunable: true },
  { kind: 'profiles', label: 'Settings profiles', prunable: true },
]

export interface RenameRow {
  from: string
  to: string
}

/**
 * Turns the rows of a rename table into a map. Blank rows and rows that
 * rename a name to itself are ignored; a half-filled row or a name renamed
 * twice is an error.
 */
export function renameMap(rows: RenameRow[], what: string): { map: Record<string, string>; error: string } {
  const map: Record<string, string> = {}
  for (const row of rows) {
    const from = row.from.trim()
    const to = row.to.trim()
    if (!from && !to) continue
    if (!from || !to) return { map: {}, error: `Fill in both names of every ${what} rename, or remove the row.` }
    if (from in map) return { map: {}, error: `${what[0].toUpperCase()}${what.slice(1)} ${from} is renamed twice.` }
    if (from !== to) map[from] = to
  }
  return { map, error: '' }
}

/** The import/plan request, or the first problem with the form. */
export function buildImportRequest(
  file: string,
  include: Record<RbacImportKind, boolean>,
  prune: Record<RbacPruneKind, boolean>,
  principals: RenameRow[],
  databases: RenameRow[],
  createUsers: string[] = [],
): { request: RbacImportRequest | null; error: string } {
  if (!file.trim()) return { request: null, error: 'Choose a file or paste its contents.' }
  const kinds = IMPORT_KINDS.map((k) => k.kind).filter((k) => include[k])
  if (kinds.length === 0) return { request: null, error: 'Pick at least one part of the file to import.' }
  const p = renameMap(principals, 'user or role')
  if (p.error) return { request: null, error: p.error }
  const d = renameMap(databases, 'database')
  if (d.error) return { request: null, error: d.error }
  const prunes: RbacPrune = {}
  for (const k of IMPORT_KINDS) {
    if (k.prunable && include[k.kind] && prune[k.kind as RbacPruneKind]) prunes[k.kind as RbacPruneKind] = true
  }
  const request: RbacImportRequest = { file, prune: prunes, rename: {} }
  if (Object.keys(p.map).length) request.rename.principals = p.map
  if (Object.keys(d.map).length) request.rename.databases = d.map
  if (kinds.length < IMPORT_KINDS.length) request.kinds = kinds
  if (createUsers.length) request.create_users = [...createUsers]
  return { request, error: '' }
}

const OP_KINDS: ReadonlySet<string> = new Set<RbacOpKind>([
  'create_role', 'drop_role', 'set_role_profile', 'grant_privilege', 'revoke_privilege', 'grant_role', 'revoke_role',
  'set_default_roles', 'create_row_policy', 'alter_row_policy', 'drop_row_policy', 'create_quota', 'alter_quota',
  'drop_quota', 'create_settings_profile', 'alter_settings_profile', 'drop_settings_profile', 'create_user',
])

function isRecord(v: unknown): v is Record<string, unknown> {
  return typeof v === 'object' && v !== null && !Array.isArray(v)
}

/**
 * Reads an import/plan response. The ops become the staged change set, so
 * anything that is not an op list with known kinds is refused rather than
 * staged.
 */
export function importPlanFrom(body: unknown): RbacImportPlan {
  if (!isRecord(body) || !Array.isArray(body.ops)) throw new Error('The server sent no changes for this file.')
  const ops: RbacOp[] = []
  for (const op of body.ops) {
    if (!isRecord(op) || typeof op.kind !== 'string' || !OP_KINDS.has(op.kind)) throw new Error('The server sent a change this page does not know.')
    ops.push(op as unknown as RbacOp)
  }
  const skipped: RbacSkip[] = Array.isArray(body.skipped)
    ? body.skipped.filter(isRecord).map((s) => ({ kind: String(s.kind ?? ''), name: String(s.name ?? ''), reason: String(s.reason ?? '') }))
    : []
  const summary: RbacImportPlan['summary'] = {}
  if (isRecord(body.summary)) {
    for (const k of IMPORT_KINDS) {
      const n = body.summary[k.kind]
      if (typeof n === 'number' && n > 0) summary[k.kind] = n
    }
  }
  const source = isRecord(body.source)
    ? {
        connection: typeof body.source.connection === 'string' ? body.source.connection : undefined,
        host: typeof body.source.host === 'string' ? body.source.host : undefined,
        exported_at: typeof body.source.exported_at === 'string' ? body.source.exported_at : undefined,
      }
    : undefined
  const newUsers: RbacNewUser[] = Array.isArray(body.new_users)
    ? body.new_users
        .filter(isRecord)
        .filter((u) => typeof u.name === 'string' && u.name !== '')
        .map((u) => ({ name: String(u.name), auth: typeof u.auth === 'string' ? u.auth : undefined, hosts: hostsFrom(u.hosts) }))
    : []
  return {
    ops,
    skipped,
    summary,
    over_cap: body.over_cap === true,
    message: typeof body.message === 'string' ? body.message : undefined,
    source,
    new_users: newUsers,
  }
}

/** The users an import plan creates. */
export function newUserCount(plan: Pick<RbacImportPlan, 'ops'>): number {
  return plan.ops.filter((op) => op.kind === 'create_user').length
}

/** Non-zero change counts per part, in file order. */
export function summaryEntries(summary: RbacImportPlan['summary']): { kind: RbacImportKind; label: string; count: number }[] {
  return IMPORT_KINDS.filter((k) => (summary[k.kind] ?? 0) > 0).map((k) => ({ kind: k.kind, label: k.label, count: summary[k.kind] ?? 0 }))
}

/** A server message about the file itself ("The file is not valid: roles.a.x: unknown key"), or ''. */
export function fileProblem(message: string): string {
  const prefix = 'The file is not valid: '
  return message.startsWith(prefix) ? message.slice(prefix.length) : ''
}
