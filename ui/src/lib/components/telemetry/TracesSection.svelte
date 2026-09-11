<script lang="ts">
  import { onMount, untrack } from 'svelte'
  import { RefreshCw, PanelLeftClose, PanelLeftOpen, Waypoints } from 'lucide-svelte'
  import Button from '../common/Button.svelte'
  import Select from '../common/Select.svelte'
  import Input from '../common/Input.svelte'
  import Spinner from '../common/Spinner.svelte'
  import Badge from '../common/Badge.svelte'
  import EmptyState from '../common/EmptyState.svelte'
  import DataTable, { type DataColumn } from '../common/DataTable.svelte'
  import TimeRangeSelector from '../dashboard/TimeRangeSelector.svelte'
  import SearchHelp from './SearchHelp.svelte'
  import TracesHistogram from './TracesHistogram.svelte'
  import TraceFacets from './TraceFacets.svelte'
  import TraceView from './TraceView.svelte'
  import { setSection } from '../../stores/nav.svelte'
  import { searchTraces, tracesHistogram, tracesFacets } from '../../api/telemetry'
  import type { TelemetrySource, TraceSummary, TraceFacets as TraceFacetsT, TraceHistogramBucket } from '../../types/telemetry'
  import { encodeAbsoluteDashboardRange } from '../../utils/dashboard-time'
  import { resolveRange } from './time'
  import { formatNumber, formatRelativeTime } from '../../utils/format'
  import { formatDuration } from './services'
  import { formatFull } from './time'

  interface Props {
    sources: TelemetrySource[]
  }
  let { sources }: Props = $props()

  const traceSources = $derived(sources.filter((s) => s.kind === 'traces' && s.enabled))

  // ── Search state (mirrored in the URL) ───────────────────────
  let sourceId = $state('')
  let query = $state('')
  let draft = $state('')
  let range = $state('1h')
  let traceId = $state('')

  function readUrl() {
    const p = new URLSearchParams(window.location.search)
    sourceId = p.get('source') ?? ''
    query = p.get('q') ?? ''
    draft = query
    const from = p.get('from')
    const to = p.get('to')
    if (from && to) range = encodeAbsoluteDashboardRange(from, to)
    else if (from) range = from
    traceId = p.get('trace') ?? ''
  }

  function writeUrl() {
    const url = new URL(window.location.href)
    const set = (k: string, v: string) => (v ? url.searchParams.set(k, v) : url.searchParams.delete(k))
    set('section', 'traces')
    set('source', sourceId)
    set('q', query)
    const abs = range.startsWith('abs:') ? resolveRange(range) : null
    set('from', abs ? abs.from : range)
    set('to', abs ? abs.to : '')
    set('trace', traceId)
    url.searchParams.delete('sev')
    url.searchParams.delete('svc')
    history.replaceState(null, '', `${url.pathname}?${url.searchParams.toString()}`)
  }

  // ── Data ─────────────────────────────────────────────────────
  let rows = $state<TraceSummary[]>([])
  let nextCursor = $state<string | null>(null)
  let loading = $state(false)
  let loadingMore = $state(false)
  let error = $state<string | null>(null)
  let tookMs = $state(0)
  let buckets = $state<TraceHistogramBucket[]>([])
  let bucketSeconds = $state(60)
  let facets = $state<TraceFacetsT | null>(null)
  let facetsLoading = $state(false)
  let facetsOpen = $state(true)
  let seq = 0

  function baseRequest() {
    const r = resolveRange(range)
    return { source_id: sourceId, from: r.from, to: r.to, q: query }
  }

  async function run() {
    if (!sourceId) return
    const mySeq = ++seq
    loading = true
    error = null
    const req = baseRequest()
    try {
      const [search, hist] = await Promise.all([
        searchTraces({ ...req, limit: 100 }),
        tracesHistogram(req).catch(() => null),
      ])
      if (mySeq !== seq) return
      rows = search.traces
      nextCursor = search.next_cursor
      tookMs = search.took_ms
      if (hist) {
        buckets = hist.buckets
        bucketSeconds = hist.bucket_seconds
      }
      writeUrl()
    } catch (e: unknown) {
      if (mySeq !== seq) return
      error = e instanceof Error ? e.message : String(e)
      rows = []
      nextCursor = null
    } finally {
      if (mySeq === seq) loading = false
    }
    void loadFacets()
  }

  async function loadFacets() {
    if (!sourceId) return
    facetsLoading = true
    try {
      facets = await tracesFacets(baseRequest())
    } catch {
      /* best effort */
    } finally {
      facetsLoading = false
    }
  }

  async function loadMore() {
    if (!nextCursor || loadingMore) return
    loadingMore = true
    try {
      const res = await searchTraces({ ...baseRequest(), limit: 100, cursor: nextCursor })
      rows = [...rows, ...res.traces]
      nextCursor = res.next_cursor
    } catch (e: unknown) {
      error = e instanceof Error ? e.message : String(e)
    } finally {
      loadingMore = false
    }
  }

  function submit() {
    query = draft.trim()
    void run()
  }

  function setRange(v: string) {
    range = v
    void run()
  }

  function quoteValue(v: string): string {
    return /[\s"():]/.test(v) ? `"${v.replace(/"/g, '\\"')}"` : v
  }

  function addFilter(key: string, value: string) {
    const term = `${key}:${quoteValue(value)}`
    draft = draft.trim() ? `${draft.trim()} ${term}` : term
    closeTrace()
    submit()
  }

  function openTrace(id: string) {
    traceId = id
    writeUrl()
  }

  function closeTrace() {
    traceId = ''
    writeUrl()
  }

  // ── Table ────────────────────────────────────────────────────
  // DataTable rows are plain records; TraceSummary is an interface, which has
  // no index signature, so cast at the boundary and back inside the cells.
  type TraceRow = Record<string, unknown>

  const columns: DataColumn<TraceRow>[] = [
    { key: 'root_span', label: 'Root span', width: '34%', truncate: true },
    { key: 'root_service', label: 'Service', width: '14%' },
    { key: 'duration_ms', label: 'Duration', align: 'right', width: '110px', format: (v) => formatDuration(Number(v)) },
    { key: 'span_count', label: 'Spans', align: 'right', width: '80px' },
    { key: 'error_count', label: 'Errors', align: 'right', width: '80px' },
    { key: 'start', label: 'Start', width: '140px', format: (v) => formatRelativeTime(v), sortValue: (r) => String(r.start) },
  ]

  let scroller = $state<HTMLDivElement | null>(null)
  function onScroll() {
    if (!scroller || !nextCursor || loadingMore) return
    if (scroller.scrollTop + scroller.clientHeight > scroller.scrollHeight - 200) void loadMore()
  }

  onMount(() => {
    readUrl()
    untrack(() => {
      if (!sourceId || !traceSources.some((s) => s.id === sourceId)) sourceId = traceSources[0]?.id ?? ''
      void run()
    })
  })

  $effect(() => {
    if (traceSources.length > 0 && !traceSources.some((s) => s.id === sourceId)) {
      sourceId = traceSources[0].id
      void run()
    }
  })
</script>

{#if traceSources.length === 0}
  <EmptyState
    icon={Waypoints}
    title="No traces source yet"
    description="Point CH-UI at the ClickHouse table your OpenTelemetry collector writes spans into. Detection finds the exporter's otel_traces table automatically."
    primary={{ label: 'Configure sources', onclick: () => setSection('sources') }}
  />
{:else if traceId}
  <TraceView {sourceId} {traceId} onback={closeTrace} onfilter={addFilter} />
{:else}
  <div class="flex h-10 shrink-0 items-center gap-2 border-b border-edge-subtle px-5">
    {#if traceSources.length > 1}
      <Select size="sm" class="w-44" options={traceSources.map((s) => ({ value: s.id, label: s.name }))} value={sourceId} onchange={(v) => { sourceId = v; void run() }} />
    {/if}
    <form class="flex min-w-0 flex-1 items-center gap-2" onsubmit={(e) => { e.preventDefault(); submit() }}>
      <Input size="sm" mono class="flex-1" placeholder="service:api duration:>200 status:error" bind:value={draft} spellcheck={false} autocomplete="off" />
      <SearchHelp />
    </form>
    <TimeRangeSelector value={range} onchange={setRange} />
    <Button icon variant="ghost" size="sm" aria-label="Refresh" title="Refresh" onclick={() => run()}>
      <RefreshCw size={14} class={loading ? 'animate-spin' : ''} />
    </Button>
  </div>

  {#if error}
    <div class="shrink-0 border-b border-edge-subtle bg-danger-soft px-5 py-1.5 text-xs text-danger">{error}</div>
  {/if}

  <div class="flex min-h-0 flex-1">
    {#if facetsOpen}
      <TraceFacets {facets} loading={facetsLoading} onaddfilter={addFilter} />
    {/if}

    <div class="flex min-w-0 flex-1 flex-col">
      <div class="flex h-8 shrink-0 items-center gap-2 px-3 text-xs text-fg-4">
        <Button icon variant="ghost" size="xs" aria-label={facetsOpen ? 'Hide facets' : 'Show facets'} title={facetsOpen ? 'Hide facets' : 'Show facets'} onclick={() => (facetsOpen = !facetsOpen)}>
          {#if facetsOpen}<PanelLeftClose size={13} />{:else}<PanelLeftOpen size={13} />{/if}
        </Button>
        {#if loading}
          <Spinner size="sm" class="h-3 w-3" /> Searching…
        {:else}
          <span>{formatNumber(rows.length)}{nextCursor ? '+' : ''} traces · {tookMs} ms</span>
          <span class="ml-auto">Drag on the chart to zoom · click a row to open the trace</span>
        {/if}
      </div>
      <div class="shrink-0 px-3 pb-1">
        <TracesHistogram {buckets} {bucketSeconds} height={110} onrange={(f, t) => setRange(encodeAbsoluteDashboardRange(f, t))} />
      </div>
      {#if !loading && rows.length === 0 && !error}
        <EmptyState size="compact" title="Nothing in this range" description="Widen the time range or loosen the query." />
      {:else}
        <div bind:this={scroller} class="min-h-0 flex-1 overflow-auto px-3 pb-3" onscroll={onScroll}>
          <div class="overflow-hidden rounded-lg border border-edge-subtle bg-surface">
            <DataTable {columns} rows={rows as unknown as TraceRow[]} rowKey={(r) => String(r.trace_id)} framed={false} emptyTitle="No traces" onrowclick={(r) => openTrace(String(r.trace_id))}>
              {#snippet cell(raw, col, value)}
                {@const row = raw as unknown as TraceSummary}
                {#if col.key === 'root_span'}
                  <div class="flex min-w-0 items-center gap-2">
                    <span class="truncate font-mono font-medium text-fg" title={row.root_span}>{row.root_span || '—'}</span>
                    {#if row.services.length > 1}
                      <span class="shrink-0 text-[11px] text-fg-4">+{row.services.length - 1} svc</span>
                    {/if}
                  </div>
                {:else if col.key === 'duration_ms'}
                  {#if row.status.toLowerCase() === 'error'}
                    <Badge tone="danger">{value}</Badge>
                  {:else}
                    <span class="font-mono">{value}</span>
                  {/if}
                {:else if col.key === 'error_count'}
                  {#if row.error_count > 0}<Badge tone="danger">{row.error_count}</Badge>{:else}<span class="text-fg-4">0</span>{/if}
                {:else if col.key === 'start'}
                  <span title={formatFull(row.start)}>{value}</span>
                {:else}
                  {value}
                {/if}
              {/snippet}
            </DataTable>
            {#if loadingMore}
              <div class="flex items-center justify-center gap-2 py-3 text-xs text-fg-3"><Spinner size="sm" /> Loading more…</div>
            {:else if !nextCursor && rows.length > 0}
              <div class="py-3 text-center text-[11px] text-fg-4">End of results</div>
            {/if}
          </div>
        </div>
      {/if}
    </div>
  </div>
{/if}
