<script lang="ts">
  import { onMount } from 'svelte'
  import type { ModelTab } from '../../../stores/tabs.svelte'
  import { updateModelTabEdit, markModelTabSaved, updateModelTabStatus } from '../../../stores/tabs.svelte'
  import type { IncrementalStrategy, Materialization, ModelEditState, ModelRunResult, RunScope } from '../../../types/models'
  import * as api from '../../../api/models'
  import { refreshModelCache } from '../../../editor/completions'
  import { success as toastSuccess, error as toastError, errorFrom as toastErrorFrom } from '../../../stores/toast.svelte'
  import SqlEditor from '../../editor/SqlEditor.svelte'
  import Input from '../../common/Input.svelte'
  import Select, { type SelectOption } from '../../common/Select.svelte'
  import Button from '../../common/Button.svelte'
  import Badge from '../../common/Badge.svelte'
  import Textarea from '../../common/Textarea.svelte'
  import Tabs, { type TabItem } from '../../common/Tabs.svelte'
  import ContextMenu, { type ContextMenuItem } from '../../common/ContextMenu.svelte'
  import TestOutcomesTable from '../../models/TestOutcomesTable.svelte'
  import ModelTestsPanel from '../../models/ModelTestsPanel.svelte'
  import ModelCompilePanel from '../../models/ModelCompilePanel.svelte'
  import ModelPreviewPanel from '../../models/ModelPreviewPanel.svelte'
  import {
    MATERIALIZATIONS,
    buildsTable,
    materializationInfo,
    parseTestOutcomes,
    supportsFullRefresh,
  } from '../../models/materializations'
  import {
    Play,
    Save,
    FileText,
    AlertCircle,
    CheckCircle,
    XCircle,
    Clock,
    Loader2,
    Code,
    GitBranch,
    ChevronDown,
    ArrowUpFromLine,
    ArrowDownFromLine,
    RotateCcw,
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
  const testOutcomes = $derived(parseTestOutcomes(runResult?.test_details))

  // Lower pane tabs. Tests and Preview stay mounted once opened so their
  // state survives switching; Compiled mounts on open, which compiles.
  type PaneTab = 'run' | 'tests' | 'compiled' | 'preview'
  const PANE_TABS: TabItem[] = [
    { id: 'run', label: 'Run' },
    { id: 'tests', label: 'Tests' },
    { id: 'compiled', label: 'Compiled' },
    { id: 'preview', label: 'Preview' },
  ]
  let paneTab = $state<PaneTab>('run')
  let visited = $state<Record<PaneTab, boolean>>({ run: true, tests: false, compiled: false, preview: false })

  function setPaneTab(id: string) {
    const next = PANE_TABS.find((t) => t.id === id)?.id as PaneTab | undefined
    if (!next) return
    paneTab = next
    visited[next] = true
  }

  // Run menu
  let runMenu = $state<{ x: number; y: number } | null>(null)

  const BASE_ENGINES = ['MergeTree', 'ReplacingMergeTree', 'SummingMergeTree', 'AggregatingMergeTree', 'Memory']
  // A model synced from GitHub may carry an engine outside the list; keep it selectable.
  const engineOptions = $derived<SelectOption[]>(
    (tab.edit.tableEngine && !BASE_ENGINES.includes(tab.edit.tableEngine)
      ? [...BASE_ENGINES, tab.edit.tableEngine]
      : BASE_ENGINES
    ).map((e) => ({ value: e, label: e })),
  )

  const STRATEGY_OPTIONS: SelectOption[] = [
    { value: 'append', label: 'append' },
    { value: 'delete_insert', label: 'delete_insert' },
  ]

  const materialization = $derived(tab.edit.materialization)
  const showTableOptions = $derived(buildsTable(materialization))
  // ClickHouse accepts ORDER BY and PARTITION BY only for the MergeTree family.
  const takesOrderBy = $derived(tab.edit.tableEngine.includes('MergeTree'))
  const engineArgsPlaceholder = $derived(
    tab.edit.tableEngine.startsWith('ReplacingMergeTree')
      ? 'version column'
      : tab.edit.tableEngine.startsWith('SummingMergeTree')
        ? 'columns to sum'
        : 'engine args',
  )
  const currentInfo = $derived(materializationInfo(materialization))
  const infoLine = $derived(
    `${currentInfo.label}: ${currentInfo.description.replace('<name>', tab.edit.modelName || 'model')}.`,
  )

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

  function currentSql(): string {
    return sqlEditor?.getValue() ?? tab.edit.sqlBody
  }

  function handleSQLChange(sql: string) {
    if (managedByGitHub) return
    updateModelTabEdit(tab.id, { sqlBody: sql })
  }

  function setMaterialization(m: Materialization) {
    const patch: Partial<ModelEditState> = { materialization: m }
    if (buildsTable(m) && !tab.edit.tableEngine) patch.tableEngine = 'MergeTree'
    if (m === 'incremental' && !tab.edit.incrementalStrategy) patch.incrementalStrategy = 'append'
    updateModelTabEdit(tab.id, patch)
  }

  /** Saves the editor state. Returns false (after a toast) when it failed. */
  async function handleSave(): Promise<boolean> {
    if (managedByGitHub) return false
    saving = true
    try {
      const e = tab.edit
      const res = await api.updateModel(tab.modelId, {
        name: e.modelName,
        description: e.description,
        target_database: e.targetDatabase,
        materialization: e.materialization as Materialization,
        sql_body: currentSql(),
        table_engine: e.tableEngine,
        order_by: e.orderBy,
        partition_by: e.partitionBy,
        engine_args: e.engineArgs,
        incremental_strategy: e.materialization === 'incremental' ? (e.incrementalStrategy as IncrementalStrategy | '') : '',
        unique_key: e.uniqueKey,
        watermark_column: e.watermarkColumn,
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
      toastErrorFrom(e, 'save the model')
      return false
    } finally {
      saving = false
    }
  }

  // Runs build what is saved on the server, so unsaved edits are saved first.
  async function handleRun(opts: { scope?: RunScope; fullRefresh?: boolean } = {}) {
    if (tab.dirty && !managedByGitHub) {
      if (!(await handleSave())) return
    }
    running = true
    setPaneTab('run')
    try {
      // The run is synchronous: when this returns, the run has finished.
      const res = await api.runSingleModel(tab.modelId, opts)
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
      toastErrorFrom(e, 'run the model')
    } finally {
      running = false
    }
  }

  function openRunMenu(e: MouseEvent) {
    const rect = (e.currentTarget as HTMLElement).getBoundingClientRect()
    runMenu = { x: rect.right - 240, y: rect.bottom + 4 }
  }

  function runMenuItems(): ContextMenuItem[] {
    return [
      { id: 'upstream', label: 'Run (with upstream)', icon: Play, onSelect: () => handleRun() },
      { id: 'downstream', label: 'Run downstream', icon: ArrowDownFromLine, onSelect: () => handleRun({ scope: 'downstream' }) },
      { id: 'only', label: 'Run only this model', icon: ArrowUpFromLine, onSelect: () => handleRun({ scope: 'only' }) },
      { id: 'sep', separator: true },
      {
        id: 'full',
        label: 'Full refresh',
        icon: RotateCcw,
        disabled: !supportsFullRefresh(materialization),
        onSelect: () => handleRun({ fullRefresh: true }),
      },
    ]
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
      {#each MATERIALIZATIONS as m (m.value)}
        <Button
          size="xs"
          variant={materialization === m.value ? 'secondary' : 'ghost'}
          aria-pressed={materialization === m.value}
          disabled={managedByGitHub}
          title="{m.label}: {m.description}"
          onclick={() => setMaterialization(m.value)}
        >
          <m.icon size={12} /> {m.label}
        </Button>
      {/each}
    </div>

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
    <div class="flex items-center">
      <Button
        size="sm"
        variant="outline"
        class="rounded-r-none"
        onclick={() => handleRun()}
        loading={running}
        disabled={saving}
        title={tab.dirty && !managedByGitHub ? 'Save and run this model with its upstream models' : 'Run this model with its upstream models'}
      >
        {#if !running}<Play size={13} />{/if}
        {running ? 'Running...' : tab.dirty && !managedByGitHub ? 'Save and run' : 'Run'}
      </Button>
      <Button
        icon
        size="sm"
        variant="outline"
        class="-ml-px rounded-l-none"
        onclick={openRunMenu}
        disabled={saving || running}
        aria-label="More run options"
        title="More run options"
        aria-expanded={!!runMenu}
      >
        <ChevronDown size={13} />
      </Button>
    </div>
    {#if !managedByGitHub}
      <Button size="sm" onclick={handleSave} loading={saving} disabled={running || !tab.dirty}>
        {#if !saving}<Save size={13} />{/if}
        {saving ? 'Saving...' : 'Save'}
      </Button>
    {/if}
  </div>

  <!-- Table options -->
  {#if showTableOptions}
    <div class="flex min-h-10 shrink-0 flex-wrap items-center gap-2 border-b border-edge-subtle bg-surface px-3 py-1.5">
      <label for="model-engine-{tab.id}" class="text-xs text-fg-3">Engine</label>
      <Select
        id="model-engine-{tab.id}"
        size="sm"
        class="w-48"
        value={tab.edit.tableEngine}
        options={engineOptions}
        disabled={managedByGitHub}
        onchange={(v) => updateModelTabEdit(tab.id, { tableEngine: v })}
      />
      <Input
        size="sm"
        mono
        value={tab.edit.engineArgs}
        oninput={(e) => updateModelTabEdit(tab.id, { engineArgs: e.currentTarget.value })}
        placeholder={engineArgsPlaceholder}
        disabled={managedByGitHub}
        class="w-36"
      />
      {#if takesOrderBy}
        <Input
          size="sm"
          mono
          value={tab.edit.orderBy}
          oninput={(e) => updateModelTabEdit(tab.id, { orderBy: e.currentTarget.value })}
          placeholder="ORDER BY (default tuple())"
          disabled={managedByGitHub}
          class="w-44"
        />
        <Input
          size="sm"
          mono
          value={tab.edit.partitionBy}
          oninput={(e) => updateModelTabEdit(tab.id, { partitionBy: e.currentTarget.value })}
          placeholder="PARTITION BY (optional)"
          disabled={managedByGitHub}
          class="w-44"
        />
      {/if}

      {#if materialization === 'incremental'}
        <span class="h-4 w-px bg-edge-subtle" aria-hidden="true"></span>
        <label for="model-strategy-{tab.id}" class="text-xs text-fg-3">Strategy</label>
        <Select
          id="model-strategy-{tab.id}"
          size="sm"
          class="w-36"
          value={tab.edit.incrementalStrategy || 'append'}
          options={STRATEGY_OPTIONS}
          disabled={managedByGitHub}
          onchange={(v) => updateModelTabEdit(tab.id, { incrementalStrategy: v })}
        />
        {#if tab.edit.incrementalStrategy === 'delete_insert'}
          <Input
            size="sm"
            mono
            value={tab.edit.uniqueKey}
            oninput={(e) => updateModelTabEdit(tab.id, { uniqueKey: e.currentTarget.value })}
            placeholder="id or id, tenant_id"
            disabled={managedByGitHub}
            class="w-40"
          />
        {/if}
        <label for="model-watermark-{tab.id}" class="text-xs text-fg-3">Watermark</label>
        <Input
          id="model-watermark-{tab.id}"
          size="sm"
          mono
          value={tab.edit.watermarkColumn}
          oninput={(e) => updateModelTabEdit(tab.id, { watermarkColumn: e.currentTarget.value })}
          placeholder="updated_at"
          disabled={managedByGitHub}
          class="w-32"
        />
      {/if}
    </div>
  {/if}

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
      <span>
        <code class="rounded-sm bg-surface-2 px-1 py-0.5 font-mono text-fg-2">$ref(model_name)</code> references another model,
        <code class="rounded-sm bg-surface-2 px-1 py-0.5 font-mono text-fg-2">$this</code> is this model's own table
      </span>
      <span class="text-fg-4">|</span>
      <span>{infoLine}</span>
      {#if showTableOptions && tab.edit.tableEngine.startsWith('ReplacingMergeTree')}
        <span class="text-fg-4">|</span>
        <span>ReplacingMergeTree takes the version column as its engine argument.</span>
      {/if}
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

  <!-- Lower pane -->
  <div class="flex flex-1 min-h-[60px] flex-col overflow-hidden bg-surface">
    <Tabs items={PANE_TABS} value={paneTab} onchange={setPaneTab} size="sm" class="shrink-0 px-2" />

    <div class="min-h-0 flex-1 overflow-auto {paneTab === 'run' ? '' : 'hidden'}">
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
            {#if testOutcomes.length > 0}
              <Badge tone="success">{runResult.tests_passed} test{runResult.tests_passed === 1 ? '' : 's'} passed</Badge>
              {#if runResult.tests_failed > 0}
                <Badge tone="danger">{runResult.tests_failed} failed</Badge>
              {/if}
            {/if}
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

          <!-- Data tests -->
          {#if testOutcomes.length > 0}
            <div class="space-y-1">
              <div class="text-xs font-medium text-fg-3">Data tests</div>
              <TestOutcomesTable outcomes={testOutcomes} />
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

    {#if visited.tests}
      <div class="min-h-0 flex-1 {paneTab === 'tests' ? '' : 'hidden'}">
        <ModelTestsPanel modelId={tab.modelId} readonly={managedByGitHub} />
      </div>
    {/if}
    {#if paneTab === 'compiled'}
      <div class="min-h-0 flex-1">
        <ModelCompilePanel modelId={tab.modelId} getSql={currentSql} />
      </div>
    {/if}
    {#if visited.preview}
      <div class="min-h-0 flex-1 {paneTab === 'preview' ? '' : 'hidden'}">
        <ModelPreviewPanel modelId={tab.modelId} getSql={currentSql} />
      </div>
    {/if}
  </div>
</div>

<!-- Drag overlay -->
{#if dragging}
  <div class="fixed inset-0 z-50 cursor-row-resize"></div>
{/if}

<ContextMenu
  open={!!runMenu}
  x={runMenu?.x ?? 0}
  y={runMenu?.y ?? 0}
  items={runMenuItems()}
  onclose={() => { runMenu = null }}
/>
