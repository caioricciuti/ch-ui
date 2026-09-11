<script lang="ts" module>
  import type { MetricAggregation, MetricType } from '../../types/telemetry'

  /** One query on the explorer: what to plot and how. Mirrored in the URL by the section. */
  export interface MetricCardSpec {
    metric: string
    type: MetricType | ''
    aggregation: MetricAggregation
    group_by: string[]
    q: string
  }

  export const AGGREGATIONS: Record<MetricType, MetricAggregation[]> = {
    gauge: ['avg', 'sum', 'min', 'max', 'last', 'count'],
    sum: ['rate', 'sum', 'avg', 'max', 'last', 'count'],
    histogram: ['p50', 'p95', 'p99', 'avg', 'count'],
    exponential_histogram: ['count', 'avg'],
    summary: ['count', 'sum', 'avg'],
  }

  export const AGG_LABEL: Record<MetricAggregation, string> = {
    avg: 'Average',
    sum: 'Sum',
    min: 'Min',
    max: 'Max',
    last: 'Last',
    count: 'Count',
    rate: 'Rate',
    p50: 'p50',
    p95: 'p95',
    p99: 'p99',
  }

  /** The aggregation that makes sense for a metric type the first time it is picked. */
  export function defaultAggregation(type: MetricType | ''): MetricAggregation {
    switch (type) {
      case 'sum':
        return 'rate'
      case 'histogram':
        return 'p95'
      case 'exponential_histogram':
        return 'count'
      default:
        return 'avg'
    }
  }
</script>

