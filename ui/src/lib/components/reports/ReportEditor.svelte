<!-- SPDX-License-Identifier: BUSL-1.1 -->
<script lang="ts">
  import { errorText } from '../../utils/errors'
  import { untrack } from 'svelte'
  import Sheet from '../common/Sheet.svelte'
  import Button from '../common/Button.svelte'
  import FormField from '../common/FormField.svelte'
  import Input from '../common/Input.svelte'
  import Select from '../common/Select.svelte'
  import Combobox from '../common/Combobox.svelte'
  import Panel from '../common/Panel.svelte'
  import Badge from '../common/Badge.svelte'
  import { apiGet } from '../../api/client'
  import { listDashboards } from '../../api/dashboards'
  import { getSession } from '../../stores/session.svelte'
  import { goTo } from '../../stores/router.svelte'
  import { setSection } from '../../stores/nav.svelte'
  import { success as toastSuccess } from '../../stores/toast.svelte'
  import {
    createReport, updateReport, REPORT_LIMITS,
    type Report, type ReportChannel, type ReportFormat, type ReportInput, type ReportSource, type ReportWorkerMode,
  } from '../../api/reports'
  import type { Connection, Dashboard, SavedQuery } from '../../types/api'

  /** Create / edit Sheet for a scheduled report. */
  interface Props {
    open: boolean
    /** The report being edited; null creates one. */
    report: Report | null
    /** Values for a new report, e.g. from Subscribe on a dashboard. */
    prefill?: Partial<ReportInput> | null
    channels: ReportChannel[]
    workerModes: Record<string, ReportWorkerMode>
    recipientDomains: string[]
    onclose: () => void
    onsaved: (report: Report) => void
  }

  let { open, report, prefill = null, channels, workerModes, recipientDomains, onclose, onsaved }: Props = $props()

  interface FormState {
    name: string
    source_type: ReportSource
    saved_query_id: string
    dashboard_id: string
    connection_id: string
    cron: string
    timezone: string
    enabled: boolean
    format: ReportFormat
    row_limit: number
    inline_rows: number
    max_mib: number
    timeout_seconds: number
    time_range: string
    variables: Record<string, string[]>
    skip_if_empty: boolean
    subject_template: string
    selected: Record<string, boolean>
    recipients: Record<string, string>
  }

  function blank(): FormState {
    return {
      name: '', source_type: 'saved_query', saved_query_id: '', dashboard_id: '',
      connection_id: getSession()?.connectionId ?? '', cron: '0 8 * * 1-5', timezone: 'UTC', enabled: true,
      format: 'csv', row_limit: REPORT_LIMITS.rowLimit.def, inline_rows: REPORT_LIMITS.inlineRows.def,
      max_mib: REPORT_LIMITS.maxBytes.def / (1 << 20), timeout_seconds: REPORT_LIMITS.timeout.def, time_range: '24h',
      variables: {}, skip_if_empty: false, subject_template: '', selected: {}, recipients: {},
    }
  }

  function fromInput(src: Partial<ReportInput>): FormState {
    const f = blank()
    if (src.name !== undefined) f.name = src.name
    if (src.source_type) f.source_type = src.source_type
    if (src.saved_query_id) f.saved_query_id = src.saved_query_id
    if (src.dashboard_id) f.dashboard_id = src.dashboard_id
    if (src.connection_id) f.connection_id = src.connection_id
    if (src.cron) f.cron = src.cron
    if (src.timezone) f.timezone = src.timezone
    if (src.enabled !== undefined) f.enabled = src.enabled
    if (src.format) f.format = src.format
    if (src.row_limit) f.row_limit = src.row_limit
    if (src.inline_rows) f.inline_rows = src.inline_rows
    if (src.max_bytes) f.max_mib = Math.round((src.max_bytes / (1 << 20)) * 100) / 100
    if (src.timeout_seconds) f.timeout_seconds = src.timeout_seconds
    if (src.time_range) f.time_range = src.time_range
    if (src.variables) f.variables = src.variables
    if (src.skip_if_empty !== undefined) f.skip_if_empty = src.skip_if_empty
    if (src.subject_template) f.subject_template = src.subject_template
    for (const t of src.targets ?? []) {
      f.selected[t.channel_id] = true
      f.recipients[t.channel_id] = t.recipients.join(', ')
    }
    return f
  }

  let form = $state<FormState>(blank())
  let saving = $state(false)
  let error = $state('')
  let savedQueries = $state<SavedQuery[]>([])
  let dashboards = $state<Dashboard[]>([])
  let connections = $state<Connection[]>([])
  let loadingLists = $state(false)

  // Reset the form each time the sheet opens.
  $effect(() => {
    if (!open) return
    untrack(() => {
      form = report ? fromInput(report) : fromInput(prefill ?? {})
      error = ''
      void loadLists()
    })
  })

  async function loadLists() {
    loadingLists = true
    try {
      const [sq, ds, cs] = await Promise.all([
        apiGet<{ saved_queries?: SavedQuery[] }>('/api/saved-queries').then((r) => r.saved_queries ?? []).catch(() => [] as SavedQuery[]),
        listDashboards().catch(() => [] as Dashboard[]),
        apiGet<Connection[]>('/api/connections').catch(() => [] as Connection[]),
      ])
      savedQueries = sq
      dashboards = ds
      connections = cs
    } finally {
      loadingLists = false
    }
  }

  const SOURCES = [
    { value: 'saved_query', label: 'Saved query result' },
    { value: 'dashboard', label: 'Dashboard summary' },
  ]
  const FORMATS = [
    { value: 'csv', label: 'CSV attachment' },
    { value: 'inline', label: 'Inline table' },
    { value: 'both', label: 'CSV attachment and inline table' },
  ]
  const RANGES = [
    { value: '1h', label: 'Last hour' },
    { value: '6h', label: 'Last 6 hours' },
    { value: '12h', label: 'Last 12 hours' },
    { value: '24h', label: 'Last 24 hours' },
    { value: '7d', label: 'Last 7 days' },
    { value: '30d', label: 'Last 30 days' },
  ]

  const connectionOptions = $derived(connections.map((c) => ({ value: c.id, label: c.name })))
  const activeChannels = $derived(channels.filter((c) => c.is_active || form.selected[c.id]))
  const selectedIds = $derived(Object.keys(form.selected).filter((id) => form.selected[id]))
  const workerMode = $derived(workerModes[form.connection_id] ?? 'session')
  const variableNames = $derived(Object.keys(form.variables))

  function isEmail(c: ReportChannel): boolean {
    return c.channel_type !== 'webhook'
  }

  function channelLabel(c: ReportChannel): string {
    if (c.channel_type === 'webhook') {
      if (c.format === 'slack') return 'Slack'
      if (c.format === 'teams') return 'Teams'
      return 'Webhook'
    }
    return 'Email'
  }

  function parseRecipients(raw: string | undefined): string[] {
    return (raw ?? '').split(/[\s,;]+/).map((s) => s.trim()).filter(Boolean)
  }

  const validation = $derived.by(() => {
    if (!form.name.trim()) return 'Name is required'
    if (form.source_type === 'saved_query' && !form.saved_query_id) return 'Choose a saved query'
    if (form.source_type === 'dashboard' && !form.dashboard_id) return 'Choose a dashboard'
    if (!form.connection_id) return 'Choose a connection'
    if (form.cron.trim().split(/\s+/).length !== 5) return 'Cron needs five fields: minute hour day month weekday'
    if (selectedIds.length === 0) return 'Choose at least one channel'
    if (selectedIds.length > REPORT_LIMITS.targets) return `At most ${REPORT_LIMITS.targets} channels`
    for (const id of selectedIds) {
      const c = channels.find((x) => x.id === id)
      if (!c || !isEmail(c)) continue
      const list = parseRecipients(form.recipients[id])
      if (list.length === 0) return `Add recipients for ${c.name}`
      if (list.length > REPORT_LIMITS.recipients) return `At most ${REPORT_LIMITS.recipients} recipients for ${c.name}`
    }
    return ''
  })

  function toInput(): ReportInput {
    return {
      name: form.name.trim(),
      source_type: form.source_type,
      saved_query_id: form.source_type === 'saved_query' ? form.saved_query_id : null,
      dashboard_id: form.source_type === 'dashboard' ? form.dashboard_id : null,
      connection_id: form.connection_id,
      cron: form.cron.trim(),
      timezone: form.timezone.trim() || 'UTC',
      enabled: form.enabled,
      format: form.source_type === 'dashboard' ? 'inline' : form.format,
      row_limit: Number(form.row_limit),
      inline_rows: Number(form.inline_rows),
      max_bytes: Math.round(Number(form.max_mib) * (1 << 20)),
      timeout_seconds: Number(form.timeout_seconds),
      time_range: form.source_type === 'dashboard' ? form.time_range : '',
      variables: form.source_type === 'dashboard' ? form.variables : {},
      skip_if_empty: form.skip_if_empty,
      subject_template: form.subject_template.trim(),
      targets: selectedIds.map((id) => {
        const c = channels.find((x) => x.id === id)
        return { channel_id: id, recipients: c && isEmail(c) ? parseRecipients(form.recipients[id]) : [] }
      }),
    }
  }

  async function save() {
    if (saving || validation) return
    saving = true
    error = ''
    try {
      const saved = report ? await updateReport(report.id, toInput()) : await createReport(toInput())
      toastSuccess(report ? 'Report saved' : 'Report created')
      onsaved(saved)
    } catch (e: unknown) {
      error = errorText(e)
    } finally {
      saving = false
    }
  }

  function openConnections() {
    onclose()
    goTo('admin')
    setSection('tunnels')
  }
