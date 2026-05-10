<script lang="ts">
  import type { Panel, PanelConfig, StatThreshold } from '../../types/api'
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
  import StatPanel from './StatPanel.svelte'
  import { isDateType, isNumericType, getStatValue, DEFAULT_COLORS, computeStat } from '../../utils/chart-transform'
  import { formatDashboardTimeRangeLabel } from '../../utils/dashboard-time'
  import { toDashboardTimeRangePayload } from '../../utils/dashboard-time'
  import ColorPicker from '../common/ColorPicker.svelte'
  import { Table2, Hash, TrendingUp, BarChart3, PanelsTopLeft, Plus, X } from 'lucide-svelte'

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

  // Stat-specific state
  let statField = $state('')
  let statCalculation = $state<NonNullable<PanelConfig['statCalculation']>>('last')
  let statUnit = $state<NonNullable<PanelConfig['statUnit']>>('none')
  let statPrefix = $state('')
  let statSuffix = $state('')
  let statDecimals = $state<number | undefined>(undefined)
  let statColorMode = $state<NonNullable<PanelConfig['statColorMode']>>('none')
  let statThresholds = $state<StatThreshold[]>([
    { value: 0, color: '#22c55e' },
    { value: 80, color: '#f59e0b' },
    { value: 90, color: '#ef4444' },
  ])

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
    statField: statField || undefined,
    statCalculation,
    statUnit,
    statPrefix: statPrefix || undefined,
    statSuffix: statSuffix || undefined,
    statDecimals,
    statColorMode,
    statThresholds,
  })

  const statPreview = $derived(computeStat(queryData, queryMeta, currentConfig))

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
    // Stat options
    statField = existingConfig.statField ?? ''
    statCalculation = existingConfig.statCalculation ?? 'last'
    statUnit = existingConfig.statUnit ?? 'none'
    statPrefix = existingConfig.statPrefix ?? ''
    statSuffix = existingConfig.statSuffix ?? ''
    statDecimals = existingConfig.statDecimals
    statColorMode = existingConfig.statColorMode ?? 'none'
    statThresholds = existingConfig.statThresholds ?? [
      { value: 0, color: '#22c55e' },
      { value: 80, color: '#f59e0b' },
      { value: 90, color: '#ef4444' },
    ]
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

  // Validate and auto-detect axes when query results change
  $effect(() => {
    if (queryMeta.length === 0) return

    const colNames = new Set(queryMeta.map(m => m.name))

    if (xColumn && !colNames.has(xColumn)) {
      xColumn = ''
    }
    const validY = yColumns.filter(c => colNames.has(c))
    if (validY.length !== yColumns.length) {
      yColumns = validY
    }

    if (!xColumn) {
      const firstDate = dateColumns[0]
      xColumn = firstDate ? firstDate.name : queryMeta[0].name
    }
    if (yColumns.length === 0) {
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
          <StatPanel stat={statPreview} />
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

        <!-- Stat config -->
        {#if chartType === 'stat'}
          <!-- Field selector -->
          <div>
            <label class="block text-xs font-medium text-gray-700 dark:text-gray-300 mb-1">Field</label>
            <Combobox
              options={[
                { value: '', label: 'Auto (first numeric)' },
                ...queryMeta.map(col => ({ value: col.name, label: col.name, hint: col.type, keywords: `${col.name} ${col.type}` })),
              ]}
              value={statField}
              onChange={(v) => statField = v}
              placeholder="Auto"
            />
          </div>

          <!-- Calculation -->
          <div>
            <label class="block text-xs font-medium text-gray-700 dark:text-gray-300 mb-1">Calculation</label>
            <div class="grid grid-cols-4 gap-1">
              {#each ['last', 'first', 'mean', 'sum', 'min', 'max', 'count', 'range'] as calc}
                <button
                  class="py-1.5 px-1 rounded text-[11px] font-medium border transition-colors
                    {statCalculation === calc
                      ? 'border-ch-blue bg-orange-50 dark:bg-orange-900/20 text-ch-blue'
                      : 'border-gray-200 dark:border-gray-700 text-gray-500 hover:border-gray-300 dark:hover:border-gray-600 hover:text-gray-700 dark:hover:text-gray-300'}"
                  onclick={() => statCalculation = calc as typeof statCalculation}
                >
                  {calc.charAt(0).toUpperCase() + calc.slice(1)}
                </button>
              {/each}
            </div>
          </div>

          <!-- Unit -->
          <div>
            <label class="block text-xs font-medium text-gray-700 dark:text-gray-300 mb-1">Unit</label>
            <Combobox
              options={[
                { value: 'none', label: 'None' },
                { value: 'short', label: 'Short (K/M/B)' },
                { value: 'percent', label: 'Percent (%)' },
                { value: 'bytes', label: 'Bytes (KB/MB/GB)' },
                { value: 'bps', label: 'Bytes/sec' },
                { value: 'duration', label: 'Duration (s)' },
                { value: 'durationMs', label: 'Duration (ms)' },
              ]}
              value={statUnit}
              onChange={(v) => statUnit = v as typeof statUnit}
              placeholder="None"
            />
          </div>

          <!-- Prefix / Suffix / Decimals row -->
          <div class="grid grid-cols-3 gap-2">
            <div>
              <label class="block text-xs font-medium text-gray-700 dark:text-gray-300 mb-1">Prefix</label>
              <input
                type="text"
                class="w-full text-sm bg-transparent border border-gray-300 dark:border-gray-700 rounded px-2 py-1.5 text-gray-800 dark:text-gray-200"
                placeholder="$"
                bind:value={statPrefix}
              />
            </div>
            <div>
              <label class="block text-xs font-medium text-gray-700 dark:text-gray-300 mb-1">Suffix</label>
              <input
                type="text"
                class="w-full text-sm bg-transparent border border-gray-300 dark:border-gray-700 rounded px-2 py-1.5 text-gray-800 dark:text-gray-200"
                placeholder="%"
                bind:value={statSuffix}
              />
            </div>
            <div>
              <label class="block text-xs font-medium text-gray-700 dark:text-gray-300 mb-1">Decimals</label>
              <input
                type="number"
                min="0"
                max="10"
                class="w-full text-sm bg-transparent border border-gray-300 dark:border-gray-700 rounded px-2 py-1.5 text-gray-800 dark:text-gray-200"
                placeholder="Auto"
                value={statDecimals ?? ''}
                oninput={(e) => {
                  const v = (e.target as HTMLInputElement).value
                  statDecimals = v === '' ? undefined : Number(v)
                }}
              />
            </div>
          </div>

          <!-- Color mode -->
          <div>
            <label class="block text-xs font-medium text-gray-700 dark:text-gray-300 mb-1">Color Mode</label>
            <div class="grid grid-cols-3 gap-1">
              {#each [
                { value: 'none', label: 'None' },
                { value: 'value', label: 'Value' },
                { value: 'background', label: 'Background' },
              ] as cm}
                <button
                  class="py-1.5 rounded text-[11px] font-medium border transition-colors
                    {statColorMode === cm.value
                      ? 'border-ch-blue bg-orange-50 dark:bg-orange-900/20 text-ch-blue'
                      : 'border-gray-200 dark:border-gray-700 text-gray-500 hover:border-gray-300 dark:hover:border-gray-600'}"
                  onclick={() => statColorMode = cm.value as typeof statColorMode}
                >
                  {cm.label}
                </button>
              {/each}
            </div>
          </div>

          <!-- Thresholds -->
          {#if statColorMode !== 'none'}
            <div>
              <div class="flex items-center justify-between mb-1">
                <label class="text-xs font-medium text-gray-700 dark:text-gray-300">Thresholds</label>
                <button
                  class="flex items-center gap-0.5 text-[11px] text-ch-blue hover:text-orange-600 transition-colors"
                  onclick={() => {
                    const lastVal = statThresholds.length > 0 ? statThresholds[statThresholds.length - 1].value + 10 : 0
                    statThresholds = [...statThresholds, { value: lastVal, color: '#ef4444' }]
                  }}
                >
                  <Plus size={12} /> Add
                </button>
              </div>
              <div class="flex flex-col gap-1.5">
                {#each statThresholds as threshold, i}
                  <div class="flex items-center gap-2">
                    <ColorPicker
                      value={threshold.color}
                      onchange={(c) => {
                        const next = [...statThresholds]
                        next[i] = { ...next[i], color: c }
                        statThresholds = next
                      }}
                    />
                    {#if i === 0}
                      <span class="flex-1 text-xs text-gray-400">Base</span>
                    {:else}
                      <input
                        type="number"
                        value={threshold.value}
                        oninput={(e) => {
                          const next = [...statThresholds]
                          next[i] = { ...next[i], value: Number((e.target as HTMLInputElement).value) }
                          statThresholds = next
                        }}
                        class="flex-1 text-sm bg-transparent border border-gray-300 dark:border-gray-700 rounded px-2 py-1 text-gray-800 dark:text-gray-200"
                      />
                    {/if}
                    {#if statThresholds.length > 1}
                      <button
                        class="p-0.5 rounded text-gray-400 hover:text-red-400 transition-colors"
                        onclick={() => statThresholds = statThresholds.filter((_, j) => j !== i)}
                      >
                        <X size={14} />
                      </button>
                    {/if}
                  </div>
                {/each}
              </div>
            </div>
          {/if}
        {/if}

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
                    <ColorPicker
                      value={colors[i] ?? DEFAULT_COLORS[i % DEFAULT_COLORS.length]}
                      onchange={(c) => updateColor(i, c)}
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
