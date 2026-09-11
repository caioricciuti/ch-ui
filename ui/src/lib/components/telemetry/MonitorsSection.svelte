<script lang="ts">
  import { onMount } from 'svelte'
  import { Plus, Play, Pencil, Trash2, RefreshCw, BellRing, Search } from 'lucide-svelte'
  import Button from '../common/Button.svelte'
  import Input from '../common/Input.svelte'
  import Badge from '../common/Badge.svelte'
  import Spinner from '../common/Spinner.svelte'
  import EmptyState from '../common/EmptyState.svelte'
  import ConfirmDialog from '../common/ConfirmDialog.svelte'
  import DataTable, { type DataColumn } from '../common/DataTable.svelte'
  import MonitorEditor from './MonitorEditor.svelte'
  import { getSession } from '../../stores/session.svelte'
  import { success as toastSuccess, error as toastError } from '../../stores/toast.svelte'
  import { listMonitors, createMonitor, updateMonitor, deleteMonitor, runMonitor } from '../../api/telemetry'
  import { formatRelativeTime, formatDate } from '../../utils/format'
  import type { Monitor, MonitorInput, TelemetrySource } from '../../types/telemetry'

  /** Monitors: saved searches with a threshold, evaluated on a schedule. */
  interface Props {
    sources: TelemetrySource[]
  }
  let { sources }: Props = $props()

  const canWrite = $derived(getSession()?.role !== 'viewer')

  let monitors = $state<Monitor[]>([])
  let loading = $state(true)
  let search = $state('')
  let editorOpen = $state(false)
  let editing = $state<Monitor | null>(null)
  let saving = $state(false)
  let deleting = $state<Monitor | null>(null)
  let deleteLoading = $state(false)
  let running = $state<string | null>(null)

  type Row = Monitor & Record<string, unknown> & { condition: string; state_label: string }

  async function load() {
    loading = true
    try {
      monitors = await listMonitors()
    } catch (e: unknown) {
      toastError(e instanceof Error ? e.message : String(e))
    } finally {
      loading = false
    }
  }
  onMount(load)

  const COMPARATOR: Record<string, string> = { gt: '>', gte: '>=', lt: '<', lte: '<=' }

  function dur(seconds: number): string {
    if (seconds % 86400 === 0) return `${seconds / 86400}d`
    if (seconds % 3600 === 0) return `${seconds / 3600}h`
    if (seconds % 60 === 0) return `${seconds / 60}m`
    return `${seconds}s`
  }

  function condition(m: Monitor): string {
    return `count ${COMPARATOR[m.comparator] ?? m.comparator} ${m.threshold} in ${dur(m.window_seconds)} every ${dur(m.interval_seconds)}`
  }

  const rows = $derived.by<Row[]>(() => {
    const term = search.trim().toLowerCase()
    return monitors
      .filter((m) => !term || m.name.toLowerCase().includes(term) || m.query.toLowerCase().includes(term))
      .map((m) => ({ ...m, condition: condition(m), state_label: m.last_state }))
  })

  const columns: DataColumn<Row>[] = [
    { key: 'name', label: 'Name', width: '22%' },
    { key: 'kind', label: 'Signal', width: '80px' },
    { key: 'query', label: 'Query', mono: true, truncate: true },
    { key: 'condition', label: 'Condition', width: '220px', sortable: false },
    { key: 'severity', label: 'Severity', width: '90px' },
    { key: 'state_label', label: 'State', width: '150px' },
    { key: 'last_run_at', label: 'Last run', width: '110px', format: (v) => (v ? formatRelativeTime(v) : 'never') },
    { key: 'enabled', label: 'Enabled', width: '90px', format: (v) => (v ? 'On' : 'Off') },
  ]

  function stateTone(s: string): 'success' | 'danger' | 'warning' | 'neutral' {
    if (s === 'firing') return 'danger'
    if (s === 'error') return 'warning'
    if (s === 'ok') return 'success'
    return 'neutral'
  }

  function severityTone(s: string): 'info' | 'warning' | 'danger' | 'neutral' {
    if (s === 'critical' || s === 'error') return 'danger'
    if (s === 'warn') return 'warning'
    if (s === 'info') return 'info'
    return 'neutral'
  }

  function openNew() {
    editing = null
    editorOpen = true
  }

  function openEdit(m: Monitor) {
    editing = m
    editorOpen = true
  }

  async function save(input: MonitorInput) {
    saving = true
    try {
      if (editing) {
        const updated = await updateMonitor(editing.id, input)
        monitors = monitors.map((m) => (m.id === updated.id ? updated : m))
        toastSuccess('Monitor updated')
      } else {
        const created = await createMonitor(input)
        monitors = [created, ...monitors]
        toastSuccess('Monitor created')
      }
      editorOpen = false
      editing = null
    } catch (e: unknown) {
      toastError(e instanceof Error ? e.message : String(e))
    } finally {
      saving = false
    }
  }

  async function confirmDelete() {
    const t = deleting
    if (!t) return
    deleteLoading = true
    try {
      await deleteMonitor(t.id)
      monitors = monitors.filter((m) => m.id !== t.id)
      toastSuccess('Monitor deleted')
      deleting = null
    } catch (e: unknown) {
      toastError(e instanceof Error ? e.message : String(e))
    } finally {
      deleteLoading = false
    }
  }

  async function runNow(m: Monitor) {
    running = m.id
    try {
      const res = await runMonitor(m.id)
      if (res.monitor) monitors = monitors.map((x) => (x.id === m.id ? res.monitor : x))
      if (res.firing) toastError(`"${m.name}": ${res.value} matches, condition met`)
      else toastSuccess(`"${m.name}": ${res.value} matches, condition not met`)
    } catch (e: unknown) {
      toastError(e instanceof Error ? e.message : String(e))
    } finally {
      running = null
    }
  }
