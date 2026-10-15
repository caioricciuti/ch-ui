import { describe, expect, it, vi } from 'vitest'
import { createFrameBatcher, createRunRegistry } from './runGuard'

function manualFrames() {
  const pending = new Map<number, () => void>()
  let next = 0
  return {
    schedule: (cb: () => void) => {
      pending.set(++next, cb)
      return next
    },
    unschedule: (id: number) => {
      pending.delete(id)
    },
    runAll: () => {
      const cbs = [...pending.values()]
      pending.clear()
      cbs.forEach((cb) => cb())
    },
    get size() {
      return pending.size
    },
  }
}

describe('createRunRegistry', () => {
  it('a new run cancels the previous one and makes it stale', () => {
    const runs = createRunRegistry()
    const cancelFirst = vi.fn()
    const first = runs.start('tab-1', cancelFirst)
    expect(runs.isCurrent('tab-1', first)).toBe(true)

    const second = runs.start('tab-1', () => {})
    expect(cancelFirst).toHaveBeenCalledOnce()
    expect(runs.isCurrent('tab-1', first)).toBe(false)
    expect(runs.isCurrent('tab-1', second)).toBe(true)
  })

  it('stop cancels once and leaves no current run', () => {
    const runs = createRunRegistry()
    const cancel = vi.fn()
    const id = runs.start('tab-1', cancel)
    runs.stop('tab-1')
    runs.stop('tab-1')
    expect(cancel).toHaveBeenCalledOnce()
    expect(runs.isCurrent('tab-1', id)).toBe(false)
  })

  it('keeps tabs independent', () => {
    const runs = createRunRegistry()
    const cancelA = vi.fn()
    const a = runs.start('a', cancelA)
    runs.start('b', () => {})
    expect(cancelA).not.toHaveBeenCalled()
    expect(runs.isCurrent('a', a)).toBe(true)
  })
})

describe('createRunRegistry owners', () => {
  it('stopOwned stops only the run that fills that result', () => {
    const runs = createRunRegistry()
    const cancel = vi.fn()
    const id = runs.start('tab-1', cancel, 'result-a')
    runs.stopOwned('tab-1', 'result-b')
    expect(cancel).not.toHaveBeenCalled()
    runs.stopOwned('tab-1', 'result-a')
    expect(cancel).toHaveBeenCalledOnce()
    expect(runs.isCurrent('tab-1', id)).toBe(false)
  })

  it('stopAll stops every tab', () => {
    const runs = createRunRegistry()
    const cancelA = vi.fn()
    const cancelB = vi.fn()
    runs.start('a', cancelA)
    runs.start('b', cancelB)
    runs.stopAll()
    expect(cancelA).toHaveBeenCalledOnce()
    expect(cancelB).toHaveBeenCalledOnce()
  })
})

