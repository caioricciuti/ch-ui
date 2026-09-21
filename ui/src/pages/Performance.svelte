<!-- SPDX-License-Identifier: BUSL-1.1 -->
<script lang="ts">
  import { onMount } from 'svelte'
  import { RefreshCw, TrendingUp, Search, ArrowLeft, ClipboardList, AlertTriangle } from 'lucide-svelte'
  import {
    fetchRegressions, fetchInvestigations, fetchInvestigation, createInvestigation, updateInvestigation,
    compareInvestigation, fetchPerformanceMonitor, setPerformanceMonitor,
    type PerformanceRange, type PerformanceReport, type Pattern, type Investigation, type InvestigationDetail,
    type Metrics, type Change, type Comparison, type PerformanceMonitor, type Window,
  } from '../lib/api/performance'
  import { getSession } from '../lib/stores/session.svelte'
  import { formatBytes, formatNumber, formatElapsed } from '../lib/utils/format'
  import PageHeader from '../lib/components/common/PageHeader.svelte'
  import PageBody from '../lib/components/common/PageBody.svelte'
  import Button from '../lib/components/common/Button.svelte'
  import Panel from '../lib/components/common/Panel.svelte'
  import Badge from '../lib/components/common/Badge.svelte'
  import Tabs from '../lib/components/common/Tabs.svelte'
  import Stat from '../lib/components/common/Stat.svelte'
  import Spinner from '../lib/components/common/Spinner.svelte'
  import EmptyState from '../lib/components/common/EmptyState.svelte'
  import Input from '../lib/components/common/Input.svelte'
  import Textarea from '../lib/components/common/Textarea.svelte'
  import Select from '../lib/components/common/Select.svelte'

  let section = $state('regressions')
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
  let patternLimit = $state(50)
  let selected = $state<Pattern | null>(null)
  let title = $state('')
  let owner = $state('')
  let note = $state('')
  let status = $state<Investigation['status']>('open')
  let replacement = $state('')
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
    title = `Investigate ${p.database || 'query'} · ${p.hash}`
    owner = getSession()?.user ?? ''
    note = ''
    replacement = ''
    error = ''
  }

  function showDetail(value: InvestigationDetail) {
    detail = value
    selected = null
    title = value.investigation.title
    owner = value.investigation.owner
    status = value.investigation.status
    note = ''
    replacement = ''
    now = Date.now()
    section = 'investigations'
  }

  async function openInvestigation(id: string) {
    const seq = ++detailSequence
    error = ''
    busy = true
    try { const value = await fetchInvestigation(id); if (seq === detailSequence) showDetail(value) }
    catch (e) { if (seq === detailSequence) error = (e as Error).message }
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
    } catch (e) { error = (e as Error).message }
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
    } catch (e) { error = (e as Error).message }
    finally { busy = false }
  }

  async function toggleMonitor() {
    if (!monitor || busy) return
    busy = true
    error = ''
    try { monitor = await setPerformanceMonitor(!monitor.enabled) }
    catch (e) { error = (e as Error).message }
    finally { busy = false }
  }

  onMount(() => {
    void load()
    const timer = setInterval(() => { now = Date.now(); if (!document.hidden && !loading && !busy && !selected && !detail) void load() }, 300000)
    return () => { clearInterval(timer); sequence++; detailSequence++ }
  })
</script>

