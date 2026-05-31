export interface SchemaContextEntry {
  database: string
  table: string
  columns: { name: string; type: string }[]
}

export interface BrainChat {
  id: string
  connection_id: string
  username: string
  title: string
  provider_id?: string | null
  model_id?: string | null
  archived: boolean
  last_message_at?: string | null
  context_database?: string | null
  context_table?: string | null
  context_tables?: string | null
  created_at: string
  updated_at: string
}

export interface ToolCall {
  id: string
  tool: string
  args?: Record<string, unknown>
  status: 'pending_approval' | 'pending' | 'running' | 'success' | 'error' | 'declined'
  result?: Record<string, unknown> | string
  approvalId?: string
}

export interface BrainMessage {
  id: string
  chat_id: string
  role: 'user' | 'assistant' | string
  content: string
  status: string
  error?: string | null
  toolCalls?: ToolCall[]
  created_at: string
  updated_at: string
}

export interface BrainArtifact {
  id: string
  chat_id: string
  message_id?: string | null
  type: string
  title: string
  content: string
  created_by?: string | null
  created_at: string
}

export interface BrainModelOption {
  id: string
  name: string
  display_name?: string
  provider_id: string
  provider_name: string
  provider_kind: string
  is_active: boolean
  is_default: boolean
  provider_active: boolean
  provider_default: boolean
}

export interface BrainProviderAdmin {
  id: string
  name: string
  kind: string
  base_url?: string | null
  has_api_key: boolean
  is_active: boolean
  is_default: boolean
  created_by?: string | null
  created_at: string
  updated_at: string
}

export interface BrainSkill {
  id: string
  name: string
  content: string
  is_active: boolean
  is_default: boolean
  created_by?: string | null
  created_at: string
  updated_at: string
}

export type MentionRef =
  | { type: 'table'; database: string; table: string }
  | { type: 'dashboard'; id: string; name: string }
  | { type: 'pipeline'; id: string; name: string }
  | { type: 'model'; id: string; name: string }
  | { type: 'saved_query'; id: string; name: string }

export interface EntityContext {
  type: 'dashboard' | 'pipeline' | 'model' | 'saved_query'
  id: string
  name: string
}
