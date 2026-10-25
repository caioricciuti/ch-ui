// A palette action or a deep link that opens a sheet on a page ("New
// dashboard", "Background accounts") leaves an intent here and navigates.
// The page takes it when it mounts, or at once when it is already on screen.
export type PageIntent =
  | 'new-dashboard'
  | 'new-model'
  | 'new-pipeline'
  | 'send-data'
  | 'new-mcp-key'
  | 'new-clickhouse-user'
  /** Admin, Connections: open Background accounts for the session's connection. */
  | 'background-accounts'
  /** Admin, Connections: open the SSO sheet of one connection. */
  | 'sso-setup'

let pending = $state<PageIntent | null>(null)
let backgroundWorker: string | null = null
let ssoConnection: string | null = null

export function requestPageIntent(intent: PageIntent): void {
  pending = intent
}

/** True once for a matching intent, which is then cleared. Reactive: call it in an $effect. */
export function takePageIntent(intent: PageIntent): boolean {
  if (pending !== intent) return false
  pending = null
  return true
}

/** Opens Admin, Connections with Background accounts showing one worker. */
export function requestBackgroundAccounts(worker: string): void {
  backgroundWorker = worker
  pending = 'background-accounts'
}

/** The worker a Background accounts deep link asked for, cleared once read. */
export function takeBackgroundWorker(): string | null {
  const w = backgroundWorker
  backgroundWorker = null
  return w
}

/** Opens Admin, Connections with the SSO sheet of one connection. */
export function requestSSOSetup(connectionId: string): void {
  ssoConnection = connectionId
  pending = 'sso-setup'
}

/** The connection an SSO setup deep link asked for, cleared once read. */
export function takeSSOConnection(): string | null {
  const id = ssoConnection
  ssoConnection = null
  return id
}
