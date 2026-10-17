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
  import EmptyState from '../lib/components/common/EmptyState.svelte'

  let reports = $state<OperationsReport[]>([])
  let settings = $state<ReportSettings | null>(null)
  let channels = $state<{ id: string; name: string; type: string }[]>([])
  let recipients = $state('')
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

  function errorMessage(e: unknown) { return e instanceof Error ? e.message : 'Request failed' }
  async function load() {
    loading = true; loadError = ''
    try {
      const [r, s, c] = await Promise.all([getReports(), getReportSettings(), getReportChannels()])
      reports = r.reports; settings = s; channels = c.channels; recipients = s.recipients.join(', ')
    } catch (e) { loadError = errorMessage(e) }
    finally { loading = false }
  }
  onMount(() => { if (admin) void load(); else loading = false })
  async function generate() {
    if (busy) return; busy = true
    try { const report = await generateReport(); reports = [report, ...reports]; selectedID = report.id; success('Report saved') }
    catch (e) { toastError(errorMessage(e)) } finally { busy = false }
  }
  async function save() {
    if (!settings || busy) return; busy = true
    try {
      settings = await saveReportSettings({ ...settings, weekday: Number(settings.weekday), hour: Number(settings.hour), recipients: recipients.split(',').map((v) => v.trim()).filter(Boolean) })
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
        <Panel>
          <form class="space-y-4" onsubmit={(e) => { e.preventDefault(); void save() }}>
            <div class="flex items-center justify-between"><h2 class="text-[13px] font-medium">Weekly schedule</h2><label class="ds-checkbox-label"><input type="checkbox" class="ds-checkbox ds-checkbox-sm" bind:checked={settings.enabled} disabled={busy} /> Enabled</label></div>
            <div class="grid gap-4 md:grid-cols-3">
              <FormField label="Day" for="report-day"><Select id="report-day" options={weekdayOptions} value={String(settings.weekday)} onchange={(v) => { if (settings) settings.weekday = Number(v) }} disabled={busy} /></FormField>
              <FormField label="Hour (UTC)" for="report-hour"><Input id="report-hour" type="number" min="0" max="23" bind:value={settings.hour} disabled={busy} required /></FormField>
              <FormField label="Email channel" for="report-channel"><Select id="report-channel" options={channelOptions} bind:value={settings.channel_id} disabled={busy} /></FormField>
            </div>
            <FormField label="Email recipients" for="report-recipients" hint="Comma-separated addresses. Leave empty for in-app reports; email contains workload identifiers and operational metrics."><Input id="report-recipients" bind:value={recipients} placeholder="data-team@example.com" disabled={busy} /></FormField>
            <p class="text-xs text-fg-3">Configure a dedicated Weekly operations reports account under <button type="button" class="text-accent hover:underline" onclick={openBackgroundAccounts}>Admin → Connections → Background accounts</button> before enabling. Manual generation uses your current ClickHouse account. Email channels are managed under <button type="button" class="text-accent hover:underline" onclick={openAlertChannels}>Governance → Alert channels</button>.</p>
            {#if staleChannel}<p role="alert" class="text-xs text-danger">The saved channel is not an active email channel, so reports cannot be emailed. Choose an email channel or In-app only.</p>{/if}
            {#if settings.last_error}<p role="alert" class="text-xs text-danger">{settings.last_error}</p>{/if}
            <div class="flex items-center justify-between gap-3"><p class="text-xs text-fg-3">{settings.enabled && settings.next_run_at ? `Next report: ${new Date(settings.next_run_at).toLocaleString()}` : 'Automatic reports are disabled.'}</p><Button type="submit" size="sm" loading={busy}>Save settings</Button></div>
          </form>
        </Panel>
      {/if}
      {#if !reports.length}
        <EmptyState size="compact" icon={FileText} title="Your first report starts here" description="Generate a report to compare the last seven days with the previous seven. Missing history is labeled as insufficient data." primary={{ label: 'Generate report', onclick: () => void generate() }} />
      {:else}
        <div class="grid gap-4 lg:grid-cols-[240px_1fr]">
          <nav aria-label="Saved reports" class="max-h-[600px] space-y-1 overflow-auto">{#each reports as report (report.id)}<button class="w-full rounded-md p-3 text-left hover:bg-hover {selected?.id === report.id ? 'bg-active' : ''}" onclick={() => selectedID = report.id}><span class="block text-[13px]">{new Date(report.created_at).toLocaleString()}</span><span class="text-xs text-fg-3">{report.delivery_status.replaceAll('_', ' ')}</span></button>{/each}</nav>
          {#if selected}
            <div class="min-w-0 space-y-4">
              <div class="grid grid-cols-3 gap-3"><Stat label="Regressions" value={String(selected.payload.regression_count)} /><Stat label="Failed executions" value={String(selected.payload.failures)} /><Stat label="Resolved investigations" value={String(selected.payload.resolved_investigations)} /></div>
              <div class="flex flex-wrap items-center justify-between gap-2"><p class="text-xs text-fg-3">Saved by {selected.created_by} · Email: {selected.delivery_status.replaceAll('_', ' ')}</p><div class="flex gap-2"><Button variant="outline" size="sm" onclick={download}><Download size={14} /> Download</Button><Button variant="outline" size="sm" disabled={busy || !settings?.channel_id || staleChannel || ['queued','retry','sending'].includes(selected.delivery_status)} onclick={send}><Send size={14} /> Email report</Button></div></div>
              {#if selected.delivery_error}<p role="alert" class="text-xs text-danger">{selected.delivery_error}</p>{/if}
              <Panel><pre class="whitespace-pre-wrap break-words font-sans text-[13px] leading-6 text-fg-2">{selected.body}</pre></Panel>
            </div>
          {/if}
        </div>
      {/if}
    </div>
  {/if}
</PageBody>
