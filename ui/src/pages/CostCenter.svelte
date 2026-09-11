<script lang="ts">
  import { onMount } from 'svelte'
  import type { CostsRange, CostsResult, CostsConfig } from '../lib/api/costs'
  import { fetchCosts, getCostsConfig, saveCostsConfig } from '../lib/api/costs'
  import { openQueryTab } from '../lib/stores/tabs.svelte'
  import { success as toastSuccess, error as toastError } from '../lib/stores/toast.svelte'
  import { formatNumber, formatBytes } from '../lib/utils/format'
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
  import DataTable, { type DataColumn } from '../lib/components/common/DataTable.svelte'
  import { getSection } from '../lib/stores/nav.svelte'
  import { PAGE_SECTIONS } from '../lib/routes'
  import Sheet from '../lib/components/common/Sheet.svelte'
  import FormField from '../lib/components/common/FormField.svelte'
  import Input from '../lib/components/common/Input.svelte'
  import Select from '../lib/components/common/Select.svelte'
  import {
    RefreshCw, AlertTriangle, Server, ExternalLink, Settings2, Download, Plus, Trash2,
  } from 'lucide-svelte'

  type Row = Record<string, unknown>
  type SectionKey = 'overview' | 'teams' | 'users' | 'queries' | 'storage'
  type ColumnSpec = DataColumn<Row>

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
  let loadSeq = 0

  // The sidebar drives `?section=`; anything unknown lands on the overview.
  const SECTION_LABELS: Record<SectionKey, string> = Object.fromEntries(
    (PAGE_SECTIONS['cost-center'] ?? []).map((s) => [s.id, s.label]),
  ) as Record<SectionKey, string>
  const activeSection = $derived.by<SectionKey>(() => {
    const s = getSection()
    return s && s in SECTION_LABELS ? (s as SectionKey) : 'overview'
  })

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
    { key: 'compute_cost', label: 'Compute cost', align: 'right', format: fmtCost },
    { key: 'share', label: 'Share', align: 'right', format: (v) => `${num(v).toFixed(1)}%` },
    { key: 'failed_cost', label: 'Failed-query cost', align: 'right', format: fmtCost },
    { key: 'cpu_core_hours', label: 'CPU core-h', align: 'right', format: (v) => num(v).toFixed(2) },
    { key: 'read_bytes', label: 'Scanned', align: 'right', format: fmtBytes },
    { key: 'queries', label: 'Queries', align: 'right', format: fmtNum },
    { key: 'users', label: 'Users', align: 'right', format: fmtNum },
  ]
  const USER_COLUMNS: ColumnSpec[] = [
    { key: 'user', label: 'User', mono: true },
    { key: 'team', label: 'Team' },
    { key: 'compute_cost', label: 'Compute cost', align: 'right', format: fmtCost },
    { key: 'failed_cost', label: 'Failed-query cost', align: 'right', format: fmtCost },
    { key: 'cpu_core_hours', label: 'CPU core-h', align: 'right', format: (v) => num(v).toFixed(2) },
    { key: 'read_bytes', label: 'Scanned', align: 'right', format: fmtBytes },
    { key: 'queries', label: 'Queries', align: 'right', format: fmtNum },
    { key: 'failures', label: 'Failures', align: 'right', format: fmtNum },
  ]
  const QUERY_COLUMNS: ColumnSpec[] = [
    { key: 'sample_query', label: 'Query pattern', mono: true, truncate: true, width: '40%' },
    { key: 'compute_cost', label: 'Total cost', align: 'right', format: fmtCost },
    { key: 'cost_per_run', label: 'Cost / run', align: 'right', format: fmtCost },
    { key: 'runs', label: 'Runs', align: 'right', format: fmtNum },
    { key: 'users', label: 'Users', align: 'right', format: fmtNum },
    { key: 'read_bytes', label: 'Scanned', align: 'right', format: fmtBytes },
    { key: 'last_seen', label: 'Last seen', align: 'right', format: fmtTime },
  ]
  const STORAGE_COLUMNS: ColumnSpec[] = [
    { key: 'database', label: 'Database', mono: true },
    { key: 'table', label: 'Table', mono: true },
    { key: 'monthly_cost', label: 'Cost / month', align: 'right', format: fmtCost },
    { key: 'bytes_on_disk', label: 'On disk', align: 'right', format: fmtBytes },
    { key: 'uncompressed_bytes', label: 'Uncompressed', align: 'right', format: fmtBytes },
    { key: 'ratio', label: 'Compression', align: 'right', format: (_v, row) => compressionRatio(row), sortValue: (row) => compressionValue(row) },
    { key: 'total_rows', label: 'Rows', align: 'right', format: fmtNum },
  ]

  const SECTION_COLUMNS: Record<Exclude<SectionKey, 'overview'>, ColumnSpec[]> = {
    teams: TEAM_COLUMNS,
    users: USER_COLUMNS,
    queries: QUERY_COLUMNS,
    storage: STORAGE_COLUMNS,
  }
  const activeColumns = $derived(activeSection === 'overview' ? [] : SECTION_COLUMNS[activeSection])

  const sectionRows = $derived.by<Row[]>(() => {
    switch (activeSection) {
      case 'teams': return teamRows
      case 'users': return userRows
      case 'queries': return queriesResult?.data ?? []
      case 'storage': return storageRows
      case 'overview': return []
    }
  })

  const rangeItems = RANGES.map((r) => ({ id: r.value, label: r.label }))
  const currencyOptions = CURRENCIES.map((c) => ({ value: c, label: c }))

  function compressionValue(row: Row): number {
    const disk = num(row.bytes_on_disk)
    const raw = num(row.uncompressed_bytes)
    return disk > 0 && raw > 0 ? raw / disk : 0
  }

  function compressionRatio(row: Row): string {
    const ratio = compressionValue(row)
    return ratio > 0 ? `${ratio.toFixed(1)}×` : '—'
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

  // Cost drivers are the expensive call, so they load only when that
  // section is open and not already loaded.
  $effect(() => {
    if (activeSection === 'queries' && !queriesResult && !queriesLoading) void loadQueries()
  })

  function setRange(r: CostsRange) {
    if (range === r) return
    range = r
    void loadAll(true)
  }

  function openPattern(row: Row) {
    let sql = String(row.sample_query ?? '')
    if (!sql) return
    if (sql.length >= SAMPLE_QUERY_CAP) {
      sql = `-- NOTE: query text truncated by Cost Center — fetch the full text from system.query_log by query hash.\n${sql}`
    }
    openQueryTab(sql)
  }

  // ── CSV showback export of the active section ──
  function exportCsv() {
    const columns = activeColumns
    const rows = sectionRows
    if (!rows.length || !columns.length) return
    const esc = (v: unknown) => {
      const s = String(v ?? '')
      return /[",\n]/.test(s) ? `"${s.replaceAll('"', '""')}"` : s
    }
    const header = columns.map((c) => esc(c.label)).join(',')
    const body = rows.map((r) =>
      columns.map((c) => esc(c.key === 'ratio' ? compressionRatio(r) : r[c.key])).join(','),
    )
    const csv = [
      `# CH-UI Cost Center showback — section: ${SECTION_LABELS[activeSection]}, range: ${range}, currency: ${currency}, exported: ${new Date().toISOString()}`,
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

{#snippet teamCell(row: Row, col: ColumnSpec, value: string)}
  {#if col.key === 'team' && value === UNALLOCATED}
    <span class="italic text-fg-4">{value}</span>
  {:else}
    {value}
  {/if}
{/snippet}

{#snippet openAction(row: Row)}
  <Button icon variant="ghost" size="xs" onclick={() => openPattern(row)} title="Open in a new query tab" aria-label="Open query in a new tab">
    <ExternalLink size={13} />
  </Button>
{/snippet}

<div class="flex h-full min-h-0 flex-col">
  <PageHeader title="Cost Center" subtitle={SECTION_LABELS[activeSection]} meta={headerMeta}>
    {#snippet actions()}
      <Tabs variant="segmented" size="sm" items={rangeItems} value={range} onchange={(id) => setRange(id as CostsRange)} />
      <Button icon variant="ghost" size="sm" aria-label="Refresh" title="Refresh" onclick={() => loadAll()}>
        <RefreshCw size={14} class={refreshing ? 'animate-spin' : ''} />
      </Button>
      <Button variant="outline" size="sm" onclick={openSettings}>
        <Settings2 size={13} /> Cost model
      </Button>
    {/snippet}
  </PageHeader>

  {#if loading}
    <div class="flex flex-1 items-center justify-center"><Spinner /></div>
  {:else if error}
    <PageBody width="lg">
      <EmptyState icon={AlertTriangle} title="Couldn't load the cost center" description={error} primary={{ label: 'Retry', onclick: () => loadAll(true) }} />
    </PageBody>
  {:else if unsupported}
    <PageBody width="lg">
      <EmptyState icon={AlertTriangle} title="system.query_log is not available">
        <p class="max-w-[52ch] text-[13px] leading-relaxed text-fg-3">
          The Cost Center prices real consumption from ClickHouse's query log. Enable it with
          <code class="rounded-sm bg-surface-2 px-1 py-0.5 font-mono text-[11px]">&lt;query_log&gt;</code>
          in the server config, run a few queries, then refresh.
        </p>
      </EmptyState>
    </PageBody>
  {:else if activeSection === 'overview'}
    <PageBody width="lg">
      <div class="space-y-5">
        {#if isDefaultConfig}
          <Panel variant="muted" padding="sm">
            <div class="flex flex-wrap items-center gap-2 text-xs text-fg-2">
              <AlertTriangle size={13} class="shrink-0 text-warning" />
              Using default rates ({fmtMoney(config?.cpuPerCoreHour)}/core-hour, {fmtMoney(config?.storageGBMonth)}/GB-month).
              <button class="text-accent hover:underline" onclick={openSettings}>Set your real rates and teams</button>
            </div>
          </Panel>
        {/if}

        <div class="grid grid-cols-2 gap-3 md:grid-cols-3 xl:grid-cols-6">
          <Stat label={`Compute cost · ${range}`} value={fmtMoney(summary?.compute_cost)} hint={`${num(summary?.cpu_core_hours).toFixed(1)} CPU core-hours`} />
          <Stat label="Failed-query cost" value={fmtMoney(summary?.failed_cost)} tone={num(summary?.failed_cost) > 0 ? 'danger' : 'default'} hint={`${fmtNum(summary?.failed_queries)} failed queries`} />
          <Stat label="Storage / month" value={fmtMoney(storageMonthlyCost)} hint={`${storageRows.length} tables priced`} />
          <Stat label="Allocated" value={`${coverage.toFixed(0)}%`} tone={coverage < 80 ? 'warning' : 'default'} hint="of compute spend mapped to a team" />
          <Stat label="Queries" value={fmtNum(summary?.total_queries)} hint={`${fmtNum(summary?.active_users)} active users`} />
          <Stat label="Data scanned" value={fmtBytes(summary?.read_bytes)} />
        </div>

        <Panel title="Compute spend by team" description={`Last ${range}`} padding="sm">
          {#if trendChart.x.length > 1}
            <TrendChart x={trendChart.x} series={trendChart.series} height={200} formatY={(v) => fmtMoney(v)} />
          {:else}
            <div class="grid h-[200px] place-items-center text-xs text-fg-4">Not enough data for this range yet</div>
          {/if}
        </Panel>
      </div>
    </PageBody>
  {:else}
    <div class="flex min-h-0 flex-1 flex-col">
      <div class="flex h-10 shrink-0 items-center gap-3 px-5">
        <span class="text-[13px] font-semibold text-fg">{SECTION_LABELS[activeSection]}</span>
        <span class="ml-auto text-xs text-fg-4">{fmtNum(sectionRows.length)} rows</span>
        <Button variant="outline" size="sm" onclick={exportCsv} disabled={!sectionRows.length} title="Export this table as a CSV showback report">
          <Download size={13} /> Export CSV
        </Button>
      </div>
      <div class="flex min-h-0 flex-1 flex-col px-5 pb-4">
        {#if activeSection === 'queries' && queriesLoading}
          <div class="flex flex-1 items-center justify-center"><Spinner /></div>
        {:else}
          <DataTable
            fill
            class="flex-1 rounded-lg border border-edge-subtle bg-surface"
            columns={activeColumns}
            rows={sectionRows}
            emptyTitle="Nothing recorded for this range"
            cell={activeSection === 'teams' || activeSection === 'users' ? teamCell : undefined}
            actions={activeSection === 'queries' ? openAction : undefined}
          />
        {/if}
      </div>
    </div>
  {/if}
</div>

<!-- Cost model settings -->
<Sheet open={settingsOpen} title="Cost model" description="Rates and team mappings used to price consumption." onclose={() => (settingsOpen = false)}>
  <div class="space-y-5">
    <div class="grid grid-cols-3 gap-3">
      <FormField label="Currency" for="cc-currency">
        <Select id="cc-currency" size="sm" options={currencyOptions} bind:value={draft.currency} />
      </FormField>
      <FormField label="CPU / core-hour" for="cc-cpu">
        <Input id="cc-cpu" type="number" min="0" step="0.001" size="sm" class="tabular-nums" bind:value={draft.cpuPerCoreHour} />
      </FormField>
      <FormField label="Storage / GB-month" for="cc-storage">
        <Input id="cc-storage" type="number" min="0" step="0.001" size="sm" class="tabular-nums" bind:value={draft.storageGBMonth} />
      </FormField>
    </div>
    <p class="text-xs leading-relaxed text-fg-4">
      Derive the CPU rate from what a core costs you per hour (instance price ÷ vCPUs, or amortized hardware).
      Compute spend is CPU time × rate; storage is on-disk bytes × the monthly rate.
    </p>

    <div>
      <div class="mb-2 flex items-center justify-between">
        <h3 class="text-[13px] font-semibold text-fg">Teams (cost centers)</h3>
        <Button variant="outline" size="xs" onclick={addTeam}>
          <Plus size={12} /> Add team
        </Button>
      </div>
      <p class="mb-3 text-xs leading-relaxed text-fg-4">
        Map ClickHouse users to teams: exact names or prefixes ending with * (e.g. <code class="font-mono">etl_*</code>).
        First match wins; unmatched users land in {UNALLOCATED}.
      </p>
      {#if draft.teams.length === 0}
        <Panel variant="muted" padding="sm">
          <p class="text-xs text-fg-3">No teams yet. All spend shows as {UNALLOCATED}.</p>
        </Panel>
      {/if}
      <div class="space-y-2">
        {#each draft.teams as team, i}
          <div class="flex items-start gap-2 rounded-md border border-edge-subtle p-2">
            <div class="flex-1 space-y-1.5">
              <Input size="sm" placeholder="Team name" bind:value={team.name} />
              <Input size="sm" mono placeholder="Users: analyst_1, etl_*, airflow" bind:value={draftUsers[i]} />
            </div>
            <Button icon variant="ghost" size="sm" class="mt-0.5" onclick={() => removeTeam(i)} title="Remove team" aria-label="Remove team">
              <Trash2 size={13} />
            </Button>
          </div>
        {/each}
      </div>
    </div>
  </div>
  {#snippet footer()}
    <Button variant="ghost" size="sm" onclick={() => (settingsOpen = false)}>Cancel</Button>
    <Button size="sm" loading={saving} onclick={saveSettings}>Save</Button>
  {/snippet}
</Sheet>