<script lang="ts">
  import Badge from '../common/Badge.svelte'
  import Button from '../common/Button.svelte'
  import Combobox from '../common/Combobox.svelte'
  import Input from '../common/Input.svelte'
  import Select from '../common/Select.svelte'
  import Spinner from '../common/Spinner.svelte'
  import TrendChart from '../common/TrendChart.svelte'
  import MetricPicker from './MetricPicker.svelte'
  import { ChevronDown, ChevronRight, Copy, Trash2, X } from 'lucide-svelte'
  import { metricsQuery, metricsAttributes } from '../../api/telemetry'
  import type {
    MetricAttributeKey, MetricCatalogEntry, MetricQueryResponse,
  } from '../../types/telemetry'
  import { DEFAULT_COLORS } from '../../utils/chart-transform'
  import { formatNumber } from '../../utils/format'

  /** Shape TrendChart expects; declared here so the card owns no chart types. */
  type TrendSeries = { label: string; values: (number | null)[]; color: string }

  interface Props {
    spec: MetricCardSpec
    catalog: MetricCatalogEntry[]
    catalogLoading: boolean
    sourceId: string
    from: string
    to: string
    /** Bumped by the section to force a refresh. */
    tick: number
    canRemove: boolean
    onchange: (spec: MetricCardSpec) => void
    onduplicate: () => void
    onremove: () => void
  }

  let {
    spec, catalog, catalogLoading, sourceId, from, to, tick, canRemove,
    onchange, onduplicate, onremove,
  }: Props = $props()

  const aggregationOptions = $derived(
    (spec.type ? AGGREGATIONS[spec.type] : []).map((a) => ({ value: a, label: AGG_LABEL[a] })),
  )

  // ── Attribute keys for group by ──────────────────────────────
  let attributeKeys = $state<MetricAttributeKey[]>([])
  let attributesFor = $state('')

  async function loadAttributes() {
    if (!spec.metric || !spec.type || !sourceId) {
      attributeKeys = []
      return
    }
    const key = `${sourceId}|${spec.type}|${spec.metric}`
    if (attributesFor === key) return
    attributesFor = key
    try {
      const res = await metricsAttributes(sourceId, spec.metric, spec.type, from, to)
      attributeKeys = res.keys ?? []
    } catch {
      attributeKeys = []
    }
  }

  const groupByOptions = $derived(
    attributeKeys
      .filter((k) => !spec.group_by.includes(k.key))
      .map((k) => ({ value: k.key, label: k.key, hint: k.values.slice(0, 3).map((v) => v.value).join(', ') })),
  )

  // ── Query ────────────────────────────────────────────────────
  let result = $state<MetricQueryResponse | null>(null)
  let loading = $state(false)
  let error = $state<string | null>(null)
  let tableOpen = $state(false)
  let seq = 0

  async function run() {
    if (!spec.metric || !spec.type || !sourceId) {
      result = null
      return
    }
    const mySeq = ++seq
    loading = true
    error = null
    try {
      const res = await metricsQuery({
        source_id: sourceId,
        from,
        to,
        metric: spec.metric,
        type: spec.type,
        aggregation: spec.aggregation,
        group_by: spec.group_by,
        q: spec.q,
        limit_series: 20,
      })
      if (mySeq !== seq) return
      result = res
    } catch (e: unknown) {
      if (mySeq !== seq) return
      error = e instanceof Error ? e.message : String(e)
      result = null
    } finally {
      if (mySeq === seq) loading = false
    }
  }

  $effect(() => {
    // Re-run when the spec, range, source or the parent's tick changes.
    void spec.metric; void spec.type; void spec.aggregation; void spec.group_by.join(','); void spec.q
    void from; void to; void sourceId; void tick
    void run()
    void loadAttributes()
  })

  // ── Chart data ───────────────────────────────────────────────
  const chart = $derived.by(() => {
    if (!result || result.series.length === 0) return { x: [] as number[], series: [] as TrendSeries[] }
    const xs = new Set<number>()
    for (const s of result.series) for (const [t] of s.points) xs.add(t)
    const x = [...xs].sort((a, b) => a - b)
    const index = new Map(x.map((t, i) => [t, i]))
    const series: TrendSeries[] = result.series.map((s, i) => {
      const values: (number | null)[] = new Array(x.length).fill(null)
      for (const [t, v] of s.points) {
        const idx = index.get(t)
        if (idx !== undefined) values[idx] = v
      }
      return { label: s.name || spec.metric, values, color: DEFAULT_COLORS[i % DEFAULT_COLORS.length] }
    })
    return { x, series }
  })

  const unit = $derived(result?.unit ?? catalog.find((m) => m.name === spec.metric && m.type === spec.type)?.unit ?? '')

  function formatValue(v: number | null): string {
    if (v === null || v === undefined || Number.isNaN(v)) return '—'
    const abs = Math.abs(v)
    let text: string
    if (abs >= 1000) text = formatNumber(Math.round(v))
    else if (abs >= 10) text = v.toFixed(1)
    else if (abs >= 1) text = v.toFixed(2)
    else if (abs === 0) text = '0'
    else text = v.toPrecision(3)
    if (spec.aggregation === 'rate') return `${text}/s`
    if (spec.aggregation === 'count') return text
    return unit ? `${text} ${unit}` : text
  }

  function update(patch: Partial<MetricCardSpec>) {
    onchange({ ...spec, ...patch })
  }

  function pickMetric(m: MetricCatalogEntry) {
    const sameType = m.type === spec.type
    update({
      metric: m.name,
      type: m.type,
      aggregation: sameType && AGGREGATIONS[m.type].includes(spec.aggregation) ? spec.aggregation : defaultAggregation(m.type),
      group_by: [],
    })
  }

  function addGroupBy(key: string) {
    if (!key || spec.group_by.includes(key) || spec.group_by.length >= 2) return
    update({ group_by: [...spec.group_by, key] })
  }

  function removeGroupBy(key: string) {
    update({ group_by: spec.group_by.filter((k) => k !== key) })
  }

  let filterDraft = $state('')
  $effect(() => {
    filterDraft = spec.q
  })

  function submitFilter() {
    if (filterDraft.trim() !== spec.q) update({ q: filterDraft.trim() })
  }

  const title = $derived(spec.metric ? `${AGG_LABEL[spec.aggregation]} of ${spec.metric}` : 'New query')
</script>

