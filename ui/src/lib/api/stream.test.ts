import { afterEach, describe, expect, it, vi } from 'vitest'
import { executeStreamQuery, STREAM_CLOSED_EARLY } from './stream'

function ndjsonResponse(lines: string[]): Response {
  const body = new ReadableStream<Uint8Array>({
    start(controller) {
      const enc = new TextEncoder()
      for (const line of lines) controller.enqueue(enc.encode(line + '\n'))
      controller.close()
    },
  })
  return new Response(body, { status: 200 })
}

async function run(lines: string[], signal?: AbortSignal) {
  vi.stubGlobal('fetch', vi.fn(async () => ndjsonResponse(lines)))
  const onDone = vi.fn()
  const onError = vi.fn()
  const onChunk = vi.fn()
  await executeStreamQuery('SELECT 1', 100, () => {}, onChunk, onDone, onError, signal)
  return { onDone, onError, onChunk }
}

describe('executeStreamQuery', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('reports done once and no error for a complete stream', async () => {
    const { onDone, onError, onChunk } = await run([
      '{"type":"meta","meta":[{"name":"x","type":"UInt8"}]}',
      '{"type":"chunk","data":[[1]],"seq":0}',
      '{"type":"done","statistics":{"elapsed":0.1,"rows_read":1,"bytes_read":1},"total_rows":1}',
    ])
    expect(onChunk).toHaveBeenCalledOnce()
    expect(onDone).toHaveBeenCalledOnce()
    expect(onError).not.toHaveBeenCalled()
  })

  it('reports an error when the stream closes without done or error', async () => {
    const { onDone, onError } = await run([
      '{"type":"meta","meta":[{"name":"x","type":"UInt8"}]}',
      '{"type":"chunk","data":[[1]],"seq":0}',
    ])
    expect(onDone).not.toHaveBeenCalled()
    expect(onError).toHaveBeenCalledWith(STREAM_CLOSED_EARLY)
  })

  it('does not add a second error after a server error line', async () => {
    const { onError } = await run(['{"type":"error","error":"Code: 60. Unknown table"}'])
    expect(onError).toHaveBeenCalledTimes(1)
    expect(onError).toHaveBeenCalledWith('Code: 60. Unknown table')
  })

  it('stays quiet when the caller aborted', async () => {
    const controller = new AbortController()
    controller.abort()
    const { onError } = await run(['{"type":"chunk","data":[[1]],"seq":0}'], controller.signal)
    expect(onError).not.toHaveBeenCalled()
  })
})
