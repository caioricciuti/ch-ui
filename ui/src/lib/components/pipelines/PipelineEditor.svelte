<script lang="ts">
  import { onMount } from 'svelte'
  import type { Node, Edge, Connection } from '@xyflow/svelte'
  import type { Pipeline, PipelineStatus, PipelineStatusInfo, NodeType, PipelineTestResult, InferredSchema } from '../../types/pipelines'
  import * as api from '../../api/pipelines'
  import { success as toastSuccess, error as toastError } from '../../stores/toast.svelte'
  import PipelineToolbar from './PipelineToolbar.svelte'
  import PipelineCanvas from './PipelineCanvas.svelte'
  import PipelineStatusBar from './PipelineStatusBar.svelte'
  import PipelineRunsSheet from './PipelineRunsSheet.svelte'
  import PipelineTestSheet from './PipelineTestSheet.svelte'
  import NodeConfigPanel from './NodeConfigPanel.svelte'
  import TransformPanel from './transforms/TransformPanel.svelte'
  import { isRouteNode, layoutBranches, otelDefaultSink, outputsOf, sinksFromSource, staleRouteEdges, storedViewport, transformsUpTo } from './transforms/graph'
  import { clearNodeError, clearTransformCounters, setNodeError, setSinkCounters, setTransformCounters } from './transforms/counters.svelte'
  import { resetPastedSample } from './transforms/sample.svelte'
  import { ApiError } from '../../api/client'
  import { isTransformType, type TransformNodeType } from '../../types/transforms'
  import IngestKeysSheet from '../ingest/IngestKeysSheet.svelte'
  import SendDataSheet from '../ingest/SendDataSheet.svelte'
  import { listIngestKeys } from '../../api/ingest'
  import { isIngestSignal, usesOtelLayout, withOtelSinkDefaults } from '../../types/ingest'
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
  let showTest = $state(false)
  let testing = $state(false)
  let testResult = $state<PipelineTestResult | null>(null)
  let testError = $state('')
  /** Bumped when the editor changes a node config, to remount the open panel. */
  let panelVersion = $state(0)

  let nodes = $state<Node[]>([])
  let edges = $state<Edge[]>([])
  /** The graph as last loaded or saved, to detect unsaved changes. */
  let savedSnapshot = $state('')

  let selectedNodeId = $state<string | null>(null)

  const selectedNode = $derived(selectedNodeId ? nodes.find((n) => n.id === selectedNodeId) : null)
  const isRunning = $derived(pipeline?.status === 'running' || pipeline?.status === 'starting')
  const dirty = $derived(!loading && pipeline !== null && snapshot() !== savedSnapshot)

  const ingestNode = $derived(nodes.find((n) => n.data.node_type === 'source_ingest') ?? null)
  const ingestSignal = $derived.by(() => {
    if (!ingestNode) return null
    const sig = ((ingestNode.data.config || {}) as Record<string, unknown>)['signal']
    return isIngestSignal(sig) ? sig : 'logs'
  })
  /** The selected transform's chain up to itself, for its preview. */
  const selectedChain = $derived.by(() => {
    if (!selectedNode || !isTransformType(selectedNode.data.node_type)) return null
    const { chain, branch, error } = transformsUpTo(nodes, edges, selectedNode.id)
    return {
      note: error,
      branch,
      nodes: chain.map((n) => ({
        id: n.id,
        node_type: n.data.node_type as TransformNodeType,
        label: String(n.data.label ?? n.id),
        config: (n.data.config || {}) as Record<string, unknown>,
      })),
    }
  })
  /** For a selected route node: the label of the node each connected output feeds. */
  const selectedOutputs = $derived.by(() => {
    if (!selectedNode || !isRouteNode(selectedNode)) return {}
    const out: Record<string, string> = {}
    for (const e of edges) {
      if (e.source !== selectedNode.id || !e.sourceHandle) continue
      const t = nodes.find((n) => n.id === e.target)
      out[e.sourceHandle] = String(t?.data.label ?? e.target)
    }
    return out
  })
  // Polling stops with the run; drop the last counters with it.
  $effect(() => {
    if (!isRunning) clearTransformCounters()
  })
  let showKeys = $state(false)
  let showSendData = $state(false)
  let sendSecret = $state<string | undefined>(undefined)
  let activeKeys = $state<number | null>(null)
  let keysRequested = false

  // Count keys once an ingest source is on the canvas; the keys sheet keeps it current.
  $effect(() => {
    if (ingestNode && !keysRequested) {
      keysRequested = true
      listIngestKeys(pipelineId)
        .then((res) => { activeKeys = (res.keys ?? []).filter((k) => !k.revoked_at).length })
        .catch(() => { activeKeys = null })
    }
  })

  onMount(() => {
    void loadPipeline()
    const warn = (e: BeforeUnloadEvent) => {
      if (dirty) e.preventDefault()
    }
    window.addEventListener('beforeunload', warn)
    return () => {
      window.removeEventListener('beforeunload', warn)
      clearTransformCounters()
      clearNodeError()
      resetPastedSample()
    }
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
      clearNodeError()
      toastSuccess(isRunning ? 'Pipeline saved. Changes apply after a restart.' : 'Pipeline saved')
      return true
    } catch (e: unknown) {
      toastError(errorText(e, 'Failed to save pipeline'))
      markNodeError(e)
      return false
    } finally {
      saving = false
    }
  }

  /** An error naming a node (bad transform config, a Pro node without Pro): mark that node and open it. */
  function markNodeError(e: unknown) {
    const body = e instanceof ApiError ? e.body : null
    const nodeId = body && typeof body === 'object' ? (body as Record<string, unknown>).node_id : null
    if (typeof nodeId === 'string' && nodes.some((n) => n.id === nodeId)) {
      setNodeError(nodeId, errorText(e, 'Invalid config'))
      selectedNodeId = nodeId
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
      markNodeError(e)
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

  /** Saves when needed, then tests the saved pipeline. */
  async function runTest(): Promise<PipelineTestResult | null> {
    if (testing) return null
    testing = true
    testError = ''
    try {
      if (dirty && !(await handleSave())) {
        testError = 'Save failed; fix the pipeline and test again.'
        return null
      }
      testResult = await api.testPipeline(pipelineId)
      return testResult
    } catch (e: unknown) {
      testResult = null
      testError = errorText(e, 'Test failed')
      return null
    } finally {
      testing = false
    }
  }

  async function handleTest() {
    showTest = true
    await runTest()
  }

  /** For the sink panel: sample the source and return ClickHouse's schema. */
  async function handleInferSchema(): Promise<InferredSchema | null> {
    const res = await runTest()
    if (!res) {
      toastError(testError || 'Test failed')
      return null
    }
    if (res.source_error) {
      toastError(res.source_error)
      return null
    }
    const schema = (selectedNodeId && res.schemas?.[selectedNodeId]) || res.schema
    if (!schema) {
      toastError(res.schema_error || 'The source returned no sample records to infer from')
      return null
    }
    return schema
  }

  /** Applies an inferred schema to the sink node from the test sheet. */
  function applySchemaToSink(schema: InferredSchema) {
    const sinks = nodes.filter((n) => n.data.node_type === 'sink_clickhouse')
    const sink = sinks.length === 1 ? sinks[0] : sinks.find((n) => n.id === selectedNodeId)
    if (!sink) {
      toastError(sinks.length ? 'Select the sink to apply the columns to, then apply again' : 'Add a ClickHouse sink first')
      return
    }
    // With several sinks each has its own schema, from the rows its branch writes.
    schema = testResult?.schemas?.[sink.id] ?? schema
    const config = {
      ...((sink.data.config || {}) as Record<string, unknown>),
      preset: 'custom',
      create_table: true,
      create_table_columns: schema.columns,
      create_table_order_by: schema.order_by,
      create_table_partition_by: schema.partition_by,
    }
    handleNodeConfigUpdate(sink.id, config, sink.data.label as string)
    panelVersion++
    showTest = false
    selectedNodeId = sink.id
    toastSuccess('Columns applied to the sink. Save to keep them.')
  }

  function handleBack() {
    if (dirty) {
      confirmLeave = true
      return
    }
    onBack()
  }

  function handleStatus(info: PipelineStatusInfo) {
    // Per transform counters show on the nodes only while the pipeline runs.
    if (info.status === 'running' || info.status === 'starting') {
      setTransformCounters(info.transforms)
      setSinkCounters(info.sinks)
    } else clearTransformCounters()
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
    // A new branch off the route: stack the branches so none overlap.
    const from = nodes.find((n) => n.id === newEdge.source)
    if (isRouteNode(from)) nodes = layoutBranches(nodes, edges, newEdge.source)
    applyOtelSinkDefaults()
  }

  /**
   * Logs and traces land in otel_logs / otel_traces so Telemetry's Detect
   * sources finds them. Only an empty or defaulted sink table is changed.
   */
  function applyOtelSinkDefaults() {
    const source = nodes.find((n) => n.data.node_type === 'source_ingest')
    if (!source) return
    const signal = ((source.data.config || {}) as Record<string, unknown>)['signal']
    const target = otelTable(signal)
    // One sink reached from the ingest source gets the table: with routes, the
    // first branch's (or the one already holding it); the others stay for the
    // user to name. otelDefaultSink walks from the first source on the canvas.
    const firstSource = nodes.find((n) => String(n.data.node_type).startsWith('source_'))
    const sinkId = target && firstSource?.id === source.id ? otelDefaultSink(nodes, edges, target) : null
    let touchedOpenPanel = false
    nodes = nodes.map((n) => {
      if (n.id !== sinkId || n.data.node_type !== 'sink_clickhouse') return n
      const config = withOtelSinkDefaults((n.data.config || {}) as Record<string, unknown>, signal)
      if (!config) return n
      if (n.id === selectedNodeId) touchedOpenPanel = true
      return { ...n, data: { ...n.data, config } }
    })
    // The open sink panel holds a copy of its config; remount it to show the change.
    if (touchedOpenPanel) panelVersion++
  }

  /** The OTel table an ingest signal defaults its sink to, or '' for none. */
  function otelTable(signal: unknown): string {
    const t = withOtelSinkDefaults({}, signal)?.table
    return typeof t === 'string' ? t : ''
  }

  /** For the selected sink of a routed OTel pipeline that did not get the default table: how to name its own. */
  const sinkTableHint = $derived.by(() => {
    if (!selectedNode || selectedNode.data.node_type !== 'sink_clickhouse' || !ingestNode) return ''
    const target = otelTable(ingestSignal)
    if (!target || !usesOtelLayout(ingestSignal)) return ''
    const sinks = sinksFromSource(nodes, edges)
    if (sinks.length < 2 || !sinks.some((n) => n.id === selectedNode.id)) return ''
    if (otelDefaultSink(nodes, edges, target) === selectedNode.id) return ''
    const { branch } = transformsUpTo(nodes, edges, selectedNode.id)
    const route = nodes.find((n) => isRouteNode(n))
    const label = route ? (outputsOf(route).find((o) => o.handle === branch)?.label ?? branch) : branch
    const suffix = label.toLowerCase().replace(/[^a-z0-9_]+/g, '_').replace(/^_+|_+$/g, '') || 'branch'
    return `Name the table for this route, e.g. ${target}_${suffix}; Telemetry Detect sources finds any table in the OTel layout.`
  })

  function handleNodeClick(nodeId: string) {
    selectedNodeId = nodeId
  }

  function handlePaneClick() {
    selectedNodeId = null
  }

  function handleNodeConfigUpdate(nodeId: string, config: Record<string, unknown>, label: string) {
    const before = nodes.find((n) => n.id === nodeId)
    const signalChanged = before?.data.node_type === 'source_ingest' &&
      ((before.data.config || {}) as Record<string, unknown>)['signal'] !== config['signal']
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
    // A removed route (or unmatched turned off) takes its output's edge with it; the panel confirmed it.
    if (isRouteNode(before)) {
      const stale = new Set(staleRouteEdges(nodes, edges).map((e) => e.id))
      if (stale.size) edges = edges.filter((e) => !stale.has(e.id))
    }
    if (signalChanged) applyOtelSinkDefaults()
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
      {testing}
      onTest={handleTest}
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
        signal={ingestSignal}
        viewport={storedViewport(pipeline.config)}
      />

      {#if selectedNode}
        <!-- Keyed: the panel copies label/config into local state on mount,
             so switching nodes must remount it or edits land on the old node. -->
        {#key `${selectedNode.id}:${panelVersion}`}
          {#if selectedChain}
            <TransformPanel
              nodeId={selectedNode.id}
              nodeType={selectedNode.data.node_type as TransformNodeType}
              label={selectedNode.data.label as string}
              config={(selectedNode.data.config || {}) as Record<string, unknown>}
              {pipelineId}
              signal={ingestSignal}
              chain={selectedChain.nodes}
              chainNote={selectedChain.note}
              branch={selectedChain.branch}
              connected={selectedOutputs}
              onUpdate={handleNodeConfigUpdate}
              onClose={() => { selectedNodeId = null }}
            />
          {:else}
            <NodeConfigPanel
              nodeId={selectedNode.id}
              nodeType={selectedNode.data.node_type as NodeType}
              label={selectedNode.data.label as string}
              config={(selectedNode.data.config || {}) as Record<string, unknown>}
              {pipelineId}
              onUpdate={handleNodeConfigUpdate}
              onClose={() => { selectedNodeId = null }}
              onInferSchema={handleInferSchema}
              sourceSignal={ingestSignal}
              tableHint={sinkTableHint}
              {activeKeys}
              onManageKeys={() => { showKeys = true }}
              onSendData={() => { sendSecret = undefined; showSendData = true }}
            />
          {/if}
        {/key}
      {/if}
    </div>

    <PipelineStatusBar
      {pipelineId}
      status={pipeline.status as PipelineStatus}
      lastError={pipeline.last_error}
      ingest={!!ingestNode}
      onStatus={handleStatus}
    />
  {:else}
    <div class="flex h-full items-center justify-center text-[13px] text-fg-3">
      Pipeline not found
    </div>
  {/if}
</div>

<PipelineRunsSheet open={showRuns} {pipelineId} running={isRunning} ingest={!!ingestNode} onclose={() => { showRuns = false }} />

<IngestKeysSheet
  open={showKeys}
  {pipelineId}
  onclose={() => { showKeys = false }}
  onchange={(n) => { activeKeys = n }}
  onsetup={(secret) => { showKeys = false; sendSecret = secret; showSendData = true }}
/>

<SendDataSheet
  open={showSendData}
  {pipelineId}
  secret={sendSecret}
  signal={ingestSignal ?? undefined}
  onclose={() => { showSendData = false }}
/>

<PipelineTestSheet
  open={showTest}
  loading={testing}
  result={testResult}
  error={testError}
  onclose={() => { showTest = false }}
  onretest={() => { void runTest() }}
  onapplyschema={applySchemaToSink}
/>

<ConfirmDialog
  open={confirmLeave}
  title="Discard unsaved changes?"
  description="The pipeline graph has changes that are not saved. Leaving now discards them."
  confirmLabel="Discard and leave"
  destructive
  onconfirm={() => { confirmLeave = false; onBack() }}
  oncancel={() => { confirmLeave = false }}
/>
