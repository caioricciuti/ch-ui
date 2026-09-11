<script lang="ts">
  import { onMount } from 'svelte'
  import type { ModelTab } from '../../../stores/tabs.svelte'
  import { updateModelTabEdit, markModelTabSaved, updateModelTabStatus } from '../../../stores/tabs.svelte'
  import type { ModelRunResult } from '../../../types/models'
  import * as api from '../../../api/models'
  import { refreshModelCache } from '../../../editor/completions'
  import { success as toastSuccess, error as toastError } from '../../../stores/toast.svelte'
  import SqlEditor from '../../editor/SqlEditor.svelte'
  import {
    Play,
    Save,
    Eye,
    Table2,
    FileText,
    AlertCircle,
    CheckCircle,
    XCircle,
    Clock,
    Loader2,
    Code,
  } from 'lucide-svelte'

  interface Props {
    tab: ModelTab
  }

  let { tab }: Props = $props()

  let sqlEditor = $state<SqlEditor | undefined>(undefined)
  let saving = $state(false)
  let running = $state(false)
  let showDescription = $state(false)

  // Split panel
  const savedSplit = parseFloat(localStorage.getItem('ch-ui-model-split-percent') ?? '60')
  let splitPercent = $state(isNaN(savedSplit) ? 60 : savedSplit)
  let dragging = $state(false)
  let containerEl: HTMLDivElement

  // Run output
  let runResult = $state<ModelRunResult | null>(null)
  let runLoading = $state(false)

  onMount(() => {
    showDescription = !!tab.edit.description
    loadLatestRun()
  })

  async function loadLatestRun() {
    try {
      const res = await api.listModelRuns(5, 0)
      const runs = res.runs ?? []
      for (const run of runs) {
        const detail = await api.getModelRun(run.id)
        const result = (detail.results ?? []).find(r => r.model_id === tab.modelId)
        if (result) {
          runResult = result
          return
        }
      }
    } catch {
      // no run to show
    }
  }

  function handleSQLChange(sql: string) {
    updateModelTabEdit(tab.id, { sqlBody: sql })
  }

  async function handleSave() {
    saving = true
    try {
      const sqlValue = sqlEditor?.getValue() ?? tab.edit.sqlBody
      const res = await api.updateModel(tab.modelId, {
        name: tab.edit.modelName,
        description: tab.edit.description,
        target_database: tab.edit.targetDatabase,
        materialization: tab.edit.materialization as 'view' | 'table',
        sql_body: sqlValue,
        table_engine: tab.edit.tableEngine,
        order_by: tab.edit.orderBy,
      })
      refreshModelCache()
      markModelTabSaved(tab.id, {
        name: res.model.name,
        status: res.model.status,
        last_error: res.model.last_error,
      })
      toastSuccess('Model saved')
    } catch (e: unknown) {
      toastError((e as Error).message || 'Failed to save model')
    } finally {
      saving = false
    }
  }

  async function handleRun() {
    running = true
    runResult = null
    try {
      const res = await api.runSingleModel(tab.modelId)
      toastSuccess('Model run started')

      // Fetch the run result
      if (res.run_id) {
        runLoading = true
        // Poll briefly for result completion
        let attempts = 0
        while (attempts < 10) {
          await new Promise(r => setTimeout(r, 1000))
          try {
            const detail = await api.getModelRun(res.run_id)
            const result = (detail.results ?? []).find(r => r.model_id === tab.modelId)
            if (result && result.status !== 'pending' && result.status !== 'running') {
              runResult = result
              // Update tab status
              const modelRes = await api.getModel(tab.modelId)
              if (modelRes.model) {
                updateModelTabStatus(tab.id, modelRes.model.status, modelRes.model.last_error)
              }
              break
            }
          } catch {
            break
          }
          attempts++
        }
        runLoading = false
      }
    } catch (e: unknown) {
      toastError((e as Error).message || 'Failed to run model')
    } finally {
      running = false
    }
  }

  function statusBadgeClass(status: string): string {
    switch (status) {
      case 'success': return 'bg-success-soft text-success'
      case 'error': return 'bg-danger-soft text-danger'
      case 'running': return 'bg-info-soft text-info'
      default: return 'bg-surface-2 text-fg-2 '
    }
  }

  function statusDot(status: string): string {
    switch (status) {
      case 'success': return 'bg-success'
      case 'error': return 'bg-danger'
      default: return 'bg-fg-4'
    }
  }

  // ── Drag handle logic ──
  function onDragStart(e: MouseEvent) {
    e.preventDefault()
    dragging = true
    document.addEventListener('mousemove', onDragMove)
    document.addEventListener('mouseup', onDragEnd)
  }

  function onDragMove(e: MouseEvent) {
    if (!containerEl) return
    const rect = containerEl.getBoundingClientRect()
    const y = e.clientY - rect.top
    const pct = (y / rect.height) * 100
    splitPercent = Math.max(20, Math.min(85, pct))
  }

  function onDragEnd() {
    dragging = false
    document.removeEventListener('mousemove', onDragMove)
    document.removeEventListener('mouseup', onDragEnd)
    localStorage.setItem('ch-ui-model-split-percent', String(splitPercent))
  }
