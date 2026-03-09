<script lang="ts">
  import { onMount } from 'svelte'
  import type { Model, ModelRun, ModelRunResult, ModelSchedule, DAGNode, DAGEdge, Pipeline } from '../lib/types/models'
  import * as api from '../lib/api/models'
  import { refreshModelCache } from '../lib/editor/completions'
  import { success as toastSuccess, error as toastError } from '../lib/stores/toast.svelte'
  import { openModelTab } from '../lib/stores/tabs.svelte'
  import ConfirmDialog from '../lib/components/common/ConfirmDialog.svelte'
  import ContextMenu from '../lib/components/common/ContextMenu.svelte'
  import type { ContextMenuItem } from '../lib/components/common/ContextMenu.svelte'
  import {
    SvelteFlow,
    Controls,
    Background,
    type Node,
    type Edge,
    type NodeTypes,
  } from '@xyflow/svelte'
  import '@xyflow/svelte/dist/style.css'
  import ModelNode from '../lib/components/models/ModelNode.svelte'
  import { getTheme } from '../lib/stores/theme.svelte'
  import {
    Boxes,
    Plus,
    Play,
    Trash2,
    RefreshCw,
    Eye,
    MoreHorizontal,
    Table2,
    GitBranch,
    History,
    Save,
    CheckCircle,
    XCircle,
    SkipForward,
    Clock,
    ChevronDown,
    ChevronRight,
    Timer,
    X,
    Info,
  } from 'lucide-svelte'

  // ── State ──────────────────────────────────────────────────────────

  let models = $state<Model[]>([])
  let loading = $state(true)

  // DAG overlay
  let showDAG = $state(false)
  let dagNodes = $state<Node[]>([])
  let dagEdges = $state<Edge[]>([])

  // DAG edges for dependency pills (raw from API)
  let dagEdgesRaw = $state<DAGEdge[]>([])

  // model_id → list of upstream model names (what it depends on)
  let upstreamMap = $derived.by(() => {
    const map = new Map<string, string[]>()
    const idToName = new Map(models.map(m => [m.id, m.name]))
    for (const e of dagEdgesRaw) {
      const name = idToName.get(e.source)
      if (!name) continue
      const list = map.get(e.target) ?? []
      list.push(name)
      map.set(e.target, list)
    }
    return map
  })

  // History overlay
  let showHistory = $state(false)
  let runs = $state<ModelRun[]>([])
  let expandedRunId = $state<string | null>(null)
  let runResults = $state<Record<string, ModelRunResult[]>>({})

  // Run state
  let running = $state(false)

  // Info banner
  let infoDismissed = $state(localStorage.getItem('chui-pipeline-info-dismissed') === '1')

  function dismissInfo() {
    infoDismissed = true
    localStorage.setItem('chui-pipeline-info-dismissed', '1')
  }

  // Pipelines
  let pipelines = $state<Pipeline[]>([])

  // Per-pipeline run state
  let runningPipeline = $state<string | null>(null)

  // Schedule (per-pipeline)
  let showSchedule = $state(false)
  let scheduleAnchorId = $state<string | null>(null)
  let schedule = $state<ModelSchedule | null>(null)
  let schedCron = $state('0 */6 * * *')
  let schedSaving = $state(false)

  // Derived: model lookup by id
  let modelById = $derived(new Map(models.map(m => [m.id, m])))

  // Context menu
  let contextMenu = $state<{ model: Model; x: number; y: number } | null>(null)

  function openContextMenuFromButton(event: MouseEvent, model: Model) {
    event.preventDefault()
    event.stopPropagation()
    const rect = (event.currentTarget as HTMLElement).getBoundingClientRect()
    contextMenu = {
      model,
      x: Math.min(window.innerWidth - 240, rect.right),
      y: Math.min(window.innerHeight - 220, rect.bottom + 6),
    }
  }

  function openContextMenu(event: MouseEvent, model: Model) {
    event.preventDefault()
    event.stopPropagation()
    contextMenu = {
      model,
      x: Math.min(window.innerWidth - 240, event.clientX),
      y: Math.min(window.innerHeight - 220, event.clientY),
    }
  }

  function closeContextMenu() {
    contextMenu = null
  }

  function getContextItems(): ContextMenuItem[] {
    const m = contextMenu?.model
    if (!m) return []
    return [
      { id: 'open', label: 'Open', icon: Eye, onSelect: () => selectModel(m.id) },
      { id: 'sep1', separator: true },
      { id: 'delete', label: 'Delete', icon: Trash2, danger: true, onSelect: () => handleDelete(m.id) },
    ]
  }

  // Delete confirm
  let confirmDeleteOpen = $state(false)
  let confirmDeleteLoading = $state(false)
  let pendingDeleteId = $state('')
  let pendingDeleteName = $state('')

  const theme = $derived(getTheme())

  const nodeTypes: NodeTypes = {
    model: ModelNode as any,
  }

  // ── Lifecycle ──────────────────────────────────────────────────────

  onMount(async () => {
    await loadModels()
    loadDAG()
    loadPipelines()
  })

  // ── Data loading ───────────────────────────────────────────────────

  async function loadModels() {
    loading = true
    try {
      const res = await api.listModels()
      models = res.models ?? []
    } catch (e: unknown) {
      toastError((e as Error).message || 'Failed to load models')
    } finally {
      loading = false
    }
  }

  async function loadDAG() {
    try {
      const res = await api.getDAG()
      dagEdgesRaw = res.edges ?? []
      dagNodes = (res.nodes ?? []).map((n: DAGNode) => ({
        id: n.id,
        type: 'model',
        position: n.position,
        data: n.data,
      }))
      dagEdges = dagEdgesRaw.map((e: DAGEdge) => ({
        id: e.id,
        source: e.source,
        target: e.target,
        animated: true,
        style: 'stroke: #f97316; stroke-width: 2px;',
      }))
    } catch (e: unknown) {
      toastError((e as Error).message || 'Failed to load DAG')
    }
  }

  async function loadRuns() {
    try {
      const res = await api.listModelRuns()
      runs = res.runs ?? []
    } catch (e: unknown) {
      toastError((e as Error).message || 'Failed to load runs')
    }
  }

  async function loadRunResults(runId: string) {
    if (runResults[runId]) return
    try {
      const res = await api.getModelRun(runId)
      runResults[runId] = res.results ?? []
    } catch (e: unknown) {
      toastError((e as Error).message || 'Failed to load run results')
    }
  }

  async function loadPipelines() {
    try {
      const res = await api.listPipelines()
      pipelines = res.pipelines ?? []
    } catch {
      // Pipelines not available — ignore
    }
  }

  // ── Actions ────────────────────────────────────────────────────────

  async function handleSaveSchedule() {
    if (!scheduleAnchorId) return
    schedSaving = true
    try {
      const res = await api.upsertPipelineSchedule(scheduleAnchorId, { cron: schedCron, enabled: true })
      schedule = res.schedule
      toastSuccess('Schedule saved')
      await loadPipelines()
    } catch (e: unknown) {
      toastError((e as Error).message || 'Failed to save schedule')
    } finally {
      schedSaving = false
    }
  }

  async function handleDeleteSchedule() {
    if (!scheduleAnchorId) return
    schedSaving = true
    try {
      await api.deletePipelineSchedule(scheduleAnchorId)
      schedule = null
      schedCron = '0 */6 * * *'
      toastSuccess('Schedule removed')
      showSchedule = false
      await loadPipelines()
    } catch (e: unknown) {
      toastError((e as Error).message || 'Failed to delete schedule')
    } finally {
      schedSaving = false
    }
  }

  async function handleRunPipeline(anchorId: string) {
    runningPipeline = anchorId
    try {
      const res = await api.runPipeline(anchorId)
      toastSuccess('Pipeline run started')
      await loadModels()
      await loadRuns()
      if (res.run_id) {
        expandedRunId = res.run_id
        await loadRunResults(res.run_id)
      }
      showHistory = true
    } catch (e: unknown) {
      toastError((e as Error).message || 'Failed to run pipeline')
    } finally {
      runningPipeline = null
    }
  }

  function openPipelineSchedule(anchorId: string, existing: ModelSchedule | null) {
    scheduleAnchorId = anchorId
    schedule = existing
    schedCron = existing?.cron ?? '0 */6 * * *'
    showSchedule = true
  }

  async function handleCreate() {
    const existing = new Set(models.map(m => m.name))
    let name = 'new_model'
    let i = 1
    while (existing.has(name)) {
      name = `new_model_${i++}`
    }
    try {
      const boilerplate = `SELECT *\nFROM default.my_table\nLIMIT 100`
      const res = await api.createModel({
        name,
        target_database: 'default',
        materialization: 'view',
        sql_body: boilerplate,
      })
      refreshModelCache()
      toastSuccess('Model created')
      await loadModels()
      await loadPipelines()
      openModelTab(res.model)
    } catch (e: unknown) {
      toastError((e as Error).message || 'Failed to create model')
    }
  }

  function handleDelete(id: string) {
    const model = models.find(m => m.id === id)
    pendingDeleteId = id
    pendingDeleteName = model?.name ?? ''
    confirmDeleteOpen = true
  }

  async function confirmDelete() {
    confirmDeleteLoading = true
    try {
      await api.deleteModel(pendingDeleteId)
      refreshModelCache()
      toastSuccess('Model deleted')
      await loadModels()
      await loadPipelines()
    } catch (e: unknown) {
      toastError((e as Error).message || 'Failed to delete model')
    } finally {
      confirmDeleteLoading = false
      confirmDeleteOpen = false
      pendingDeleteId = ''
      pendingDeleteName = ''
    }
  }

  async function handleRunAll() {
    running = true
    try {
      const res = await api.runAllModels()
      toastSuccess('Model run started')
      await loadModels()
      await loadRuns()
      if (res.run_id) {
        expandedRunId = res.run_id
        await loadRunResults(res.run_id)
      }
      showHistory = true
    } catch (e: unknown) {
      toastError((e as Error).message || 'Failed to run models')
    } finally {
      running = false
    }
  }

  // ── Navigation ─────────────────────────────────────────────────────

  function selectModel(id: string) {
    const model = models.find(m => m.id === id)
    if (!model) return
    openModelTab(model)
  }

  function openDAG() {
    loadDAG()
    showDAG = true
  }

  function openHistory() {
    loadRuns()
    showHistory = true
  }

  function toggleRunExpand(runId: string) {
    if (expandedRunId === runId) {
      expandedRunId = null
    } else {
      expandedRunId = runId
      loadRunResults(runId)
    }
  }

  // ── Helpers ────────────────────────────────────────────────────────

  function formatDate(d: string | null): string {
    if (!d) return '\u2014'
    return new Date(d).toLocaleString()
  }

  function statusBadge(status: string): string {
    switch (status) {
      case 'success': return 'bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-400'
      case 'error': return 'bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-400'
      case 'partial': return 'bg-yellow-100 text-yellow-700 dark:bg-yellow-900/30 dark:text-yellow-400'
      case 'running': return 'bg-blue-100 text-blue-700 dark:bg-blue-900/30 dark:text-blue-400'
      default: return 'bg-gray-100 text-gray-600 dark:bg-gray-800 dark:text-gray-400'
    }
  }

  function statusDot(status: string): string {
    switch (status) {
      case 'success': return 'bg-green-500'
      case 'error': return 'bg-red-500'
      default: return 'bg-gray-400'
    }
  }
