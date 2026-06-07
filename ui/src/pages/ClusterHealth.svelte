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
  import MiniTrendChart from '../lib/components/common/MiniTrendChart.svelte'
  import Modal from '../lib/components/common/Modal.svelte'
  import { success as toastSuccess, error as toastError } from '../lib/stores/toast.svelte'

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

  let activeSection = $state<LiveSection>('replication')
  let sectionResult = $state<LiveResult | null>(null)
  let sectionLoading = $state(false)

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
      empty: 'No stalled or failing replication tasks. 🎉',
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

  const activeSpec = $derived(SECTIONS.find(s => s.key === activeSection) ?? SECTIONS[0])

  // ── Derived aggregates ─────────────────────────────────────────────────────
  const nodes = $derived(summary?.nodes ?? [])
  const agg = $derived.by(() => {
    const list = nodes
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

  // ── Status thresholds ──────────────────────────────────────────────────────
  type Status = 'ok' | 'warn' | 'crit'
  function band(value: number, warn: number, crit: number): Status {
    if (value >= crit) return 'crit'
    if (value >= warn) return 'warn'
    return 'ok'
  }
  function badgeClass(s: Status): string {
    return s === 'crit' ? 'ds-badge-danger' : s === 'warn' ? 'ds-badge-warn' : 'ds-badge-success'
  }
  function tileBorder(s: Status): string {
    if (s === 'crit') return 'border-red-400/50 dark:border-red-500/40'
    if (s === 'warn') return 'border-amber-400/50 dark:border-amber-500/40'
    return 'border-gray-200 dark:border-gray-800'
  }

  // ── Trend series (aggregate across nodes per timestamp) ────────────────────
  function buildSeries(metric: (n: NodeSample) => number): { x: number[]; y: number[] } {
    const byTime = new Map<string, number>()
    for (const s of history) {
      const v = metric(s) || 0
      const cur = byTime.get(s.captured_at)
      byTime.set(s.captured_at, cur === undefined ? v : Math.max(cur, v))
    }
    const times = Array.from(byTime.keys()).sort()
    return {
      x: times.map((_, i) => i),
      y: times.map(t => byTime.get(t) ?? 0),
    }
  }
  const delaySeries = $derived(buildSeries(n => n.replication_max_delay))
  const partsSeries = $derived(buildSeries(n => n.parts_pressure_pct))

  // ── Formatting ─────────────────────────────────────────────────────────────
  function fmtNum(v: unknown): string {
    const n = typeof v === 'number' ? v : Number(v)
    if (!isFinite(n)) return String(v ?? '—')
    return n.toLocaleString()
  }
  function cell(row: Record<string, unknown>, key: string): string {
    const v = row[key]
    if (v === null || v === undefined || v === '') return '—'
    return String(v)
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
      const res = await fetchHistory('6h')
      history = res.data ?? []
    } catch {
      // history is best-effort; ignore failures
    }
  }

  async function loadSection(section: LiveSection) {
    activeSection = section
    sectionLoading = true
    sectionResult = null
    try {
      sectionResult = await fetchLive(section)
    } catch (e: any) {
      toastError(e?.message ?? 'Failed to load section')
    } finally {
      sectionLoading = false
    }
  }

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
        if (sectionResult) loadSection(activeSection)
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
    await loadSection('replication')
    startPolling()
  })
  onDestroy(stopPolling)
</script>

