<script lang="ts">
  import type { DashboardVariable, PanelConfig, StatThreshold } from '../../types/api'
  import { DEFAULT_COLORS, DEFAULT_HISTOGRAM_BUCKETS, MAX_HISTOGRAM_BUCKETS, isDateType, isNumericType, type ColumnMeta } from '../../utils/chart-transform'
  import Combobox from '../common/Combobox.svelte'
  import type { ComboboxOption } from '../common/Combobox.svelte'
  import ColorPicker from '../common/ColorPicker.svelte'
  import PanelLinksFields from './PanelLinksFields.svelte'
  import { Table2, Hash, TrendingUp, BarChart3, Plus, X, FileText, Gauge, PieChart, ChartColumnBig, ChartScatter, Grid3x3 } from 'lucide-svelte'

  interface Props {
    config: PanelConfig
    meta: ColumnMeta[]
    /** Type, X, Y and Series only: the SQL editor's chart view. */
    compact?: boolean
    /** Dashboard time range label, shown as the time scope in full mode. */
    rangeLabel?: string
    /** Variables of the panel's dashboard. Set in the dashboard panel editor, which then offers drilldown links. */
    linkVariables?: DashboardVariable[]
  }

  let { config = $bindable(), meta, compact = false, rangeLabel = '', linkVariables }: Props = $props()

  type ChartType = PanelConfig['chartType']
  type StatCalc = NonNullable<PanelConfig['statCalculation']>
  type StatUnit = NonNullable<PanelConfig['statUnit']>
  type StatColorMode = NonNullable<PanelConfig['statColorMode']>
  type BarMode = NonNullable<PanelConfig['barMode']>
  type LegendPosition = NonNullable<PanelConfig['legendPosition']>
  type Fill = NonNullable<PanelConfig['fill']>

  const allVizTypes: { type: ChartType; label: string; icon: typeof Table2 }[] = [
    { type: 'table', label: 'Table', icon: Table2 },
    { type: 'stat', label: 'Stat', icon: Hash },
    { type: 'gauge', label: 'Gauge', icon: Gauge },
    { type: 'timeseries', label: 'Time Series', icon: TrendingUp },
    { type: 'bar', label: 'Bar', icon: BarChart3 },
    { type: 'pie', label: 'Pie', icon: PieChart },
    { type: 'histogram', label: 'Histogram', icon: ChartColumnBig },
    { type: 'scatter', label: 'Scatter', icon: ChartScatter },
    { type: 'heatmap', label: 'Heatmap', icon: Grid3x3 },
    { type: 'text', label: 'Text', icon: FileText },
  ]
  // Text panels have no query result to chart.
  const vizTypes = $derived(compact ? allVizTypes.filter(v => v.type !== 'text') : allVizTypes)

  const STAT_CALCS: StatCalc[] = ['last', 'first', 'mean', 'sum', 'min', 'max', 'count', 'range']
  const STAT_UNITS: ComboboxOption[] = [
    { value: 'none', label: 'None' },
    { value: 'short', label: 'Short (K/M/B)' },
    { value: 'percent', label: 'Percent (%)' },
    { value: 'bytes', label: 'Bytes (KB/MB/GB)' },
    { value: 'bps', label: 'Bytes/sec' },
    { value: 'duration', label: 'Duration (s)' },
    { value: 'durationMs', label: 'Duration (ms)' },
  ]
  const COLOR_MODES: { value: StatColorMode; label: string }[] = [
    { value: 'none', label: 'None' },
    { value: 'value', label: 'Value' },
    { value: 'background', label: 'Background' },
  ]
  const BAR_MODES: { value: BarMode; label: string }[] = [
    { value: 'grouped', label: 'Grouped' },
    { value: 'stacked', label: 'Stacked' },
  ]
  const FILLS: { value: Fill; label: string }[] = [
    { value: 'line', label: 'Line' },
    { value: 'area', label: 'Area' },
  ]
  const legendOptions: ComboboxOption[] = [
    { value: 'bottom', label: 'Bottom' },
    { value: 'right', label: 'Right' },
    { value: 'none', label: 'Hidden' },
  ]

  const uid = Math.random().toString(36).slice(2, 8)

  const columnOptions = $derived<ComboboxOption[]>(
    meta.map(col => ({ value: col.name, label: col.name, hint: col.type, keywords: `${col.name} ${col.type}` })),
  )
  const xAxisOptions = $derived<ComboboxOption[]>([{ value: '', label: 'Select column...' }, ...columnOptions])
  const numericOptions = $derived(columnOptions.filter(o => isNumericType(o.hint ?? '')))
  const textOptions = $derived(columnOptions.filter(o => !isNumericType(o.hint ?? '')))
  // Scatter x: numbers or dates, anything that has a position on an axis.
  const scatterXOptions = $derived(columnOptions.filter(o => isNumericType(o.hint ?? '') || isDateType(o.hint ?? '')))
  // Series splits rows by a column's values; numbers rarely make good series.
  const seriesOptions = $derived<ComboboxOption[]>([
    { value: '', label: 'None' },
    ...columnOptions.filter(o => o.value !== config.xColumn && !isNumericType(o.hint ?? '')),
  ])

  const xColumn = $derived(config.xColumn ?? '')
  const yColumns = $derived(config.yColumns ?? [])
  const colors = $derived(config.colors ?? DEFAULT_COLORS)
  const thresholds = $derived<StatThreshold[]>(config.statThresholds ?? [])
  const isAxisChart = $derived(config.chartType === 'timeseries' || config.chartType === 'bar')
  const legendTypes: PanelConfig['chartType'][] = ['timeseries', 'bar', 'scatter']
  // Panels whose data points can be clicked.
  const linkTypes: PanelConfig['chartType'][] = ['table', 'timeseries', 'bar', 'pie', 'histogram', 'scatter', 'heatmap']

  // Client-side keys for the threshold rows, parallel to the thresholds.
  // Kept out of the threshold objects so they never reach the saved config.
  let nextThresholdId = 0
  let thresholdIds = $state<number[]>([])
  $effect(() => {
    const n = thresholds.length
    if (thresholdIds.length !== n) thresholdIds = Array.from({ length: n }, () => nextThresholdId++)
  })
  const thresholdRows = $derived(thresholds.map((threshold, i) => ({ threshold, i, id: thresholdIds[i] ?? -1 - i })))

  function set(patch: Partial<PanelConfig>) {
    config = { ...config, ...patch }
  }

  function setChartType(type: ChartType) {
    set({ chartType: type })
  }

  function setXColumn(v: string) {
    set({
      xColumn: v,
      yColumns: yColumns.filter(c => c !== v),
      seriesColumn: config.seriesColumn === v ? undefined : config.seriesColumn,
    })
  }

  function toggleYColumn(name: string) {
    set({ yColumns: yColumns.includes(name) ? yColumns.filter(c => c !== name) : [...yColumns, name] })
  }

  function setSeries(v: string) {
    set({ seriesColumn: v || undefined, yColumns: yColumns.filter(c => c !== v) })
  }

  function updateColor(index: number, color: string) {
    const next = [...colors]
    next[index] = color
    set({ colors: next })
  }

  function setThresholds(next: StatThreshold[]) {
    set({ statThresholds: next })
  }

  function addThreshold() {
    const lastVal = thresholds.length > 0 ? thresholds[thresholds.length - 1].value + 10 : 0
    thresholdIds = [...thresholdIds, nextThresholdId++]
    setThresholds([...thresholds, { value: lastVal, color: '#ef4444' }])
  }

  function removeThreshold(i: number) {
    thresholdIds = thresholdIds.filter((_, j) => j !== i)
    setThresholds(thresholds.filter((_, j) => j !== i))
  }

  const choiceClass = (active: boolean) =>
    active ? 'border-accent bg-accent-soft text-accent' : 'border-edge-subtle text-fg-3 hover:border-edge hover:text-fg'