</script>

<div class="flex flex-col h-full overflow-hidden" bind:this={containerEl}>
  <!-- Config toolbar -->
  <div class="flex items-center gap-2 px-3 py-2 border-b border-edge-subtle bg-surface shrink-0 flex-wrap">
    <input
      type="text"
      value={tab.edit.modelName}
      oninput={(e) => updateModelTabEdit(tab.id, { modelName: (e.target as HTMLInputElement).value })}
      class="text-sm font-semibold bg-transparent border-0 border-b border-transparent hover:border-edge focus:border-accent focus:outline-none text-fg px-1 py-0.5 min-w-[120px] max-w-[200px]"
      placeholder="model_name"
    />
    <span class="text-fg-4">|</span>
    <label for="model-target-db-{tab.id}" class="text-[10px] text-fg-4 uppercase tracking-wide">db</label>
    <input
      id="model-target-db-{tab.id}"
      type="text"
      value={tab.edit.targetDatabase}
      oninput={(e) => updateModelTabEdit(tab.id, { targetDatabase: (e.target as HTMLInputElement).value })}
      class="text-xs bg-transparent border border-edge rounded px-1.5 py-0.5 w-24 text-fg-2 focus:border-accent focus:outline-none"
    />
    <span class="text-fg-4">|</span>
    <!-- Materialization toggle -->
    <div class="flex rounded border border-edge overflow-hidden">
      <button
        onclick={() => updateModelTabEdit(tab.id, { materialization: 'view' })}
        class="flex items-center gap-1 text-[10px] px-2 py-0.5 transition-colors
 {tab.edit.materialization === 'view'
            ? 'bg-accent-soft text-accent'
            : 'text-fg-3 hover:bg-hover'}"
      >
        <Eye size={11} /> View
      </button>
      <button
        onclick={() => updateModelTabEdit(tab.id, { materialization: 'table' })}
        class="flex items-center gap-1 text-[10px] px-2 py-0.5 border-l border-edge transition-colors
 {tab.edit.materialization === 'table'
            ? 'bg-accent-soft text-accent'
            : 'text-fg-3 hover:bg-hover'}"
      >
        <Table2 size={11} /> Table
      </button>
    </div>

    {#if tab.edit.materialization === 'table'}
      <span class="text-fg-4">|</span>
      <select
        value={tab.edit.tableEngine}
        onchange={(e) => updateModelTabEdit(tab.id, { tableEngine: (e.target as HTMLSelectElement).value })}
        class="text-[10px] bg-transparent border border-edge rounded px-1 py-0.5 text-fg-2 focus:outline-none"
      >
        <option value="MergeTree">MergeTree</option>
        <option value="ReplacingMergeTree">ReplacingMergeTree</option>
        <option value="SummingMergeTree">SummingMergeTree</option>
        <option value="AggregatingMergeTree">AggregatingMergeTree</option>
        <option value="Memory">Memory</option>
      </select>
      <input
        type="text"
        value={tab.edit.orderBy}
        oninput={(e) => updateModelTabEdit(tab.id, { orderBy: (e.target as HTMLInputElement).value })}
        placeholder="ORDER BY"
        class="text-[10px] bg-transparent border border-edge rounded px-1.5 py-0.5 w-24 text-fg-2 focus:border-accent focus:outline-none"
      />
    {/if}

    <div class="flex-1"></div>

    <button
      onclick={() => { showDescription = !showDescription }}
      class="text-[10px] text-fg-4 hover:text-fg flex items-center gap-0.5 transition-colors"
      title="Toggle description"
    >
      <FileText size={11} />
    </button>
    <span class="w-1.5 h-1.5 rounded-full {statusDot(tab.status)}" title={tab.status}></span>
    <button
      onclick={handleRun}
      disabled={running}
      class="flex items-center gap-1 text-[10px] px-2 py-1 rounded text-fg-3 hover:text-success hover:bg-success-soft disabled:opacity-40 transition-colors"
      title="Run this model"
    >
      <Play size={12} /> {running ? 'Running...' : 'Run'}
    </button>
    <button
      onclick={handleSave}
      disabled={saving}
      class="flex items-center gap-1 text-[10px] px-2.5 py-1 rounded bg-accent text-accent-fg hover:brightness-110 disabled:opacity-50 transition-colors font-medium"
    >
      <Save size={12} /> {saving ? 'Saving...' : 'Save'}
    </button>
  </div>

  <!-- Description (collapsible) -->
  {#if showDescription}
    <div class="px-3 py-1.5 border-b border-edge-subtle bg-surface shrink-0">
      <textarea
        value={tab.edit.description}
        oninput={(e) => updateModelTabEdit(tab.id, { description: (e.target as HTMLTextAreaElement).value })}
        rows={2}
        placeholder="Model description (optional)"
        class="w-full text-xs bg-transparent border-0 focus:outline-none text-fg-2 resize-none placeholder:text-fg-4"
      ></textarea>
    </div>
  {/if}

  <!-- Info hint -->
  <div class="px-3 py-1 border-b border-edge-subtle bg-surface shrink-0">
    <div class="flex items-center gap-2 text-[10px] text-fg-4">
      <span>Use <code class="px-1 py-0.5 rounded bg-surface-2 font-mono">$ref(model_name)</code> to reference other models</span>
      <span class="opacity-40">|</span>
      <span>View = computed on read, Table = snapshot on run</span>
    </div>
  </div>

  <!-- Editor pane -->
  <div class="flex flex-col min-h-[80px] overflow-hidden" style="height: {splitPercent}%">
    <div class="flex-1 min-h-0">
      <SqlEditor
        bind:this={sqlEditor}
        value={tab.edit.sqlBody}
        onchange={handleSQLChange}
      />
    </div>
  </div>

  <!-- Drag handle -->
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div
    class="h-1 shrink-0 cursor-row-resize group flex items-center justify-center hover:bg-ch-orange/20 transition-colors {dragging ? 'bg-ch-orange/30' : 'bg-surface-2'}"
    onmousedown={onDragStart}
  >
    <div class="w-8 h-0.5 rounded-full {dragging ? 'bg-accent' : 'bg-fg-4 group-hover:bg-accent/60'} transition-colors"></div>
  </div>

  <!-- Run Output Panel -->
  <div class="flex-1 min-h-[60px] overflow-auto bg-surface">
    {#if running || runLoading}
      <div class="flex items-center justify-center h-full gap-2 text-fg-4 text-sm">
        <Loader2 size={16} class="animate-spin" />
        <span>Running model...</span>
      </div>
    {:else if runResult}
      <div class="p-3 space-y-3">
        <!-- Status header -->
        <div class="flex items-center gap-3">
          <span class="shrink-0">
            {#if runResult.status === 'success'}
              <CheckCircle size={16} class="text-success" />
            {:else if runResult.status === 'error'}
              <XCircle size={16} class="text-danger" />
            {:else}
              <Clock size={16} class="text-info" />
            {/if}
          </span>
          <span class="text-[10px] px-2 py-0.5 rounded-full {statusBadgeClass(runResult.status)} font-medium uppercase tracking-wide">
            {runResult.status}
          </span>
          <span class="text-xs text-fg-3">{runResult.elapsed_ms}ms</span>
          {#if runResult.finished_at}
            <span class="text-[10px] text-fg-4 ml-auto">{new Date(runResult.finished_at).toLocaleString()}</span>
          {/if}
        </div>

        <!-- Error message -->
        {#if runResult.error}
          <div class="flex items-start gap-2 px-3 py-2 rounded-md bg-danger-soft">
            <AlertCircle size={14} class="text-danger shrink-0 mt-0.5" />
            <pre class="text-xs text-danger whitespace-pre-wrap break-all flex-1">{runResult.error}</pre>
          </div>
        {/if}

        <!-- Resolved SQL -->
        {#if runResult.resolved_sql}
          <div class="space-y-1">
            <div class="flex items-center gap-1.5 text-[10px] text-fg-4 uppercase tracking-wide font-medium">
              <Code size={11} />
              <span>Resolved SQL</span>
            </div>
            <pre class="text-xs font-mono text-fg-2 bg-surface-2 rounded p-3 overflow-auto max-h-48 whitespace-pre-wrap break-all">{runResult.resolved_sql}</pre>
          </div>
        {/if}
      </div>
    {:else}
      <div class="flex items-center justify-center h-full text-fg-4 text-sm">
        Run this model to see results
      </div>
    {/if}
  </div>
</div>

<!-- Drag overlay -->
{#if dragging}
  <div class="fixed inset-0 z-50 cursor-row-resize"></div>
{/if}
