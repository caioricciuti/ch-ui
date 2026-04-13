import type { Node } from '@xyflow/svelte'
import type { LineageNode, LineageEdge } from '../types/governance'

const LAYER_GAP = 300
const NODE_GAP = 140
const NODE_WIDTH = 220

export interface LineageFlowNode extends Node {
  data: {
    database: string
    table: string
    nodeType: string
    columns: LineageNode['columns']
    linkedColumns: string[]
  }
}

/**
 * Assigns left-to-right layered positions to lineage nodes using BFS.
 * Returns SvelteFlow-compatible Node objects with computed positions.
 */
export function layoutLineageGraph(
  nodes: LineageNode[],
  edges: LineageEdge[],
): LineageFlowNode[] {
  if (nodes.length === 0) return []

  // Build adjacency: source → targets
  const outgoing = new Map<string, string[]>()
  const incoming = new Map<string, Set<string>>()

  // Collect all linked columns per node from column_edges
  const linkedColumnsMap = new Map<string, Set<string>>()

  for (const node of nodes) {
    outgoing.set(node.id, [])
    incoming.set(node.id, new Set())
    linkedColumnsMap.set(node.id, new Set())
  }

  for (const edge of edges) {
    const srcKey = `${edge.source_database}.${edge.source_table}`
    const tgtKey = `${edge.target_database}.${edge.target_table}`

    outgoing.get(srcKey)?.push(tgtKey)
    incoming.get(tgtKey)?.add(srcKey)

    // Track linked columns
    if (edge.column_edges) {
      for (const ce of edge.column_edges) {
        linkedColumnsMap.get(srcKey)?.add(ce.source_column)
        linkedColumnsMap.get(tgtKey)?.add(ce.target_column)
      }
    }
  }

  // BFS layering from roots (nodes with no incoming edges)
  const layers = new Map<string, number>()
  const queue: string[] = []

  for (const node of nodes) {
    const inc = incoming.get(node.id)
    if (!inc || inc.size === 0) {
      layers.set(node.id, 0)
      queue.push(node.id)
    }
  }

  // If no roots found (cycle), assign all to layer 0
  if (queue.length === 0) {
    for (const node of nodes) {
      layers.set(node.id, 0)
      queue.push(node.id)
    }
  }

  let head = 0
  while (head < queue.length) {
    const current = queue[head++]
    const currentLayer = layers.get(current) ?? 0
    for (const target of outgoing.get(current) ?? []) {
      const existingLayer = layers.get(target)
      if (existingLayer === undefined || existingLayer < currentLayer + 1) {
        layers.set(target, currentLayer + 1)
        queue.push(target)
      }
    }
  }

  // Group nodes by layer
  const layerGroups = new Map<number, LineageNode[]>()
  for (const node of nodes) {
    const layer = layers.get(node.id) ?? 0
    const group = layerGroups.get(layer) ?? []
    group.push(node)
    layerGroups.set(layer, group)
  }

  // Assign positions
  const nodeMap = new Map(nodes.map((n) => [n.id, n]))
  const flowNodes: LineageFlowNode[] = []

  for (const [layer, group] of layerGroups) {
    const x = layer * LAYER_GAP
    const totalHeight = group.length * NODE_GAP
    const startY = -totalHeight / 2

    for (let i = 0; i < group.length; i++) {
      const node = group[i]
      const linked = linkedColumnsMap.get(node.id)

      flowNodes.push({
        id: node.id,
        type: 'lineageTable',
        position: { x, y: startY + i * NODE_GAP },
        width: NODE_WIDTH,
        data: {
          database: node.database,
          table: node.table,
          nodeType: node.type,
          columns: node.columns ?? [],
          linkedColumns: linked ? [...linked] : [],
        },
      })
    }
  }

  return flowNodes
}
