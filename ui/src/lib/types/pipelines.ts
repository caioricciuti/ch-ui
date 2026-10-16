import { defaultTransformConfig, isTransformType, type TransformCounters, type TransformNodeType } from './transforms'

export type PipelineStatus = 'draft' | 'stopped' | 'starting' | 'running' | 'error' | 'stopping'

export type NodeType =
  | 'source_kafka'
  | 'source_webhook'
  | 'source_database'
  | 'source_s3'
  | 'source_ingest'
  | TransformNodeType
  | 'sink_clickhouse'

export interface Pipeline {
  id: string
  name: string
  description: string | null
  connection_id: string
  status: PipelineStatus
  config: string
  created_by: string | null
  last_started_at: string | null
  last_stopped_at: string | null
  last_error: string | null
  created_at: string
  updated_at: string
  /** Node type of the pipeline's source, in list responses. */
  source_type?: NodeType | null
  /** Signal of an ingest source, in list responses. */
  signal?: string | null
}

export interface PipelineNode {
  id: string
  pipeline_id: string
  node_type: NodeType
  label: string
  position_x: number
  position_y: number
  config_encrypted: string
  created_at: string
  updated_at: string
}

export interface PipelineEdge {
  id: string
  pipeline_id: string
  source_node_id: string
  target_node_id: string
  source_handle: string | null
  target_handle: string | null
  created_at: string
}

export interface PipelineRun {
  id: string
  pipeline_id: string
  status: string
  started_at: string
  finished_at: string | null
  rows_ingested: number
  bytes_ingested: number
  errors_count: number
  last_error: string | null
  metrics_json: string
  created_at: string
}

export interface PipelineRunLog {
  id: string
  run_id: string
  level: string
  message: string
  created_at: string
}

export interface PipelineStatusInfo {
  pipeline_id: string
  status: PipelineStatus
  last_error: string | null
  rows_ingested?: number
  bytes_ingested?: number
  batches_sent?: number
  errors_count?: number
  last_batch_at?: string
  /** Rows sent to the dead-letter table, across runs. */
  dlq_rows?: number
  dlq_database?: string
  dlq_table?: string
  /** Per transform node counters since start, while running. */
  transforms?: TransformCounters[] | null
  /** Per sink counters; the totals above are their sums. */
  sinks?: SinkCounters[] | null
}

export interface SinkCounters {
  node_id: string
  rows: number
  batches: number
  last_write_at?: string | null
  errors: number
}

/** Most sinks one pipeline may have. */
export const MAX_SINKS = 8

/** A source position saved after a written batch. */
export interface PipelineCheckpoint {
  source_key: string
  position: string
  updated_at: string
}

export interface SinkCheck {
  /** Set when the pipeline has several sinks; the name then starts with the sink label. */
  node_id?: string
  name: string
  status: 'ok' | 'warn' | 'fail'
  detail: string
}

export interface SchemaColumn {
  name: string
  type: string
}

export interface InferredSchema {
  columns: SchemaColumn[]
  order_by: string
  partition_by: string
}

export interface PipelineTestResult {
  source_type: NodeType
  records: Record<string, unknown>[]
  note: string
  source_error?: string
  sink_checks: SinkCheck[]
  schema?: InferredSchema
  schema_error?: string
}

/** Secrets come back from the API as this mask; sending it back keeps the stored value. */
export const SECRET_MASK = '***'

export interface PipelineGraph {
  nodes: PipelineNode[]
  edges: PipelineEdge[]
}

export interface ConnectorFieldDef {
  key: string
  label: string
  type: 'text' | 'password' | 'number' | 'select' | 'textarea' | 'toggle' | 'info'
  placeholder?: string
  required?: boolean
  default?: unknown
  options?: { value: string; label: string }[]
  help?: string
  /** Smallest accepted value for number fields. */
  min?: number
  /** Largest accepted value for number fields. */
  max?: number
  /** Number fields that accept fractions (otherwise whole numbers only). */
  decimal?: boolean
}

