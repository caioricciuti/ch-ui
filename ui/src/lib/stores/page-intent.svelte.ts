// A palette action that opens a sheet on a page ("New dashboard") leaves
// an intent here and navigates. The page takes it when it mounts, or at
// once when it is already on screen.
export type PageIntent =
  | 'new-dashboard'
  | 'new-model'
  | 'new-pipeline'
  | 'send-data'
  | 'new-mcp-key'
  | 'new-clickhouse-user'

let pending = $state<PageIntent | null>(null)

export function requestPageIntent(intent: PageIntent): void {
  pending = intent
}

/** True once for a matching intent, which is then cleared. Reactive: call it in an $effect. */
export function takePageIntent(intent: PageIntent): boolean {
  if (pending !== intent) return false
  pending = null
  return true
}
