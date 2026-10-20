<script lang="ts">
  import { isInModal } from '../../utils/shortcuts'
  import { errorText } from '../../utils/errors'
  import { onMount, untrack } from 'svelte'
  import { RefreshCw, PanelLeftClose, PanelLeftOpen, Radio, Search, Columns3, X } from 'lucide-svelte'
  import Button from '../common/Button.svelte'
  import Select from '../common/Select.svelte'
  import Input from '../common/Input.svelte'
  import Spinner from '../common/Spinner.svelte'
  import EmptyState from '../common/EmptyState.svelte'
  import TimeRangeSelector from '../dashboard/TimeRangeSelector.svelte'
  import SearchHelp from './SearchHelp.svelte'
  import LogsHistogram from './LogsHistogram.svelte'
  import LogsFacets from './LogsFacets.svelte'
  import LogList from './LogList.svelte'
  import LogDetailPanel from './LogDetailPanel.svelte'
  import SavedSearchMenu from './SavedSearchMenu.svelte'
  import { setSection } from '../../stores/nav.svelte'
  import { isMissingTableError } from '../../utils/ch-error'
  import { searchLogs, logsHistogram, logsFacets, logsContext } from '../../api/telemetry'
  import type { TelemetrySource, LogRow, LogFacets, HistogramBucket } from '../../types/telemetry'
  import { encodeAbsoluteDashboardRange } from '../../utils/dashboard-time'
  import { resolveRange } from './time'
  import { formatNumber } from '../../utils/format'
  import { readScoped, writeScoped } from '../../stores/workspace-scope'

  interface Props {
    sources: TelemetrySource[]
    /** Opens the Send data here sheet. */
    onsenddata?: () => void
  }
  let { sources, onsenddata }: Props = $props()

  const logSources = $derived(sources.filter((s) => s.kind === 'logs' && s.enabled))

  // ── Search state (mirrored in the URL) ───────────────────────
  let sourceId = $state('')

  // The Trace tab needs a traces source, not this logs one: prefer the
  // source this logs source is correlated with, else the first enabled
  // traces source. Empty means the tab falls back to correlated logs.
  const traceSourceId = $derived.by(() => {
    const current = logSources.find((s) => s.id === sourceId)
    if (current?.correlated_traces) return current.correlated_traces
    return sources.find((s) => s.kind === 'traces' && s.enabled)?.id ?? ''
  })
  let query = $state('')
  let draft = $state('')
  let range = $state('1h')
  let severity = $state<string[]>([])
  let services = $state<string[]>([])
  /** A row named by a copied link (row=<ts ns>&rowid=<hash>), opened after the first search. */
  let pendingRow: { ts: string; id: string } | null = null

  function readUrl() {
    const p = new URLSearchParams(window.location.search)
    sourceId = p.get('source') ?? ''
    query = p.get('q') ?? ''
    draft = query
    const from = p.get('from')
    const to = p.get('to')
    if (from && to) range = encodeAbsoluteDashboardRange(from, to)
    else if (from) range = from
    severity = (p.get('sev') ?? '').split(',').filter(Boolean)
    services = (p.get('svc') ?? '').split(',').filter(Boolean)
    const row = p.get('row')
    pendingRow = row && /^\d+$/.test(row) ? { ts: row, id: p.get('rowid') ?? '' } : null
  }

  function writeUrl() {
    const url = new URL(window.location.href)
    const set = (k: string, v: string) => (v ? url.searchParams.set(k, v) : url.searchParams.delete(k))
    set('section', 'logs')
    set('source', sourceId)
    set('q', query)
    const abs = range.startsWith('abs:') ? resolveRange(range) : null
    set('from', abs ? abs.from : range)
    set('to', abs ? abs.to : '')
    set('sev', severity.join(','))
    set('svc', services.join(','))
    // A link's row is opened once; the copy-link button makes a new one.
    url.searchParams.delete('row')
    url.searchParams.delete('rowid')
    history.replaceState(null, '', `${url.pathname}?${url.searchParams.toString()}`)
  }

  // ── Data ─────────────────────────────────────────────────────
  let rows = $state<LogRow[]>([])
  let nextCursor = $state<string | null>(null)
  let loading = $state(false)
  let loadingMore = $state(false)
  let error = $state<string | null>(null)
  let tookMs = $state(0)
  let buckets = $state<HistogramBucket[]>([])
  let bucketSeconds = $state(60)
  let histogramRange = $state<{ from: string; to: string } | undefined>()
  let facets = $state<LogFacets | null>(null)
  let facetsLoading = $state(false)
  let facetsError = $state<string | null>(null)
  let expandedKeys = $state<string[]>([])
  let seq = 0
  let facetsSeq = 0
  /** Keyset position of the newest row shown: live tail asks for rows past it. */
  let headCursor: string | null = null

  function baseRequest() {
    const r = resolveRange(range)
    return { source_id: sourceId, from: r.from, to: r.to, q: query, severity, services }
  }

  async function run(opts: { keepScroll?: boolean } = {}) {
    if (!sourceId) return
    const mySeq = ++seq
    loading = !opts.keepScroll
    error = null
    const req = baseRequest()
    try {
      const [search, hist] = await Promise.all([
        searchLogs({ ...req, limit: 200, order: 'desc' }),
        logsHistogram(req).catch(() => null),
      ])
      if (mySeq !== seq) return
      rows = search.rows
      nextCursor = search.next_cursor
      headCursor = search.head_cursor
      tookMs = search.took_ms
      lastSummaryAt = Date.now()
      if (hist) {
        buckets = hist.buckets
        bucketSeconds = hist.bucket_seconds
        histogramRange = { from: req.from, to: req.to }
      } else {
        buckets = []
      }
      writeUrl()
    } catch (e: unknown) {
      if (mySeq !== seq) return
      error = e instanceof Error ? e.message : String(e)
      rows = []
      buckets = []
      nextCursor = null
      headCursor = null
    } finally {
      if (mySeq === seq) loading = false
    }
    void loadFacets()
    if (mySeq === seq) void openPendingRow()
  }

  async function loadHistogram() {
    if (!sourceId) return
    const mySeq = seq
    const req = baseRequest()
    try {
      const hist = await logsHistogram(req)
      if (mySeq !== seq) return
      buckets = hist.buckets
      bucketSeconds = hist.bucket_seconds
      histogramRange = { from: req.from, to: req.to }
    } catch {
      /* the next refresh retries */
    }
  }

  async function loadFacets() {
    if (!sourceId) return
    const mySeq = ++facetsSeq
    facetsLoading = true
    try {
      const res = await logsFacets({ ...baseRequest(), keys: expandedKeys })
      if (mySeq !== facetsSeq) return
      facets = res
      facetsError = null
    } catch (e: unknown) {
      if (mySeq !== facetsSeq) return
      facets = null
      facetsError = errorText(e)
    } finally {
      if (mySeq === facetsSeq) facetsLoading = false
    }
  }

  /** Open the row a copied link points at: from the page, else fetched around its timestamp. */
  async function openPendingRow() {
    const target = pendingRow
    if (!target) return
    pendingRow = null
    const matches = (r: LogRow) => r.timestamp_ns === target.ts && (!target.id || r.row_id === target.id)
    const i = rows.findIndex(matches)
    if (i >= 0) {
      openAt(i)
      list?.scrollToIndex(i)
      return
    }
    try {
      const res = await logsContext({ source_id: sourceId, timestamp_ns: target.ts, row_id: target.id, service: '', before: 1, after: 50 })
      const row = res.rows.find(matches)
      if (row) {
        selectedIndex = -1
        openRow = row
      } else {
        error = 'The linked log row was not found. It may have aged out of the table.'
      }
    } catch (e: unknown) {
      error = e instanceof Error ? e.message : String(e)
    }
  }

  async function loadMore() {
    if (!nextCursor || loadingMore) return
    loadingMore = true
    try {
      const res = await searchLogs({ ...baseRequest(), limit: 200, order: 'desc', cursor: nextCursor })
      rows = [...rows, ...res.rows]
      nextCursor = res.next_cursor
    } catch (e: unknown) {
      error = e instanceof Error ? e.message : String(e)
    } finally {
      loadingMore = false
    }
  }

  function submit() {
    query = draft.trim()
    selectedIndex = -1
    openRow = null
    void run()
  }

  function setRange(v: string) {
    range = v
    void run()
  }

  function toggleSeverity(v: string) {
    severity = severity.includes(v) ? severity.filter((s) => s !== v) : [...severity, v]
    void run()
  }

  function toggleService(v: string) {
    services = services.includes(v) ? services.filter((s) => s !== v) : [...services, v]
    void run()
  }

  function expandKey(key: string) {
    expandedKeys = expandedKeys.includes(key) ? expandedKeys.filter((k) => k !== key) : [...expandedKeys, key].slice(-10)
    void loadFacets()
  }

  function quoteValue(v: string): string {
    return /[\s"():]/.test(v) ? `"${v.replace(/"/g, '\\"')}"` : v
  }

  function addFilter(key: string, value: string, negate = false) {
    const term = `${negate ? '-' : ''}${key}:${quoteValue(value)}`
    draft = draft.trim() ? `${draft.trim()} ${term}` : term
    submit()
  }

  function onHistogramRange(from: string, to: string) {
    setRange(encodeAbsoluteDashboardRange(from, to))
  }

  // ── Saved searches ───────────────────────────────────────────
  function loadSaved(q: string, preset: string, savedSourceId: string | null) {
    if (savedSourceId && savedSourceId !== sourceId && logSources.some((s) => s.id === savedSourceId)) {
      sourceId = savedSourceId
      loadColumns()
    }
    draft = q
    query = q
    range = preset || range
    selectedIndex = -1
    openRow = null
    void run()
  }

  // ── Extra columns (attribute keys), remembered per source ────
  const COLUMNS_KEY = 'ch-ui-telemetry-log-columns'
  let extraColumns = $state<string[]>([])
  let columnsOpen = $state(false)
  let columnsFilter = $state('')
  let columnsRoot = $state<HTMLDivElement | null>(null)

  function readColumns(): Record<string, string[]> {
    try {
      const parsed: unknown = JSON.parse(readScoped(COLUMNS_KEY) ?? '{}')
      return parsed && typeof parsed === 'object' ? (parsed as Record<string, string[]>) : {}
    } catch {
      return {}
    }
  }

  function loadColumns() {
    extraColumns = sourceId ? (readColumns()[sourceId] ?? []) : []
  }

  function toggleColumn(key: string) {
    extraColumns = extraColumns.includes(key) ? extraColumns.filter((k) => k !== key) : [...extraColumns, key]
    writeScoped(COLUMNS_KEY, JSON.stringify({ ...readColumns(), [sourceId]: extraColumns }))
  }

  const columnCandidates = $derived.by(() => {
    const keys = (facets?.attribute_keys ?? []).map((k) => k.key)
    const all = Array.from(new Set([...extraColumns, ...keys]))
    const f = columnsFilter.trim().toLowerCase()
    return f ? all.filter((k) => k.toLowerCase().includes(f)) : all
  })

  function onDocClick(e: MouseEvent) {
    if (columnsOpen && columnsRoot && !columnsRoot.contains(e.target as Node)) columnsOpen = false
  }

  // ── Selection / panel ────────────────────────────────────────
  let selectedIndex = $state(-1)
  let openRow = $state<LogRow | null>(null)
  let panelWidth = $state(520)
  let list = $state<LogList | null>(null)
  let facetsOpen = $state(true)

  function openAt(i: number) {
    selectedIndex = i
    openRow = rows[i] ?? null
  }

  function onKey(e: KeyboardEvent) {
    const target = e.target as HTMLElement | null
    if (target && (target.tagName === 'INPUT' || target.tagName === 'TEXTAREA' || target.isContentEditable)) return
    if (isInModal(target)) return
    if (e.key === 'j' || e.key === 'ArrowDown') {
      e.preventDefault()
      selectedIndex = Math.min(rows.length - 1, selectedIndex + 1)
      list?.scrollToIndex(selectedIndex)
      if (openRow) openRow = rows[selectedIndex] ?? null
    } else if (e.key === 'k' || e.key === 'ArrowUp') {
      e.preventDefault()
      selectedIndex = Math.max(0, selectedIndex - 1)
      list?.scrollToIndex(selectedIndex)
      if (openRow) openRow = rows[selectedIndex] ?? null
    } else if (e.key === 'Enter' && selectedIndex >= 0) {
      openAt(selectedIndex)
    } else if (e.key === 'Escape') {
      openRow = null
    }
  }

  // ── Live tail ────────────────────────────────────────────────
  // Every 5 s ask only for rows newer than the newest one shown and put
  // them on top; the histogram and facets refresh at most every 30 s. An
  // absolute range has no "new" rows, so live is off there. Opening a row
  // or scrolling down pauses it so the list does not move under the reader.
  const LIVE_POLL_MS = 5_000
  const LIVE_SUMMARY_MS = 30_000
  const LIVE_MAX_ROWS = 2_000
  let live = $state(false)
  let atTop = $state(true)
  let polling = false
  let lastSummaryAt = 0
  const absoluteRange = $derived(range.startsWith('abs:'))
  const livePaused = $derived(live && (openRow !== null || !atTop))

  $effect(() => {
    if (absoluteRange && untrack(() => live)) live = false
  })

  $effect(() => {
    if (!live) return
    const timer = setInterval(() => void liveTick(), LIVE_POLL_MS)
    return () => clearInterval(timer)
  })

  async function liveTick() {
    if (polling || loading || openRow || !atTop || !sourceId || range.startsWith('abs:')) return
    polling = true
    const mySeq = seq
    try {
      const res = await searchLogs({ ...baseRequest(), limit: 200, order: 'asc', cursor: headCursor })
      if (mySeq !== seq || openRow || !atTop) return
      if (res.next_cursor || rows.length + res.rows.length > LIVE_MAX_ROWS) {
        // More arrived than one page, or the list grew long: start over.
        polling = false
        await run({ keepScroll: true })
        return
      }
      if (res.rows.length > 0) {
        rows = [...[...res.rows].reverse(), ...rows]
        headCursor = res.tail_cursor
        if (selectedIndex >= 0) selectedIndex += res.rows.length
      }
      if (Date.now() - lastSummaryAt >= LIVE_SUMMARY_MS) {
        lastSummaryAt = Date.now()
        void loadHistogram()
        void loadFacets()
      }
    } catch (e: unknown) {
      if (mySeq === seq) error = e instanceof Error ? e.message : String(e)
    } finally {
      polling = false
    }
  }

  const highlightTerms = $derived(
    query
      .split(/\s+/)
      .filter((t) => t && !t.includes(':') && !/^(AND|OR|NOT)$/i.test(t) && !t.startsWith('-') && !t.startsWith('('))
      .map((t) => t.replace(/^"|"$|\*|\)/g, '')),
  )

  onMount(() => {
    readUrl()
    untrack(() => {
      if (!sourceId || !logSources.some((s) => s.id === sourceId)) sourceId = logSources[0]?.id ?? ''
      loadColumns()
      void run()
    })
  })

  $effect(() => {
    // A source list that arrives after mount (or loses the chosen source).
    if (logSources.length > 0 && !logSources.some((s) => s.id === sourceId)) {
      sourceId = logSources[0].id
      loadColumns()
      void run()
    }
  })
</script>

<svelte:window onkeydown={onKey} />
<svelte:document onclick={onDocClick} />

{#if logSources.length === 0}
  <EmptyState
    icon={Search}
    title="No logs source yet"
    description="Send logs straight to CH-UI, or point it at the ClickHouse table your OpenTelemetry collector already writes to."
    primary={onsenddata ? { label: 'Send data here', onclick: onsenddata } : { label: 'Configure sources', onclick: () => setSection('sources') }}
    secondary={onsenddata ? { label: 'Configure sources', onclick: () => setSection('sources') } : undefined}
  />
{:else}
  <div class="flex h-10 shrink-0 items-center gap-2 border-b border-edge-subtle px-5">
    {#if logSources.length > 1}
      <Select size="sm" class="w-44" options={logSources.map((s) => ({ value: s.id, label: s.name }))} value={sourceId} onchange={(v) => { sourceId = v; loadColumns(); void run() }} />
    {/if}
    <form class="flex min-w-0 flex-1 items-center gap-2" onsubmit={(e) => { e.preventDefault(); submit() }}>
      <Input size="sm" mono class="flex-1" placeholder={'service:api level:error "rate limit" http.status_code:>=500'} bind:value={draft} spellcheck={false} autocomplete="off" />
      <SearchHelp />
    </form>
    <TimeRangeSelector value={range} onchange={setRange} />
    <SavedSearchMenu kind="logs" {sourceId} currentQuery={query} currentRange={range} onload={loadSaved} />
    <div bind:this={columnsRoot} class="relative">
      <Button icon variant="ghost" size="sm" aria-label="Choose columns" title="Columns" aria-pressed={extraColumns.length > 0} onclick={() => (columnsOpen = !columnsOpen)}>
        <Columns3 size={14} />
      </Button>
      {#if columnsOpen}
        <div class="surface-card absolute right-0 z-[120] mt-1 w-72 rounded-md p-2">
          <div class="mb-2 flex items-center justify-between">
            <span class="text-xs font-medium text-fg">Extra columns</span>
            {#if extraColumns.length > 0}
              <button class="inline-flex items-center gap-1 text-[11px] text-fg-3 hover:text-fg" onclick={() => { for (const k of [...extraColumns]) toggleColumn(k) }}>
                <X size={11} /> Clear
              </button>
            {/if}
          </div>
          <Input size="sm" placeholder="Filter attribute keys" bind:value={columnsFilter} />
          <div class="mt-2 max-h-64 overflow-y-auto">
            {#if columnCandidates.length === 0}
              <p class="px-1 py-2 text-xs text-fg-4">Run a search to discover attribute keys.</p>
            {:else}
              {#each columnCandidates as key (key)}
                <label class="flex cursor-pointer items-center gap-2 rounded-sm px-1 py-1 text-xs text-fg-2 hover:bg-hover">
                  <input type="checkbox" class="ds-checkbox ds-checkbox-sm" checked={extraColumns.includes(key)} onchange={() => toggleColumn(key)} />
                  <span class="truncate font-mono">{key}</span>
                </label>
              {/each}
            {/if}
          </div>
        </div>
      {/if}
    </div>
    <Button
      size="sm"
      variant={live && !livePaused ? 'primary' : 'outline'}
      aria-pressed={live}
      disabled={absoluteRange}
      title={absoluteRange
        ? 'Live tail needs a relative range such as the last hour'
        : livePaused
          ? 'Paused while a row is open or the list is scrolled down'
          : 'Show new rows every 5 seconds'}
      onclick={() => (live = !live)}
    >
      <Radio size={13} /> {livePaused ? 'Paused' : 'Live'}
    </Button>
    <Button icon variant="ghost" size="sm" aria-label="Refresh" title="Refresh" onclick={() => run()}>
      <RefreshCw size={14} class={loading ? 'animate-spin' : ''} />
    </Button>
  </div>

  {#if error && isMissingTableError(error)}
    <EmptyState
      icon={Search}
      title="Telemetry tables not found"
      description="The logs table this source points at does not exist in ClickHouse. Check the source, or start the OpenTelemetry collector so it creates its tables."
      primary={{ label: 'Configure sources', onclick: () => setSection('sources') }}
    />
  {:else}
  {#if error}
    <div class="shrink-0 border-b border-edge-subtle bg-danger-soft px-5 py-1.5 text-xs text-danger">{errorText(error)}</div>
  {/if}

  <div class="flex min-h-0 flex-1">
    {#if facetsOpen}
      <LogsFacets
        {facets}
        loading={facetsLoading}
        error={facetsError}
        selectedSeverity={severity}
        selectedServices={services}
        {expandedKeys}
        ontoggleseverity={toggleSeverity}
        ontoggleservice={toggleService}
        onexpandkey={expandKey}
        onaddfilter={(k, v) => addFilter(k, v)}
      />
    {/if}

    <div class="flex min-w-0 flex-1 flex-col">
      <div class="flex h-8 shrink-0 items-center gap-2 px-3 text-xs text-fg-4">
        <Button icon variant="ghost" size="xs" aria-label={facetsOpen ? 'Hide facets' : 'Show facets'} title={facetsOpen ? 'Hide facets' : 'Show facets'} onclick={() => (facetsOpen = !facetsOpen)}>
          {#if facetsOpen}<PanelLeftClose size={13} />{:else}<PanelLeftOpen size={13} />{/if}
        </Button>
        {#if loading}
          <Spinner size="sm" class="h-3 w-3" /> Searching…
        {:else}
          <span>{formatNumber(rows.length)}{nextCursor ? '+' : ''} rows · {tookMs} ms</span>
          <span class="ml-auto">Drag on the chart to zoom · j/k move · Enter opens · Esc closes</span>
        {/if}
      </div>
      <div class="shrink-0 px-3 pb-1">
        <LogsHistogram {buckets} {bucketSeconds} range={histogramRange} height={110} onrange={onHistogramRange} />
      </div>
      {#if !loading && rows.length === 0 && !error}
        <EmptyState size="compact" title="Nothing in this range" description="Widen the time range or loosen the query." />
      {:else}
        <LogList
          bind:this={list}
          {rows}
          {selectedIndex}
          {highlightTerms}
          {loadingMore}
          hasMore={!!nextCursor}
          {extraColumns}
          onselect={(i) => (selectedIndex = i)}
          onopen={openAt}
          onnearend={loadMore}
          onscrolltop={(v) => (atTop = v)}
        />
      {/if}
    </div>

    {#if openRow}
      <LogDetailPanel
        row={openRow}
        {sourceId}
        {traceSourceId}
        {range}
        width={panelWidth}
        onclose={() => (openRow = null)}
        onwidth={(w) => (panelWidth = w)}
        onfilter={(k, v, neg) => addFilter(k, v, neg)}
      />
    {/if}
  </div>
  {/if}
{/if}