</script>

<Sheet {open} title={report ? 'Edit report' : 'New report'} description="Delivered on a cron to your alert channels." size="md" onclose={() => { if (!saving) onclose() }}>
  <form id="report-form" class="space-y-4" onsubmit={(e) => { e.preventDefault(); void save() }}>
    {#if form.connection_id && workerMode !== 'service_account'}
      <Panel variant="muted" padding="sm">
        <p class="text-[13px] text-fg-2">
          Reports run with the connection's dedicated <span class="font-mono">report</span> background account and never borrow a session.
          Set it up in
          <button type="button" class="font-medium text-accent hover:underline" onclick={openConnections}>Admin, Connections</button>.
          Until then runs fail and nothing is sent.
        </p>
      </Panel>
    {/if}

    <FormField label="Name" for="report-name" required>
      <Input id="report-name" placeholder="e.g. Daily signups" maxlength={200} bind:value={form.name} />
    </FormField>

    <div class="grid grid-cols-1 gap-3 md:grid-cols-2">
      <FormField label="Source" for="report-source">
        <Select id="report-source" options={SOURCES} value={form.source_type} onchange={(v) => (form.source_type = v as ReportSource)} />
      </FormField>
      <FormField label="Connection" for="report-connection">
        <Select id="report-connection" options={connectionOptions} value={form.connection_id} placeholder="Choose a connection" onchange={(v) => (form.connection_id = v)} />
      </FormField>
    </div>

    {#if form.source_type === 'saved_query'}
      <FormField label="Saved query" for="report-query" hint="Parameters use their saved default values.">
        <Combobox
          id="report-query"
          options={savedQueries.map((q) => ({ value: q.id, label: q.name, hint: q.description || q.query, keywords: `${q.name} ${q.description ?? ''}` }))}
          value={form.saved_query_id}
          placeholder="Choose a saved query"
          emptyText={loadingLists ? 'Loading...' : 'No saved queries'}
          onChange={(v) => (form.saved_query_id = v)}
        />
      </FormField>
    {:else}
      <FormField label="Dashboard" for="report-dashboard" hint="Sent as a text summary of each panel, with a link to the dashboard.">
        <Combobox
          id="report-dashboard"
          options={dashboards.map((d) => ({ value: d.id, label: d.name, hint: d.description ?? '', keywords: d.name }))}
          value={form.dashboard_id}
          placeholder="Choose a dashboard"
          emptyText={loadingLists ? 'Loading...' : 'No dashboards'}
          onChange={(v) => (form.dashboard_id = v)}
        />
      </FormField>
      <FormField label="Time range" for="report-range">
        <Select id="report-range" options={RANGES} value={form.time_range} onchange={(v) => (form.time_range = v)} />
      </FormField>
      {#if variableNames.length > 0}
        <div class="flex flex-wrap items-center gap-1.5 text-xs text-fg-3">
          Variables:
          {#each variableNames as name (name)}
            <Badge class="font-mono">{name}={form.variables[name].join(',')}</Badge>
          {/each}
        </div>
      {/if}
    {/if}

    <div class="grid grid-cols-1 gap-3 md:grid-cols-2">
      <FormField label="Cron" for="report-cron" hint="0 8 * * 1-5 = weekdays at 08:00">
        <Input id="report-cron" mono bind:value={form.cron} />
      </FormField>
      <FormField label="Time zone" for="report-tz" hint="IANA name, e.g. Europe/Madrid">
        <Input id="report-tz" bind:value={form.timezone} />
      </FormField>
    </div>

    {#if form.source_type === 'saved_query'}
      <FormField label="Format" for="report-format">
        <Select id="report-format" options={FORMATS} value={form.format} onchange={(v) => (form.format = v as ReportFormat)} />
      </FormField>
      <div class="grid grid-cols-2 gap-3 md:grid-cols-3">
        <FormField label="Row cap" for="report-rows" hint={`Up to ${REPORT_LIMITS.rowLimit.max.toLocaleString()}`}>
          <Input id="report-rows" type="number" min="1" max={REPORT_LIMITS.rowLimit.max} bind:value={form.row_limit} />
        </FormField>
        <FormField label="Inline rows" for="report-inline" hint={`Up to ${REPORT_LIMITS.inlineRows.max}`}>
          <Input id="report-inline" type="number" min="1" max={REPORT_LIMITS.inlineRows.max} bind:value={form.inline_rows} />
        </FormField>
        <FormField label="Attachment (MiB)" for="report-bytes" hint="Up to 5">
          <Input id="report-bytes" type="number" min="0.01" max="5" step="0.25" bind:value={form.max_mib} />
        </FormField>
      </div>
    {/if}

    <div class="grid grid-cols-1 gap-3 md:grid-cols-2">
      <FormField label="Timeout (seconds)" for="report-timeout" hint="5 to 300">
        <Input id="report-timeout" type="number" min={REPORT_LIMITS.timeout.min} max={REPORT_LIMITS.timeout.max} bind:value={form.timeout_seconds} />
      </FormField>
      <FormField label="Subject" for="report-subject" hint={'Optional. {{name}}, {{date}}, {{rows}}'}>
        <Input id="report-subject" placeholder="[CH-UI] Report: {'{{name}}'}" maxlength={300} bind:value={form.subject_template} />
      </FormField>
    </div>

    <FormField label="Deliver to" hint={`Up to ${REPORT_LIMITS.targets} channels. Channels are managed in Governance, Alerts.`}>
      {#if activeChannels.length === 0}
        <p class="text-[13px] text-fg-3">No alert channels yet. An admin adds them in Governance, Alerts.</p>
      {:else}
        <div class="space-y-2">
          {#each activeChannels as c (c.id)}
            <div class="rounded-md border border-edge-subtle px-3 py-2">
              <label class="ds-checkbox-label">
                <input type="checkbox" class="ds-checkbox" bind:checked={form.selected[c.id]} />
                <span class="text-fg">{c.name}</span>
                <Badge>{channelLabel(c)}</Badge>
                {#if !c.is_active}<Badge tone="warning">Disabled</Badge>{/if}
              </label>
              {#if form.selected[c.id] && isEmail(c)}
                <div class="mt-2">
                  <Input placeholder="ops@example.com, team@example.com" bind:value={form.recipients[c.id]} />
                  {#if recipientDomains.length > 0}
                    <p class="mt-1 text-xs text-fg-4">Analysts can send to {recipientDomains.join(', ')} only.</p>
                  {/if}
                </div>
              {/if}
            </div>
          {/each}
        </div>
      {/if}
    </FormField>

    <div class="flex flex-wrap gap-x-6 gap-y-2">
      <label class="ds-checkbox-label">
        <input type="checkbox" class="ds-checkbox" bind:checked={form.skip_if_empty} />
        Skip when the result is empty
      </label>
      <label class="ds-checkbox-label">
        <input type="checkbox" class="ds-checkbox" bind:checked={form.enabled} />
        Enabled
      </label>
    </div>

    {#if error}<p class="text-xs text-danger" role="alert">{error}</p>{/if}
  </form>
  {#snippet footer()}
    {#if validation}<span class="mr-auto truncate text-xs text-fg-4">{validation}</span>{/if}
    <Button variant="ghost" size="sm" disabled={saving} onclick={onclose}>Cancel</Button>
    <Button size="sm" type="submit" form="report-form" loading={saving} disabled={!!validation}>{report ? 'Save' : 'Create report'}</Button>
  {/snippet}
</Sheet>
