<!-- SPDX-License-Identifier: BUSL-1.1 -->
<script lang="ts">
  import { onMount } from 'svelte'
  import type { Schedule, ScheduleRun, SavedQuery } from '../lib/types/api'
  import { apiGet, apiPost, apiPut, apiDel } from '../lib/api/client'
  import { success as toastSuccess, error as toastError, errorFrom as toastErrorFrom } from '../lib/stores/toast.svelte'
  import { openSavedQueryTab } from '../lib/stores/tabs.svelte'
  import { formatDate, formatRelativeTime } from '../lib/utils/format'
  import Button from '../lib/components/common/Button.svelte'
  import Combobox from '../lib/components/common/Combobox.svelte'
  import Spinner from '../lib/components/common/Spinner.svelte'
  import Sheet from '../lib/components/common/Sheet.svelte'
  import ConfirmDialog from '../lib/components/common/ConfirmDialog.svelte'
  import PageHeader from '../lib/components/common/PageHeader.svelte'
  import Badge from '../lib/components/common/Badge.svelte'
  import Switch from '../lib/components/common/Switch.svelte'
  import EmptyState from '../lib/components/common/EmptyState.svelte'
  import FormField from '../lib/components/common/FormField.svelte'
  import Input from '../lib/components/common/Input.svelte'
  import Panel from '../lib/components/common/Panel.svelte'
  import DataTable, { type DataColumn } from '../lib/components/common/DataTable.svelte'
  import ReportsSection from '../lib/components/reports/ReportsSection.svelte'
  import { getSection } from '../lib/stores/nav.svelte'
  import { getSession } from '../lib/stores/session.svelte'
  import { loadConnectionNames, otherConnectionId } from '../lib/stores/connection-names'
  import { PAGE_SECTIONS, docsURL } from '../lib/routes'
  import { Clock, Plus, Play, Trash2, FileText, ExternalLink, Pencil, History, Search } from 'lucide-svelte'

  // The sidebar drives the section through ?section=: query jobs (the
  // default) or report delivery.
  type Section = 'jobs' | 'reports'
  const section = $derived<Section>(getSection() === 'reports' ? 'reports' : 'jobs')
  const sectionLabel = $derived((PAGE_SECTIONS.schedules ?? []).find((s) => s.id === section)?.label ?? 'Query jobs')

  let schedules = $state<Schedule[]>([])
  let loading = $state(true)
  let savedQueries = $state<SavedQuery[]>([])
  let savedQueriesLoading = $state(false)

  // Create/edit modal
  let showModal = $state(false)
  let editingId = $state<string | null>(null)
  let formName = $state('')
  let formSavedQueryId = $state('')
  let formCron = $state('')
  let formTimezone = $state('UTC')
  let formTimeout = $state(60000)
  let saving = $state(false)

  // Run history, in a sheet for one schedule at a time
  const RUNS_PAGE_SIZE = 10
  let showRunsSheet = $state(false)
  let expandedSchedule = $state<string | null>(null)
  let runs = $state<ScheduleRun[]>([])
  let runsLoading = $state(false)
  let runsLoadingMore = $state(false)
  let runsOffset = $state(0)
  let runsHasMore = $state(false)

  // Run details sheet
  let showRunSheet = $state(false)
  let selectedRun = $state<ScheduleRun | null>(null)
  let selectedSchedule = $state<Schedule | null>(null)

  // Confirm delete
  let confirmOpen = $state(false)
  let confirmLoading = $state(false)
  let pendingDeleteId = $state<string | null>(null)

  // Manual run loading
  let runningId = $state<string | null>(null)

  // A schedule runs on the connection it was created on. Running or editing
  // one of another connection needs a switch there first.
  const session = $derived(getSession())
  let connectionNames = $state<Map<string, string>>(new Map())

  function connectionName(id: string | null): string {
    if (!id || id === session?.connectionId) return session?.connectionName || 'This connection'
    return connectionNames.get(id) ?? 'Another connection'
  }

  /** Why Run now and Edit are off for this schedule, or null when they are on. */
  function switchHint(s: Schedule): string | null {
    const other = otherConnectionId(s.connection_id, session?.connectionId)
    if (!other) return null
    return `Runs on ${connectionName(other)}. Switch to that connection to run or edit it.`
  }

  onMount(async () => {
    await Promise.all([loadSchedules(), loadSavedQueries(), loadConnectionNames().then((m) => (connectionNames = m))])
  })

  const savedQueryMap = $derived.by(() => {
    const map = new Map<string, SavedQuery>()
    for (const q of savedQueries) map.set(q.id, q)
    return map
  })

  async function loadSchedules() {
    loading = true
    try {
      const res = await apiGet<{ schedules: Schedule[] }>('/api/schedules')
      schedules = res.schedules ?? []
    } catch (e: unknown) {
      toastErrorFrom(e)
    } finally {
      loading = false
    }
  }

  async function loadSavedQueries() {
    savedQueriesLoading = true
    try {
      const res = await apiGet<{ saved_queries: SavedQuery[] }>('/api/saved-queries')
      savedQueries = res.saved_queries ?? []
    } catch (e: unknown) {
      toastErrorFrom(e)
    } finally {
      savedQueriesLoading = false
    }
  }

  function openCreateModal() {
    editingId = null
    formName = ''
    formSavedQueryId = ''
    formCron = ''
    formTimezone = 'UTC'
    formTimeout = 60000
    void loadSavedQueries()
    showModal = true
  }

  function openEditModal(s: Schedule) {
    editingId = s.id
    formName = s.name
    formSavedQueryId = s.saved_query_id
    formCron = s.cron
    formTimezone = s.timezone
    formTimeout = s.timeout_ms
    showModal = true
  }

  async function saveSchedule() {
    if (!formName.trim() || !formCron.trim()) {
      toastError('Name and cron expression are required')
      return
    }
    saving = true
    try {
      if (editingId) {
        await apiPut(`/api/schedules/${editingId}`, {
          name: formName.trim(),
          cron: formCron.trim(),
          timezone: formTimezone,
          timeout_ms: formTimeout,
        })
        toastSuccess('Schedule updated')
      } else {
        if (!formSavedQueryId) {
          toastError('Please select a saved query')
          saving = false
          return
        }
        await apiPost('/api/schedules', {
          name: formName.trim(),
          saved_query_id: formSavedQueryId,
          cron: formCron.trim(),
          timezone: formTimezone,
          timeout_ms: formTimeout,
        })
        toastSuccess('Schedule created')
      }
      showModal = false
      await loadSchedules()
    } catch (e: unknown) {
      toastErrorFrom(e)
    } finally {
      saving = false
    }
  }

  function askDeleteSchedule(id: string) {
    pendingDeleteId = id
    confirmOpen = true
  }

  async function performDeleteSchedule(id: string) {
    try {
      await apiDel(`/api/schedules/${id}`)
      schedules = schedules.filter(s => s.id !== id)
      if (expandedSchedule === id) closeRuns()
      toastSuccess('Schedule deleted')
    } catch (e: unknown) {
      toastErrorFrom(e)
    }
  }

  async function confirmDeleteSchedule() {
    if (!pendingDeleteId) return
    confirmLoading = true
    try {
      await performDeleteSchedule(pendingDeleteId)
      confirmOpen = false
      pendingDeleteId = null
    } finally {
      confirmLoading = false
    }
  }

  async function toggleEnabled(s: Schedule) {
    try {
      await apiPut(`/api/schedules/${s.id}`, { enabled: !s.enabled })
      schedules = schedules.map(sc => sc.id === s.id ? { ...sc, enabled: !sc.enabled } : sc)
      toastSuccess(s.enabled ? 'Schedule disabled' : 'Schedule enabled')
    } catch (e: unknown) {
      toastErrorFrom(e)
    }
  }

  async function manualRun(id: string) {
    runningId = id
    try {
      const res = await apiPost<{ success: boolean; error?: string }>(`/api/schedules/${id}/run`)
      if (res.success) {
        toastSuccess('Manual run completed')
      } else {
        toastError(res.error ?? 'Run failed')
      }
      await loadSchedules()
      if (expandedSchedule === id) await loadRuns(id)
    } catch (e: unknown) {
      toastErrorFrom(e)
    } finally {
      runningId = null
    }
  }

  async function loadRuns(id: string, append = false) {
    const offset = append ? runsOffset : 0
    if (append) {
      runsLoadingMore = true
    } else {
      runsLoading = true
      runs = []
      runsOffset = 0
      runsHasMore = false
    }
    try {
      const res = await apiGet<{ runs: ScheduleRun[]; has_more?: boolean; next_offset?: number }>(
        `/api/schedules/${id}/runs?limit=${RUNS_PAGE_SIZE}&offset=${offset}`,
      )
      if (expandedSchedule !== id) return
      const incoming = res.runs ?? []
      runs = append ? [...runs, ...incoming] : incoming
      runsHasMore = !!res.has_more
      runsOffset = typeof res.next_offset === 'number' ? res.next_offset : offset + incoming.length
    } catch (e: unknown) {
      toastErrorFrom(e)
    } finally {
      if (append) {
        runsLoadingMore = false
      } else {
        runsLoading = false
      }
    }
  }

  async function openRuns(id: string) {
    expandedSchedule = id
    showRunsSheet = true
    await loadRuns(id, false)
  }

  function closeRuns() {
    showRunsSheet = false
    expandedSchedule = null
    runs = []
    runsOffset = 0
    runsHasMore = false
    runsLoadingMore = false
  }

  const runsSchedule = $derived(schedules.find((s) => s.id === expandedSchedule) ?? null)

  // The details sheet replaces the runs sheet while open and hands back on close.
  async function openRunDetails(schedule: Schedule, run: ScheduleRun) {
    if (savedQueries.length === 0) await loadSavedQueries()
    selectedSchedule = schedule
    selectedRun = run
    showRunsSheet = false
    showRunSheet = true
  }

  function closeRunDetails() {
    showRunSheet = false
    if (expandedSchedule) showRunsSheet = true
  }

  function openScheduleQueryInEditor() {
    if (!selectedSchedule) return
    const q = savedQueryMap.get(selectedSchedule.saved_query_id)
    if (!q) return
    openSavedQueryTab(q)
    showRunSheet = false
    closeRuns()
  }

  // Newest first, as the API returns them; no initial sort keeps that order.
  const runColumns: DataColumn<Record<string, unknown>>[] = [
    { key: 'started_at', label: 'Started', mono: true, format: (v) => formatDate(v) },
    { key: 'status', label: 'Status' },
    { key: 'elapsed_ms', label: 'Elapsed', align: 'right', format: (v) => `${v ?? 0} ms` },
    { key: 'rows_affected', label: 'Rows', align: 'right', format: (v) => Number(v ?? 0).toLocaleString() },
    { key: 'error', label: 'Error', truncate: true, sortable: false, format: (v) => (v ? String(v) : '-') },
  ]

  type BadgeTone = 'neutral' | 'success' | 'warning' | 'danger' | 'info' | 'brand'
  function statusBadge(status: string | null): { tone: BadgeTone; label: string } {
    switch (status) {
      case 'success': return { tone: 'success', label: 'Success' }
      case 'error': return { tone: 'danger', label: 'Error' }
      case 'running': return { tone: 'brand', label: 'Running' }
      default: return { tone: 'neutral', label: status ?? 'Pending' }
    }
  }

  // The schedule list
  let search = $state('')
  type Row = Record<string, unknown> & Schedule & { target: string; connection: string }

  const rows = $derived.by<Row[]>(() => {
    const q = search.trim().toLowerCase()
    const all = schedules.map((s) => ({
      ...s,
      target: savedQueryMap.get(s.saved_query_id)?.name ?? s.saved_query_name ?? '',
      connection: connectionName(s.connection_id),
    }))
    if (!q) return all
    return all.filter((r) => r.name.toLowerCase().includes(q) || r.target.toLowerCase().includes(q) || r.cron.includes(q) || r.connection.toLowerCase().includes(q))
  })

  function ts(value: string | null): number {
    const n = value ? new Date(value).getTime() : NaN
    return Number.isNaN(n) ? 0 : n
  }

  // API order by default, as the cards had it.
  const scheduleColumns: DataColumn<Row>[] = [
    { key: 'name', label: 'Name', truncate: true },
    { key: 'target', label: 'Saved query', truncate: true },
    { key: 'connection', label: 'Connection', truncate: true, width: '160px' },
    { key: 'cron', label: 'Cron', mono: true, width: '130px' },
    { key: 'timezone', label: 'Timezone', width: '110px' },
    { key: 'last_run_at', label: 'Last run', width: '170px', format: (v) => (v ? formatRelativeTime(v) : 'Never'), sortValue: (row) => ts(row.last_run_at) },
    { key: 'next_run_at', label: 'Next run', width: '170px', format: (v) => formatDate(v), sortValue: (row) => ts(row.next_run_at) },
    { key: 'enabled', label: 'Enabled', width: '80px', sortable: false },
  ]
