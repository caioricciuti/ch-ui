export type PipelineStatus = 'draft' | 'stopped' | 'starting' | 'running' | 'error' | 'stopping'

export type NodeType =
  | 'source_kafka'
  | 'source_webhook'
  | 'source_database'
  | 'source_s3'
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

export interface PipelineGraph {
  nodes: PipelineNode[]
  edges: PipelineEdge[]
}

export interface PipelineRun {
  id: string
  pipeline_id: string
  status: 'running' | 'success' | 'error' | 'stopped'
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
  level: 'debug' | 'info' | 'warn' | 'error'
  message: string
  created_at: string
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
}

export const SOURCE_NODE_TYPES: { type: NodeType; label: string; description: string }[] = [
  { type: 'source_kafka', label: 'Kafka', description: 'Stream from Kafka topic' },
  { type: 'source_webhook', label: 'Webhook', description: 'Receive HTTP POST events' },
  { type: 'source_database', label: 'Database', description: 'Poll from PostgreSQL, MySQL, or SQLite' },
  { type: 'source_s3', label: 'S3', description: 'Read files from S3-compatible storage' },
]

export const SINK_NODE_TYPES: { type: NodeType; label: string; description: string }[] = [
  { type: 'sink_clickhouse', label: 'ClickHouse', description: 'Insert into ClickHouse table' },
]

export const CONNECTOR_FIELDS: Record<NodeType, ConnectorFieldDef[]> = {
  source_kafka: [
    { key: 'brokers', label: 'Brokers', type: 'text', placeholder: 'broker1:9092,broker2:9092', required: true, help: 'Comma-separated list of Kafka broker addresses' },
    { key: 'topic', label: 'Topic', type: 'text', required: true },
    { key: 'consumer_group', label: 'Consumer Group', type: 'text', required: true, default: 'ch-ui-pipeline' },
    { key: 'sasl_mechanism', label: 'SASL Mechanism', type: 'select', options: [
      { value: '', label: 'None' },
      { value: 'PLAIN', label: 'PLAIN' },
      { value: 'SCRAM-SHA-256', label: 'SCRAM-SHA-256' },
      { value: 'SCRAM-SHA-512', label: 'SCRAM-SHA-512' },
    ], default: '' },
    { key: 'sasl_username', label: 'SASL Username', type: 'text' },
    { key: 'sasl_password', label: 'SASL Password', type: 'password' },
    { key: 'use_tls', label: 'Enable TLS', type: 'toggle', default: false },
    { key: 'batch_size', label: 'Batch Size', type: 'number', default: 1000, help: 'Records per batch before flushing to ClickHouse' },
    { key: 'batch_timeout_ms', label: 'Batch Timeout (ms)', type: 'number', default: 5000 },
  ],
  source_webhook: [
    { key: 'webhook_url', label: 'Webhook URL', type: 'info', help: 'POST JSON data to this URL. Include Authorization: Bearer <token> header if auth token is set.' },
    { key: 'auth_enabled', label: 'Require Authentication', type: 'toggle', default: false, help: 'When enabled, a Bearer token is generated. Include it in the Authorization header of requests.' },
    { key: 'batch_size', label: 'Batch Size', type: 'number', default: 100 },
    { key: 'batch_timeout_ms', label: 'Batch Timeout (ms)', type: 'number', default: 2000 },
  ],
  source_database: [
    { key: 'db_type', label: 'Database Type', type: 'select', required: true, options: [
      { value: 'postgres', label: 'PostgreSQL' },
      { value: 'mysql', label: 'MySQL' },
      { value: 'sqlite', label: 'SQLite' },
    ] },
    { key: 'connection_string', label: 'Connection String', type: 'password', required: true, placeholder: 'postgres://user:pass@host/db or /path/to/file.db', help: 'For SQLite, use a file path like /data/my.db' },
    { key: 'query', label: 'SQL Query', type: 'textarea', required: true, placeholder: 'SELECT * FROM events WHERE id > $1', help: 'Use $1 placeholder with watermark column for incremental polling' },
    { key: 'poll_interval', label: 'Poll Interval (seconds)', type: 'number', default: 60, help: 'Seconds between each poll' },
    { key: 'watermark_column', label: 'Watermark Column', type: 'text', help: 'Column for incremental polling (e.g. id or created_at)' },
    { key: 'batch_size', label: 'Batch Size', type: 'number', default: 1000 },
  ],
  source_s3: [
    { key: 'endpoint', label: 'S3 Endpoint', type: 'text', placeholder: 'https://s3.amazonaws.com', help: 'S3-compatible endpoint URL. Leave empty for AWS S3.' },
    { key: 'region', label: 'Region', type: 'text', default: 'us-east-1' },
    { key: 'bucket', label: 'Bucket', type: 'text', required: true },
    { key: 'prefix', label: 'Key Prefix', type: 'text', placeholder: 'data/events/' },
    { key: 'access_key', label: 'Access Key ID', type: 'password', required: true },
    { key: 'secret_key', label: 'Secret Access Key', type: 'password', required: true },
    { key: 'format', label: 'File Format', type: 'select', required: true, options: [
      { value: 'json', label: 'JSON' },
      { value: 'ndjson', label: 'JSON Lines (NDJSON)' },
      { value: 'csv', label: 'CSV' },
    ], default: 'json' },
    { key: 'poll_interval', label: 'Poll Interval (seconds)', type: 'number', default: 300, help: 'Seconds between each poll' },
    { key: 'batch_size', label: 'Batch Size', type: 'number', default: 1000 },
  ],
  sink_clickhouse: [
    { key: 'database', label: 'Target Database', type: 'text', required: true, default: 'default' },
    { key: 'table', label: 'Target Table', type: 'text', required: true },
    { key: 'create_table', label: 'Create Table If Not Exists', type: 'toggle', default: false },
    { key: 'create_table_engine', label: 'Table Engine', type: 'select', options: [
      { value: 'MergeTree', label: 'MergeTree' },
      { value: 'ReplacingMergeTree', label: 'ReplacingMergeTree' },
      { value: 'SummingMergeTree', label: 'SummingMergeTree' },
    ], default: 'MergeTree', help: 'Only used when "Create Table" is enabled' },
    { key: 'create_table_order_by', label: 'ORDER BY', type: 'text', placeholder: 'tuple()', help: 'ClickHouse ORDER BY clause' },
  ],
}
