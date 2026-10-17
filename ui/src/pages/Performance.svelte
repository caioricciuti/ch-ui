<!-- SPDX-License-Identifier: BUSL-1.1 -->
<script lang="ts">
  import { onMount, untrack } from 'svelte'
  import { RefreshCw, Search, ArrowLeft, AlertTriangle } from 'lucide-svelte'
  import {
    fetchRegressions, fetchInvestigations, fetchInvestigation, createInvestigation, updateInvestigation,
    compareInvestigation, fetchPerformanceMonitor, setPerformanceMonitor,
    type PerformanceRange, type PerformanceReport, type Pattern, type Investigation, type InvestigationDetail,
    type Metrics, type Change, type Comparison, type PerformanceMonitor, type Window,
  } from '../lib/api/performance'
  import { getSession } from '../lib/stores/session.svelte'
  import { getSection, setSection } from '../lib/stores/nav.svelte'
  import { goTo } from '../lib/stores/router.svelte'
  import { PAGE_SECTIONS, docsURL } from '../lib/routes'
  import { formatBytes, formatNumber, formatElapsed, formatDate } from '../lib/utils/format'
  import PageHeader from '../lib/components/common/PageHeader.svelte'
  import PageBody from '../lib/components/common/PageBody.svelte'
  import Button from '../lib/components/common/Button.svelte'
  import Panel from '../lib/components/common/Panel.svelte'
  import Badge from '../lib/components/common/Badge.svelte'
  import DataTable, { type DataColumn } from '../lib/components/common/DataTable.svelte'
  import Stat from '../lib/components/common/Stat.svelte'
  import Spinner from '../lib/components/common/Spinner.svelte'
  import EmptyState from '../lib/components/common/EmptyState.svelte'
  import Input from '../lib/components/common/Input.svelte'
  import Textarea from '../lib/components/common/Textarea.svelte'
  import Select from '../lib/components/common/Select.svelte'
  import FormField from '../lib/components/common/FormField.svelte'
  import Sheet from '../lib/components/common/Sheet.svelte'

  // The sidebar drives the section through ?section=.
  type Section = 'regressions' | 'investigations'
  const section = $derived<Section>(getSection() === 'investigations' ? 'investigations' : 'regressions')
  const sectionLabel = $derived((PAGE_SECTIONS.performance ?? []).find((s) => s.id === section)?.label ?? 'Regressions')
  // The section whose view is on screen; a change from the sidebar resets it.
  let shownSection: Section = untrack(() => section)
  let range = $state<PerformanceRange>('24h')
  let report = $state<PerformanceReport | null>(null)
  let investigations = $state<Investigation[]>([])
  let detail = $state<InvestigationDetail | null>(null)
  let monitor = $state<PerformanceMonitor | null>(null)
  let loading = $state(false)
  let busy = $state(false)
  let error = $state('')
  let search = $state('')
  let allPatterns = $state(false)
  let selected = $state<Pattern | null>(null)
  let title = $state('')
  let owner = $state('')
  let note = $state('')
  let status = $state<Investigation['status']>('open')
  let replacement = $state('')
  let baselineOpen = $state(false)
  const selectedRuns = $derived(selected?.current.runs ?? 0)
  let now = $state(Date.now())
  let sequence = 0
  let detailSequence = 0
  const canWrite = $derived(['admin', 'analyst'].includes(getSession()?.role ?? ''))
  const isAdmin = $derived(getSession()?.role === 'admin')
  const rangeOptions = [{ value: '1h', label: '1 hour' }, { value: '6h', label: '6 hours' }, { value: '24h', label: '24 hours' }, { value: '7d', label: '7 days' }]
  const visiblePatterns = $derived((allPatterns ? report?.patterns : report?.regressions)?.filter(p =>
    `${p.sample_query} ${p.hash} ${p.database}`.toLowerCase().includes(search.toLowerCase())) ?? [])
  const filteredInvestigations = $derived(investigations.filter(i => `${i.title} ${i.owner} ${i.status}`.toLowerCase().includes(search.toLowerCase())))
  const comparisonReadyAt = $derived(detail ? Date.parse(detail.investigation.baseline.window.end) +
    (Date.parse(detail.investigation.baseline.window.end) - Date.parse(detail.investigation.baseline.window.start)) + 60000 : 0)
  const comparisonReady = $derived(now >= comparisonReadyAt)
  const latestComparison = $derived(detail?.events.find(e => e.kind === 'comparison')?.payload as Comparison | undefined)
  const replacementOptions = $derived([{value:'',label:'Original query pattern'}, ...(report?.patterns ?? []).filter(p => p.hash !== detail?.investigation.query_hash || p.database !== detail?.investigation.database).map(p => ({value:JSON.stringify([p.hash,p.database]),label:`${p.database || 'default'} · ${p.hash} · ${p.sample_query.slice(0,90)}`}))])
  const metricNames: Partial<Record<keyof Metrics, string>> = { p95_ms: 'p95 latency', mean_ms: 'Mean latency', mean_memory_bytes: 'Memory / query', mean_read_bytes: 'Bytes read / query', mean_cpu_ms: 'CPU / query' }
  const ms = (n: number) => formatElapsed(n / 1000)
  const date = (s: string) => new Date(s).toLocaleString()
  const windowLabel = (w: Window) => `${date(w.start)} → ${date(w.end)}`
  const metricValue = (metric: keyof Metrics, value: number) => metric.includes('bytes') ? formatBytes(value) : ms(value)
  const delta = (c: Change) => c.percent === null ? 'No percentage baseline' : `${c.percent > 0 ? '+' : ''}${c.percent.toFixed(1)}%`

  type PatternRow = Record<string, unknown> & Pattern & { pattern: string; db: string; current_p95: number; baseline_p95: number; change: number | null; runs: number; memory: number }
  type InvestigationRow = Record<string, unknown> & Investigation & { pattern: string; owner_label: string }
  const p95Change = (p: Pattern) => p.changes.find((c) => c.metric === 'p95_ms')
  const patternRows = $derived<PatternRow[]>(visiblePatterns.map((p) => ({
    ...p, pattern: p.sample_query.replace(/\s+/g, ' ').trim(), db: p.database || 'default',
    current_p95: p.current.p95_ms, baseline_p95: p.baseline.p95_ms, change: p95Change(p)?.percent ?? null, runs: p.current.runs, memory: p.current.mean_memory_bytes,
  })))
  const investigationRows = $derived<InvestigationRow[]>(filteredInvestigations.map((i) => ({ ...i, pattern: `${i.database || 'default'} · ${i.query_hash}`, owner_label: i.owner || 'Unassigned' })))
  const statusLabel = (p: Pattern) => p.status === 'insufficient_data' ? 'Insufficient data' : p.status === 'regressed' ? 'Regression' : 'Stable'
  const patternColumns: DataColumn<PatternRow>[] = [
    { key: 'status', label: 'Status', width: '130px', format: (_, r) => statusLabel(r) },
    { key: 'pattern', label: 'Pattern', mono: true, truncate: true },
    { key: 'db', label: 'Database', mono: true, width: '140px' },
    { key: 'current_p95', label: 'Current p95', align: 'right', mono: true, width: '110px', format: (v) => ms(Number(v)) },
    { key: 'baseline_p95', label: 'Baseline p95', align: 'right', mono: true, width: '110px', format: (v) => ms(Number(v)) },
    { key: 'change', label: 'Change', align: 'right', width: '120px', sortValue: (r) => r.change ?? Number.NEGATIVE_INFINITY, format: (_, r) => { const c = p95Change(r); return r.status === 'insufficient_data' ? 'Insufficient data' : c ? delta(c) : '' } },
    { key: 'runs', label: 'Executions', align: 'right', mono: true, width: '110px', format: (v) => formatNumber(Number(v)) },
    { key: 'memory', label: 'Memory / query', align: 'right', mono: true, width: '130px', format: (v) => formatBytes(Number(v)) },
  ]
  const investigationColumns: DataColumn<InvestigationRow>[] = [
    { key: 'title', label: 'Title', truncate: true },
    { key: 'pattern', label: 'Pattern', mono: true, truncate: true },
    { key: 'status', label: 'Status', width: '120px' },
    { key: 'owner_label', label: 'Owner', width: '180px' },
    { key: 'created_at', label: 'Opened', width: '180px', format: formatDate },
    { key: 'updated_at', label: 'Updated', width: '180px', format: formatDate },
  ]

  // A handful of fixed metrics in a set order: no sorting.
  const changeColumns: DataColumn<Record<string, unknown>>[] = [
    { key: 'metric', label: 'Metric', sortable: false, format: (v) => metricNames[v as keyof Metrics] ?? String(v) },
    { key: 'before', label: 'Baseline', align: 'right', mono: true, sortable: false, format: (v, r) => metricValue(r.metric as keyof Metrics, Number(v)) },
    { key: 'after', label: 'Current', align: 'right', mono: true, sortable: false, format: (v, r) => metricValue(r.metric as keyof Metrics, Number(v)) },
    { key: 'percent', label: 'Change', align: 'right', mono: true, sortable: false },
  ]

  async function load() {
    const seq = ++sequence
    loading = true
    error = ''
    // Persisted investigations remain available if query_log is unavailable.
    const results = await Promise.allSettled([fetchRegressions(range), fetchInvestigations(), isAdmin ? fetchPerformanceMonitor() : Promise.resolve(null)])
    if (seq !== sequence) return
    const [r, i, m] = results
    if (r.status === 'fulfilled') report = r.value
    else { report = null; error = r.reason instanceof Error ? r.reason.message : 'Could not read query log' }
    if (i.status === 'fulfilled') investigations = i.value.investigations
    else error = i.reason instanceof Error ? i.reason.message : 'Could not load investigations'
    if (m.status === 'fulfilled') monitor = m.value
    else error = m.reason instanceof Error ? m.reason.message : 'Could not load background scan settings'
    now = Date.now()
    loading = false
  }

  function selectPattern(p: Pattern) {
    selected = p
    detail = null
    baselineOpen = false
    title = `Investigate ${p.database || 'query'} · ${p.hash}`
    owner = getSession()?.user ?? ''
    note = ''
    replacement = ''
    error = ''
  }

  function showDetail(value: InvestigationDetail) {
    detail = value
    selected = null
    baselineOpen = false
    title = value.investigation.title
    owner = value.investigation.owner
    status = value.investigation.status
    note = ''
    replacement = ''
    now = Date.now()
    // Set before navigating so the section effect does not clear this detail.
    shownSection = 'investigations'
    setSection('investigations')
  }

  async function openInvestigation(id: string) {
    const seq = ++detailSequence
    error = ''
    busy = true
    try { const value = await fetchInvestigation(id); if (seq === detailSequence) showDetail(value) }
    catch (e) { if (seq === detailSequence) error = (e instanceof Error ? e.message : String(e)) }
    finally { if (seq === detailSequence) busy = false }
  }

  async function save() {
    if (busy || !title.trim()) return
    error = ''
    busy = true
    try {
      const value = selected
        ? await createInvestigation({ title, owner, note, hash: selected.hash, database: selected.database, range, cluster: report?.cluster ?? '' })
        : detail ? await updateInvestigation(detail.investigation.id, { title, owner, note, status, revision: detail.investigation.revision }) : null
      if (value) showDetail(value)
      investigations = (await fetchInvestigations()).investigations
    } catch (e) { error = (e instanceof Error ? e.message : String(e)) }
    finally { busy = false }
  }

  async function compare() {
    if (!detail || busy) return
    error = ''
    busy = true
    try {
      const [hash,database] = replacement ? JSON.parse(replacement) as [string,string] : [detail.investigation.query_hash,detail.investigation.database]
      showDetail(await compareInvestigation(detail.investigation.id, {hash,database}))
      investigations = (await fetchInvestigations()).investigations
    } catch (e) { error = (e instanceof Error ? e.message : String(e)) }
    finally { busy = false }
  }

  async function toggleMonitor() {
    if (!monitor || busy) return
    busy = true
    error = ''
    try { monitor = await setPerformanceMonitor(!monitor.enabled) }
    catch (e) { error = (e instanceof Error ? e.message : String(e)) }
    finally { busy = false }
  }

  $effect(() => {
    const next = section
    untrack(() => {
      if (next === shownSection) return
      shownSection = next
      detail = null
      selected = null
      search = ''
      detailSequence++
    })
  })

  onMount(() => {
    void load()
    const timer = setInterval(() => { now = Date.now(); if (!document.hidden && !loading && !busy && !selected && !detail) void load() }, 300000)
    return () => { clearInterval(timer); sequence++; detailSequence++ }
  })
  function openBackgroundAccounts() {
    goTo('admin')
    setSection('tunnels')
  }