</script>

<div class="flex flex-col h-full overflow-hidden">
  <!-- ─── Toolbar ─────────────────────────────────────────────────── -->
  <div class="flex items-center gap-3 px-4 py-2.5 border-b border-gray-200 dark:border-gray-700 bg-white dark:bg-gray-950 shrink-0">
    <Boxes size={18} class="text-orange-500 shrink-0" />
    <h1 class="text-sm font-semibold text-gray-800 dark:text-gray-200">Model Pipeline</h1>
    {#if !loading}
      <span class="text-[10px] text-gray-400 dark:text-gray-500 tabular-nums">{models.length} model{models.length !== 1 ? 's' : ''}</span>
    {/if}
    <div class="flex-1"></div>
    <button
      onclick={handleRunAll}
      disabled={running || models.length === 0}
      class="flex items-center gap-1.5 text-xs px-2.5 py-1.5 rounded text-gray-600 dark:text-gray-300 hover:text-green-600 hover:bg-green-50 dark:hover:bg-green-900/20 disabled:opacity-40 transition-colors"
      title="Run all models in dependency order"
    >
      <Play size={13} /> {running ? 'Running...' : 'Run Pipeline'}
    </button>
    <button
      onclick={openDAG}
      disabled={models.length === 0}
      class="flex items-center gap-1.5 text-xs px-2.5 py-1.5 rounded text-gray-600 dark:text-gray-300 hover:text-orange-500 hover:bg-orange-50 dark:hover:bg-orange-900/20 disabled:opacity-40 transition-colors"
      title="Dependency graph"
    >
      <GitBranch size={13} /> DAG
    </button>
    <button
      onclick={openHistory}
      class="flex items-center gap-1.5 text-xs px-2.5 py-1.5 rounded text-gray-600 dark:text-gray-300 hover:text-orange-500 hover:bg-orange-50 dark:hover:bg-orange-900/20 transition-colors"
      title="Run history"
    >
      <History size={13} /> History
    </button>
    <button
      onclick={handleCreate}
      class="flex items-center gap-1.5 text-xs px-3 py-1.5 rounded bg-orange-500 text-white hover:bg-orange-600 transition-colors font-medium"
    >
      <Plus size={14} /> New Model
    </button>
  </div>

  <!-- ─── Content ─────────────────────────────────────────────────── -->
  <div class="flex-1 min-h-0 overflow-auto">
    {#if loading}
      <div class="flex items-center justify-center h-full text-gray-400 text-sm">Loading...</div>
    {:else if models.length === 0}
      <div class="flex flex-col items-center justify-center h-full gap-4 text-gray-400 dark:text-gray-500">
        <Boxes size={56} strokeWidth={1} class="opacity-20" />
        <div class="text-center space-y-1">
          <p class="text-sm font-medium text-gray-500 dark:text-gray-400">No models yet</p>
          <p class="text-xs">Models are SQL transformations that form a pipeline. They can reference each other with <code class="text-[11px] px-1 py-0.5 rounded bg-gray-100 dark:bg-gray-800 font-mono">$ref(model_name)</code> and run in dependency order.</p>
        </div>
        <button
          onclick={handleCreate}
          class="flex items-center gap-1.5 text-xs px-4 py-2 rounded bg-orange-500 text-white hover:bg-orange-600 transition-colors font-medium"
        >
          <Plus size={14} /> Create your first model
        </button>
      </div>
    {:else}
      {#if !infoDismissed}
        <div class="mx-4 mt-4 mb-0 flex items-start gap-2.5 px-3 py-2.5 rounded-lg border border-blue-200 dark:border-blue-800/50 bg-blue-50/70 dark:bg-blue-950/30 text-xs text-gray-600 dark:text-gray-400 leading-relaxed">
          <Info size={14} class="text-blue-400 shrink-0 mt-0.5" />
          <p class="flex-1">
            Models are SQL transformations that form a pipeline. Use <code class="text-[11px] px-1 py-0.5 rounded bg-blue-100 dark:bg-blue-900/40 font-mono text-blue-700 dark:text-blue-300">$ref(model_name)</code> to reference other models.
            <span class="font-semibold">Run Pipeline</span> executes all models in dependency order — if a model fails, its dependents are automatically skipped.
          </p>
          <button onclick={dismissInfo} class="text-gray-400 hover:text-gray-600 dark:hover:text-gray-300 shrink-0 mt-0.5" title="Dismiss">
            <X size={14} />
          </button>
        </div>
      {/if}
      <div class="p-4 space-y-4">
        {#each pipelines as pipeline (pipeline.anchor_model_id)}
          {@const pipelineModels = pipeline.model_ids.map(id => modelById.get(id)).filter((m): m is Model => !!m)}
          {#if pipelineModels.length > 0}
            <div class="rounded-lg border border-gray-200 dark:border-gray-700/80 bg-white dark:bg-gray-900/30 overflow-hidden">
              <!-- Pipeline header -->
              <div class="flex items-center gap-2.5 px-4 py-2.5 bg-gray-50 dark:bg-gray-900/50 border-b border-gray-200 dark:border-gray-700/60">
                <Boxes size={14} class="text-orange-400 shrink-0" />
                <span class="text-xs font-medium text-gray-600 dark:text-gray-300">
                  {pipelineModels.length} model{pipelineModels.length !== 1 ? 's' : ''}
                </span>
                <div class="flex-1"></div>
                <button
                  onclick={() => handleRunPipeline(pipeline.anchor_model_id)}
                  disabled={runningPipeline === pipeline.anchor_model_id || running}
                  class="flex items-center gap-1 text-[11px] px-2 py-1 rounded text-gray-500 dark:text-gray-400 hover:text-green-600 hover:bg-green-50 dark:hover:bg-green-900/20 disabled:opacity-40 transition-colors"
                  title="Run this pipeline"
                >
                  <Play size={11} />
                  {runningPipeline === pipeline.anchor_model_id ? 'Running...' : 'Run'}
                </button>
                <button
                  onclick={() => openPipelineSchedule(pipeline.anchor_model_id, pipeline.schedule)}
                  class="flex items-center gap-1 text-[11px] px-2 py-1 rounded transition-colors
                    {pipeline.schedule
                      ? 'text-orange-600 dark:text-orange-400 bg-orange-50 dark:bg-orange-900/20 font-mono'
                      : 'text-gray-500 dark:text-gray-400 hover:text-orange-500 hover:bg-orange-50 dark:hover:bg-orange-900/20'}"
                  title={pipeline.schedule ? `Schedule: ${pipeline.schedule.cron}` : 'No schedule'}
                >
                  <Timer size={11} />
                  {pipeline.schedule ? pipeline.schedule.cron : 'No schedule'}
                </button>
              </div>
              <!-- Model cards grid -->
              <div class="p-3 grid gap-3 grid-cols-[repeat(auto-fill,minmax(240px,1fr))]">
                {#each pipelineModels as model (model.id)}
                  <!-- svelte-ignore a11y_no_static_element_interactions -->
                  <div
                    class="group relative flex flex-col gap-2 p-3.5 rounded-lg border border-gray-200 dark:border-gray-700/80 bg-white dark:bg-gray-900/50 hover:border-orange-300 dark:hover:border-orange-500/40 hover:shadow-sm cursor-pointer transition-all"
                    onclick={() => selectModel(model.id)}
                    oncontextmenu={(e) => openContextMenu(e, model)}
                    onkeydown={(e: KeyboardEvent) => { if (e.key === 'Enter') selectModel(model.id) }}
                    role="button"
                    tabindex="0"
                  >
                    <div class="flex items-center gap-2">
                      {#if model.materialization === 'table'}
                        <Table2 size={14} class="text-orange-400 shrink-0" />
                      {:else}
                        <Eye size={14} class="text-blue-400 shrink-0" />
                      {/if}
                      <span class="text-sm font-semibold text-gray-800 dark:text-gray-200 truncate flex-1">{model.name}</span>
                      <span class="w-2 h-2 rounded-full {statusDot(model.status)} shrink-0" title={model.status}></span>
                      <button
                        onclick={(e) => openContextMenuFromButton(e, model)}
                        class="p-1 rounded opacity-0 group-hover:opacity-100 text-gray-400 hover:text-gray-600 dark:hover:text-gray-300 hover:bg-gray-200/70 dark:hover:bg-gray-800/70 transition-all"
                        title="More actions"
                      >
                        <MoreHorizontal size={15} />
                      </button>
                    </div>
                    <div class="flex items-center gap-2 text-[11px] text-gray-400 dark:text-gray-500">
                      <span class="px-1.5 py-0.5 rounded bg-gray-100 dark:bg-gray-800 text-gray-500 dark:text-gray-400 font-medium">
                        {model.materialization}
                      </span>
                      <span class="truncate">{model.target_database}</span>
                      {#if model.last_run_at}
                        <span class="ml-auto shrink-0" title="Last run">{formatDate(model.last_run_at)}</span>
                      {/if}
                    </div>
                    {#if upstreamMap.get(model.id)?.length}
                      <div class="flex items-center gap-1.5 flex-wrap">
                        <GitBranch size={11} class="text-orange-400 shrink-0" />
                        {#each upstreamMap.get(model.id)! as dep}
                          <span class="text-[10px] px-1.5 py-0.5 rounded-full bg-orange-100 dark:bg-orange-900/30 text-orange-600 dark:text-orange-400 font-medium">{dep}</span>
                        {/each}
                      </div>
                    {/if}
                    {#if model.last_error}
                      <p class="text-[11px] text-red-500 dark:text-red-400 truncate" title={model.last_error}>{model.last_error}</p>
                    {:else if model.description}
                      <p class="text-[11px] text-gray-400 dark:text-gray-500 truncate">{model.description}</p>
                    {/if}
                  </div>
                {/each}
              </div>
            </div>
          {/if}
        {/each}
      </div>
    {/if}
  </div>

  <!-- ─── Schedule Footer ──────────────────────────────────────────── -->
  {#if !loading && pipelines.length > 0}
    {@const scheduledPipelines = pipelines.filter(p => p.schedule)}
    <div class="shrink-0 flex items-center gap-2 px-4 py-2 border-t border-gray-200 dark:border-gray-700 bg-gray-50 dark:bg-gray-900/50 text-xs text-gray-500 dark:text-gray-400">
      <Timer size={13} class="shrink-0 text-gray-400 dark:text-gray-500" />
      {#if scheduledPipelines.length > 0}
        <span>{scheduledPipelines.length} pipeline{scheduledPipelines.length !== 1 ? 's' : ''} scheduled</span>
        {#each scheduledPipelines as sp}
          {@const anchorModel = modelById.get(sp.anchor_model_id)}
          <span class="text-gray-300 dark:text-gray-600">·</span>
          <span class="font-mono text-gray-600 dark:text-gray-300" title={anchorModel?.name ?? sp.anchor_model_id}>
            {sp.schedule?.cron}
          </span>
          {#if sp.schedule?.last_status}
            <span class="{sp.schedule.last_status === 'success' ? 'text-green-600 dark:text-green-400' : sp.schedule.last_status === 'error' ? 'text-red-600 dark:text-red-400' : ''} font-medium">
              {sp.schedule.last_status}
            </span>
          {/if}
        {/each}
      {:else}
        <span>No pipelines scheduled</span>
      {/if}
    </div>
  {/if}
</div>

<!-- ─── DAG Overlay ──────────────────────────────────────────────── -->
{#if showDAG}
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div class="fixed inset-0 z-50 flex flex-col bg-white dark:bg-gray-950" role="dialog" tabindex="-1">
    <div class="flex items-center gap-3 px-4 py-3 border-b border-gray-200 dark:border-gray-700 shrink-0">
      <GitBranch size={18} class="text-orange-500" />
      <h2 class="text-sm font-semibold text-gray-800 dark:text-gray-200 flex-1">Dependency Graph</h2>
      <button
        onclick={loadDAG}
        class="text-xs text-gray-500 hover:text-gray-700 dark:hover:text-gray-300 flex items-center gap-1"
      >
        <RefreshCw size={12} /> Refresh
      </button>
      <button
        onclick={() => { showDAG = false }}
        class="text-xs px-3 py-1 rounded border border-gray-300 dark:border-gray-600 text-gray-600 dark:text-gray-400 hover:bg-gray-100 dark:hover:bg-gray-800"
      >
        Close
      </button>
    </div>
    <div class="flex-1 min-h-0">
      {#if dagNodes.length === 0}
        <div class="flex items-center justify-center h-full text-gray-400 text-sm">
          {models.length === 0 ? 'No models to show' : 'Loading DAG...'}
        </div>
      {:else}
        <SvelteFlow
          nodes={dagNodes}
          edges={dagEdges}
          {nodeTypes}
          fitView
          colorMode={theme === 'dark' ? 'dark' : 'light'}
          onnodeclick={({ node }) => { showDAG = false; selectModel(node.id); }}
        >
          <Background />
          <Controls />
        </SvelteFlow>
      {/if}
    </div>
  </div>
{/if}

<!-- ─── History Overlay ──────────────────────────────────────────── -->
{#if showHistory}
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div class="fixed inset-0 z-50 flex flex-col bg-white dark:bg-gray-950" role="dialog" tabindex="-1">
    <div class="flex items-center gap-3 px-4 py-3 border-b border-gray-200 dark:border-gray-700 shrink-0">
      <History size={18} class="text-orange-500" />
      <h2 class="text-sm font-semibold text-gray-800 dark:text-gray-200 flex-1">Run History</h2>
      <button
        onclick={loadRuns}
        class="text-xs text-gray-500 hover:text-gray-700 dark:hover:text-gray-300 flex items-center gap-1"
      >
        <RefreshCw size={12} /> Refresh
      </button>
      <button
        onclick={() => { showHistory = false }}
        class="text-xs px-3 py-1 rounded border border-gray-300 dark:border-gray-600 text-gray-600 dark:text-gray-400 hover:bg-gray-100 dark:hover:bg-gray-800"
      >
        Close
      </button>
    </div>
    <div class="flex-1 min-h-0 overflow-auto">
      {#if runs.length === 0}
        <div class="flex items-center justify-center h-64 text-gray-400 text-sm">No runs yet</div>
      {:else}
        <div class="p-4 space-y-2">
          {#each runs as run (run.id)}
            <div class="border border-gray-200 dark:border-gray-700 rounded-lg overflow-hidden">
              <button
                onclick={() => toggleRunExpand(run.id)}
                class="w-full flex items-center gap-3 px-4 py-3 hover:bg-gray-50 dark:hover:bg-gray-800/50 transition-colors text-left"
              >
                {#if expandedRunId === run.id}
                  <ChevronDown size={14} class="text-gray-400 shrink-0" />
                {:else}
                  <ChevronRight size={14} class="text-gray-400 shrink-0" />
                {/if}
                <span class="text-[10px] px-2 py-0.5 rounded-full {statusBadge(run.status)} font-medium uppercase tracking-wide">
                  {run.status}
                </span>
                <span class="text-xs text-gray-600 dark:text-gray-300 flex-1">
                  {run.total_models} models
                  <span class="text-gray-400">|</span>
                  <span class="text-green-600 dark:text-green-400">{run.succeeded} ok</span>
                  {#if run.failed > 0}
                    <span class="text-gray-400">|</span>
                    <span class="text-red-600 dark:text-red-400">{run.failed} failed</span>
                  {/if}
                  {#if run.skipped > 0}
                    <span class="text-gray-400">|</span>
                    <span class="text-gray-500">{run.skipped} skipped</span>
                  {/if}
                </span>
                <span class="text-[10px] text-gray-400">{formatDate(run.started_at)}</span>
              </button>

              {#if expandedRunId === run.id && runResults[run.id]}
                <div class="border-t border-gray-200 dark:border-gray-700">
                  {#each runResults[run.id] as result (result.id)}
                    <div class="flex items-center gap-3 px-4 py-2 text-xs border-b border-gray-100 dark:border-gray-800 last:border-0">
                      <span class="shrink-0">
                        {#if result.status === 'success'}
                          <CheckCircle size={14} class="text-green-500" />
                        {:else if result.status === 'error'}
                          <XCircle size={14} class="text-red-500" />
                        {:else if result.status === 'skipped'}
                          <SkipForward size={14} class="text-gray-400" />
                        {:else}
                          <Clock size={14} class="text-blue-400" />
                        {/if}
                      </span>
                      <span class="font-medium text-gray-700 dark:text-gray-300 min-w-[120px]">{result.model_name}</span>
                      <span class="text-[10px] px-1.5 py-0.5 rounded-full {statusBadge(result.status)}">{result.status}</span>
                      <span class="text-gray-400">{result.elapsed_ms}ms</span>
                      {#if result.error}
                        <span class="text-red-500 truncate flex-1" title={result.error}>{result.error}</span>
                      {/if}
                    </div>
                  {/each}
                </div>
              {/if}
            </div>
          {/each}
        </div>
      {/if}
    </div>
  </div>
{/if}

<!-- ─── Schedule Overlay ─────────────────────────────────────────── -->
{#if showSchedule}
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div
    class="fixed inset-0 z-50 flex items-center justify-center bg-black/40"
    role="dialog"
    tabindex="-1"
    onkeydown={(e: KeyboardEvent) => { if (e.key === 'Escape') showSchedule = false }}
    onclick={() => { showSchedule = false }}
  >
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div
      class="bg-white dark:bg-gray-900 rounded-lg shadow-xl w-full max-w-md border border-gray-200 dark:border-gray-700"
      onclick={(e: MouseEvent) => e.stopPropagation()}
      onkeydown={() => {}}
    >
      <div class="flex items-center gap-2 px-4 py-3 border-b border-gray-200 dark:border-gray-700">
        <Timer size={16} class="text-orange-500" />
        <h3 class="text-sm font-semibold text-gray-800 dark:text-gray-200 flex-1">
          Pipeline Schedule
          {#if scheduleAnchorId}
            {@const anchorModel = modelById.get(scheduleAnchorId)}
            {#if anchorModel}
              <span class="font-normal text-gray-400 dark:text-gray-500 ml-1">({anchorModel.name})</span>
            {/if}
          {/if}
        </h3>
        <button onclick={() => { showSchedule = false }} class="text-gray-400 hover:text-gray-600 dark:hover:text-gray-300">
          <X size={16} />
        </button>
      </div>

      <div class="p-4 space-y-4">
        <!-- Cron input -->
        <p class="text-[11px] text-gray-400 dark:text-gray-500">Runs the models in this pipeline group in dependency order on this schedule.</p>
        <div>
          <label for="sched-cron" class="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">Cron Expression (5-field)</label>
          <input
            id="sched-cron"
            type="text"
            bind:value={schedCron}
            placeholder="0 */6 * * *"
            class="w-full text-sm border border-gray-300 dark:border-gray-600 rounded px-3 py-1.5 bg-transparent text-gray-800 dark:text-gray-200 focus:border-orange-400 focus:outline-none font-mono"
          />
        </div>

        <!-- Presets -->
        <div class="flex flex-wrap gap-1.5">
          {#each [
            { label: 'Every hour', cron: '0 * * * *' },
            { label: 'Every 6h', cron: '0 */6 * * *' },
            { label: 'Daily midnight', cron: '0 0 * * *' },
            { label: 'Weekly Mon 2am', cron: '0 2 * * 1' },
          ] as preset}
            <button
              onclick={() => { schedCron = preset.cron }}
              class="text-[10px] px-2 py-1 rounded border transition-colors
                {schedCron === preset.cron
                  ? 'border-orange-400 bg-orange-50 dark:bg-orange-900/20 text-orange-600 dark:text-orange-400'
                  : 'border-gray-300 dark:border-gray-600 text-gray-500 dark:text-gray-400 hover:border-gray-400'}"
            >
              {preset.label}
            </button>
          {/each}
        </div>

        <!-- Status info -->
        {#if schedule}
          <div class="text-xs space-y-1 text-gray-500 dark:text-gray-400 bg-gray-50 dark:bg-gray-800/50 rounded p-3">
            <div class="flex justify-between">
              <span>Status</span>
              <span class="font-medium {schedule.last_status === 'success' ? 'text-green-600 dark:text-green-400' : schedule.last_status === 'error' ? 'text-red-600 dark:text-red-400' : ''}">
                {schedule.last_status ?? 'pending'}
              </span>
            </div>
            <div class="flex justify-between">
              <span>Next run</span>
              <span>{formatDate(schedule.next_run_at)}</span>
            </div>
            <div class="flex justify-between">
              <span>Last run</span>
              <span>{formatDate(schedule.last_run_at)}</span>
            </div>
            {#if schedule.last_error}
              <div class="text-red-500 dark:text-red-400 mt-1 text-[10px] break-all">{schedule.last_error}</div>
            {/if}
          </div>
        {/if}

        <!-- Actions -->
        <div class="flex items-center gap-2 pt-2">
          <button
            onclick={handleSaveSchedule}
            disabled={schedSaving || !schedCron.trim()}
            class="flex items-center gap-1 text-xs px-3 py-1.5 rounded bg-orange-500 text-white hover:bg-orange-600 disabled:opacity-50 transition-colors font-medium"
          >
            <Save size={12} /> {schedSaving ? 'Saving...' : 'Save'}
          </button>
          {#if schedule}
            <button
              onclick={handleDeleteSchedule}
              disabled={schedSaving}
              class="flex items-center gap-1 text-xs px-3 py-1.5 rounded border border-red-300 dark:border-red-700 text-red-600 dark:text-red-400 hover:bg-red-50 dark:hover:bg-red-900/20 disabled:opacity-50 transition-colors"
            >
              <Trash2 size={12} /> Remove
            </button>
          {/if}
          <div class="flex-1"></div>
          <button
            onclick={() => { showSchedule = false }}
            class="text-xs px-3 py-1.5 rounded border border-gray-300 dark:border-gray-600 text-gray-600 dark:text-gray-400 hover:bg-gray-100 dark:hover:bg-gray-800 transition-colors"
          >
            Close
          </button>
        </div>
      </div>
    </div>
  </div>
{/if}

<ConfirmDialog
  open={confirmDeleteOpen}
  title="Delete Model"
  description={`Are you sure you want to delete "${pendingDeleteName}"? This cannot be undone.`}
  confirmLabel="Delete"
  destructive
  loading={confirmDeleteLoading}
  onconfirm={confirmDelete}
  oncancel={() => { confirmDeleteOpen = false; pendingDeleteId = ''; pendingDeleteName = '' }}
/>

<ContextMenu
  open={!!contextMenu}
  x={contextMenu?.x ?? 0}
  y={contextMenu?.y ?? 0}
  items={getContextItems()}
  onclose={closeContextMenu}
/>
