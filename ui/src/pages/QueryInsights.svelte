<script lang="ts">
  import { onMount } from 'svelte'
  import type { InsightsRange, InsightsSection, InsightsResult, InsightsFilters } from '../lib/api/queryInsights'
  import { fetchInsights } from '../lib/api/queryInsights'
  import { openQueryTab } from '../lib/stores/tabs.svelte'
  import { getSection } from '../lib/stores/nav.svelte'
  import { PAGE_SECTIONS } from '../lib/routes'
  import { formatNumber, formatBytes, formatElapsed } from '../lib/utils/format'
  import TrendChart from '../lib/components/common/TrendChart.svelte'
  import PageHeader from '../lib/components/common/PageHeader.svelte'
  import PageBody from '../lib/components/common/PageBody.svelte'
  import Button from '../lib/components/common/Button.svelte'
  import Badge from '../lib/components/common/Badge.svelte'
  import Tabs from '../lib/components/common/Tabs.svelte'
  import Stat from '../lib/components/common/Stat.svelte'
  import Panel from '../lib/components/common/Panel.svelte'
  import EmptyState from '../lib/components/common/EmptyState.svelte'
  import Spinner from '../lib/components/common/Spinner.svelte'
  import Input from '../lib/components/common/Input.svelte'
  import Select from '../lib/components/common/Select.svelte'
  import DataTable, { type DataColumn } from '../lib/components/common/DataTable.svelte'
  import {
    RefreshCw, AlertTriangle, Server, Timer, MemoryStick, Repeat,
    Users, Table2, ExternalLink, Search, X, Funnel,
  } from 'lucide-svelte'

  type Row = Record<string, unknown>

  type SectionKey = Exclude<InsightsSection, 'summary' | 'volume' | 'latency'>

  interface SectionSpec {
    key: SectionKey
    label: string
    icon: typeof Timer
    columns: DataColumn<Row>[]
    openable?: boolean
    /** Clicking the funnel on a row applies this filter from this row field. */
    filterKey?: 'user' | 'table'
    filterField?: string
  }

  const num = (v: unknown) => Number(v ?? 0) || 0
  const fmtNum = (v: unknown) => formatNumber(num(v))
  const fmtBytes = (v: unknown) => formatBytes(num(v))
  const fmtMs = (v: unknown) => formatElapsed(num(v) / 1000)
  const fmtTime = (v: unknown) => String(v ?? '')

  const RANGES: { value: InsightsRange; label: string }[] = [
    { value: '1h', label: '1h' },
    { value: '6h', label: '6h' },
    { value: '24h', label: '24h' },
    { value: '7d', label: '7d' },
    { value: '30d', label: '30d' },
  ]

  const PATTERN_COLUMNS: DataColumn<Row>[] = [
    { key: 'sample_query', label: 'Query pattern', mono: true, truncate: true, width: '40%' },
    { key: 'runs', label: 'Runs', align: 'right', format: fmtNum },
    { key: 'p50_ms', label: 'p50', align: 'right', format: fmtMs },
    { key: 'p95_ms', label: 'p95', align: 'right', format: fmtMs },
    { key: 'max_ms', label: 'Max', align: 'right', format: fmtMs },
    { key: 'max_memory', label: 'Peak mem', align: 'right', format: fmtBytes },
    { key: 'read_rows', label: 'Rows read', align: 'right', format: fmtNum },
    { key: 'last_seen', label: 'Last seen', align: 'right', format: fmtTime },
  ]

  const SECTIONS: SectionSpec[] = [
    { key: 'slow', label: 'Slow queries', icon: Timer, columns: PATTERN_COLUMNS, openable: true },
    { key: 'memory', label: 'Memory', icon: MemoryStick, columns: PATTERN_COLUMNS, openable: true },
    { key: 'frequent', label: 'Frequent', icon: Repeat, columns: PATTERN_COLUMNS, openable: true },
    {
      key: 'errors', label: 'Errors', icon: AlertTriangle, openable: true,
      columns: [
        { key: 'error_name', label: 'Error', mono: true, width: '22%' },
        { key: 'exception_code', label: 'Code', align: 'right' },
        { key: 'occurrences', label: 'Count', align: 'right', format: fmtNum },
        { key: 'sample_error', label: 'Last message', mono: true, truncate: true, width: '40%' },
        { key: 'last_seen', label: 'Last seen', align: 'right', format: fmtTime },
      ],
    },
    {
      key: 'users', label: 'Users', icon: Users, filterKey: 'user', filterField: 'user',
      columns: [
        { key: 'user', label: 'User', mono: true },
        { key: 'runs', label: 'Runs', align: 'right', format: fmtNum },
        { key: 'failures', label: 'Failures', align: 'right', format: fmtNum },
        { key: 'p95_ms', label: 'p95', align: 'right', format: fmtMs },
        { key: 'read_bytes', label: 'Read', align: 'right', format: fmtBytes },
        { key: 'max_memory', label: 'Peak mem', align: 'right', format: fmtBytes },
      ],
    },
    {
      key: 'tables', label: 'Hot tables', icon: Table2, filterKey: 'table', filterField: 'table',
      columns: [
        { key: 'table', label: 'Table', mono: true },
        { key: 'reads', label: 'Queries', align: 'right', format: fmtNum },
        { key: 'read_rows', label: 'Rows read', align: 'right', format: fmtNum },
        { key: 'read_bytes', label: 'Bytes read', align: 'right', format: fmtBytes },
        { key: 'last_seen', label: 'Last read', align: 'right', format: fmtTime },
      ],
    },
  ]

  let range = $state<InsightsRange>('24h')
  let loading = $state(true)
  let refreshing = $state(false)
  let error = $state<string | null>(null)
  let unsupported = $state(false)

  // ── Filters (apply to the whole dashboard: tiles, charts, sections) ──
  let filters = $state<InsightsFilters>({})
  let searchInput = $state('')
  let searchTimer: ReturnType<typeof setTimeout> | null = null

  const KIND_OPTIONS = ['Select', 'Insert', 'Create', 'Alter', 'Drop', 'Other']
  const DURATION_OPTIONS: { value: number; label: string }[] = [
    { value: 0, label: 'Any duration' },
    { value: 100, label: '≥ 100ms' },
    { value: 1000, label: '≥ 1s' },
    { value: 10000, label: '≥ 10s' },
  ]

  const hasFilters = $derived(
    !!(filters.user || filters.kind || filters.search || filters.table || filters.minMs)
  )

  const rangeItems = RANGES.map((r) => ({ id: r.value, label: r.label }))
  const kindOptions = [{ value: '', label: 'All kinds' }, ...KIND_OPTIONS.map((k) => ({ value: k, label: k }))]
  const durationOptions = DURATION_OPTIONS.map((d) => ({ value: String(d.value), label: d.label }))

  function applyFilters(patch: Partial<InsightsFilters>) {
    filters = { ...filters, ...patch }
    void loadAll(true)
  }

  function handleSearchInput() {
    if (searchTimer) clearTimeout(searchTimer)
    searchTimer = setTimeout(() => {
      applyFilters({ search: searchInput.trim() || undefined })
    }, 400)
  }

  function clearFilters() {
    searchInput = ''
    filters = {}
    void loadAll(true)
  }

  function filterFromRow(spec: SectionSpec, row: Record<string, unknown>) {
    if (!spec.filterKey || !spec.filterField) return
    const value = String(row[spec.filterField] ?? '')
    if (value) applyFilters({ [spec.filterKey]: value })
  }

  let summary = $state<Record<string, unknown> | null>(null)
  let meta = $state<{ cluster: string; is_cluster: boolean; degraded: boolean }>({ cluster: '', is_cluster: false, degraded: false })
  let volume = $state<Record<string, unknown>[]>([])
  let latency = $state<Record<string, unknown>[]>([])

  // The sidebar drives sections through ?section=; 'overview' is the
  // tiles + charts, everything else is one table.
  const SECTION_IDS = new Set((PAGE_SECTIONS['query-insights'] ?? []).map((s) => s.id))
  const section = $derived.by((): SectionKey | 'overview' => {
    const current = getSection()
    return current && SECTION_IDS.has(current) ? (current as SectionKey | 'overview') : 'overview'
  })
  const sectionLabel = $derived(
    (PAGE_SECTIONS['query-insights'] ?? []).find((s) => s.id === section)?.label ?? 'Overview',
  )
  // The table section currently loaded (or being loaded).
  let activeSection = $state<SectionKey | null>(null)
  let sectionResult = $state<InsightsResult | null>(null)
  let sectionLoading = $state(false)
  let sectionError = $state<string | null>(null)
  let loadSeq = 0

  // Backend caps sample_query at this many chars (queryinsights.SampleQueryCap);
  // samples at the cap are flagged when opened so broken SQL isn't run blindly.
  const SAMPLE_QUERY_CAP = 2000

  // Pass the already-resolved cluster on follow-up requests so the backend
  // can skip re-resolving it on every call.
  const knownCluster = () => (meta.is_cluster && meta.cluster ? meta.cluster : undefined)

  const activeSpec = $derived(SECTIONS.find((s) => s.key === section) ?? null)

  const failureCount = $derived(num(summary?.failed_queries))

  const volumeChart = $derived({
    x: volume.map((r) => num(r.t)),
    series: [
      { label: 'Queries', values: volume.map((r) => num(r.queries)), color: '#f97316', fill: 'rgba(249,115,22,0.14)' },
      { label: 'Failures', values: volume.map((r) => num(r.failures)), color: '#ef4444' },
    ],
  })
  const latencyChart = $derived({
    x: latency.map((r) => num(r.t)),
    series: [
      { label: 'p50', values: latency.map((r) => num(r.p50_ms)), color: '#7dd3fc' },
      { label: 'p95', values: latency.map((r) => num(r.p95_ms)), color: '#0ea5e9', fill: 'rgba(14,165,233,0.12)' },
    ],
  })

  async function loadAll(showSpinner = false) {
    const seq = ++loadSeq
    if (showSpinner) loading = true
    refreshing = true
    error = null
    try {
      const cluster = knownCluster()
      const [s, v, l] = await Promise.all([
        fetchInsights('summary', range, cluster, filters),
        fetchInsights('volume', range, cluster, filters),
        fetchInsights('latency', range, cluster, filters),
      ])
      if (seq !== loadSeq) return
      unsupported = !s.supported
      summary = s.data[0] ?? null
      meta = { cluster: s.cluster, is_cluster: s.is_cluster, degraded: !!s.degraded }
      volume = v.supported ? v.data : []
      latency = l.supported ? l.data : []
      if (section !== 'overview') void loadSection(section, seq)
    } catch (e: any) {
      if (seq !== loadSeq) return
      error = e.message
    } finally {
      if (seq === loadSeq) {
        loading = false
        refreshing = false
      }
    }
  }

  async function loadSection(key: SectionKey, seq = loadSeq) {
    activeSection = key
    sectionLoading = true
    sectionError = null
    // Guard against both a newer loadAll (seq) and a newer section click
    // (activeSection): two rapid section clicks share the same seq, and the
    // slower response must not land under the other section's columns.
    const stale = () => seq !== loadSeq || key !== activeSection
    try {
      const res = await fetchInsights(key, range, knownCluster(), filters)
      if (stale()) return
      sectionResult = res
    } catch (e: any) {
      if (stale()) return
      sectionResult = null
      sectionError = e.message
    } finally {
      if (!stale()) sectionLoading = false
    }
  }

  function setRange(r: InsightsRange) {
    if (range === r) return
    range = r
    void loadAll(true)
  }

  function openPattern(row: Record<string, unknown>) {
    let sql = String(row.sample_query ?? '')
    if (!sql) return
    if (sql.length >= SAMPLE_QUERY_CAP) {
      sql = `-- NOTE: query text truncated by Query Insights — fetch the full text from system.query_log by query hash.\n${sql}`
    }
    openQueryTab(sql)
  }

  onMount(() => {
    void loadAll(true)
    return () => {
      if (searchTimer) clearTimeout(searchTimer)
    }
  })

  // Follow the sidebar: load a table section when it becomes visible and
  // the summary is already in (loadAll handles the first load itself).
  $effect(() => {
    const key = section
    if (key === 'overview' || loading || key === activeSection) return
    void loadSection(key)
  })
