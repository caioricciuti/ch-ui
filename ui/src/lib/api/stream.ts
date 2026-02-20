import type { ColumnMeta, QueryStats, StreamMessage } from '../types/query'

/** Execute a streaming query via NDJSON. Calls the provided callbacks as data arrives. */
export async function executeStreamQuery(
  sql: string,
  onMeta: (meta: ColumnMeta[]) => void,
  onChunk: (rows: unknown[][], seq: number) => void,
  onDone: (stats: QueryStats | undefined, totalRows: number) => void,
  onError: (error: string) => void,
  signal?: AbortSignal,
): Promise<void> {
  const res = await fetch('/api/query/stream', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ query: sql }),
    credentials: 'include',
    signal,
  })

  if (!res.ok) {
    const body = await res.json().catch(() => ({ error: `HTTP ${res.status}` }))
    onError(body.error || `Request failed (${res.status})`)
    return
  }

  const reader = res.body!.getReader()
  const decoder = new TextDecoder()
  let buf = ''

  while (true) {
    const { done, value } = await reader.read()
    if (done) break

    buf += decoder.decode(value, { stream: true })
    const lines = buf.split('\n')
    buf = lines.pop()!

    for (const line of lines) {
      if (!line.trim()) continue
      try {
        const msg: StreamMessage = JSON.parse(line)
        switch (msg.type) {
          case 'meta':
            onMeta(msg.meta)
            break
          case 'chunk':
            onChunk(msg.data, msg.seq)
            break
          case 'done':
            onDone(msg.statistics, msg.total_rows)
            break
          case 'error':
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
      const msg: StreamMessage = JSON.parse(buf)
      if (msg.type === 'done') onDone(msg.statistics, msg.total_rows)
      else if (msg.type === 'error') onError(msg.error)
    } catch {
      // ignore
    }
  }
}
