<script lang="ts">
  import { onMount } from 'svelte'
  import type { Model, ModelRun, ModelRunResult, DAGNode, DAGEdge } from '../lib/types/models'
  import * as api from '../lib/api/models'
  import { refreshModelCache } from '../lib/editor/completions'
  import { success as toastSuccess, error as toastError } from '../lib/stores/toast.svelte'
  import SqlEditor from '../lib/components/editor/SqlEditor.svelte'
  import ConfirmDialog from '../lib/components/common/ConfirmDialog.svelte'
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
    Table2,
    GitBranch,
    History,
    Save,
    AlertCircle,
    CheckCircle,
    XCircle,
    SkipForward,
    Clock,
    ChevronDown,
    ChevronRight,
    FileText,
  } from 'lucide-svelte'

  // ── State ──────────────────────────────────────────────────────────

  let models = $state<Model[]>([])
  let loading = $state(true)
  let selectedModelId = $state<string | null>(null)

  // Editor state
  let editModel = $state<Partial<Model>>({})
  let saving = $state(false)
  let sqlEditor = $state<SqlEditor | undefined>(undefined)
  let showDescription = $state(false)

  // DAG overlay
  let showDAG = $state(false)
  let dagNodes = $state<Node[]>([])
  let dagEdges = $state<Edge[]>([])

  // History overlay
  let showHistory = $state(false)
  let runs = $state<ModelRun[]>([])
  let expandedRunId = $state<string | null>(null)
  let runResults = $state<Record<string, ModelRunResult[]>>({})

  // Run state
  let running = $state(false)

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

  onMount(loadModels)

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
      dagNodes = (res.nodes ?? []).map((n: DAGNode) => ({
        id: n.id,
        type: 'model',
        position: n.position,
        data: n.data,
      }))
      dagEdges = (res.edges ?? []).map((e: DAGEdge) => ({
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

  // ── Actions ────────────────────────────────────────────────────────

  async function handleCreate() {
    const existing = new Set(models.map(m => m.name))
    let name = 'new_model'
    let i = 1
    while (existing.has(name)) {
      name = `new_model_${i++}`
    }
    try {
      const boilerplate = `-- Model: ${name}\n-- Write a SELECT query that defines this model.\n-- Use $ref(model_name) to reference other models.\n\nSELECT *\nFROM default.my_table\nLIMIT 100`
      const res = await api.createModel({
        name,
        target_database: 'default',
        materialization: 'view',
        sql_body: boilerplate,
      })
      refreshModelCache()
      toastSuccess('Model created')
      await loadModels()
      selectModel(res.model.id)
    } catch (e: unknown) {
      toastError((e as Error).message || 'Failed to create model')
    }
  }

  async function handleSave() {
    if (!selectedModelId) return
    saving = true
    try {
      const sqlValue = sqlEditor?.getValue() ?? editModel.sql_body ?? ''
      await api.updateModel(selectedModelId, {
        name: editModel.name,
        description: editModel.description,
        target_database: editModel.target_database,
        materialization: editModel.materialization,
        sql_body: sqlValue,
        table_engine: editModel.table_engine,
        order_by: editModel.order_by,
      })
      refreshModelCache()
      toastSuccess('Model saved')
      await loadModels()
    } catch (e: unknown) {
      toastError((e as Error).message || 'Failed to save model')
    } finally {
      saving = false
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
      if (selectedModelId === pendingDeleteId) {
        selectedModelId = null
      }
      await loadModels()
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

  async function handleRunSingle(id: string) {
    running = true
    try {
      await api.runSingleModel(id)
      toastSuccess('Model run started')
      await loadModels()
      if (selectedModelId === id) {
        const res = await api.getModel(id)
        if (res.model) editModel = { ...res.model }
      }
    } catch (e: unknown) {
      toastError((e as Error).message || 'Failed to run model')
    } finally {
      running = false
    }
  }

  // ── Navigation ─────────────────────────────────────────────────────

  function selectModel(id: string) {
    const model = models.find(m => m.id === id)
    if (!model) return
    selectedModelId = id
    editModel = { ...model }
    showDescription = !!model.description
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

<div class="flex h-full overflow-hidden">
  <!-- ─── Left Panel: Model List ─────────────────────────────────── -->
  <div class="w-60 shrink-0 flex flex-col border-r border-gray-200 dark:border-gray-700 bg-white dark:bg-gray-950">
    <!-- Header -->
    <div class="flex items-center gap-2 px-3 py-2.5 border-b border-gray-200 dark:border-gray-700">
      <Boxes size={16} class="text-orange-500 shrink-0" />
      <span class="text-sm font-semibold text-gray-800 dark:text-gray-200 flex-1">Models</span>
      <button
        onclick={handleCreate}
        class="p-1 rounded hover:bg-gray-100 dark:hover:bg-gray-800 text-gray-500 hover:text-orange-500 transition-colors"
        title="New model"
      >
        <Plus size={16} />
      </button>
    </div>

    <!-- Model list -->
    <div class="flex-1 min-h-0 overflow-y-auto">
      {#if loading}
        <div class="flex items-center justify-center py-8 text-gray-400 text-xs">Loading...</div>
      {:else if models.length === 0}
        <div class="flex flex-col items-center justify-center py-12 gap-2 text-gray-400 px-4">
          <Boxes size={32} strokeWidth={1} />
          <p class="text-xs text-center">No models yet</p>
          <button onclick={handleCreate} class="text-xs text-orange-500 hover:text-orange-600 font-medium">
            Create your first model
          </button>
        </div>
      {:else}
        <div class="py-1">
          {#each models as model (model.id)}
            <!-- svelte-ignore a11y_no_static_element_interactions -->
            <div
              class="w-full flex items-center gap-2 px-3 py-1.5 text-left transition-colors group cursor-pointer
                {selectedModelId === model.id
                  ? 'bg-orange-50 dark:bg-orange-500/10 text-orange-700 dark:text-orange-400 border-l-2 border-orange-500'
                  : 'text-gray-700 dark:text-gray-300 hover:bg-gray-50 dark:hover:bg-gray-800/50 border-l-2 border-transparent'}"
              onclick={() => selectModel(model.id)}
              onkeydown={(e: KeyboardEvent) => { if (e.key === 'Enter') selectModel(model.id) }}
              role="button"
              tabindex="0"
            >
              {#if model.materialization === 'table'}
                <Table2 size={13} class="shrink-0 opacity-60" />
              {:else}
                <Eye size={13} class="shrink-0 opacity-60" />
              {/if}
              <span class="text-xs font-medium truncate flex-1">{model.name}</span>
              <span class="w-1.5 h-1.5 rounded-full {statusDot(model.status)} shrink-0"></span>
              <button
                onclick={(e: MouseEvent) => { e.stopPropagation(); handleDelete(model.id) }}
                class="p-0.5 rounded opacity-0 group-hover:opacity-100 hover:bg-red-100 dark:hover:bg-red-900/30 text-gray-400 hover:text-red-500 transition-all"
                title="Delete"
              >
                <Trash2 size={12} />
              </button>
            </div>
          {/each}
        </div>
      {/if}
    </div>

    <!-- Bottom toolbar -->
    <div class="flex items-center gap-1 px-2 py-2 border-t border-gray-200 dark:border-gray-700 shrink-0">
      <button
        onclick={handleRunAll}
        disabled={running || models.length === 0}
        class="flex items-center gap-1 text-[10px] px-2 py-1 rounded text-gray-500 dark:text-gray-400 hover:text-green-600 hover:bg-green-50 dark:hover:bg-green-900/20 disabled:opacity-40 transition-colors"
        title="Run all models"
      >
        <Play size={12} /> {running ? 'Running...' : 'Run All'}
      </button>
      <div class="flex-1"></div>
      <button
        onclick={openDAG}
        disabled={models.length === 0}
        class="p-1.5 rounded text-gray-400 hover:text-orange-500 hover:bg-gray-100 dark:hover:bg-gray-800 disabled:opacity-40 transition-colors"
        title="Dependency graph"
      >
        <GitBranch size={14} />
      </button>
      <button
        onclick={openHistory}
        class="p-1.5 rounded text-gray-400 hover:text-orange-500 hover:bg-gray-100 dark:hover:bg-gray-800 transition-colors"
        title="Run history"
      >
        <History size={14} />
      </button>
    </div>
  </div>

  <!-- ─── Right Panel: Editor / Empty State ──────────────────────── -->
  <div class="flex-1 flex flex-col min-w-0 min-h-0">
    {#if selectedModelId && editModel.name != null}
      <!-- Config toolbar -->
      <div class="flex items-center gap-2 px-3 py-2 border-b border-gray-200 dark:border-gray-700 bg-gray-50 dark:bg-gray-800/50 shrink-0 flex-wrap">
        <input
          type="text"
          bind:value={editModel.name}
          class="text-sm font-semibold bg-transparent border-0 border-b border-transparent hover:border-gray-300 dark:hover:border-gray-600 focus:border-orange-400 focus:outline-none text-gray-800 dark:text-gray-200 px-1 py-0.5 min-w-[120px] max-w-[200px]"
          placeholder="model_name"
        />
        <span class="text-gray-300 dark:text-gray-600">|</span>
        <label for="toolbar-target-db" class="text-[10px] text-gray-400 uppercase tracking-wide">db</label>
        <input
          id="toolbar-target-db"
          type="text"
          bind:value={editModel.target_database}
          class="text-xs bg-transparent border border-gray-300 dark:border-gray-600 rounded px-1.5 py-0.5 w-24 text-gray-700 dark:text-gray-300 focus:border-orange-400 focus:outline-none"
        />
        <span class="text-gray-300 dark:text-gray-600">|</span>
        <!-- Materialization toggle -->
        <div class="flex rounded border border-gray-300 dark:border-gray-600 overflow-hidden">
          <button
            onclick={() => { editModel.materialization = 'view' }}
            class="flex items-center gap-1 text-[10px] px-2 py-0.5 transition-colors
              {editModel.materialization === 'view'
                ? 'bg-orange-100 dark:bg-orange-900/30 text-orange-700 dark:text-orange-400'
                : 'text-gray-500 hover:bg-gray-100 dark:hover:bg-gray-800'}"
          >
            <Eye size={11} /> View
          </button>
          <button
            onclick={() => { editModel.materialization = 'table' }}
            class="flex items-center gap-1 text-[10px] px-2 py-0.5 border-l border-gray-300 dark:border-gray-600 transition-colors
              {editModel.materialization === 'table'
                ? 'bg-orange-100 dark:bg-orange-900/30 text-orange-700 dark:text-orange-400'
                : 'text-gray-500 hover:bg-gray-100 dark:hover:bg-gray-800'}"
          >
            <Table2 size={11} /> Table
          </button>
        </div>

        {#if editModel.materialization === 'table'}
          <span class="text-gray-300 dark:text-gray-600">|</span>
          <select
            bind:value={editModel.table_engine}
            class="text-[10px] bg-transparent border border-gray-300 dark:border-gray-600 rounded px-1 py-0.5 text-gray-700 dark:text-gray-300 focus:outline-none"
          >
            <option value="MergeTree">MergeTree</option>
            <option value="ReplacingMergeTree">ReplacingMergeTree</option>
            <option value="SummingMergeTree">SummingMergeTree</option>
            <option value="AggregatingMergeTree">AggregatingMergeTree</option>
            <option value="Memory">Memory</option>
          </select>
          <input
            type="text"
            bind:value={editModel.order_by}
            placeholder="ORDER BY"
            class="text-[10px] bg-transparent border border-gray-300 dark:border-gray-600 rounded px-1.5 py-0.5 w-24 text-gray-700 dark:text-gray-300 focus:border-orange-400 focus:outline-none"
          />
        {/if}

        <div class="flex-1"></div>

        <button
          onclick={() => { showDescription = !showDescription }}
          class="text-[10px] text-gray-400 hover:text-gray-600 dark:hover:text-gray-300 flex items-center gap-0.5 transition-colors"
          title="Toggle description"
        >
          <FileText size={11} />
        </button>
        <span class="w-1.5 h-1.5 rounded-full {statusDot(editModel.status ?? 'draft')}" title={editModel.status ?? 'draft'}></span>
        <button
          onclick={() => handleRunSingle(selectedModelId!)}
          disabled={running}
          class="flex items-center gap-1 text-[10px] px-2 py-1 rounded text-gray-500 hover:text-green-600 hover:bg-green-50 dark:hover:bg-green-900/20 disabled:opacity-40 transition-colors"
          title="Run this model"
        >
          <Play size={12} /> Run
        </button>
        <button
          onclick={handleSave}
          disabled={saving}
          class="flex items-center gap-1 text-[10px] px-2.5 py-1 rounded bg-orange-500 text-white hover:bg-orange-600 disabled:opacity-50 transition-colors font-medium"
        >
          <Save size={12} /> {saving ? 'Saving...' : 'Save'}
        </button>
      </div>

      <!-- Description (collapsible) -->
      {#if showDescription}
        <div class="px-3 py-1.5 border-b border-gray-200 dark:border-gray-700 bg-gray-50/50 dark:bg-gray-800/30 shrink-0">
          <textarea
            bind:value={editModel.description}
            rows={2}
            placeholder="Model description (optional)"
            class="w-full text-xs bg-transparent border-0 focus:outline-none text-gray-600 dark:text-gray-400 resize-none placeholder:text-gray-300 dark:placeholder:text-gray-600"
          ></textarea>
        </div>
      {/if}

      <!-- Info hint -->
      <div class="px-3 py-1 border-b border-gray-200 dark:border-gray-700 bg-gray-50/50 dark:bg-gray-800/30 shrink-0">
        <div class="flex items-center gap-2 text-[10px] text-gray-400 dark:text-gray-500">
          <span>Use <code class="px-1 py-0.5 rounded bg-gray-200 dark:bg-gray-700 font-mono">$ref(model_name)</code> to reference other models</span>
          <span class="opacity-40">|</span>
          <span>View = computed on read, Table = snapshot on run</span>
        </div>
      </div>

      <!-- SQL Editor -->
      <div class="flex-1 min-h-0">
        {#key selectedModelId}
          <SqlEditor
            bind:this={sqlEditor}
            value={editModel.sql_body ?? ''}
            onchange={(sql: string) => { editModel.sql_body = sql }}
          />
        {/key}
      </div>

      <!-- Error bar -->
      {#if editModel.status === 'error' && editModel.last_error}
        <div class="px-3 py-2 border-t border-red-200 dark:border-red-800 bg-red-50 dark:bg-red-900/20 text-xs text-red-600 dark:text-red-400 flex items-center gap-2 shrink-0">
          <AlertCircle size={14} />
          {editModel.last_error}
        </div>
      {/if}
    {:else}
      <!-- Empty state -->
      <div class="flex flex-col items-center justify-center h-full gap-3 text-gray-400 dark:text-gray-500">
        <Boxes size={48} strokeWidth={1} class="opacity-30" />
        <p class="text-sm">Select a model or create a new one</p>
        <button
          onclick={handleCreate}
          class="flex items-center gap-1.5 text-xs px-3 py-1.5 rounded bg-orange-500 text-white hover:bg-orange-600 transition-colors"
        >
          <Plus size={14} /> New Model
        </button>
      </div>
    {/if}
  </div>
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
