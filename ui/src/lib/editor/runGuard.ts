// Bookkeeping for query runs in the editor. A run can outlive the component
// that started it (tabs remount on switch), and its callbacks can fire after a
// newer run took over the same tab. These helpers make "is this still the run
// the user is looking at?" a single check.

export interface RunRegistry {
  /**
   * Stop the current run for key (if any) and register a new one. Returns its
   * id. owner names what the run fills (a result id), see stopOwned. Side
   * runs of the key keep going, except one filling the same owner.
   */
  start(key: string, cancel: () => void, owner?: string): number
  /**
   * Register a side run that fills one existing result (owner), next to the
   * key's run instead of replacing it: re-sorting a result must not stop a
   * Run All. It supersedes only an earlier run filling the same owner.
   */
  startSide(key: string, owner: string, cancel: () => void): number
  /** Stop every run for key, side runs included: calls their cancel and forgets them. */
  stop(key: string): void
  /** Stop the runs for key that fill owner. */
  stopOwned(key: string, owner: string): void
  /** Stop every run. */
  stopAll(): void
  /** True while id is a live run registered for key. */
  isCurrent(key: string, id: number): boolean
}

interface Run {
  id: number
  cancel: () => void
  owner?: string
}

export function createRunRegistry(): RunRegistry {
  let seq = 0
  const runs = new Map<string, Run>()
  const sides = new Map<string, Map<string, Run>>()

  function stopMain(key: string): void {
    const run = runs.get(key)
    if (!run) return
    runs.delete(key)
    run.cancel()
  }

  function stopSide(key: string, owner: string): void {
    const forKey = sides.get(key)
    const run = forKey?.get(owner)
    if (!forKey || !run) return
    forKey.delete(owner)
    if (forKey.size === 0) sides.delete(key)
    run.cancel()
  }

  function stopOwned(key: string, owner: string): void {
    if (runs.get(key)?.owner === owner) stopMain(key)
    stopSide(key, owner)
  }

  function stop(key: string): void {
    stopMain(key)
    for (const owner of [...(sides.get(key)?.keys() ?? [])]) stopSide(key, owner)
  }

  return {
    start(key, cancel, owner) {
      stopMain(key)
      if (owner !== undefined) stopSide(key, owner)
      const id = ++seq
      runs.set(key, { id, cancel, owner })
      return id
    },
    startSide(key, owner, cancel) {
      stopOwned(key, owner)
      const id = ++seq
      let forKey = sides.get(key)
      if (!forKey) sides.set(key, (forKey = new Map()))
      forKey.set(owner, { id, cancel, owner })
      return id
    },
    stop,
    stopOwned,
    stopAll() {
      for (const key of new Set([...runs.keys(), ...sides.keys()])) stop(key)
    },
    isCurrent(key, id) {
      if (runs.get(key)?.id === id) return true
      for (const run of sides.get(key)?.values() ?? []) if (run.id === id) return true
      return false
    },
  }
}

export interface FrameBatcher<T> {
  /** Everything pushed so far, in order. */
  readonly items: T[]
  /** Append items and schedule one flush for the next frame. Ignored once closed. */
  push(items: readonly T[]): void
  /** Cancel the pending frame. Later pushes and frames do nothing. */
  close(): void
}

/**
 * Accumulates streamed rows and hands the whole buffer to flush at most once
 * per animation frame. Closing it cancels the pending frame, so rows from an
 * aborted run cannot land after a new run has started.
 */
export function createFrameBatcher<T>(
  flush: (items: T[]) => void,
  schedule: (cb: () => void) => number = (cb) => requestAnimationFrame(cb),
  unschedule: (id: number) => void = (id) => cancelAnimationFrame(id),
): FrameBatcher<T> {
  const items: T[] = []
  let frame: number | null = null
  let closed = false

  return {
    items,
    push(next) {
      if (closed) return
      // A loop, not push(...next): large chunks would overflow the call stack.
      for (const item of next) items.push(item)
      if (frame !== null) return
      frame = schedule(() => {
        frame = null
        if (!closed) flush(items)
      })
    },
    close() {
      closed = true
      if (frame !== null) {
        unschedule(frame)
        frame = null
      }
    },
  }
}
