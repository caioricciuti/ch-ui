import { describe, expect, it } from 'vitest'
import { createInlineProfileLoader, type InlineProfileState } from './inline-profile'
import type { QueryProfileResult } from '../types/query'

function deferred() {
  let resolve!: (v: QueryProfileResult) => void
  let reject!: (e: unknown) => void
  const promise = new Promise<QueryProfileResult>((res, rej) => {
    resolve = res
    reject = rej
  })
  return { promise, resolve, reject }
}

function setup() {
  const pending = new Map<string, ReturnType<typeof deferred>>()
  let state: InlineProfileState | null = null
  const loader = createInlineProfileLoader(
    (queryId) => {
      const d = deferred()
      pending.set(queryId, d)
      return d.promise
    },
    (next) => (state = next),
  )
  return { loader, pending, get state() { return state! } }
}

describe('createInlineProfileLoader', () => {
  it('shows the profile of the run it was asked for', async () => {
    const t = setup()
    const load = t.loader.load('q1', 'SELECT 1')
    expect(t.state.loading).toBe(true)
    t.pending.get('q1')!.resolve({ success: true, available: true, profile: { rows: 1 } })
    await load
    expect(t.state).toMatchObject({ loading: false, available: true, profile: { rows: 1 } })
  })

  it('drops an earlier run\'s profile that answers after a newer run started', async () => {
    const t = setup()
    const first = t.loader.load('q1', 'SELECT 1')
    // A new run starts: the panel clears and the old load no longer counts.
    t.loader.reset()
    expect(t.state).toMatchObject({ loading: false, profile: null })
    const second = t.loader.load('q2', 'SELECT 2')
    t.pending.get('q1')!.resolve({ success: true, available: true, profile: { from: 'q1' } })
    await first
    expect(t.state.profile).toBeNull()
    expect(t.state.loading).toBe(true) // still waiting for q2, not cleared by q1
    t.pending.get('q2')!.resolve({ success: true, available: true, profile: { from: 'q2' } })
    await second
    expect(t.state).toMatchObject({ loading: false, profile: { from: 'q2' } })
  })

  it('keeps the spinner off for a run that never asked for a profile', async () => {
    const t = setup()
    const first = t.loader.load('q1', 'SELECT 1')
    t.loader.reset() // e.g. EXPLAIN or a run without a query id
    t.pending.get('q1')!.reject(new Error('timeout'))
    await first
    expect(t.state).toMatchObject({ loading: false, error: null, profile: null })
  })

  it('reports an error of the current load', async () => {
    const t = setup()
    const load = t.loader.load('q1', 'SELECT 1')
    t.pending.get('q1')!.reject(new Error('profile unavailable'))
    await load
    expect(t.state).toMatchObject({ loading: false, available: false, error: 'profile unavailable' })
  })
})
