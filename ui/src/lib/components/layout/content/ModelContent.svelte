<script lang="ts">
  import { onMount } from 'svelte'
  import type { ModelTab } from '../../../stores/tabs.svelte'
  import { updateModelTabEdit, markModelTabSaved, updateModelTabStatus } from '../../../stores/tabs.svelte'
  import type { Materialization, ModelRunResult } from '../../../types/models'
  import * as api from '../../../api/models'
  import { refreshModelCache } from '../../../editor/completions'
  import { success as toastSuccess, error as toastError } from '../../../stores/toast.svelte'
  import SqlEditor from '../../editor/SqlEditor.svelte'
  import Input from '../../common/Input.svelte'
  import Select, { type SelectOption } from '../../common/Select.svelte'
  import Button from '../../common/Button.svelte'
  import Badge from '../../common/Badge.svelte'
  import Textarea from '../../common/Textarea.svelte'
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
    GitBranch,
  } from 'lucide-svelte'

  interface Props {
    tab: ModelTab
  }

  let { tab }: Props = $props()

  let sqlEditor = $state<SqlEditor | undefined>(undefined)
  let saving = $state(false)
  let running = $state(false)
  let showDescription = $state(false)
  // GitHub-managed models are edited in the repository, never here.
  let managedByGitHub = $state(false)

  // Split panel
  const savedSplit = parseFloat(localStorage.getItem('ch-ui-model-split-percent') ?? '60')
  let splitPercent = $state(isNaN(savedSplit) ? 60 : savedSplit)
  let dragging = $state(false)
  let containerEl: HTMLDivElement

  // Run output
  let runResult = $state<ModelRunResult | null>(null)

  const ENGINE_OPTIONS: SelectOption[] = [
    { value: 'MergeTree', label: 'MergeTree' },
    { value: 'ReplacingMergeTree', label: 'ReplacingMergeTree' },
    { value: 'SummingMergeTree', label: 'SummingMergeTree' },
    { value: 'AggregatingMergeTree', label: 'AggregatingMergeTree' },
    { value: 'Memory', label: 'Memory' },
  ]

  // ClickHouse accepts ORDER BY only for the MergeTree family.
  const takesOrderBy = $derived(tab.edit.tableEngine.includes('MergeTree'))

  onMount(() => {
    showDescription = !!tab.edit.description
    loadSource()
    loadLatestRun()
  })

  async function loadSource() {
    try {
      const res = await api.getModel(tab.modelId)
      managedByGitHub = res.model.source === 'github'
    } catch {
      // The run and save calls report their own errors.
    }
  }

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
    if (managedByGitHub) return
    updateModelTabEdit(tab.id, { sqlBody: sql })
  }

  function setMaterialization(m: Materialization) {
    updateModelTabEdit(tab.id, { materialization: m })
  }

  /** Saves the editor state. Returns false (after a toast) when it failed. */
  async function handleSave(): Promise<boolean> {
    if (managedByGitHub) return false
    saving = true
    try {
      const sqlValue = sqlEditor?.getValue() ?? tab.edit.sqlBody
      const res = await api.updateModel(tab.modelId, {
        name: tab.edit.modelName,
        description: tab.edit.description,
        target_database: tab.edit.targetDatabase,
        materialization: tab.edit.materialization as Materialization,
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
      return true
    } catch (e: unknown) {
      toastError((e instanceof Error ? e.message : '') || 'Failed to save model')
      return false
    } finally {
      saving = false
    }
  }

  // Runs build what is saved on the server, so unsaved edits are saved first.
  async function handleRun() {
    if (tab.dirty && !managedByGitHub) {
      if (!(await handleSave())) return
    }
    running = true
    try {
      // The run is synchronous: when this returns, the run has finished.
      const res = await api.runSingleModel(tab.modelId)
      const detail = await api.getModelRun(res.run_id)
      runResult = (detail.results ?? []).find(r => r.model_id === tab.modelId) ?? null
      const modelRes = await api.getModel(tab.modelId)
      updateModelTabStatus(tab.id, modelRes.model.status, modelRes.model.last_error)
      if (res.status === 'success') {
        toastSuccess('Model run finished')
      } else {
        toastError(runResult?.error || res.error || 'Model run failed')
      }
    } catch (e: unknown) {
      toastError((e instanceof Error ? e.message : '') || 'Failed to run model')
    } finally {
      running = false
    }
  }

  type StatusTone = 'neutral' | 'success' | 'danger' | 'info'

  function statusTone(status: string): StatusTone {
    switch (status) {
      case 'success': return 'success'
      case 'error': return 'danger'
      case 'running': return 'info'
      default: return 'neutral'
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
  <div class="flex min-h-10 shrink-0 flex-wrap items-center gap-2 border-b border-edge-subtle bg-surface px-3 py-1.5">
    <Input
      size="sm"
      mono
      value={tab.edit.modelName}
      oninput={(e) => updateModelTabEdit(tab.id, { modelName: e.currentTarget.value })}
      placeholder="model_name"
      disabled={managedByGitHub}
      class="w-44 font-semibold"
    />
    <label for="model-target-db-{tab.id}" class="text-xs text-fg-3">Database</label>
    <Input
      id="model-target-db-{tab.id}"
      size="sm"
      mono
      value={tab.edit.targetDatabase}
      oninput={(e) => updateModelTabEdit(tab.id, { targetDatabase: e.currentTarget.value })}
      disabled={managedByGitHub}
      class="w-32"
    />
    <div class="flex items-center gap-0.5 rounded-md border border-edge p-0.5" role="group" aria-label="Materialization">
      <Button
        size="xs"
        variant={tab.edit.materialization === 'view' ? 'secondary' : 'ghost'}
        aria-pressed={tab.edit.materialization === 'view'}
        disabled={managedByGitHub}
        onclick={() => setMaterialization('view')}
      >
        <Eye size={12} /> View
      </Button>
      <Button
        size="xs"
        variant={tab.edit.materialization === 'table' ? 'secondary' : 'ghost'}
        aria-pressed={tab.edit.materialization === 'table'}
        disabled={managedByGitHub}
        onclick={() => setMaterialization('table')}
      >
        <Table2 size={12} /> Table
      </Button>
    </div>

    {#if tab.edit.materialization === 'table'}
      <Select
        size="sm"
        class="w-48"
        value={tab.edit.tableEngine}
        options={ENGINE_OPTIONS}
        disabled={managedByGitHub}
        onchange={(v) => updateModelTabEdit(tab.id, { tableEngine: v })}
      />
      {#if takesOrderBy}
        <Input
          size="sm"
          mono
          value={tab.edit.orderBy}
          oninput={(e) => updateModelTabEdit(tab.id, { orderBy: e.currentTarget.value })}
          placeholder="ORDER BY (default tuple())"
          disabled={managedByGitHub}
          class="w-40"
        />
      {/if}
    {/if}

    <div class="flex-1"></div>

    <Button
      icon
      size="sm"
      variant="ghost"
      onclick={() => { showDescription = !showDescription }}
      title="Toggle description"
      aria-label="Toggle description"
      aria-pressed={showDescription}
    >
      <FileText size={14} />
    </Button>
    <Badge dot tone={statusTone(tab.status)}>{tab.status}</Badge>
    <Button
      size="sm"
      variant="outline"
      onclick={handleRun}
      loading={running}
      disabled={saving}
      title={tab.dirty && !managedByGitHub ? 'Save and run this model' : 'Run this model'}
    >
      {#if !running}<Play size={13} />{/if}
      {running ? 'Running...' : tab.dirty && !managedByGitHub ? 'Save and run' : 'Run'}
    </Button>
    {#if !managedByGitHub}
      <Button size="sm" onclick={handleSave} loading={saving} disabled={running || !tab.dirty}>
        {#if !saving}<Save size={13} />{/if}
        {saving ? 'Saving...' : 'Save'}
      </Button>
    {/if}
  </div>

  {#if managedByGitHub}
    <div class="flex shrink-0 items-center gap-2 border-b border-edge-subtle bg-info-soft px-3 py-2 text-xs text-fg-2">
      <GitBranch size={14} class="shrink-0 text-info" />
      <Badge tone="info">Managed by GitHub</Badge>
      <span>This model is read-only here. Edit the .sql file in your repository and sync again.</span>
    </div>
  {/if}

  <!-- Description (collapsible) -->
  {#if showDescription}
    <div class="shrink-0 border-b border-edge-subtle bg-surface px-3 py-2">
      <Textarea
        value={tab.edit.description}
        oninput={(e) => updateModelTabEdit(tab.id, { description: e.currentTarget.value })}
        rows={2}
        placeholder="Model description (optional)"
        disabled={managedByGitHub}
      />
    </div>
  {/if}

  <!-- Info hint -->
  <div class="shrink-0 border-b border-edge-subtle bg-surface px-3 py-1.5">
    <div class="flex flex-wrap items-center gap-2 text-xs text-fg-3">
      <span>Use <code class="rounded-sm bg-surface-2 px-1 py-0.5 font-mono text-fg-2">$ref(model_name)</code> to reference other models</span>
      <span class="text-fg-4">|</span>
      <span>View: computed on read. Table: rebuilt on every run.</span>
    </div>
  </div>

  <!-- Editor pane -->
  <div class="flex flex-col min-h-[80px] overflow-hidden" style="height: {splitPercent}%">
    <div class="flex-1 min-h-0">
      {#if managedByGitHub}
        <pre class="h-full overflow-auto bg-canvas p-3 font-mono text-xs leading-relaxed text-fg whitespace-pre" aria-label="Model SQL (read-only)">{tab.edit.sqlBody}</pre>
      {:else}
        <SqlEditor
          bind:this={sqlEditor}
          value={tab.edit.sqlBody}
          onchange={handleSQLChange}
          onsave={handleSave}
        />
      {/if}
    </div>
  </div>

  <!-- Drag handle -->
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div
    class="h-1 shrink-0 cursor-row-resize group flex items-center justify-center hover:bg-accent-soft transition-colors {dragging ? 'bg-accent-soft' : 'bg-surface-2'}"
    onmousedown={onDragStart}
  >
    <div class="w-8 h-0.5 rounded-full {dragging ? 'bg-accent' : 'bg-fg-4 group-hover:bg-accent/60'} transition-colors"></div>
  </div>

  <!-- Run Output Panel -->
  <div class="flex-1 min-h-[60px] overflow-auto bg-surface">
    {#if running}
      <div class="flex items-center justify-center h-full gap-2 text-[13px] text-fg-3">
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
          <Badge tone={statusTone(runResult.status)}>{runResult.status}</Badge>
          <span class="text-xs text-fg-3">{runResult.elapsed_ms}ms</span>
          {#if runResult.finished_at}
            <span class="ml-auto text-xs text-fg-4">{new Date(runResult.finished_at).toLocaleString()}</span>
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
            <div class="flex items-center gap-1.5 text-xs font-medium text-fg-3">
              <Code size={11} />
              <span>Resolved SQL</span>
            </div>
            <pre class="text-xs font-mono text-fg-2 bg-surface-2 rounded p-3 overflow-auto max-h-48 whitespace-pre-wrap break-all">{runResult.resolved_sql}</pre>
          </div>
        {/if}
      </div>
    {:else}
      <div class="flex items-center justify-center h-full text-[13px] text-fg-3">
        Run this model to see results
      </div>
    {/if}
  </div>
</div>

<!-- Drag overlay -->
{#if dragging}
  <div class="fixed inset-0 z-50 cursor-row-resize"></div>
{/if}
