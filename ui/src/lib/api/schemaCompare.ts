// SPDX-License-Identifier: BUSL-1.1
import { apiGet, apiPost } from './client'

export interface SchemaEndpoint {
  connection_id: string
  database: string
  username?: string
  password?: string
}

export interface SchemaConnection { id: string; name: string; online: boolean; uses_session: boolean }
export interface SchemaDifference { table: string; kind: 'changed' | 'source_only' | 'target_only'; field: string; source: string; target: string; review_sql: string }
export interface SchemaComparison {
  source: SchemaEndpoint
  target: SchemaEndpoint
  captured_at: string
  scope: string
  result: {
    source_tables: number
    target_tables: number
    matching_tables: number
    differences: SchemaDifference[]
    review_sql: string
  }
}

export function fetchSchemaConnections(): Promise<{ connections: SchemaConnection[] }> { return apiGet('/api/schema-compare/connections') }
export function compareSchemas(source: SchemaEndpoint, target: SchemaEndpoint): Promise<SchemaComparison> { return apiPost('/api/schema-compare/compare', { source, target }) }