</script>

<div class="flex h-full min-h-0 flex-col">
  <div class="flex h-10 shrink-0 items-center gap-2 border-b border-edge-subtle px-5">
    <div class="relative">
      <Search size={13} class="pointer-events-none absolute left-2.5 top-1/2 -translate-y-1/2 text-fg-4" />
      <Input size="sm" class="w-64 pl-8" placeholder="Search monitors" bind:value={search} />
    </div>
    <span class="ml-auto text-xs text-fg-4 tabular-nums">{rows.length} {rows.length === 1 ? 'monitor' : 'monitors'}</span>
    <Button icon variant="ghost" size="sm" aria-label="Refresh" title="Refresh" onclick={load}>
      <RefreshCw size={14} class={loading ? 'animate-spin' : ''} />
    </Button>
    {#if canWrite}
      <Button size="sm" onclick={openNew}><Plus size={14} /> New monitor</Button>
    {/if}
  </div>

  <div class="min-h-0 flex-1 px-5 pb-4 pt-3">
    {#if loading && monitors.length === 0}
      <div class="flex h-full items-center justify-center"><Spinner /></div>
    {:else if monitors.length === 0}
      <EmptyState
        icon={BellRing}
        title="No monitors yet"
        description="A monitor runs a search on a schedule and raises an alert event when the number of matches crosses a threshold. Rules in Governance › Alerts deliver it."
        primary={canWrite ? { label: 'New monitor', onclick: openNew } : undefined}
      />
    {:else}
      <div class="h-full overflow-hidden rounded-lg border border-edge-subtle bg-surface">
        <DataTable fill framed={false} {columns} {rows} rowKey={(r) => r.id} sort={{ key: 'name', dir: 'asc' }} emptyTitle="Nothing matches">
          {#snippet cell(row, col, value)}
            {#if col.key === 'name'}
              <span class="font-medium text-fg">{row.name}</span>
            {:else if col.key === 'kind'}
              <Badge tone={row.kind === 'logs' ? 'brand' : 'info'}>{row.kind}</Badge>
            {:else if col.key === 'severity'}
              <Badge tone={severityTone(row.severity)}>{row.severity}</Badge>
            {:else if col.key === 'state_label'}
              <span class="inline-flex items-center gap-1.5" title={row.last_error ?? undefined}>
                <Badge tone={stateTone(row.last_state)}>{row.last_state}</Badge>
                {#if row.last_value !== null && row.last_value !== undefined}
                  <span class="text-xs text-fg-4 tabular-nums">{row.last_value}</span>
                {/if}
              </span>
            {:else if col.key === 'last_run_at'}
              <span title={row.last_run_at ? formatDate(row.last_run_at) : undefined}>{value}</span>
            {:else if col.key === 'enabled'}
              <Badge dot tone={row.enabled ? 'success' : 'neutral'}>{value}</Badge>
            {:else if col.key === 'query'}
              <span class="text-fg-2">{row.query || '(everything)'}</span>
            {:else}
              {value}
            {/if}
          {/snippet}
          {#snippet actions(row)}
            {#if canWrite}
              <Button icon variant="ghost" size="xs" aria-label="Run now" title="Run now" loading={running === row.id} onclick={() => runNow(row)}><Play size={13} /></Button>
              <Button icon variant="ghost" size="xs" aria-label="Edit monitor" title="Edit" onclick={() => openEdit(row)}><Pencil size={13} /></Button>
              <Button icon variant="ghost" size="xs" aria-label="Delete monitor" title="Delete" onclick={() => (deleting = row)}><Trash2 size={13} /></Button>
            {/if}
          {/snippet}
        </DataTable>
      </div>
    {/if}
  </div>
</div>

<MonitorEditor open={editorOpen} monitor={editing} {sources} {saving} onsave={save} oncancel={() => { editorOpen = false; editing = null }} />

<ConfirmDialog
  open={!!deleting}
  title="Delete monitor?"
  description={deleting ? `"${deleting.name}" stops being evaluated. Past alert events stay.` : ''}
  confirmLabel="Delete"
  destructive
  loading={deleteLoading}
  onconfirm={confirmDelete}
  oncancel={() => (deleting = null)}
/>
