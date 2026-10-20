import { describe, expect, it } from 'vitest'
import { ApiError } from '../api/api-error'
import { errorText, friendlyError, isHumanMessage } from './errors'

describe('isHumanMessage', () => {
  it('accepts messages written for people', () => {
    expect(isHumanMessage('Dashboard name is required')).toBe(true)
    expect(isHumanMessage('A GitHub sync is already running for this connection')).toBe(true)
  })

  it('rejects Go, SQLite, network and ClickHouse internals', () => {
    expect(isHumanMessage('sql: no rows in result set')).toBe(false)
    expect(isHumanMessage('create model: UNIQUE constraint failed: models.connection_id, models.name')).toBe(false)
    expect(isHumanMessage('dial tcp 10.0.0.1:8123: connect: connection refused')).toBe(false)
    expect(isHumanMessage('Code: 60. DB::Exception: Table default.x does not exist. (UNKNOWN_TABLE)')).toBe(false)
    expect(isHumanMessage('json: cannot unmarshal string into Go value of type int')).toBe(false)
    expect(isHumanMessage('')).toBe(false)
  })
})

describe('friendlyError', () => {
  it('keeps a human 4xx message, with the action in front when given', () => {
    const e = new ApiError('Name is required', 400)
    expect(friendlyError(e)).toEqual({ message: 'Name is required', detail: null })
    expect(friendlyError(e, 'save the policy')).toEqual({ message: 'Could not save the policy. Name is required.', detail: null })
  })

  it('says the role is the problem on 403 and keeps the server text as detail', () => {
    expect(friendlyError(new ApiError('Admin access required', 403))).toEqual({
      message: 'Your role cannot do this. Ask an admin for access.',
      detail: 'Admin access required',
    })
  })

  it('tells how long to wait on 429', () => {
    expect(friendlyError(new ApiError('Too many requests', 429, null, 30)).message).toBe('Too many requests. Wait 30 seconds and try again.')
    expect(friendlyError(new ApiError('Too many requests', 429)).message).toBe('Too many requests. Wait a moment and try again.')
  })

  it('maps 413 to a size hint', () => {
    expect(friendlyError(new ApiError('http: request body too large', 413)).message).toBe('That is too large to send. Use a smaller file or fewer rows.')
  })

  it('never shows a 500 body as the message', () => {
    expect(friendlyError(new ApiError('create model: UNIQUE constraint failed: models.name', 500), 'save the model')).toEqual({
      message: 'Could not save the model. The server hit an unexpected error. Try again. If it keeps happening, check the CH-UI server logs.',
      detail: 'create model: UNIQUE constraint failed: models.name',
    })
    expect(friendlyError(new ApiError('Failed to list dashboards', 500)).detail).toBe('Failed to list dashboards')
  })

  it('replaces a raw 4xx body with status copy', () => {
    expect(friendlyError(new ApiError('sql: no rows in result set', 404))).toEqual({
      message: 'That item no longer exists. Refresh the page and try again.',
      detail: 'sql: no rows in result set',
    })
    expect(friendlyError(new ApiError('', 409)).message).toBe('This changed in the meantime. Refresh the page and try again.')
  })

  it('maps known ClickHouse codes and keeps the cleaned ClickHouse text', () => {
    const raw = 'Code: 497. DB::Exception: alice: Not enough privileges. To execute this query, it is necessary to have the grant SELECT ON db.t. (ACCESS_DENIED) (version 24.8.4.13 (official build))'
    expect(friendlyError(new ApiError(raw, 403))).toEqual({
      message: 'Your ClickHouse user is not allowed to do this. Ask an admin to grant access.',
      detail: 'alice: Not enough privileges. To execute this query, it is necessary to have the grant SELECT ON db.t.',
    })
    expect(friendlyError(new ApiError('Code: 241. DB::Exception: Memory limit exceeded. (MEMORY_LIMIT_EXCEEDED)', 502)).message).toBe(
      'ClickHouse ran out of memory. Narrow the time range or add filters, then try again.',
    )
  })

  it('keeps a server prefix ahead of the ClickHouse text', () => {
    const e = new ApiError('Source: Code: 60. DB::Exception: Table default.x does not exist. (UNKNOWN_TABLE)', 502)
    expect(friendlyError(e, 'compare the schemas')).toEqual({
      message: 'Could not compare the schemas. That table does not exist in ClickHouse. Refresh the explorer and check the name.',
      detail: 'Source: Table default.x does not exist.',
    })
  })

  it('uses generic ClickHouse copy for an unknown code', () => {
    expect(friendlyError(new ApiError('Code: 999. DB::Exception: Something odd. (ODD_THING)', 502)).message).toBe(
      'ClickHouse returned an error. Check the details, then try again.',
    )
  })

  it('tells an unreachable ClickHouse from one that answered', () => {
    expect(friendlyError(new ApiError('dial tcp 10.0.0.1:8123: connect: connection refused', 502)).message).toBe(
      'Could not reach ClickHouse. Check that the server and its connection are up, then try again.',
    )
    expect(friendlyError(new ApiError('clickhouse: bad response', 502)).message).toBe('ClickHouse returned an error. Check the details, then try again.')
    expect(friendlyError(new ApiError('Connection is offline', 503))).toEqual({ message: 'Connection is offline', detail: null })
  })

  it('names a network failure', () => {
    expect(friendlyError(new TypeError('Failed to fetch'), 'load the logs')).toEqual({
      message: 'Could not load the logs. Could not reach the CH-UI server. Check your network connection and try again.',
      detail: 'Failed to fetch',
    })
  })

  it('handles the redirect to sign in and aborts', () => {
    expect(friendlyError(new Error('Session expired'))).toEqual({ message: 'Your session ended. Sign in again to continue.', detail: null })
    expect(friendlyError(new DOMException('aborted', 'AbortError')).message).toBe('The request was cancelled.')
  })

  it('handles non-API errors and plain strings', () => {
    expect(friendlyError(new Error('Pick a file first'))).toEqual({ message: 'Pick a file first', detail: null })
    expect(friendlyError('odd')).toEqual({ message: 'odd', detail: null })
    expect(friendlyError(null)).toEqual({ message: 'Something went wrong. Try again.', detail: null })
    expect(friendlyError(new Error('context deadline exceeded'))).toEqual({ message: 'Something went wrong. Try again.', detail: 'context deadline exceeded' })
  })

  it('clips a long detail', () => {
    const detail = friendlyError(new ApiError('x'.repeat(1000), 500)).detail ?? ''
    expect(detail.length).toBe(403)
    expect(detail.endsWith('...')).toBe(true)
  })

  it('never writes an em dash or an exclamation mark', () => {
    const samples: unknown[] = [
      new ApiError('', 401), new ApiError('', 402), new ApiError('', 403), new ApiError('', 404),
      new ApiError('', 409), new ApiError('', 413), new ApiError('', 429), new ApiError('', 500),
      new ApiError('', 502), new ApiError('Code: 62. DB::Exception: Syntax error', 400), new TypeError('Failed to fetch'),
    ]
    for (const e of samples) {
      const { message } = friendlyError(e, 'save it')
      expect(message).not.toMatch(/[—!]/)
    }
  })
})

describe('errorText', () => {
  it('adds the detail as a Details tail', () => {
    expect(errorText(new ApiError('sql: database is locked', 500))).toBe(
      'The server hit an unexpected error. Try again. If it keeps happening, check the CH-UI server logs. Details: sql: database is locked',
    )
    expect(errorText(new ApiError('Name is required', 400))).toBe('Name is required')
  })
})
