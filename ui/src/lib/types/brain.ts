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

/** Tokens one assistant message (all its provider calls) or request used. */
export interface BrainUsage {
  provider: string
  model: string
  input_tokens: number
  output_tokens: number
  cache_read_tokens: number
  cache_write_tokens: number
  /** Estimated from the admin's price table; absent when the model has no price. */
  cost_usd?: number
}

export interface BrainMessage {
  id: string
  chat_id: string
  role: 'user' | 'assistant' | string
  content: string
  status: string
  error?: string | null
  toolCalls?: ToolCall[]
  usage?: BrainUsage
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

/** US dollars per million tokens; null means not set. */
export interface BrainModelPrice {
  input: number | null
  output: number | null
  cacheRead: number | null
  cacheWrite: number | null
}

export interface BrainSettings {
  agentModelId: string
  quickModelId: string
  /** Tokens per person per UTC day; 0 means no limit. */
  dailyTokenBudget: number
  /** Output cap per request; 0 means the model default. */
  maxOutputTokens: number
  prices: Record<string, BrainModelPrice> | null
}

export interface BrainUsageRow {
  username: string
  provider: string
  model: string
  requests: number
  chat_turns: number
  ask_ai: number
  input_tokens: number
  output_tokens: number
  cache_read_tokens: number
  cache_write_tokens: number
  last_used_at: string
  cost_usd: number | null
}

export interface BrainUsageTotals {
  requests: number
  users: number
  input_tokens: number
  output_tokens: number
  cache_read_tokens: number
  cache_write_tokens: number
  cost_usd: number
  priced_rows: number
  unpriced_rows: number
}

export interface BrainUsageReport {
  since: string
  until: string
  days: number
  rows: BrainUsageRow[]
  totals: BrainUsageTotals
}
