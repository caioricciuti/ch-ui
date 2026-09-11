<script lang="ts">
  import { onMount, onDestroy } from 'svelte'
  import {
    HeartPulse,
    RefreshCw,
    Settings2,
    AlertTriangle,
    Server,
    GitBranch,
    Layers,
    Wrench,
    Database,
    Timer,
    Save,
    HardDrive,
    Network,
    Archive,
    Search,
    X,
    Funnel,
  } from 'lucide-svelte'
  import {
    fetchSummary,
    fetchLive,
    fetchHistory,
    fetchSettings,
    saveSettings,
    type HealthSummary,
    type NodeSample,
    type LiveResult,
    type LiveSection,
    type ClusterHealthSettings,
  } from '../lib/api/clusterHealth'
  import TrendChart from '../lib/components/common/TrendChart.svelte'
  import DataTable, { type DataColumn } from '../lib/components/common/DataTable.svelte'
  import Sheet from '../lib/components/common/Sheet.svelte'
  import PageHeader from '../lib/components/common/PageHeader.svelte'
  import PageBody from '../lib/components/common/PageBody.svelte'
  import Button from '../lib/components/common/Button.svelte'
  import Badge from '../lib/components/common/Badge.svelte'
  import Tabs from '../lib/components/common/Tabs.svelte'
  import Stat from '../lib/components/common/Stat.svelte'
  import Panel from '../lib/components/common/Panel.svelte'
  import EmptyState from '../lib/components/common/EmptyState.svelte'
  import Spinner from '../lib/components/common/Spinner.svelte'
  import FormField from '../lib/components/common/FormField.svelte'
  import Input from '../lib/components/common/Input.svelte'
  import Select from '../lib/components/common/Select.svelte'
  import { success as toastSuccess, error as toastError } from '../lib/stores/toast.svelte'
  import { getSection } from '../lib/stores/nav.svelte'
  import { PAGE_SECTIONS } from '../lib/routes'

  // ── State ──────────────────────────────────────────────────────────────────
  let summary = $state<HealthSummary | null>(null)
  let loading = $state(true)
  let error = $state<string | null>(null)
  let lastUpdated = $state<Date | null>(null)
  let refreshing = $state(false)

  let history = $state<NodeSample[]>([])
  let settings = $state<ClusterHealthSettings | null>(null)
  let showSettings = $state(false)
  let savingSettings = $state(false)
  let settingsForm = $state<ClusterHealthSettings | null>(null)

  // Sections come from the sidebar (?section=); 'overview' is the summary,
  // everything else is one live system table.
  type SectionId = 'overview' | LiveSection
  const SECTION_META = PAGE_SECTIONS['cluster-health'] ?? []
  const activeSection = $derived.by<SectionId>(() => {
    const s = getSection()
    return s && SECTION_META.some((m) => m.id === s) ? (s as SectionId) : 'overview'
  })
  const activeLabel = $derived(SECTION_META.find((m) => m.id === activeSection)?.label ?? 'Overview')

  let sectionResult = $state<LiveResult | null>(null)
  let loadedSection = $state<LiveSection | null>(null)
  let sectionLoading = $state(false)

  // ── Filters (client-side: section rows are already loaded and capped) ──────
  let nodeFilter = $state<string | null>(null)
  let searchFilter = $state('')
  let historyRange = $state<'1h' | '6h' | '24h' | '7d'>('6h')
  const HISTORY_RANGES: Array<'1h' | '6h' | '24h' | '7d'> = ['1h', '6h', '24h', '7d']

  function rowMatchesFilters(row: Record<string, unknown>): boolean {
    if (nodeFilter && String(row.node ?? '') !== nodeFilter) return false
    const q = searchFilter.trim().toLowerCase()
    if (!q) return true
    return Object.values(row).some((v) => v != null && String(v).toLowerCase().includes(q))
  }

  const UI_REFRESH_MS = 20000
  let timer: ReturnType<typeof setInterval> | null = null

  // ── Section definitions ──────────────────────────────────────────────────
  interface ColumnSpec {
    key: string
    label: string
    mono?: boolean
  }
  interface SectionSpec {
    key: LiveSection
    label: string
    icon: typeof HeartPulse
    columns: ColumnSpec[]
    empty: string
  }

  const SECTIONS: SectionSpec[] = [
    {
      key: 'replication',
      label: 'Replication',
      icon: GitBranch,
      empty: 'No replicated tables found.',
      columns: [
        { key: 'node', label: 'Node', mono: true },
        { key: 'database', label: 'Database', mono: true },
        { key: 'table', label: 'Table', mono: true },
        { key: 'absolute_delay', label: 'Delay (s)' },
        { key: 'queue_size', label: 'Queue' },
        { key: 'inserts_in_queue', label: 'Inserts' },
        { key: 'merges_in_queue', label: 'Merges' },
        { key: 'is_readonly', label: 'Readonly' },
        { key: 'active_replicas', label: 'Active' },
        { key: 'total_replicas', label: 'Total' },
      ],
    },
    {
      key: 'replication-queue',
      label: 'Queue Issues',
      icon: AlertTriangle,
      empty: 'No stalled or failing replication tasks.',
      columns: [
        { key: 'node', label: 'Node', mono: true },
        { key: 'database', label: 'Database', mono: true },
        { key: 'table', label: 'Table', mono: true },
        { key: 'type', label: 'Type' },
        { key: 'num_tries', label: 'Tries' },
        { key: 'num_postponed', label: 'Postponed' },
        { key: 'postpone_reason', label: 'Reason', mono: true },
        { key: 'last_exception', label: 'Exception', mono: true },
      ],
    },
    {
      key: 'merges',
      label: 'Merges',
      icon: Layers,
      empty: 'No merges currently running.',
      columns: [
        { key: 'node', label: 'Node', mono: true },
        { key: 'database', label: 'Database', mono: true },
        { key: 'table', label: 'Table', mono: true },
        { key: 'elapsed', label: 'Elapsed (s)' },
        { key: 'progress', label: 'Progress' },
        { key: 'is_mutation', label: 'Mutation' },
        { key: 'num_parts', label: 'Parts' },
        { key: 'memory', label: 'Memory', mono: true },
      ],
    },
    {
      key: 'mutations',
      label: 'Mutations',
      icon: Wrench,
      empty: 'No unfinished mutations.',
      columns: [
        { key: 'node', label: 'Node', mono: true },
        { key: 'database', label: 'Database', mono: true },
        { key: 'table', label: 'Table', mono: true },
        { key: 'mutation_id', label: 'ID', mono: true },
        { key: 'parts_to_do', label: 'Parts left' },
        { key: 'create_time', label: 'Created', mono: true },
        { key: 'latest_fail_reason', label: 'Fail reason', mono: true },
      ],
    },
    {
      key: 'long-queries',
      label: 'Long Queries',
      icon: Timer,
      empty: 'No long-running queries.',
      columns: [
        { key: 'node', label: 'Node', mono: true },
        { key: 'user', label: 'User', mono: true },
        { key: 'elapsed', label: 'Elapsed (s)' },
        { key: 'memory', label: 'Memory', mono: true },
        { key: 'read_rows', label: 'Read rows' },
        { key: 'query', label: 'Query', mono: true },
      ],
    },
    {
      key: 'parts',
      label: 'Parts Pressure',
      icon: Database,
      empty: 'No partitions with multiple active parts.',
      columns: [
        { key: 'node', label: 'Node', mono: true },
        { key: 'database', label: 'Database', mono: true },
        { key: 'table', label: 'Table', mono: true },
        { key: 'partition', label: 'Partition', mono: true },
        { key: 'parts', label: 'Active parts' },
        { key: 'rows', label: 'Rows' },
        { key: 'size', label: 'Size', mono: true },
      ],
    },
    {
      key: 'disks',
      label: 'Data Location',
      icon: HardDrive,
      empty: 'No disks reported.',
      columns: [
        { key: 'node', label: 'Node', mono: true },
        { key: 'name', label: 'Disk', mono: true },
        { key: 'type', label: 'Type' },
        { key: 'free', label: 'Free', mono: true },
        { key: 'total', label: 'Total', mono: true },
        { key: 'used_pct', label: 'Used %' },
      ],
    },
    {
      key: 'keeper',
      label: 'Keeper',
      icon: Network,
      empty: 'No ZooKeeper/Keeper connection reported.',
      columns: [
        { key: 'node', label: 'Node', mono: true },
        { key: 'name', label: 'Name', mono: true },
        { key: 'host', label: 'Host', mono: true },
        { key: 'port', label: 'Port' },
        { key: 'session_uptime_elapsed_seconds', label: 'Uptime (s)' },
        { key: 'is_expired', label: 'Expired' },
      ],
    },
    {
      key: 'backups',
      label: 'Backups',
      icon: Archive,
      empty: 'No backup/restore operations recorded.',
      columns: [
        { key: 'node', label: 'Node', mono: true },
        { key: 'name', label: 'Name', mono: true },
        { key: 'status', label: 'Status' },
        { key: 'start_time', label: 'Started', mono: true },
        { key: 'end_time', label: 'Ended', mono: true },
        { key: 'total_size', label: 'Size', mono: true },
        { key: 'error', label: 'Error', mono: true },
      ],
    },
  ]

  // Parts pressure is usually a fraction of a percent on a healthy cluster;
  // rounding to whole numbers would show 0% for every point.
  function fmtPct(v: number): string {
    if (v === 0) return '0%'
    if (Math.abs(v) < 1) return `${v.toFixed(2)}%`
    if (Math.abs(v) < 10) return `${v.toFixed(1)}%`
    return `${Math.round(v)}%`
  }
  function fmtSeconds(v: number): string {
    if (v === 0) return '0s'
    if (Math.abs(v) < 1) return `${v.toFixed(2)}s`
    if (Math.abs(v) < 10) return `${v.toFixed(1)}s`
    return `${Math.round(v)}s`
  }

  const activeSpec = $derived(SECTIONS.find((sp) => sp.key === activeSection) ?? SECTIONS[0])

  // Counts, sizes and durations read better right-aligned.
  const NUMERIC_KEYS = new Set([
    'absolute_delay', 'queue_size', 'inserts_in_queue', 'merges_in_queue', 'active_replicas', 'total_replicas',
    'num_tries', 'num_postponed', 'elapsed', 'progress', 'num_parts', 'memory', 'parts_to_do', 'read_rows',
    'parts', 'rows', 'size', 'free', 'total', 'used_pct', 'port', 'session_uptime_elapsed_seconds', 'total_size',
  ])
  function toDataColumns(spec: SectionSpec): DataColumn<Record<string, unknown>>[] {
    return spec.columns.map((c) => ({
      key: c.key,
      label: c.label,
      mono: c.mono,
      truncate: c.mono,
      align: NUMERIC_KEYS.has(c.key) ? 'right' : 'left',
      format: (v) => (v === null || v === undefined || v === '' ? '—' : String(v)),
    }))
  }
  const sectionColumns = $derived(toDataColumns(activeSpec))
  const sectionRows = $derived((sectionResult?.data ?? []).filter(rowMatchesFilters))


  // ── Derived aggregates ─────────────────────────────────────────────────────
  const nodes = $derived(summary?.nodes ?? [])
  // Tiles, the node table and the trend charts all respect the node filter, so
  // funnelling a node shows that node's health at a glance.
  const filteredNodes = $derived(nodeFilter ? nodes.filter((n) => n.node === nodeFilter) : nodes)
  const agg = $derived.by(() => {
    const list = filteredNodes
    const max = (f: (n: NodeSample) => number) => list.reduce((m, n) => Math.max(m, f(n) || 0), 0)
    const sum = (f: (n: NodeSample) => number) => list.reduce((m, n) => m + (f(n) || 0), 0)
    return {
      maxDelay: max(n => n.replication_max_delay),
      totalQueue: sum(n => n.replication_queue_total),
      readonly: sum(n => n.replicas_readonly),
      merges: sum(n => n.merges_running),
      mutations: sum(n => n.mutations_pending),
      partsPressure: max(n => n.parts_pressure_pct),
      partsMax: max(n => n.parts_max_active),
      longQueries: sum(n => n.long_queries),
    }
  })

  // Nodes table on the overview. NodeSample is an interface, so spread it
  // into a plain record for the generic table.
  const nodeRows = $derived(filteredNodes.map((n) => ({ ...n })))
  const NODE_COLUMNS: DataColumn<Record<string, unknown>>[] = [
    { key: 'node', label: 'Node', mono: true },
    { key: 'replication_max_delay', label: 'Repl. delay (s)', align: 'right' },
    { key: 'replication_queue_total', label: 'Queue', align: 'right', format: fmtNum },
    { key: 'replicas_readonly', label: 'Readonly', align: 'right' },
    { key: 'merges_running', label: 'Merges', align: 'right', format: fmtNum },
    { key: 'mutations_pending', label: 'Mutations', align: 'right', format: fmtNum },
    { key: 'parts_max_active', label: 'Max parts', align: 'right', format: fmtNum },
    { key: 'long_queries', label: 'Long queries', align: 'right', format: fmtNum },
  ]

  // ── Status thresholds ──────────────────────────────────────────────────────
  type Status = 'ok' | 'warn' | 'crit'
  function band(value: number, warn: number, crit: number): Status {
    if (value >= crit) return 'crit'
    if (value >= warn) return 'warn'
    return 'ok'
  }
  function badgeTone(s: Status): 'danger' | 'warning' | 'success' {
    return s === 'crit' ? 'danger' : s === 'warn' ? 'warning' : 'success'
  }
  function statTone(s: Status): 'danger' | 'warning' | 'default' {
    return s === 'crit' ? 'danger' : s === 'warn' ? 'warning' : 'default'
  }

  const rangeItems = HISTORY_RANGES.map((r) => ({ id: r, label: r }))
  const nodeOptions = $derived([
    { value: '', label: 'All nodes' },
    ...nodes.map((n) => ({ value: n.node, label: n.node })),
  ])

  // ── Trend series (one series per node, aligned on captured_at) ─────────────
  const NODE_COLORS = ['#f97316', '#0ea5e9', '#10b981', '#a855f7', '#f59e0b', '#ec4899', '#84cc16', '#ef4444']

  function buildNodeSeries(metric: (n: NodeSample) => number) {
    const samples = nodeFilter ? history.filter((s) => s.node === nodeFilter) : history
    const times = Array.from(new Set(samples.map((s) => s.captured_at))).sort()
    const timeIdx = new Map(times.map((t, i) => [t, i]))
    const nodeNames = Array.from(new Set(samples.map((s) => s.node))).sort()

    const series = nodeNames.map((name, ni) => {
      const values: (number | null)[] = times.map(() => null)
      for (const s of samples) {
        if (s.node !== name) continue
        const idx = timeIdx.get(s.captured_at)
        if (idx !== undefined) values[idx] = metric(s) || 0
      }
      return {
        label: name,
        values,
        color: NODE_COLORS[ni % NODE_COLORS.length],
        fill: nodeNames.length === 1 ? `${NODE_COLORS[ni % NODE_COLORS.length]}24` : undefined,
      }
    })

    return {
      x: times.map((t) => Math.floor(Date.parse(t) / 1000)),
      series,
    }
  }
  const delayChart = $derived(buildNodeSeries((n) => n.replication_max_delay))
  const partsChart = $derived(buildNodeSeries((n) => n.parts_pressure_pct))

  // ── Formatting ─────────────────────────────────────────────────────────────
  function fmtNum(v: unknown): string {
    const n = typeof v === 'number' ? v : Number(v)
    if (!isFinite(n)) return String(v ?? '—')
    return n.toLocaleString()
  }
  // ── Data loading ───────────────────────────────────────────────────────────
  async function loadSummary(initial = false) {
    if (initial) loading = true
    refreshing = !initial
    try {
      summary = await fetchSummary()
      lastUpdated = new Date()
      error = null
    } catch (e: any) {
      error = e?.message ?? 'Failed to load cluster health'
    } finally {
      loading = false
      refreshing = false
    }
  }

  async function loadHistory() {
    try {
      const res = await fetchHistory(historyRange)
      history = res.data ?? []
    } catch {
      // history is best-effort; ignore failures
    }
  }

  function setHistoryRange(r: '1h' | '6h' | '24h' | '7d') {
    if (historyRange === r) return
    historyRange = r
    void loadHistory()
  }

  async function loadSection(section: LiveSection) {
    // Switching sections shows a spinner; the periodic refresh of the same
    // section swaps rows in place so the table does not flicker.
    if (loadedSection !== section) {
      sectionResult = null
      sectionLoading = true
    }
    loadedSection = section
    try {
      const res = await fetchLive(section)
      if (loadedSection === section) sectionResult = res
    } catch (e: any) {
      toastError(e?.message ?? 'Failed to load section')
    } finally {
      if (loadedSection === section) sectionLoading = false
    }
  }

  // Follow the sidebar: load a live section when it becomes active.
  $effect(() => {
    const section = activeSection
    if (section === 'overview' || section === loadedSection) return
    void loadSection(section)
  })

  async function loadSettings() {
    try {
      settings = await fetchSettings()
    } catch {
      // non-fatal
    }
  }

  function openSettings() {
    settingsForm = settings ? { ...settings } : null
    showSettings = true
  }

  async function persistSettings() {
    if (!settingsForm) return
    savingSettings = true
    try {
      settings = await saveSettings({
        enabled: settingsForm.enabled,
        retention_days: settingsForm.retention_days,
        poll_interval_seconds: settingsForm.poll_interval_seconds,
        long_query_threshold_seconds: settingsForm.long_query_threshold_seconds,
      })
      toastSuccess('Cluster health settings saved')
      showSettings = false
      await loadSummary()
    } catch (e: any) {
      toastError(e?.message ?? 'Failed to save settings')
    } finally {
      savingSettings = false
    }
  }

  function startPolling() {
    stopPolling()
    timer = setInterval(() => {
      if (document.visibilityState === 'visible') {
        loadSummary()
        if (activeSection !== 'overview') loadSection(activeSection)
      }
    }, UI_REFRESH_MS)
  }
  function stopPolling() {
    if (timer) {
      clearInterval(timer)
      timer = null
    }
  }

  onMount(async () => {
    await Promise.all([loadSummary(true), loadSettings(), loadHistory()])
    startPolling()
  })
  onDestroy(stopPolling)
