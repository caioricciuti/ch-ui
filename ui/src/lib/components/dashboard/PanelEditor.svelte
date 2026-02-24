<script lang="ts">
  import type { Panel, PanelConfig } from '../../types/api'
  import type { ColumnMeta } from '../../types/query'
  import { apiPost, apiPut } from '../../api/client'
  import { formatSQL } from '../../api/query'
  import { success as toastSuccess, error as toastError } from '../../stores/toast.svelte'
  import Button from '../common/Button.svelte'
  import Combobox from '../common/Combobox.svelte'
  import type { ComboboxOption } from '../common/Combobox.svelte'
  import Spinner from '../common/Spinner.svelte'
  import Toolbar from '../editor/Toolbar.svelte'
  import SqlEditor from '../editor/SqlEditor.svelte'
  import VirtualTable from '../table/VirtualTable.svelte'
  import ChartPanel from './ChartPanel.svelte'
  import { isDateType, isNumericType, getStatValue, DEFAULT_COLORS } from '../../utils/chart-transform'
  import { formatDashboardTimeRangeLabel } from '../../utils/dashboard-time'
  import { toDashboardTimeRangePayload } from '../../utils/dashboard-time'
  import { Table2, Hash, TrendingUp, BarChart3, PanelsTopLeft } from 'lucide-svelte'

  interface Props {
    dashboardId: string
    dashboardTimeRange?: string
    panel?: Panel | null
    onclose: () => void
    onsave: (panel: Panel) => void
  }

  let { dashboardId, dashboardTimeRange = '1h', panel = null, onclose, onsave }: Props = $props()

  // Form state
  let name = $state('')
  let query = $state('')
  let saving = $state(false)
  let running = $state(false)
  let formatting = $state(false)

  let chartType = $state<PanelConfig['chartType']>('table')
  let xColumn = $state('')
  let yColumns = $state<string[]>([])
  let colors = $state<string[]>([...DEFAULT_COLORS])
  let legendPosition = $state<'bottom' | 'right' | 'none'>('bottom')

  // Query result state
  let queryData = $state<Record<string, unknown>[]>([])
  let queryMeta = $state<ColumnMeta[]>([])
  let queryError = $state<string | null>(null)

  let editorComponent: SqlEditor | undefined = $state()

  // Derived: VirtualTable needs positional arrays
  const vtData = $derived(queryData.map(row => queryMeta.map(col => row[col.name])))
  const vtMeta = $derived(queryMeta.map(m => ({ name: m.name, type: m.type })))

  // Derived: Detect columns by type
  const dateColumns = $derived(queryMeta.filter(m => isDateType(m.type)))
  const numericColumns = $derived(queryMeta.filter(m => isNumericType(m.type)))

  // Current config for chart preview
  const currentConfig = $derived<PanelConfig>({
    chartType,
    xColumn,
    yColumns,
    colors,
    legendPosition,
  })

  const dashboardRangeLabel = $derived(formatDashboardTimeRangeLabel(dashboardTimeRange))
  const xAxisOptions = $derived.by<ComboboxOption[]>(() => [
    { value: '', label: 'Select column...' },
    ...queryMeta.map(col => ({ value: col.name, label: `${col.name}`, hint: col.type, keywords: `${col.name} ${col.type}` })),
  ])
  const legendOptions: ComboboxOption[] = [
    { value: 'bottom', label: 'Bottom' },
    { value: 'right', label: 'Right' },
    { value: 'none', label: 'Hidden' },
  ]

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
    query = currentPanel?.query ?? ''
    chartType = existingConfig.chartType ?? (currentPanel?.panel_type as PanelConfig['chartType']) ?? 'table'
    xColumn = existingConfig.xColumn ?? ''
    yColumns = existingConfig.yColumns ?? []
    colors = existingConfig.colors ?? [...DEFAULT_COLORS]
    legendPosition = existingConfig.legendPosition ?? 'bottom'
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

  // Auto-detect axes when results arrive
  $effect(() => {
    if (queryMeta.length > 0 && !xColumn && !yColumns.length) {
      // Auto-pick first date column for X, or first column
      const firstDate = dateColumns[0]
      if (firstDate) {
        xColumn = firstDate.name
      } else if (queryMeta.length > 0) {
        xColumn = queryMeta[0].name
      }
      // Auto-pick first numeric columns for Y
      const autoY = numericColumns.filter(m => m.name !== xColumn).slice(0, 3)
      if (autoY.length > 0) {
        yColumns = autoY.map(m => m.name)
      }
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
      const res = await apiPost<{ data: any[]; meta: any[]; error?: string; success?: boolean }>('/api/dashboards/query', {
        query: sql.trim(),
        time_range: toDashboardTimeRangePayload(dashboardTimeRange || '1h'),
      })
      if (res.success === false) {
        queryError = res.error ?? 'Query failed'
      } else {
        queryData = res.data ?? []
        queryMeta = res.meta ?? []
      }
    } catch (e: any) {
      queryError = e.message
    } finally {
      running = false
    }
  }

  async function handleFormat() {
    const sql = editorComponent?.getValue() ?? query
    if (!sql.trim()) return
    formatting = true
    try {
      const formatted = await formatSQL(sql)
      editorComponent?.setValue(formatted)
      query = formatted
      toastSuccess('Query formatted')
    } catch (e: any) {
      toastError(e.message)
    } finally {
      formatting = false
    }
  }

  function toggleYColumn(colName: string) {
    if (yColumns.includes(colName)) {
      yColumns = yColumns.filter(c => c !== colName)
    } else {
      yColumns = [...yColumns, colName]
    }
  }

  function updateColor(index: number, color: string) {
    const next = [...colors]
    next[index] = color
    colors = next
  }

  async function handleSave() {
    const sql = editorComponent?.getValue() ?? query
    if (!name.trim() || !sql.trim()) {
      toastError('Name and query are required')
      return
    }
    saving = true
    const configJson = JSON.stringify(currentConfig)
    try {
      if (panel?.id) {
        const res = await apiPut<{ panel: Panel }>(`/api/dashboards/${dashboardId}/panels/${panel.id}`, {
          name: name.trim(),
          panel_type: chartType,
          query: sql.trim(),
          config: configJson,
        })
        if (res.panel) onsave(res.panel)
      } else {
        const res = await apiPost<{ panel: Panel }>(`/api/dashboards/${dashboardId}/panels`, {
          name: name.trim(),
          panel_type: chartType,
          query: sql.trim(),
          config: configJson,
          layout_x: 0,
          layout_y: 0,
          layout_w: 6,
          layout_h: 4,
        })
        if (res.panel) onsave(res.panel)
      }
    } catch (e: any) {
      toastError(e.message)
    } finally {
      saving = false
    }
  }

  const vizTypes: { type: PanelConfig['chartType']; label: string; icon: typeof Table2 }[] = [
    { type: 'table', label: 'Table', icon: Table2 },
    { type: 'stat', label: 'Stat', icon: Hash },
    { type: 'timeseries', label: 'Time Series', icon: TrendingUp },
    { type: 'bar', label: 'Bar', icon: BarChart3 },
  ]
</script>

<div class="flex flex-col h-full">
  <div class="flex items-center justify-between px-4 py-3 border-b border-gray-200 dark:border-gray-800">
    <div class="flex items-center gap-2">
      <PanelsTopLeft size={16} class="text-ch-orange" />
      <h2 class="text-sm font-semibold text-gray-900 dark:text-gray-100">
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
    <!-- Left side: Query workspace -->
    <div class="flex-[3] flex flex-col border-r border-gray-200 dark:border-gray-800 min-w-0">
      <Toolbar running={running} onrun={runQuery} onformat={handleFormat} onsave={handleSave} />

      <!-- SQL Editor -->
      <div class="h-[42%] min-h-[200px] shrink-0 border-b border-gray-200 dark:border-gray-800">
        <SqlEditor
          bind:this={editorComponent}
          value={query}
          onrun={runQuery}
          onchange={(v) => query = v}
        />
      </div>

      <!-- Result area -->
      <div class="flex-1 min-h-0 overflow-auto">
        {#if running}
          <div class="flex items-center justify-center h-full"><Spinner /></div>
        {:else if queryError}
          <div class="p-4">
            <p class="text-sm text-red-500 bg-red-50 dark:bg-red-900/20 rounded p-3">{queryError}</p>
          </div>
        {:else if queryData.length === 0 && queryMeta.length === 0}
          <div class="flex items-center justify-center h-full text-gray-400 text-sm">
            Run a query to see results
          </div>
        {:else if chartType === 'table'}
          <VirtualTable meta={vtMeta} data={vtData} />
        {:else if chartType === 'stat'}
          <div class="flex items-center justify-center h-full">
            <span class="text-5xl font-bold text-gray-900 dark:text-gray-100">
              {getStatValue(queryData, queryMeta)}
            </span>
          </div>
        {:else}
          <ChartPanel
            data={queryData}
            meta={queryMeta}
            config={currentConfig}
          />
        {/if}
      </div>
    </div>

    <!-- Right side: Configuration -->
    <div class="flex-[2] flex flex-col min-w-0 overflow-y-auto">
      <div class="p-4 flex flex-col gap-4">
        <!-- Panel name -->
        <div>
          <label for="panel-name" class="block text-xs font-medium text-gray-700 dark:text-gray-300 mb-1">Panel Name</label>
          <input
            id="panel-name"
            type="text"
            class="w-full text-sm bg-transparent border border-gray-300 dark:border-gray-700 rounded px-3 py-2 text-gray-800 dark:text-gray-200"
            placeholder="My Panel"
            bind:value={name}
          />
        </div>

        <!-- Available variables -->
        <details class="group/vars">
          <summary class="flex items-center gap-1.5 text-xs font-medium text-gray-500 dark:text-gray-400 cursor-pointer select-none hover:text-gray-700 dark:hover:text-gray-300 list-none [&::-webkit-details-marker]:hidden">
            <span class="text-[10px] transition-transform group-open/vars:rotate-90">&#9654;</span>
            Available Variables
          </summary>
          <div class="mt-2 space-y-1.5 text-[11px] text-gray-500 dark:text-gray-400">
            <div><code class="px-1 py-0.5 rounded bg-gray-200 dark:bg-gray-800 text-gray-600 dark:text-gray-300">$__timestamp(col)</code> — DateTime range filter</div>
            <div><code class="px-1 py-0.5 rounded bg-gray-200 dark:bg-gray-800 text-gray-600 dark:text-gray-300">$__timeFilter(col)</code> — Epoch range filter</div>
            <div><code class="px-1 py-0.5 rounded bg-gray-200 dark:bg-gray-800 text-gray-600 dark:text-gray-300">$__interval</code> — Aggregation interval (seconds)</div>
            <div><code class="px-1 py-0.5 rounded bg-gray-200 dark:bg-gray-800 text-gray-600 dark:text-gray-300">$__timeFrom</code> / <code class="px-1 py-0.5 rounded bg-gray-200 dark:bg-gray-800 text-gray-600 dark:text-gray-300">$__timeTo</code> — Range boundaries</div>
          </div>
        </details>

        <!-- Visualization type -->
        <div>
          <p class="block text-xs font-medium text-gray-700 dark:text-gray-300 mb-2">Visualization</p>
          <div class="grid grid-cols-4 gap-1.5">
            {#each vizTypes as vt}
              {@const Icon = vt.icon}
              <button
                class="flex flex-col items-center gap-1 py-2 px-1 rounded-md border text-xs transition-colors
                  {chartType === vt.type
                    ? 'border-ch-blue bg-orange-50 dark:bg-orange-900/20 text-ch-blue'
                    : 'border-gray-200 dark:border-gray-700 text-gray-500 hover:border-gray-300 dark:hover:border-gray-600 hover:text-gray-700 dark:hover:text-gray-300'}"
                onclick={() => chartType = vt.type}
              >
                <Icon size={16} />
                {vt.label}
              </button>
            {/each}
          </div>
        </div>

        <!-- Chart config (only for timeseries/bar) -->
        {#if chartType === 'timeseries' || chartType === 'bar'}
          <div class="rounded-lg border border-gray-200 dark:border-gray-800 bg-gray-100/60 dark:bg-gray-900/60 px-2.5 py-2">
            <p class="text-[11px] uppercase tracking-wide text-gray-500 dark:text-gray-400">Time Scope</p>
            <p class="mt-0.5 text-xs text-gray-700 dark:text-gray-300">
              Uses dashboard picker: <span class="text-ch-orange">{dashboardRangeLabel}</span>
            </p>
          </div>

          <!-- X-Axis column -->
          <div>
            <label for="x-axis-column" class="block text-xs font-medium text-gray-700 dark:text-gray-300 mb-1">X-Axis Column</label>
            <Combobox
              options={xAxisOptions}
              value={xColumn}
              onChange={(v) => xColumn = v}
              placeholder="Select column..."
            />
          </div>

          <!-- Y-Axis columns -->
          <div>
            <p class="block text-xs font-medium text-gray-700 dark:text-gray-300 mb-1">Y-Axis Columns</p>
            {#if queryMeta.length === 0}
              <p class="text-xs text-gray-400">Run a query first</p>
            {:else}
              <div class="flex flex-col gap-1 max-h-40 overflow-y-auto">
                {#each queryMeta.filter(m => m.name !== xColumn) as col}
                  <label class="flex items-center gap-2 text-xs text-gray-700 dark:text-gray-300 cursor-pointer hover:bg-gray-100 dark:hover:bg-gray-800 rounded px-2 py-1">
                    <input
                      type="checkbox"
                      class="ds-checkbox ds-checkbox-sm"
                      checked={yColumns.includes(col.name)}
                      onchange={() => toggleYColumn(col.name)}
                    />
                    <span class="truncate">{col.name}</span>
                    <span class="text-gray-400 ml-auto shrink-0">{col.type}</span>
                  </label>
                {/each}
              </div>
            {/if}
          </div>

          <!-- Series colors -->
          {#if yColumns.length > 0}
            <div>
              <p class="block text-xs font-medium text-gray-700 dark:text-gray-300 mb-1">Series Colors</p>
              <div class="flex flex-col gap-1.5">
                {#each yColumns as yCol, i}
                  <div class="flex items-center gap-2">
                    <input
                      type="color"
                      value={colors[i] ?? DEFAULT_COLORS[i % DEFAULT_COLORS.length]}
                      oninput={(e) => updateColor(i, (e.target as HTMLInputElement).value)}
                      class="w-6 h-6 rounded border border-gray-300 dark:border-gray-600 cursor-pointer"
                    />
                    <span class="text-xs text-gray-600 dark:text-gray-400 truncate">{yCol}</span>
                  </div>
                {/each}
              </div>
            </div>
          {/if}

          <!-- Legend position -->
          <div>
            <label for="legend-position" class="block text-xs font-medium text-gray-700 dark:text-gray-300 mb-1">Legend</label>
            <Combobox
              options={legendOptions}
              value={legendPosition}
              onChange={(v) => legendPosition = v as 'bottom' | 'right' | 'none'}
              placeholder="Legend position"
            />
          </div>
        {/if}
      </div>
    </div>
  </div>
</div>
