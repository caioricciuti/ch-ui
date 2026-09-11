<script lang="ts">
  import { onMount } from 'svelte'
  import type { Schedule, ScheduleRun, SavedQuery } from '../lib/types/api'
  import { apiGet, apiPost, apiPut, apiDel } from '../lib/api/client'
  import { success as toastSuccess, error as toastError } from '../lib/stores/toast.svelte'
  import { openSavedQueryTab } from '../lib/stores/tabs.svelte'
  import { formatDate } from '../lib/utils/format'
  import Button from '../lib/components/common/Button.svelte'
  import Combobox from '../lib/components/common/Combobox.svelte'
  import Spinner from '../lib/components/common/Spinner.svelte'
  import Sheet from '../lib/components/common/Sheet.svelte'
  import ConfirmDialog from '../lib/components/common/ConfirmDialog.svelte'
  import PageHeader from '../lib/components/common/PageHeader.svelte'
  import PageBody from '../lib/components/common/PageBody.svelte'
  import Badge from '../lib/components/common/Badge.svelte'
  import EmptyState from '../lib/components/common/EmptyState.svelte'
  import FormField from '../lib/components/common/FormField.svelte'
  import Input from '../lib/components/common/Input.svelte'
  import Panel from '../lib/components/common/Panel.svelte'
  import { Clock, Plus, Play, Trash2, ChevronDown, ChevronRight, FileText, ExternalLink, Pencil } from 'lucide-svelte'

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

  // Run history
  const RUNS_PAGE_SIZE = 10
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

  onMount(async () => {
    await Promise.all([loadSchedules(), loadSavedQueries()])
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
    } catch (e: any) {
      toastError(e.message)
    } finally {
      loading = false
    }
  }

  async function loadSavedQueries() {
    savedQueriesLoading = true
    try {
      const res = await apiGet<{ saved_queries: SavedQuery[] }>('/api/saved-queries')
      savedQueries = res.saved_queries ?? []
    } catch (e: any) {
      toastError(e.message)
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
    } catch (e: any) {
      toastError(e.message)
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
      if (expandedSchedule === id) {
        expandedSchedule = null
        runs = []
        runsOffset = 0
        runsHasMore = false
      }
      toastSuccess('Schedule deleted')
    } catch (e: any) {
      toastError(e.message)
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
    } catch (e: any) {
      toastError(e.message)
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
    } catch (e: any) {
      toastError(e.message)
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
    } catch (e: any) {
      toastError(e.message)
    } finally {
      if (append) {
        runsLoadingMore = false
      } else {
        runsLoading = false
      }
    }
  }

  async function toggleRuns(id: string) {
    if (expandedSchedule === id) {
      expandedSchedule = null
      runs = []
      runsOffset = 0
      runsHasMore = false
      runsLoadingMore = false
      return
    }
    expandedSchedule = id
    await loadRuns(id, false)
  }

  async function openRunDetails(schedule: Schedule, run: ScheduleRun) {
    if (savedQueries.length === 0) await loadSavedQueries()
    selectedSchedule = schedule
    selectedRun = run
    showRunSheet = true
  }

  function openScheduleQueryInEditor() {
    if (!selectedSchedule) return
    const q = savedQueryMap.get(selectedSchedule.saved_query_id)
    if (!q) return
    openSavedQueryTab(q)
    showRunSheet = false
  }

  type BadgeTone = 'neutral' | 'success' | 'warning' | 'danger' | 'info' | 'brand'
  function statusBadge(status: string | null): { tone: BadgeTone; label: string } {
    switch (status) {
      case 'success': return { tone: 'success', label: 'Success' }
      case 'error': return { tone: 'danger', label: 'Error' }
      case 'running': return { tone: 'brand', label: 'Running' }
      default: return { tone: 'neutral', label: status ?? 'Pending' }
    }
  }
</script>