export const SOURCE_NODE_TYPES: { type: NodeType; label: string; description: string }[] = [
  { type: 'source_kafka', label: 'Kafka', description: 'Stream from Kafka topic' },
  { type: 'source_webhook', label: 'Webhook', description: 'Receive HTTP POST events' },
  { type: 'source_database', label: 'Database', description: 'Poll from PostgreSQL, MySQL, or SQLite' },
  { type: 'source_s3', label: 'S3', description: 'Read files from S3-compatible storage' },
  { type: 'source_ingest', label: 'Ingest (OTLP, JSON)', description: 'Receive OTLP logs, traces or JSON events' },
]

export const SINK_NODE_TYPES: { type: NodeType; label: string; description: string }[] = [
  { type: 'sink_clickhouse', label: 'ClickHouse', description: 'Insert into ClickHouse table' },
]

const STALL_HELP = 'Raise an ingest.stalled alert when a pipeline that received data gets nothing for this many minutes. 0 turns it off.'

export const CONNECTOR_FIELDS: Record<NodeType, ConnectorFieldDef[]> = {
  source_kafka: [
    { key: 'brokers', label: 'Brokers', type: 'text', placeholder: 'broker1:9092,broker2:9092', required: true, help: 'Comma-separated list of Kafka broker addresses' },
    { key: 'topic', label: 'Topic', type: 'text', required: true },
    { key: 'consumer_group', label: 'Consumer Group', type: 'text', required: true, placeholder: 'ch-ui-<pipeline id>', help: 'New Kafka nodes get ch-ui-<pipeline id>, so each pipeline keeps its own offsets' },
    { key: 'sasl_mechanism', label: 'SASL Mechanism', type: 'select', options: [
      { value: '', label: 'None' },
      { value: 'PLAIN', label: 'PLAIN' },
      { value: 'SCRAM-SHA-256', label: 'SCRAM-SHA-256' },
      { value: 'SCRAM-SHA-512', label: 'SCRAM-SHA-512' },
    ], default: '' },
    { key: 'sasl_username', label: 'SASL Username', type: 'text' },
    { key: 'sasl_password', label: 'SASL Password', type: 'password' },
    { key: 'use_tls', label: 'Enable TLS', type: 'toggle', default: false },
    { key: 'batch_size', label: 'Batch Size', type: 'number', default: 1000, min: 1, help: 'Records per batch before flushing to ClickHouse' },
    { key: 'batch_timeout_ms', label: 'Batch Timeout (ms)', type: 'number', default: 5000, min: 100 },
  ],
  source_webhook: [
    { key: 'webhook_url', label: 'Webhook URL', type: 'info', help: 'POST JSON data to this URL. Include Authorization: Bearer <token> header if auth token is set.' },
    { key: 'auth_enabled', label: 'Require Authentication', type: 'toggle', default: false, help: 'When enabled, a Bearer token is generated. Include it in the Authorization header of requests. On by default for new webhook nodes.' },
    { key: 'batch_size', label: 'Batch Size', type: 'number', default: 100, min: 1 },
    { key: 'batch_timeout_ms', label: 'Batch Timeout (ms)', type: 'number', default: 2000, min: 100 },
    { key: 'stall_after_minutes', label: 'Stall Alert (minutes)', type: 'number', default: 15, min: 0, help: STALL_HELP },
  ],
  source_database: [
    { key: 'db_type', label: 'Database Type', type: 'select', required: true, options: [
      { value: 'postgres', label: 'PostgreSQL' },
      { value: 'mysql', label: 'MySQL' },
      { value: 'sqlite', label: 'SQLite (admins only)' },
    ], default: 'postgres' },
    { key: 'connection_string', label: 'Connection String', type: 'password', required: true, placeholder: 'postgres://user:pass@host/db or /path/to/file.db', help: 'Stored encrypted. For SQLite, a file path like /data/my.db (admins only, not inside the CH-UI data directory)' },
    { key: 'query', label: 'SQL Query', type: 'textarea', required: true, placeholder: 'SELECT * FROM events WHERE id > COALESCE($1, 0) ORDER BY id', help: 'With a watermark column, $1 (? for MySQL and SQLite) is the last written watermark, NULL before the first batch. Order by the watermark column.' },
    { key: 'poll_interval', label: 'Poll Interval (seconds)', type: 'number', default: 60, min: 1, help: 'Seconds between each poll' },
    { key: 'watermark_column', label: 'Watermark Column', type: 'text', help: 'Column for incremental polling (e.g. id or created_at)' },
    { key: 'batch_size', label: 'Batch Size', type: 'number', default: 1000, min: 1 },
  ],
  source_s3: [
    { key: 'endpoint', label: 'S3 Endpoint', type: 'text', placeholder: 'https://minio.example.com:9000', help: 'Host and port, optionally with http:// or https:// (no path). http:// turns TLS off. Leave empty for AWS S3.' },
    { key: 'region', label: 'Region', type: 'text', default: 'us-east-1' },
    { key: 'bucket', label: 'Bucket', type: 'text', required: true },
    { key: 'prefix', label: 'Key Prefix', type: 'text', placeholder: 'data/events/' },
    { key: 'access_key', label: 'Access Key ID', type: 'password', required: true },
    { key: 'secret_key', label: 'Secret Access Key', type: 'password', required: true },
    { key: 'format', label: 'File Format', type: 'select', required: true, options: [
      { value: 'json', label: 'JSON (array or one object per line)' },
      { value: 'ndjson', label: 'JSON Lines (NDJSON)' },
      { value: 'csv', label: 'CSV' },
    ], default: 'json' },
    { key: 'poll_interval', label: 'Poll Interval (seconds)', type: 'number', default: 300, min: 1, help: 'Seconds between each poll' },
    { key: 'batch_size', label: 'Batch Size', type: 'number', default: 1000, min: 1 },
  ],
  source_ingest: [
    { key: 'signal', label: 'Signal', type: 'select', required: true, options: [
      { value: 'logs', label: 'Logs (OTLP)' },
      { value: 'traces', label: 'Traces (OTLP)' },
      { value: 'events', label: 'Events (JSON)' },
    ], default: 'logs' },
    { key: 'ttl_days', label: 'TTL (days)', type: 'number', default: 30, min: 0, help: 'Applied when the sink creates the table. 0 keeps data forever.' },
    { key: 'rate_limit_rps', label: 'Rate Limit (requests/s)', type: 'number', default: 0, min: 0, help: 'Per key. 0 uses the default of 200 requests per second.' },
    { key: 'daily_quota_mb', label: 'Daily Quota (MB)', type: 'number', default: 0, min: 0, help: 'Decompressed request bytes per day, reset at 00:00 UTC. 0 is unlimited.' },
    { key: 'stall_after_minutes', label: 'Stall Alert (minutes)', type: 'number', default: 15, min: 0, help: STALL_HELP },
  ],
  // Transforms have their own typed panels (components/pipelines/transforms).
  transform_parse: [],
  transform_map: [],
  transform_filter: [],
  transform_sample: [],
  transform_redact: [],
  transform_route: [],
  sink_clickhouse: [
    { key: 'database', label: 'Target Database', type: 'text', required: true, default: 'default' },
    { key: 'table', label: 'Target Table', type: 'text', required: true },
    { key: 'create_table', label: 'Create Table If Not Exists', type: 'toggle', default: false },
    { key: 'create_table_engine', label: 'Table Engine', type: 'select', options: [
      { value: 'MergeTree', label: 'MergeTree' },
      { value: 'ReplacingMergeTree', label: 'ReplacingMergeTree' },
      { value: 'SummingMergeTree', label: 'SummingMergeTree' },
      { value: 'AggregatingMergeTree', label: 'AggregatingMergeTree' },
    ], default: 'MergeTree', help: 'Only used when "Create Table" is enabled' },
    { key: 'create_table_order_by', label: 'ORDER BY', type: 'text', placeholder: 'tuple()', help: 'One ClickHouse expression, e.g. (user_id, ts)' },
    { key: 'create_table_partition_by', label: 'PARTITION BY', type: 'text', placeholder: 'toYYYYMM(ts)', help: 'Optional. One ClickHouse expression' },
    { key: 'dead_letter', label: 'Dead-letter Table', type: 'toggle', default: true, help: 'Rows ClickHouse refuses (parse or type errors) are isolated and written to <table>_dlq; the rest of the batch is inserted. Off: the batch is retried until it is fixed.' },
    { key: 'max_error_ratio', label: 'Max Error Ratio', type: 'number', default: 0, min: 0, max: 1, decimal: true, help: 'Optional, 0 to 1. Lets ClickHouse skip up to this share of unparsable rows per insert (input_format_allow_errors_ratio). Skipped rows are dropped, not dead-lettered. 0 turns it off.' },
  ],
}

