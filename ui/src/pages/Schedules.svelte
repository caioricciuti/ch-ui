<script lang="ts">
  import { onMount } from 'svelte'
  import type { Schedule, ScheduleRun, SavedQuery } from '../lib/types/api'
  import { apiGet, apiPost, apiPut, apiDel } from '../lib/api/client'
  import { success as toastSuccess, error as toastError } from '../lib/stores/toast.svelte'
  import { openSavedQueryTab } from '../lib/stores/tabs.svelte'
  import Button from '../lib/components/common/Button.svelte'
  import Combobox from '../lib/components/common/Combobox.svelte'
  import Spinner from '../lib/components/common/Spinner.svelte'
  import Sheet from '../lib/components/common/Sheet.svelte'
  import ConfirmDialog from '../lib/components/common/ConfirmDialog.svelte'
  import { Clock, Plus, Play, Trash2, ChevronDown, ChevronRight, FileText, ExternalLink } from 'lucide-svelte'

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

  function formatTime(ts: string | null): string {
    if (!ts) return '—'
    try {
      return new Date(ts).toLocaleString()
    } catch {
      return ts
    }
  }

  function statusBadge(status: string | null): { cls: string; label: string } {
    switch (status) {
      case 'success': return { cls: 'bg-emerald-100 dark:bg-emerald-900/30 text-emerald-700 dark:text-emerald-300', label: 'Success' }
      case 'error': return { cls: 'bg-red-100 dark:bg-red-900/30 text-red-700 dark:text-red-300', label: 'Error' }
      case 'running': return { cls: 'bg-orange-100 dark:bg-orange-900/30 text-orange-700 dark:text-orange-300', label: 'Running' }
      default: return { cls: 'bg-gray-100 dark:bg-gray-800 text-gray-500', label: status ?? 'Pending' }
    }
  }
</script>

