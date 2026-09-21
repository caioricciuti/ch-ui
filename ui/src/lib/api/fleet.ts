// SPDX-License-Identifier: BUSL-1.1
import { apiGet } from './client'
import type { NodeSample } from './clusterHealth'

export interface FleetConnection {
  id: string
  name: string
  online: boolean
  monitoring_enabled: boolean
  status: 'healthy' | 'warning' | 'critical' | 'offline' | 'disabled' | 'missing' | 'stale'
  captured_at?: string
  stale_after_seconds: number
  cluster: string
  nodes: NodeSample[]
  max_replication_delay: number
  replication_queue: number
  readonly_replicas: number
  parts_pressure_pct: number
  pending_mutations: number
  long_queries: number
  open_incidents: number
  regressions?: number
}

export interface FleetResult { captured_at: string; connections: FleetConnection[] }

export function fetchFleet(): Promise<FleetResult> { return apiGet('/api/fleet/') }
