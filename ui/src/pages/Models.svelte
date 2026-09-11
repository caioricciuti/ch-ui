<script lang="ts">
  import { onMount } from 'svelte'
  import type { Model, ModelRun, ModelRunResult, ModelSchedule, DAGNode, DAGEdge, Pipeline } from '../lib/types/models'
  import * as api from '../lib/api/models'
  import { triggerGitHubSync, getGitHubIntegration } from '../lib/api/github'
  import { isProActive } from '../lib/stores/license.svelte'
  import { getSession } from '../lib/stores/session.svelte'
  import { refreshModelCache } from '../lib/editor/completions'
  import { formatDate } from '../lib/utils/format'
  import { success as toastSuccess, error as toastError } from '../lib/stores/toast.svelte'
  import { openModelTab } from '../lib/stores/tabs.svelte'
  import ConfirmDialog from '../lib/components/common/ConfirmDialog.svelte'
  import ContextMenu from '../lib/components/common/ContextMenu.svelte'
  import PageHeader from '../lib/components/common/PageHeader.svelte'
  import Button from '../lib/components/common/Button.svelte'
  import Badge from '../lib/components/common/Badge.svelte'
  import EmptyState from '../lib/components/common/EmptyState.svelte'
  import Modal from '../lib/components/common/Modal.svelte'
  import FormField from '../lib/components/common/FormField.svelte'
  import Input from '../lib/components/common/Input.svelte'
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
    CloudDownload,
  } from 'lucide-svelte'

  // ── State ──────────────────────────────────────────────────────────

  let models = $state<Model[]>([])
  let loading = $state(true)
  let syncing = $state(false)
  let hasGitHubIntegration = $state(false)

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
    const items: ContextMenuItem[] = [
      { id: 'open', label: 'Open', icon: Eye, onSelect: () => selectModel(m.id) },
    ]
    if (m.source !== 'github') {
      items.push(
        { id: 'sep1', separator: true },
        { id: 'delete', label: 'Delete', icon: Trash2, danger: true, onSelect: () => handleDelete(m.id) },
      )
    }
    return items
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
    checkGitHubIntegration()
  })

  async function checkGitHubIntegration() {
    if (!isProActive()) return
    try {
      const session = getSession()
      if (!session) return
      const integration = await getGitHubIntegration(session.connectionId)
      hasGitHubIntegration = !!(integration?.enabled && integration?.has_pat)
    } catch { /* ignore */ }
  }

  async function handleGitHubSync() {
    const session = getSession()
    if (!session || syncing) return
    syncing = true
    try {
      const result = await triggerGitHubSync(session.connectionId)
      const parts: string[] = []
      if (result.created > 0) parts.push(`${result.created} created`)
      if (result.updated > 0) parts.push(`${result.updated} updated`)
      if (result.deleted > 0) parts.push(`${result.deleted} deleted`)
      if (result.unchanged > 0) parts.push(`${result.unchanged} unchanged`)
      toastSuccess(parts.length > 0 ? `Sync complete: ${parts.join(', ')}` : 'Already up to date')
      await loadModels()
      loadDAG()
      loadPipelines()
    } catch (e: unknown) {
      toastError((e as Error).message || 'Sync failed')
    } finally {
      syncing = false
    }
  }

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

  type StatusTone = 'neutral' | 'success' | 'warning' | 'danger' | 'info'

  function statusTone(status: string): StatusTone {
    switch (status) {
      case 'success': return 'success'
      case 'error': return 'danger'
      case 'partial': return 'warning'
      case 'running': return 'info'
      default: return 'neutral'
    }
  }

  function statusDot(status: string): string {
    switch (status) {
      case 'success': return 'bg-success'
      case 'error': return 'bg-danger'
      default: return 'bg-fg-4'
    }
  }

  const SCHEDULE_PRESETS = [
    { label: 'Every hour', cron: '0 * * * *' },
    { label: 'Every 6h', cron: '0 */6 * * *' },
    { label: 'Daily midnight', cron: '0 0 * * *' },
    { label: 'Weekly Mon 2am', cron: '0 2 * * 1' },
  ]
</script>

