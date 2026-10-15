// The inline profile of the latest run. The server waits and retries for a
// few seconds until query_log has the query, so an earlier run's profile can
// answer after a newer run started; only the profile asked for by the run
// started last may land, and only that load may move the spinner.

import type { QueryProfileResult } from '../types/query'

export interface InlineProfileState {
  profile: Record<string, unknown> | null
  available: boolean
  reason: string | null
  loading: boolean
  error: string | null
}

export const EMPTY_PROFILE: InlineProfileState = { profile: null, available: false, reason: null, loading: false, error: null }

export interface InlineProfileLoader {
  /** A new run started: clear the profile and drop any load still out. */
  reset(): void
  /** Load the profile of the run that just finished. */
  load(queryId: string, sql: string): Promise<void>
}

export function createInlineProfileLoader(
  fetchProfile: (queryId: string, sql: string) => Promise<QueryProfileResult>,
  set: (state: InlineProfileState) => void,
): InlineProfileLoader {
  let latest = 0
  let state = EMPTY_PROFILE
  const update = (patch: Partial<InlineProfileState>) => {
    state = { ...state, ...patch }
    set(state)
  }

  return {
    reset() {
      latest++
      state = EMPTY_PROFILE
      set(state)
    },
    async load(queryId, sql) {
      const id = ++latest
      update({ loading: true, error: null, reason: null })
      try {
        const res = await fetchProfile(queryId, sql)
        if (id !== latest) return
        update({ available: !!res.available, profile: res.profile ?? null, reason: res.reason ?? null })
      } catch (e) {
        if (id !== latest) return
        update({ available: false, profile: null, error: e instanceof Error ? e.message : String(e) })
      } finally {
        if (id === latest) update({ loading: false })
      }
    },
  }
}
