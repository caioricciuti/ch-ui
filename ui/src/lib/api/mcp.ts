import { apiGet, apiPost, apiDel } from './client'

export interface MCPKey {
  id: string
  name: string
  key_prefix: string
  connection_id: string
  ch_user: string
  scopes: 'read' | 'read_write'
  allowed_databases: string
  created_by: string
  created_at: string
  last_used_at: string | null
  revoked_at: string | null
}

export interface MCPKeysResult {
  success: boolean
  keys: MCPKey[]
}

export interface CreateMCPKeyRequest {
  name: string
  connection_id: string
  ch_user: string
  ch_password: string
  scopes: 'read' | 'read_write'
  allowed_databases: string
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

export function revokeMCPKey(id: string): Promise<{ success: boolean }> {
  return apiDel<{ success: boolean }>(`/api/mcp-keys/${encodeURIComponent(id)}`)
}
