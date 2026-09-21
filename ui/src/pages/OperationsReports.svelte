<!-- SPDX-License-Identifier: BUSL-1.1 -->
<script lang="ts">
  import { onMount } from 'svelte'
  import { FileText, RefreshCw, Send, Download } from 'lucide-svelte'
  import { getReports, getReportSettings, getReportChannels, saveReportSettings, generateReport, sendReport, type ReportSettings, type OperationsReport } from '../lib/api/operationsReports'
  import { success, error as toastError } from '../lib/stores/toast.svelte'
  import { getSession } from '../lib/stores/session.svelte'
  import PageHeader from '../lib/components/common/PageHeader.svelte'
  import PageBody from '../lib/components/common/PageBody.svelte'
  import Panel from '../lib/components/common/Panel.svelte'
  import Button from '../lib/components/common/Button.svelte'
  import FormField from '../lib/components/common/FormField.svelte'
  import Input from '../lib/components/common/Input.svelte'
  import Spinner from '../lib/components/common/Spinner.svelte'
  import Stat from '../lib/components/common/Stat.svelte'

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
  const weekdays = ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday']

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
</script>

<PageHeader title="Reports" subtitle="Weekly operations">
  {#snippet actions()}
    {#if admin}<Button variant="ghost" size="sm" disabled={loading || busy} onclick={load}><RefreshCw size={14} /> Refresh</Button><Button size="sm" loading={busy} disabled={loading || !!loadError} onclick={generate}><FileText size={14} /> Generate report</Button>{/if}
  {/snippet}
</PageHeader>
<PageBody width="full">
  {#if !admin}
    <p class="text-sm text-fg-3">An administrator can configure and view operational reports for this connection.</p>
  {:else if loading}<Spinner />
  {:else if loadError}<p role="alert" class="text-sm text-danger">{loadError}</p>
  {:else}
    <div class="space-y-6">
      <p class="max-w-3xl text-[13px] text-fg-3">Save a weekly summary of regressions, CPU-heavy workloads, failures, storage growth and resolved investigations. Reports use observed system-table data and remain available after query logs expire.</p>
      {#if settings}
        <Panel>
          <form class="space-y-4" onsubmit={(e) => { e.preventDefault(); void save() }}>
            <div class="flex items-center justify-between"><h2 class="text-sm font-medium">Weekly schedule</h2><label class="flex items-center gap-2 text-[13px]"><input type="checkbox" bind:checked={settings.enabled} disabled={busy} /> Enabled</label></div>
            <div class="grid gap-4 md:grid-cols-3">
              <FormField label="Day" for="report-day"><select id="report-day" class="ds-select w-full" bind:value={settings.weekday} disabled={busy}>{#each weekdays as day, i}<option value={i}>{day}</option>{/each}</select></FormField>
              <FormField label="Hour (UTC)" for="report-hour"><Input id="report-hour" type="number" min="0" max="23" bind:value={settings.hour} disabled={busy} required /></FormField>
              <FormField label="Email channel" for="report-channel"><select id="report-channel" class="ds-select w-full" bind:value={settings.channel_id} disabled={busy}><option value="">In-app only</option>{#each channels as c}<option value={c.id}>{c.name} · {c.type}</option>{/each}</select></FormField>
            </div>
            <FormField label="Email recipients" for="report-recipients" hint="Comma-separated addresses. Leave empty for in-app reports; email contains workload identifiers and operational metrics."><Input id="report-recipients" bind:value={recipients} placeholder="data-team@example.com" disabled={busy} /></FormField>
            <p class="text-xs text-fg-3">Configure a dedicated Weekly operations reports account under Admin → Connections → Background accounts before enabling. Manual generation uses your current ClickHouse account. Email channels are managed under Governance → Alerts.</p>
            {#if settings.last_error}<p role="alert" class="text-xs text-danger">{settings.last_error}</p>{/if}
            <div class="flex items-center justify-between gap-3"><p class="text-xs text-fg-3">{settings.enabled && settings.next_run_at ? `Next report: ${new Date(settings.next_run_at).toLocaleString()}` : 'Automatic reports are disabled.'}</p><Button type="submit" size="sm" loading={busy}>Save settings</Button></div>
          </form>
        </Panel>
      {/if}
      {#if !reports.length}
        <Panel><p class="text-sm font-medium">Your first report starts here</p><p class="mt-1 text-[13px] text-fg-3">Generate a report to compare the last seven days with the previous seven. Missing history is labeled as insufficient data.</p></Panel>
      {:else}
        <div class="grid gap-4 lg:grid-cols-[240px_1fr]">
          <nav aria-label="Saved reports" class="max-h-[600px] space-y-1 overflow-auto">{#each reports as report (report.id)}<button class="w-full rounded-md p-3 text-left hover:bg-hover {selected?.id === report.id ? 'bg-active' : ''}" onclick={() => selectedID = report.id}><span class="block text-[13px]">{new Date(report.created_at).toLocaleString()}</span><span class="text-xs text-fg-3">{report.delivery_status.replaceAll('_', ' ')}</span></button>{/each}</nav>
          {#if selected}
            <div class="min-w-0 space-y-4">
              <div class="grid grid-cols-3 gap-3"><Stat label="Regressions" value={String(selected.payload.regression_count)} /><Stat label="Failed executions" value={String(selected.payload.failures)} /><Stat label="Resolved investigations" value={String(selected.payload.resolved_investigations)} /></div>
              <div class="flex flex-wrap items-center justify-between gap-2"><p class="text-xs text-fg-3">Saved by {selected.created_by} · Email: {selected.delivery_status.replaceAll('_', ' ')}</p><div class="flex gap-2"><Button variant="outline" size="sm" onclick={download}><Download size={14} /> Download</Button><Button variant="outline" size="sm" disabled={busy || !settings?.channel_id || ['queued','retry','sending'].includes(selected.delivery_status)} onclick={send}><Send size={14} /> Email report</Button></div></div>
              {#if selected.delivery_error}<p role="alert" class="text-xs text-danger">{selected.delivery_error}</p>{/if}
              <Panel><pre class="whitespace-pre-wrap break-words font-sans text-[13px] leading-6 text-fg-2">{selected.body}</pre></Panel>
            </div>
          {/if}
        </div>
      {/if}
    </div>
  {/if}
</PageBody>