describe('createRunRegistry side runs', () => {
  it('a side run (re-sorting one result) leaves the tab run alone', () => {
    const runs = createRunRegistry()
    const cancelMain = vi.fn()
    const main = runs.start('tab-1', cancelMain, 'statement-2')
    const side = runs.startSide('tab-1', 'statement-1', () => {})
    expect(cancelMain).not.toHaveBeenCalled()
    expect(runs.isCurrent('tab-1', main)).toBe(true)
    expect(runs.isCurrent('tab-1', side)).toBe(true)
    // The next statement of the Run All does not stop the re-sort either.
    runs.start('tab-1', () => {}, 'statement-3')
    expect(cancelMain).toHaveBeenCalledOnce()
    expect(runs.isCurrent('tab-1', side)).toBe(true)
  })

  it('a side run supersedes only an earlier run filling the same result', () => {
    const runs = createRunRegistry()
    const cancelFirst = vi.fn()
    const cancelOther = vi.fn()
    const first = runs.startSide('tab-1', 'r1', cancelFirst)
    runs.startSide('tab-1', 'r2', cancelOther)
    const second = runs.startSide('tab-1', 'r1', () => {})
    expect(cancelFirst).toHaveBeenCalledOnce()
    expect(cancelOther).not.toHaveBeenCalled()
    expect(runs.isCurrent('tab-1', first)).toBe(false)
    expect(runs.isCurrent('tab-1', second)).toBe(true)
  })

  it('a tab run filling a result stops that result\'s side run', () => {
    const runs = createRunRegistry()
    const cancelSide = vi.fn()
    runs.startSide('tab-1', 'r1', cancelSide)
    runs.start('tab-1', () => {}, 'r1')
    expect(cancelSide).toHaveBeenCalledOnce()
  })

  it('stop, stopOwned and stopAll reach side runs', () => {
    const runs = createRunRegistry()
    const a = vi.fn()
    const b = vi.fn()
    const c = vi.fn()
    const d = vi.fn()
    runs.start('tab-1', a, 'r1')
    runs.startSide('tab-1', 'r2', b)
    runs.startSide('tab-1', 'r3', c)
    runs.stopOwned('tab-1', 'r2')
    expect(b).toHaveBeenCalledOnce()
    expect(a).not.toHaveBeenCalled()
    runs.stop('tab-1') // Cancel stops everything the tab runs
    expect(a).toHaveBeenCalledOnce()
    expect(c).toHaveBeenCalledOnce()
    runs.startSide('tab-2', 'r4', d)
    runs.stopAll()
    expect(d).toHaveBeenCalledOnce()
  })

  it('a Run All finishes every statement while a result is re-sorted', async () => {
    const runs = createRunRegistry()
    const gates: Array<() => void> = []
    // One statement: claims the tab run, waits, reports whether it still owns it.
    const runStatement = async (owner: string) => {
      const id = runs.start('tab-1', () => {}, owner)
      await new Promise<void>((resolve) => gates.push(resolve))
      return runs.isCurrent('tab-1', id) ? 'ok' : 'stopped'
    }
    const outcomes: string[] = []
    const runAll = (async () => {
      for (const owner of ['s1', 's2', 's3']) {
        const outcome = await runStatement(owner)
        outcomes.push(outcome)
        if (outcome !== 'ok') return
      }
    })()
    await Promise.resolve()
    gates.shift()!() // s1 done
    await new Promise((r) => setTimeout(r))
    // While s2 runs, the user sorts the truncated s1 result (server re-query).
    runs.startSide('tab-1', 's1', () => {})
    gates.shift()!() // s2 done
    await new Promise((r) => setTimeout(r))
    gates.shift()!() // s3 done
    await runAll
    expect(outcomes).toEqual(['ok', 'ok', 'ok'])
  })
})

describe('createFrameBatcher', () => {
  it('flushes all pushed items once per frame', () => {
    const frames = manualFrames()
    const flush = vi.fn<(items: number[]) => void>()
    const batch = createFrameBatcher<number>(flush, frames.schedule, frames.unschedule)
    batch.push([1, 2])
    batch.push([3])
    expect(frames.size).toBe(1)
    frames.runAll()
    expect(flush).toHaveBeenCalledOnce()
    expect(flush.mock.calls[0][0]).toEqual([1, 2, 3])
  })

  it('drops the pending frame and later pushes after close', () => {
    const frames = manualFrames()
    const flush = vi.fn<(items: number[]) => void>()
    const batch = createFrameBatcher<number>(flush, frames.schedule, frames.unschedule)
    batch.push([1])
    batch.close()
    batch.push([2])
    frames.runAll()
    expect(flush).not.toHaveBeenCalled()
    expect(batch.items).toEqual([1])
  })

  it('ignores a frame that fires after close', () => {
    const queued: Array<() => void> = []
    const flush = vi.fn<(items: number[]) => void>()
    // An unschedule that cannot cancel, like a frame already dequeued.
    const batch = createFrameBatcher<number>(flush, (cb) => queued.push(cb), () => {})
    batch.push([1])
    batch.close()
    queued.forEach((cb) => cb())
    expect(flush).not.toHaveBeenCalled()
  })
})