{#snippet changesTable(changes: Change[], sufficient = true)}
  <div class="overflow-x-auto">
    <table class="w-full text-left text-xs">
      <thead class="border-b border-edge-subtle text-fg-3"><tr><th class="py-2 font-medium">Metric</th><th class="py-2 text-right font-medium">Baseline</th><th class="py-2 text-right font-medium">Current</th><th class="py-2 text-right font-medium">Change</th></tr></thead>
      <tbody>{#each changes as change}
        <tr class="border-b border-edge-subtle last:border-0"><td class="py-2 text-fg-2">{metricNames[change.metric]}</td><td class="py-2 text-right font-mono text-fg-3">{metricValue(change.metric, change.before)}</td><td class="py-2 text-right font-mono">{metricValue(change.metric, change.after)}</td><td class="py-2 text-right font-mono {sufficient && change.regressed ? 'text-danger' : 'text-fg-3'}">{sufficient ? delta(change) : 'Insufficient data'}</td></tr>
      {/each}</tbody>
    </table>
  </div>
{/snippet}

<div class="flex h-full min-h-0 flex-col">
  <PageHeader title="Performance" subtitle="Detect regressions and verify improvements">
    {#snippet actions()}
      <Tabs variant="segmented" size="sm" items={[{id:'regressions',label:'Regressions'},{id:'investigations',label:'Investigations'}]} value={section}
        onchange={(value) => { if (busy) return; section = value; detail = null; selected = null; search = ''; detailSequence++ }} />
      <Button variant="ghost" size="sm" disabled={loading || busy} onclick={() => load()}><RefreshCw size={14} class={loading ? 'animate-spin' : ''} /> Refresh</Button>
    {/snippet}
  </PageHeader>
  <PageBody width="lg">
    <div class="space-y-4">
      {#if error}<div role="alert" class="rounded-lg border border-danger/30 bg-danger/5 px-4 py-3 text-sm text-danger">{error}</div>{/if}

      {#if selected || detail}
        <Button variant="ghost" size="sm" disabled={busy} onclick={() => { selected = null; detail = null; detailSequence++ }}><ArrowLeft size={14} /> Back</Button>
        <Panel title={selected ? 'Capture a performance baseline' : detail?.investigation.title} description="Saved measurements and notes are shared with users of this connection.">
          <pre class="mb-4 max-h-40 overflow-auto whitespace-pre-wrap rounded-md bg-surface-2 p-3 text-xs text-fg-3">{selected?.sample_query ?? detail?.investigation.sample_query}</pre>
          {#if selected}
            <p class="mb-4 text-xs leading-relaxed text-fg-3">Capture the current {range} before making a change. A comparison uses a full, equally long window after this baseline. Query literals are normalized. If you rewrite SQL and its normalized hash changes, select the replacement pattern when capturing a comparison.</p>
          {/if}
          <div class="grid gap-4 md:grid-cols-2">
            <div><label for="performance-title" class="mb-1 block text-xs text-fg-3">Title</label><Input id="performance-title" bind:value={title} disabled={!canWrite || busy} /></div>
            <div><label for="performance-owner" class="mb-1 block text-xs text-fg-3">Owner</label><Input id="performance-owner" bind:value={owner} placeholder="Name or email" disabled={!canWrite || busy} /></div>
            {#if detail}<div><label for="performance-status" class="mb-1 block text-xs text-fg-3">Status</label><Select id="performance-status" value={status} onchange={v => status = v as Investigation['status']} disabled={!canWrite || busy} options={[{value:'open',label:'Open'},{value:'monitoring',label:'Monitoring a change'},{value:'resolved',label:'Resolved'}]} /></div>{/if}
          </div>
          {#if canWrite}
            <div class="mt-4"><label for="performance-note" class="mb-1 block text-xs text-fg-3">{selected ? 'Initial notes' : 'Add a note or record a change'}</label><Textarea id="performance-note" bind:value={note} disabled={busy} placeholder="What changed, when it was deployed, and what you expect to improve" /></div>
            {#if detail}<div class="mt-4"><label for="performance-replacement" class="mb-1 block text-xs text-fg-3">Compare against (choose a replacement if the SQL changed)</label><Select id="performance-replacement" options={replacementOptions} bind:value={replacement} disabled={busy} /><p class="mt-1 text-xs text-fg-4">Replacement choices come from the latest {range} scan. Compare equivalent workloads and wait a full baseline window after your deployment so the comparison excludes pre-change workload.</p></div>{/if}
            <div class="mt-4 flex flex-wrap items-center gap-3">
              <Button disabled={busy || !title.trim()} onclick={save}>{selected ? 'Capture baseline' : 'Save update'}</Button>
              {#if detail}<Button variant="secondary" disabled={busy || !comparisonReady || !!note.trim()} onclick={compare}>Capture comparison</Button>{/if}
              {#if detail && !comparisonReady}<span class="text-xs text-fg-3">Full comparison available {new Date(comparisonReadyAt).toLocaleString()}</span>{/if}
              {#if detail && note.trim()}<span class="text-xs text-fg-3">Save your note before capturing a comparison.</span>{/if}
            </div>
          {/if}
        </Panel>

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
      {:else if section === 'regressions'}
        <div class="flex flex-wrap items-end gap-3">
          <div><label for="performance-range" class="mb-1 block text-xs text-fg-3">Compare each window</label><Select id="performance-range" class="w-36" value={range} options={rangeOptions} onchange={v => { range = v as PerformanceRange; void load() }} disabled={loading} /></div>
          <label class="flex h-8 items-center gap-2 text-xs text-fg-3"><input type="checkbox" bind:checked={allPatterns} /> All observed patterns</label>
          <div class="ml-auto flex items-center gap-2"><Search size={14} class="text-fg-4" /><Input type="search" bind:value={search} placeholder="Find a pattern or database" /></div>
        </div>
        {#if loading && !report}<div class="flex justify-center py-12"><Spinner /></div>
        {:else if report && !report.supported}<EmptyState icon={AlertTriangle} title="Query log is not available" description={report.coverage} />
        {:else if report}
          <div class="grid grid-cols-3 gap-3"><Stat label="Regressions" value={formatNumber(report.regressions.length)} tone={report.regressions.length ? 'danger' : 'default'} /><Stat label="Patterns compared" value={formatNumber(report.compared)} /><Stat label="Insufficient data" value={formatNumber(report.insufficient)} /></div>
          <Panel variant="muted" padding="sm">
            <div class="space-y-1 text-xs leading-relaxed text-fg-3"><p>Current: {windowLabel(report.current)}</p><p>Baseline: {windowLabel(report.baseline)}</p><p>{report.coverage}</p><p>Requires ≥{report.min_samples} successful queries in each window and ≥50% growth, plus ≥100 ms p95 latency or ≥16 MiB mean memory / bytes read. The newest minute is excluded to allow query-log flushing. Refreshes every 5 minutes while this page is visible.</p>{#if report.truncated}<p class="text-warning">Limited to the 1,000 most frequent current patterns. Other patterns have not been evaluated.</p>{/if}</div>
          </Panel>
          {#if visiblePatterns.length === 0}<EmptyState icon={TrendingUp} title={search ? 'No matching patterns' : allPatterns ? 'No queries recorded in these windows' : 'No qualifying regressions'} description={report.insufficient ? `${report.insufficient} patterns lack enough successful observations. Use a longer window or inspect all patterns.` : 'Choose another window or return after more workload queries have been recorded.'} />
          {:else}<div class="space-y-3">
            {#each visiblePatterns.slice(0,patternLimit) as p (`${p.database}:${p.hash}`)}
              <Panel padding="sm">
                <div class="mb-3 flex items-center gap-2"><Badge tone={p.status === 'regressed' ? 'danger' : p.status === 'insufficient_data' ? 'warning' : 'neutral'}>{p.status === 'insufficient_data' ? 'Insufficient data' : p.status === 'regressed' ? 'Regression' : 'Stable'}</Badge><span class="truncate font-mono text-xs text-fg-3">{p.database || 'default'} · {p.hash}</span>{#if canWrite}<Button size="xs" variant="secondary" class="ml-auto shrink-0" disabled={p.current.runs < 10} onclick={() => selectPattern(p)}>Investigate</Button>{/if}</div>
                <pre class="mb-2 max-h-24 overflow-auto whitespace-pre-wrap text-xs text-fg-2">{p.sample_query}</pre>
                {@render changesTable(p.changes.filter(c => ['p95_ms', 'mean_memory_bytes', 'mean_read_bytes'].includes(c.metric)), p.status !== 'insufficient_data')}
                <p class="mt-2 text-xs text-fg-4">Successful queries: {formatNumber(p.baseline.runs)} → {formatNumber(p.current.runs)} · Failures: {formatNumber(p.baseline.failures)} → {formatNumber(p.current.failures)}{#if p.extra_duration_ms > 0} · Additional aggregate duration: {ms(p.extra_duration_ms)}{/if}</p>
              </Panel>
            {/each}
            {#if visiblePatterns.length > patternLimit}<div class="flex justify-center"><Button variant="secondary" size="sm" onclick={() => patternLimit += 50}>Show more patterns ({visiblePatterns.length - patternLimit} remaining)</Button></div>{/if}
          </div>{/if}
        {/if}
        {#if monitor}<Panel title="Hourly background detection" description="Compares the last 24 hours with the previous 24 hours on this connection's node, even when this page is closed.">
          <div class="flex flex-wrap items-center gap-3"><Badge tone={monitor.enabled ? 'brand' : 'neutral'}>{monitor.enabled ? 'Enabled' : 'Disabled'}</Badge>{#if isAdmin}<Button variant="secondary" size="sm" disabled={busy} onclick={toggleMonitor}>{monitor.enabled ? 'Disable hourly scans' : 'Enable hourly scans'}</Button>{/if}<span class="text-xs text-fg-3">Requires a dedicated Performance account under Connections → Background accounts.</span></div>
          {#if monitor.last_scan_at}<p class="mt-3 text-xs text-fg-3">Last attempt: {date(monitor.last_scan_at)}</p>{/if}
          {#if monitor.report && monitor.report_at}<p class="mt-1 text-xs text-fg-3">Last successful scan: {date(monitor.report_at)} · {monitor.report.regressions.length} regressions · {monitor.report.insufficient} patterns with insufficient data</p>{/if}
          {#if monitor.last_error}<p class="mt-2 text-xs text-warning">Last scan failed: {monitor.last_error}. Previous results may be stale.</p>{/if}
        </Panel>{/if}
      {:else}
        <Input type="search" bind:value={search} placeholder="Find investigations by title, owner or status" />
        {#if loading && investigations.length === 0}<div class="flex justify-center py-12"><Spinner /></div>
        {:else if filteredInvestigations.length === 0}<EmptyState icon={ClipboardList} title="No investigations yet" description="Choose Investigate on a query pattern to capture a baseline, record a change and compare its results." />
        {:else}<div class="space-y-2">{#each filteredInvestigations as item (item.id)}
          <button class="flex w-full items-center gap-3 rounded-lg border border-edge-subtle bg-surface p-4 text-left hover:border-edge-strong disabled:opacity-50" disabled={busy} onclick={() => openInvestigation(item.id)}><ClipboardList size={18} class="shrink-0 text-fg-3" /><span class="min-w-0 flex-1"><span class="block truncate text-sm font-medium text-fg">{item.title}</span><span class="mt-1 block text-xs text-fg-3">{item.owner || 'Unassigned'} · {item.database || 'default'} · Updated {date(item.updated_at)}</span></span><Badge tone={item.status === 'resolved' ? 'success' : 'neutral'}>{item.status}</Badge></button>
        {/each}</div>{/if}
      {/if}
    </div>
  </PageBody>
</div>
