<script lang="ts">
  import { onMount } from 'svelte'
  import type { Node, Edge, Connection } from '@xyflow/svelte'
  import type { Pipeline, PipelineStatus, NodeType } from '../../types/pipelines'
  import * as api from '../../api/pipelines'
  import { success as toastSuccess, error as toastError } from '../../stores/toast.svelte'
  import PipelineToolbar from './PipelineToolbar.svelte'
  import PipelineCanvas from './PipelineCanvas.svelte'
  import PipelineStatusBar from './PipelineStatusBar.svelte'
  import NodeConfigPanel from './NodeConfigPanel.svelte'
  import Spinner from '../common/Spinner.svelte'

  interface Props {
    pipelineId: string
    onBack: () => void
  }

  let { pipelineId, onBack }: Props = $props()

  let pipeline = $state<Pipeline | null>(null)
  let loading = $state(true)
  let saving = $state(false)

  let nodes = $state<Node[]>([])
  let edges = $state<Edge[]>([])

  let selectedNodeId = $state<string | null>(null)

  const selectedNode = $derived(selectedNodeId ? nodes.find((n) => n.id === selectedNodeId) : null)

  onMount(async () => {
    await loadPipeline()
  })

  async function loadPipeline() {
    loading = true
    try {
      const res = await api.getPipeline(pipelineId)
      pipeline = res.pipeline

      // Convert backend graph nodes to Svelte Flow nodes
      nodes = (res.graph.nodes || []).map((n) => ({
        id: n.id,
        type: n.node_type as string,
        position: { x: n.position_x, y: n.position_y },
        data: {
          label: n.label,
          node_type: n.node_type,
          config: parseConfig(n.config_encrypted),
        },
      }))

      // Convert backend edges to Svelte Flow edges
      edges = (res.graph.edges || []).map((e) => ({
        id: e.id,
        source: e.source_node_id,
        target: e.target_node_id,
        sourceHandle: e.source_handle ?? undefined,
        targetHandle: e.target_handle ?? undefined,
        animated: true,
      }))
    } catch (e: any) {
      toastError(e.message || 'Failed to load pipeline')
    } finally {
      loading = false
    }
  }

  function parseConfig(encrypted: string): Record<string, unknown> {
    try {
      return JSON.parse(encrypted)
    } catch {
      return {}
    }
  }

  async function handleSave() {
    saving = true
    try {
      const graphNodes = nodes.map((n) => ({
        id: n.id,
        node_type: n.data.node_type as string,
        label: n.data.label as string,
        position_x: n.position.x,
        position_y: n.position.y,
        config: (n.data.config || {}) as Record<string, unknown>,
      }))

      const graphEdges = edges.map((e) => ({
        id: e.id,
        source_node_id: e.source,
        target_node_id: e.target,
        source_handle: e.sourceHandle ?? undefined,
        target_handle: e.targetHandle ?? undefined,
      }))

      await api.saveGraph(pipelineId, { nodes: graphNodes, edges: graphEdges })
      toastSuccess('Pipeline saved')
    } catch (e: any) {
      toastError(e.message || 'Failed to save pipeline')
    } finally {
      saving = false
    }
  }

  async function handleStart() {
    try {
      // Save first, then start
      await handleSave()
      await api.startPipeline(pipelineId)
      toastSuccess('Pipeline started')
      await loadPipeline()
    } catch (e: any) {
      toastError(e.message || 'Failed to start pipeline')
    }
  }

  async function handleStop() {
    try {
      await api.stopPipeline(pipelineId)
      toastSuccess('Pipeline stopped')
      await loadPipeline()
    } catch (e: any) {
      toastError(e.message || 'Failed to stop pipeline')
    }
  }

  function handleConnect(connection: Connection) {
    const newEdge: Edge = {
      id: `edge-${Date.now()}`,
      source: connection.source!,
      target: connection.target!,
      sourceHandle: connection.sourceHandle ?? undefined,
      targetHandle: connection.targetHandle ?? undefined,
      animated: true,
    }
    edges = [...edges, newEdge]
  }

  function handleNodeClick(nodeId: string) {
    selectedNodeId = nodeId
  }

  function handlePaneClick() {
    selectedNodeId = null
  }

  function handleNodeConfigUpdate(nodeId: string, config: Record<string, unknown>, label: string) {
    nodes = nodes.map((n) => {
      if (n.id !== nodeId) return n
      return {
        ...n,
        data: {
          ...n.data,
          config,
          label,
        },
      }
    })
  }
</script>

<div class="flex flex-col h-full">
  {#if loading}
    <div class="flex items-center justify-center h-full">
      <Spinner />
    </div>
  {:else if pipeline}
    <PipelineToolbar
      pipelineName={pipeline.name}
      status={pipeline.status as PipelineStatus}
      {saving}
      {onBack}
      onSave={handleSave}
      onStart={handleStart}
      onStop={handleStop}
    />

    <div class="flex flex-1 min-h-0">
      <PipelineCanvas
        {nodes}
        {edges}
        onNodesChange={(updated) => { nodes = updated }}
        onEdgesChange={(updated) => { edges = updated }}
        onConnect={handleConnect}
        onNodeClick={handleNodeClick}
        onPaneClick={handlePaneClick}
      />

      {#if selectedNode}
        <NodeConfigPanel
          nodeId={selectedNode.id}
          nodeType={selectedNode.data.node_type as NodeType}
          label={selectedNode.data.label as string}
          config={(selectedNode.data.config || {}) as Record<string, unknown>}
          {pipelineId}
          onUpdate={handleNodeConfigUpdate}
          onClose={() => { selectedNodeId = null }}
        />
      {/if}
    </div>

    <PipelineStatusBar
      pipelineId={pipelineId}
      status={pipeline.status as PipelineStatus}
      onStatusChange={(newStatus) => {
        if (pipeline) pipeline = { ...pipeline, status: newStatus }
      }}
    />
  {:else}
    <div class="flex items-center justify-center h-full text-gray-500">
      Pipeline not found
    </div>
  {/if}
</div>
