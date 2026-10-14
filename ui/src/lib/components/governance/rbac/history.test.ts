// SPDX-License-Identifier: BUSL-1.1
import { describe, expect, it } from 'vitest'
import { changeLine, driftLabel, effectiveLine, entityKeyLabel, stageRevert, summaryText, takeStagedRevert } from './history'

describe('access history formatting', () => {
  it('summarizes counts in a fixed order', () => {
    expect(summaryText({ summary: { added: { grant: 2, entity: 1 }, removed: { role_grant: 1 } } }))
      .toBe('1 entity added, 2 grants added, 1 role grant removed')
    expect(summaryText({ summary: { changed: { default_roles: 1, setting: 2 } } })).toBe('1 default roles change, 2 setting changes')
    expect(summaryText({ summary: { first: true } })).toBe('First snapshot')
    expect(summaryText({ summary: {} })).toBe('No change')
    expect(summaryText({ summary: { changed: { entity: 1 }, host_changed: true, incomplete: ['grants: x'] } }))
      .toBe('1 entity changed (host changed) (partial read)')
  })

  it('writes change and effective lines', () => {
    expect(changeLine({ kind: 'grant', change: 'added', entity_kind: 'role', grantee: 'analyst', detail: 'GRANT SELECT ON `sales`.* TO `analyst`' }))
      .toBe('role analyst: GRANT SELECT ON `sales`.* TO `analyst`')
    expect(changeLine({ kind: 'entity', change: 'removed', entity_kind: 'row_policy', grantee: 'p ON d.t', detail: 'row policy dropped' }))
      .toBe('row policy p ON d.t: row policy dropped')
    expect(effectiveLine({ user: 'alice', change: 'gained', what: 'SELECT ON `sales`.*', via: 'analyst' })).toBe('alice gained SELECT ON `sales`.* via analyst')
    expect(effectiveLine({ user: 'bob', change: 'lost', what: 'role auditor', via: 'direct' })).toBe('bob lost role auditor directly')
    expect(entityKeyLabel('settings_profile:p1')).toBe('settings profile p1')
    expect(driftLabel('outside')).toBe('Outside change')
  })

  it('hands a staged revert over once', () => {
    stageRevert({ ops: [{ kind: 'drop_role', name: 'r' }], revertOf: 'abc' })
    expect(takeStagedRevert()?.revertOf).toBe('abc')
    expect(takeStagedRevert()).toBeNull()
  })
})
