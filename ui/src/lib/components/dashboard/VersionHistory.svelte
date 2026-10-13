<script lang="ts">
  import { onMount } from 'svelte'
  import type { DashboardVersion } from '../../types/api'
  import {
    listDashboardVersions, getDashboardVersion, restoreDashboardVersion, type DashboardDetail,
  } from '../../api/dashboards'
  import { success as toastSuccess, error as toastError } from '../../stores/toast.svelte'
  import { formatDate, formatRelativeTime } from '../../utils/format'
  import DataTable, { type DataColumn } from '../common/DataTable.svelte'
  import ConfirmDialog from '../common/ConfirmDialog.svelte'
  import EmptyState from '../common/EmptyState.svelte'
  import Modal from '../common/Modal.svelte'
  import Button from '../common/Button.svelte'
  import Badge from '../common/Badge.svelte'
  import Spinner from '../common/Spinner.svelte'
  import { Eye, RotateCcw, Copy } from 'lucide-svelte'

  interface Props {
    dashboardId: string
    /** Receives the dashboard as restored; the server already saved it as a new version. */
    onrestored: (detail: DashboardDetail) => void
  }

  let { dashboardId, onrestored }: Props = $props()

  type Row = Record<string, unknown> & DashboardVersion

  let versions = $state<DashboardVersion[]>([])
  let loading = $state(true)

  const newest = $derived(versions.reduce((max, v) => Math.max(max, v.version), 0))
  const rows = $derived<Row[]>(versions.map((v) => ({ ...v })))

  const columns: DataColumn<Row>[] = [
    { key: 'version', label: 'Version', width: '80px', format: (v) => `v${String(v)}` },
    { key: 'created_at', label: 'When', width: '110px', format: (v) => formatRelativeTime(v) },
    { key: 'created_by', label: 'Who', width: '140px', truncate: true, format: (v) => (v ? String(v) : 'Unknown') },
    { key: 'note', label: 'Note', truncate: true },
    { key: 'panel_count', label: 'Panels', align: 'right', width: '70px' },
  ]

  async function load() {
    loading = true
    try {
      versions = await listDashboardVersions(dashboardId)
    } catch (e: unknown) {
      toastError(e instanceof Error ? e.message : String(e))
    } finally {
      loading = false
    }
  }

  // The parent mounts this component when the History tab opens, so every
  // visit reloads the list.
  onMount(() => {
    void load()
  })

  // ── View ───────────────────────────────────────────────────
  let viewing = $state<DashboardVersion | null>(null)
  let viewJson = $state('')
  let viewLoading = $state(false)

  async function openView(v: DashboardVersion) {
    viewing = v
    viewJson = ''
    viewLoading = true
    try {
      const full = await getDashboardVersion(dashboardId, v.version)
      viewJson = JSON.stringify(full.snapshot ?? {}, null, 2)
    } catch (e: unknown) {
      viewing = null
      toastError(e instanceof Error ? e.message : String(e))
    } finally {
      viewLoading = false
    }
  }

  function copyView() {
    navigator.clipboard.writeText(viewJson)
    toastSuccess('Copied to clipboard')
  }

  // ── Restore ────────────────────────────────────────────────
  let restoring = $state<DashboardVersion | null>(null)
  let restoreSaving = $state(false)

  async function confirmRestore() {
    if (!restoring) return
    restoreSaving = true
    const n = restoring.version
    try {
      const detail = await restoreDashboardVersion(dashboardId, n)
      restoring = null
      toastSuccess(`Restored version ${n}`)
      onrestored(detail)
      await load()
    } catch (e: unknown) {
      toastError(e instanceof Error ? e.message : String(e))
    } finally {
      restoreSaving = false
    }
  }
</script>

{#if loading && versions.length === 0}
  <div class="flex items-center justify-center py-16"><Spinner /></div>
{:else if versions.length === 0}
  <EmptyState
    size="compact"
    title="No versions yet"
    description="Every save of the dashboard, its panels or variables is kept here. Make a change and it shows up."
  />
{:else}
  <DataTable
    {columns}
    {rows}
    rowKey={(r) => r.id}
    sort={{ key: 'version', dir: 'desc' }}
    onrowclick={(r) => void openView(r)}
  >
    {#snippet cell(row, col, value)}
      {#if col.key === 'version'}
        <span class="inline-flex items-center gap-1.5">
          <span class="tabular-nums">{value}</span>
          {#if row.version === newest}<Badge tone="success">Current</Badge>{/if}
        </span>
      {:else if col.key === 'created_at'}
        <span title={formatDate(row.created_at)}>{value}</span>
      {:else if col.key === 'note'}
        <span class={row.note ? '' : 'text-fg-4'}>{row.note || 'No note'}</span>
      {:else}
        {value}
      {/if}
    {/snippet}
    {#snippet actions(row)}
      <Button icon variant="ghost" size="xs" aria-label="View JSON" title="View JSON" onclick={(e) => { e.stopPropagation(); void openView(row) }}>
        <Eye size={13} />
      </Button>
      {#if row.version !== newest}
        <Button icon variant="ghost" size="xs" aria-label="Restore" title="Restore this version" onclick={(e) => { e.stopPropagation(); restoring = row }}>
          <RotateCcw size={13} />
        </Button>
      {/if}
    {/snippet}
  </DataTable>
{/if}

<Modal
  open={!!viewing}
  title={viewing ? `Version ${viewing.version}` : ''}
  description={viewing ? `${formatDate(viewing.created_at)}${viewing.created_by ? `, by ${viewing.created_by}` : ''}. Read only.` : ''}
  size="lg"
  onclose={() => (viewing = null)}
>
  {#if viewLoading}
    <div class="flex items-center justify-center py-10"><Spinner /></div>
  {:else}
    <pre class="max-h-[60vh] overflow-auto rounded-md border border-edge-subtle bg-canvas p-3 font-mono text-xs leading-relaxed text-fg-2">{viewJson}</pre>
  {/if}
  {#snippet footer()}
    <Button variant="ghost" size="sm" onclick={() => (viewing = null)}>Close</Button>
    <Button variant="secondary" size="sm" disabled={viewLoading} onclick={copyView}>
      <Copy size={13} /> Copy
    </Button>
    {#if viewing && viewing.version !== newest}
      <Button size="sm" onclick={() => { restoring = viewing; viewing = null }}>
        <RotateCcw size={13} /> Restore
      </Button>
    {/if}
  {/snippet}
</Modal>

<ConfirmDialog
  open={!!restoring}
  title={restoring ? `Restore version ${restoring.version}?` : 'Restore version?'}
  description="The current state stays in the history as its own version."
  confirmLabel="Restore"
  loading={restoreSaving}
  onconfirm={confirmRestore}
  oncancel={() => (restoring = null)}
/>