</script>

<!-- Visualization type -->
<div>
  <p class="block text-xs font-medium text-fg-2 mb-2">{compact ? 'Type' : 'Visualization'}</p>
  <div class="grid {compact ? 'grid-cols-3' : 'grid-cols-4'} gap-1.5">
    {#each vizTypes as vt (vt.type)}
      {@const Icon = vt.icon}
      <button
        type="button"
        class="flex flex-col items-center gap-1 py-2 px-1 rounded-md border text-xs transition-colors {choiceClass(config.chartType === vt.type)}"
        aria-pressed={config.chartType === vt.type}
        onclick={() => setChartType(vt.type)}
      >
        <Icon size={16} />
        {vt.label}
      </button>
    {/each}
  </div>
</div>

<!-- Stat/Gauge config -->
{#if config.chartType === 'stat' || config.chartType === 'gauge'}
  <div>
    <span class="block text-xs font-medium text-fg-2 mb-1">Field</span>
    <Combobox
      options={[{ value: '', label: 'Auto (first numeric)' }, ...columnOptions]}
      value={config.statField ?? ''}
      onChange={(v) => set({ statField: v || undefined })}
      placeholder="Auto"
    />
  </div>

  {#if !compact}
    <div>
      <span id="stat-calc-{uid}" class="block text-xs font-medium text-fg-2 mb-1">Calculation</span>
      <div class="grid grid-cols-4 gap-1" role="group" aria-labelledby="stat-calc-{uid}">
        {#each STAT_CALCS as calc (calc)}
          <button
            type="button"
            class="py-1.5 px-1 rounded text-[11px] font-medium border transition-colors {choiceClass((config.statCalculation ?? 'last') === calc)}"
            onclick={() => set({ statCalculation: calc })}
          >
            {calc.charAt(0).toUpperCase() + calc.slice(1)}
          </button>
        {/each}
      </div>
    </div>

    <div>
      <span class="block text-xs font-medium text-fg-2 mb-1">Unit</span>
      <Combobox
        options={STAT_UNITS}
        value={config.statUnit ?? 'none'}
        onChange={(v) => set({ statUnit: v as StatUnit })}
        placeholder="None"
      />
    </div>

    <div class="grid grid-cols-3 gap-2">
      <div>
        <label for="stat-prefix-{uid}" class="block text-xs font-medium text-fg-2 mb-1">Prefix</label>
        <input
          id="stat-prefix-{uid}"
          type="text"
          class="w-full text-sm bg-transparent border border-edge rounded px-2 py-1.5 text-fg"
          placeholder="$"
          value={config.statPrefix ?? ''}
          oninput={(e) => set({ statPrefix: e.currentTarget.value })}
        />
      </div>
      <div>
        <label for="stat-suffix-{uid}" class="block text-xs font-medium text-fg-2 mb-1">Suffix</label>
        <input
          id="stat-suffix-{uid}"
          type="text"
          class="w-full text-sm bg-transparent border border-edge rounded px-2 py-1.5 text-fg"
          placeholder="%"
          value={config.statSuffix ?? ''}
          oninput={(e) => set({ statSuffix: e.currentTarget.value })}
        />
      </div>
      <div>
        <label for="stat-decimals-{uid}" class="block text-xs font-medium text-fg-2 mb-1">Decimals</label>
        <input
          id="stat-decimals-{uid}"
          type="number"
          min="0"
          max="10"
          class="w-full text-sm bg-transparent border border-edge rounded px-2 py-1.5 text-fg"
          placeholder="Auto"
          value={config.statDecimals ?? ''}
          oninput={(e) => {
            const v = e.currentTarget.value
            set({ statDecimals: v === '' ? undefined : Number(v) })
          }}
        />
      </div>
    </div>

    <div>
      <span id="stat-colormode-{uid}" class="block text-xs font-medium text-fg-2 mb-1">Color Mode</span>
      <div class="grid grid-cols-3 gap-1" role="group" aria-labelledby="stat-colormode-{uid}">
        {#each COLOR_MODES as cm (cm.value)}
          <button
            type="button"
            class="py-1.5 rounded text-[11px] font-medium border transition-colors {choiceClass((config.statColorMode ?? 'none') === cm.value)}"
            onclick={() => set({ statColorMode: cm.value })}
          >
            {cm.label}
          </button>
        {/each}
      </div>
    </div>

    {#if (config.statColorMode ?? 'none') !== 'none'}
      <div>
        <div class="flex items-center justify-between mb-1">
          <span class="text-xs font-medium text-fg-2">Thresholds</span>
          <button type="button" class="flex items-center gap-0.5 text-[11px] text-accent hover:underline transition-colors" onclick={addThreshold}>
            <Plus size={12} /> Add
          </button>
        </div>
        <div class="flex flex-col gap-1.5">
          {#each thresholdRows as { threshold, i, id } (id)}
            <div class="flex items-center gap-2">
              <ColorPicker
                value={threshold.color}
                onchange={(c) => {
                  const next = [...thresholds]
                  next[i] = { ...next[i], color: c }
                  setThresholds(next)
                }}
              />
              {#if i === 0}
                <span class="flex-1 text-xs text-fg-4">Base</span>
              {:else}
                <input
                  type="number"
                  value={threshold.value}
                  oninput={(e) => {
                    const next = [...thresholds]
                    next[i] = { ...next[i], value: Number(e.currentTarget.value) }
                    setThresholds(next)
                  }}
                  class="flex-1 text-sm bg-transparent border border-edge rounded px-2 py-1 text-fg"
                />
              {/if}
              {#if thresholds.length > 1}
                <button
                  type="button"
                  class="p-0.5 rounded text-fg-4 hover:text-danger transition-colors"
                  onclick={() => removeThreshold(i)}
                  aria-label="Remove threshold"
                >
                  <X size={14} />
                </button>
              {/if}
            </div>
          {/each}
        </div>
      </div>
    {/if}

    {#if config.chartType === 'gauge'}
      <div class="grid grid-cols-2 gap-2">
        <div>
          <label for="gauge-min-{uid}" class="block text-xs font-medium text-fg-2 mb-1">Min</label>
          <input
            id="gauge-min-{uid}"
            type="number"
            class="w-full text-sm bg-transparent border border-edge rounded px-2 py-1.5 text-fg"
            value={config.gaugeMin ?? 0}
            oninput={(e) => set({ gaugeMin: Number(e.currentTarget.value) })}
          />
        </div>
        <div>
          <label for="gauge-max-{uid}" class="block text-xs font-medium text-fg-2 mb-1">Max</label>
          <input
            id="gauge-max-{uid}"
            type="number"
            class="w-full text-sm bg-transparent border border-edge rounded px-2 py-1.5 text-fg"
            value={config.gaugeMax ?? 100}
            oninput={(e) => set({ gaugeMax: Number(e.currentTarget.value) })}
          />
        </div>
      </div>
    {/if}
  {/if}
{/if}

<!-- Pie config -->
{#if config.chartType === 'pie'}
  <div>
    <span class="block text-xs font-medium text-fg-2 mb-1">Label Column</span>
    <Combobox
      options={[{ value: '', label: 'Auto (first string)' }, ...columnOptions]}
      value={config.pieLabelColumn ?? ''}
      onChange={(v) => set({ pieLabelColumn: v || undefined })}
      placeholder="Auto"
    />
  </div>
  <div>
    <span class="block text-xs font-medium text-fg-2 mb-1">Value Column</span>
    <Combobox
      options={[{ value: '', label: 'Auto (first numeric)' }, ...columnOptions]}
      value={config.pieValueColumn ?? ''}
      onChange={(v) => set({ pieValueColumn: v || undefined })}
      placeholder="Auto"
    />
  </div>
  {#if !compact}
    <label class="flex items-center gap-2 text-xs text-fg-2 cursor-pointer">
      <input
        type="checkbox"
        class="ds-checkbox ds-checkbox-sm"
        checked={config.pieDonut ?? false}
        onchange={(e) => set({ pieDonut: e.currentTarget.checked })}
      />
      Donut mode
    </label>
  {/if}
{/if}

<!-- Axis charts: time series and bar -->
{#if isAxisChart}
  {#if !compact}
    <div class="rounded-lg border border-edge-subtle bg-surface-2 px-2.5 py-2">
      <p class="text-[11px] uppercase tracking-wide text-fg-3">Time Scope</p>
      <p class="mt-0.5 text-xs text-fg-2">
        Uses dashboard picker: <span class="text-accent">{rangeLabel}</span>
      </p>
    </div>

    {#if config.chartType === 'bar'}
      <div>
        <span id="bar-mode-{uid}" class="block text-xs font-medium text-fg-2 mb-1">Bar Mode</span>
        <div class="grid grid-cols-2 gap-1" role="group" aria-labelledby="bar-mode-{uid}">
          {#each BAR_MODES as bm (bm.value)}
            <button
              type="button"
              class="py-1.5 rounded text-[11px] font-medium border transition-colors {choiceClass((config.barMode ?? 'grouped') === bm.value)}"
              onclick={() => set({ barMode: bm.value })}
            >
              {bm.label}
            </button>
          {/each}
        </div>
      </div>
    {/if}

    {#if config.chartType === 'timeseries'}
      <div>
        <span id="fill-{uid}" class="block text-xs font-medium text-fg-2 mb-1">Style</span>
        <div class="grid grid-cols-2 gap-1" role="group" aria-labelledby="fill-{uid}">
          {#each FILLS as f (f.value)}
            <button
              type="button"
              class="py-1.5 rounded text-[11px] font-medium border transition-colors {choiceClass((config.fill ?? 'line') === f.value)}"
              onclick={() => set({ fill: f.value })}
            >
              {f.label}
            </button>
          {/each}
        </div>
      </div>
      <label class="flex items-center gap-2 text-xs text-fg-2 cursor-pointer">
        <input
          type="checkbox"
          class="ds-checkbox ds-checkbox-sm"
          checked={config.stacked ?? false}
          onchange={(e) => set({ stacked: e.currentTarget.checked || undefined })}
        />
        Stack series
      </label>
    {/if}
  {/if}

  <div>
    <label for="x-axis-{uid}" class="block text-xs font-medium text-fg-2 mb-1">{compact ? 'X' : 'X-Axis Column'}</label>
    <Combobox id="x-axis-{uid}" options={xAxisOptions} value={xColumn} onChange={setXColumn} placeholder="Select column..." />
  </div>

  <div>
    <p class="block text-xs font-medium text-fg-2 mb-1">{compact ? 'Y' : 'Y-Axis Columns'}</p>
    {#if meta.length === 0}
      <p class="text-xs text-fg-4">Run a query first</p>
    {:else}
      <div class="flex flex-col gap-1 max-h-40 overflow-y-auto">
        {#each meta.filter(m => m.name !== xColumn && m.name !== config.seriesColumn) as col (col.name)}
          <label class="flex items-center gap-2 text-xs text-fg-2 cursor-pointer hover:bg-hover rounded px-2 py-1">
            <input
              type="checkbox"
              class="ds-checkbox ds-checkbox-sm"
              checked={yColumns.includes(col.name)}
              onchange={() => toggleYColumn(col.name)}
            />
            <span class="truncate">{col.name}</span>
            <span class="text-fg-4 ml-auto shrink-0 truncate max-w-[45%]">{col.type}</span>
          </label>
        {/each}
      </div>
    {/if}
  </div>

  <div>
    <label for="series-{uid}" class="block text-xs font-medium text-fg-2 mb-1">Series</label>
    <Combobox id="series-{uid}" options={seriesOptions} value={config.seriesColumn ?? ''} onChange={setSeries} placeholder="None" />
    {#if config.seriesColumn}
      <p class="mt-1 text-xs text-fg-3">One series per value of {config.seriesColumn}, from the first Y column. Up to 10 are drawn.</p>
    {/if}
  </div>

  {#if !compact}
    {#if yColumns.length > 0 && !config.seriesColumn}
      <div>
        <p class="block text-xs font-medium text-fg-2 mb-1">Series Colors</p>
        <div class="flex flex-col gap-1.5">
          {#each yColumns as yCol, i (yCol)}
            <div class="flex items-center gap-2">
              <ColorPicker value={colors[i] ?? DEFAULT_COLORS[i % DEFAULT_COLORS.length]} onchange={(c) => updateColor(i, c)} />
              <span class="text-xs text-fg-2 truncate">{yCol}</span>
            </div>
          {/each}
        </div>
      </div>
    {/if}

  {/if}
{/if}

<!-- Histogram config -->
{#if config.chartType === 'histogram'}
  <div>
    <label for="histogram-column-{uid}" class="block text-xs font-medium text-fg-2 mb-1">Column</label>
    <Combobox
      id="histogram-column-{uid}"
      options={[{ value: '', label: 'Auto (first numeric)' }, ...numericOptions]}
      value={config.histogramColumn ?? ''}
      onChange={(v) => set({ histogramColumn: v || undefined })}
      placeholder="Auto"
    />
  </div>
  <div>
    <label for="histogram-buckets-{uid}" class="block text-xs font-medium text-fg-2 mb-1">Buckets</label>
    <input
      id="histogram-buckets-{uid}"
      type="number"
      min="1"
      max={MAX_HISTOGRAM_BUCKETS}
      class="w-full text-sm bg-transparent border border-edge rounded px-2 py-1.5 text-fg"
      placeholder={String(DEFAULT_HISTOGRAM_BUCKETS)}
      value={config.histogramBuckets ?? ''}
      oninput={(e) => {
        const v = e.currentTarget.value
        set({ histogramBuckets: v === '' ? undefined : Number(v) })
      }}
    />
  </div>
{/if}

<!-- Scatter config -->
{#if config.chartType === 'scatter'}
  <div>
    <label for="scatter-x-{uid}" class="block text-xs font-medium text-fg-2 mb-1">{compact ? 'X' : 'X-Axis Column'}</label>
    <Combobox
      id="scatter-x-{uid}"
      options={[{ value: '', label: 'Auto (first numeric)' }, ...scatterXOptions]}
      value={config.scatterX ?? ''}
      onChange={(v) => set({ scatterX: v || undefined })}
      placeholder="Auto"
    />
  </div>
  <div>
    <label for="scatter-y-{uid}" class="block text-xs font-medium text-fg-2 mb-1">{compact ? 'Y' : 'Y-Axis Column'}</label>
    <Combobox
      id="scatter-y-{uid}"
      options={[{ value: '', label: 'Auto (next numeric)' }, ...numericOptions.filter(o => o.value !== config.scatterX)]}
      value={config.scatterY ?? ''}
      onChange={(v) => set({ scatterY: v || undefined })}
      placeholder="Auto"
    />
  </div>
  {#if !compact}
    <div>
      <label for="scatter-size-{uid}" class="block text-xs font-medium text-fg-2 mb-1">Point Size</label>
      <Combobox
        id="scatter-size-{uid}"
        options={[{ value: '', label: 'Fixed' }, ...numericOptions]}
        value={config.scatterSize ?? ''}
        onChange={(v) => set({ scatterSize: v || undefined })}
        placeholder="Fixed"
      />
    </div>
  {/if}
  <div>
    <label for="scatter-series-{uid}" class="block text-xs font-medium text-fg-2 mb-1">Series</label>
    <Combobox
      id="scatter-series-{uid}"
      options={[{ value: '', label: 'None' }, ...textOptions]}
      value={config.scatterSeries ?? ''}
      onChange={(v) => set({ scatterSeries: v || undefined })}
      placeholder="None"
    />
    {#if config.scatterSeries}
      <p class="mt-1 text-xs text-fg-3">One color per value of {config.scatterSeries}. Up to 10; the rest are grouped as Other.</p>
    {/if}
  </div>
{/if}

<!-- Heatmap config -->
{#if config.chartType === 'heatmap'}
  <div>
    <label for="heatmap-x-{uid}" class="block text-xs font-medium text-fg-2 mb-1">{compact ? 'X' : 'X-Axis Column'}</label>
    <Combobox
      id="heatmap-x-{uid}"
      options={[{ value: '', label: 'Auto (first date or text)' }, ...columnOptions]}
      value={config.heatmapX ?? ''}
      onChange={(v) => set({ heatmapX: v || undefined })}
      placeholder="Auto"
    />
  </div>
  <div>
    <label for="heatmap-y-{uid}" class="block text-xs font-medium text-fg-2 mb-1">{compact ? 'Y' : 'Y-Axis Column'}</label>
    <Combobox
      id="heatmap-y-{uid}"
      options={[{ value: '', label: 'Auto (next text)' }, ...columnOptions.filter(o => o.value !== config.heatmapX)]}
      value={config.heatmapY ?? ''}
      onChange={(v) => set({ heatmapY: v || undefined })}
      placeholder="Auto"
    />
  </div>
  <div>
    <label for="heatmap-value-{uid}" class="block text-xs font-medium text-fg-2 mb-1">Value</label>
    <Combobox
      id="heatmap-value-{uid}"
      options={[{ value: '', label: 'Auto (first numeric, else row count)' }, ...numericOptions]}
      value={config.heatmapValue ?? ''}
      onChange={(v) => set({ heatmapValue: v || undefined })}
      placeholder="Auto"
    />
    {#if !compact}
      <p class="mt-1 text-xs text-fg-3">Values are summed per cell. Up to 200 columns and rows; beyond that, neighbouring values share a cell.</p>
    {/if}
  </div>
{/if}

{#if !compact && legendTypes.includes(config.chartType)}
  <div>
    <label for="legend-{uid}" class="block text-xs font-medium text-fg-2 mb-1">Legend</label>
    <Combobox
      id="legend-{uid}"
      options={legendOptions}
      value={config.legendPosition ?? 'bottom'}
      onChange={(v) => set({ legendPosition: v as LegendPosition })}
      placeholder="Legend position"
    />
  </div>
{/if}

{#if !compact && linkVariables && linkTypes.includes(config.chartType)}
  <PanelLinksFields links={config.links ?? []} variables={linkVariables} onchange={(links) => set({ links })} />
{/if}