</script>

{#snippet headerMeta()}
  {#if meta.is_cluster}
    <Badge tone="neutral"><Server size={11} /> {meta.cluster}</Badge>
  {/if}
  {#if meta.degraded}
    <Badge tone="warning" title="Some nodes could not be reached; showing local node only">
      <AlertTriangle size={11} /> Degraded
    </Badge>
  {/if}
{/snippet}

<div class="flex h-full min-h-0 flex-col">
  <PageHeader title="Query Insights" subtitle={sectionLabel} meta={headerMeta}>
    {#snippet actions()}
      <Tabs variant="segmented" size="sm" items={rangeItems} value={range} onchange={(id) => setRange(id as InsightsRange)} />
      <Button icon variant="ghost" size="sm" aria-label="Refresh" title="Refresh" onclick={() => loadAll()}>
        <RefreshCw size={14} class={refreshing ? 'animate-spin' : ''} />
      </Button>
    {/snippet}
  </PageHeader>

  <!-- Filters apply to tiles, charts and every table section -->
  <div class="flex h-10 shrink-0 items-center gap-2 border-b border-edge-subtle px-5">
    <div class="relative">
      <Search size={13} class="pointer-events-none absolute left-2.5 top-1/2 -translate-y-1/2 text-fg-4" />
      <Input size="sm" type="search" class="w-64 pl-8 pr-7" placeholder="Filter by query text..." bind:value={searchInput} oninput={handleSearchInput} spellcheck={false} />
      {#if searchInput}
        <button
          class="absolute right-2 top-1/2 -translate-y-1/2 text-fg-4 hover:text-fg"
          onclick={() => { searchInput = ''; applyFilters({ search: undefined }) }}
          aria-label="Clear search"
        >
          <X size={13} />
        </button>
      {/if}
    </div>

    <Select size="sm" class="w-36" options={kindOptions} value={filters.kind ?? ''} onchange={(v) => applyFilters({ kind: v || undefined })} />
    <Select size="sm" class="w-36" options={durationOptions} value={String(filters.minMs ?? 0)} onchange={(v) => applyFilters({ minMs: parseInt(v) || undefined })} />

    {#if filters.user}
      <Badge tone="brand" class="pr-0.5">
        <Users size={10} class="shrink-0" />
        <span class="font-mono">{filters.user}</span>
        <button class="rounded-sm p-0.5 hover:bg-accent-soft" onclick={() => applyFilters({ user: undefined })} aria-label="Remove user filter">
          <X size={10} />
        </button>
      </Badge>
    {/if}
    {#if filters.table}
      <Badge tone="brand" class="pr-0.5">
        <Table2 size={10} class="shrink-0" />
        <span class="font-mono">{filters.table}</span>
        <button class="rounded-sm p-0.5 hover:bg-accent-soft" onclick={() => applyFilters({ table: undefined })} aria-label="Remove table filter">
          <X size={10} />
        </button>
      </Badge>
    {/if}

    {#if hasFilters}
      <Button variant="ghost" size="xs" onclick={clearFilters}>Clear all</Button>
    {/if}
  </div>

  {#if loading}
    <div class="flex flex-1 items-center justify-center"><Spinner /></div>
  {:else if error}
    <PageBody width="lg">
      <EmptyState icon={AlertTriangle} title="Couldn't load query insights" description={error} primary={{ label: 'Retry', onclick: () => loadAll(true) }} />
    </PageBody>
  {:else if unsupported}
    <PageBody width="lg">
      <EmptyState icon={AlertTriangle} title="system.query_log is not available">
        <p class="max-w-[52ch] text-[13px] leading-relaxed text-fg-3">
          Query Insights needs ClickHouse's query log. Enable it with
          <code class="rounded-sm bg-surface-2 px-1 py-0.5 font-mono text-[11px]">&lt;query_log&gt;</code>
          in the server config (it is on by default in most deployments), then run a few queries and refresh.
        </p>
      </EmptyState>
    </PageBody>
  {:else if section === 'overview' || !activeSpec}
    <PageBody width="lg">
      <div class="space-y-5">
        <div class="grid grid-cols-2 gap-3 md:grid-cols-3 xl:grid-cols-6">
          <Stat label="Queries" value={fmtNum(summary?.total_queries)} hint={`${fmtNum(summary?.active_users)} active users`} />
          <Stat label="Failures" value={fmtNum(failureCount)} tone={failureCount > 0 ? 'danger' : 'default'} />
          <Stat label="p50 latency" value={fmtMs(summary?.p50_ms)} />
          <Stat label="p95 latency" value={fmtMs(summary?.p95_ms)} hint={`max ${fmtMs(summary?.max_ms)}`} />
          <Stat label="Data read" value={fmtBytes(summary?.read_bytes)} />
          <Stat label="Peak memory" value={fmtBytes(summary?.peak_memory)} />
        </div>

        <div class="grid grid-cols-1 gap-3 md:grid-cols-2">
          <Panel title="Query volume" description={`Last ${range}`} padding="sm">
            {#if volumeChart.x.length > 1}
              <TrendChart x={volumeChart.x} series={volumeChart.series} height={160} formatY={(v) => formatNumber(Math.round(v))} />
            {:else}
              <div class="grid h-[160px] place-items-center text-xs text-fg-4">Not enough data for this range yet</div>
            {/if}
          </Panel>
          <Panel title="Latency" description={`Last ${range}`} padding="sm">
            {#if latencyChart.x.length > 1}
              <TrendChart x={latencyChart.x} series={latencyChart.series} height={160} formatY={(v) => formatElapsed(v / 1000)} />
            {:else}
              <div class="grid h-[160px] place-items-center text-xs text-fg-4">Not enough data for this range yet</div>
            {/if}
          </Panel>
        </div>
      </div>
    </PageBody>
  {:else}
    {@const spec = activeSpec}
    <div class="flex min-h-0 flex-1 flex-col">
      <div class="flex h-10 shrink-0 items-center gap-3 px-5">
        <spec.icon size={14} class="text-fg-3" />
        <span class="text-[13px] font-semibold text-fg">{spec.label}</span>
        {#if sectionResult?.degraded}
          <span class="inline-flex items-center gap-1 text-[11px] text-warning">
            <AlertTriangle size={12} /> Cluster-wide query failed. Showing the connected node only.
          </span>
        {/if}
        <span class="ml-auto text-xs tabular-nums text-fg-4">
          {#if sectionResult && !sectionLoading}{fmtNum(sectionResult.data.length)} rows{/if}
        </span>
      </div>

      <div class="min-h-0 flex-1 px-5 pb-4">
        {#if sectionLoading}
          <div class="flex h-full items-center justify-center"><Spinner /></div>
        {:else if sectionError}
          <EmptyState icon={AlertTriangle} title={`Couldn't load ${spec.label.toLowerCase()}`} description={sectionError} secondary={{ label: 'Retry', onclick: () => loadSection(spec.key) }} />
        {:else if sectionResult && !sectionResult.supported}
          <EmptyState icon={AlertTriangle} title={`${spec.label} is not available on this ClickHouse version`} />
        {:else if sectionResult}
          <div class="h-full overflow-hidden rounded-lg border border-edge-subtle bg-surface">
            <DataTable
              fill
              columns={spec.columns}
              rows={sectionResult.data}
              emptyTitle="Nothing recorded for this range"
            >
              {#snippet actions(row)}
                {#if spec.filterKey}
                  <Button icon variant="ghost" size="xs"
                    onclick={() => filterFromRow(spec, row)}
                    title={`Filter the page by this ${spec.filterKey}`}
                    aria-label={`Filter by this ${spec.filterKey}`}
                  >
                    <Funnel size={13} />
                  </Button>
                {/if}
                {#if spec.openable}
                  <Button icon variant="ghost" size="xs"
                    onclick={() => openPattern(row)}
                    title="Open in a new query tab"
                    aria-label="Open query in a new tab"
                  >
                    <ExternalLink size={13} />
                  </Button>
                {/if}
              {/snippet}
            </DataTable>
          </div>
        {/if}
      </div>
    </div>
  {/if}
</div>
