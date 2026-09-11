<script lang="ts">
  import { onMount, untrack } from 'svelte'
  import { RefreshCw, Plus, Gauge } from 'lucide-svelte'
  import Button from '../common/Button.svelte'
  import Select from '../common/Select.svelte'
  import EmptyState from '../common/EmptyState.svelte'
  import PageBody from '../common/PageBody.svelte'
  import TimeRangeSelector from '../dashboard/TimeRangeSelector.svelte'
  import MetricQueryCard, { defaultAggregation, type MetricCardSpec } from './MetricQueryCard.svelte'
  import { setSection } from '../../stores/nav.svelte'
  import { metricsCatalog } from '../../api/telemetry'
  import type { TelemetrySource, MetricCatalogEntry, MetricType, MetricAggregation } from '../../types/telemetry'
  import { encodeAbsoluteDashboardRange } from '../../utils/dashboard-time'
  import { resolveRange } from './time'

  interface Props {
    sources: TelemetrySource[]
  }
  let { sources }: Props = $props()

  const metricSources = $derived(sources.filter((s) => s.kind === 'metrics' && s.enabled))
  const MAX_CARDS = 4

  // ── State (mirrored in the URL) ──────────────────────────────
  let sourceId = $state('')
  let range = $state('1h')
  let cards = $state<MetricCardSpec[]>([])
  let tick = $state(0)

  function emptyCard(): MetricCardSpec {
    return { metric: '', type: '', aggregation: 'avg', group_by: [], q: '' }
  }

  function decodeCards(raw: string | null): MetricCardSpec[] {
    if (!raw) return []
    try {
      const parsed: unknown = JSON.parse(atob(raw))
      if (!Array.isArray(parsed)) return []
      return parsed.slice(0, MAX_CARDS).map((c) => {
        const o = (c ?? {}) as Partial<MetricCardSpec>
        return {
          metric: typeof o.metric === 'string' ? o.metric : '',
          type: (typeof o.type === 'string' ? o.type : '') as MetricType | '',
          aggregation: (typeof o.aggregation === 'string' ? o.aggregation : 'avg') as MetricAggregation,
          group_by: Array.isArray(o.group_by) ? o.group_by.filter((k): k is string => typeof k === 'string').slice(0, 2) : [],
          q: typeof o.q === 'string' ? o.q : '',
        }
      })
    } catch {
      return []
    }
  }

  function encodeCards(list: MetricCardSpec[]): string {
    return btoa(JSON.stringify(list.map(({ metric, type, aggregation, group_by, q }) => ({ metric, type, aggregation, group_by, q }))))
  }

  function readUrl() {
    const p = new URLSearchParams(window.location.search)
    sourceId = p.get('source') ?? ''
    const from = p.get('from')
    const to = p.get('to')
    if (from && to) range = encodeAbsoluteDashboardRange(from, to)
    else if (from) range = from
    cards = decodeCards(p.get('cards'))
  }

  function writeUrl() {
    const url = new URL(window.location.href)
    const set = (k: string, v: string) => (v ? url.searchParams.set(k, v) : url.searchParams.delete(k))
    set('section', 'metrics')
    set('source', sourceId)
    const abs = range.startsWith('abs:') ? resolveRange(range) : null
    set('from', abs ? abs.from : range)
    set('to', abs ? abs.to : '')
    set('cards', cards.some((c) => c.metric) ? encodeCards(cards) : '')
    history.replaceState(null, '', `${url.pathname}?${url.searchParams.toString()}`)
  }

  // ── Catalog ──────────────────────────────────────────────────
  let catalog = $state<MetricCatalogEntry[]>([])
  let catalogLoading = $state(false)
  let error = $state<string | null>(null)
  let seq = 0

  const bounds = $derived(resolveRange(range))

  async function loadCatalog() {
    if (!sourceId) return
    const mySeq = ++seq
    catalogLoading = true
    error = null
    try {
      const r = resolveRange(range)
      const list = await metricsCatalog(sourceId, r.from, r.to)
      if (mySeq !== seq) return
      catalog = list
      if (cards.length === 0 && list.length > 0) {
        const first = list[0]
        cards = [{ metric: first.name, type: first.type, aggregation: defaultAggregation(first.type), group_by: [], q: '' }]
      } else if (cards.length === 0) {
        cards = [emptyCard()]
      }
      writeUrl()
    } catch (e: unknown) {
      if (mySeq !== seq) return
      error = e instanceof Error ? e.message : String(e)
      catalog = []
    } finally {
      if (mySeq === seq) catalogLoading = false
    }
  }

  function refresh() {
    tick += 1
    void loadCatalog()
  }

  function setRange(v: string) {
    range = v
    writeUrl()
    tick += 1
  }

  function updateCard(i: number, spec: MetricCardSpec) {
    cards = cards.map((c, idx) => (idx === i ? spec : c))
    writeUrl()
  }

  function addCard(spec?: MetricCardSpec) {
    if (cards.length >= MAX_CARDS) return
    cards = [...cards, spec ? { ...spec, group_by: [...spec.group_by] } : emptyCard()]
    writeUrl()
  }

  function removeCard(i: number) {
    if (cards.length <= 1) return
    cards = cards.filter((_, idx) => idx !== i)
    writeUrl()
  }

  onMount(() => {
    readUrl()
    untrack(() => {
      if (!sourceId || !metricSources.some((s) => s.id === sourceId)) sourceId = metricSources[0]?.id ?? ''
      void loadCatalog()
    })
  })

  $effect(() => {
    if (metricSources.length > 0 && !metricSources.some((s) => s.id === sourceId)) {
      sourceId = metricSources[0].id
      void loadCatalog()
    }
  })
