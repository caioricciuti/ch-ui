import type { SinkCounters } from '../../../types/pipelines'
import type { TransformCounters } from '../../../types/transforms'

// Live per-node counters of the open pipeline, fed by the status bar's polling.
// Transform nodes read them by id; the editor clears them when the run stops.
let counters = $state<Record<string, TransformCounters>>({})

export function getTransformCounters(nodeId: string): TransformCounters | null {
  return counters[nodeId] ?? null
}

export function setTransformCounters(list: TransformCounters[] | null | undefined): void {
  const next: Record<string, TransformCounters> = {}
  for (const c of list ?? []) if (c && typeof c.node_id === 'string') next[c.node_id] = c
  counters = next
}

// Per sink counters (status `sinks`), shown on the sink nodes while running.
let sinkCounters = $state<Record<string, SinkCounters>>({})

export function getSinkCounters(nodeId: string): SinkCounters | null {
  return sinkCounters[nodeId] ?? null
}

export function setSinkCounters(list: SinkCounters[] | null | undefined): void {
  const next: Record<string, SinkCounters> = {}
  for (const c of list ?? []) if (c && typeof c.node_id === 'string') next[c.node_id] = c
  sinkCounters = next
}

/** Clears transform and sink counters. */
export function clearTransformCounters(): void {
  if (Object.keys(counters).length) counters = {}
  if (Object.keys(sinkCounters).length) sinkCounters = {}
}

// The save error the server attached to one node (PUT /graph answers 400 with node_id).
let nodeError = $state<{ nodeId: string; message: string } | null>(null)

export function getNodeError(nodeId: string): string {
  return nodeError?.nodeId === nodeId ? nodeError.message : ''
}

export function setNodeError(nodeId: string, message: string): void {
  nodeError = { nodeId, message }
}

/** Clears the error, or only when it belongs to nodeId. */
export function clearNodeError(nodeId?: string): void {
  if (nodeError && (!nodeId || nodeError.nodeId === nodeId)) nodeError = null
}
