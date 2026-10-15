// SPDX-License-Identifier: BUSL-1.1
import { describe, expect, it } from 'vitest'
import { lineDiff } from '../../../utils/lineDiff'
import { ackKey, allAcknowledged, assigneesLabel, describeOp, dropConfirmName, grantObject, revokeOpFor, splitList } from './changes'
import { effectBadge, explainPath, objectName, queryFromSearch, searchWithQuery, verdictLabel, verdictTone } from './explain'

describe('lineDiff', () => {
  it('marks unchanged, changed, added and removed lines', () => {
    const rows = lineDiff('CREATE ROLE r\nGRANT SELECT ON d.* TO r\nGRANT X ON *.* TO r', 'CREATE ROLE r\nGRANT INSERT ON d.* TO r\nGRANT X ON *.* TO r\nGRANT Y ON *.* TO r')
    expect(rows.map((r) => r.kind)).toEqual(['same', 'changed', 'same', 'added'])
    expect(rows[1]).toEqual({ kind: 'changed', left: 'GRANT SELECT ON d.* TO r', right: 'GRANT INSERT ON d.* TO r' })
  })

  it('handles creation and removal of a whole entity', () => {
    expect(lineDiff('', 'CREATE ROLE r').map((r) => r.kind)).toEqual(['added'])
    expect(lineDiff('CREATE ROLE r\nGRANT A', '').map((r) => r.kind)).toEqual(['removed', 'removed'])
    expect(lineDiff('', '')).toEqual([])
  })
})

describe('change helpers', () => {
  it('describes ops in plain words', () => {
    expect(describeOp({ kind: 'grant_privilege', target: { type: 'role', name: 'r' }, access: ['SELECT'], database: 'd', table: 't', columns: ['a'], grant_option: true }))
      .toBe('Grant SELECT(a) on d.t to role r with grant option')
    expect(describeOp({ kind: 'set_default_roles', target: { type: 'user', name: 'u' } })).toBe('Set default roles of user u to NONE')
    expect(describeOp({ kind: 'set_default_roles', target: { type: 'user', name: 'u' }, default_all: true, default_except: ['a', 'b'] })).toBe('Set default roles of user u to ALL EXCEPT a, b')
    expect(describeOp({ kind: 'drop_row_policy', row_policy: { name: 'p', database: 'd', table: 't', using: '', to: {} } })).toBe('Drop row policy p on d.t')
  })

  it('names what a drop must be confirmed with', () => {
    expect(dropConfirmName({ kind: 'drop_role', name: 'r' })).toBe('r')
    expect(dropConfirmName({ kind: 'drop_row_policy', row_policy: { name: 'p', database: 'd', table: 't', using: '', to: {} } })).toBe('p')
    expect(dropConfirmName({ kind: 'create_role', name: 'r' })).toBeNull()
  })

  it('formats objects, assignees and lists', () => {
    expect(grantObject()).toBe('*.*')
    expect(grantObject('d')).toBe('d.*')
    expect(grantObject('d', 't')).toBe('d.t')
    expect(assigneesLabel({ all: true, except: ['a'] })).toBe('all except a')
    expect(assigneesLabel({ names: [] })).toBe('none')
    expect(splitList(' a, b\n,c ')).toEqual(['a', 'b', 'c'])
  })

  it('turns a grant row into its revoke, and a partial revoke into a grant', () => {
    const t = { type: 'role' as const, name: 'r' }
    expect(revokeOpFor(t, { role_name: 'r', access: 'SELECT', database: 'd', column: 'c' })).toEqual({
      kind: 'revoke_privilege', target: t, access: ['SELECT'], database: 'd', table: '', columns: ['c'],
    })
    expect(revokeOpFor(t, { role_name: 'r', access: 'SELECT', partial_revoke: true }).kind).toBe('grant_privilege')
  })
})

describe('effective access helpers', () => {
  it('round-trips the question through the URL and keeps the section', () => {
    const q = queryFromSearch('?section=effective-access&user=ana&database=d&table=t')
    expect(q).toEqual({ user: 'ana', database: 'd', table: 't', column: '', privilege: 'SELECT' })
    expect(searchWithQuery('section=effective-access&user=old', { ...q, user: '', privilege: 'INSERT' }))
      .toBe('section=effective-access&database=d&table=t&privilege=INSERT')
    // A table without a database is dropped; the default privilege is implied.
    expect(searchWithQuery('', { user: 'u', database: '', table: 't', column: 'c', privilege: 'SELECT' })).toBe('user=u')
  })

  it('links a user into the section', () => {
    expect(explainPath('we ird')).toBe('/governance?section=effective-access&user=we+ird')
  })

  it('labels verdicts and effects', () => {
    expect(verdictLabel('denied')).toBe('No access')
    expect(verdictTone('allowed')).toBe('success')
    expect(verdictTone('unknown')).toBe('neutral')
    expect(effectBadge('filters')).toEqual({ label: 'Filters rows', tone: 'info' })
    expect(objectName({ database: 'd', table: 't', column: 'c' })).toBe('d.t(c)')
    expect(objectName({ database: '', table: '', column: '' })).toBe('*.*')
  })
})

describe('acknowledgements', () => {
  const drop = (name: string) => ({ code: 'drop_user', severity: 'ack', message: `Dropping user "${name}" cannot be undone.` })

  it('needs one tick per warning, even when two share a code', () => {
    const warnings = [drop('ann'), drop('bob'), { code: 'self_revoke', severity: 'block', message: 'x' }]
    expect(ackKey(warnings[0])).not.toBe(ackKey(warnings[1]))
    expect(allAcknowledged(warnings, {})).toBe(false)
    expect(allAcknowledged(warnings, { [ackKey(warnings[0])]: true })).toBe(false)
    expect(allAcknowledged(warnings, { [ackKey(warnings[0])]: true, [ackKey(warnings[1])]: true })).toBe(true)
    expect(allAcknowledged([], {})).toBe(true)
  })
})