<div class="flex flex-col h-full">
  <div class="ds-page-header">
    <div class="flex items-center gap-3">
      <Clock size={18} class="text-ch-orange" />
      <h1 class="ds-page-title">Scheduled Queries</h1>
    </div>
    <Button size="sm" onclick={openCreateModal}>
      <Plus size={14} /> Create Schedule
    </Button>
  </div>

  <div class="flex-1 overflow-auto p-4">
    {#if loading}
      <div class="flex items-center justify-center py-12"><Spinner /></div>
    {:else if schedules.length === 0}
      <div class="ds-empty text-gray-500">
        <Clock size={36} class="mx-auto mb-2 text-gray-300 dark:text-gray-700" />
        <p class="mb-1">No scheduled queries yet</p>
        <p class="text-xs text-gray-400 dark:text-gray-600">Create a schedule to run saved queries automatically</p>
      </div>
    {:else}
      <div class="flex flex-col gap-2">
        {#each schedules as schedule (schedule.id)}
          {@const badge = statusBadge(schedule.last_status)}
          {@const queryRef = savedQueryMap.get(schedule.saved_query_id)}
          <div class="ds-panel rounded-xl overflow-hidden">
            <div class="flex items-center gap-3 p-3 border-b border-gray-200/70 dark:border-gray-800/70">
              <button
                class="text-gray-400 hover:text-gray-600 dark:hover:text-gray-300"
                onclick={() => toggleRuns(schedule.id)}
                title="View runs"
              >
                {#if expandedSchedule === schedule.id}<ChevronDown size={14} />{:else}<ChevronRight size={14} />{/if}
              </button>

              <div class="flex-1 min-w-0">
                <div class="flex items-center gap-2 flex-wrap">
                  <span class="text-sm font-semibold text-gray-800 dark:text-gray-200">{schedule.name}</span>
                  <code class="ds-badge ds-badge-neutral font-mono">{schedule.cron}</code>
                  <span class="text-xs text-gray-400">{schedule.timezone}</span>
                </div>
                <div class="flex items-center gap-3 mt-1 text-xs text-gray-500 flex-wrap">
                  <span class="px-1.5 py-0.5 rounded {badge.cls}">{badge.label}</span>
                  <span>Last: {formatTime(schedule.last_run_at)}</span>
                  <span>Next: {formatTime(schedule.next_run_at)}</span>
                  {#if queryRef}
                    <span class="text-gray-400">{queryRef.name}</span>
                  {/if}
                </div>
              </div>

              <button
                class="relative w-9 h-5 rounded-full transition-colors {schedule.enabled ? 'bg-ch-blue' : 'bg-gray-300 dark:bg-gray-700'}"
                onclick={() => toggleEnabled(schedule)}
                title={schedule.enabled ? 'Disable' : 'Enable'}
              >
                <span class="absolute top-0.5 left-0.5 w-4 h-4 rounded-full bg-white transition-transform {schedule.enabled ? 'translate-x-4' : ''}"></span>
              </button>

              <div class="flex items-center gap-1">
                <button
                  class="ds-icon-btn disabled:opacity-50"
                  onclick={() => manualRun(schedule.id)}
                  disabled={runningId === schedule.id}
                  title="Run now"
                >
                  {#if runningId === schedule.id}<Spinner size="sm" />{:else}<Play size={14} />{/if}
                </button>
                <button
                  class="ds-icon-btn hover:text-gray-700 dark:hover:text-gray-300"
                  onclick={() => openEditModal(schedule)}
                  title="Edit"
                >
                  <Clock size={14} />
                </button>
                <button
                  class="ds-icon-btn hover:text-red-500 dark:hover:text-red-400"
                  onclick={() => askDeleteSchedule(schedule.id)}
                  title="Delete"
                >
                  <Trash2 size={14} />
                </button>
              </div>
            </div>

            {#if expandedSchedule === schedule.id}
              <div class="px-3 py-3">
                {#if runsLoading}
                  <div class="flex items-center justify-center py-4"><Spinner size="sm" /></div>
                {:else if runs.length === 0}
                  <p class="text-xs text-gray-500 py-2">No runs yet</p>
                {:else}
                  <div class="overflow-x-auto">
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
                            <td class="ds-td-compact">{formatTime(run.started_at)}</td>
                            <td class="ds-td-compact"><span class="ds-badge {rb.cls}">{rb.label}</span></td>
                            <td class="ds-td-compact text-right">{run.elapsed_ms}ms</td>
                            <td class="ds-td-compact text-right">{run.rows_affected}</td>
                            <td class="ds-td-compact text-red-500 max-w-xs truncate">{run.error ?? '—'}</td>
                            <td class="ds-td-compact text-right">
                              <button
                                class="ds-btn-outline px-2 py-1"
                                onclick={() => openRunDetails(schedule, run)}
                              >
                                <FileText size={12} /> View
                              </button>
                            </td>
                          </tr>
                        {/each}
                      </tbody>
                    </table>
                  </div>
                  {#if runsHasMore}
                    <div class="mt-2 flex justify-center">
                      <button
                        class="ds-btn-outline px-3 py-1.5 disabled:opacity-60"
                        onclick={() => loadRuns(schedule.id, true)}
                        disabled={runsLoadingMore}
                      >
                        {#if runsLoadingMore}<Spinner size="sm" />{:else}Load {RUNS_PAGE_SIZE} more{/if}
                      </button>
                    </div>
                  {/if}
                {/if}
              </div>
            {/if}
          </div>
        {/each}
      </div>
    {/if}
  </div>
</div>

<Sheet open={showModal} title={editingId ? 'Edit Schedule' : 'Create Schedule'} size="sm" onclose={() => showModal = false}>
  <div class="flex flex-col gap-3">
    <div>
      <label for="schedule-name" class="ds-form-label">Name</label>
      <input
        id="schedule-name"
        type="text"
        class="ds-input"
        placeholder="e.g. Daily Aggregation"
        bind:value={formName}
      />
    </div>

    {#if !editingId}
      <div>
        <p class="ds-form-label">Saved Query</p>
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
        {#if savedQueries.length === 0}
          <p class="mt-1 text-xs text-amber-500">No saved queries available. Create one first in Saved Queries.</p>
        {/if}
      </div>
    {/if}

    <div>
      <label for="schedule-cron" class="ds-form-label">Cron Expression</label>
      <input
        id="schedule-cron"
        type="text"
        class="ds-input font-mono"
        placeholder="0 */6 * * *"
        bind:value={formCron}
      />
      <p class="text-xs text-gray-400 mt-1">e.g. <code>0 */6 * * *</code> = every 6 hours</p>
    </div>

    <div class="flex gap-3">
      <div class="flex-1">
        <label for="schedule-timezone" class="ds-form-label">Timezone</label>
        <input
          id="schedule-timezone"
          type="text"
          class="ds-input"
          bind:value={formTimezone}
        />
      </div>
      <div class="flex-1">
        <label for="schedule-timeout" class="ds-form-label">Timeout (ms)</label>
        <input
          id="schedule-timeout"
          type="number"
          class="ds-input"
          bind:value={formTimeout}
        />
      </div>
    </div>

    <div class="flex justify-end gap-2 pt-2">
      <Button variant="secondary" size="sm" onclick={() => showModal = false}>Cancel</Button>
      <Button size="sm" loading={saving} onclick={saveSchedule} disabled={!editingId && (savedQueriesLoading || savedQueries.length === 0)}>
        {editingId ? 'Update' : 'Create'}
      </Button>
    </div>
  </div>
</Sheet>

<Sheet open={showRunSheet} title="Schedule Run Details" size="lg" onclose={() => showRunSheet = false}>
  {#if selectedRun && selectedSchedule}
    {@const runBadge = statusBadge(selectedRun.status)}
    {@const saved = savedQueryMap.get(selectedSchedule.saved_query_id)}

    <div class="space-y-5">
      <div class="grid grid-cols-2 gap-3">
        <div class="surface-card rounded-lg p-3">
          <p class="text-xs text-gray-500 mb-1">Schedule</p>
          <p class="text-sm font-semibold text-gray-800 dark:text-gray-100">{selectedSchedule.name}</p>
          <p class="text-xs text-gray-500 mt-1 font-mono">{selectedSchedule.cron} ({selectedSchedule.timezone})</p>
        </div>
        <div class="surface-card rounded-lg p-3">
          <p class="text-xs text-gray-500 mb-1">Run Status</p>
          <span class="inline-flex px-2 py-1 rounded text-xs font-medium {runBadge.cls}">{runBadge.label}</span>
          <p class="text-xs text-gray-500 mt-1">ID: <span class="font-mono">{selectedRun.id}</span></p>
        </div>
      </div>

      <div class="grid grid-cols-2 gap-3">
        <div class="surface-card rounded-lg p-3">
          <p class="text-xs text-gray-500">Started</p>
          <p class="text-sm text-gray-800 dark:text-gray-100 mt-1">{formatTime(selectedRun.started_at)}</p>
        </div>
        <div class="surface-card rounded-lg p-3">
          <p class="text-xs text-gray-500">Finished</p>
          <p class="text-sm text-gray-800 dark:text-gray-100 mt-1">{formatTime(selectedRun.finished_at)}</p>
        </div>
        <div class="surface-card rounded-lg p-3">
          <p class="text-xs text-gray-500">Elapsed</p>
          <p class="text-sm text-gray-800 dark:text-gray-100 mt-1">{selectedRun.elapsed_ms} ms</p>
        </div>
        <div class="surface-card rounded-lg p-3">
          <p class="text-xs text-gray-500">Rows Affected</p>
          <p class="text-sm text-gray-800 dark:text-gray-100 mt-1">{selectedRun.rows_affected}</p>
        </div>
      </div>

      {#if selectedRun.error}
        <div class="surface-card rounded-lg p-3 border-red-400/40">
          <p class="text-xs font-medium text-red-500 mb-1">Error</p>
          <pre class="text-xs text-red-400 whitespace-pre-wrap break-all font-mono">{selectedRun.error}</pre>
        </div>
      {/if}

      <div class="surface-card rounded-lg p-3">
        <div class="flex items-center justify-between gap-2 mb-2">
          <p class="text-xs font-medium text-gray-600 dark:text-gray-300">Saved Query</p>
          {#if saved}
            <button class="inline-flex items-center gap-1 text-xs text-ch-blue hover:underline" onclick={openScheduleQueryInEditor}>
              <ExternalLink size={12} /> Open in editor
            </button>
          {/if}
        </div>
        {#if saved}
          <p class="text-sm text-gray-800 dark:text-gray-100 mb-2">{saved.name}</p>
          <pre class="text-xs font-mono text-gray-600 dark:text-gray-300 bg-gray-100 dark:bg-gray-900 rounded p-2 overflow-auto max-h-48">{saved.query}</pre>
        {:else}
          <p class="text-xs text-gray-500">Saved query metadata not available.</p>
        {/if}
      </div>
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
