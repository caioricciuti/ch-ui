<!-- SPDX-License-Identifier: BUSL-1.1 -->
<script lang="ts">
  import { onMount } from 'svelte'
  import { FileText, RefreshCw, Send, Download, AlertTriangle } from 'lucide-svelte'
  import { getReports, getReportSettings, getReportChannels, saveReportSettings, generateReport, sendReport, type ReportSettings, type OperationsReport } from '../lib/api/operationsReports'
  import { success, error as toastError } from '../lib/stores/toast.svelte'
  import { getSession } from '../lib/stores/session.svelte'
  import { goTo } from '../lib/stores/router.svelte'
  import { setSection } from '../lib/stores/nav.svelte'
  import PageHeader from '../lib/components/common/PageHeader.svelte'
  import { docsURL } from '../lib/routes'
  import PageBody from '../lib/components/common/PageBody.svelte'
  import Panel from '../lib/components/common/Panel.svelte'
  import Button from '../lib/components/common/Button.svelte'
  import FormField from '../lib/components/common/FormField.svelte'
  import Input from '../lib/components/common/Input.svelte'
  import Spinner from '../lib/components/common/Spinner.svelte'
  import Stat from '../lib/components/common/Stat.svelte'
  import Select from '../lib/components/common/Select.svelte'
  import Sheet from '../lib/components/common/Sheet.svelte'
  import Switch from '../lib/components/common/Switch.svelte'
  import EmptyState from '../lib/components/common/EmptyState.svelte'
  import Badge from '../lib/components/common/Badge.svelte'
  import DataTable, { type DataColumn } from '../lib/components/common/DataTable.svelte'
  import { formatDate } from '../lib/utils/format'

  type ReportRow = Record<string, unknown> & OperationsReport & { regressions: number; failures: number; resolved: number }

  let reports = $state<OperationsReport[]>([])
  let settings = $state<ReportSettings | null>(null)
  let channels = $state<{ id: string; name: string; type: string }[]>([])
  type ScheduleDraft = { enabled: boolean; weekday: string; hour: number; channel_id: string; recipients: string }
  let editOpen = $state(false)
  let draft = $state<ScheduleDraft>({ enabled: false, weekday: '1', hour: 9, channel_id: '', recipients: '' })
  let loading = $state(true)
  let busy = $state(false)
  let loadError = $state('')
  let selectedID = $state('')
  const selected = $derived(reports.find((r) => r.id === selectedID) ?? reports[0])
  const admin = $derived(getSession()?.role === 'admin')
  /** A saved channel the list no longer offers: a webhook, or one turned off or deleted. Reports go out as email only. */
  const staleChannel = $derived(!!settings?.channel_id && !channels.some((c) => c.id === settings?.channel_id))
  const weekdays = ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday']
  const weekdayOptions = weekdays.map((label, i) => ({ value: String(i), label }))
  const channelOptions = $derived([{ value: '', label: 'In-app only' }, ...channels.map((c) => ({ value: c.id, label: `${c.name} · ${c.type}` }))])
  const savedChannel = $derived(channels.find((c) => c.id === settings?.channel_id))
  const scheduleSummary = $derived.by(() => {
    if (!settings?.enabled) return 'Automatic reports are off'
    const delivery = !settings.channel_id ? 'in-app only' : savedChannel ? `emailed via ${savedChannel.name}` : 'email channel unavailable'
    return `Every ${weekdays[settings.weekday] ?? 'week'} at ${String(settings.hour).padStart(2, '0')}:00 UTC, ${delivery}`
  })
  const rows = $derived<ReportRow[]>(reports.map((r) => ({ ...r, regressions: r.payload.regression_count, failures: r.payload.failures, resolved: r.payload.resolved_investigations })))
  const columns: DataColumn<ReportRow>[] = [
    { key: 'created_at', label: 'Generated', format: formatDate },
    { key: 'regressions', label: 'Regressions', align: 'right', width: '120px' },
    { key: 'failures', label: 'Failed executions', align: 'right', width: '150px' },
    { key: 'resolved', label: 'Resolved investigations', align: 'right', width: '190px' },
    { key: 'delivery_status', label: 'Delivery', width: '140px', format: (v) => String(v).replaceAll('_', ' ') },
  ]
  const deliveryTone = (status: string): 'neutral' | 'success' | 'warning' | 'danger' | 'info' =>
    status === 'sent' ? 'success' : status === 'failed' ? 'danger' : ['queued', 'retry', 'sending'].includes(status) ? 'info' : 'neutral'

  function errorMessage(e: unknown) { return e instanceof Error ? e.message : 'Request failed' }
  async function load() {
    loading = true; loadError = ''
    try {
      const [r, s, c] = await Promise.all([getReports(), getReportSettings(), getReportChannels()])
      reports = r.reports; settings = s; channels = c.channels
    } catch (e) { loadError = errorMessage(e) }
    finally { loading = false }
  }
  onMount(() => { if (admin) void load(); else loading = false })
  async function generate() {
    if (busy) return; busy = true
    try { const report = await generateReport(); reports = [report, ...reports]; selectedID = report.id; success('Report saved') }
    catch (e) { toastError(errorMessage(e)) } finally { busy = false }
  }
  function openEdit() {
    if (!settings) return
    draft = { enabled: settings.enabled, weekday: String(settings.weekday), hour: settings.hour, channel_id: settings.channel_id, recipients: settings.recipients.join(', ') }
    editOpen = true
  }
  async function save() {
    if (!settings || busy) return; busy = true
    try {
      settings = await saveReportSettings({ ...settings, enabled: draft.enabled, channel_id: draft.channel_id, weekday: Number(draft.weekday), hour: Number(draft.hour), recipients: draft.recipients.split(',').map((v) => v.trim()).filter(Boolean) })
      editOpen = false
      success('Report settings saved')
    } catch (e) { toastError(errorMessage(e)) } finally { busy = false }
  }
  async function send() {
    if (!selected || busy) return; busy = true
    try { await sendReport(selected.id); success('Report queued for delivery'); reports = (await getReports()).reports }
    catch (e) { toastError(errorMessage(e)) } finally { busy = false }
  }
  function download() {
    if (!selected) return
    const url = URL.createObjectURL(new Blob([selected.body], { type: 'text/plain;charset=utf-8' }))
    const a = document.createElement('a'); a.href = url; a.download = `ch-ui-report-${selected.created_at.slice(0, 10)}.txt`; a.click(); URL.revokeObjectURL(url)
  }
  function openBackgroundAccounts() {
    goTo('admin')
    setSection('tunnels')
  }

  function openAlertChannels() {
    goTo('governance')
    setSection('alert-channels')
  }
