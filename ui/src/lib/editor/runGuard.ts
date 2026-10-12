// Bookkeeping for query runs in the editor. A run can outlive the component
// that started it (tabs remount on switch), and its callbacks can fire after a
// newer run took over the same tab. These helpers make "is this still the run
// the user is looking at?" a single check.

export interface RunRegistry {
  /** Stop the current run for key (if any) and register a new one. Returns its id. */
  start(key: string, cancel: () => void): number
  /** Stop the current run for key: calls its cancel and forgets it. */
  stop(key: string): void
  /** True while id is the latest run registered for key. */
  isCurrent(key: string, id: number): boolean
}

export function createRunRegistry(): RunRegistry {
  let seq = 0
  const runs = new Map<string, { id: number; cancel: () => void }>()

  function stop(key: string): void {
    const run = runs.get(key)
    if (!run) return
    runs.delete(key)
    run.cancel()
  }

  return {
    start(key, cancel) {
      stop(key)
      const id = ++seq
      runs.set(key, { id, cancel })
      return id
    },
    stop,
    isCurrent(key, id) {
      return runs.get(key)?.id === id
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
