export type Materialization = 'view' | 'table'
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
  status: ModelStatus
  last_error: string | null
  last_run_at: string | null
  created_by: string | null
  created_at: string
  updated_at: string
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
}

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

export interface ValidationError {
  model_id?: string
  model_name?: string
  error: string
}

export interface ValidationResult {
  valid: boolean
  errors: ValidationError[]
}
