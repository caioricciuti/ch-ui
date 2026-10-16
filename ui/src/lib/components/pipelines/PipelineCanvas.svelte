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
  import TransformNode from './nodes/TransformNode.svelte'
  import { SOURCE_NODE_TYPES, SINK_NODE_TYPES, newNodeConfig, type NodeType } from '../../types/pipelines'
  import { TRANSFORM_NODE_TYPES, isTransformType } from '../../types/transforms'
  import { bridgeDeleted, connectionProblem, insertIntoEdge, makeRoom } from './transforms/graph'
  import { Radio, Webhook, Database, HardDrive, Inbox, Braces, ArrowLeftRight, Funnel, Percent, EyeOff } from 'lucide-svelte'
  import { getTheme } from '../../stores/theme.svelte'
  import { isProActive } from '../../stores/license.svelte'
  import { error as toastError } from '../../stores/toast.svelte'

  interface Props {
    pipelineId: string
    nodes: Node[]
    edges: Edge[]
    onNodesChange: (nodes: Node[]) => void
    onEdgesChange: (edges: Edge[]) => void
    onConnect: (connection: Connection) => void
    onNodeClick: (nodeId: string) => void
    onPaneClick: () => void
    /** Signal of the ingest source, if any, for new transform defaults. */
    signal?: string | null
  }

  let { pipelineId, nodes, edges, onNodesChange, onEdgesChange, onConnect, onNodeClick, onPaneClick, signal = null }: Props = $props()

  const transformNode = TransformNode as unknown as NodeTypes[string]

  const nodeTypes: NodeTypes = {
    source_kafka: SourceNode as unknown as NodeTypes[string],
    source_webhook: SourceNode as unknown as NodeTypes[string],
    source_database: SourceNode as unknown as NodeTypes[string],
    source_s3: SourceNode as unknown as NodeTypes[string],
    source_ingest: SourceNode as unknown as NodeTypes[string],
    transform_parse: transformNode,
    transform_map: transformNode,
    transform_filter: transformNode,
    transform_sample: transformNode,
    transform_redact: transformNode,
    sink_clickhouse: SinkNode as unknown as NodeTypes[string],
  }

  let idSeq = 0
  /** Unique ids for nodes and edges made in this session. */
  function newId(prefix: string): string {
    idSeq++
    return `${prefix}-${Date.now()}-${idSeq}`
  }

  /** The edge under a screen point (the wide invisible hit path counts). */
  function edgeAt(x: number, y: number): string | null {
    for (const el of document.elementsFromPoint(x, y)) {
      const g = el.closest('.svelte-flow__edge')
      const id = g?.getAttribute('data-id')
      if (id) return id
    }
    return null
  }

  /**
   * The node under the last pointerdown. A click that starts on a node can
   * reach the pane instead (the node re-renders between down and up, so the
   * click lands on their common ancestor); it still means "open this node".
   */
  let pressedNode: { id: string; at: number } | null = null

  function onPointerDownCapture(e: PointerEvent) {
    const id = (e.target as Element | null)?.closest('.svelte-flow__node')?.getAttribute('data-id')
    pressedNode = id ? { id, at: Date.now() } : null
  }

  function handlePaneClick() {
    const pressed = pressedNode
    pressedNode = null
    if (pressed && Date.now() - pressed.at < 1000 && nodes.some((n) => n.id === pressed.id)) {
      onNodeClick(pressed.id)
      return
    }
    onPaneClick()
  }

  function refuseConnection(connection: Connection): Connection | false {
    const problem = connectionProblem(nodes, edges, connection.source, connection.target)
    if (problem) {
      toastError(problem)
      return false
    }
    return connection
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

    const allTypes = [...SOURCE_NODE_TYPES, ...TRANSFORM_NODE_TYPES, ...SINK_NODE_TYPES]
    const meta = allTypes.find((t) => t.type === type)
    const label = meta?.label || type

    const newNode: Node = {
      id: newId('node'),
      type,
      position,
      data: {
        label,
        node_type: type,
        config: newNodeConfig(type, pipelineId, signal),
      },
    }

    // A transform dropped onto an edge goes into the chain there.
    const edgeId = isTransformType(type) ? edgeAt(e.clientX, e.clientY) : null
    if (edgeId) {
      const nextEdges = insertIntoEdge(edges, edgeId, newNode.id, () => newId('edge')) as Edge[]
      // Line the chain up after the insert so no node lands on another.
      onNodesChange(makeRoom([...nodes, newNode], nextEdges, newNode.id))
      onEdgesChange(nextEdges)
      return
    }

    onNodesChange([...nodes, newNode])
  }

  const transformIcons: Record<string, typeof Radio> = {
    transform_parse: Braces,
    transform_map: ArrowLeftRight,
    transform_filter: Funnel,
    transform_sample: Percent,
    transform_redact: EyeOff,
  }

  const sourceIcons: Record<string, typeof Radio> = {
    source_kafka: Radio,
    source_webhook: Webhook,
    source_database: Database,
    source_s3: HardDrive,
    source_ingest: Inbox,
  }
</script>

