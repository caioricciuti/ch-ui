import { describe, expect, it } from 'vitest'
import { linkedSaveBody, savedQueryConflict } from './saved-query-save'

class FakeApiError extends Error {
  constructor(readonly status: number, readonly body: unknown) {
    super('api error')
  }
}

describe('linkedSaveBody', () => {
  it('sends the loaded version as if_version', () => {
    expect(linkedSaveBody('SELECT 1', { x: '1' }, 4)).toEqual({ query: 'SELECT 1', parameters: { x: '1' }, if_version: 4 })
    expect(linkedSaveBody('SELECT 1', {}, 0)).toEqual({ query: 'SELECT 1', parameters: {}, if_version: 0 })
  })

  it('sends no precondition for a tab without a known version', () => {
    expect(linkedSaveBody('SELECT 1', {}, undefined)).toEqual({ query: 'SELECT 1', parameters: {} })
  })
})

describe('savedQueryConflict', () => {
  const stored = { id: 'q1', name: 'Revenue', query: 'SELECT 2', version: 7 }

  it('returns the stored query of a 409 conflict', () => {
    const e = new FakeApiError(409, { error: 'changed', code: 'saved_query_conflict', saved_query: stored })
    expect(savedQueryConflict(e)).toEqual(stored)
  })

  it('ignores other failures', () => {
    expect(savedQueryConflict(new FakeApiError(409, { error: 'connection_changed' }))).toBeNull()
    expect(savedQueryConflict(new FakeApiError(500, { code: 'saved_query_conflict', saved_query: stored }))).toBeNull()
    expect(savedQueryConflict(new FakeApiError(409, { code: 'saved_query_conflict', saved_query: { id: 'q1' } }))).toBeNull()
    expect(savedQueryConflict(new Error('network'))).toBeNull()
    expect(savedQueryConflict(null)).toBeNull()
  })
})
