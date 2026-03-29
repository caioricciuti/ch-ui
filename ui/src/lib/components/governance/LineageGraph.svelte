<script lang="ts">
  import {
    SvelteFlow,
    Controls,
    Background,
    MiniMap,
    type Edge,
    type NodeTypes,
  } from '@xyflow/svelte'
  import '@xyflow/svelte/dist/style.css'
  import { getTheme } from '../../stores/theme.svelte'
  import type { LineageGraph as LineageGraphType, LineageEdge } from '../../types/governance'
  import { layoutLineageGraph } from '../../utils/lineage-layout'
  import LineageTableNode from './LineageTableNode.svelte'

  interface Props {
    graph: LineageGraphType
    searchFilter?: string
    onedgeclick?: (edge: LineageEdge) => void
    onnodeclick?: (nodeId: string) => void
  }

  let { graph, searchFilter = '', onedgeclick, onnodeclick }: Props = $props()

  const nodeTypes: NodeTypes = {
    lineageTable: LineageTableNode as unknown as NodeTypes['lineageTable'],
  }

  const filteredGraph = $derived.by(() => {
    if (!searchFilter.trim()) return graph
    const q = searchFilter.toLowerCase()
    const matchingNodes = graph.nodes.filter(
      (n) =>
        n.table.toLowerCase().includes(q) ||
        n.database.toLowerCase().includes(q) ||
        n.id.toLowerCase().includes(q),
    )
    const matchingIds = new Set(matchingNodes.map((n) => n.id))
    // Also include directly connected nodes
    for (const edge of graph.edges) {
      const srcKey = `${edge.source_database}.${edge.source_table}`
      const tgtKey = `${edge.target_database}.${edge.target_table}`
      if (matchingIds.has(srcKey)) matchingIds.add(tgtKey)
      if (matchingIds.has(tgtKey)) matchingIds.add(srcKey)
    }
    const nodes = graph.nodes.filter((n) => matchingIds.has(n.id))
    const nodeIdSet = new Set(nodes.map((n) => n.id))
    const edges = graph.edges.filter((e) => {
      const srcKey = `${e.source_database}.${e.source_table}`
      const tgtKey = `${e.target_database}.${e.target_table}`
      return nodeIdSet.has(srcKey) && nodeIdSet.has(tgtKey)
    })
    return { nodes, edges }
  })

  const flowNodes = $derived(layoutLineageGraph(filteredGraph.nodes, filteredGraph.edges))

  const flowEdges = $derived<Edge[]>(
    filteredGraph.edges.map((e) => {
      const isCreate = e.edge_type === 'create_as_select'
      return {
        id: e.id,
        source: `${e.source_database}.${e.source_table}`,
        target: `${e.target_database}.${e.target_table}`,
        animated: true,
        style: isCreate
          ? 'stroke: #3b82f6; stroke-width: 2px; stroke-dasharray: 5 3;'
          : 'stroke: #f97316; stroke-width: 2px;',
      }
    }),
  )

  // Map edge IDs to LineageEdge for click handler
  const edgeMap = $derived(new Map(filteredGraph.edges.map((e) => [e.id, e])))

  function handleEdgeClick(event: { edge: Edge }) {
    const lineageEdge = edgeMap.get(event.edge.id)
    if (lineageEdge && onedgeclick) onedgeclick(lineageEdge)
  }

  function handleNodeClick(event: { node: { id: string } }) {
    if (onnodeclick) onnodeclick(event.node.id)
  }
</script>

<SvelteFlow
  nodes={flowNodes}
  edges={flowEdges}
  {nodeTypes}
  fitView
  colorMode={getTheme()}
  proOptions={{ hideAttribution: true }}
  onedgeclick={handleEdgeClick}
  onnodeclick={handleNodeClick}
  defaultEdgeOptions={{ animated: true }}
>
  <Controls />
  <Background gap={16} />
  <MiniMap />
</SvelteFlow>