</script>

<div class="flex h-full min-h-0 flex-col">
  <PageHeader title="Cluster Health" subtitle={activeLabel}>
    {#snippet meta()}
      {#if summary}
        <Badge tone="neutral">
          <Server size={11} />
          {#if summary.is_cluster}
            {summary.cluster} · {nodes.length} node{nodes.length === 1 ? '' : 's'}
          {:else}
            Single node
          {/if}
        </Badge>
      {/if}
      {#if summary?.degraded}
        <Badge tone="warning" title="Some nodes could not be reached; showing local node only">
          <AlertTriangle size={11} /> Degraded
        </Badge>
      {/if}
    {/snippet}
    {#snippet actions()}
      <Tabs variant="segmented" size="sm" items={rangeItems} value={historyRange} onchange={(id) => setHistoryRange(id as typeof historyRange)} />
      <Button icon variant="ghost" size="sm" aria-label="Refresh" title="Refresh" onclick={() => loadSummary()}>
        <RefreshCw size={14} class={refreshing ? 'animate-spin' : ''} />
      </Button>
      <Button variant="outline" size="sm" onclick={openSettings}>
        <Settings2 size={13} /> Settings
      </Button>
    {/snippet}
  </PageHeader>

  {#if loading}
    <div class="flex flex-1 items-center justify-center"><Spinner /></div>
  {:else if error}
    <EmptyState icon={AlertTriangle} title="Couldn't load cluster health" description={error} primary={{ label: 'Retry', onclick: () => loadSummary(true) }} />
  {:else if summary}
    {#if activeSection === 'overview'}
      {@const delayStatus = band(agg.maxDelay, 10, 60)}
      {@const queueStatus = band(agg.totalQueue, 10, 100)}
      {@const roStatus = agg.readonly > 0 ? 'crit' : 'ok'}
      {@const partsStatus = band(agg.partsPressure, 50, 80)}
      {@const mutStatus = band(agg.mutations, 1, 5)}
      {@const lqStatus = band(agg.longQueries, 1, 5)}
      <PageBody width="lg">
        <div class="space-y-5">
          {#if nodeFilter}
            <div class="flex items-center gap-2 text-xs text-fg-3">
              Showing
              <Badge tone="brand" class="pr-0.5">
                <Server size={10} class="shrink-0" />
                <span class="font-mono">{nodeFilter}</span>
                <button class="rounded-sm p-0.5 hover:bg-accent-soft" onclick={() => (nodeFilter = null)} aria-label="Show all nodes">
                  <X size={10} />
                </button>
              </Badge>
            </div>
          {/if}

          <!-- Headline tiles -->
          <div class="grid grid-cols-2 gap-3 md:grid-cols-3 xl:grid-cols-6">
            <Stat label="Max repl. delay" value={`${agg.maxDelay} s`} tone={statTone(delayStatus)} />
            <Stat label="Repl. queue" value={fmtNum(agg.totalQueue)} tone={statTone(queueStatus)} />
            <Stat label="Readonly replicas" value={agg.readonly} tone={statTone(roStatus)} />
            <Stat label="Parts pressure" value={fmtPct(agg.partsPressure)} tone={statTone(partsStatus)} hint={`${fmtNum(agg.partsMax)} / ${fmtNum(summary.parts_limits?.parts_to_throw_insert)} parts`} />
            <Stat label="Mutations pending" value={fmtNum(agg.mutations)} tone={statTone(mutStatus)} hint={`${fmtNum(agg.merges)} merges running`} />
            <Stat label="Long queries" value={fmtNum(agg.longQueries)} tone={statTone(lqStatus)} hint={`> ${summary.threshold_seconds}s`} />
          </div>

          <!-- Trends (one series per node) -->
          <div class="grid grid-cols-1 gap-3 md:grid-cols-2">
            <Panel title="Replication delay" description={`Last ${historyRange}`} padding="sm">
              {#if delayChart.x.length > 1}
                <TrendChart x={delayChart.x} series={delayChart.series} height={150} formatY={fmtSeconds} />
              {:else}
                <div class="grid h-[150px] place-items-center text-xs text-fg-4">Not enough history yet. Samples arrive as the harvester polls.</div>
              {/if}
            </Panel>
            <Panel title="Parts pressure" description={`Last ${historyRange}`} padding="sm">
              {#if partsChart.x.length > 1}
                <TrendChart x={partsChart.x} series={partsChart.series} height={150} formatY={fmtPct} />
              {:else}
                <div class="grid h-[150px] place-items-center text-xs text-fg-4">Not enough history yet. Samples arrive as the harvester polls.</div>
              {/if}
            </Panel>
          </div>

          <!-- Per-node table -->
          {#if nodeRows.length > 0}
            <DataTable columns={NODE_COLUMNS} rows={nodeRows} rowKey={(r) => String(r.node)} emptyTitle="No nodes reported">
              {#snippet cell(row, col, value)}
                {#if col.key === 'replication_max_delay'}
                  <Badge tone={badgeTone(band(Number(row.replication_max_delay) || 0, 10, 60))}>{value}</Badge>
                {:else if col.key === 'replicas_readonly'}
                  {#if Number(row.replicas_readonly) > 0}<Badge tone="danger">{value}</Badge>{:else}0{/if}
                {:else}
                  {value}
                {/if}
              {/snippet}
              {#snippet actions(row)}
                {#if nodes.length > 1 && !nodeFilter}
                  <Button icon variant="ghost" size="xs" onclick={() => (nodeFilter = String(row.node))} title="Filter the page by this node" aria-label={`Filter by node ${row.node}`}>
                    <Funnel size={12} />
                  </Button>
                {/if}
              {/snippet}
            </DataTable>
          {/if}

          {#if lastUpdated}
            <div class="text-right text-[11px] text-fg-4">
              Updated {lastUpdated.toLocaleTimeString()} · auto-refresh every {UI_REFRESH_MS / 1000}s
            </div>
          {/if}
        </div>
      </PageBody>
    {:else}
      <!-- One live system table, filling the viewport -->
      <div class="flex min-h-0 flex-1 flex-col">
        <div class="flex h-10 shrink-0 items-center gap-2 px-5">
          <div class="relative">
            <Search size={13} class="pointer-events-none absolute left-2.5 top-1/2 -translate-y-1/2 text-fg-4" />
            <Input size="sm" type="search" class="w-64 pl-8 pr-7" placeholder="Filter rows" bind:value={searchFilter} spellcheck={false} />
            {#if searchFilter}
              <button class="absolute right-2 top-1/2 -translate-y-1/2 text-fg-4 hover:text-fg" onclick={() => (searchFilter = '')} aria-label="Clear search">
                <X size={13} />
              </button>
            {/if}
          </div>
          {#if nodes.length > 1}
            <Select size="sm" class="w-44" options={nodeOptions} value={nodeFilter ?? ''} onchange={(v) => (nodeFilter = v || null)} />
          {/if}
          {#if nodeFilter}
            <Badge tone="brand" class="pr-0.5">
              <Server size={10} class="shrink-0" />
              <span class="font-mono">{nodeFilter}</span>
              <button class="rounded-sm p-0.5 hover:bg-accent-soft" onclick={() => (nodeFilter = null)} aria-label="Remove node filter">
                <X size={10} />
              </button>
            </Badge>
          {/if}
          <div class="flex-1"></div>
          {#if sectionResult?.degraded}
            <span class="inline-flex items-center gap-1 text-[11px] text-warning">
              <AlertTriangle size={12} /> Local node only, remote nodes unreachable
            </span>
          {/if}
          {#if sectionResult?.supported}
            <span class="text-[11px] tabular-nums text-fg-4">
              {sectionRows.length === sectionResult.data.length ? `${sectionRows.length} rows` : `${sectionRows.length} of ${sectionResult.data.length} rows`}
            </span>
          {/if}
        </div>

        <div class="min-h-0 flex-1 px-5 pb-4">
          {#if sectionLoading && !sectionResult}
            <div class="flex h-full items-center justify-center"><Spinner /></div>
          {:else if sectionResult && !sectionResult.supported}
            <EmptyState icon={AlertTriangle} title={`${activeSpec.label} is not available on this ClickHouse deployment`} />
          {:else if sectionResult}
            <div class="h-full overflow-hidden rounded-lg border border-edge-subtle bg-surface">
              <DataTable
                fill
                columns={sectionColumns}
                rows={sectionRows}
                emptyTitle={sectionResult.data.length === 0 ? activeSpec.empty : 'No rows match the current filters'}
              />
            </div>
          {/if}
        </div>
      </div>
    {/if}
  {/if}
</div>

<!-- Settings modal -->
<Sheet open={showSettings} title="Monitoring settings" description="How the collector samples this cluster and how long history is kept." onclose={() => (showSettings = false)}>
  {#if settingsForm}
    <div class="space-y-4">
      <label class="ds-checkbox-label">
        <input type="checkbox" class="ds-checkbox" bind:checked={settingsForm.enabled} />
        Enable background collection
      </label>
      <FormField label="History retention (days)" for="ch-retention" hint="Samples older than this are pruned to keep storage small. Default 7.">
        <Input id="ch-retention" type="number" min={1} max={365} class="w-32 tabular-nums" bind:value={settingsForm.retention_days} />
      </FormField>
      <FormField label="Poll interval (seconds)" for="ch-poll" hint="How often each cluster is sampled. Minimum 15s.">
        <Input id="ch-poll" type="number" min={15} max={3600} class="w-32 tabular-nums" bind:value={settingsForm.poll_interval_seconds} />
      </FormField>
      <FormField label="Long-query threshold (seconds)" for="ch-threshold" hint='Queries running longer than this count as "long". Default 30.'>
        <Input id="ch-threshold" type="number" min={1} max={3600} class="w-32 tabular-nums" bind:value={settingsForm.long_query_threshold_seconds} />
      </FormField>
    </div>
  {/if}
  {#snippet footer()}
    <Button variant="ghost" size="sm" onclick={() => (showSettings = false)}>Cancel</Button>
    <Button size="sm" loading={savingSettings} onclick={persistSettings}>
      <Save size={13} /> Save
    </Button>
  {/snippet}
</Sheet>
