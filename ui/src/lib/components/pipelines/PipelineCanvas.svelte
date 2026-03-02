<script lang="ts">
  import {
    SvelteFlow,
    Controls,
    Background,
    MiniMap,
    type Node,
    type Edge,
    type NodeTypes,
    type Connection,
  } from '@xyflow/svelte'
  import '@xyflow/svelte/dist/style.css'
  import SourceNode from './nodes/SourceNode.svelte'
  import SinkNode from './nodes/SinkNode.svelte'
  import { SOURCE_NODE_TYPES, SINK_NODE_TYPES, type NodeType } from '../../types/pipelines'
  import { Radio, Webhook, Database, HardDrive } from 'lucide-svelte'
  import { getTheme } from '../../stores/theme.svelte'

  interface Props {
    nodes: Node[]
    edges: Edge[]
    onNodesChange: (nodes: Node[]) => void
    onEdgesChange: (edges: Edge[]) => void
    onConnect: (connection: Connection) => void
    onNodeClick: (nodeId: string) => void
    onPaneClick: () => void
  }

  let { nodes, edges, onNodesChange, onEdgesChange, onConnect, onNodeClick, onPaneClick }: Props = $props()

  const nodeTypes: NodeTypes = {
    source_kafka: SourceNode as any,
    source_webhook: SourceNode as any,
    source_database: SourceNode as any,
    source_s3: SourceNode as any,
    sink_clickhouse: SinkNode as any,
  }

  function onDragStart(e: DragEvent, type: NodeType) {
    if (e.dataTransfer) {
      e.dataTransfer.setData('application/pipeline-node', type)
      e.dataTransfer.effectAllowed = 'move'
    }
  }

  function onDragOver(e: DragEvent) {
    e.preventDefault()
    if (e.dataTransfer) {
      e.dataTransfer.dropEffect = 'move'
    }
  }

  function onDrop(e: DragEvent) {
    e.preventDefault()
    const type = e.dataTransfer?.getData('application/pipeline-node') as NodeType
    if (!type) return

    // Find the SvelteFlow container and its viewport element
    const wrapper = e.currentTarget as HTMLElement
    const flowEl = wrapper.querySelector('.svelte-flow') as HTMLElement
    if (!flowEl) return

    const viewport = flowEl.querySelector('.svelte-flow__viewport') as HTMLElement
    if (!viewport) return

    // Parse viewport transform: translate(tx, ty) scale(zoom)
    const transform = viewport.style.transform
    const match = transform.match(/translate\(([^,]+),\s*([^)]+)\)\s*scale\(([^)]+)\)/)
    const tx = match ? parseFloat(match[1]) : 0
    const ty = match ? parseFloat(match[2]) : 0
    const zoom = match ? parseFloat(match[3]) : 1

    // Convert screen coordinates to flow coordinates
    const rect = flowEl.getBoundingClientRect()
    const position = {
      x: (e.clientX - rect.left - tx) / zoom,
      y: (e.clientY - rect.top - ty) / zoom,
    }

    const allTypes = [...SOURCE_NODE_TYPES, ...SINK_NODE_TYPES]
    const meta = allTypes.find((t) => t.type === type)
    const label = meta?.label || type

    const newNode: Node = {
      id: `node-${Date.now()}`,
      type,
      position,
      data: {
        label,
        node_type: type,
        config: {},
      },
    }

    onNodesChange([...nodes, newNode])
  }

  const sourceIcons: Record<string, typeof Radio> = {
    source_kafka: Radio,
    source_webhook: Webhook,
    source_database: Database,
    source_s3: HardDrive,
  }
</script>

