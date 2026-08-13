<script lang="ts">
  import { onMount } from 'svelte'
  import type { CostsRange, CostsResult, CostsConfig } from '../lib/api/costs'
  import { fetchCosts, getCostsConfig, saveCostsConfig } from '../lib/api/costs'
  import { openQueryTab } from '../lib/stores/tabs.svelte'
  import { success as toastSuccess, error as toastError } from '../lib/stores/toast.svelte'
  import { formatNumber, formatBytes } from '../lib/utils/format'
  import TrendChart from '../lib/components/common/TrendChart.svelte'
  import {
    Coins, RefreshCw, AlertTriangle, Server, Users, Table2, Activity,
    ExternalLink, Settings2, Download, X, Plus, Trash2, PieChart, Flame,
  } from 'lucide-svelte'

  type SectionKey = 'teams' | 'users' | 'queries' | 'storage'

  interface ColumnSpec {
    key: string
    label: string
    mono?: boolean
    right?: boolean
    format?: (v: unknown) => string
  }

  const num = (v: unknown) => Number(v ?? 0) || 0
  const fmtNum = (v: unknown) => formatNumber(num(v))
  const fmtBytes = (v: unknown) => formatBytes(num(v))
  const fmtTime = (v: unknown) => String(v ?? '')

  const RANGES: { value: CostsRange; label: string }[] = [
    { value: '24h', label: '24h' },
    { value: '7d', label: '7d' },
    { value: '30d', label: '30d' },
  ]

  const UNALLOCATED = 'Unallocated'
  const SAMPLE_QUERY_CAP = 2000

  let range = $state<CostsRange>('7d')
  let loading = $state(true)
  let refreshing = $state(false)
  let error = $state<string | null>(null)
  let unsupported = $state(false)

  let config = $state<CostsConfig | null>(null)
  let isDefaultConfig = $state(false)

  let summary = $state<Record<string, unknown> | null>(null)
  let trendRows = $state<Record<string, unknown>[]>([])
  let userRows = $state<Record<string, unknown>[]>([])
  let storageRows = $state<Record<string, unknown>[]>([])
  let queriesResult = $state<CostsResult | null>(null)
  let queriesLoading = $state(false)
  let meta = $state<{ cluster: string; is_cluster: boolean; degraded: boolean }>({ cluster: '', is_cluster: false, degraded: false })
  let activeSection = $state<SectionKey>('teams')
  let loadSeq = 0

  // ── Settings drawer ──
  let settingsOpen = $state(false)
  let saving = $state(false)
  let draft = $state<CostsConfig>({ currency: 'USD', cpuPerCoreHour: 0.06, storageGBMonth: 0.023, teams: [] })
  // Team users are edited as comma-separated text and split on save.
  let draftUsers = $state<string[]>([])

  const CURRENCIES = ['USD', 'EUR', 'GBP', 'BRL', 'CHF', 'JPY', 'INR', 'AUD', 'CAD', 'SEK']

  const currency = $derived(config?.currency ?? 'USD')

  function fmtMoney(v: unknown): string {
    const n = num(v)
    try {
      return new Intl.NumberFormat(undefined, {
        style: 'currency',
        currency,
        minimumFractionDigits: 2,
        maximumFractionDigits: n !== 0 && Math.abs(n) < 0.01 ? 4 : 2,
      }).format(n)
    } catch {
      return `${n.toFixed(2)} ${currency}`
    }
  }
  const fmtCost = (v: unknown) => fmtMoney(v)

  // ── Team rollup (client-side aggregation of the per-user rows) ──
  interface TeamRow extends Record<string, unknown> {
    team: string
    users: number
    queries: number
    failures: number
    cpu_core_hours: number
    compute_cost: number
    failed_cost: number
    read_bytes: number
    share: number
  }

  const totalComputeCost = $derived(userRows.reduce((a, r) => a + num(r.compute_cost), 0))

  const teamRows = $derived.by<TeamRow[]>(() => {
    const byTeam = new Map<string, TeamRow>()
    for (const r of userRows) {
      const team = String(r.team ?? UNALLOCATED)
      const t = byTeam.get(team) ?? {
        team, users: 0, queries: 0, failures: 0,
        cpu_core_hours: 0, compute_cost: 0, failed_cost: 0, read_bytes: 0, share: 0,
      }
      t.users += 1
      t.queries += num(r.queries)
      t.failures += num(r.failures)
      t.cpu_core_hours += num(r.cpu_core_hours)
      t.compute_cost += num(r.compute_cost)
      t.failed_cost += num(r.failed_cost)
      t.read_bytes += num(r.read_bytes)
      byTeam.set(team, t)
    }
    const rows = [...byTeam.values()].sort((a, b) => b.compute_cost - a.compute_cost)
    for (const r of rows) r.share = totalComputeCost > 0 ? (r.compute_cost / totalComputeCost) * 100 : 0
    return rows
  })

  /** Share of compute spend attributed to a real team (the FinOps allocation-coverage KPI). */
  const coverage = $derived.by(() => {
    if (totalComputeCost <= 0) return 100
    const unallocated = teamRows.find((t) => t.team === UNALLOCATED)?.compute_cost ?? 0
    return Math.max(0, Math.min(100, (1 - unallocated / totalComputeCost) * 100))
  })

  const storageMonthlyCost = $derived(storageRows.reduce((a, r) => a + num(r.monthly_cost), 0))

  // ── Trend chart: stack spend by team (top 5 + Other) ──
  const TEAM_COLORS = ['#f97316', '#0ea5e9', '#8b5cf6', '#10b981', '#f59e0b']
  const trendChart = $derived.by(() => {
    const xs = [...new Set(trendRows.map((r) => num(r.t)))].sort((a, b) => a - b)
    const xIndex = new Map(xs.map((t, i) => [t, i]))
    const totals = new Map<string, number>()
    for (const r of trendRows) {
      const team = String(r.team ?? UNALLOCATED)
      totals.set(team, (totals.get(team) ?? 0) + num(r.cost))
    }
    const top = [...totals.entries()].sort((a, b) => b[1] - a[1]).slice(0, 5).map(([t]) => t)
    const series = top.map((team, i) => ({
      label: team,
      values: new Array<number>(xs.length).fill(0),
      color: team === UNALLOCATED ? '#6b7280' : TEAM_COLORS[i % TEAM_COLORS.length],
      fill: i === 0 ? 'rgba(249,115,22,0.12)' : undefined,
    }))
    const other = { label: 'Other', values: new Array<number>(xs.length).fill(0), color: '#94a3b8', fill: undefined as string | undefined }
    let hasOther = false
    for (const r of trendRows) {
      const i = xIndex.get(num(r.t))
      if (i === undefined) continue
      const team = String(r.team ?? UNALLOCATED)
      const s = series.find((x) => x.label === team)
      if (s) s.values[i] += num(r.cost)
      else { other.values[i] += num(r.cost); hasOther = true }
    }
    return { x: xs, series: hasOther ? [...series, other] : series }
  })

  // ── Section tables ──
  const TEAM_COLUMNS: ColumnSpec[] = [
    { key: 'team', label: 'Team', mono: true },
    { key: 'compute_cost', label: 'Compute cost', right: true, format: fmtCost },
    { key: 'share', label: 'Share', right: true, format: (v) => `${num(v).toFixed(1)}%` },
    { key: 'failed_cost', label: 'Failed-query cost', right: true, format: fmtCost },
    { key: 'cpu_core_hours', label: 'CPU core-h', right: true, format: (v) => num(v).toFixed(2) },
    { key: 'read_bytes', label: 'Scanned', right: true, format: fmtBytes },
    { key: 'queries', label: 'Queries', right: true, format: fmtNum },
    { key: 'users', label: 'Users', right: true, format: fmtNum },
  ]
  const USER_COLUMNS: ColumnSpec[] = [
    { key: 'user', label: 'User', mono: true },
    { key: 'team', label: 'Team' },
    { key: 'compute_cost', label: 'Compute cost', right: true, format: fmtCost },
    { key: 'failed_cost', label: 'Failed-query cost', right: true, format: fmtCost },
    { key: 'cpu_core_hours', label: 'CPU core-h', right: true, format: (v) => num(v).toFixed(2) },
    { key: 'read_bytes', label: 'Scanned', right: true, format: fmtBytes },
    { key: 'queries', label: 'Queries', right: true, format: fmtNum },
    { key: 'failures', label: 'Failures', right: true, format: fmtNum },
  ]
  const QUERY_COLUMNS: ColumnSpec[] = [
    { key: 'sample_query', label: 'Query pattern', mono: true },
    { key: 'compute_cost', label: 'Total cost', right: true, format: fmtCost },
    { key: 'cost_per_run', label: 'Cost / run', right: true, format: fmtCost },
    { key: 'runs', label: 'Runs', right: true, format: fmtNum },
    { key: 'users', label: 'Users', right: true, format: fmtNum },
    { key: 'read_bytes', label: 'Scanned', right: true, format: fmtBytes },
    { key: 'last_seen', label: 'Last seen', right: true, format: fmtTime },
  ]
  const STORAGE_COLUMNS: ColumnSpec[] = [
    { key: 'database', label: 'Database', mono: true },
    { key: 'table', label: 'Table', mono: true },
    { key: 'monthly_cost', label: 'Cost / month', right: true, format: fmtCost },
    { key: 'bytes_on_disk', label: 'On disk', right: true, format: fmtBytes },
    { key: 'uncompressed_bytes', label: 'Uncompressed', right: true, format: fmtBytes },
    { key: 'ratio', label: 'Compression', right: true },
    { key: 'total_rows', label: 'Rows', right: true, format: fmtNum },
  ]

  const SECTIONS: { key: SectionKey; label: string; icon: typeof Users; columns: ColumnSpec[] }[] = [
    { key: 'teams', label: 'Teams', icon: PieChart, columns: TEAM_COLUMNS },
    { key: 'users', label: 'Users', icon: Users, columns: USER_COLUMNS },
    { key: 'queries', label: 'Cost drivers', icon: Flame, columns: QUERY_COLUMNS },
    { key: 'storage', label: 'Storage', icon: Table2, columns: STORAGE_COLUMNS },
  ]
  const activeSpec = $derived(SECTIONS.find((s) => s.key === activeSection) ?? SECTIONS[0])

  const sectionRows = $derived.by<Record<string, unknown>[]>(() => {
    switch (activeSection) {
      case 'teams': return teamRows
      case 'users': return userRows
      case 'queries': return queriesResult?.data ?? []
      case 'storage': return storageRows
    }
  })

  function compressionRatio(row: Record<string, unknown>): string {
    const disk = num(row.bytes_on_disk)
    const raw = num(row.uncompressed_bytes)
    if (disk <= 0 || raw <= 0) return '—'
    return `${(raw / disk).toFixed(1)}×`
  }

  const knownCluster = () => (meta.is_cluster && meta.cluster ? meta.cluster : undefined)

  async function loadAll(showSpinner = false) {
    const seq = ++loadSeq
    if (showSpinner) loading = true
    refreshing = true
    error = null
    try {
      const cfgPromise = getCostsConfig()
      const cluster = knownCluster()
      const [s, t, u, st, cfg] = await Promise.all([
        fetchCosts('summary', range, cluster),
        fetchCosts('trend', range, cluster),
        fetchCosts('users', range, cluster),
        fetchCosts('storage', range, cluster),
        cfgPromise,
      ])
      if (seq !== loadSeq) return
      config = cfg.config
      isDefaultConfig = cfg.is_default
      unsupported = !s.supported
      summary = s.data[0] ?? null
      meta = { cluster: s.cluster, is_cluster: s.is_cluster, degraded: !!s.degraded }
      trendRows = t.supported ? t.data : []
      userRows = u.supported ? u.data : []
      storageRows = st.supported ? st.data : []
      queriesResult = null
      if (activeSection === 'queries') void loadQueries(seq)
    } catch (e: unknown) {
      if (seq !== loadSeq) return
      error = e instanceof Error ? e.message : String(e)
    } finally {
      if (seq === loadSeq) {
        loading = false
        refreshing = false
      }
    }
  }

  async function loadQueries(seq = loadSeq) {
    queriesLoading = true
    try {
      const res = await fetchCosts('queries', range, knownCluster())
      if (seq !== loadSeq) return
      queriesResult = res
    } catch (e: unknown) {
      if (seq !== loadSeq) return
      toastError('Failed to load cost drivers: ' + (e instanceof Error ? e.message : String(e)))
    } finally {
      if (seq === loadSeq) queriesLoading = false
    }
  }

  function selectSection(key: SectionKey) {
    activeSection = key
    if (key === 'queries' && !queriesResult && !queriesLoading) void loadQueries()
  }

  function setRange(r: CostsRange) {
    if (range === r) return
    range = r
    void loadAll(true)
  }

  function openPattern(row: Record<string, unknown>) {
    let sql = String(row.sample_query ?? '')
    if (!sql) return
    if (sql.length >= SAMPLE_QUERY_CAP) {
      sql = `-- ⚠ Query text truncated by Cost Center — fetch the full text from system.query_log by query hash.\n${sql}`
    }
    openQueryTab(sql)
  }

  // ── CSV showback export of the active section ──
  function exportCsv() {
    const spec = activeSpec
    const rows = sectionRows
    if (!rows.length) return
    const esc = (v: unknown) => {
      const s = String(v ?? '')
      return /[",\n]/.test(s) ? `"${s.replaceAll('"', '""')}"` : s
    }
    const header = spec.columns.map((c) => esc(c.label)).join(',')
    const body = rows.map((r) =>
      spec.columns.map((c) => esc(c.key === 'ratio' ? compressionRatio(r) : r[c.key])).join(','),
    )
    const csv = [
      `# CH-UI Cost Center showback — section: ${spec.label}, range: ${range}, currency: ${currency}, exported: ${new Date().toISOString()}`,
      header, ...body,
    ].join('\n')
    const blob = new Blob([csv], { type: 'text/csv;charset=utf-8' })
    const a = document.createElement('a')
    a.href = URL.createObjectURL(blob)
    a.download = `ch-ui-costs-${activeSection}-${range}.csv`
    a.click()
    URL.revokeObjectURL(a.href)
  }

  // ── Settings ──
  function openSettings() {
    const c = config ?? { currency: 'USD', cpuPerCoreHour: 0.06, storageGBMonth: 0.023, teams: [] }
    draft = {
      currency: c.currency,
      cpuPerCoreHour: c.cpuPerCoreHour,
      storageGBMonth: c.storageGBMonth,
      teams: (c.teams ?? []).map((t) => ({ name: t.name, users: [...t.users] })),
    }
    draftUsers = (c.teams ?? []).map((t) => t.users.join(', '))
    settingsOpen = true
  }

  function addTeam() {
    draft.teams = [...draft.teams, { name: '', users: [] }]
    draftUsers = [...draftUsers, '']
  }

  function removeTeam(i: number) {
    draft.teams = draft.teams.filter((_, j) => j !== i)
    draftUsers = draftUsers.filter((_, j) => j !== i)
  }

  async function saveSettings() {
    saving = true
    try {
      const teams = draft.teams
        .map((t, i) => ({
          name: t.name.trim(),
          users: draftUsers[i].split(',').map((u) => u.trim()).filter(Boolean),
        }))
        .filter((t) => t.name)
      const res = await saveCostsConfig({ ...draft, teams })
      config = res.config
      isDefaultConfig = false
      settingsOpen = false
      toastSuccess('Cost model saved')
      void loadAll(true)
    } catch (e: unknown) {
      toastError('Failed to save: ' + (e instanceof Error ? e.message : String(e)))
    } finally {
      saving = false
    }
  }

  onMount(() => {
    void loadAll(true)
  })
</script>

<div class="flex flex-col h-full overflow-hidden">
  <!-- Header -->
  <div class="ds-page-header">
    <div class="w-full flex items-center justify-between gap-4">
      <div class="flex items-center gap-3 min-w-0">
        <Coins size={20} class="text-ch-orange shrink-0" />
        <div class="min-w-0">
          <h1 class="ds-page-title">Cost Center</h1>
          <p class="ds-page-subtitle">Showback and chargeback: who spends what on this cluster</p>
        </div>
      </div>
      <div class="flex items-center gap-2 shrink-0">
        {#if meta.is_cluster}
          <span class="ds-badge ds-badge-neutral inline-flex items-center gap-1.5">
            <Server size={12} /> {meta.cluster}
          </span>
        {/if}
        {#if meta.degraded}
          <span class="ds-badge ds-badge-warn inline-flex items-center gap-1.5" title="Some nodes could not be reached; showing local node only">
            <AlertTriangle size={12} /> Degraded
          </span>
        {/if}
        <div class="ds-segment">
          {#each RANGES as r}
            <button
              class="ds-segment-btn {range === r.value ? 'ds-segment-btn-active' : ''}"
              onclick={() => setRange(r.value)}
            >{r.label}</button>
          {/each}
        </div>
        <button class="ds-icon-btn" onclick={openSettings} title="Cost model settings" aria-label="Cost model settings">
          <Settings2 size={15} />
        </button>
        <button class="ds-icon-btn" onclick={() => loadAll()} title="Refresh" aria-label="Refresh">
          <RefreshCw size={15} class={refreshing ? 'animate-spin' : ''} />
        </button>
      </div>
    </div>
  </div>

  <div class="flex-1 overflow-auto p-4 space-y-5">
    {#if loading}
      <div class="ds-empty">Loading cost center…</div>
    {:else if error}
      <div class="ds-panel p-6 flex items-start gap-3 text-sm">
        <AlertTriangle size={18} class="text-red-500 shrink-0 mt-0.5" />
        <div>
          <div class="font-semibold text-gray-900 dark:text-gray-100">Couldn't load the cost center</div>
          <div class="text-gray-500 mt-1">{error}</div>
          <button class="ds-btn-outline px-2.5 py-1.5 mt-3" onclick={() => loadAll(true)}>Retry</button>
        </div>
      </div>
    {:else if unsupported}
      <div class="ds-panel p-6 flex items-start gap-3 text-sm">
        <AlertTriangle size={18} class="text-amber-500 shrink-0 mt-0.5" />
        <div>
          <div class="font-semibold text-gray-900 dark:text-gray-100">system.query_log is not available</div>
          <div class="text-gray-500 mt-1">
            The Cost Center prices real consumption from ClickHouse's query log. Enable it with
            <code class="px-1 py-0.5 rounded bg-gray-200/70 dark:bg-gray-800 font-mono text-[11px]">&lt;query_log&gt;</code>
            in the server config, run a few queries, then refresh.
          </div>
        </div>
      </div>
    {:else}
      {#if isDefaultConfig}
        <div class="ds-panel-muted px-4 py-2.5 text-xs text-gray-600 dark:text-gray-300 flex items-center gap-2">
          <AlertTriangle size={13} class="text-amber-500 shrink-0" />
          Using default rates ({fmtMoney(config?.cpuPerCoreHour)}/core-hour, {fmtMoney(config?.storageGBMonth)}/GB-month).
          <button class="text-ch-blue hover:underline" onclick={openSettings}>Set your real rates and teams</button>
        </div>
      {/if}

      <!-- Headline tiles -->
      <div class="grid grid-cols-2 md:grid-cols-3 xl:grid-cols-6 gap-3">
        <div class="ds-stat-card border border-gray-200 dark:border-gray-800">
          <div class="flex items-center gap-2 text-gray-500 text-xs mb-1"><Coins size={14} /> Compute cost · {range}</div>
          <div class="text-2xl font-bold text-gray-900 dark:text-gray-100">{fmtMoney(summary?.compute_cost)}</div>
          <div class="text-[11px] text-gray-400 mt-0.5">{num(summary?.cpu_core_hours).toFixed(1)} CPU core-hours</div>
        </div>
        <div class="ds-stat-card border {num(summary?.failed_cost) > 0 ? 'border-red-300/70 dark:border-red-800/70' : 'border-gray-200 dark:border-gray-800'}">
          <div class="flex items-center gap-2 text-gray-500 text-xs mb-1"><Flame size={14} /> Failed-query cost</div>
          <div class="text-2xl font-bold {num(summary?.failed_cost) > 0 ? 'text-red-600 dark:text-red-400' : 'text-gray-900 dark:text-gray-100'}">{fmtMoney(summary?.failed_cost)}</div>
          <div class="text-[11px] text-gray-400 mt-0.5">{fmtNum(summary?.failed_queries)} failed queries</div>
        </div>
        <div class="ds-stat-card border border-gray-200 dark:border-gray-800">
          <div class="flex items-center gap-2 text-gray-500 text-xs mb-1"><Table2 size={14} /> Storage / month</div>
          <div class="text-2xl font-bold text-gray-900 dark:text-gray-100">{fmtMoney(storageMonthlyCost)}</div>
          <div class="text-[11px] text-gray-400 mt-0.5">{storageRows.length} tables priced</div>
        </div>
        <div class="ds-stat-card border border-gray-200 dark:border-gray-800">
          <div class="flex items-center gap-2 text-gray-500 text-xs mb-1"><PieChart size={14} /> Allocated</div>
          <div class="text-2xl font-bold {coverage < 80 ? 'text-amber-600 dark:text-amber-400' : 'text-gray-900 dark:text-gray-100'}">{coverage.toFixed(0)}%</div>
          <div class="text-[11px] text-gray-400 mt-0.5">of compute spend mapped to a team</div>
        </div>
        <div class="ds-stat-card border border-gray-200 dark:border-gray-800">
          <div class="flex items-center gap-2 text-gray-500 text-xs mb-1"><Activity size={14} /> Queries</div>
          <div class="text-2xl font-bold text-gray-900 dark:text-gray-100">{fmtNum(summary?.total_queries)}</div>
          <div class="text-[11px] text-gray-400 mt-0.5">{fmtNum(summary?.active_users)} active users</div>
        </div>
        <div class="ds-stat-card border border-gray-200 dark:border-gray-800">
          <div class="flex items-center gap-2 text-gray-500 text-xs mb-1"><Table2 size={14} /> Data scanned</div>
          <div class="text-2xl font-bold text-gray-900 dark:text-gray-100">{fmtBytes(summary?.read_bytes)}</div>
        </div>
      </div>

      <!-- Spend trend by team -->
      <div class="ds-card p-3">
        <div class="text-xs text-gray-500 mb-2 flex items-center gap-2"><Coins size={13} /> Compute spend by team · {range}</div>
        {#if trendChart.x.length > 1}
          <TrendChart x={trendChart.x} series={trendChart.series} height={170} yLabel={currency} formatY={(v) => fmtMoney(v)} />
        {:else}
          <div class="h-[170px] grid place-items-center text-xs text-gray-400">Not enough data for this range yet</div>
        {/if}
      </div>

      <!-- Section tables -->
      <div>
        <div class="flex items-center gap-2 mb-3">
          <div class="ds-segment flex-wrap">
            {#each SECTIONS as s}
              <button
                class="ds-segment-btn {activeSection === s.key ? 'ds-segment-btn-active' : ''} inline-flex items-center gap-1.5"
                onclick={() => selectSection(s.key)}
              >
                <s.icon size={13} /> {s.label}
              </button>
            {/each}
          </div>
          <div class="flex-1"></div>
          <button
            class="ds-btn-outline px-2.5 py-1.5 inline-flex items-center gap-1.5 text-xs"
            onclick={exportCsv}
            disabled={!sectionRows.length}
            title="Export this table as a CSV showback report"
          >
            <Download size={13} /> Export CSV
          </button>
        </div>

        {#if activeSection === 'queries' && queriesLoading}
          <div class="ds-empty">Loading cost drivers…</div>
        {:else if sectionRows.length === 0}
          <div class="ds-panel-muted p-4 text-sm text-gray-500">Nothing recorded for this range.</div>
        {:else}
          <div class="ds-table-wrap">
            <table class="ds-table">
              <thead>
                <tr class="ds-table-head-row">
                  {#each activeSpec.columns as c}
                    <th class={c.right ? 'ds-table-th-right' : 'ds-table-th'}>{c.label}</th>
                  {/each}
                  {#if activeSection === 'queries'}<th class="ds-table-th-right"></th>{/if}
                </tr>
              </thead>
              <tbody>
                {#each sectionRows as row}
                  <tr class="ds-table-row">
                    {#each activeSpec.columns as c}
                      <td class="{c.right ? 'ds-td-right' : c.mono ? 'ds-td-mono' : 'ds-td'} {c.key === 'sample_query' ? 'max-w-md' : ''}">
                        {#if c.key === 'sample_query'}
                          <span class="block truncate font-mono text-[11px]" title={String(row[c.key] ?? '')}>{String(row[c.key] ?? '')}</span>
                        {:else if c.key === 'ratio'}
                          {compressionRatio(row)}
                        {:else if c.key === 'team'}
                          <span class={String(row[c.key]) === UNALLOCATED ? 'text-gray-400 italic' : ''}>{String(row[c.key] ?? '')}</span>
                        {:else}
                          {c.format ? c.format(row[c.key]) : String(row[c.key] ?? '')}
                        {/if}
                      </td>
                    {/each}
                    {#if activeSection === 'queries'}
                      <td class="ds-td-right">
                        <button
                          class="ds-icon-btn"
                          onclick={() => openPattern(row)}
                          title="Open in a new query tab"
                          aria-label="Open query in a new tab"
                        >
                          <ExternalLink size={13} />
                        </button>
                      </td>
                    {/if}
                  </tr>
                {/each}
              </tbody>
            </table>
          </div>
        {/if}
      </div>
    {/if}
  </div>
</div>

<!-- Cost model settings drawer -->
{#if settingsOpen}
  <div class="fixed inset-0 z-50 flex">
    <button
      class="flex-1 bg-black/30"
      onclick={() => (settingsOpen = false)}
      aria-label="Close settings"
    ></button>
    <div class="w-full max-w-md h-full bg-white dark:bg-gray-950 border-l border-gray-200 dark:border-gray-800 flex flex-col">
      <div class="flex items-center justify-between px-4 py-3 border-b border-gray-200 dark:border-gray-800">
        <h2 class="text-sm font-semibold text-gray-800 dark:text-gray-200 flex items-center gap-2">
          <Settings2 size={15} /> Cost model
        </h2>
        <button class="ds-icon-btn" onclick={() => (settingsOpen = false)} aria-label="Close">
          <X size={15} />
        </button>
      </div>

      <div class="flex-1 overflow-auto p-4 space-y-5">
        <div class="grid grid-cols-3 gap-3">
          <div>
            <label class="block text-[10px] font-medium text-gray-500 uppercase mb-1" for="cc-currency">Currency</label>
            <select
              id="cc-currency"
              bind:value={draft.currency}
              class="w-full text-xs px-2 py-1.5 rounded-md border border-gray-300 dark:border-gray-700 bg-white dark:bg-gray-900 text-gray-800 dark:text-gray-200"
            >
              {#each CURRENCIES as c}<option value={c}>{c}</option>{/each}
            </select>
          </div>
          <div>
            <label class="block text-[10px] font-medium text-gray-500 uppercase mb-1" for="cc-cpu">CPU / core-hour</label>
            <input
              id="cc-cpu" type="number" min="0" step="0.001"
              bind:value={draft.cpuPerCoreHour}
              class="w-full text-xs px-2 py-1.5 rounded-md border border-gray-300 dark:border-gray-700 bg-white dark:bg-gray-900 text-gray-800 dark:text-gray-200"
            />
          </div>
          <div>
            <label class="block text-[10px] font-medium text-gray-500 uppercase mb-1" for="cc-storage">Storage / GB-month</label>
            <input
              id="cc-storage" type="number" min="0" step="0.001"
              bind:value={draft.storageGBMonth}
              class="w-full text-xs px-2 py-1.5 rounded-md border border-gray-300 dark:border-gray-700 bg-white dark:bg-gray-900 text-gray-800 dark:text-gray-200"
            />
          </div>
        </div>
        <p class="text-[11px] text-gray-400">
          Derive the CPU rate from what a core costs you per hour (instance price ÷ vCPUs, or amortized hardware).
          Compute spend is CPU time × rate; storage is on-disk bytes × the monthly rate.
        </p>

        <div>
          <div class="flex items-center justify-between mb-2">
            <h3 class="text-xs font-medium text-gray-700 dark:text-gray-300">Teams (cost centers)</h3>
            <button class="ds-btn-outline px-2 py-1 text-[11px] inline-flex items-center gap-1" onclick={addTeam}>
              <Plus size={12} /> Add team
            </button>
          </div>
          <p class="text-[11px] text-gray-400 mb-3">
            Map ClickHouse users to teams: exact names or prefixes ending with * (e.g. <code class="font-mono">etl_*</code>).
            First match wins; unmatched users land in {UNALLOCATED}.
          </p>
          {#if draft.teams.length === 0}
            <div class="ds-panel-muted p-3 text-xs text-gray-500">No teams yet — all spend shows as {UNALLOCATED}.</div>
          {/if}
          <div class="space-y-2">
            {#each draft.teams as team, i}
              <div class="flex items-start gap-2 rounded-md border border-gray-200 dark:border-gray-800 p-2">
                <div class="flex-1 space-y-1.5">
                  <input
                    type="text" placeholder="Team name"
                    bind:value={team.name}
                    class="w-full text-xs px-2 py-1.5 rounded-md border border-gray-300 dark:border-gray-700 bg-white dark:bg-gray-900 text-gray-800 dark:text-gray-200"
                  />
                  <input
                    type="text" placeholder="Users: analyst_1, etl_*, airflow"
                    bind:value={draftUsers[i]}
                    class="w-full text-xs px-2 py-1.5 rounded-md border border-gray-300 dark:border-gray-700 bg-white dark:bg-gray-900 font-mono text-gray-800 dark:text-gray-200"
                  />
                </div>
                <button class="ds-icon-btn mt-0.5" onclick={() => removeTeam(i)} title="Remove team" aria-label="Remove team">
                  <Trash2 size={13} />
                </button>
              </div>
            {/each}
          </div>
        </div>
      </div>

      <div class="px-4 py-3 border-t border-gray-200 dark:border-gray-800 flex justify-end gap-2">
        <button class="ds-btn-outline px-3 py-1.5 text-xs" onclick={() => (settingsOpen = false)}>Cancel</button>
        <button class="ds-btn-primary px-3 py-1.5 text-xs" disabled={saving} onclick={saveSettings}>
          {saving ? 'Saving…' : 'Save'}
        </button>
      </div>
    </div>
  </div>
{/if}
