<script lang="ts">
  import { onMount } from 'svelte'
  import type { Materialization, Model, ModelPipeline, ModelRun, ModelRunResult, DAGNode, DAGEdge, RunOutcome, RunScope } from '../lib/types/models'
  import * as api from '../lib/api/models'
  import { refreshModelCache } from '../lib/editor/completions'
  import { formatDate, formatDuration } from '../lib/utils/format'
  import { success as toastSuccess, error as toastError } from '../lib/stores/toast.svelte'
  import { openModelTab } from '../lib/stores/tabs.svelte'
  import ConfirmDialog from '../lib/components/common/ConfirmDialog.svelte'
  import ContextMenu from '../lib/components/common/ContextMenu.svelte'
  import PageHeader from '../lib/components/common/PageHeader.svelte'
  import { docsURL } from '../lib/routes'
  import Button from '../lib/components/common/Button.svelte'
  import Badge from '../lib/components/common/Badge.svelte'
  import EmptyState from '../lib/components/common/EmptyState.svelte'
  import Skeleton from '../lib/components/common/Skeleton.svelte'
  import Sheet from '../lib/components/common/Sheet.svelte'
  import DataTable, { type DataColumn } from '../lib/components/common/DataTable.svelte'
  import { trapFocus } from '../lib/utils/focus-trap'
  import FormField from '../lib/components/common/FormField.svelte'
  import Input from '../lib/components/common/Input.svelte'
  import Select from '../lib/components/common/Select.svelte'
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
  import GitHubSyncButton from '../lib/components/models/GitHubSyncButton.svelte'
  import PipelineSheet from '../lib/components/models/PipelineSheet.svelte'
  import TestOutcomesTable from '../lib/components/models/TestOutcomesTable.svelte'
  import {
    MATERIALIZATIONS,
    materializationInfo,
    parseTestOutcomes,
    supportsFullRefresh,
  } from '../lib/components/models/materializations'
  import { getTheme } from '../lib/stores/theme.svelte'
  import {
    Boxes,
    Plus,
    Play,
    Trash2,
    RefreshCw,
    Eye,
    MoreHorizontal,
    GitBranch,
    History,
    X,
    Info,
    Code,
    Pencil,
    ArrowUpFromLine,
    ArrowDownFromLine,
    RotateCcw,
    FlaskConical,
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
  // Run whose per-model results are open in the run sheet.
  let selectedRunId = $state<string | null>(null)
  let selectedRun = $derived(selectedRunId ? runs.find((r) => r.id === selectedRunId) ?? null : null)
  let runResults = $state<Record<string, ModelRunResult[]>>({})
  // Result whose stored SQL or test outcomes are shown under the results table.
  let resultDetail = $state<{ id: string; kind: 'sql' | 'tests' } | null>(null)
  let detailResult = $derived.by(() => {
    const detail = resultDetail
    if (!detail || !selectedRunId) return null
    return runResults[selectedRunId]?.find((r) => r.id === detail.id) ?? null
  })

  // New model sheet
  let showCreate = $state(false)
  let createName = $state('')
  let createDatabase = $state('default')
  let createMaterialization = $state<Materialization>('view')
  let createError = $state('')
  let creating = $state(false)
  const MODEL_NAME_RE = /^[A-Za-z_][A-Za-z0-9_]*$/
  const MATERIALIZATION_OPTIONS = MATERIALIZATIONS.map((m) => ({
    value: m.value,
    label: `${m.label}: ${m.description}`,
  }))

  // Run state
  let running = $state(false)
  let runningModelId = $state<string | null>(null)

  // Info banner
  let infoDismissed = $state(localStorage.getItem('chui-pipeline-info-dismissed') === '1')

  function dismissInfo() {
    infoDismissed = true
    localStorage.setItem('chui-pipeline-info-dismissed', '1')
  }

  // Named pipelines
  let pipelines = $state<ModelPipeline[]>([])
  // Models no pipeline picks, per the server; null until it answered.
  let serverUngrouped = $state<string[] | null>(null)
  let runningPipeline = $state<string | null>(null)

  // Pipeline sheet
  let pipelineSheetOpen = $state(false)
  let editingPipeline = $state<ModelPipeline | null>(null)

  // Pipeline delete confirm
  let pendingPipelineDelete = $state<ModelPipeline | null>(null)
  let deletingPipeline = $state(false)

  // Derived: model lookup by id
  let modelById = $derived(new Map(models.map(m => [m.id, m])))
  let pipelineById = $derived(new Map(pipelines.map(p => [p.id, p])))

  // Models outside every pipeline: the server's list plus models created
  // since it answered.
  let ungroupedModels = $derived.by(() => {
    const grouped = new Set(pipelines.flatMap(p => p.model_ids))
    const fromServer = new Set(serverUngrouped ?? [])
    return models.filter(m => fromServer.has(m.id) || !grouped.has(m.id))
  })

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

  function openContextMenuAtPointer(event: MouseEvent, model: Model) {
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
    const busy = runningModelId !== null || running
    const items: ContextMenuItem[] = [
      { id: 'open', label: 'Open', icon: Eye, onSelect: () => selectModel(m.id) },
      { id: 'sep0', separator: true },
      { id: 'run-up', label: 'Run with upstream', icon: ArrowUpFromLine, disabled: busy, onSelect: () => handleRunModel(m, {}) },
      { id: 'run-down', label: 'Run downstream', icon: ArrowDownFromLine, disabled: busy, onSelect: () => handleRunModel(m, { scope: 'downstream' }) },
    ]
    if (supportsFullRefresh(m.materialization)) {
      items.push({ id: 'full', label: 'Full refresh', icon: RotateCcw, disabled: busy, onSelect: () => handleRunModel(m, { fullRefresh: true }) })
    }
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
    model: ModelNode as unknown as NodeTypes[string],
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
      toastError((e instanceof Error ? e.message : '') || 'Failed to load models')
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
        style: 'stroke: var(--accent); stroke-width: 2px;',
      }))
    } catch (e: unknown) {
      toastError((e instanceof Error ? e.message : '') || 'Failed to load DAG')
    }
  }

  async function loadRuns() {
    try {
      const res = await api.listModelRuns()
      runs = res.runs ?? []
    } catch (e: unknown) {
      toastError((e instanceof Error ? e.message : '') || 'Failed to load runs')
    }
  }

  async function loadRunResults(runId: string) {
    if (runResults[runId]) return
    try {
      const res = await api.getModelRun(runId)
      runResults[runId] = res.results ?? []
    } catch (e: unknown) {
      toastError((e instanceof Error ? e.message : '') || 'Failed to load run results')
    }
  }

  async function loadPipelines() {
    try {
      const res = await api.listPipelines()
      pipelines = res.pipelines ?? []
      serverUngrouped = res.ungrouped_model_ids ?? []
    } catch (e: unknown) {
      toastError((e instanceof Error ? e.message : '') || 'Failed to load pipelines')
    }
  }

  // ── Actions ────────────────────────────────────────────────────────

  async function handleRunPipeline(p: ModelPipeline) {
    runningPipeline = p.id
    try {
      const res = await api.runPipeline(p.id)
      await showRunOutcome(res, `Pipeline ${p.name} finished`)
      await loadPipelines()
    } catch (e: unknown) {
      toastError((e instanceof Error ? e.message : '') || 'Failed to run pipeline')
    } finally {
      runningPipeline = null
    }
  }

  function openNewPipeline() {
    editingPipeline = null
    pipelineSheetOpen = true
  }

  function openEditPipeline(p: ModelPipeline) {
    editingPipeline = p
    pipelineSheetOpen = true
  }

  async function confirmDeletePipeline() {
    if (!pendingPipelineDelete) return
    deletingPipeline = true
    try {
      await api.deletePipeline(pendingPipelineDelete.id)
      toastSuccess('Pipeline deleted')
      await loadPipelines()
    } catch (e: unknown) {
      toastError((e instanceof Error ? e.message : '') || 'Failed to delete pipeline')
    } finally {
      deletingPipeline = false
      pendingPipelineDelete = null
    }
  }

  async function handleRunModel(m: Model, opts: { scope?: RunScope; fullRefresh?: boolean }) {
    runningModelId = m.id
    try {
      const res = await api.runSingleModel(m.id, opts)
      await showRunOutcome(res, opts.fullRefresh ? `Full refresh of ${m.name} finished` : 'Model run finished')
    } catch (e: unknown) {
      toastError((e instanceof Error ? e.message : '') || 'Failed to run model')
    } finally {
      runningModelId = null
    }
  }

  function handleCreate() {
    const existing = new Set(models.map(m => m.name))
    let name = 'new_model'
    let i = 1
    while (existing.has(name)) {
      name = `new_model_${i++}`
    }
    createName = name
    createDatabase = 'default'
    createMaterialization = 'view'
    createError = ''
    showCreate = true
  }

  async function submitCreate() {
    const name = createName.trim()
    if (!MODEL_NAME_RE.test(name)) {
      createError = 'Use letters, digits and underscores, starting with a letter or underscore.'
      return
    }
    if (models.some(m => m.name === name)) {
      createError = 'A model with this name already exists.'
      return
    }
    creating = true
    createError = ''
    try {
      const res = await api.createModel({
        name,
        target_database: createDatabase.trim() || 'default',
        materialization: createMaterialization,
        sql_body: 'SELECT 1 AS id',
      })
      refreshModelCache()
      toastSuccess('Model created')
      showCreate = false
      await loadModels()
      await loadPipelines()
      openModelTab(res.model)
    } catch (e: unknown) {
      createError = (e instanceof Error ? e.message : '') || 'Failed to create model'
    } finally {
      creating = false
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
      toastError((e instanceof Error ? e.message : '') || 'Failed to delete model')
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
      await showRunOutcome(res, 'Model run finished')
    } catch (e: unknown) {
      toastError((e instanceof Error ? e.message : '') || 'Failed to run models')
    } finally {
      running = false
    }
  }

  // Runs are synchronous: the response arrives when the run has finished.
  async function showRunOutcome(res: RunOutcome, successMessage: string) {
    if (res.status === 'success') {
      toastSuccess(successMessage)
    } else {
      toastError(res.error || `Run finished with status ${res.status}`)
    }
    await loadModels()
    await loadRuns()
    selectedRunId = res.run_id
    resultDetail = null
    await loadRunResults(res.run_id)
    showHistory = true
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

  function openRun(runId: string) {
    selectedRunId = runId
    resultDetail = null
    loadRunResults(runId)
  }

  function closeRun() {
    selectedRunId = null
    resultDetail = null
  }

  function toggleResultDetail(id: string, kind: 'sql' | 'tests') {
    if (resultDetail?.id === id && resultDetail.kind === kind) {
      resultDetail = null
    } else {
      resultDetail = { id, kind }
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

  function pipelineModels(p: ModelPipeline): Model[] {
    return p.model_ids.map(id => modelById.get(id)).filter((m): m is Model => !!m)
  }

  function hasTests(r: ModelRunResult): boolean {
    return r.tests_passed + r.tests_failed > 0 || !!r.test_details
  }

  // DataTable is typed over Record rows; our row types are interfaces, so cast in and out.
  type Row = Record<string, unknown>
  const asRows = <T,>(rows: T[]): Row[] => rows as unknown as Row[]
  const asModel = (row: Row): Model => row as unknown as Model
  const asRun = (row: Row): ModelRun => row as unknown as ModelRun
  const asResult = (row: Row): ModelRunResult => row as unknown as ModelRunResult

  function runDurationMs(run: ModelRun): number | null {
    if (!run.finished_at) return null
    const ms = Date.parse(run.finished_at) - Date.parse(run.started_at)
    return Number.isFinite(ms) ? Math.max(ms, 0) : null
  }

  function runScope(run: ModelRun): string {
    const pipeline = run.pipeline_id ? pipelineById.get(run.pipeline_id) : undefined
    if (pipeline) return pipeline.name
    return run.selector || 'All models'
  }

  function runOutcome(run: ModelRun): string {
    const parts = [`${run.succeeded} ok`]
    if (run.failed > 0) parts.push(`${run.failed} failed`)
    if (run.skipped > 0) parts.push(`${run.skipped} skipped`)
    return parts.join(', ')
  }

  const runColumns: DataColumn<Row>[] = [
    { key: 'started_at', label: 'Started', mono: true, format: (v) => formatDate(v), sortValue: (r) => Date.parse(String(r.started_at)) || 0 },
    { key: 'status', label: 'Status' },
    { key: 'total_models', label: 'Models', align: 'right' },
    { key: 'succeeded', label: 'Outcome', sortable: false, format: (_v, r) => runOutcome(asRun(r)) },
    { key: 'finished_at', label: 'Duration', align: 'right', format: (_v, r) => { const ms = runDurationMs(asRun(r)); return ms == null ? '-' : formatDuration(ms) }, sortValue: (r) => runDurationMs(asRun(r)) ?? -1 },
    { key: 'pipeline_id', label: 'Scope', truncate: true, format: (_v, r) => runScope(asRun(r)), sortValue: (r) => runScope(asRun(r)) },
    { key: 'triggered_by', label: 'Triggered by', format: (v) => (v ? String(v) : '-') },
  ]

  const resultColumns: DataColumn<Row>[] = [
    { key: 'model_name', label: 'Model' },
    { key: 'status', label: 'Status' },
    { key: 'elapsed_ms', label: 'Duration', align: 'right', format: (v) => formatDuration(Number(v ?? 0)) },
    { key: 'tests_passed', label: 'Tests', sortable: false },
    { key: 'error', label: 'Error', truncate: true, sortable: false, format: (v) => (v ? String(v) : '') },
  ]

  const modelColumns: DataColumn<Row>[] = [
    { key: 'name', label: 'Model' },
    { key: 'materialization', label: 'Type' },
    { key: 'target_database', label: 'Database', mono: true },
    { key: 'depends_on', label: 'Depends on', sortable: false },
    { key: 'status', label: 'Status' },
    { key: 'last_run_at', label: 'Last run', mono: true, format: (v) => (v ? formatDate(String(v)) : '-') },
    { key: 'description', label: 'Description', truncate: true, sortable: false, format: (v, r) => String(r.last_error ?? v ?? '') },
  ]
</script>

{#snippet modelTable(rows: Model[])}
  <DataTable
    columns={modelColumns}
    rows={asRows(rows)}
    rowKey={(r) => String(r.id)}
    framed={false}
    onrowclick={(r) => selectModel(String(r.id))}
    onrowcontextmenu={(r, e) => { const m = models.find((x) => x.id === String(r.id)); if (m) openContextMenuAtPointer(e, m) }}
  >
    {#snippet cell(row, col, value)}
      {@const model = asModel(row)}
      {#if col.key === 'name'}
        {@const MatIcon = materializationInfo(model.materialization).icon}
        <span class="inline-flex items-center gap-2">
          <MatIcon size={13} class="shrink-0 text-fg-3" />
          <span class="font-medium text-fg">{model.name}</span>
          {#if model.source === 'github'}
            <span title="Managed by GitHub: edit in your repository"><GitBranch size={12} class="shrink-0 text-fg-3" /></span>
          {/if}
          {#if runningModelId === model.id}
            <Badge tone="info">running</Badge>
          {/if}
        </span>
      {:else if col.key === 'materialization'}
        <Badge tone="neutral">{materializationInfo(model.materialization).label}</Badge>
      {:else if col.key === 'depends_on'}
        {@const deps = upstreamMap.get(model.id) ?? []}
        {#if deps.length}
          <span class="inline-flex flex-wrap gap-1">
            {#each deps as dep (dep)}<Badge tone="brand">{dep}</Badge>{/each}
          </span>
        {:else}
          <span class="text-fg-4">-</span>
        {/if}
      {:else if col.key === 'status'}
        <Badge tone={statusTone(model.status)}>{model.status}</Badge>
      {:else if col.key === 'description' && model.last_error}
        <span class="text-danger">{value}</span>
      {:else}
        {value}
      {/if}
    {/snippet}
    {#snippet actions(row)}
      <Button
        icon
        size="xs"
        variant="ghost"
        aria-label="More actions"
        title="More actions"
        onclick={(e) => openContextMenuFromButton(e, asModel(row))}
      >
        <MoreHorizontal size={14} />
      </Button>
    {/snippet}
  </DataTable>
{/snippet}

<div class="flex h-full min-h-0 flex-col overflow-hidden">
  <PageHeader title="Models" subtitle="SQL transformations that run in dependency order" docs={docsURL('models')}>
    {#snippet meta()}
      {#if !loading}
        <Badge tone="neutral">{models.length} model{models.length !== 1 ? 's' : ''}</Badge>
        <Badge tone="neutral">{pipelines.length} pipeline{pipelines.length !== 1 ? 's' : ''}</Badge>
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
        <Play size={13} /> {running ? 'Running...' : 'Run all'}
      </Button>
      <Button size="sm" variant="outline" onclick={openDAG} disabled={models.length === 0} title="Dependency graph">
        <GitBranch size={13} /> DAG
      </Button>
      <Button size="sm" variant="outline" onclick={openHistory} title="Run history">
        <History size={13} /> History
      </Button>
      <GitHubSyncButton onsynced={async () => { await loadModels(); loadDAG(); loadPipelines() }} />
      <Button size="sm" variant="outline" onclick={openNewPipeline} disabled={models.length === 0}>
        <Plus size={13} /> New pipeline
      </Button>
      <Button size="sm" onclick={handleCreate}>
        <Plus size={14} /> New model
      </Button>
    {/snippet}
  </PageHeader>

  <!-- ─── Content ─────────────────────────────────────────────────── -->
  <div class="min-h-0 flex-1 overflow-auto">
    {#if loading}
      <Skeleton variant="rows" rows={6} label="Loading models" />
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
            Models are SQL transformations. Use <code class="rounded-sm bg-surface-2 px-1 py-0.5 font-mono text-[11px] text-fg">$ref(model_name)</code> to reference other models.
            A <span class="font-semibold">pipeline</span> is a named group picked by a selector, run and scheduled together.
            <span class="font-semibold">Run all</span> executes every model in dependency order; if a model fails, its dependents are skipped.
          </p>
          <button onclick={dismissInfo} class="mt-0.5 shrink-0 text-fg-4 hover:text-fg" title="Dismiss" aria-label="Dismiss">
            <X size={14} />
          </button>
        </div>
      {/if}
      <div class="space-y-4 p-5">
        {#each pipelines as pipeline (pipeline.id)}
          {@const picked = pipelineModels(pipeline)}
          {@const nextRun = pipeline.next_runs[0] ?? pipeline.next_run_at}
          <div class="overflow-hidden rounded-lg border border-edge-subtle bg-surface">
            <div class="flex flex-wrap items-center gap-x-3 gap-y-1 border-b border-edge-subtle px-4 py-2 text-xs">
              <span class="text-[13px] font-medium text-fg">{pipeline.name}</span>
              <code class="rounded-sm bg-surface-2 px-1.5 py-0.5 font-mono text-[11px] text-fg-2" title="Selector">{pipeline.selector || '*'}</code>
              <span class="text-fg-3">{picked.length} model{picked.length !== 1 ? 's' : ''}</span>
              {#if pipeline.cron}
                <span class="text-fg-4">|</span>
                <span class="font-mono text-fg-2">{pipeline.cron}</span>
                <span class="text-fg-3">{pipeline.timezone}</span>
                <Badge tone={pipeline.enabled ? 'success' : 'warning'}>{pipeline.enabled ? 'enabled' : 'paused'}</Badge>
                {#if pipeline.enabled && nextRun}
                  <span class="tabular-nums text-fg-3">Next {formatDate(nextRun)}</span>
                {/if}
              {:else}
                <span class="text-fg-4">|</span>
                <span class="text-fg-3">Not scheduled</span>
              {/if}
              {#if pipeline.last_status}
                <Badge tone={statusTone(pipeline.last_status)} title={pipeline.last_error ?? undefined}>last run {pipeline.last_status}</Badge>
              {/if}
              <div class="flex-1"></div>
              <Button
                size="xs"
                variant="ghost"
                onclick={() => handleRunPipeline(pipeline)}
                disabled={runningPipeline !== null || running || picked.length === 0}
                title="Run this pipeline now"
              >
                <Play size={11} />
                {runningPipeline === pipeline.id ? 'Running...' : 'Run'}
              </Button>
              <Button size="xs" variant="ghost" onclick={() => openEditPipeline(pipeline)} title="Edit pipeline">
                <Pencil size={11} /> Edit
              </Button>
              <Button icon size="xs" variant="ghost" onclick={() => { pendingPipelineDelete = pipeline }} aria-label="Delete pipeline" title="Delete pipeline">
                <Trash2 size={12} />
              </Button>
            </div>
            {#if pipeline.selector_error}
              <div class="border-b border-edge-subtle bg-danger-soft px-4 py-2 text-xs text-danger" role="alert">
                {pipeline.selector_error}
              </div>
            {/if}
            {#if pipeline.last_status === 'error' && pipeline.last_error}
              <div class="truncate border-b border-edge-subtle px-4 py-1.5 text-xs text-danger" title={pipeline.last_error}>
                Last run: {pipeline.last_error}
              </div>
            {/if}
            {#if picked.length > 0}
              {@render modelTable(picked)}
            {:else if !pipeline.selector_error}
              <EmptyState size="compact" title="The selector picks no models" description="Edit the pipeline to change its selector." />
            {/if}
          </div>
        {/each}

        {#if ungroupedModels.length > 0}
          <div class="overflow-hidden rounded-lg border border-edge-subtle bg-surface">
            <div class="flex flex-wrap items-center gap-x-3 gap-y-1 border-b border-edge-subtle px-4 py-2 text-xs">
              <span class="text-[13px] font-medium text-fg">{pipelines.length > 0 ? 'Ungrouped' : 'All models'}</span>
              <span class="text-fg-3">{ungroupedModels.length} model{ungroupedModels.length !== 1 ? 's' : ''}</span>
              <span class="text-fg-4">|</span>
              <span class="text-fg-3">
                {pipelines.length > 0 ? 'Not picked by any pipeline; Run all still runs them.' : 'Create a pipeline to run and schedule a group of models together.'}
              </span>
            </div>
            {@render modelTable(ungroupedModels)}
          </div>
        {/if}
      </div>
    {/if}
  </div>
</div>

<!-- ─── DAG Overlay ──────────────────────────────────────────────── -->
{#if showDAG}
  <div
    class="fixed inset-0 z-50 flex flex-col bg-canvas"
    role="dialog"
    aria-modal="true"
    aria-labelledby="models-dag-title"
    tabindex="-1"
    use:trapFocus
    onkeydown={(e: KeyboardEvent) => { if (e.key === 'Escape') { e.stopPropagation(); showDAG = false } }}
  >
    <div class="flex h-12 shrink-0 items-center gap-3 border-b border-edge-subtle px-5">
      <h2 id="models-dag-title" class="flex-1 text-[15px] font-semibold tracking-[-0.01em] text-fg">Dependency graph</h2>
      <Button size="sm" variant="ghost" onclick={loadDAG}>
        <RefreshCw size={12} /> Refresh
      </Button>
      <Button size="sm" variant="outline" onclick={() => { showDAG = false }}>Close</Button>
    </div>
    <div class="min-h-0 flex-1">
      {#if dagNodes.length === 0 && models.length === 0}
        <div class="flex h-full items-center justify-center">
          <EmptyState size="compact" title="No models to show" description="Create a model and its dependencies appear here as a graph." />
        </div>
      {:else if dagNodes.length === 0}
        <Skeleton variant="block" label="Loading dependency graph" class="p-5" />
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
  <div
    class="fixed inset-0 z-50 flex flex-col bg-canvas"
    role="dialog"
    aria-modal="true"
    aria-labelledby="models-history-title"
    tabindex="-1"
    use:trapFocus
    onkeydown={(e: KeyboardEvent) => {
      if (e.key !== 'Escape') return
      e.stopPropagation()
      // The run sheet sits inside this overlay, so Escape closes it first.
      if (selectedRunId) closeRun()
      else showHistory = false
    }}
  >
    <div class="flex h-12 shrink-0 items-center gap-3 border-b border-edge-subtle px-5">
      <h2 id="models-history-title" class="flex-1 text-[15px] font-semibold tracking-[-0.01em] text-fg">Run history</h2>
      <Button size="sm" variant="ghost" onclick={loadRuns}>
        <RefreshCw size={12} /> Refresh
      </Button>
      <Button size="sm" variant="outline" onclick={() => { showHistory = false }}>Close</Button>
    </div>
    <div class="min-h-0 flex-1">
      {#if runs.length === 0}
        <EmptyState icon={History} title="No runs yet" description="Run the pipeline and each run shows up here with per-model results." />
      {:else}
        <div class="mx-auto flex h-full w-full max-w-6xl flex-col px-5 py-5">
          <div class="min-h-0 flex-1 overflow-hidden rounded-lg border border-edge-subtle bg-surface">
            <DataTable
              fill
              columns={runColumns}
              rows={asRows(runs)}
              rowKey={(r) => String(r.id)}
              sort={{ key: 'started_at', dir: 'desc' }}
              onrowclick={(r) => openRun(String(r.id))}
            >
              {#snippet cell(_row, col, value)}
                {#if col.key === 'status'}
                  <Badge tone={statusTone(value)} class="uppercase tracking-wide">{value}</Badge>
                {:else if col.key === 'started_at'}
                  <span class="text-fg">{value}</span>
                {:else}
                  {value}
                {/if}
              {/snippet}
            </DataTable>
          </div>
        </div>
      {/if}
    </div>

    <!-- Inside the overlay so it stacks above it. -->
    <Sheet
      open={!!selectedRun}
      title={selectedRun ? formatDate(selectedRun.started_at) : ''}
      description={selectedRun ? `${runScope(selectedRun)}: ${selectedRun.total_models} models, ${runOutcome(selectedRun)}` : ''}
      size="lg"
      onclose={closeRun}
    >
      {#if selectedRun}
        {@const results = runResults[selectedRun.id]}
        <div class="space-y-4">
          {#if !results}
            <Skeleton variant="rows" rows={4} label="Loading run results" />
          {:else}
            <DataTable
              columns={resultColumns}
              rows={asRows(results)}
              rowKey={(r) => String(r.id)}
              emptyTitle="No model results"
              emptyDescription="This run has no per-model results."
            >
              {#snippet cell(row, col, value)}
                {@const result = asResult(row)}
                {#if col.key === 'model_name'}
                  <span class="font-medium text-fg">{value}</span>
                {:else if col.key === 'status'}
                  <Badge tone={statusTone(value)}>{value}</Badge>
                {:else if col.key === 'tests_passed'}
                  {#if hasTests(result)}
                    <span class="inline-flex items-center gap-1">
                      <Badge tone="success">{result.tests_passed} passed</Badge>
                      {#if result.tests_failed > 0}<Badge tone="danger">{result.tests_failed} failed</Badge>{/if}
                    </span>
                  {:else}
                    <span class="text-fg-4">-</span>
                  {/if}
                {:else if col.key === 'error'}
                  <span class="text-danger">{value}</span>
                {:else}
                  {value}
                {/if}
              {/snippet}
              {#snippet actions(row)}
                {@const result = asResult(row)}
                {#if hasTests(result)}
                  <Button
                    size="xs"
                    variant="ghost"
                    aria-expanded={resultDetail?.id === result.id && resultDetail.kind === 'tests'}
                    onclick={() => toggleResultDetail(result.id, 'tests')}
                  >
                    <FlaskConical size={12} /> Tests
                  </Button>
                {/if}
                {#if result.resolved_sql}
                  <Button
                    size="xs"
                    variant="ghost"
                    aria-expanded={resultDetail?.id === result.id && resultDetail.kind === 'sql'}
                    onclick={() => toggleResultDetail(result.id, 'sql')}
                  >
                    <Code size={12} /> SQL
                  </Button>
                {/if}
              {/snippet}
            </DataTable>
          {/if}

          {#if detailResult && resultDetail}
            <div class="overflow-hidden rounded-lg border border-edge-subtle bg-surface">
              <div class="flex h-9 items-center gap-2 border-b border-edge-subtle px-3">
                <span class="text-[13px] font-medium text-fg">{detailResult.model_name}</span>
                <span class="text-xs text-fg-3">{resultDetail.kind === 'sql' ? 'Resolved SQL' : 'Test outcomes'}</span>
                <Button icon variant="ghost" size="xs" class="ml-auto" aria-label="Close" onclick={() => { resultDetail = null }}>
                  <X size={13} />
                </Button>
              </div>
              {#if resultDetail.kind === 'tests'}
                <div class="px-3 py-3">
                  <TestOutcomesTable outcomes={parseTestOutcomes(detailResult.test_details)} />
                </div>
              {:else}
                <pre class="max-h-80 overflow-auto bg-surface-2 px-4 py-3 font-mono text-xs text-fg-2 whitespace-pre-wrap break-all">{detailResult.resolved_sql}</pre>
              {/if}
            </div>
          {/if}
        </div>
      {/if}

      {#snippet footer()}
        <Button size="sm" variant="outline" onclick={closeRun}>Close</Button>
      {/snippet}
    </Sheet>
  </div>
{/if}

<!-- ─── Pipeline sheet ───────────────────────────────────────────── -->
<PipelineSheet
  open={pipelineSheetOpen}
  pipeline={editingPipeline}
  onclose={() => { pipelineSheetOpen = false }}
  onsaved={loadPipelines}
/>

<!-- ─── New model sheet ──────────────────────────────────────────── -->
<Sheet
  open={showCreate}
  title="New model"
  description="Starts with SELECT 1 AS id. Write the real SELECT in the editor, then save and run."
  size="sm"
  onclose={() => { showCreate = false }}
>
  <form id="new-model-form" class="space-y-4" onsubmit={(e) => { e.preventDefault(); submitCreate() }}>
    <FormField label="Name" for="new-model-name" hint="Also the name of the table or view it builds." error={createError} required>
      <Input id="new-model-name" bind:value={createName} mono spellcheck={false} invalid={!!createError} required />
    </FormField>
    <FormField label="Target database" for="new-model-db">
      <Input id="new-model-db" bind:value={createDatabase} mono spellcheck={false} placeholder="default" />
    </FormField>
    <FormField label="Materialization" for="new-model-mat" hint="Engine and incremental options are set in the editor.">
      <Select
        id="new-model-mat"
        value={createMaterialization}
        options={MATERIALIZATION_OPTIONS}
        onchange={(v) => { createMaterialization = MATERIALIZATIONS.find((m) => m.value === v)?.value ?? 'view' }}
      />
    </FormField>
  </form>

  {#snippet footer()}
    <Button size="sm" variant="outline" onclick={() => { showCreate = false }}>Cancel</Button>
    <Button size="sm" type="submit" form="new-model-form" loading={creating}>
      <Plus size={12} /> Create model
    </Button>
  {/snippet}
</Sheet>

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

<ConfirmDialog
  open={!!pendingPipelineDelete}
  title="Delete pipeline"
  description={pendingPipelineDelete ? `Delete the pipeline "${pendingPipelineDelete.name}" and its schedule? Its models are kept.` : ''}
  confirmLabel="Delete"
  destructive
  loading={deletingPipeline}
  onconfirm={confirmDeletePipeline}
  oncancel={() => { pendingPipelineDelete = null }}
/>

<ContextMenu
  open={!!contextMenu}
  x={contextMenu?.x ?? 0}
  y={contextMenu?.y ?? 0}
  items={getContextItems()}
  onclose={closeContextMenu}
/>
