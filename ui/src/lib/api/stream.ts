import { withBase } from '../basePath'
import { connectionHeaders } from '../stores/workspace-scope'
import type { ColumnMeta, QueryProgress, QueryStats, StreamMessage } from '../types/query'
import { safeParse } from '../utils/safe-json'

export const STREAM_CLOSED_EARLY = 'The connection closed before the query finished'

/** Execute a streaming query via NDJSON. Calls the provided callbacks as data arrives. */
export async function executeStreamQuery(
  sql: string,
  maxResultRows: number,
  onMeta: (meta: ColumnMeta[]) => void,
  onChunk: (rows: unknown[][], seq: number) => void,
  /** queryId is the ClickHouse query_id of the run, when the server reports it. */
  onDone: (stats: QueryStats | undefined, totalRows: number, queryId?: string) => void,
  onError: (error: string) => void,
  signal?: AbortSignal,
  params?: Record<string, string>,
  onProgress?: (progress: QueryProgress) => void,
): Promise<void> {
  const res = await fetch(withBase('/api/query/stream'), {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...connectionHeaders() },
    body: JSON.stringify({ query: sql, maxResultRows, params }),
    credentials: 'include',
    signal,
  })

  if (!res.ok) {
    const body = await res.json().catch(() => ({ error: `HTTP ${res.status}` }))
    onError(body.error || `Request failed (${res.status})`)
    return
  }

  if (!res.body) {
    onError('The server returned an empty response')
    return
  }

  const reader = res.body.getReader()
  const decoder = new TextDecoder()
  let buf = ''
  // done and error are terminal. A stream that closes without either (proxy
  // timeout, server crash) would otherwise leave the caller running forever.
  let finished = false

  while (true) {
    const { done, value } = await reader.read()
    if (done) break

    buf += decoder.decode(value, { stream: true })
    const lines = buf.split('\n')
    buf = lines.pop()!

    for (const line of lines) {
      if (!line.trim()) continue
      try {
        const msg: StreamMessage = safeParse(line)
        switch (msg.type) {
          case 'meta':
            onMeta(msg.meta)
            break
          case 'chunk':
            onChunk(msg.data, msg.seq)
            break
          case 'progress':
            onProgress?.(msg.progress)
            break
          case 'done':
            finished = true
            onDone(msg.statistics, msg.total_rows, msg.query_id)
            break
          case 'error':
            finished = true
            onError(msg.error)
            break
        }
      } catch {
        // Skip malformed lines
      }
    }
  }

  // Process remaining buffer
  if (buf.trim()) {
    try {
      const msg: StreamMessage = safeParse(buf)
      if (msg.type === 'done') {
        finished = true
        onDone(msg.statistics, msg.total_rows, msg.query_id)
      } else if (msg.type === 'error') {
        finished = true
        onError(msg.error)
      }
    } catch {
      // ignore
    }
  }

  if (!finished && !signal?.aborted) {
    onError(STREAM_CLOSED_EARLY)
  }
}

/** ClickHouse output formats the server can export a full result in. */
export type FullExportFormat = 'CSVWithNames' | 'TSVWithNames' | 'JSONEachRow' | 'Parquet'

export const EXPORT_CUT_OFF =
  'The export stopped before it finished (a ClickHouse error or the export size limit). Nothing was saved; Query History has the reason.'

/** Picks the file name out of a Content-Disposition header. */
export function filenameFromDisposition(header: string | null, fallback: string): string {
  const m = /filename="([^"]+)"/.exec(header ?? '')
  const name = m?.[1] ?? ''
  return /^[\w.-]+$/.test(name) ? name : fallback
}

/**
 * Re-runs a statement on the server with FORMAT <format> and collects the
 * whole file. onProgress gets the bytes received so far. Throws with the
 * server's message when the export is refused, and EXPORT_CUT_OFF when it
 * stops part way (the server aborts the download rather than send a
 * truncated file).
 */
export async function exportFullResult(
  sql: string,
  format: FullExportFormat,
  params: Record<string, string> | undefined,
  onProgress: (bytes: number) => void,
  signal?: AbortSignal,
): Promise<{ blob: Blob; filename: string }> {
  const res = await fetch(withBase('/api/query/export'), {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...connectionHeaders() },
    body: JSON.stringify({ sql, format, params }),
    credentials: 'include',
    signal,
  })
  if (!res.ok) {
    const body: unknown = await res.json().catch(() => null)
    const msg = typeof body === 'object' && body !== null && 'error' in body ? String((body as { error: unknown }).error) : ''
    throw new Error(msg || `Export failed (${res.status})`)
  }
  const type = res.headers.get('content-type') ?? 'application/octet-stream'
  const filename = filenameFromDisposition(res.headers.get('content-disposition'), 'ch-ui-export')
  if (!res.body) return { blob: new Blob([], { type }), filename }

  const reader = res.body.getReader()
  const parts: Uint8Array[] = []
  let bytes = 0
  try {
    while (true) {
      const { done, value } = await reader.read()
      if (done) break
      parts.push(value)
      bytes += value.byteLength
      onProgress(bytes)
    }
  } catch (e) {
    if (signal?.aborted) throw e
    throw new Error(EXPORT_CUT_OFF)
  }
  return { blob: new Blob(parts as BlobPart[], { type }), filename }
}