/** A random hex token for webhook authentication. */
export function generateToken(): string {
  const bytes = new Uint8Array(24)
  crypto.getRandomValues(bytes)
  return Array.from(bytes, (b) => b.toString(16).padStart(2, '0')).join('')
}

/** Starting config for a node dropped on the canvas. signal is the ingest source's, if any. */
export function newNodeConfig(type: NodeType, pipelineId: string, signal?: string | null): Record<string, unknown> {
  if (isTransformType(type)) return defaultTransformConfig(type, signal)
  switch (type) {
    case 'source_webhook':
      return { auth_enabled: true, auth_token: generateToken() }
    case 'source_kafka':
      return { consumer_group: `ch-ui-${pipelineId}` }
    case 'source_ingest':
      return { signal: 'logs', ttl_days: 30, rate_limit_rps: 0, daily_quota_mb: 0 }
    default:
      return {}
  }
}

/** Deduplication window the sink sets on tables it creates with a non-replicated engine. */
export const DEDUP_WINDOW = 1000

/** Backtick-quotes a ClickHouse identifier, as the server does. */
export function quoteIdent(name: string): string {
  return '`' + name.replace(/\\/g, '\\\\').replace(/`/g, '\\`') + '`'
}

/** The CREATE TABLE the sink runs for a saved column list. */
export function createTableDDL(opts: {
  database: string
  table: string
  columns: SchemaColumn[]
  engine?: string
  orderBy?: string
  partitionBy?: string
}): string {
  const cols = opts.columns.map((c) => `  ${quoteIdent(c.name)} ${c.type}`).join(',\n')
  const engine = (opts.engine || 'MergeTree').replace(/\(\)$/, '')
  let ddl = `CREATE TABLE IF NOT EXISTS ${quoteIdent(opts.database || 'default')}.${quoteIdent(opts.table || 'table')} (\n${cols}\n) ENGINE = ${engine}`
  if (opts.partitionBy?.trim()) ddl += `\nPARTITION BY ${opts.partitionBy.trim()}`
  ddl += `\nORDER BY ${opts.orderBy?.trim() || 'tuple()'}`
  // Replicated and Shared engines deduplicate inserts on their own; the others need a window for the sink's dedup tokens.
  if (!engine.startsWith('Replicated') && !engine.startsWith('Shared')) ddl += `\nSETTINGS non_replicated_deduplication_window = ${DEDUP_WINDOW}`
  return ddl
}

/** Reads create_table_columns from a sink config. */
export function schemaColumnsOf(config: Record<string, unknown>): SchemaColumn[] {
  const raw = config['create_table_columns']
  if (!Array.isArray(raw)) return []
  const out: SchemaColumn[] = []
  for (const item of raw) {
    if (item && typeof item === 'object') {
      const rec = item as Record<string, unknown>
      if (typeof rec.name === 'string' && typeof rec.type === 'string') out.push({ name: rec.name, type: rec.type })
    }
  }
  return out
}
