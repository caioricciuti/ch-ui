export type Materialization = 'view' | 'table' | 'incremental' | 'materialized_view'
export type IncrementalStrategy = 'append' | 'delete_insert'
export type ModelStatus = 'draft' | 'success' | 'error'
export type RunStatus = 'running' | 'success' | 'partial' | 'error'
export type ResultStatus = 'pending' | 'running' | 'success' | 'error' | 'skipped'

export interface Model {
  id: string
  name: string
  description: string
  connection_id: string
  target_database: string
  materialization: Materialization
  sql_body: string
  table_engine: string
  order_by: string
  /** PARTITION BY expression (MergeTree-family engines). */
  partition_by: string
  /** Engine arguments without parentheses, e.g. the ReplacingMergeTree version column. */
  engine_args: string
  /** incremental only; '' for other materializations. */
  incremental_strategy: IncrementalStrategy | ''
  /** delete_insert key: one column or a comma-separated list. */
  unique_key: string
  /** incremental: later runs take rows above max(watermark_column) of the table. */
  watermark_column: string
  status: ModelStatus
  last_error: string | null
  last_run_at: string | null
  created_by: string | null
  source: 'manual' | 'github'
  created_at: string
  updated_at: string
}

export interface GitHubIntegration {
  enabled: boolean
  repo: string
  branch: string
  path: string
  has_pat: boolean
  has_webhook_secret: boolean
  last_sync_sha: string
}

export interface GitHubSyncLog {
  id: string
  connection_id: string
  status: string
  models_created: number
  models_updated: number
  models_deleted: number
  models_unchanged: number
  error: string | null
  triggered_by: string | null
  started_at: string
  finished_at: string | null
  commit_sha: string | null
  created_at: string
}

export interface GitHubSyncResult {
  created: number
  updated: number
  deleted: number
  unchanged: number
  commit_sha: string
}

export interface ModelRun {
  id: string
  connection_id: string
  status: RunStatus
  total_models: number
  succeeded: number
  failed: number
  skipped: number
  started_at: string
  finished_at: string | null
  triggered_by: string | null
  created_at: string
  /** Selector the run was started with ('' = every model). */
  selector: string
  /** Named pipeline that started the run, if any. */
  pipeline_id: string | null
}

/** Response of a synchronous run: the run is finished when this arrives. */
export interface RunOutcome {
  run_id: string
  status: Exclude<RunStatus, 'running'>
  /** Set when a model failed or was skipped. */
  error?: string
}

export interface ModelRunResult {
  id: string
  run_id: string
  model_id: string
  model_name: string
  status: ResultStatus
  resolved_sql: string | null
  elapsed_ms: number
  error: string | null
  started_at: string | null
  finished_at: string | null
  created_at: string
  tests_passed: number
  tests_failed: number
  /** JSON array of TestOutcome, or null when the model has no tests. */
  test_details: string | null
}

export type TestKind = 'not_null' | 'unique' | 'accepted_values' | 'relationship' | 'expression'
export type TestSeverity = 'error' | 'warn'

/** Kind-specific settings of a data test. */
export interface TestArgs {
  /** accepted_values: allowed values, compared as text. */
  values?: string[]
  /** relationship: parent model, table in the same database, or database.table. */
  to?: string
  /** relationship: the parent's key column. */
  field?: string
  /** expression: boolean that must hold for every row. */
  expression?: string
}

export interface ModelTest {
  id: string
  model_id: string
  kind: TestKind
  /** Empty for expression tests without a column. */
  column_name: string
  /** JSON-encoded TestArgs. */
  args: string
  severity: TestSeverity
  created_at: string
  updated_at: string
}

export interface ModelTestInput {
  kind: TestKind
  column_name: string
  args: TestArgs
  severity: TestSeverity
}

export interface TestOutcome {
  test_id: string
  kind: TestKind
  column?: string
  severity: TestSeverity
  status: 'pass' | 'fail' | 'error'
  failures: number
  error?: string
  sql: string
}

export interface PlanStep {
  when: 'first build' | 'rebuild' | 'incremental run' | 'full refresh' | 'every run'
  sql: string
}

export interface CompileResult {
  resolved_sql: string
  statements: PlanStep[]
  /** Set when the SQL does not resolve (unknown $ref). */
  error?: string
}

export interface PreviewColumn {
  name: string
  type: string
}

export interface PreviewResult {
  sql: string
  meta?: PreviewColumn[]
  data?: Record<string, unknown>[]
  /** ClickHouse error, when the query failed. */
  error?: string
}

export type RunScope = 'upstream' | 'downstream' | 'only'

export interface DAGNode {
  id: string
  data: {
    name: string
    materialization: Materialization
    status: ModelStatus
    target_database: string
  }
  position: { x: number; y: number }
}

export interface DAGEdge {
  id: string
  source: string
  target: string
}

export interface ModelDAG {
  nodes: DAGNode[]
  edges: DAGEdge[]
}

/** A named, stable group of models picked by a selector, with an optional schedule. */
export interface ModelPipeline {
  id: string
  connection_id: string
  name: string
  /** dbt-style: name, +name (with upstream), name+ (with downstream), *, comma-separated. */
  selector: string
  /** 5-field cron; '' = not scheduled. */
  cron: string
  enabled: boolean
  /** IANA timezone the cron is read in. */
  timezone: string
  last_run_at: string | null
  next_run_at: string | null
  last_status: 'success' | 'error' | null
  last_error: string | null
  last_run_id: string | null
  created_by: string | null
  created_at: string
  updated_at: string
  /** Models the selector picks today, in run order. */
  model_ids: string[]
  /** Next three run times (UTC ISO) when scheduled and enabled. */
  next_runs: string[]
  /** Set when the selector no longer resolves. */
  selector_error?: string
}

export interface PipelineInput {
  name: string
  selector: string
  cron: string
  enabled?: boolean
  timezone: string
}

export interface ModelEditState {
  modelName: string
  description: string
  targetDatabase: string
  materialization: string
  sqlBody: string
  tableEngine: string
  orderBy: string
  partitionBy: string
  engineArgs: string
  incrementalStrategy: string
  uniqueKey: string
  watermarkColumn: string
}