<div class="flex flex-col h-full overflow-hidden">
  <!-- Header -->
  <div class="ds-page-header">
    <div class="flex items-center justify-between gap-4">
      <div class="flex items-center gap-3 min-w-0">
        <HeartPulse size={20} class="text-ch-orange shrink-0" />
        <div class="min-w-0">
          <h1 class="ds-page-title">Cluster Health</h1>
          <p class="ds-page-subtitle">
            Operations &amp; database monitoring across all nodes
          </p>
        </div>
      </div>
      <div class="flex items-center gap-2 shrink-0">
        {#if summary}
          <span class="ds-badge ds-badge-neutral inline-flex items-center gap-1.5">
            <Server size={12} />
            {#if summary.is_cluster}
              {summary.cluster} · {nodes.length} node{nodes.length === 1 ? '' : 's'}
            {:else}
              Single node
            {/if}
          </span>
        {/if}
        {#if summary?.degraded}
          <span class="ds-badge ds-badge-warn inline-flex items-center gap-1.5" title="Some nodes could not be reached; showing local node only">
            <AlertTriangle size={12} /> Degraded
          </span>
        {/if}
        <button class="ds-icon-btn" onclick={() => loadSummary()} title="Refresh" aria-label="Refresh">
          <RefreshCw size={15} class={refreshing ? 'animate-spin' : ''} />
        </button>
        <button class="ds-btn-outline px-2.5 py-1.5 inline-flex items-center gap-1.5" onclick={openSettings}>
          <Settings2 size={14} /> Settings
        </button>
      </div>
    </div>
  </div>

  <div class="flex-1 overflow-auto p-4 space-y-5">
    {#if loading}
      <div class="ds-empty">Loading cluster health…</div>
    {:else if error}
      <div class="ds-panel p-6 flex items-start gap-3 text-sm">
        <AlertTriangle size={18} class="text-red-500 shrink-0 mt-0.5" />
        <div>
          <div class="font-semibold text-gray-900 dark:text-gray-100">Couldn't load cluster health</div>
          <div class="text-gray-500 mt-1">{error}</div>
          <button class="ds-btn-outline px-2.5 py-1.5 mt-3" onclick={() => loadSummary(true)}>Retry</button>
        </div>
      </div>
    {:else if summary}
      <!-- Headline tiles -->
      {@const delayStatus = band(agg.maxDelay, 10, 60)}
      {@const queueStatus = band(agg.totalQueue, 10, 100)}
      {@const roStatus = agg.readonly > 0 ? 'crit' : 'ok'}
      {@const partsStatus = band(agg.partsPressure, 50, 80)}
      {@const mutStatus = band(agg.mutations, 1, 5)}
      {@const lqStatus = band(agg.longQueries, 1, 5)}
      <div class="grid grid-cols-2 md:grid-cols-3 xl:grid-cols-6 gap-3">
        <div class="ds-stat-card border {tileBorder(delayStatus)}">
          <div class="flex items-center gap-2 text-gray-500 text-xs mb-1"><GitBranch size={14} /> Max repl. delay</div>
          <div class="text-2xl font-bold text-gray-900 dark:text-gray-100">{agg.maxDelay}<span class="text-sm font-normal text-gray-500"> s</span></div>
        </div>
        <div class="ds-stat-card border {tileBorder(queueStatus)}">
          <div class="flex items-center gap-2 text-gray-500 text-xs mb-1"><Layers size={14} /> Repl. queue</div>
          <div class="text-2xl font-bold text-gray-900 dark:text-gray-100">{fmtNum(agg.totalQueue)}</div>
        </div>
        <div class="ds-stat-card border {tileBorder(roStatus)}">
          <div class="flex items-center gap-2 text-gray-500 text-xs mb-1"><AlertTriangle size={14} /> Readonly replicas</div>
          <div class="text-2xl font-bold text-gray-900 dark:text-gray-100">{agg.readonly}</div>
        </div>
        <div class="ds-stat-card border {tileBorder(partsStatus)}">
          <div class="flex items-center gap-2 text-gray-500 text-xs mb-1"><Database size={14} /> Parts pressure</div>
          <div class="text-2xl font-bold text-gray-900 dark:text-gray-100">{agg.partsPressure.toFixed(0)}<span class="text-sm font-normal text-gray-500">%</span></div>
          <div class="text-[11px] text-gray-400 mt-0.5">{fmtNum(agg.partsMax)} / {fmtNum(summary.parts_limits?.parts_to_throw_insert)} parts</div>
        </div>
        <div class="ds-stat-card border {tileBorder(mutStatus)}">
          <div class="flex items-center gap-2 text-gray-500 text-xs mb-1"><Wrench size={14} /> Mutations pending</div>
          <div class="text-2xl font-bold text-gray-900 dark:text-gray-100">{fmtNum(agg.mutations)}</div>
          <div class="text-[11px] text-gray-400 mt-0.5">{fmtNum(agg.merges)} merges running</div>
        </div>
        <div class="ds-stat-card border {tileBorder(lqStatus)}">
          <div class="flex items-center gap-2 text-gray-500 text-xs mb-1"><Timer size={14} /> Long queries</div>
          <div class="text-2xl font-bold text-gray-900 dark:text-gray-100">{fmtNum(agg.longQueries)}</div>
          <div class="text-[11px] text-gray-400 mt-0.5">&gt; {summary.threshold_seconds}s</div>
        </div>
      </div>

      <!-- Trends -->
      {#if history.length > 1}
        <div class="grid grid-cols-1 md:grid-cols-2 gap-3">
          <div class="ds-card p-3">
            <div class="text-xs text-gray-500 mb-2 flex items-center gap-2"><GitBranch size={13} /> Max replication delay · last 6h</div>
            <MiniTrendChart x={delaySeries.x} y={delaySeries.y} height={110} />
          </div>
          <div class="ds-card p-3">
            <div class="text-xs text-gray-500 mb-2 flex items-center gap-2"><Database size={13} /> Parts pressure % · last 6h</div>
            <MiniTrendChart x={partsSeries.x} y={partsSeries.y} height={110} color="#0ea5e9" fill="rgba(14,165,233,0.16)" />
          </div>
        </div>
      {/if}

      <!-- Per-node table -->
      {#if nodes.length > 0}
        <div class="ds-table-wrap">
          <table class="ds-table">
            <thead>
              <tr class="ds-table-head-row">
                <th class="ds-table-th">Node</th>
                <th class="ds-table-th-right">Repl. delay (s)</th>
                <th class="ds-table-th-right">Queue</th>
                <th class="ds-table-th-right">Readonly</th>
                <th class="ds-table-th-right">Merges</th>
                <th class="ds-table-th-right">Mutations</th>
                <th class="ds-table-th-right">Max parts</th>
                <th class="ds-table-th-right">Long queries</th>
              </tr>
            </thead>
            <tbody>
              {#each nodes as n}
                <tr class="ds-table-row">
                  <td class="ds-td-mono">{n.node}</td>
                  <td class="ds-td-right">
                    <span class="ds-badge {badgeClass(band(n.replication_max_delay, 10, 60))}">{n.replication_max_delay}</span>
                  </td>
                  <td class="ds-td-right">{fmtNum(n.replication_queue_total)}</td>
                  <td class="ds-td-right">
                    {#if n.replicas_readonly > 0}<span class="ds-badge ds-badge-danger">{n.replicas_readonly}</span>{:else}0{/if}
                  </td>
                  <td class="ds-td-right">{fmtNum(n.merges_running)}</td>
                  <td class="ds-td-right">{fmtNum(n.mutations_pending)}</td>
                  <td class="ds-td-right">{fmtNum(n.parts_max_active)}</td>
                  <td class="ds-td-right">{fmtNum(n.long_queries)}</td>
                </tr>
              {/each}
            </tbody>
          </table>
        </div>
      {/if}

      <!-- Drill-down sections -->
      <div>
        <div class="ds-segment flex-wrap mb-3">
          {#each SECTIONS as s}
            <button
              class="ds-segment-btn {activeSection === s.key ? 'ds-segment-btn-active' : ''} inline-flex items-center gap-1.5"
              onclick={() => loadSection(s.key)}
            >
              <s.icon size={13} /> {s.label}
            </button>
          {/each}
        </div>

        {#if sectionLoading}
          <div class="ds-empty">Loading {activeSpec.label.toLowerCase()}…</div>
        {:else if sectionResult && !sectionResult.supported}
          <div class="ds-panel-muted p-4 text-sm text-gray-500 flex items-center gap-2">
            <AlertTriangle size={15} /> {activeSpec.label} is not available on this ClickHouse deployment.
          </div>
        {:else if sectionResult && sectionResult.data.length === 0}
          <div class="ds-empty">{activeSpec.empty}</div>
        {:else if sectionResult}
          {#if sectionResult.degraded}
            <div class="text-[11px] text-amber-600 dark:text-amber-500 mb-2 flex items-center gap-1.5">
              <AlertTriangle size={12} /> Showing local node only — remote nodes were unreachable.
            </div>
          {/if}
          <div class="ds-table-wrap">
            <table class="ds-table">
              <thead>
                <tr class="ds-table-head-row">
                  {#each activeSpec.columns as col}
                    <th class="ds-table-th">{col.label}</th>
                  {/each}
                </tr>
              </thead>
              <tbody>
                {#each sectionResult.data as row}
                  <tr class="ds-table-row">
                    {#each activeSpec.columns as col}
                      <td class={col.mono ? 'ds-td-mono max-w-xs truncate' : 'ds-td'} title={cell(row, col.key)}>
                        {cell(row, col.key)}
                      </td>
                    {/each}
                  </tr>
                {/each}
              </tbody>
            </table>
          </div>
        {/if}
      </div>

      {#if lastUpdated}
        <div class="text-[11px] text-gray-400 text-right">
          Updated {lastUpdated.toLocaleTimeString()} · auto-refresh every {UI_REFRESH_MS / 1000}s
        </div>
      {/if}
    {/if}
  </div>
</div>

<!-- Settings modal -->
<Modal open={showSettings} title="Monitoring Settings" onclose={() => (showSettings = false)}>
  {#if settingsForm}
    <div class="space-y-4">
      <label class="ds-checkbox-label">
        <input type="checkbox" class="ds-checkbox" bind:checked={settingsForm.enabled} />
        Enable background collection
      </label>
      <div>
        <div class="ds-form-label">History retention (days)</div>
        <input type="number" min="1" max="365" class="ds-input-sm w-full" bind:value={settingsForm.retention_days} />
        <p class="text-[11px] text-gray-400 mt-1">Samples older than this are pruned to keep storage small. Default 7.</p>
      </div>
      <div>
        <div class="ds-form-label">Poll interval (seconds)</div>
        <input type="number" min="15" max="3600" class="ds-input-sm w-full" bind:value={settingsForm.poll_interval_seconds} />
        <p class="text-[11px] text-gray-400 mt-1">How often each cluster is sampled. Minimum 15s.</p>
      </div>
      <div>
        <div class="ds-form-label">Long-query threshold (seconds)</div>
        <input type="number" min="1" max="3600" class="ds-input-sm w-full" bind:value={settingsForm.long_query_threshold_seconds} />
        <p class="text-[11px] text-gray-400 mt-1">Queries running longer than this count as "long". Default 30.</p>
      </div>
      <div class="flex justify-end gap-2 pt-2">
        <button class="ds-btn-ghost px-3 py-1.5" onclick={() => (showSettings = false)}>Cancel</button>
        <button class="ds-btn-primary px-3 py-1.5 inline-flex items-center gap-1.5" disabled={savingSettings} onclick={persistSettings}>
          <Save size={14} /> {savingSettings ? 'Saving…' : 'Save'}
        </button>
      </div>
    </div>
  {/if}
</Modal>
