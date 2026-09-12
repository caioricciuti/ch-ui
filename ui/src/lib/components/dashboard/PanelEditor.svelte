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
  import { isDateType, isNumericType, DEFAULT_COLORS, computeStat } from '../../utils/chart-transform'
  import { formatDashboardTimeRangeLabel } from '../../utils/dashboard-time'
  import { toDashboardTimeRangePayload } from '../../utils/dashboard-time'
  import ColorPicker from '../common/ColorPicker.svelte'
  import MarkdownPanel from './MarkdownPanel.svelte'
  import GaugePanel from './GaugePanel.svelte'
  import PiePanel from './PiePanel.svelte'
  import { Table2, Hash, TrendingUp, BarChart3, PanelsTopLeft, Plus, X, FileText, Gauge, PieChart } from 'lucide-svelte'

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
  let description = $state('')
  let query = $state('')
  let content = $state('')
  let saving = $state(false)
  let running = $state(false)

  let chartType = $state<PanelConfig['chartType']>('table')
  let xColumn = $state('')
  let yColumns = $state<string[]>([])
  let colors = $state<string[]>([...DEFAULT_COLORS])
  let legendPosition = $state<'bottom' | 'right' | 'none'>('bottom')

  // Gauge-specific state
  let gaugeMin = $state(0)
  let gaugeMax = $state(100)

  // Bar mode state
  let barMode = $state<'grouped' | 'stacked'>('grouped')

  // Pie-specific state
  let pieLabelColumn = $state('')
  let pieValueColumn = $state('')
  let pieDonut = $state(false)

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
    content: content || undefined,
    barMode: chartType === 'bar' ? barMode : undefined,
    gaugeMin: chartType === 'gauge' ? gaugeMin : undefined,
    gaugeMax: chartType === 'gauge' ? gaugeMax : undefined,
    pieDonut: chartType === 'pie' ? pieDonut : undefined,
    pieLabelColumn: chartType === 'pie' && pieLabelColumn ? pieLabelColumn : undefined,
    pieValueColumn: chartType === 'pie' && pieValueColumn ? pieValueColumn : undefined,
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
    description = currentPanel?.description ?? ''
    query = currentPanel?.query ?? ''
    content = existingConfig.content ?? ''
    chartType = existingConfig.chartType ?? (currentPanel?.panel_type as PanelConfig['chartType']) ?? 'table'
    xColumn = existingConfig.xColumn ?? ''
    yColumns = existingConfig.yColumns ?? []
    colors = existingConfig.colors ?? [...DEFAULT_COLORS]
    legendPosition = existingConfig.legendPosition ?? 'bottom'
    // Gauge options
    gaugeMin = existingConfig.gaugeMin ?? 0
    gaugeMax = existingConfig.gaugeMax ?? 100
    // Bar mode
    barMode = existingConfig.barMode ?? 'grouped'
    // Pie options
    pieLabelColumn = existingConfig.pieLabelColumn ?? ''
    pieValueColumn = existingConfig.pieValueColumn ?? ''
    pieDonut = existingConfig.pieDonut ?? false
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
    try {
      const formatted = await formatSQL(sql)
      editorComponent?.setValue(formatted)
      query = formatted
      toastSuccess('Query formatted')
    } catch (e: any) {
      toastError(e.message)
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
    const isText = chartType === 'text'
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
          panel_type: chartType,
          query: sql.trim(),
          config: configJson,
        })
        if (res.panel) onsave(res.panel)
      } else {
        const res = await apiPost<{ panel: Panel }>(`/api/dashboards/${dashboardId}/panels`, {
          name: name.trim(),
          description: description.trim(),
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
    { type: 'gauge', label: 'Gauge', icon: Gauge },
    { type: 'timeseries', label: 'Time Series', icon: TrendingUp },
    { type: 'bar', label: 'Bar', icon: BarChart3 },
    { type: 'pie', label: 'Pie', icon: PieChart },
    { type: 'text', label: 'Text', icon: FileText },
  ]
</script>

<div class="flex flex-col h-full">
  <div class="flex items-center justify-between px-4 py-3 border-b border-edge-subtle">
    <div class="flex items-center gap-2">
      <PanelsTopLeft size={16} class="text-accent" />
      <h2 class="text-sm font-semibold text-fg">
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
      {#if chartType === 'text'}
        <!-- Markdown editor -->
        <div class="flex items-center px-3 py-2 border-b border-edge-subtle bg-surface">
          <span class="text-xs font-medium text-fg-3">Markdown Editor</span>
        </div>
        <div class="flex flex-1 min-h-0">
          <!-- Editor -->
          <div class="flex-1 flex flex-col min-w-0 border-r border-edge-subtle">
            <textarea
              class="flex-1 w-full font-mono text-sm bg-transparent text-fg resize-none p-4 focus:outline-none"
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
        <Toolbar running={running} onrun={runQuery} onformat={handleFormat} onsave={handleSave} />

        <!-- SQL Editor -->
        <div class="h-[42%] min-h-[200px] shrink-0 border-b border-edge-subtle">
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
              <p class="text-[13px] text-danger bg-danger-soft rounded-md p-3">{queryError}</p>
            </div>
          {:else if queryData.length === 0 && queryMeta.length === 0}
            <div class="flex items-center justify-center h-full text-fg-4 text-sm">
              Run a query to see results
            </div>
          {:else if chartType === 'table'}
            <VirtualTable meta={vtMeta} data={vtData} />
          {:else if chartType === 'stat'}
            <StatPanel stat={statPreview} />
          {:else if chartType === 'gauge'}
            <GaugePanel stat={statPreview} min={gaugeMin} max={gaugeMax} />
          {:else if chartType === 'pie'}
            <PiePanel data={queryData} meta={queryMeta} config={currentConfig} />
          {:else}
            <ChartPanel
              data={queryData}
              meta={queryMeta}
              config={currentConfig}
            />
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
            class="w-full text-sm bg-transparent border border-edge rounded px-3 py-2 text-fg"
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
            class="w-full text-sm bg-transparent border border-edge rounded px-3 py-2 text-fg"
            placeholder="Optional description"
            bind:value={description}
          />
        </div>

        <!-- Available variables (query panels only) -->
        {#if chartType !== 'text'}
        <details class="group/vars">
          <summary class="flex items-center gap-1.5 text-xs font-medium text-fg-3 cursor-pointer select-none hover:text-fg list-none [&::-webkit-details-marker]:hidden">
            <span class="text-[10px] transition-transform group-open/vars:rotate-90">&#9654;</span>
            Available Variables
          </summary>
          <div class="mt-2 space-y-1.5 text-[11px] text-fg-3">
            <div><code class="px-1 py-0.5 rounded bg-surface-2 text-fg-2">$__timestamp(col)</code> — DateTime range filter</div>
            <div><code class="px-1 py-0.5 rounded bg-surface-2 text-fg-2">$__timeFilter(col)</code> — Epoch range filter</div>
            <div><code class="px-1 py-0.5 rounded bg-surface-2 text-fg-2">$__interval</code> — Aggregation interval (seconds)</div>
            <div><code class="px-1 py-0.5 rounded bg-surface-2 text-fg-2">$__timeFrom</code> / <code class="px-1 py-0.5 rounded bg-surface-2 text-fg-2">$__timeTo</code> — Range boundaries</div>
          </div>
        </details>
        {/if}

        <!-- Visualization type -->
        <div>
          <p class="block text-xs font-medium text-fg-2 mb-2">Visualization</p>
          <div class="grid grid-cols-4 gap-1.5">
            {#each vizTypes as vt}
              {@const Icon = vt.icon}
              <button
                class="flex flex-col items-center gap-1 py-2 px-1 rounded-md border text-xs transition-colors
 {chartType === vt.type
                    ? 'border-accent bg-accent-soft text-accent'
                    : 'border-edge-subtle text-fg-3 hover:border-edge hover:text-fg'}"
                onclick={() => chartType = vt.type}
              >
                <Icon size={16} />
                {vt.label}
              </button>
            {/each}
          </div>
        </div>

        <!-- Stat/Gauge config -->
        {#if chartType === 'stat' || chartType === 'gauge'}
          <!-- Field selector -->
          <div>
            <span class="block text-xs font-medium text-fg-2 mb-1">Field</span>
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
            <span id="panel-stat-calc-label" class="block text-xs font-medium text-fg-2 mb-1">Calculation</span>
            <div class="grid grid-cols-4 gap-1" role="group" aria-labelledby="panel-stat-calc-label">
              {#each ['last', 'first', 'mean', 'sum', 'min', 'max', 'count', 'range'] as calc}
                <button
                  class="py-1.5 px-1 rounded text-[11px] font-medium border transition-colors
 {statCalculation === calc
                      ? 'border-accent bg-accent-soft text-accent'
                      : 'border-edge-subtle text-fg-3 hover:border-edge hover:text-fg'}"
                  onclick={() => statCalculation = calc as typeof statCalculation}
                >
                  {calc.charAt(0).toUpperCase() + calc.slice(1)}
                </button>
              {/each}
            </div>
          </div>

          <!-- Unit -->
          <div>
            <span class="block text-xs font-medium text-fg-2 mb-1">Unit</span>
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
              <label for="panel-stat-prefix" class="block text-xs font-medium text-fg-2 mb-1">Prefix</label>
              <input
                id="panel-stat-prefix"
                type="text"
                class="w-full text-sm bg-transparent border border-edge rounded px-2 py-1.5 text-fg"
                placeholder="$"
                bind:value={statPrefix}
              />
            </div>
            <div>
              <label for="panel-stat-suffix" class="block text-xs font-medium text-fg-2 mb-1">Suffix</label>
              <input
                id="panel-stat-suffix"
                type="text"
                class="w-full text-sm bg-transparent border border-edge rounded px-2 py-1.5 text-fg"
                placeholder="%"
                bind:value={statSuffix}
              />
            </div>
            <div>
              <label for="panel-stat-decimals" class="block text-xs font-medium text-fg-2 mb-1">Decimals</label>
              <input
                id="panel-stat-decimals"
                type="number"
                min="0"
                max="10"
                class="w-full text-sm bg-transparent border border-edge rounded px-2 py-1.5 text-fg"
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
            <span id="panel-stat-colormode-label" class="block text-xs font-medium text-fg-2 mb-1">Color Mode</span>
            <div class="grid grid-cols-3 gap-1" role="group" aria-labelledby="panel-stat-colormode-label">
              {#each [
                { value: 'none', label: 'None' },
                { value: 'value', label: 'Value' },
                { value: 'background', label: 'Background' },
              ] as cm}
                <button
                  class="py-1.5 rounded text-[11px] font-medium border transition-colors
 {statColorMode === cm.value
                      ? 'border-accent bg-accent-soft text-accent'
                      : 'border-edge-subtle text-fg-3 hover:border-edge '}"
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
                <span class="text-xs font-medium text-fg-2">Thresholds</span>
                <button
                  class="flex items-center gap-0.5 text-[11px] text-accent hover:underline transition-colors"
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
                      <span class="flex-1 text-xs text-fg-4">Base</span>
                    {:else}
                      <input
                        type="number"
                        value={threshold.value}
                        oninput={(e) => {
                          const next = [...statThresholds]
                          next[i] = { ...next[i], value: Number((e.target as HTMLInputElement).value) }
                          statThresholds = next
                        }}
                        class="flex-1 text-sm bg-transparent border border-edge rounded px-2 py-1 text-fg"
                      />
                    {/if}
                    {#if statThresholds.length > 1}
                      <button
                        class="p-0.5 rounded text-fg-4 hover:text-danger transition-colors"
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

          <!-- Gauge min/max -->
          {#if chartType === 'gauge'}
            <div class="grid grid-cols-2 gap-2">
              <div>
                <label for="panel-gauge-min" class="block text-xs font-medium text-fg-2 mb-1">Min</label>
                <input
                  id="panel-gauge-min"
                  type="number"
                  class="w-full text-sm bg-transparent border border-edge rounded px-2 py-1.5 text-fg"
                  bind:value={gaugeMin}
                />
              </div>
              <div>
                <label for="panel-gauge-max" class="block text-xs font-medium text-fg-2 mb-1">Max</label>
                <input
                  id="panel-gauge-max"
                  type="number"
                  class="w-full text-sm bg-transparent border border-edge rounded px-2 py-1.5 text-fg"
                  bind:value={gaugeMax}
                />
              </div>
            </div>
          {/if}
        {/if}

        <!-- Pie config -->
        {#if chartType === 'pie'}
          <div>
            <span class="block text-xs font-medium text-fg-2 mb-1">Label Column</span>
            <Combobox
              options={[
                { value: '', label: 'Auto (first string)' },
                ...queryMeta.map(col => ({ value: col.name, label: col.name, hint: col.type, keywords: `${col.name} ${col.type}` })),
              ]}
              value={pieLabelColumn}
              onChange={(v) => pieLabelColumn = v}
              placeholder="Auto"
            />
          </div>
          <div>
            <span class="block text-xs font-medium text-fg-2 mb-1">Value Column</span>
            <Combobox
              options={[
                { value: '', label: 'Auto (first numeric)' },
                ...queryMeta.map(col => ({ value: col.name, label: col.name, hint: col.type, keywords: `${col.name} ${col.type}` })),
              ]}
              value={pieValueColumn}
              onChange={(v) => pieValueColumn = v}
              placeholder="Auto"
            />
          </div>
          <label class="flex items-center gap-2 text-xs text-fg-2 cursor-pointer">
            <input type="checkbox" class="ds-checkbox ds-checkbox-sm" bind:checked={pieDonut} />
            Donut mode
          </label>
        {/if}

        <!-- Chart config (only for timeseries/bar) -->
        {#if chartType === 'timeseries' || chartType === 'bar'}
          <div class="rounded-lg border border-edge-subtle bg-surface-2 px-2.5 py-2">
            <p class="text-[11px] uppercase tracking-wide text-fg-3">Time Scope</p>
            <p class="mt-0.5 text-xs text-fg-2">
              Uses dashboard picker: <span class="text-accent">{dashboardRangeLabel}</span>
            </p>
          </div>

          <!-- Bar mode toggle -->
          {#if chartType === 'bar'}
            <div>
              <span id="panel-bar-mode-label" class="block text-xs font-medium text-fg-2 mb-1">Bar Mode</span>
              <div class="grid grid-cols-2 gap-1" role="group" aria-labelledby="panel-bar-mode-label">
                {#each [{ value: 'grouped', label: 'Grouped' }, { value: 'stacked', label: 'Stacked' }] as bm}
                  <button
                    class="py-1.5 rounded text-[11px] font-medium border transition-colors
 {barMode === bm.value
                        ? 'border-accent bg-accent-soft text-accent'
                        : 'border-edge-subtle text-fg-3 hover:border-edge '}"
                    onclick={() => barMode = bm.value as typeof barMode}
                  >
                    {bm.label}
                  </button>
                {/each}
              </div>
            </div>
          {/if}

          <!-- X-Axis column -->
          <div>
            <label for="x-axis-column" class="block text-xs font-medium text-fg-2 mb-1">X-Axis Column</label>
            <Combobox
              options={xAxisOptions}
              value={xColumn}
              onChange={(v) => xColumn = v}
              placeholder="Select column..."
            />
          </div>

          <!-- Y-Axis columns -->
          <div>
            <p class="block text-xs font-medium text-fg-2 mb-1">Y-Axis Columns</p>
            {#if queryMeta.length === 0}
              <p class="text-xs text-fg-4">Run a query first</p>
            {:else}
              <div class="flex flex-col gap-1 max-h-40 overflow-y-auto">
                {#each queryMeta.filter(m => m.name !== xColumn) as col}
                  <label class="flex items-center gap-2 text-xs text-fg-2 cursor-pointer hover:bg-hover rounded px-2 py-1">
                    <input
                      type="checkbox"
                      class="ds-checkbox ds-checkbox-sm"
                      checked={yColumns.includes(col.name)}
                      onchange={() => toggleYColumn(col.name)}
                    />
                    <span class="truncate">{col.name}</span>
                    <span class="text-fg-4 ml-auto shrink-0">{col.type}</span>
                  </label>
                {/each}
              </div>
            {/if}
          </div>

          <!-- Series colors -->
          {#if yColumns.length > 0}
            <div>
              <p class="block text-xs font-medium text-fg-2 mb-1">Series Colors</p>
              <div class="flex flex-col gap-1.5">
                {#each yColumns as yCol, i}
                  <div class="flex items-center gap-2">
                    <ColorPicker
                      value={colors[i] ?? DEFAULT_COLORS[i % DEFAULT_COLORS.length]}
                      onchange={(c) => updateColor(i, c)}
                    />
                    <span class="text-xs text-fg-2 truncate">{yCol}</span>
                  </div>
                {/each}
              </div>
            </div>
          {/if}

          <!-- Legend position -->
          <div>
            <label for="legend-position" class="block text-xs font-medium text-fg-2 mb-1">Legend</label>
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
