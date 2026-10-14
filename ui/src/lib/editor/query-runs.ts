// The SQL editor's in-flight query runs, one per tab. It lives outside the
// editor component: a tab's component is destroyed on every tab switch while
// its stream keeps running, and closing a tab, closing a result or switching
// connection must still be able to stop that stream. Stopping a run aborts its
// fetch, and the server then cancels the query on ClickHouse.

import { createRunRegistry } from './runGuard'

export const queryRuns = createRunRegistry()

/** Stops the run of a tab, if one is in flight. */
export function stopRunsForTab(tabId: string): void {
  queryRuns.stop(tabId)
}

/** Stops the run of a tab when it is filling the given result. */
export function stopRunForResult(tabId: string, resultId: string): void {
  queryRuns.stopOwned(tabId, resultId)
}

/** Stops every run, for a switch to another person or connection. */
export function stopAllRuns(): void {
  queryRuns.stopAll()
}
