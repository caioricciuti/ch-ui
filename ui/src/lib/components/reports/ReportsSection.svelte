<!-- SPDX-License-Identifier: BUSL-1.1 -->
<script lang="ts">
  import { errorText } from '../../utils/errors'
  import { onMount } from 'svelte'
  import { Plus, RefreshCw, Search, Send, Eye, History, Pencil, Trash2, Mail, Download, Settings } from 'lucide-svelte'
  import Button from '../common/Button.svelte'
  import Input from '../common/Input.svelte'
  import Badge from '../common/Badge.svelte'
  import Spinner from '../common/Spinner.svelte'
  import Sheet from '../common/Sheet.svelte'
  import FormField from '../common/FormField.svelte'
  import EmptyState from '../common/EmptyState.svelte'
  import ConfirmDialog from '../common/ConfirmDialog.svelte'
  import DataTable, { type DataColumn } from '../common/DataTable.svelte'
  import ReportEditor from './ReportEditor.svelte'
  import { apiGet } from '../../api/client'
  import { listDashboards } from '../../api/dashboards'
  import {
    fetchReports, deleteReport, previewReport, sendReport, fetchReportDeliveries, downloadDeliveryCSV, saveRecipientDomains,
    type Report, type ReportChannel, type ReportDelivery, type ReportPreview, type ReportWorkerMode,
  } from '../../api/reports'
  import { getSession } from '../../stores/session.svelte'
  import { success as toastSuccess, errorFrom as toastErrorFrom } from '../../stores/toast.svelte'
  import { downloadBlob } from '../../utils/export'
  import { formatBytes, formatDate, formatRelativeTime } from '../../utils/format'
  import type { Dashboard, SavedQuery } from '../../types/api'

  /** Report delivery: saved query results and dashboard summaries sent on a cron to alert channels. The API lists only the caller's own reports (all of them for admins). */
  const session = $derived(getSession())
  const isAdmin = $derived(session?.role === 'admin')
  const canWrite = $derived(session?.role === 'admin' || session?.role === 'analyst')

  let reports = $state<Report[]>([])
  let channels = $state<ReportChannel[]>([])
  let workerModes = $state<Record<string, ReportWorkerMode>>({})
  let recipientDomains = $state<string[]>([])
  let savedQueries = $state<SavedQuery[]>([])
  let dashboards = $state<Dashboard[]>([])
  let loading = $state(true)
  let search = $state('')

  let editorOpen = $state(false)
  let editing = $state<Report | null>(null)
  let deleting = $state<Report | null>(null)
  let deleteLoading = $state(false)
  let busyId = $state<string | null>(null)

  let deliveriesFor = $state<Report | null>(null)
  let deliveries = $state<ReportDelivery[]>([])
  let deliveriesLoading = $state(false)

  let previewFor = $state<Report | null>(null)
  let preview = $state<ReportPreview | null>(null)
  let previewError = $state('')

  let settingsOpen = $state(false)
  let domainsInput = $state('')
  let settingsSaving = $state(false)

  type Row = Report & Record<string, unknown> & { source_label: string; targets_label: string; status_label: string }

  async function load() {
    loading = true
    try {
      const [res, sq, ds] = await Promise.all([
        fetchReports(),
        apiGet<{ saved_queries?: SavedQuery[] }>('/api/saved-queries').then((r) => r.saved_queries ?? []).catch(() => [] as SavedQuery[]),
        listDashboards().catch(() => [] as Dashboard[]),
      ])
      reports = res.reports
      channels = res.channels
      workerModes = res.worker_modes
      recipientDomains = res.recipient_domains
      savedQueries = sq
      dashboards = ds
    } catch (e: unknown) {
      toastErrorFrom(e)
    } finally {
      loading = false
    }
  }
  onMount(load)

  function sourceLabel(r: Report): string {
    if (r.source_type === 'dashboard') {
      const d = dashboards.find((x) => x.id === r.dashboard_id)
      return `Dashboard: ${d?.name ?? 'deleted'}`
    }
    const q = savedQueries.find((x) => x.id === r.saved_query_id)
    return q?.name ?? 'Saved query'
  }

  function statusLabel(r: Report): string {
    if (!r.enabled) return r.consecutive_failures >= 10 ? 'disabled after failures' : 'off'
    return r.last_status ?? 'not run yet'
  }

  type Tone = 'neutral' | 'success' | 'warning' | 'danger' | 'info' | 'brand'
  function statusTone(s: string | null): Tone {
    switch (s) {
      case 'delivered': return 'success'
      case 'partial': return 'warning'
      case 'failed': return 'danger'
      case 'sending': return 'info'
      default: return 'neutral'
    }
  }

  const rows = $derived.by<Row[]>(() => {
    const term = search.trim().toLowerCase()
    return reports
      .map((r) => ({
        ...r,
        source_label: sourceLabel(r),
        targets_label: r.targets.map((t) => t.channel_name || 'channel').join(', '),
        status_label: statusLabel(r),
      }))
      .filter((r) => !term || `${r.name} ${r.source_label} ${r.targets_label}`.toLowerCase().includes(term))
  })

  const columns: DataColumn<Row>[] = [
    { key: 'name', label: 'Name', width: '22%' },
    { key: 'source_label', label: 'Source', truncate: true },
    { key: 'cron', label: 'Cron', mono: true, width: '130px' },
    { key: 'targets_label', label: 'Targets', truncate: true },
    { key: 'status_label', label: 'Last status', width: '170px' },
    {
      key: 'next_run_at', label: 'Next run', width: '120px',
      format: (v, r) => (r.enabled && v ? formatRelativeTime(v) : '-'),
      sortValue: (r) => (r.enabled && r.next_run_at ? new Date(r.next_run_at).getTime() : Number.MAX_SAFE_INTEGER),
    },
  ]

  function canChange(r: Report): boolean {
    return canWrite && (isAdmin || r.created_by === session?.user)
  }

  function openNew() {
    editing = null
    editorOpen = true
  }

  function openEdit(r: Report) {
    editing = r
    editorOpen = true
  }

  function saved(r: Report) {
    reports = reports.some((x) => x.id === r.id) ? reports.map((x) => (x.id === r.id ? r : x)) : [...reports, r]
    editorOpen = false
    editing = null
  }

  async function confirmDelete() {
    const target = deleting
    if (!target) return
    deleteLoading = true
    try {
      await deleteReport(target.id)
      reports = reports.filter((r) => r.id !== target.id)
      toastSuccess('Report deleted')
      deleting = null
    } catch (e: unknown) {
      toastErrorFrom(e)
    } finally {
      deleteLoading = false
    }
  }

  async function sendNow(r: Report) {
    busyId = r.id
    try {
      const d = await sendReport(r.id)
      if (d?.status === 'skipped') toastSuccess(`"${r.name}" returned no rows; nothing was sent`)
      else if (d?.status === 'delivered') toastSuccess(`"${r.name}" sent`)
      else toastSuccess(`"${r.name}" is sending; failed channels retry automatically`)
    } catch (e: unknown) {
      toastErrorFrom(e)
    } finally {
      busyId = null
      await load()
      if (deliveriesFor?.id === r.id) await loadDeliveries(r)
    }
  }

  async function openPreview(r: Report) {
    previewFor = r
    preview = null
    previewError = ''
    try {
      preview = await previewReport(r.id)
    } catch (e: unknown) {
      previewError = errorText(e)
    }
  }

  async function loadDeliveries(r: Report) {
    deliveriesLoading = true
    try {
      deliveries = await fetchReportDeliveries(r.id)
    } catch (e: unknown) {
      toastErrorFrom(e)
    } finally {
      deliveriesLoading = false
    }
  }

  function openDeliveries(r: Report) {
    deliveriesFor = r
    deliveries = []
    void loadDeliveries(r)
  }

  async function download(d: ReportDelivery) {
    try {
      const { blob, filename } = await downloadDeliveryCSV(d.id)
      downloadBlob(blob, filename)
    } catch (e: unknown) {
      toastErrorFrom(e)
    }
  }

  function openSettings() {
    domainsInput = recipientDomains.join(', ')
    settingsOpen = true
  }

  async function saveSettings() {
    settingsSaving = true
    try {
      recipientDomains = await saveRecipientDomains(domainsInput.split(/[\s,;]+/).filter(Boolean))
      toastSuccess('Recipient domains saved')
      settingsOpen = false
    } catch (e: unknown) {
      toastErrorFrom(e)
    } finally {
      settingsSaving = false
    }
  }

  type DeliveryRow = ReportDelivery & Record<string, unknown> & { channels_label: string }
  const deliveryRows = $derived<DeliveryRow[]>(deliveries.map((d) => ({
    ...d,
    channels_label: d.targets.map((t) => `${t.channel_name || 'channel'}: ${t.status}${t.attempts > 1 ? ` (${t.attempts} tries)` : ''}`).join(', '),
  })))
  const deliveryColumns: DataColumn<DeliveryRow>[] = [
    { key: 'started_at', label: 'Started', mono: true, width: '150px', format: (v) => formatDate(v) },
    { key: 'trigger', label: 'Trigger', width: '90px' },
    { key: 'status', label: 'Status', width: '100px' },
    { key: 'rows', label: 'Rows', align: 'right', width: '80px', format: (v, r) => `${Number(v ?? 0).toLocaleString()}${r.truncated ? '+' : ''}` },
    { key: 'bytes', label: 'Size', align: 'right', width: '80px', format: (v) => (Number(v) > 0 ? formatBytes(Number(v)) : '-') },
    { key: 'channels_label', label: 'Channels', truncate: true, sortable: false },
    { key: 'error', label: 'Error', truncate: true, sortable: false, format: (v) => (v ? String(v) : '') },
  ]
