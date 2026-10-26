import { afterEach, describe, expect, it, vi } from 'vitest'
import { executeStreamQuery, exportFullResult, filenameFromDisposition, EXPORT_CUT_OFF, STREAM_CLOSED_EARLY } from './stream'
import { CONNECTION_CHANGED_MESSAGE, onConnectionChanged } from './connection-changed'

function connectionChanged(): Response {
  return new Response(JSON.stringify({ success: false, error: 'connection_changed' }), { status: 409 })
}

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

  it('a 409 connection_changed reports the notice text, not the code', async () => {
    const changed = vi.fn()
    onConnectionChanged(changed)
    vi.stubGlobal('fetch', vi.fn(async () => connectionChanged()))
    const onError = vi.fn()
    await executeStreamQuery('SELECT 1', 100, () => {}, () => {}, () => {}, onError)
    onConnectionChanged(null)
    expect(onError).toHaveBeenCalledWith(CONNECTION_CHANGED_MESSAGE)
    expect(changed).toHaveBeenCalledTimes(1)
  })

  it('stays quiet when the caller aborted', async () => {
    const controller = new AbortController()
    controller.abort()
    const { onError } = await run(['{"type":"chunk","data":[[1]],"seq":0}'], controller.signal)
    expect(onError).not.toHaveBeenCalled()
  })
})

describe('exportFullResult', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('collects the file, reports progress and reads the file name', async () => {
    const body = new ReadableStream<Uint8Array>({
      start(controller) {
        controller.enqueue(new Uint8Array([80, 65, 82, 49]))
        controller.enqueue(new Uint8Array([0, 80, 65, 82, 49]))
        controller.close()
      },
    })
    const fetchMock = vi.fn(async () => new Response(body, {
      status: 200,
      headers: { 'Content-Type': 'application/vnd.apache.parquet', 'Content-Disposition': 'attachment; filename="ch-ui-export-20261013-101112.parquet"' },
    }))
    vi.stubGlobal('fetch', fetchMock)
    const progress: number[] = []
    const { blob, filename } = await exportFullResult('SELECT 1', 'Parquet', { n: '1' }, (b) => progress.push(b))
    expect(filename).toBe('ch-ui-export-20261013-101112.parquet')
    expect(blob.size).toBe(9)
    expect(progress).toEqual([4, 9])
    const init = fetchMock.mock.calls[0] as unknown as [string, RequestInit]
    expect(JSON.parse(String(init[1].body))).toEqual({ sql: 'SELECT 1', format: 'Parquet', params: { n: '1' } })
  })

  it('throws the server message when the export is refused', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify({ error: 'update the agent' }), { status: 409 })))
    await expect(exportFullResult('SELECT 1', 'CSVWithNames', undefined, () => {})).rejects.toThrow('update the agent')
  })

  it('a 409 connection_changed on export throws the notice text', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => connectionChanged()))
    await expect(exportFullResult('SELECT 1', 'CSVWithNames', undefined, () => {})).rejects.toThrow(CONNECTION_CHANGED_MESSAGE)
  })

  it('reports a download cut off by the server', async () => {
    const body = new ReadableStream<Uint8Array>({
      start(controller) {
        controller.enqueue(new Uint8Array([1, 2, 3]))
        controller.error(new TypeError('network error'))
      },
    })
    vi.stubGlobal('fetch', vi.fn(async () => new Response(body, { status: 200 })))
    await expect(exportFullResult('SELECT 1', 'CSVWithNames', undefined, () => {})).rejects.toThrow(EXPORT_CUT_OFF)
  })

  it('accepts only plain file names from the header', () => {
    expect(filenameFromDisposition('attachment; filename="../../etc/x"', 'f')).toBe('f')
    expect(filenameFromDisposition(null, 'f')).toBe('f')
  })
})
