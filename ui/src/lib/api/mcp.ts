import { apiGet, apiPost, apiDel } from './client'

export interface MCPKey {
  id: string
  name: string
  key_prefix: string
  connection_id: string
  ch_user: string
  scopes: 'read' | 'read_write'
  /** Lifecycle actions (run models, start/stop pipelines, run schedules and reports now). Only with read_write. */
  allow_actions: boolean
  allowed_databases: string
  created_by: string
  created_at: string
  /** RFC 3339 instant after which the key is rejected; null = never. */
  expires_at: string | null
  last_used_at: string | null
  revoked_at: string | null
  /** 'api' for admin-created keys, 'oauth' for tokens a person granted via OAuth. */
  kind: 'api' | 'oauth'
  /** The person who granted an OAuth token or created a key that runs as them. */
  subject: string
  client_id: string
}

export interface MCPKeysResult {
  success: boolean
  keys: MCPKey[]
  /** Connection id to the signed-in person's per-person ClickHouse user, where a new key can run as them. */
  as_me: Record<string, string>
}

export interface CreateMCPKeyRequest {
  name: string
  connection_id: string
  ch_user: string
  ch_password: string
  scopes: 'read' | 'read_write'
  /** Requires scopes 'read_write'. */
  allow_actions?: boolean
  allowed_databases: string
  /** Days until the key expires; 0 or omitted = never. */
  expires_in_days?: number
  /** Run as the signed-in person's per-person ClickHouse user; ch_user and ch_password are then ignored. */
  as_me?: boolean
}

export interface CreateMCPKeyResult {
  success: boolean
  key: MCPKey
  /** The plaintext chm_ key. Returned exactly once, at creation. */
  secret: string
}

export function listMCPKeys(): Promise<MCPKeysResult> {
  return apiGet<MCPKeysResult>('/api/mcp-keys')
}

export function createMCPKey(req: CreateMCPKeyRequest): Promise<CreateMCPKeyResult> {
  return apiPost<CreateMCPKeyResult>('/api/mcp-keys', req)
}

/** Issues a replacement key with the same binding and revokes the old one. */
export function rotateMCPKey(id: string): Promise<CreateMCPKeyResult> {
  return apiPost<CreateMCPKeyResult>(`/api/mcp-keys/${encodeURIComponent(id)}/rotate`)
}

export function revokeMCPKey(id: string): Promise<{ success: boolean }> {
  return apiDel<{ success: boolean }>(`/api/mcp-keys/${encodeURIComponent(id)}`)
}