</script>

{#snippet changesTable(changes: Change[], sufficient = true)}
  <DataTable columns={changeColumns} rows={changes as unknown as Record<string, unknown>[]} rowKey={(r) => String(r.metric)} framed={false} emptyTitle="No metrics">
    {#snippet cell(row, col, value)}
      {@const change = row as unknown as Change}
      {#if col.key === 'before'}
        <span class="text-fg-3">{value}</span>
      {:else if col.key === 'after'}
        <span class="text-fg">{value}</span>
      {:else if col.key === 'percent'}
        <span class={sufficient && change.regressed ? 'text-danger' : 'text-fg-3'}>{sufficient ? delta(change) : 'Insufficient data'}</span>
      {:else}
        {value}
      {/if}
    {/snippet}
  </DataTable>
{/snippet}

<div class="flex h-full min-h-0 flex-col">
  <PageHeader title="Performance" subtitle={sectionLabel} docs={docsURL('performance', section)}>
    {#snippet actions()}
      <Button variant="ghost" size="sm" disabled={loading || busy} onclick={() => load()}><RefreshCw size={14} class={loading ? 'animate-spin' : ''} /> Refresh</Button>
    {/snippet}
  </PageHeader>
  {#if selected || detail}
    <PageBody width="lg">
      <div class="space-y-4">
        {#if error}<EmptyState size="compact" icon={AlertTriangle} title="Request failed" description={error} />{/if}
        <Button variant="ghost" size="sm" disabled={busy} onclick={() => { selected = null; detail = null; detailSequence++ }}><ArrowLeft size={14} /> Back</Button>
        {#if selected}
          {@const sufficient = selected.status !== 'insufficient_data'}
          <Panel title={`${selected.database || 'default'} · ${selected.hash}`} description="Saved measurements and notes are shared with users of this connection.">
            {#snippet actions()}
              {#if canWrite}
                {#if selectedRuns < 10}<span class="text-xs text-fg-3">At least 10 successful queries in the current window are needed to capture a baseline.</span>{/if}
                <Button size="sm" disabled={busy || selectedRuns < 10} onclick={() => (baselineOpen = true)}>Capture baseline</Button>
              {/if}
            {/snippet}
            <pre class="mb-4 max-h-40 overflow-auto whitespace-pre-wrap rounded-md bg-surface-2 p-3 text-xs text-fg-3">{selected.sample_query}</pre>
            <div class="mb-4 grid grid-cols-2 gap-3 md:grid-cols-4">
              <Stat label="Status" size="md" value={statusLabel(selected)} tone={selected.status === 'regressed' ? 'danger' : selected.status === 'insufficient_data' ? 'warning' : 'default'} />
              <Stat label="Current p95" value={ms(selected.current.p95_ms)} hint={`Baseline ${ms(selected.baseline.p95_ms)}`} />
              <Stat label="Executions" value={formatNumber(selected.current.runs)} hint={`Baseline ${formatNumber(selected.baseline.runs)}`} />
              <Stat label="Memory / query" value={formatBytes(selected.current.mean_memory_bytes)} hint={`Baseline ${formatBytes(selected.baseline.mean_memory_bytes)}`} />
            </div>
            {@render changesTable(selected.changes, sufficient)}
          </Panel>
        {:else if detail}
          <Panel title={detail.investigation.title} description="Saved measurements and notes are shared with users of this connection.">
            <pre class="mb-4 max-h-40 overflow-auto whitespace-pre-wrap rounded-md bg-surface-2 p-3 text-xs text-fg-3">{detail.investigation.sample_query}</pre>
            <div class="grid gap-4 md:grid-cols-2">
              <FormField label="Title" for="performance-title" controlWidth="full"><Input id="performance-title" bind:value={title} disabled={!canWrite || busy} /></FormField>
              <FormField label="Owner" for="performance-owner" controlWidth="full"><Input id="performance-owner" bind:value={owner} placeholder="Name or email" disabled={!canWrite || busy} /></FormField>
              <FormField label="Status" for="performance-status" controlWidth="full"><Select id="performance-status" value={status} onchange={v => status = v as Investigation['status']} disabled={!canWrite || busy} options={[{value:'open',label:'Open'},{value:'monitoring',label:'Monitoring a change'},{value:'resolved',label:'Resolved'}]} /></FormField>
            </div>
            {#if canWrite}
              <FormField class="mt-4" label="Add a note or record a change" for="performance-note" controlWidth="full"><Textarea id="performance-note" bind:value={note} disabled={busy} placeholder="What changed, when it was deployed, and what you expect to improve" /></FormField>
              <FormField class="mt-4" label="Compare against (choose a replacement if the SQL changed)" for="performance-replacement" controlWidth="full" hint={`Replacement choices come from the latest ${range} scan. Compare equivalent workloads and wait a full baseline window after your deployment so the comparison excludes pre-change workload.`}><Select id="performance-replacement" options={replacementOptions} bind:value={replacement} disabled={busy} /></FormField>
              <div class="mt-4 flex flex-wrap items-center gap-3">
                <Button disabled={busy || !title.trim()} onclick={save}>Save update</Button>
                <Button variant="secondary" disabled={busy || !comparisonReady || !!note.trim()} onclick={compare}>Capture comparison</Button>
                {#if !comparisonReady}<span class="text-xs text-fg-3">Full comparison available {new Date(comparisonReadyAt).toLocaleString()}</span>{/if}
                {#if note.trim()}<span class="text-xs text-fg-3">Save your note before capturing a comparison.</span>{/if}
              </div>
            {/if}
          </Panel>
        {/if}

        {#if detail}
          {@const baseline = detail.investigation.baseline}
          <Panel title="Captured baseline" description={`${windowLabel(baseline.window)} · ${baseline.cluster ? `Cluster: ${baseline.cluster}` : `Connected node: ${baseline.node || 'unavailable'}`}`}>
            <div class="grid grid-cols-2 gap-3 md:grid-cols-4"><Stat label="Successful queries" value={formatNumber(baseline.metrics.runs)} /><Stat label="p95 latency" value={ms(baseline.metrics.p95_ms)} /><Stat label="Mean bytes read" value={formatBytes(baseline.metrics.mean_read_bytes)} /><Stat label="Failed queries" value={formatNumber(baseline.metrics.failures)} /></div>
          </Panel>
          {#if latestComparison}
            <Panel title="Latest comparison" description={windowLabel(latestComparison.current.window)}>
              <p class="mb-2 font-mono text-xs text-fg-3">{latestComparison.baseline.database} · {latestComparison.baseline.hash} → {latestComparison.current.database} · {latestComparison.current.hash}</p>
              {#if !latestComparison.sufficient}<p class="mb-3 text-xs text-warning">At least 10 successful queries are needed in each window. Missing observations do not demonstrate an improvement.</p>{/if}
              {@render changesTable(latestComparison.changes, latestComparison.sufficient)}
              <p class="mt-3 text-xs text-fg-3">Successful queries: {formatNumber(latestComparison.baseline.metrics.runs)} → {formatNumber(latestComparison.current.metrics.runs)}. Failures: {formatNumber(latestComparison.baseline.metrics.failures)} → {formatNumber(latestComparison.current.metrics.failures)}. Changes are observational; workload and cache differences can affect the result.</p>
            </Panel>
          {/if}
          <Panel title="Investigation history" description="Latest 100 events; captured baselines and comparisons stay unchanged.">
            <div class="divide-y divide-edge-subtle">
              {#each detail.events as event (event.id)}
                <div class="py-3 first:pt-0 last:pb-0">
                  <div class="flex flex-wrap items-center gap-2"><Badge tone="neutral">{event.kind === 'baseline' ? 'Baseline captured' : event.kind === 'comparison' ? 'Comparison captured' : 'Updated'}</Badge><span class="text-xs text-fg-3">{event.actor} · {date(event.created_at)}</span></div>
                  {#if event.note}<p class="mt-2 whitespace-pre-wrap text-sm text-fg-2">{event.note}</p>{/if}
                  {#if event.kind === 'update'}{@const update = event.payload as {title:string;owner:string;status:string}}<p class="mt-2 text-xs text-fg-3">{update.title} · {update.status} · Owner: {update.owner || 'Unassigned'}</p>{/if}
                  {#if event.kind === 'comparison'}{@const comparison = event.payload as Comparison}<details class="mt-2"><summary class="cursor-pointer text-xs text-fg-3">View results · {formatNumber(comparison.current.metrics.runs)} successful queries</summary><div class="mt-2">{@render changesTable(comparison.changes, comparison.sufficient)}</div></details>{/if}
                </div>
              {/each}
            </div>
          </Panel>
        {/if}
      </div>
    </PageBody>
  {:else if section === 'regressions'}
    <div class="flex h-10 shrink-0 items-center gap-3 border-b border-edge-subtle px-4">
      <label for="performance-range" class="text-xs text-fg-3">Compare each window</label>
      <Select id="performance-range" size="sm" class="w-32" value={range} options={rangeOptions} onchange={v => { range = v as PerformanceRange; void load() }} disabled={loading} />
      <label class="ds-checkbox-label"><input type="checkbox" class="ds-checkbox ds-checkbox-sm" bind:checked={allPatterns} /> All observed patterns</label>
      <div class="relative ml-auto">
        <Search size={13} class="pointer-events-none absolute left-2.5 top-1/2 -translate-y-1/2 text-fg-4" />
        <Input size="sm" type="search" class="w-64 pl-8" bind:value={search} placeholder="Find a pattern or database" />
      </div>
      {#if report?.supported}<span class="text-xs text-fg-4">{formatNumber(patternRows.length)} patterns</span>{/if}
    </div>
    <div class="flex min-h-0 flex-1 flex-col gap-4 px-5 py-4">
      {#if error}<EmptyState size="compact" icon={AlertTriangle} title="Request failed" description={error} />{/if}
      {#if loading && !report}<div class="flex justify-center py-12"><Spinner /></div>
      {:else if report && !report.supported}<EmptyState icon={AlertTriangle} title="Query log is not available" description={report.coverage} />
      {:else if report}
        <div class="grid shrink-0 grid-cols-3 gap-3"><Stat label="Regressions" value={formatNumber(report.regressions.length)} tone={report.regressions.length ? 'danger' : 'default'} /><Stat label="Patterns compared" value={formatNumber(report.compared)} /><Stat label="Insufficient data" value={formatNumber(report.insufficient)} /></div>
        <Panel variant="muted" padding="sm" class="shrink-0">
          <div class="space-y-1 text-xs leading-relaxed text-fg-3"><p>Current: {windowLabel(report.current)}</p><p>Baseline: {windowLabel(report.baseline)}</p><p>{report.coverage}</p><p>Requires ≥{report.min_samples} successful queries in each window and ≥50% growth, plus ≥100 ms p95 latency or ≥16 MiB mean memory / bytes read. The newest minute is excluded to allow query-log flushing. Refreshes every 5 minutes while this page is visible.</p>{#if report.truncated}<p class="text-warning">Limited to the 1,000 most frequent current patterns. Other patterns have not been evaluated.</p>{/if}</div>
        </Panel>
        <div class="min-h-0 flex-1 overflow-hidden rounded-lg border border-edge-subtle bg-surface">
          <DataTable
            fill
            columns={patternColumns}
            rows={patternRows}
            rowKey={(r) => `${r.database}:${r.hash}`}
            emptyTitle={search ? 'No matching patterns' : allPatterns ? 'No queries recorded in these windows' : 'No qualifying regressions'}
            emptyDescription={report.insufficient ? `${report.insufficient} patterns lack enough successful observations. Use a longer window or inspect all patterns.` : 'Choose another window or return after more workload queries have been recorded.'}
            onrowclick={(r) => selectPattern(r)}
          >
            {#snippet cell(row, col, value)}
              {#if col.key === 'status'}
                <Badge tone={row.status === 'regressed' ? 'danger' : row.status === 'insufficient_data' ? 'warning' : 'neutral'}>{value}</Badge>
              {:else if col.key === 'change'}
                {#if row.status === 'insufficient_data'}<span class="text-fg-3">{value}</span>
                {:else if p95Change(row)?.regressed}<Badge tone="danger">{value}</Badge>
                {:else}<span class="font-mono text-xs text-fg-3">{value}</span>{/if}
              {:else}
                {value}
              {/if}
            {/snippet}
          </DataTable>
        </div>
      {/if}
      {#if monitor}<Panel title="Hourly background detection" description="Compares the last 24 hours with the previous 24 hours on this connection's node, even when this page is closed." class="shrink-0">
        <div class="flex flex-wrap items-center gap-3"><Badge tone={monitor.enabled ? 'brand' : 'neutral'}>{monitor.enabled ? 'Enabled' : 'Disabled'}</Badge>{#if isAdmin}<Button variant="secondary" size="sm" disabled={busy} onclick={toggleMonitor}>{monitor.enabled ? 'Disable hourly scans' : 'Enable hourly scans'}</Button>{/if}<span class="text-xs text-fg-3">Requires a dedicated Performance account under <button type="button" class="text-accent hover:underline" onclick={openBackgroundAccounts}>Admin → Connections → Background accounts</button>.</span></div>
        {#if monitor.last_scan_at}<p class="mt-3 text-xs text-fg-3">Last attempt: {date(monitor.last_scan_at)}</p>{/if}
        {#if monitor.report && monitor.report_at}<p class="mt-1 text-xs text-fg-3">Last successful scan: {date(monitor.report_at)} · {monitor.report.regressions.length} regressions · {monitor.report.insufficient} patterns with insufficient data</p>{/if}
        {#if monitor.last_error}<p class="mt-2 text-xs text-warning">Last scan failed: {monitor.last_error}. Previous results may be stale.</p>{/if}
      </Panel>{/if}
    </div>
  {:else}
    <div class="flex h-10 shrink-0 items-center gap-2 border-b border-edge-subtle px-4">
      <div class="relative">
        <Search size={13} class="pointer-events-none absolute left-2.5 top-1/2 -translate-y-1/2 text-fg-4" />
        <Input size="sm" type="search" class="w-72 pl-8" bind:value={search} placeholder="Find investigations by title, owner or status" />
      </div>
      <span class="ml-auto text-xs text-fg-4">{formatNumber(investigationRows.length)} investigations</span>
    </div>
    {#if error}<div class="shrink-0 px-5 pt-4"><EmptyState size="compact" icon={AlertTriangle} title="Request failed" description={error} /></div>{/if}
    <div class="min-h-0 flex-1">
      {#if loading && investigations.length === 0}<div class="flex justify-center py-12"><Spinner /></div>
      {:else}
        <DataTable
          fill
          columns={investigationColumns}
          rows={investigationRows}
          rowKey={(r) => r.id}
          emptyTitle={search ? 'No matching investigations' : 'No investigations yet'}
          emptyDescription={search ? 'Try another title, owner or status.' : 'Choose a query pattern under Regressions to capture a baseline, record a change and compare its results.'}
          onrowclick={(r) => { if (!busy) void openInvestigation(r.id) }}
        >
          {#snippet cell(row, col, value)}
            {#if col.key === 'status'}<Badge tone={row.status === 'resolved' ? 'success' : 'neutral'}>{value}</Badge>
            {:else if col.key === 'title'}<span class="text-fg">{value}</span>
            {:else}{value}{/if}
          {/snippet}
        </DataTable>
      {/if}
    </div>
  {/if}
</div>

{#if selected}
  <Sheet open={baselineOpen} size="md" title="Capture baseline" description={`${selected.database || 'default'} · ${selected.hash}`} onclose={() => (baselineOpen = false)}>
    <form id="performance-baseline-form" class="space-y-4" onsubmit={(e) => { e.preventDefault(); void save() }}>
      <p class="text-xs leading-relaxed text-fg-3">Capture the current {range} before making a change. A comparison uses a full, equally long window after this baseline. Query literals are normalized. If you rewrite SQL and its normalized hash changes, select the replacement pattern when capturing a comparison.</p>
      <FormField label="Title" for="performance-title" required controlWidth="full"><Input id="performance-title" bind:value={title} disabled={busy} required /></FormField>
      <FormField label="Owner" for="performance-owner" controlWidth="full"><Input id="performance-owner" bind:value={owner} placeholder="Name or email" disabled={busy} /></FormField>
      <FormField label="Initial notes" for="performance-note" controlWidth="full"><Textarea id="performance-note" bind:value={note} disabled={busy} placeholder="What changed, when it was deployed, and what you expect to improve" /></FormField>
      {#if error}<p role="alert" class="text-xs text-danger">{error}</p>{/if}
    </form>
    {#snippet footer()}
      <Button size="sm" variant="ghost" disabled={busy} onclick={() => (baselineOpen = false)}>Cancel</Button>
      <Button size="sm" type="submit" form="performance-baseline-form" disabled={!title.trim() || selectedRuns < 10} loading={busy}>Capture baseline</Button>
    {/snippet}
  </Sheet>
{/if}
