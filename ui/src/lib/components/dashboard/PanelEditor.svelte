<script lang="ts">
  import type { DashboardVariable, Panel, PanelConfig, StatThreshold } from '../../types/api'
  import type { ColumnMeta } from '../../types/query'
  import { apiPost, apiPut } from '../../api/client'
  import { formatSQL } from '../../api/query'
  import { success as toastSuccess, error as toastError, errorFrom as toastErrorFrom } from '../../stores/toast.svelte'
  import Button from '../common/Button.svelte'
  import Spinner from '../common/Spinner.svelte'
  import Toolbar from '../editor/Toolbar.svelte'
  import SqlEditor from '../editor/SqlEditor.svelte'
  import PanelRenderer from './PanelRenderer.svelte'
  import ChartConfigFields from './ChartConfigFields.svelte'
  import { resolveChartColumns } from '../../utils/chart-transform'
  import { formatDashboardTimeRangeLabel } from '../../utils/dashboard-time'
  import { toDashboardTimeRangePayload } from '../../utils/dashboard-time'
  import MarkdownPanel from './MarkdownPanel.svelte'
  import { onMount, tick, untrack } from 'svelte'
  import { PanelsTopLeft } from 'lucide-svelte'
  import Combobox from '../common/Combobox.svelte'
  import { listSavedQueries } from '../../api/savedQueries'
  import type { SavedQuery } from '../../types/api'

  interface Props {
    dashboardId: string
    dashboardTimeRange?: string
    panel?: Panel | null
    /** Grid row below every existing panel: where a new panel is placed. */
    nextRow?: number
    /** Connection the panel is built on, recorded on new panels. */
    connectionId?: string
    /** Current dashboard variable values, so the preview runs like the panel will. */
    variables?: Record<string, string[]>
    /** Variable definitions of the dashboard, for drilldown links. */
    variableDefs?: DashboardVariable[]
    onclose: () => void
    onsave: (panel: Panel) => void
  }

  let { dashboardId, dashboardTimeRange = '1h', panel = null, nextRow = 0, connectionId = '', variables = {}, variableDefs = [], onclose, onsave }: Props = $props()

  // Form state
  let name = $state('')
  let description = $state('')
  let query = $state('')
  let content = $state('')
  let saving = $state(false)
  let running = $state(false)

  const DEFAULT_THRESHOLDS: StatThreshold[] = [
    { value: 0, color: '#22c55e' },
    { value: 80, color: '#f59e0b' },
    { value: 90, color: '#ef4444' },
  ]

  // Visualization settings, edited by ChartConfigFields.
  let config = $state<PanelConfig>({ chartType: 'table' })

  // Query result state
  let queryData = $state<Record<string, unknown>[]>([])
  let queryMeta = $state<ColumnMeta[]>([])
  let queryError = $state<string | null>(null)

  let editorComponent: SqlEditor | undefined = $state()

  // Saved queries the panel can start from: the SQL is copied into the panel,
  // so later edits to the saved query do not change the dashboard.
  let savedQueries = $state<SavedQuery[]>([])
  let savedQueryId = $state('')
  const savedQueryOptions = $derived(
    savedQueries.map((q) => ({ value: q.id, label: q.name, hint: q.description ?? undefined, keywords: q.query })),
  )

  onMount(() => {
    void listSavedQueries()
      .then((qs) => (savedQueries = qs))
      .catch(() => (savedQueries = []))
  })

  async function useSavedQuery(id: string): Promise<void> {
    savedQueryId = id
    const q = savedQueries.find((s) => s.id === id)
    if (!q) return
    query = q.query
    if (!name.trim()) name = q.name
    if (!description.trim() && q.description) description = q.description
    await tick()
    await runQuery()
  }

  // The config as saved: settings that do not apply to the chosen type are left out.
  const currentConfig = $derived.by<PanelConfig>(() => {
    const t = config.chartType
    const axis = t === 'timeseries' || t === 'bar'
    return {
      chartType: t,
      xColumn: config.xColumn ?? '',
      yColumns: config.yColumns ?? [],
      seriesColumn: axis && config.seriesColumn ? config.seriesColumn : undefined,
      colors: config.colors,
      legendPosition: config.legendPosition ?? 'bottom',
      content: content || undefined,
      barMode: t === 'bar' ? (config.barMode ?? 'grouped') : undefined,
      fill: t === 'timeseries' ? config.fill : undefined,
      stacked: t === 'timeseries' && config.stacked ? true : undefined,
      histogramColumn: t === 'histogram' && config.histogramColumn ? config.histogramColumn : undefined,
      histogramBuckets: t === 'histogram' ? config.histogramBuckets : undefined,
      scatterX: t === 'scatter' && config.scatterX ? config.scatterX : undefined,
      scatterY: t === 'scatter' && config.scatterY ? config.scatterY : undefined,
      scatterSize: t === 'scatter' && config.scatterSize ? config.scatterSize : undefined,
      scatterSeries: t === 'scatter' && config.scatterSeries ? config.scatterSeries : undefined,
      heatmapX: t === 'heatmap' && config.heatmapX ? config.heatmapX : undefined,
      heatmapY: t === 'heatmap' && config.heatmapY ? config.heatmapY : undefined,
      heatmapValue: t === 'heatmap' && config.heatmapValue ? config.heatmapValue : undefined,
      gaugeMin: t === 'gauge' ? (config.gaugeMin ?? 0) : undefined,
      gaugeMax: t === 'gauge' ? (config.gaugeMax ?? 100) : undefined,
      pieDonut: t === 'pie' ? (config.pieDonut ?? false) : undefined,
      pieLabelColumn: t === 'pie' && config.pieLabelColumn ? config.pieLabelColumn : undefined,
      pieValueColumn: t === 'pie' && config.pieValueColumn ? config.pieValueColumn : undefined,
      statField: config.statField || undefined,
      statCalculation: config.statCalculation ?? 'last',
      statUnit: config.statUnit ?? 'none',
      statPrefix: config.statPrefix || undefined,
      statSuffix: config.statSuffix || undefined,
      statDecimals: config.statDecimals,
      statColorMode: config.statColorMode ?? 'none',
      statThresholds: config.statThresholds ?? DEFAULT_THRESHOLDS,
      links: t !== 'stat' && t !== 'gauge' && t !== 'text' && config.links?.length ? config.links : undefined,
    }
  })

  const dashboardRangeLabel = $derived(formatDashboardTimeRangeLabel(dashboardTimeRange))

  function parsePanelConfig(value: Panel | null): Partial<PanelConfig> {
    if (!value?.config) return {}
    try {
      return JSON.parse(value.config) as Partial<PanelConfig>
    } catch {
      return {}
    }
  }

  $effect(() => {
    const currentPanel = panel
    const existingConfig = parsePanelConfig(currentPanel)

    name = currentPanel?.name ?? ''
    description = currentPanel?.description ?? ''
    query = currentPanel?.query ?? ''
    content = existingConfig.content ?? ''
    config = {
      ...existingConfig,
      chartType: existingConfig.chartType ?? (currentPanel?.panel_type as PanelConfig['chartType']) ?? 'table',
      xColumn: existingConfig.xColumn ?? '',
      yColumns: [...new Set(existingConfig.yColumns ?? [])],
      statThresholds: existingConfig.statThresholds ?? DEFAULT_THRESHOLDS.map(t => ({ ...t })),
    }
    queryData = []
    queryMeta = []
    queryError = null
  })

  // Sync panel query into the CodeMirror editor after it mounts
  $effect(() => {
    const q = panel?.query ?? ''
    if (editorComponent && q) {
      const current = editorComponent.getValue()
      if (current !== q) {
        editorComponent.setValue(q)
      }
    }
  })

  // Validate and auto-detect axes when query results change: columns the
  // result no longer has are dropped, and empty axes get the same pick
  // suggestChart makes (resolveChartColumns shares it).
  $effect(() => {
    if (queryMeta.length === 0) return
    const current = untrack(() => config)
    const names = new Set(queryMeta.map(m => m.name))
    const kept = {
      xColumn: current.xColumn && names.has(current.xColumn) ? current.xColumn : '',
      yColumns: (current.yColumns ?? []).filter(c => names.has(c)),
      seriesColumn: current.seriesColumn && names.has(current.seriesColumn) ? current.seriesColumn : undefined,
    }
    const picked = resolveChartColumns(queryMeta, kept)
    const next = { xColumn: picked.xColumn ?? '', yColumns: kept.yColumns.length > 0 ? kept.yColumns : picked.yColumns, seriesColumn: kept.seriesColumn }
    if (
      next.xColumn !== current.xColumn
      || next.seriesColumn !== current.seriesColumn
      || next.yColumns.join('\u0000') !== (current.yColumns ?? []).join('\u0000')
    ) {
      config = { ...current, ...next }
    }
  })

  async function runQuery() {
    const sql = editorComponent?.getValue() ?? query
    if (!sql.trim()) {
      queryError = 'Enter a query first'
      return
    }
    running = true
    queryError = null
    queryData = []
    queryMeta = []
    try {
      const res = await apiPost<{ data: Record<string, unknown>[]; meta: ColumnMeta[]; error?: string; success?: boolean }>('/api/dashboards/query', {
        query: sql.trim(),
        time_range: toDashboardTimeRangePayload(dashboardTimeRange || '1h'),
        dashboard_id: dashboardId || undefined,
        variables,
      })
      if (res.success === false) {
        queryError = res.error ?? 'Query failed'
      } else {
        queryData = res.data ?? []
        queryMeta = res.meta ?? []
      }
    } catch (e: unknown) {
      queryError = e instanceof Error ? e.message : String(e)
    } finally {
      running = false
    }
  }

  async function handleFormat() {
    const sql = editorComponent?.getValue() ?? query
    if (!sql.trim()) return
    try {
      const formatted = await formatSQL(sql)
      editorComponent?.setValue(formatted)
      query = formatted
      toastSuccess('Query formatted')
    } catch (e: unknown) {
      toastErrorFrom(e)
    }
  }

  async function handleSave() {
    const isText = config.chartType === 'text'
    const sql = isText ? 'SELECT 1' : (editorComponent?.getValue() ?? query)
    if (!name.trim()) {
      toastError('Name is required')
      return
    }
    if (!isText && !sql.trim()) {
      toastError('Query is required')
      return
    }
    saving = true
    const configJson = JSON.stringify(currentConfig)
    try {
      if (panel?.id) {
        const res = await apiPut<{ panel: Panel }>(`/api/dashboards/${dashboardId}/panels/${panel.id}`, {
          name: name.trim(),
          description: description.trim(),
          panel_type: config.chartType,
          query: sql.trim(),
          config: configJson,
        })
        if (res.panel) onsave(res.panel)
      } else {
        const res = await apiPost<{ panel: Panel }>(`/api/dashboards/${dashboardId}/panels`, {
          name: name.trim(),
          description: description.trim(),
          panel_type: config.chartType,
          query: sql.trim(),
          config: configJson,
          ...(connectionId ? { connection_id: connectionId } : {}),
          layout_x: 0,
          layout_y: nextRow,
          layout_w: 6,
          layout_h: 4,
        })
        if (res.panel) onsave(res.panel)
      }
    } catch (e: unknown) {
      toastErrorFrom(e)
    } finally {
      saving = false
    }
  }