<section class="rounded-lg border border-edge-subtle bg-surface">
  <div class="flex flex-wrap items-center gap-2 border-b border-edge-subtle px-3 py-2">
    <div class="w-72 min-w-[16rem]">
      <MetricPicker {catalog} value={spec.metric} type={spec.type} loading={catalogLoading} onchange={pickMetric} />
    </div>
    <Select
      size="sm"
      class="w-32"
      options={aggregationOptions}
      value={spec.aggregation}
      disabled={!spec.type}
      onchange={(v) => update({ aggregation: v as MetricAggregation })}
    />
    <div class="flex items-center gap-1.5">
      <span class="text-xs text-fg-3">by</span>
      {#each spec.group_by as key (key)}
        <Badge tone="brand" class="pr-0.5">
          <span class="font-mono">{key}</span>
          <button type="button" class="rounded-sm p-0.5 hover:bg-accent/20" aria-label={`Remove group by ${key}`} onclick={() => removeGroupBy(key)}>
            <X size={10} />
          </button>
        </Badge>
      {/each}
      {#if spec.group_by.length < 2}
        <div class="w-44">
          <Combobox
            options={groupByOptions}
            value=""
            placeholder={spec.group_by.length ? 'Add attribute' : 'Attribute'}
            emptyText="No attributes"
            disabled={!spec.metric}
            onChange={addGroupBy}
          />
        </div>
      {/if}
    </div>
    <form class="min-w-[14rem] flex-1" onsubmit={(e) => { e.preventDefault(); submitFilter() }}>
      <Input size="sm" mono placeholder="service:api http.method:GET" bind:value={filterDraft} onchange={submitFilter} spellcheck={false} autocomplete="off" />
    </form>
    <div class="ml-auto flex items-center gap-0.5">
      {#if loading}<Spinner size="sm" class="mr-1 h-3 w-3" />{/if}
      <Button icon variant="ghost" size="sm" aria-label="Duplicate query" title="Duplicate" onclick={onduplicate}>
        <Copy size={14} />
      </Button>
      <Button icon variant="ghost" size="sm" aria-label="Remove query" title="Remove" disabled={!canRemove} onclick={onremove}>
        <Trash2 size={14} />
      </Button>
    </div>
  </div>

  <div class="px-3 pb-3 pt-2">
    <div class="mb-1 flex items-center gap-2 text-xs text-fg-3">
      <span class="truncate font-medium text-fg-2">{title}</span>
      {#if result}
        <span class="text-fg-4">{result.series.length} series · {result.bucket_seconds}s buckets · {result.took_ms} ms</span>
        {#if result.truncated}
          <Badge tone="warning">truncated to {result.series.length}</Badge>
        {/if}
      {/if}
    </div>

    {#if error}
      <div class="rounded-md bg-danger-soft px-3 py-2 text-xs text-danger">{error}</div>
    {:else if !spec.metric}
      <div class="grid h-[180px] place-items-center text-xs text-fg-4">Pick a metric to plot.</div>
    {:else if chart.x.length > 1 && chart.series.length > 0}
      <TrendChart x={chart.x} series={chart.series} height={180} formatY={(v) => formatValue(v)} />
    {:else if !loading}
      <div class="grid h-[180px] place-items-center text-xs text-fg-4">No data points in this range.</div>
    {:else}
      <div class="grid h-[180px] place-items-center"><Spinner size="sm" /></div>
    {/if}

    {#if result && result.series.length > 0}
      <button
        type="button"
        class="mt-2 inline-flex items-center gap-1 text-xs text-fg-3 hover:text-fg"
        aria-expanded={tableOpen}
        onclick={() => (tableOpen = !tableOpen)}
      >
        {#if tableOpen}<ChevronDown size={12} />{:else}<ChevronRight size={12} />{/if}
        Series
      </button>
      {#if tableOpen}
        <div class="mt-2 overflow-hidden rounded-md border border-edge-subtle">
          <table class="w-full text-xs">
            <thead>
              <tr class="border-b border-edge-subtle text-fg-3">
                <th class="h-7 px-2 text-left font-medium">Series</th>
                <th class="h-7 px-2 text-right font-medium">Last</th>
                <th class="h-7 px-2 text-right font-medium">Min</th>
                <th class="h-7 px-2 text-right font-medium">Max</th>
                <th class="h-7 px-2 text-right font-medium">Avg</th>
              </tr>
            </thead>
            <tbody>
              {#each result.series as s, i (s.name + i)}
                <tr class="border-b border-edge-subtle last:border-b-0">
                  <td class="px-2 py-1">
                    <span class="inline-flex items-center gap-1.5">
                      <span class="h-1.5 w-1.5 shrink-0 rounded-[2px]" style="background: {DEFAULT_COLORS[i % DEFAULT_COLORS.length]}"></span>
                      <span class="font-mono text-fg-2">{s.name || spec.metric}</span>
                      {#each Object.entries(s.labels) as [k, v] (k)}
                        <span class="text-fg-4">{k}=<span class="text-fg-3">{v}</span></span>
                      {/each}
                    </span>
                  </td>
                  <td class="px-2 py-1 text-right tabular-nums text-fg">{formatValue(s.last)}</td>
                  <td class="px-2 py-1 text-right tabular-nums text-fg-2">{formatValue(s.min)}</td>
                  <td class="px-2 py-1 text-right tabular-nums text-fg-2">{formatValue(s.max)}</td>
                  <td class="px-2 py-1 text-right tabular-nums text-fg-2">{formatValue(s.avg)}</td>
                </tr>
              {/each}
            </tbody>
          </table>
        </div>
      {/if}
    {/if}
  </div>
</section>