</script>

<PageHeader title="Reports" subtitle="Weekly operations" docs={docsURL('operations-reports')}>
  {#snippet actions()}
    {#if admin}<Button variant="ghost" size="sm" disabled={loading || busy} onclick={load}><RefreshCw size={14} /> Refresh</Button><Button size="sm" loading={busy} disabled={loading || !!loadError} onclick={generate}><FileText size={14} /> Generate report</Button>{/if}
  {/snippet}
</PageHeader>
<PageBody width="full">
  {#if !admin}
    <EmptyState size="compact" title="Admins only" description="An administrator can configure and view operational reports for this connection." />
  {:else if loading}<Spinner />
  {:else if loadError}<EmptyState size="compact" icon={AlertTriangle} title="Could not load reports" description={loadError} primary={{ label: 'Retry', onclick: () => void load() }} />
  {:else}
    <div class="space-y-6">
      <p class="max-w-3xl text-[13px] text-fg-3">Save a weekly summary of regressions, CPU-heavy workloads, failures, storage growth and resolved investigations. Reports use observed system-table data and remain available after query logs expire.</p>
      {#if settings}
        <Panel title="Weekly schedule">
          {#snippet actions()}
            <Button variant="outline" size="sm" disabled={busy} onclick={openEdit}>Edit schedule</Button>
          {/snippet}
          <div class="space-y-2">
            <p class="text-[13px] text-fg">{scheduleSummary}</p>
            {#if settings.enabled && settings.next_run_at}<p class="text-xs text-fg-3">Next report: {new Date(settings.next_run_at).toLocaleString()}</p>{/if}
            {#if settings.channel_id && settings.recipients.length}<p class="text-xs text-fg-3">Recipients: {settings.recipients.join(', ')}</p>{/if}
            {#if staleChannel}<p role="alert" class="text-xs text-danger">The saved channel is not an active email channel, so reports cannot be emailed. Choose an email channel or In-app only.</p>{/if}
            {#if settings.last_error}<p role="alert" class="text-xs text-danger">{settings.last_error}</p>{/if}
            <p class="text-xs text-fg-3">Configure a dedicated Weekly operations reports account under <button type="button" class="text-accent hover:underline" onclick={openBackgroundAccounts}>Admin → Connections → Background accounts</button> before enabling. Manual generation uses your current ClickHouse account. Email channels are managed under <button type="button" class="text-accent hover:underline" onclick={openAlertChannels}>Governance → Alert channels</button>.</p>
          </div>
        </Panel>
      {/if}
      {#if !reports.length}
        <EmptyState size="compact" icon={FileText} title="Your first report starts here" description="Generate a report to compare the last seven days with the previous seven. Missing history is labeled as insufficient data." primary={{ label: 'Generate report', onclick: () => void generate() }} />
      {:else}
        <DataTable {columns} {rows} rowKey={(r) => r.id} selectedKey={selected?.id} onrowclick={(r) => selectedID = r.id} emptyTitle="No reports yet">
          {#snippet cell(row, col, value)}
            {#if col.key === 'delivery_status'}<Badge tone={deliveryTone(row.delivery_status)}>{value}</Badge>{:else}{value}{/if}
          {/snippet}
        </DataTable>
        {#if selected}
          <div class="min-w-0 space-y-4">
            <div class="grid grid-cols-3 gap-3"><Stat label="Regressions" value={String(selected.payload.regression_count)} /><Stat label="Failed executions" value={String(selected.payload.failures)} /><Stat label="Resolved investigations" value={String(selected.payload.resolved_investigations)} /></div>
            <div class="flex flex-wrap items-center justify-between gap-2"><p class="text-xs text-fg-3">Generated {formatDate(selected.created_at)} by {selected.created_by} · Email: {selected.delivery_status.replaceAll('_', ' ')}</p><div class="flex gap-2"><Button variant="outline" size="sm" onclick={download}><Download size={14} /> Download</Button><Button variant="outline" size="sm" disabled={busy || !settings?.channel_id || staleChannel || ['queued','retry','sending'].includes(selected.delivery_status)} onclick={send}><Send size={14} /> Email report</Button></div></div>
            {#if selected.delivery_error}<p role="alert" class="text-xs text-danger">{selected.delivery_error}</p>{/if}
            <Panel><pre class="whitespace-pre-wrap break-words font-sans text-[13px] leading-6 text-fg-2">{selected.body}</pre></Panel>
          </div>
        {/if}
      {/if}
    </div>
  {/if}
</PageBody>

<Sheet open={editOpen} size="sm" title="Weekly schedule" description="Reports are generated at this hour and kept in-app. Choose an email channel to send them out as well." onclose={() => (editOpen = false)}>
  <form id="report-schedule-form" class="space-y-4" onsubmit={(e) => { e.preventDefault(); void save() }}>
    <div class="flex items-center justify-between gap-4">
      <div>
        <div class="text-xs font-medium text-fg-2">Enabled</div>
        <p class="mt-0.5 text-xs text-fg-3">Generate a report every week on the schedule below.</p>
      </div>
      <Switch checked={draft.enabled} label={draft.enabled ? 'Turn automatic reports off' : 'Turn automatic reports on'} disabled={busy} onchange={(next) => { draft.enabled = next }} />
    </div>
    <div class="grid gap-4 md:grid-cols-2">
      <FormField label="Day" for="report-day" controlWidth="full"><Select id="report-day" options={weekdayOptions} bind:value={draft.weekday} disabled={busy} /></FormField>
      <FormField label="Hour (UTC)" for="report-hour" controlWidth="full"><Input id="report-hour" type="number" min="0" max="23" bind:value={draft.hour} disabled={busy} required /></FormField>
    </div>
    <FormField label="Email channel" for="report-channel" controlWidth="full"><Select id="report-channel" options={channelOptions} bind:value={draft.channel_id} disabled={busy} /></FormField>
    <FormField label="Email recipients" for="report-recipients" controlWidth="full" hint="Comma-separated addresses. Leave empty for in-app reports; email contains workload identifiers and operational metrics."><Input id="report-recipients" bind:value={draft.recipients} placeholder="data-team@example.com" disabled={busy} /></FormField>
  </form>
  {#snippet footer()}
    <Button size="sm" variant="ghost" disabled={busy} onclick={() => (editOpen = false)}>Cancel</Button>
    <Button size="sm" type="submit" form="report-schedule-form" loading={busy}>Save</Button>
  {/snippet}
</Sheet>