<div class="flex h-full min-h-0 flex-col overflow-hidden">
  <PageHeader title="Models" subtitle="SQL transformations that run in dependency order">
    {#snippet meta()}
      {#if !loading}
        <Badge tone="neutral">{models.length} model{models.length !== 1 ? 's' : ''}</Badge>
      {/if}
    {/snippet}
    {#snippet actions()}
      <Button
        size="sm"
        variant="outline"
        onclick={handleRunAll}
        disabled={running || models.length === 0}
        title="Run all models in dependency order"
      >
        <Play size={13} /> {running ? 'Running...' : 'Run pipeline'}
      </Button>
      <Button size="sm" variant="outline" onclick={openDAG} disabled={models.length === 0} title="Dependency graph">
        <GitBranch size={13} /> DAG
      </Button>
      <Button size="sm" variant="outline" onclick={openHistory} title="Run history">
        <History size={13} /> History
      </Button>
      {#if hasGitHubIntegration}
        <Button size="sm" variant="outline" onclick={handleGitHubSync} disabled={syncing} title="Sync models from GitHub">
          <CloudDownload size={13} class={syncing ? 'animate-pulse' : ''} /> {syncing ? 'Syncing...' : 'Sync GitHub'}
        </Button>
      {/if}
      <Button size="sm" onclick={handleCreate}>
        <Plus size={14} /> New model
      </Button>
    {/snippet}
  </PageHeader>

  <!-- ─── Content ─────────────────────────────────────────────────── -->
  <div class="min-h-0 flex-1 overflow-auto">
    {#if loading}
      <div class="flex h-full items-center justify-center text-[13px] text-fg-3">Loading...</div>
    {:else if models.length === 0}
      <div class="flex h-full items-center justify-center">
        <EmptyState
          icon={Boxes}
          title="No models yet"
          description="Models are SQL transformations that form a pipeline. They can reference each other with $ref(model_name) and run in dependency order."
          primary={{ label: 'Create your first model', onclick: handleCreate }}
        />
      </div>
    {:else}
      {#if !infoDismissed}
        <div class="mx-5 mt-5 flex items-start gap-2.5 rounded-md bg-info-soft px-3 py-2.5 text-xs leading-relaxed text-fg-2">
          <Info size={14} class="mt-0.5 shrink-0 text-info" />
          <p class="flex-1">
            Models are SQL transformations that form a pipeline. Use <code class="rounded-sm bg-surface-2 px-1 py-0.5 font-mono text-[11px] text-fg">$ref(model_name)</code> to reference other models.
            <span class="font-semibold">Run pipeline</span> executes all models in dependency order; if a model fails, its dependents are skipped.
          </p>
          <button onclick={dismissInfo} class="mt-0.5 shrink-0 text-fg-4 hover:text-fg" title="Dismiss" aria-label="Dismiss">
            <X size={14} />
          </button>
        </div>
      {/if}
      <div class="space-y-4 p-5">
        {#each pipelines as pipeline (pipeline.anchor_model_id)}
          {@const pipelineModels = pipeline.model_ids.map(id => modelById.get(id)).filter((m): m is Model => !!m)}
          {#if pipelineModels.length > 0}
            <div class="overflow-hidden rounded-lg border border-edge-subtle bg-surface">
              <!-- Pipeline header -->
              <div class="flex items-center gap-2.5 border-b border-edge-subtle px-4 py-2">
                <Boxes size={14} class="shrink-0 text-fg-3" />
                <span class="text-xs font-medium text-fg-2">
                  {pipelineModels.length} model{pipelineModels.length !== 1 ? 's' : ''}
                </span>
                <div class="flex-1"></div>
                <Button
                  size="xs"
                  variant="ghost"
                  onclick={() => handleRunPipeline(pipeline.anchor_model_id)}
                  disabled={runningPipeline === pipeline.anchor_model_id || running}
                  title="Run this pipeline"
                >
                  <Play size={11} />
                  {runningPipeline === pipeline.anchor_model_id ? 'Running...' : 'Run'}
                </Button>
                <Button
                  size="xs"
                  variant={pipeline.schedule ? 'secondary' : 'ghost'}
                  class={pipeline.schedule ? 'font-mono' : ''}
                  onclick={() => openPipelineSchedule(pipeline.anchor_model_id, pipeline.schedule)}
                  title={pipeline.schedule ? `Schedule: ${pipeline.schedule.cron}` : 'No schedule'}
                >
                  <Timer size={11} />
                  {pipeline.schedule ? pipeline.schedule.cron : 'No schedule'}
                </Button>
              </div>
              <!-- Model cards grid -->
              <div class="grid grid-cols-[repeat(auto-fill,minmax(240px,1fr))] gap-3 p-3">
                {#each pipelineModels as model (model.id)}
                  <!-- svelte-ignore a11y_no_static_element_interactions -->
                  <div
                    class="group relative flex cursor-pointer flex-col gap-2 rounded-md border border-edge-subtle bg-surface p-3.5 transition-colors hover:border-edge-strong hover:bg-hover"
                    onclick={() => selectModel(model.id)}
                    oncontextmenu={(e) => openContextMenu(e, model)}
                    onkeydown={(e: KeyboardEvent) => { if (e.key === 'Enter') selectModel(model.id) }}
                    role="button"
                    tabindex="0"
                  >
                    <div class="flex items-center gap-2">
                      {#if model.materialization === 'table'}
                        <Table2 size={14} class="shrink-0 text-accent" />
                      {:else}
                        <Eye size={14} class="shrink-0 text-info" />
                      {/if}
                      <span class="flex-1 truncate text-[13px] font-semibold text-fg">{model.name}</span>
                      {#if model.source === 'github'}
                        <span title="Managed by GitHub: edit in your repository"><GitBranch size={12} class="shrink-0 text-fg-3" /></span>
                      {/if}
                      <span class="h-2 w-2 shrink-0 rounded-full {statusDot(model.status)}" title={model.status}></span>
                      <button
                        onclick={(e) => openContextMenuFromButton(e, model)}
                        class="rounded-sm p-1 text-fg-4 opacity-0 transition-colors hover:bg-active hover:text-fg group-hover:opacity-100 focus-visible:opacity-100"
                        title="More actions"
                        aria-label="More actions"
                      >
                        <MoreHorizontal size={15} />
                      </button>
                    </div>
                    <div class="flex items-center gap-2 text-[11px] text-fg-4">
                      <Badge tone="neutral">{model.materialization}</Badge>
                      <span class="truncate">{model.target_database}</span>
                      {#if model.last_run_at}
                        <span class="ml-auto shrink-0" title="Last run">{formatDate(model.last_run_at)}</span>
                      {/if}
                    </div>
                    {#if upstreamMap.get(model.id)?.length}
                      <div class="flex flex-wrap items-center gap-1.5">
                        <GitBranch size={11} class="shrink-0 text-fg-4" />
                        {#each upstreamMap.get(model.id)! as dep}
                          <Badge tone="brand">{dep}</Badge>
                        {/each}
                      </div>
                    {/if}
                    {#if model.last_error}
                      <p class="truncate text-[11px] text-danger" title={model.last_error}>{model.last_error}</p>
                    {:else if model.description}
                      <p class="truncate text-[11px] text-fg-4">{model.description}</p>
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
    <div class="flex shrink-0 items-center gap-2 border-t border-edge-subtle bg-surface px-5 py-2 text-xs text-fg-3">
      <Timer size={13} class="shrink-0 text-fg-4" />
      {#if scheduledPipelines.length > 0}
        <span>{scheduledPipelines.length} pipeline{scheduledPipelines.length !== 1 ? 's' : ''} scheduled</span>
        {#each scheduledPipelines as sp}
          {@const anchorModel = modelById.get(sp.anchor_model_id)}
          <span class="text-fg-4">·</span>
          <span class="font-mono text-fg-2" title={anchorModel?.name ?? sp.anchor_model_id}>
            {sp.schedule?.cron}
          </span>
          {#if sp.schedule?.last_status}
            <span class="font-medium {sp.schedule.last_status === 'success' ? 'text-success' : sp.schedule.last_status === 'error' ? 'text-danger' : ''}">
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
  <div class="fixed inset-0 z-50 flex flex-col bg-canvas" role="dialog" tabindex="-1">
    <div class="flex h-12 shrink-0 items-center gap-3 border-b border-edge-subtle px-5">
      <h2 class="flex-1 text-[15px] font-semibold tracking-[-0.01em] text-fg">Dependency graph</h2>
      <Button size="sm" variant="ghost" onclick={loadDAG}>
        <RefreshCw size={12} /> Refresh
      </Button>
      <Button size="sm" variant="outline" onclick={() => { showDAG = false }}>Close</Button>
    </div>
    <div class="min-h-0 flex-1">
      {#if dagNodes.length === 0}
        <div class="flex h-full items-center justify-center text-[13px] text-fg-3">
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
  <div class="fixed inset-0 z-50 flex flex-col bg-canvas" role="dialog" tabindex="-1">
    <div class="flex h-12 shrink-0 items-center gap-3 border-b border-edge-subtle px-5">
      <h2 class="flex-1 text-[15px] font-semibold tracking-[-0.01em] text-fg">Run history</h2>
      <Button size="sm" variant="ghost" onclick={loadRuns}>
        <RefreshCw size={12} /> Refresh
      </Button>
      <Button size="sm" variant="outline" onclick={() => { showHistory = false }}>Close</Button>
    </div>
    <div class="min-h-0 flex-1 overflow-auto">
      {#if runs.length === 0}
        <EmptyState icon={History} title="No runs yet" description="Run the pipeline and each run shows up here with per-model results." />
      {:else}
        <div class="mx-auto w-full max-w-6xl space-y-2 px-5 py-5">
          {#each runs as run (run.id)}
            <div class="overflow-hidden rounded-lg border border-edge-subtle bg-surface">
              <button
                onclick={() => toggleRunExpand(run.id)}
                class="flex w-full items-center gap-3 px-4 py-2.5 text-left transition-colors hover:bg-hover"
              >
                {#if expandedRunId === run.id}
                  <ChevronDown size={14} class="shrink-0 text-fg-4" />
                {:else}
                  <ChevronRight size={14} class="shrink-0 text-fg-4" />
                {/if}
                <Badge tone={statusTone(run.status)} class="uppercase tracking-wide">{run.status}</Badge>
                <span class="flex-1 text-xs text-fg-2">
                  {run.total_models} models
                  <span class="text-fg-4">|</span>
                  <span class="text-success">{run.succeeded} ok</span>
                  {#if run.failed > 0}
                    <span class="text-fg-4">|</span>
                    <span class="text-danger">{run.failed} failed</span>
                  {/if}
                  {#if run.skipped > 0}
                    <span class="text-fg-4">|</span>
                    <span class="text-fg-3">{run.skipped} skipped</span>
                  {/if}
                </span>
                <span class="text-[11px] tabular-nums text-fg-4">{formatDate(run.started_at)}</span>
              </button>

              {#if expandedRunId === run.id && runResults[run.id]}
                <div class="border-t border-edge-subtle">
                  {#each runResults[run.id] as result (result.id)}
                    <div class="flex items-center gap-3 border-b border-edge-subtle px-4 py-2 text-xs last:border-0">
                      <span class="shrink-0">
                        {#if result.status === 'success'}
                          <CheckCircle size={14} class="text-success" />
                        {:else if result.status === 'error'}
                          <XCircle size={14} class="text-danger" />
                        {:else if result.status === 'skipped'}
                          <SkipForward size={14} class="text-fg-4" />
                        {:else}
                          <Clock size={14} class="text-info" />
                        {/if}
                      </span>
                      <span class="min-w-[120px] font-medium text-fg-2">{result.model_name}</span>
                      <Badge tone={statusTone(result.status)}>{result.status}</Badge>
                      <span class="tabular-nums text-fg-4">{result.elapsed_ms}ms</span>
                      {#if result.error}
                        <span class="flex-1 truncate text-danger" title={result.error}>{result.error}</span>
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

<!-- ─── Schedule dialog ──────────────────────────────────────────── -->
<Modal
  open={showSchedule}
  title="Pipeline schedule"
  description={scheduleAnchorId && modelById.get(scheduleAnchorId)
    ? `Runs the models grouped under ${modelById.get(scheduleAnchorId)?.name} in dependency order.`
    : 'Runs the models in this pipeline group in dependency order.'}
  size="sm"
  onclose={() => { showSchedule = false }}
>
  <div class="space-y-4">
    <FormField label="Cron expression (5-field)" for="sched-cron">
      <Input id="sched-cron" bind:value={schedCron} placeholder="0 */6 * * *" mono spellcheck={false} />
    </FormField>

    <div class="flex flex-wrap gap-1.5">
      {#each SCHEDULE_PRESETS as preset}
        <Button
          size="xs"
          variant={schedCron === preset.cron ? 'secondary' : 'outline'}
          onclick={() => { schedCron = preset.cron }}
        >
          {preset.label}
        </Button>
      {/each}
    </div>

    {#if schedule}
      <div class="space-y-1 rounded-md bg-surface-2 p-3 text-xs text-fg-3">
        <div class="flex justify-between">
          <span>Status</span>
          <span class="font-medium {schedule.last_status === 'success' ? 'text-success' : schedule.last_status === 'error' ? 'text-danger' : 'text-fg-2'}">
            {schedule.last_status ?? 'pending'}
          </span>
        </div>
        <div class="flex justify-between">
          <span>Next run</span>
          <span class="tabular-nums text-fg-2">{formatDate(schedule.next_run_at)}</span>
        </div>
        <div class="flex justify-between">
          <span>Last run</span>
          <span class="tabular-nums text-fg-2">{formatDate(schedule.last_run_at)}</span>
        </div>
        {#if schedule.last_error}
          <div class="mt-1 break-all text-[11px] text-danger">{schedule.last_error}</div>
        {/if}
      </div>
    {/if}
  </div>

  {#snippet footer()}
    {#if schedule}
      <Button size="sm" variant="ghost" class="mr-auto text-danger hover:text-danger" onclick={handleDeleteSchedule} disabled={schedSaving}>
        <Trash2 size={12} /> Remove
      </Button>
    {/if}
    <Button size="sm" variant="outline" onclick={() => { showSchedule = false }}>Close</Button>
    <Button size="sm" onclick={handleSaveSchedule} disabled={!schedCron.trim()} loading={schedSaving}>
      <Save size={12} /> Save
    </Button>
  {/snippet}
</Modal>

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