<div class="flex h-full min-h-0 flex-col">
  <PageHeader title="Scheduled Queries" subtitle="Run saved queries on a cron, with run history.">
    {#snippet meta()}
      {#if !loading}<Badge>{schedules.length}</Badge>{/if}
    {/snippet}
    {#snippet actions()}
      <Button size="sm" onclick={openCreateModal}>
        <Plus size={14} /> Create Schedule
      </Button>
    {/snippet}
  </PageHeader>

  <PageBody width="md">
    {#if loading}
      <div class="flex items-center justify-center py-12"><Spinner /></div>
    {:else if schedules.length === 0}
      <EmptyState
        icon={Clock}
        title="No scheduled queries yet"
        description="Create a schedule to run saved queries automatically."
        primary={{ label: 'Create Schedule', onclick: openCreateModal }}
      />
    {:else}
      <div class="flex flex-col gap-3">
        {#each schedules as schedule (schedule.id)}
          {@const badge = statusBadge(schedule.last_status)}
          {@const queryRef = savedQueryMap.get(schedule.saved_query_id)}
          <div class="overflow-hidden rounded-lg border border-edge-subtle bg-surface">
            <div class="flex items-center gap-3 p-3 {expandedSchedule === schedule.id ? 'border-b border-edge-subtle' : ''}">
              <Button
                icon
                variant="ghost"
                size="xs"
                aria-label={expandedSchedule === schedule.id ? 'Hide runs' : 'View runs'}
                onclick={() => toggleRuns(schedule.id)}
              >
                {#if expandedSchedule === schedule.id}<ChevronDown size={14} />{:else}<ChevronRight size={14} />{/if}
              </Button>

              <div class="min-w-0 flex-1">
                <div class="flex flex-wrap items-center gap-2">
                  <span class="text-[13px] font-semibold text-fg">{schedule.name}</span>
                  <Badge class="font-mono">{schedule.cron}</Badge>
                  <span class="text-xs text-fg-4">{schedule.timezone}</span>
                </div>
                <div class="mt-1 flex flex-wrap items-center gap-3 text-xs text-fg-3">
                  <Badge tone={badge.tone} dot>{badge.label}</Badge>
                  <span>Last: {formatDate(schedule.last_run_at)}</span>
                  <span>Next: {formatDate(schedule.next_run_at)}</span>
                  {#if queryRef}
                    <span class="text-fg-4">{queryRef.name}</span>
                  {/if}
                </div>
              </div>

              <button
                role="switch"
                aria-checked={schedule.enabled}
                aria-label={schedule.enabled ? 'Disable schedule' : 'Enable schedule'}
                class="relative h-5 w-9 shrink-0 rounded-full transition-colors {schedule.enabled ? 'bg-accent' : 'bg-edge-strong'}"
                onclick={() => toggleEnabled(schedule)}
                title={schedule.enabled ? 'Disable' : 'Enable'}
              >
                <span class="absolute left-0.5 top-0.5 h-4 w-4 rounded-full bg-white shadow-sm transition-transform {schedule.enabled ? 'translate-x-4' : ''}"></span>
              </button>

              <div class="flex items-center gap-0.5">
                <Button
                  icon
                  variant="ghost"
                  size="sm"
                  aria-label="Run now"
                  title="Run now"
                  onclick={() => manualRun(schedule.id)}
                  disabled={runningId === schedule.id}
                >
                  {#if runningId === schedule.id}<Spinner size="sm" />{:else}<Play size={14} />{/if}
                </Button>
                <Button icon variant="ghost" size="sm" aria-label="Edit" title="Edit" onclick={() => openEditModal(schedule)}>
                  <Pencil size={14} />
                </Button>
                <Button icon variant="ghost" size="sm" aria-label="Delete" title="Delete" class="hover:text-danger" onclick={() => askDeleteSchedule(schedule.id)}>
                  <Trash2 size={14} />
                </Button>
              </div>
            </div>

            {#if expandedSchedule === schedule.id}
              <div class="px-3 py-3">
                {#if runsLoading}
                  <div class="flex items-center justify-center py-4"><Spinner size="sm" /></div>
                {:else if runs.length === 0}
                  <p class="py-2 text-xs text-fg-3">No runs yet</p>
                {:else}
                  <div class="ds-table-wrap">
                    <table class="ds-table text-xs">
                      <thead>
                        <tr class="ds-table-head-row">
                          <th class="ds-table-th-compact">Started</th>
                          <th class="ds-table-th-compact">Status</th>
                          <th class="ds-table-th-right-compact">Elapsed</th>
                          <th class="ds-table-th-right-compact">Rows</th>
                          <th class="ds-table-th-compact">Error</th>
                          <th class="ds-table-th-right-compact">Details</th>
                        </tr>
                      </thead>
                      <tbody>
                        {#each runs as run}
                          {@const rb = statusBadge(run.status)}
                          <tr class="ds-table-row">
                            <td class="ds-td-compact">{formatDate(run.started_at)}</td>
                            <td class="ds-td-compact"><Badge tone={rb.tone}>{rb.label}</Badge></td>
                            <td class="ds-td-compact text-right tabular-nums">{run.elapsed_ms}ms</td>
                            <td class="ds-td-compact text-right tabular-nums">{run.rows_affected}</td>
                            <td class="ds-td-compact max-w-xs truncate text-danger">{run.error ?? '—'}</td>
                            <td class="ds-td-compact text-right">
                              <Button size="xs" variant="outline" onclick={() => openRunDetails(schedule, run)}>
                                <FileText size={12} /> View
                              </Button>
                            </td>
                          </tr>
                        {/each}
                      </tbody>
                    </table>
                  </div>
                  {#if runsHasMore}
                    <div class="mt-2 flex justify-center">
                      <Button size="sm" variant="outline" loading={runsLoadingMore} onclick={() => loadRuns(schedule.id, true)}>
                        Load {RUNS_PAGE_SIZE} more
                      </Button>
                    </div>
                  {/if}
                {/if}
              </div>
            {/if}
          </div>
        {/each}
      </div>
    {/if}
  </PageBody>
</div>

<Sheet open={showModal} title={editingId ? 'Edit Schedule' : 'Create Schedule'} size="sm" onclose={() => showModal = false}>
  {#snippet footer()}
    <Button variant="outline" size="sm" onclick={() => showModal = false}>Cancel</Button>
    <Button size="sm" loading={saving} onclick={saveSchedule} disabled={!editingId && (savedQueriesLoading || savedQueries.length === 0)}>
      {editingId ? 'Update' : 'Create'}
    </Button>
  {/snippet}
  <div class="flex flex-col gap-4">
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

<Sheet open={showRunSheet} title="Schedule Run Details" size="lg" onclose={() => showRunSheet = false}>
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
