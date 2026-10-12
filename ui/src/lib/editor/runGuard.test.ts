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