</script>

{#if metricSources.length === 0}
  <EmptyState
    icon={Gauge}
    title="No metrics source yet"
    description="Point CH-UI at the otel_metrics_* tables your OpenTelemetry collector writes. Detection finds them automatically."
    primary={{ label: 'Configure sources', onclick: () => setSection('sources') }}
  />
{:else}
  <div class="flex h-10 shrink-0 items-center gap-2 border-b border-edge-subtle px-5">
    {#if metricSources.length > 1}
      <Select size="sm" class="w-44" options={metricSources.map((s) => ({ value: s.id, label: s.name }))} value={sourceId} onchange={(v) => { sourceId = v; void loadCatalog(); tick += 1 }} />
    {/if}
    <span class="text-xs text-fg-4">
      {#if catalogLoading}Loading catalog…{:else}{catalog.length} metric{catalog.length === 1 ? '' : 's'} in range{/if}
    </span>
    <div class="ml-auto flex items-center gap-2">
      <TimeRangeSelector value={range} onchange={setRange} />
      <Button size="sm" variant="outline" disabled={cards.length >= MAX_CARDS} title={cards.length >= MAX_CARDS ? `At most ${MAX_CARDS} queries` : 'Add a query'} onclick={() => addCard()}>
        <Plus size={13} /> Add query
      </Button>
      <Button icon variant="ghost" size="sm" aria-label="Refresh" title="Refresh" onclick={refresh}>
        <RefreshCw size={14} class={catalogLoading ? 'animate-spin' : ''} />
      </Button>
    </div>
  </div>

  {#if error}
    <div class="shrink-0 border-b border-edge-subtle bg-danger-soft px-5 py-1.5 text-xs text-danger">{error}</div>
  {/if}

  <PageBody width="lg">
    {#if !catalogLoading && catalog.length === 0 && !error}
      <EmptyState
        size="compact"
        icon={Gauge}
        title="No metrics in this range"
        description="Nothing was written to the metrics tables for the selected time range. Widen the range or check the collector."
      />
    {:else}
      <div class="space-y-4">
        {#each cards as card, i (i)}
          <MetricQueryCard
            spec={card}
            {catalog}
            {catalogLoading}
            {sourceId}
            from={bounds.from}
            to={bounds.to}
            {tick}
            canRemove={cards.length > 1}
            onchange={(spec) => updateCard(i, spec)}
            onduplicate={() => addCard(card)}
            onremove={() => removeCard(i)}
          />
        {/each}
      </div>
    {/if}
  </PageBody>
{/if}