<div class="flex flex-1 min-h-0">
  <!-- Node Palette -->
  <div class="w-44 border-r border-gray-200 dark:border-gray-800 bg-gray-50 dark:bg-gray-950 p-2 overflow-auto">
    <p class="text-[10px] font-semibold text-gray-500 dark:text-gray-400 uppercase tracking-wider mb-2 px-1">
      Sources
    </p>
    {#each SOURCE_NODE_TYPES as source}
      {@const Icon = sourceIcons[source.type] || Radio}
      <div
        class="flex items-center gap-2 px-2 py-1.5 mb-1 rounded-lg border border-gray-200 dark:border-gray-700 bg-white dark:bg-gray-900 cursor-grab hover:border-orange-300 dark:hover:border-orange-700 transition-colors text-xs"
        draggable="true"
        ondragstart={(e: DragEvent) => onDragStart(e, source.type)}
        role="button"
        tabindex={0}
      >
        <Icon size={14} class="text-gray-500 shrink-0" />
        <div class="min-w-0">
          <div class="font-medium text-gray-700 dark:text-gray-300 text-xs">{source.label}</div>
          <div class="text-[9px] text-gray-400 dark:text-gray-500 truncate">{source.description}</div>
        </div>
      </div>
    {/each}

    <p class="text-[10px] font-semibold text-gray-500 dark:text-gray-400 uppercase tracking-wider mt-3 mb-2 px-1">
      Sinks
    </p>
    {#each SINK_NODE_TYPES as sink}
      <div
        class="flex items-center gap-2 px-2 py-1.5 mb-1 rounded-lg border border-orange-200 dark:border-orange-800 bg-orange-50 dark:bg-orange-900/20 cursor-grab hover:border-orange-400 dark:hover:border-orange-600 transition-colors text-xs"
        draggable="true"
        ondragstart={(e: DragEvent) => onDragStart(e, sink.type)}
        role="button"
        tabindex={0}
      >
        <Database size={14} class="text-orange-500 shrink-0" />
        <div class="min-w-0">
          <div class="font-medium text-gray-700 dark:text-gray-300 text-xs">{sink.label}</div>
          <div class="text-[9px] text-gray-400 dark:text-gray-500 truncate">{sink.description}</div>
        </div>
      </div>
    {/each}
  </div>

  <!-- Flow Canvas -->
  <div
    class="flex-1 min-h-0"
    ondragover={onDragOver}
    ondrop={onDrop}
    role="presentation"
  >
    <SvelteFlow
      {nodes}
      {edges}
      {nodeTypes}
      fitView
      colorMode={getTheme()}
      proOptions={{ hideAttribution: true }}
      onconnect={onConnect}
      onnodeclick={({ node }) => {
        if (node?.id) onNodeClick(node.id)
      }}
      onpaneclick={() => onPaneClick()}
      onnodedragstop={({ nodes: draggedNodes }) => {
        // Update node positions after drag
        let updated = [...nodes]
        for (const dn of draggedNodes) {
          const idx = updated.findIndex((n) => n.id === dn.id)
          if (idx >= 0) {
            updated[idx] = { ...updated[idx], position: { ...dn.position } }
          }
        }
        onNodesChange(updated)
      }}
      ondelete={({ nodes: deletedNodes, edges: deletedEdges }) => {
        if (deletedNodes.length > 0) {
          const deleteIds = new Set(deletedNodes.map((n) => n.id))
          onNodesChange(nodes.filter((n) => !deleteIds.has(n.id)))
        }
        if (deletedEdges.length > 0) {
          const deleteIds = new Set(deletedEdges.map((e) => e.id))
          onEdgesChange(edges.filter((e) => !deleteIds.has(e.id)))
        }
      }}
      defaultEdgeOptions={{ animated: true, style: 'stroke: #f97316; stroke-width: 2;' }}
    >
      <Controls />
      <Background gap={16} />
      <MiniMap />
    </SvelteFlow>
  </div>
</div>

<style>
  /* Canvas background */
  :global(.svelte-flow) {
    background-color: #fafafa;
  }
  :global(.svelte-flow.dark) {
    background-color: #0a0a0a;
  }

  /* Edge styling */
  :global(.svelte-flow .svelte-flow__edge-path) {
    stroke: #f97316;
    stroke-width: 2;
  }

  /* Handle sizing */
  :global(.svelte-flow .svelte-flow__handle) {
    width: 10px;
    height: 10px;
  }

  /* Controls — dark mode */
  :global(.svelte-flow.dark .svelte-flow__controls) {
    background: #1a1a1a;
    border: 1px solid #333;
    border-radius: 8px;
    box-shadow: 0 2px 8px rgba(0, 0, 0, 0.4);
  }
  :global(.svelte-flow.dark .svelte-flow__controls-button) {
    background: #1a1a1a;
    border-color: #333;
    fill: #a3a3a3;
    color: #a3a3a3;
  }
  :global(.svelte-flow.dark .svelte-flow__controls-button:hover) {
    background: #2a2a2a;
    fill: #f97316;
    color: #f97316;
  }

  /* Controls — light mode */
  :global(.svelte-flow .svelte-flow__controls) {
    border-radius: 8px;
    box-shadow: 0 1px 4px rgba(0, 0, 0, 0.1);
  }

  /* MiniMap — dark mode */
  :global(.svelte-flow.dark .svelte-flow__minimap) {
    background: #141414;
    border: 1px solid #333;
    border-radius: 8px;
  }

  /* MiniMap — light mode */
  :global(.svelte-flow .svelte-flow__minimap) {
    border-radius: 8px;
    box-shadow: 0 1px 4px rgba(0, 0, 0, 0.1);
  }

  /* Background dots */
  :global(.svelte-flow .svelte-flow__background) {
    --xy-background-pattern-dots-color-default: #e5e5e5;
  }
  :global(.svelte-flow.dark .svelte-flow__background) {
    --xy-background-pattern-dots-color-default: #7e7d7d;
  }
</style>
