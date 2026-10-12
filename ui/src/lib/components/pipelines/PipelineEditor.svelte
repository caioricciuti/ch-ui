<script lang="ts">
  import { onMount } from 'svelte'
  import type { Node, Edge, Connection } from '@xyflow/svelte'
  import type { Pipeline, PipelineStatus, PipelineStatusInfo, NodeType } from '../../types/pipelines'
  import * as api from '../../api/pipelines'
  import { success as toastSuccess, error as toastError } from '../../stores/toast.svelte'
  import PipelineToolbar from './PipelineToolbar.svelte'
  import PipelineCanvas from './PipelineCanvas.svelte'
  import PipelineStatusBar from './PipelineStatusBar.svelte'
  import PipelineRunsSheet from './PipelineRunsSheet.svelte'
  import NodeConfigPanel from './NodeConfigPanel.svelte'
  import Spinner from '../common/Spinner.svelte'
  import ConfirmDialog from '../common/ConfirmDialog.svelte'
  import { Info } from 'lucide-svelte'

  interface Props {
    pipelineId: string
    onBack: () => void
  }

  let { pipelineId, onBack }: Props = $props()

  let pipeline = $state<Pipeline | null>(null)
  let loading = $state(true)
  let saving = $state(false)
  /** A start or stop request is in flight; Run and Stop are disabled. */
  let pending = $state(false)
  let showRuns = $state(false)
  let confirmLeave = $state(false)

  let nodes = $state<Node[]>([])
  let edges = $state<Edge[]>([])
  /** The graph as last loaded or saved, to detect unsaved changes. */
  let savedSnapshot = $state('')

  let selectedNodeId = $state<string | null>(null)

  const selectedNode = $derived(selectedNodeId ? nodes.find((n) => n.id === selectedNodeId) : null)
  const isRunning = $derived(pipeline?.status === 'running' || pipeline?.status === 'starting')
  const dirty = $derived(!loading && pipeline !== null && snapshot() !== savedSnapshot)

  onMount(() => {
    void loadPipeline()
    const warn = (e: BeforeUnloadEvent) => {
      if (dirty) e.preventDefault()
    }
    window.addEventListener('beforeunload', warn)
    return () => window.removeEventListener('beforeunload', warn)
  })

  function errorText(e: unknown, fallback: string): string {
    return (e instanceof Error ? e.message : String(e)) || fallback
  }

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
      savedSnapshot = snapshot()
    } catch (e: unknown) {
      toastError(errorText(e, 'Failed to load pipeline'))
    } finally {
      loading = false
    }
  }

  /** Refresh status fields without touching the graph being edited. */
  async function refreshPipeline() {
    try {
      const res = await api.getPipeline(pipelineId)
      pipeline = res.pipeline
    } catch {
      // The status bar keeps polling; a failed refresh is not worth a toast.
    }
  }

  function parseConfig(encrypted: string): Record<string, unknown> {
    try {
      const parsed: unknown = JSON.parse(encrypted)
      return parsed && typeof parsed === 'object' ? (parsed as Record<string, unknown>) : {}
    } catch {
      return {}
    }
  }

  function graphPayload() {
    return {
      nodes: nodes.map((n) => ({
        id: n.id,
        node_type: n.data.node_type as string,
        label: n.data.label as string,
        position_x: n.position.x,
        position_y: n.position.y,
        config: (n.data.config || {}) as Record<string, unknown>,
      })),
      edges: edges.map((e) => ({
        id: e.id,
        source_node_id: e.source,
        target_node_id: e.target,
        source_handle: e.sourceHandle ?? undefined,
        target_handle: e.targetHandle ?? undefined,
      })),
    }
  }

  function snapshot(): string {
    return JSON.stringify(graphPayload())
  }

  /** Saves the graph; reports whether it was saved. */
  async function handleSave(): Promise<boolean> {
    saving = true
    try {
      const payload = graphPayload()
      await api.saveGraph(pipelineId, payload)
      savedSnapshot = JSON.stringify(payload)
      toastSuccess(isRunning ? 'Pipeline saved. Changes apply after a restart.' : 'Pipeline saved')
      return true
    } catch (e: unknown) {
      toastError(errorText(e, 'Failed to save pipeline'))
      return false
    } finally {
      saving = false
    }
  }

  async function handleStart() {
    if (pending) return
    pending = true
    try {
      // Save first; never start a pipeline whose save failed.
      if (!(await handleSave())) return
      await api.startPipeline(pipelineId)
      toastSuccess('Pipeline started')
      await refreshPipeline()
    } catch (e: unknown) {
      toastError(errorText(e, 'Failed to start pipeline'))
      await refreshPipeline()
    } finally {
      pending = false
    }
  }

  async function handleStop() {
    if (pending) return
    pending = true
    try {
      await api.stopPipeline(pipelineId)
      toastSuccess('Pipeline stopped')
      await refreshPipeline()
    } catch (e: unknown) {
      toastError(errorText(e, 'Failed to stop pipeline'))
    } finally {
      pending = false
    }
  }

  function handleBack() {
    if (dirty) {
      confirmLeave = true
      return
    }
    onBack()
  }

  function handleStatus(info: PipelineStatusInfo) {
    if (!pipeline) return
    if (info.status !== pipeline.status || info.last_error !== pipeline.last_error) {
      pipeline = { ...pipeline, status: info.status, last_error: info.last_error }
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

<div class="flex h-full min-h-0 flex-col">
  {#if loading}
    <div class="flex items-center justify-center h-full">
      <Spinner />
    </div>
  {:else if pipeline}
    <PipelineToolbar
      pipelineName={pipeline.name}
      status={pipeline.status as PipelineStatus}
      {saving}
      {pending}
      {dirty}
      retrying={isRunning && !!pipeline.last_error}
      onBack={handleBack}
      onSave={handleSave}
      onStart={handleStart}
      onStop={handleStop}
      onRuns={() => { showRuns = true }}
    />

    {#if isRunning}
      <div class="flex shrink-0 items-center gap-2 border-b border-edge-subtle bg-info-soft px-5 py-1.5 text-[13px] text-info">
        <Info size={14} class="shrink-0" />
        This pipeline is running. Saved changes apply after you stop and run it again.
      </div>
    {/if}

    <div class="flex min-h-0 flex-1">
      <PipelineCanvas
        {pipelineId}
        {nodes}
        {edges}
        onNodesChange={(updated) => { nodes = updated }}
        onEdgesChange={(updated) => { edges = updated }}
        onConnect={handleConnect}
        onNodeClick={handleNodeClick}
        onPaneClick={handlePaneClick}
      />

      {#if selectedNode}
        <!-- Keyed: the panel copies label/config into local state on mount,
             so switching nodes must remount it or edits land on the old node. -->
        {#key selectedNode.id}
          <NodeConfigPanel
            nodeId={selectedNode.id}
            nodeType={selectedNode.data.node_type as NodeType}
            label={selectedNode.data.label as string}
            config={(selectedNode.data.config || {}) as Record<string, unknown>}
            {pipelineId}
            onUpdate={handleNodeConfigUpdate}
            onClose={() => { selectedNodeId = null }}
          />
        {/key}
      {/if}
    </div>

    <PipelineStatusBar
      {pipelineId}
      status={pipeline.status as PipelineStatus}
      lastError={pipeline.last_error}
      onStatus={handleStatus}
    />
  {:else}
    <div class="flex h-full items-center justify-center text-[13px] text-fg-3">
      Pipeline not found
    </div>
  {/if}
</div>

<PipelineRunsSheet open={showRuns} {pipelineId} onclose={() => { showRuns = false }} />

<ConfirmDialog
  open={confirmLeave}
  title="Discard unsaved changes?"
  description="The pipeline graph has changes that are not saved. Leaving now discards them."
  confirmLabel="Discard and leave"
  destructive
  onconfirm={() => { confirmLeave = false; onBack() }}
  oncancel={() => { confirmLeave = false }}
/>