</script>

<div class="flex flex-col h-full">
  <div class="flex items-center justify-between px-4 py-3 border-b border-edge-subtle">
    <div class="flex items-center gap-2">
      <PanelsTopLeft size={16} class="text-accent" />
      <h2 class="text-[15px] font-semibold text-fg">
        {panel?.id ? 'Edit Panel' : 'New Panel'}
      </h2>
    </div>
    <div class="flex items-center gap-2">
      <Button variant="secondary" size="sm" onclick={onclose}>Cancel</Button>
      <Button size="sm" loading={saving} onclick={handleSave}>
        {panel?.id ? 'Update Panel' : 'Create Panel'}
      </Button>
    </div>
  </div>

  <!-- Editor: left/right split -->
  <div class="flex flex-1 min-h-0 overflow-hidden">
    <!-- Left side: Query workspace / Markdown editor -->
    <div class="flex-[3] flex flex-col border-r border-edge-subtle min-w-0">
      {#if config.chartType === 'text'}
        <!-- Markdown editor -->
        <div class="flex items-center px-3 py-2 border-b border-edge-subtle bg-surface">
          <span class="text-xs font-medium text-fg-3">Markdown Editor</span>
        </div>
        <div class="flex flex-1 min-h-0">
          <!-- Editor -->
          <div class="flex-1 flex flex-col min-w-0 border-r border-edge-subtle">
            <textarea
              class="flex-1 w-full font-mono bg-transparent text-fg resize-none p-4 focus:outline-none"
              bind:value={content}
              placeholder="# My Panel&#10;&#10;Write **markdown** here...&#10;&#10;- Lists&#10;- Links: [example](https://example.com)&#10;- `code` and more"
              spellcheck="false"
            ></textarea>
          </div>
          <!-- Live preview -->
          <div class="flex-1 min-w-0 overflow-y-auto bg-surface">
            <MarkdownPanel content={content} />
          </div>
        </div>
      {:else}
        <Toolbar running={running} onrun={runQuery} onformat={handleFormat} />
        {#if savedQueryOptions.length > 0}
          <div class="flex h-9 shrink-0 items-center gap-2 border-b border-edge-subtle bg-surface px-3 text-xs text-fg-3">
            <label for="panel-saved-query" class="shrink-0">Start from a saved query</label>
            <div class="w-72">
              <Combobox
                id="panel-saved-query"
                options={savedQueryOptions}
                value={savedQueryId}
                placeholder="Choose one"
                emptyText="No saved query matches"
                onChange={(v) => void useSavedQuery(v)}
              />
            </div>
          </div>
        {/if}

        <!-- SQL Editor -->
        <div class="h-[42%] min-h-[200px] shrink-0 border-b border-edge-subtle">
          <SqlEditor
            bind:this={editorComponent}
            value={query}
            onrun={runQuery}
            onchange={(v) => query = v}
            placeholder="SQL for this panel. Run it to preview, then pick a visualization on the right."
          />
        </div>

        <!-- Result area -->
        <div class="flex-1 min-h-0 overflow-auto">
          {#if running}
            <div class="flex items-center justify-center h-full"><Spinner /></div>
          {:else if queryError}
            <div class="p-4">
              <p class="text-[13px] text-danger bg-danger-soft rounded-md p-3">{queryError}</p>
            </div>
          {:else if queryData.length === 0 && queryMeta.length === 0}
            <div class="flex items-center justify-center h-full text-fg-4">
              Run a query to see results
            </div>
          {:else}
            <PanelRenderer type={config.chartType} data={queryData} meta={queryMeta} config={currentConfig} table="virtual" />
          {/if}
        </div>
      {/if}
    </div>

    <!-- Right side: Configuration -->
    <div class="flex-[2] flex flex-col min-w-0 overflow-y-auto">
      <div class="p-4 flex flex-col gap-4">
        <!-- Panel name -->
        <div>
          <label for="panel-name" class="block text-xs font-medium text-fg-2 mb-1">Panel Name</label>
          <input
            id="panel-name"
            type="text"
            class="w-full bg-transparent border border-edge rounded px-3 py-2 text-fg"
            placeholder="My Panel"
            bind:value={name}
          />
        </div>

        <!-- Panel description -->
        <div>
          <label for="panel-description" class="block text-xs font-medium text-fg-2 mb-1">Description</label>
          <input
            id="panel-description"
            type="text"
            class="w-full bg-transparent border border-edge rounded px-3 py-2 text-fg"
            placeholder="Optional description"
            bind:value={description}
          />
        </div>

        <!-- Available variables (query panels only) -->
        {#if config.chartType !== 'text'}
        <details class="group/vars">
          <summary class="flex items-center gap-1.5 text-xs font-medium text-fg-3 cursor-pointer select-none hover:text-fg list-none [&::-webkit-details-marker]:hidden">
            <span class="text-[10px] transition-transform group-open/vars:rotate-90">&#9654;</span>
            Available Variables
          </summary>
          <div class="mt-2 space-y-1.5 text-[11px] text-fg-3">
            <div><code class="px-1 py-0.5 rounded bg-surface-2 text-fg-2">$__timeFilter(col)</code>: range filter for DateTime, DateTime64 or epoch seconds/ms columns</div>
            <div><code class="px-1 py-0.5 rounded bg-surface-2 text-fg-2">$__timeFilter(col, ms)</code>: names the unit: s, ms, us, ns, or datetime (also for Date columns)</div>
            <div><code class="px-1 py-0.5 rounded bg-surface-2 text-fg-2">$__timestamp(col)</code>: DateTime range filter, second precision</div>
            <div><code class="px-1 py-0.5 rounded bg-surface-2 text-fg-2">$__interval</code> / <code class="px-1 py-0.5 rounded bg-surface-2 text-fg-2">$__interval_ms</code>: bucket size in seconds / milliseconds</div>
            <div><code class="px-1 py-0.5 rounded bg-surface-2 text-fg-2">$__timeFrom</code> / <code class="px-1 py-0.5 rounded bg-surface-2 text-fg-2">$__timeTo</code>: range bounds in epoch milliseconds</div>
            <div><code class="px-1 py-0.5 rounded bg-surface-2 text-fg-2">$name</code> / <code class="px-1 py-0.5 rounded bg-surface-2 text-fg-2">${'{'}name{'}'}</code>: dashboard variables, quoted for you (<code class="px-1 py-0.5 rounded bg-surface-2 text-fg-2">IN (${'{'}db{'}'})</code>); <code class="px-1 py-0.5 rounded bg-surface-2 text-fg-2">${'{'}name:number{'}'}</code> for bare numbers</div>
          </div>
        </details>
        {/if}

        <ChartConfigFields bind:config meta={queryMeta} rangeLabel={dashboardRangeLabel} linkVariables={variableDefs} />
      </div>
    </div>
  </div>
</div>
