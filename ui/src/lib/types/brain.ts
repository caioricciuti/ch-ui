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
  /** The global daily rule's token limit, kept for older clients; 0 means none. Budgets are edited as rules. */
  dailyTokenBudget: number
  /** Output cap per request; 0 means the model default. */
  maxOutputTokens: number
  prices: Record<string, BrainModelPrice> | null
}

export type BrainUsageGroupBy = 'person' | 'role'

export interface BrainUsageRow {
  /** The person or role the row sums. */
  group: string
  username?: string
  /** Empty for requests stored before roles were recorded. */
  role?: string
  provider: string
  model: string
  requests: number
  chat_turns: number
  /** Ask AI and Fix with AI requests. */
  ask_ai: number
  /** Extra calls that repaired SQL ClickHouse rejected. */
  repairs: number
  input_tokens: number
  output_tokens: number
  cache_read_tokens: number
  cache_write_tokens: number
  last_used_at: string
  /** Cost stored when the requests ran; null when none was priced. */
  cost_usd: number | null
  unpriced_requests: number
  /** Part of the cost was backfilled from the prices in effect at the upgrade. */
  estimated: boolean
}

export interface BrainUsageTotals {
  requests: number
  groups: number
  users?: number
  input_tokens: number
  output_tokens: number
  cache_read_tokens: number
  cache_write_tokens: number
  cost_usd: number
  cost_estimated: boolean
  unpriced_requests: number
}

export interface BrainUnpricedModel {
  provider: string
  model: string
  requests: number
}

export interface BrainUsageReport {
  since: string
  until: string
  days: number
  group_by: BrainUsageGroupBy
  rows: BrainUsageRow[]
  unpriced_models: BrainUnpricedModel[]
  totals: BrainUsageTotals
}

export type BrainBudgetScope = 'global' | 'role' | 'user'
export type BrainBudgetPeriod = 'day' | 'month'

/** One budget rule. Each rule caps every person it applies to on their own. */
export interface BrainBudgetRule {
  id: string
  scope: BrainBudgetScope
  /** '' for global, a role for role, a user key (ClickHouse user or sso:email) for user. */
  subject: string
  period: BrainBudgetPeriod
  token_limit: number | null
  usd_limit: number | null
  block_unpriced: boolean
  created_at: string
  updated_at: string
}

export interface BrainBudgetRuleUsage {
  people: number
  top_subject?: string
  top_tokens: number
  top_usd: number
  top_percent: number
  over_limit: number
  reset_at: string
}

export interface BrainBudgetRuleView extends BrainBudgetRule {
  usage: BrainBudgetRuleUsage
  /** A role or person rule the license does not cover. */
  inactive: boolean
  /** The dollar limit is not enforced without Pro. */
  ignored_usd: boolean
}

export interface BrainBudgetList {
  budgets: BrainBudgetRuleView[]
  pro: boolean
  legacyDailyTokens: number
}

/** One budget rule that applies to the signed-in person, with what they used. */
export interface BrainBudgetStatus {
  period: BrainBudgetPeriod
  scope: BrainBudgetScope
  subject?: string
  /** How the person reads the rule, such as "your role's daily Brain budget of $5.00". */
  rule: string
  token_limit: number | null
  usd_limit: number | null
  block_unpriced: boolean
  used_tokens: number
  used_usd: number
  percent: number
  exceeded: boolean
  reset_at: string
}