</script>

<div class="flex h-full min-h-0 flex-col">
  <PageHeader title="Schedules" subtitle={sectionLabel} docs={docsURL('schedules', section)}>
    {#snippet meta()}
      {#if section === 'jobs' && !loading}<Badge>{schedules.length}</Badge>{/if}
    {/snippet}
    {#snippet actions()}
      {#if section === 'jobs'}
        <Button size="sm" onclick={openCreateModal}>
          <Plus size={14} /> Create Schedule
        </Button>
      {/if}
    {/snippet}
  </PageHeader>

  {#if section === 'reports'}
    <div class="min-h-0 flex-1">
      <ReportsSection />
    </div>
  {:else}
  {#if loading}
    <div class="flex flex-1 items-center justify-center py-12"><Spinner /></div>
  {:else if schedules.length === 0}
    <EmptyState
      icon={Clock}
      title="No scheduled queries yet"
      description="Create a schedule to run saved queries automatically."
      primary={{ label: 'Create Schedule', onclick: openCreateModal }}
    />
  {:else}
    <div class="flex h-10 shrink-0 items-center gap-2 border-b border-edge-subtle px-4">
      <div class="relative">
        <Search size={13} class="pointer-events-none absolute left-2.5 top-1/2 -translate-y-1/2 text-fg-4" />
        <Input size="sm" type="search" class="w-64 pl-8" placeholder="Search name, query, cron" bind:value={search} spellcheck={false} />
      </div>
      <span class="ml-auto text-xs text-fg-4">{rows.length} {rows.length === 1 ? 'schedule' : 'schedules'}</span>
    </div>
    <div class="min-h-0 flex-1">
      <DataTable
        fill
        columns={scheduleColumns}
        {rows}
        rowKey={(r) => r.id}
        emptyTitle="Nothing matches"
        emptyDescription="Try another name, saved query or cron expression."
        onrowclick={(r) => (switchHint(r) ? openRuns(r.id) : openEditModal(r))}
      >
        {#snippet cell(row, col, value)}
          {#if col.key === 'name'}
            <span class="font-medium text-fg">{row.name}</span>
          {:else if col.key === 'target'}
            <span class={value ? '' : 'text-fg-4'}>{value || 'Unavailable'}</span>
          {:else if col.key === 'connection'}
            {@const hint = switchHint(row)}
            {#if hint}
              <Badge tone="warning" title={hint}>{value}</Badge>
            {:else}
              <span class="text-fg-2">{value}</span>
            {/if}
          {:else if col.key === 'last_run_at'}
            {@const badge = statusBadge(row.last_status)}
            <span class="inline-flex items-center gap-2">
              <Badge tone={badge.tone} dot>{badge.label}</Badge>
              <span class="text-fg-3" title={row.last_run_at ? formatDate(row.last_run_at) : undefined}>{value}</span>
            </span>
          {:else if col.key === 'enabled'}
            <!-- svelte-ignore a11y_click_events_have_key_events -->
            <span role="presentation" class="inline-flex" onclick={(e: MouseEvent) => e.stopPropagation()}>
              <Switch
                size="sm"
                checked={row.enabled}
                label={row.enabled ? 'Disable schedule' : 'Enable schedule'}
                title={row.enabled ? 'Disable' : 'Enable'}
                onchange={() => toggleEnabled(row)}
              />
            </span>
          {:else}
            {value}
          {/if}
        {/snippet}
        {#snippet actions(row)}
          {@const hint = switchHint(row)}
          <Button icon variant="ghost" size="xs" aria-label="View runs" title="View runs" onclick={(e) => { e.stopPropagation(); openRuns(row.id) }}>
            <History size={13} />
          </Button>
          <Button
            icon
            variant="ghost"
            size="xs"
            aria-label="Run now"
            title={hint ?? 'Run now'}
            disabled={runningId === row.id || !!hint}
            onclick={(e) => { e.stopPropagation(); manualRun(row.id) }}
          >
            {#if runningId === row.id}<Spinner size="sm" />{:else}<Play size={13} />{/if}
          </Button>
          <Button icon variant="ghost" size="xs" aria-label="Edit" title={hint ?? 'Edit'} disabled={!!hint} onclick={(e) => { e.stopPropagation(); openEditModal(row) }}>
            <Pencil size={13} />
          </Button>
          <Button icon variant="ghost" size="xs" aria-label="Delete" title="Delete" class="hover:text-danger" onclick={(e) => { e.stopPropagation(); askDeleteSchedule(row.id) }}>
            <Trash2 size={13} />
          </Button>
        {/snippet}
      </DataTable>
    </div>
  {/if}
  {/if}
</div>

<Sheet open={showModal} title={editingId ? 'Edit Schedule' : 'Create Schedule'} size="sm" onclose={() => showModal = false}>
  {#snippet footer()}
    <Button variant="outline" size="sm" onclick={() => showModal = false}>Cancel</Button>
    <Button size="sm" loading={saving} onclick={saveSchedule} disabled={!editingId && (savedQueriesLoading || savedQueries.length === 0)}>
      {editingId ? 'Update' : 'Create'}
    </Button>
  {/snippet}
  <div class="flex flex-col gap-4">
    {#if !editingId}
      <p class="text-[13px] text-fg-3">Runs on <span class="font-medium text-fg">{connectionName(null)}</span>, the connection you are signed in to.</p>
    {/if}
    <FormField label="Name" for="schedule-name">
      <Input id="schedule-name" placeholder="e.g. Daily Aggregation" bind:value={formName} />
    </FormField>

    {#if !editingId}
      <FormField
        label="Saved Query"
        error={savedQueries.length === 0 && !savedQueriesLoading ? 'No saved queries available. Create one first in Saved Queries.' : undefined}
      >
        <Combobox
          options={savedQueries.map((q) => ({
            value: q.id,
            label: q.name,
            hint: q.description || q.query,
            keywords: `${q.name} ${q.description ?? ''} ${q.query}`,
          }))}
          value={formSavedQueryId}
          emptyText={savedQueriesLoading ? 'Loading saved queries...' : 'No saved queries found'}
          placeholder="Select a saved query..."
          disabled={savedQueriesLoading || savedQueries.length === 0}
          onChange={(id) => formSavedQueryId = id}
        />
      </FormField>
    {/if}

    <FormField label="Cron Expression" for="schedule-cron" hint="e.g. 0 */6 * * * = every 6 hours">
      <Input id="schedule-cron" mono placeholder="0 */6 * * *" bind:value={formCron} />
    </FormField>

    <div class="grid grid-cols-2 gap-3">
      <FormField label="Timezone" for="schedule-timezone">
        <Input id="schedule-timezone" bind:value={formTimezone} />
      </FormField>
      <FormField label="Timeout (ms)" for="schedule-timeout">
        <Input id="schedule-timeout" type="number" bind:value={formTimeout} />
      </FormField>
    </div>
  </div>
</Sheet>

<Sheet
  open={showRunsSheet}
  title="Run history"
  description={runsSchedule ? `${runsSchedule.name}, newest first` : ''}
  size="lg"
  onclose={closeRuns}
>
  {#if runsSchedule}
    {#if runsLoading}
      <div class="flex items-center justify-center py-8"><Spinner size="sm" /></div>
    {:else if runs.length === 0}
      <EmptyState size="compact" title="No runs yet" description="Runs are listed here after the first scheduled or manual run." />
    {:else}
      {@const schedule = runsSchedule}
      <DataTable
        columns={runColumns}
        rows={runs as unknown as Record<string, unknown>[]}
        rowKey={(r) => String(r.id)}
        onrowclick={(r) => openRunDetails(schedule, r as unknown as ScheduleRun)}
      >
        {#snippet cell(row, col, value)}
          {#if col.key === 'status'}
            {@const rb = statusBadge((row as unknown as ScheduleRun).status)}
            <Badge tone={rb.tone}>{rb.label}</Badge>
          {:else if col.key === 'error' && (row as unknown as ScheduleRun).error}
            <span class="text-danger">{value}</span>
          {:else}
            {value}
          {/if}
        {/snippet}
        {#snippet actions(row)}
          <Button size="xs" variant="outline" onclick={(e) => { e.stopPropagation(); openRunDetails(schedule, row as unknown as ScheduleRun) }}>
            <FileText size={12} /> View
          </Button>
        {/snippet}
      </DataTable>
      {#if runsHasMore}
        <div class="mt-2 flex justify-center">
          <Button size="sm" variant="outline" loading={runsLoadingMore} onclick={() => loadRuns(schedule.id, true)}>
            Load {RUNS_PAGE_SIZE} more
          </Button>
        </div>
      {/if}
    {/if}
  {/if}
</Sheet>

<Sheet open={showRunSheet} title="Schedule Run Details" size="lg" onclose={closeRunDetails}>
  {#if selectedRun && selectedSchedule}
    {@const runBadge = statusBadge(selectedRun.status)}
    {@const saved = savedQueryMap.get(selectedSchedule.saved_query_id)}

    <div class="space-y-4">
      <div class="grid grid-cols-2 gap-3">
        <Panel variant="muted" padding="sm">
          <p class="text-[11px] font-medium uppercase tracking-wider text-fg-3">Schedule</p>
          <p class="mt-1 text-[13px] font-semibold text-fg">{selectedSchedule.name}</p>
          <p class="mt-1 font-mono text-xs text-fg-3">{selectedSchedule.cron} ({selectedSchedule.timezone})</p>
        </Panel>
        <Panel variant="muted" padding="sm">
          <p class="text-[11px] font-medium uppercase tracking-wider text-fg-3">Run Status</p>
          <div class="mt-1"><Badge tone={runBadge.tone}>{runBadge.label}</Badge></div>
          <p class="mt-1 text-xs text-fg-3">ID: <span class="font-mono">{selectedRun.id}</span></p>
        </Panel>
      </div>

      <div class="grid grid-cols-2 gap-3 sm:grid-cols-4">
        <Panel variant="muted" padding="sm">
          <p class="text-[11px] font-medium uppercase tracking-wider text-fg-3">Started</p>
          <p class="mt-1 text-[13px] text-fg">{formatDate(selectedRun.started_at)}</p>
        </Panel>
        <Panel variant="muted" padding="sm">
          <p class="text-[11px] font-medium uppercase tracking-wider text-fg-3">Finished</p>
          <p class="mt-1 text-[13px] text-fg">{formatDate(selectedRun.finished_at)}</p>
        </Panel>
        <Panel variant="muted" padding="sm">
          <p class="text-[11px] font-medium uppercase tracking-wider text-fg-3">Elapsed</p>
          <p class="mt-1 text-[13px] tabular-nums text-fg">{selectedRun.elapsed_ms} ms</p>
        </Panel>
        <Panel variant="muted" padding="sm">
          <p class="text-[11px] font-medium uppercase tracking-wider text-fg-3">Rows Affected</p>
          <p class="mt-1 text-[13px] tabular-nums text-fg">{selectedRun.rows_affected}</p>
        </Panel>
      </div>

      {#if selectedRun.error}
        <div class="rounded-md border border-danger/30 bg-danger-soft p-3">
          <p class="mb-1 text-xs font-medium text-danger">Error</p>
          <pre class="whitespace-pre-wrap break-all font-mono text-xs text-danger">{selectedRun.error}</pre>
        </div>
      {/if}

      <Panel variant="muted" padding="sm" title="Saved Query">
        {#snippet actions()}
          {#if saved}
            <Button size="xs" variant="ghost" onclick={openScheduleQueryInEditor}>
              <ExternalLink size={12} /> Open in editor
            </Button>
          {/if}
        {/snippet}
        {#if saved}
          <p class="mb-2 text-[13px] text-fg">{saved.name}</p>
          <pre class="max-h-48 overflow-auto rounded-md border border-edge-subtle bg-surface p-2 font-mono text-xs text-fg-2">{saved.query}</pre>
        {:else if selectedSchedule.saved_query_name}
          <p class="text-[13px] text-fg">{selectedSchedule.saved_query_name}</p>
          <p class="mt-1 text-xs text-fg-3">This query is private to its owner, so only its name is shown.</p>
        {:else}
          <p class="text-xs text-fg-3">Saved query metadata not available.</p>
        {/if}
      </Panel>
    </div>
  {/if}
</Sheet>

<ConfirmDialog
  open={confirmOpen}
  title="Delete schedule?"
  description="This schedule and its run history will be removed permanently."
  confirmLabel="Delete"
  destructive={true}
  loading={confirmLoading}
  onconfirm={confirmDeleteSchedule}
  oncancel={() => confirmOpen = false}
/>