<div class="flex flex-1 min-h-0">
  <!-- Node Palette -->
  <div class="w-44 border-r border-edge-subtle bg-canvas p-2 overflow-auto">
    <p class="text-[10px] font-semibold text-fg-3 uppercase tracking-wider mb-2 px-1">
      Sources
    </p>
    {#each SOURCE_NODE_TYPES as source}
      {@const Icon = sourceIcons[source.type] || Radio}
      <div
        class="flex items-center gap-2 px-2 py-1.5 mb-1 rounded-lg border border-edge-subtle bg-surface cursor-grab hover:border-edge-strong transition-colors text-xs"
        draggable="true"
        ondragstart={(e: DragEvent) => onDragStart(e, source.type)}
        role="button"
        tabindex={0}
      >
        <Icon size={14} class="text-fg-3 shrink-0" />
        <div class="min-w-0">
          <div class="font-medium text-fg-2 text-xs">{source.label}</div>
          <div class="text-[9px] text-fg-4 truncate">{source.description}</div>
        </div>
      </div>
    {/each}

    <p class="text-[10px] font-semibold text-fg-3 uppercase tracking-wider mt-3 mb-2 px-1">
      Transforms
    </p>
    {#each TRANSFORM_NODE_TYPES as transform}
      {@const Icon = transformIcons[transform.type] || Braces}
      <div
        class="flex items-center gap-2 px-2 py-1.5 mb-1 rounded-lg border border-edge-subtle bg-surface cursor-grab hover:border-edge-strong transition-colors text-xs"
        draggable="true"
        ondragstart={(e: DragEvent) => onDragStart(e, transform.type)}
        role="button"
        tabindex={0}
        title="Drag onto the canvas, or onto an edge to insert it into the chain"
      >
        <Icon size={14} class="text-fg-3 shrink-0" />
        <div class="min-w-0 flex-1">
          <div class="flex items-center gap-1 font-medium text-fg-2 text-xs">
            {transform.label}
            {#if transform.pro && !isProActive()}
              <span class="ml-auto rounded-sm bg-surface-2 px-1 text-[9px] font-semibold uppercase tracking-wider text-fg-4">Pro</span>
            {/if}
          </div>
          <div class="text-[9px] text-fg-4 truncate">{transform.description}</div>
        </div>
      </div>
    {/each}

    <p class="text-[10px] font-semibold text-fg-3 uppercase tracking-wider mt-3 mb-2 px-1">
      Sinks
    </p>
    {#each SINK_NODE_TYPES as sink}
      <div
        class="flex items-center gap-2 px-2 py-1.5 mb-1 rounded-lg border border-accent/40 bg-accent-soft cursor-grab hover:border-accent transition-colors text-xs"
        draggable="true"
        ondragstart={(e: DragEvent) => onDragStart(e, sink.type)}
        role="button"
        tabindex={0}
      >
        <Database size={14} class="text-accent shrink-0" />
        <div class="min-w-0">
          <div class="font-medium text-fg-2 text-xs">{sink.label}</div>
          <div class="text-[9px] text-fg-4 truncate">{sink.description}</div>
        </div>
      </div>
    {/each}
  </div>

  <!-- Flow Canvas -->
  <div
    class="flex-1 min-h-0"
    ondragover={onDragOver}
    ondrop={onDrop}
    onpointerdowncapture={onPointerDownCapture}
    role="presentation"
  >
    <SvelteFlow
      {nodes}
      {edges}
      {nodeTypes}
      fitView
      colorMode={getTheme()}
      proOptions={{ hideAttribution: true }}
      onbeforeconnect={refuseConnection}
      onconnect={onConnect}
      onnodeclick={({ node }) => {
        if (node?.id) onNodeClick(node.id)
      }}
      onpaneclick={handlePaneClick}
      deleteKey={['Delete', 'Backspace']}
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
        let nextEdges = edges
        if (deletedEdges.length > 0) {
          const deleteIds = new Set(deletedEdges.map((e) => e.id))
          // Edges removed with a node are rejoined below; drop only the others here.
          const nodeIds = new Set(deletedNodes.map((n) => n.id))
          nextEdges = nextEdges.filter((e) => !deleteIds.has(e.id) || nodeIds.has(e.source) || nodeIds.has(e.target))
        }
        if (deletedNodes.length > 0) {
          const deleteIds = new Set(deletedNodes.map((n) => n.id))
          onNodesChange(nodes.filter((n) => !deleteIds.has(n.id)))
          // Removing a transform from the middle keeps the chain connected.
          nextEdges = bridgeDeleted(nextEdges, deleteIds, () => newId('edge')) as Edge[]
        }
        if (nextEdges !== edges) onEdgesChange(nextEdges)
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

  /* Controls: dark mode */
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

  /* Controls: light mode */
  :global(.svelte-flow .svelte-flow__controls) {
    border-radius: 8px;
    box-shadow: 0 1px 4px rgba(0, 0, 0, 0.1);
  }

  /* MiniMap: dark mode */
  :global(.svelte-flow.dark .svelte-flow__minimap) {
    background: #141414;
    border: 1px solid #333;
    border-radius: 8px;
  }

  /* MiniMap: light mode */
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