</script>

<div class="flex h-full min-h-0 flex-col">
  <div class="flex h-10 shrink-0 items-center gap-2 border-b border-edge-subtle px-5">
    <div class="relative">
      <Search size={13} class="pointer-events-none absolute left-2.5 top-1/2 -translate-y-1/2 text-fg-4" />
      <Input size="sm" class="w-64 pl-8" placeholder="Search reports" bind:value={search} />
    </div>
    <span class="ml-auto text-xs text-fg-4 tabular-nums">{rows.length} {rows.length === 1 ? 'report' : 'reports'}</span>
    <Button icon variant="ghost" size="sm" aria-label="Refresh" title="Refresh" onclick={load}>
      <RefreshCw size={14} class={loading ? 'animate-spin' : ''} />
    </Button>
    {#if isAdmin}
      <Button icon variant="ghost" size="sm" aria-label="Recipient domains" title="Recipient domains" onclick={openSettings}>
        <Settings size={14} />
      </Button>
    {/if}
    {#if canWrite}
      <Button size="sm" onclick={openNew}><Plus size={14} /> New report</Button>
    {/if}
  </div>

  <div class="min-h-0 flex-1 px-5 pb-4 pt-3">
    {#if loading && reports.length === 0}
      <div class="flex h-full items-center justify-center"><Spinner /></div>
    {:else if reports.length === 0}
      <EmptyState
        icon={Mail}
        title="No reports yet"
        description="Send a saved query result or a dashboard summary on a cron to email, Slack, Teams or a webhook. Reports run with the connection's dedicated report account, so each report is visible only to its creator and admins."
        primary={canWrite ? { label: 'New report', onclick: openNew } : undefined}
      />
    {:else}
      <div class="h-full overflow-hidden rounded-lg border border-edge-subtle bg-surface">
        <DataTable fill framed={false} {columns} {rows} rowKey={(r) => r.id} sort={{ key: 'name', dir: 'asc' }} emptyTitle="Nothing matches" onrowclick={(r) => openDeliveries(r)}>
          {#snippet cell(row, col, value)}
            {#if col.key === 'name'}
              <span class="font-medium text-fg">{row.name}</span>
            {:else if col.key === 'status_label'}
              <span title={row.last_error ?? undefined}>
                <Badge tone={row.enabled ? statusTone(row.last_status) : 'neutral'}>{value}</Badge>
              </span>
            {:else if col.key === 'next_run_at'}
              <span title={row.enabled && row.next_run_at ? `${formatDate(row.next_run_at)} (${row.timezone})` : undefined}>{value}</span>
            {:else}
              {value}
            {/if}
          {/snippet}
          {#snippet actions(row)}
            {#if canChange(row)}
              <Button icon variant="ghost" size="xs" aria-label="Send now" title="Send now" loading={busyId === row.id} onclick={(e) => { e.stopPropagation(); void sendNow(row) }}><Send size={13} /></Button>
              <Button icon variant="ghost" size="xs" aria-label="Preview" title="Preview" onclick={(e) => { e.stopPropagation(); void openPreview(row) }}><Eye size={13} /></Button>
            {/if}
            <Button icon variant="ghost" size="xs" aria-label="Deliveries" title="Deliveries" onclick={(e) => { e.stopPropagation(); openDeliveries(row) }}><History size={13} /></Button>
            {#if canChange(row)}
              <Button icon variant="ghost" size="xs" aria-label="Edit report" title="Edit" onclick={(e) => { e.stopPropagation(); openEdit(row) }}><Pencil size={13} /></Button>
              <Button icon variant="ghost" size="xs" aria-label="Delete report" title="Delete" onclick={(e) => { e.stopPropagation(); deleting = row }}><Trash2 size={13} /></Button>
            {/if}
          {/snippet}
        </DataTable>
      </div>
    {/if}
  </div>
</div>

<ReportEditor
  open={editorOpen}
  report={editing}
  {channels}
  {workerModes}
  {recipientDomains}
  onclose={() => { editorOpen = false; editing = null }}
  onsaved={saved}
/>

<Sheet open={!!deliveriesFor} title={deliveriesFor ? `Deliveries · ${deliveriesFor.name}` : 'Deliveries'} description="The latest 100 runs. CSV files stay downloadable for 7 days; history is kept for 90." size="lg" onclose={() => (deliveriesFor = null)}>
  {#if deliveriesLoading && deliveries.length === 0}
    <div class="flex justify-center py-10"><Spinner /></div>
  {:else}
    <DataTable columns={deliveryColumns} rows={deliveryRows} rowKey={(r) => r.id} emptyTitle="No runs yet" emptyDescription="Runs appear here after the first slot or Send now.">
      {#snippet cell(row, col, value)}
        {#if col.key === 'status'}
          <Badge tone={statusTone(row.status)}>{value}</Badge>
        {:else if col.key === 'error' && row.error}
          <span class="text-danger">{value}</span>
        {:else}
          {value}
        {/if}
      {/snippet}
      {#snippet actions(row)}
        {#if row.has_snapshot && row.bytes > 0 && deliveriesFor?.source_type === 'saved_query' && deliveriesFor?.format !== 'inline'}
          <Button size="xs" variant="outline" onclick={() => void download(row)}><Download size={12} /> Download</Button>
        {/if}
      {/snippet}
    </DataTable>
  {/if}
  {#snippet footer()}
    <Button variant="ghost" size="sm" onclick={() => (deliveriesFor = null)}>Close</Button>
  {/snippet}
</Sheet>

<Sheet open={!!previewFor} title={previewFor ? `Preview · ${previewFor.name}` : 'Preview'} description="Rendered with the report account. Nothing is sent." size="lg" onclose={() => (previewFor = null)}>
  {#if previewError}
    <p class="font-mono text-xs text-danger" role="alert">{previewError}</p>
  {:else if !preview}
    <div class="flex justify-center py-10"><Spinner /></div>
  {:else}
    <div class="space-y-3">
      <div class="flex flex-wrap items-center gap-2 text-[13px] text-fg-2">
        <span class="font-medium text-fg">{preview.subject}</span>
        <Badge>{preview.rows.toLocaleString()} {previewFor?.source_type === 'dashboard' ? 'panels' : 'rows'}</Badge>
        {#if preview.truncated}<Badge tone="warning">Truncated</Badge>{/if}
        {#if preview.csv_name}<Badge tone="info">{preview.csv_name} · {formatBytes(preview.csv_bytes)}</Badge>{/if}
      </div>
      <pre class="max-h-[70vh] overflow-auto rounded-md border border-edge-subtle bg-surface p-3 font-mono text-xs text-fg-2">{preview.text}</pre>
    </div>
  {/if}
  {#snippet footer()}
    <Button variant="ghost" size="sm" onclick={() => (previewFor = null)}>Close</Button>
  {/snippet}
</Sheet>

<Sheet open={settingsOpen} title="Recipient domains" description="Limit the addresses analysts can send reports to." size="sm" onclose={() => { if (!settingsSaving) settingsOpen = false }}>
  <FormField label="Allowed domains" for="report-domains" hint="Comma separated, e.g. example.com, example.org. Empty allows any address. Admins are not limited.">
    <Input id="report-domains" placeholder="example.com" bind:value={domainsInput} />
  </FormField>
  {#snippet footer()}
    <Button variant="ghost" size="sm" disabled={settingsSaving} onclick={() => (settingsOpen = false)}>Cancel</Button>
    <Button size="sm" loading={settingsSaving} onclick={() => void saveSettings()}>Save</Button>
  {/snippet}
</Sheet>

<ConfirmDialog
  open={!!deleting}
  title="Delete report?"
  description={deleting ? `"${deleting.name}" stops being sent and its delivery history is removed.` : ''}
  confirmLabel="Delete"
  destructive
  loading={deleteLoading}
  onconfirm={confirmDelete}
  oncancel={() => (deleting = null)}
/>
