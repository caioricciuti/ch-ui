// SPDX-License-Identifier: BUSL-1.1
import { describe, expect, it } from 'vitest'
import type { RbacImportKind, RbacPruneKind } from '../../../types/rbac'
import { buildImportRequest, fileProblem, importPlanFrom, renameMap, summaryEntries } from './importFile'

const allKinds = (): Record<RbacImportKind, boolean> => ({ roles: true, users: true, grants: true, role_grants: true, row_policies: true, quotas: true, profiles: true })
const noPrune = (): Record<RbacPruneKind, boolean> => ({ roles: false, grants: false, role_grants: false, row_policies: false, quotas: false, profiles: false })

describe('renameMap', () => {
  it('maps filled rows, trims, and ignores blank and identity rows', () => {
    expect(renameMap([{ from: ' analyst ', to: 'stg_analyst' }, { from: '', to: '' }, { from: 'same', to: 'same' }], 'database')).toEqual({
      map: { analyst: 'stg_analyst' },
      error: '',
    })
  })

  it('refuses half-filled rows and a name renamed twice', () => {
    expect(renameMap([{ from: 'a', to: '' }], 'database').error).toMatch(/Fill in both names of every database rename/)
    expect(renameMap([{ from: '', to: 'b' }], 'database').error).toMatch(/Fill in both/)
    expect(renameMap([{ from: 'a', to: 'b' }, { from: 'a', to: 'c' }], 'database').error).toBe('Database a is renamed twice.')
  })
})

describe('buildImportRequest', () => {
  it('sends everything by default, with no prune and no kinds filter', () => {
    const { request, error } = buildImportRequest('chui_access: 1\n', allKinds(), noPrune(), [], [])
    expect(error).toBe('')
    expect(request).toEqual({ file: 'chui_access: 1\n', prune: {}, rename: {} })
  })

  it('limits kinds, keeps prune only for imported kinds, and carries renames', () => {
    const include = { ...allKinds(), quotas: false, profiles: false }
    const prune = { ...noPrune(), roles: true, quotas: true }
    const { request } = buildImportRequest('x', include, prune, [{ from: 'a', to: 'b' }], [{ from: 'prod', to: 'stg' }])
    expect(request?.kinds).toEqual(['roles', 'users', 'grants', 'role_grants', 'row_policies'])
    expect(request?.prune).toEqual({ roles: true })
    expect(request?.rename).toEqual({ principals: { a: 'b' }, databases: { prod: 'stg' } })
  })

  it('names the first problem', () => {
    expect(buildImportRequest('  ', allKinds(), noPrune(), [], []).error).toMatch(/Choose a file/)
    const none = Object.fromEntries(Object.keys(allKinds()).map((k) => [k, false])) as Record<RbacImportKind, boolean>
    expect(buildImportRequest('x', none, noPrune(), [], []).error).toMatch(/at least one part/)
    expect(buildImportRequest('x', allKinds(), noPrune(), [{ from: 'a', to: '' }], []).error).toMatch(/user or role rename/)
  })
})

describe('importPlanFrom', () => {
  it('turns a response into the ops to stage', () => {
    const plan = importPlanFrom({
      success: true,
      ops: [
        { kind: 'create_role', name: 'r' },
        { kind: 'grant_privilege', target: { type: 'role', name: 'r' }, access: ['SELECT'], database: 'd' },
      ],
      skipped: [{ kind: 'user', name: 'ghost', reason: 'user not found' }, 'junk'],
      summary: { roles: 1, grants: 1, quotas: 0, bogus: 3 },
      over_cap: false,
      source: { connection: 'prod', host: 7 },
    })
    expect(plan.ops.map((o) => o.kind)).toEqual(['create_role', 'grant_privilege'])
    expect(plan.ops[1].target).toEqual({ type: 'role', name: 'r' })
    expect(plan.skipped).toEqual([{ kind: 'user', name: 'ghost', reason: 'user not found' }])
    expect(plan.summary).toEqual({ roles: 1, grants: 1 })
    expect(plan.over_cap).toBe(false)
    expect(plan.source).toEqual({ connection: 'prod', host: undefined, exported_at: undefined })
    expect(summaryEntries(plan.summary)).toEqual([
      { kind: 'roles', label: 'Roles', count: 1 },
      { kind: 'grants', label: 'Privileges', count: 1 },
    ])
  })

  it('keeps an over-cap message and an empty plan', () => {
    const plan = importPlanFrom({ ops: [], skipped: null, over_cap: true, message: 'too many' })
    expect(plan.ops).toEqual([])
    expect(plan.skipped).toEqual([])
    expect(plan.over_cap).toBe(true)
    expect(plan.message).toBe('too many')
  })

  it('refuses anything that is not a list of known ops', () => {
    expect(() => importPlanFrom(null)).toThrow(/no changes/)
    expect(() => importPlanFrom({ ops: 'DROP ROLE x' })).toThrow(/no changes/)
    expect(() => importPlanFrom({ ops: [{ kind: 'raw_sql', sql: 'DROP TABLE t' }] })).toThrow(/does not know/)
    expect(() => importPlanFrom({ ops: [null] })).toThrow(/does not know/)
    // Import never drops users.
    expect(() => importPlanFrom({ ops: [{ kind: 'drop_user', name: 'x' }] })).toThrow(/does not know/)
  })

  it('reads the new users and asks for the ticked ones', () => {
    const plan = importPlanFrom({
      ops: [{ kind: 'create_user', name: 'ann' }],
      new_users: [{ name: 'ann', auth: 'ldap', hosts: [{ kind: 'local' }, { kind: 'bogus' }] }, { name: '' }, 'x'],
    })
    expect(plan.new_users).toEqual([{ name: 'ann', auth: 'ldap', hosts: [{ kind: 'local' }] }])
    const built = buildImportRequest('chui_access: 1', allKinds(), noPrune(), [], [], ['ann'])
    expect(built.request?.create_users).toEqual(['ann'])
    expect(buildImportRequest('chui_access: 1', allKinds(), noPrune(), [], []).request?.create_users).toBeUndefined()
  })
})

describe('fileProblem', () => {
  it('extracts the path message of an invalid file', () => {
    expect(fileProblem('The file is not valid: roles.a.x: unknown key (line 3)')).toBe('roles.a.x: unknown key (line 3)')
    expect(fileProblem('The connection is offline')).toBe('')
  })
})
