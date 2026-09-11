<script lang="ts">
  import { onMount } from 'svelte'
  import type { AlertChannel, AlertChannelType, AlertEvent, AlertRule } from '../../types/alerts'
  import {
    adminListAlertChannels, adminCreateAlertChannel, adminUpdateAlertChannel, adminDeleteAlertChannel,
    adminTestAlertChannel, adminListAlertRules, adminCreateAlertRule, adminUpdateAlertRule,
    adminDeleteAlertRule, adminListAlertEvents, type AlertRuleChannelPayload,
  } from '../../api/alerts'
  import { success as toastSuccess, error as toastError } from '../../stores/toast.svelte'
  import { formatDate } from '../../utils/format'
  import Button from '../common/Button.svelte'
  import Badge from '../common/Badge.svelte'
  import Tabs from '../common/Tabs.svelte'
  import Select from '../common/Select.svelte'
  import Input from '../common/Input.svelte'
  import Textarea from '../common/Textarea.svelte'
  import FormField from '../common/FormField.svelte'
  import Sheet from '../common/Sheet.svelte'
  import Modal from '../common/Modal.svelte'
  import ConfirmDialog from '../common/ConfirmDialog.svelte'
  import Spinner from '../common/Spinner.svelte'
  import DataTable, { type DataColumn } from '../common/DataTable.svelte'
  import { Send, Pencil, Trash2, RefreshCw, Plus } from 'lucide-svelte'

  // Alerting admin: channels hold delivery credentials, rules route events
  // to channels, events are what was fired. One table per sub-view, all
  // editing in sheets.
  type Row = Record<string, unknown>
  type SubView = 'channels' | 'rules' | 'events'
  type BadgeTone = 'neutral' | 'success' | 'warning' | 'danger' | 'info' | 'brand'
  const asRows = <T,>(rows: T[]): Row[] => rows as unknown as Row[]

  let view = $state<SubView>('channels')
  let loading = $state(true)
  let channels = $state<AlertChannel[]>([])
  let rules = $state<AlertRule[]>([])
  let events = $state<AlertEvent[]>([])
  let eventLimit = $state('50')

  // ── Channel form ──────────────────────────────────────────
  const emptyChannel = () => ({
    name: '',
    channel_type: 'smtp' as AlertChannelType,
    is_active: true,
    smtp_host: '',
    smtp_port: 587,
    smtp_username: '',
    smtp_password: '',
    smtp_from_email: '',
    smtp_from_name: '',
    smtp_use_tls: true,
    smtp_starttls: false,
    api_key: '',
    api_from_email: '',
    api_from_name: '',
    api_base_url: '',
  })
  let channelSheetOpen = $state(false)
  let editingChannel = $state<AlertChannel | null>(null)
  let channelForm = $state(emptyChannel())
  let channelSaving = $state(false)
  let deletingChannel = $state<AlertChannel | null>(null)

  // ── Test send ─────────────────────────────────────────────
  let testChannel = $state<AlertChannel | null>(null)
  let testRecipients = $state('')
  let testSending = $state(false)

  // ── Rule form ─────────────────────────────────────────────
  type Binding = { channel_id: string; checked: boolean; recipients: string }
  const emptyRule = () => ({
    name: '',
    event_type: 'policy.violation',
    severity_min: 'warn',
    enabled: true,
    cooldown_seconds: 300,
    max_attempts: 3,
    subject_template: '',
    body_template: '',
  })
  let ruleSheetOpen = $state(false)
  let editingRule = $state<AlertRule | null>(null)
  let ruleForm = $state(emptyRule())
  let bindings = $state<Binding[]>([])
  let ruleSaving = $state(false)
  let deletingRule = $state<AlertRule | null>(null)

  let selectedEvent = $state<AlertEvent | null>(null)

  const channelTypeOptions = [
    { value: 'smtp', label: 'SMTP' },
    { value: 'resend', label: 'Resend' },
    { value: 'brevo', label: 'Brevo' },
  ]
  const eventTypeOptions = [
    { value: 'policy.violation', label: 'Policy violation' },
    { value: 'schedule.failed', label: 'Schedule failed' },
    { value: 'schedule.slow', label: 'Schedule slow' },
    { value: 'telemetry.monitor', label: 'Telemetry monitor' },
    { value: '*', label: 'All events' },
  ]
  const severityOptions = [
    { value: 'info', label: 'Info' },
    { value: 'warn', label: 'Warning' },
    { value: 'error', label: 'Error' },
    { value: 'critical', label: 'Critical' },
  ]
  const limitOptions = ['25', '50', '100', '200'].map((v) => ({ value: v, label: `${v} events` }))
  const viewItems = [
    { id: 'channels', label: 'Channels' },
    { id: 'rules', label: 'Rules' },
    { id: 'events', label: 'Events' },
  ]

  function severityTone(severity: string | null | undefined): BadgeTone {
    if (severity === 'critical' || severity === 'error') return 'danger'
    if (severity === 'warn') return 'warning'
    if (severity === 'info') return 'info'
    return 'neutral'
  }
  function statusTone(status: string): BadgeTone {
    if (status === 'sent' || status === 'delivered' || status === 'processed') return 'success'
    if (status === 'failed' || status === 'error') return 'danger'
    if (status === 'pending' || status === 'queued') return 'warning'
    return 'neutral'
  }
  function channelTarget(ch: AlertChannel): string {
    const from = String(ch.config?.from_email ?? '')
    const host = ch.channel_type === 'smtp'
      ? String(ch.config?.host ?? '')
      : String(ch.config?.base_url || `${ch.channel_type} API`)
    return [from, host].filter(Boolean).join(' via ')
  }
  function ruleChannelNames(rule: AlertRule): string {
    return rule.channels.map((b) => b.channel_name).join(', ') || '—'
  }
  function eventLabel(v: string): string {
    return eventTypeOptions.find((o) => o.value === v)?.label ?? v
  }

  const channelColumns: DataColumn<Row>[] = [
    { key: 'name', label: 'Name' },
    { key: 'channel_type', label: 'Type', width: '110px' },
    { key: 'target', label: 'Sends from', truncate: true, sortable: false },
    { key: 'is_active', label: 'Status', width: '110px' },
  ]
  const ruleColumns: DataColumn<Row>[] = [
    { key: 'name', label: 'Name' },
    { key: 'event_type', label: 'Event', width: '160px', format: (v) => eventLabel(String(v)) },
    { key: 'severity_min', label: 'Min severity', width: '120px' },
    { key: 'channel_names', label: 'Channels', truncate: true, sortable: false },
    { key: 'enabled', label: 'Status', width: '110px' },
  ]
  const eventColumns: DataColumn<Row>[] = [
    { key: 'created_at', label: 'Time', mono: true, width: '170px', format: (v) => formatDate(v) },
    { key: 'event_type', label: 'Event', width: '160px', format: (v) => eventLabel(String(v)) },
    { key: 'severity', label: 'Severity', width: '110px' },
    { key: 'status', label: 'Status', width: '110px' },
    { key: 'title', label: 'Message', truncate: true },
  ]

  const channelRows = $derived(asRows(channels.map((c) => ({ ...c, target: channelTarget(c) }))))
  const ruleRows = $derived(asRows(rules.map((r) => ({ ...r, channel_names: ruleChannelNames(r) }))))

  // ── Loading ───────────────────────────────────────────────
  async function loadAll() {
    loading = true
    try {
      const [c, r, e] = await Promise.all([
        adminListAlertChannels(),
        adminListAlertRules(),
        adminListAlertEvents({ limit: Number(eventLimit) }),
      ])
      channels = c
      rules = r
      events = e
    } catch (e: any) {
      toastError(e.message)
    } finally {
      loading = false
    }
  }

  async function loadEvents() {
    try {
      events = await adminListAlertEvents({ limit: Number(eventLimit) })
    } catch (e: any) {
      toastError(e.message)
    }
  }

  onMount(loadAll)

  // ── Channels ──────────────────────────────────────────────
  function openNewChannel() {
    editingChannel = null
    channelForm = emptyChannel()
    channelSheetOpen = true
  }

  function openEditChannel(ch: AlertChannel) {
    const cfg = ch.config ?? {}
    editingChannel = ch
    channelForm = {
      ...emptyChannel(),
      name: ch.name,
      channel_type: ch.channel_type,
      is_active: ch.is_active,
      smtp_host: String(cfg.host ?? ''),
      smtp_port: Number(cfg.port ?? 587),
      smtp_username: String(cfg.username ?? ''),
      smtp_from_email: ch.channel_type === 'smtp' ? String(cfg.from_email ?? '') : '',
      smtp_from_name: ch.channel_type === 'smtp' ? String(cfg.from_name ?? '') : '',
      smtp_use_tls: cfg.use_tls !== false,
      smtp_starttls: cfg.starttls === true,
      api_from_email: ch.channel_type !== 'smtp' ? String(cfg.from_email ?? '') : '',
      api_from_name: ch.channel_type !== 'smtp' ? String(cfg.from_name ?? '') : '',
      api_base_url: String(cfg.base_url ?? ''),
    }
    channelSheetOpen = true
  }

  function channelConfig(): Record<string, unknown> {
    const f = channelForm
    const config: Record<string, unknown> = {}
    if (f.channel_type === 'smtp') {
      // Keys match what internal/alerts/dispatcher.go reads (host, port,
      // username, password). The old UI sent smtp_* and was rejected.
      config.host = f.smtp_host
      config.port = f.smtp_port
      config.username = f.smtp_username
      // Secrets are only sent when typed; an empty field keeps the stored one.
      if (f.smtp_password) config.password = f.smtp_password
      config.from_email = f.smtp_from_email
      config.from_name = f.smtp_from_name
      config.use_tls = f.smtp_use_tls
      config.starttls = f.smtp_starttls
    } else {
      if (f.api_key) config.api_key = f.api_key
      config.from_email = f.api_from_email
      config.from_name = f.api_from_name
      if (f.api_base_url) config.base_url = f.api_base_url
    }
    return config
  }

  async function saveChannel() {
    if (!channelForm.name.trim()) return
    channelSaving = true
    try {
      const payload = {
        name: channelForm.name.trim(),
        channel_type: channelForm.channel_type,
        is_active: channelForm.is_active,
        config: channelConfig(),
      }
      if (editingChannel) {
        await adminUpdateAlertChannel(editingChannel.id, payload)
        toastSuccess('Channel updated')
      } else {
        await adminCreateAlertChannel(payload)
        toastSuccess('Channel created')
      }
      channelSheetOpen = false
      await loadAll()
    } catch (e: any) {
      toastError(e.message)
    } finally {
      channelSaving = false
    }
  }

  async function confirmDeleteChannel() {
    if (!deletingChannel) return
    const ch = deletingChannel
    deletingChannel = null
    try {
      await adminDeleteAlertChannel(ch.id)
      toastSuccess('Channel deleted')
      await loadAll()
    } catch (e: any) {
      toastError(e.message)
    }
  }

  function openTest(ch: AlertChannel) {
    testChannel = ch
    testRecipients = ''
  }

  async function sendTest() {
    if (!testChannel) return
    const recipients = testRecipients.split(',').map((r) => r.trim()).filter(Boolean)
    if (recipients.length === 0) {
      toastError('Enter at least one recipient')
      return
    }
    testSending = true
    try {
      await adminTestAlertChannel(testChannel.id, { recipients })
      toastSuccess(`Test alert sent through "${testChannel.name}"`)
      testChannel = null
    } catch (e: any) {
      toastError(e.message)
    } finally {
      testSending = false
    }
  }

  // ── Rules ─────────────────────────────────────────────────
  function bindingsFor(rule: AlertRule | null): Binding[] {
    return channels.map((ch) => {
      const existing = rule?.channels.find((b) => b.channel_id === ch.id)
      return { channel_id: ch.id, checked: !!existing, recipients: existing?.recipients.join(', ') ?? '' }
    })
  }

  function openNewRule() {
    editingRule = null
    ruleForm = emptyRule()
    bindings = bindingsFor(null)
    ruleSheetOpen = true
  }

  function openEditRule(rule: AlertRule) {
    editingRule = rule
    ruleForm = {
      name: rule.name,
      event_type: rule.event_type,
      severity_min: rule.severity_min,
      enabled: rule.enabled,
      cooldown_seconds: rule.cooldown_seconds,
      max_attempts: rule.max_attempts,
      subject_template: rule.subject_template ?? '',
      body_template: rule.body_template ?? '',
    }
    bindings = bindingsFor(rule)
    ruleSheetOpen = true
  }

  async function saveRule() {
    if (!ruleForm.name.trim()) return
    const channelPayload: AlertRuleChannelPayload[] = bindings
      .filter((b) => b.checked)
      .map((b) => ({
        channel_id: b.channel_id,
        recipients: b.recipients.split(',').map((s) => s.trim()).filter(Boolean),
        is_active: true,
      }))
    if (channelPayload.length === 0) {
      toastError('Pick at least one channel')
      return
    }
    const missing = bindings.find((b) => b.checked && !b.recipients.split(',').some((s) => s.trim()))
    if (missing) {
      toastError(`Add at least one recipient for "${channels.find((c) => c.id === missing.channel_id)?.name ?? 'the selected channel'}"`)
      return
    }
    ruleSaving = true
    try {
      const payload = {
        name: ruleForm.name.trim(),
        event_type: ruleForm.event_type,
        severity_min: ruleForm.severity_min,
        enabled: ruleForm.enabled,
        cooldown_seconds: Number(ruleForm.cooldown_seconds),
        max_attempts: Number(ruleForm.max_attempts),
        subject_template: ruleForm.subject_template || undefined,
        body_template: ruleForm.body_template || undefined,
        channels: channelPayload,
      }
      if (editingRule) {
        await adminUpdateAlertRule(editingRule.id, payload)
        toastSuccess('Rule updated')
      } else {
        await adminCreateAlertRule(payload)
        toastSuccess('Rule created')
      }
      ruleSheetOpen = false
      await loadAll()
    } catch (e: any) {
      toastError(e.message)
    } finally {
      ruleSaving = false
    }
  }

  async function confirmDeleteRule() {
    if (!deletingRule) return
    const rule = deletingRule
    deletingRule = null
    try {
      await adminDeleteAlertRule(rule.id)
      toastSuccess('Rule deleted')
      await loadAll()
    } catch (e: any) {
      toastError(e.message)
    }
  }

  function prettyPayload(raw: string | null | undefined): string {
    if (!raw) return ''
    try {
      return JSON.stringify(JSON.parse(raw), null, 2)
    } catch {
      return raw
    }
  }
</script>

<div class="flex h-full min-h-0 flex-col">
  <div class="flex h-10 shrink-0 items-center gap-2 px-5">
    <Tabs variant="segmented" size="sm" items={viewItems} value={view} onchange={(id) => (view = id as SubView)} />
    <div class="flex-1"></div>
    {#if view === 'channels'}
      <span class="text-xs text-fg-4">{channels.length} {channels.length === 1 ? 'channel' : 'channels'}</span>
      <Button size="sm" onclick={openNewChannel}><Plus size={13} /> New channel</Button>
    {:else if view === 'rules'}
      <span class="text-xs text-fg-4">{rules.length} {rules.length === 1 ? 'rule' : 'rules'}</span>
      <Button size="sm" onclick={openNewRule}><Plus size={13} /> New rule</Button>
    {:else}
      <Select size="sm" class="w-32" options={limitOptions} bind:value={eventLimit} onchange={() => void loadEvents()} />
      <Button icon variant="ghost" size="sm" aria-label="Refresh events" title="Refresh" onclick={() => void loadEvents()}>
        <RefreshCw size={14} />
      </Button>
    {/if}
  </div>

  <div class="min-h-0 flex-1 px-5 pb-4">
    {#if loading}
      <div class="flex h-full items-center justify-center"><Spinner /></div>
    {:else}
      <div class="h-full overflow-hidden rounded-lg border border-edge-subtle bg-surface">
        {#if view === 'channels'}
          <DataTable
            columns={channelColumns}
            rows={channelRows}
            fill
            rowKey={(r) => String(r.id)}
            sort={{ key: 'name', dir: 'asc' }}
            emptyTitle="No channels yet"
            emptyDescription="A channel holds the credentials alerts are sent with: an SMTP relay, Resend or Brevo."
          >
            {#snippet cell(row, col, value)}
              {#if col.key === 'name'}
                <span class="font-medium text-fg">{value}</span>
              {:else if col.key === 'channel_type'}
                <Badge>{String(value).toUpperCase()}</Badge>
              {:else if col.key === 'is_active'}
                <Badge dot tone={row.is_active ? 'success' : 'neutral'}>{row.is_active ? 'Active' : 'Off'}</Badge>
              {:else}
                {value}
              {/if}
            {/snippet}
            {#snippet actions(row)}
              {@const ch = row as unknown as AlertChannel}
              <Button icon variant="ghost" size="xs" aria-label="Send a test alert" title="Send test" onclick={() => openTest(ch)}><Send size={13} /></Button>
              <Button icon variant="ghost" size="xs" aria-label="Edit channel" title="Edit" onclick={() => openEditChannel(ch)}><Pencil size={13} /></Button>
              <Button icon variant="ghost" size="xs" aria-label="Delete channel" title="Delete" onclick={() => (deletingChannel = ch)}><Trash2 size={13} /></Button>
            {/snippet}
          </DataTable>
        {:else if view === 'rules'}
          <DataTable
            columns={ruleColumns}
            rows={ruleRows}
            fill
            rowKey={(r) => String(r.id)}
            sort={{ key: 'name', dir: 'asc' }}
            emptyTitle="No rules yet"
            emptyDescription="A rule watches an event type at a minimum severity and delivers it to one or more channels."
          >
            {#snippet cell(row, col, value)}
              {#if col.key === 'name'}
                <span class="font-medium text-fg">{value}</span>
              {:else if col.key === 'severity_min'}
                <Badge tone={severityTone(String(value))}>{value}</Badge>
              {:else if col.key === 'enabled'}
                <Badge dot tone={row.enabled ? 'success' : 'neutral'}>{row.enabled ? 'Enabled' : 'Off'}</Badge>
              {:else}
                {value}
              {/if}
            {/snippet}
            {#snippet actions(row)}
              {@const rule = row as unknown as AlertRule}
              <Button icon variant="ghost" size="xs" aria-label="Edit rule" title="Edit" onclick={() => openEditRule(rule)}><Pencil size={13} /></Button>
              <Button icon variant="ghost" size="xs" aria-label="Delete rule" title="Delete" onclick={() => (deletingRule = rule)}><Trash2 size={13} /></Button>
            {/snippet}
          </DataTable>
        {:else}
          <DataTable
            columns={eventColumns}
            rows={asRows(events)}
            fill
            rowKey={(r) => String(r.id)}
            sort={{ key: 'created_at', dir: 'desc' }}
            emptyTitle="No alert events yet"
            emptyDescription="Events appear here once a rule matches something."
            onrowclick={(row) => (selectedEvent = row as unknown as AlertEvent)}
          >
            {#snippet cell(row, col, value)}
              {#if col.key === 'severity'}
                <Badge tone={severityTone(String(value))}>{value}</Badge>
              {:else if col.key === 'status'}
                <Badge tone={statusTone(String(value))}>{value}</Badge>
              {:else}
                {value}
              {/if}
            {/snippet}
          </DataTable>
        {/if}
      </div>
    {/if}
  </div>
</div>

<!-- Channel sheet -->
<Sheet
  open={channelSheetOpen}
  title={editingChannel ? 'Edit channel' : 'New channel'}
  description="Where alerts are sent from. SMTP for your own relay, Resend or Brevo for API delivery."
  size="md"
  onclose={() => (channelSheetOpen = false)}
>
  <form
    id="alert-channel-form"
    class="space-y-4"
    onsubmit={(e) => { e.preventDefault(); void saveChannel() }}
  >
    <div class="grid grid-cols-1 gap-3 md:grid-cols-2">
      <FormField label="Name" for="ch-name" required>
        <Input id="ch-name" placeholder="Ops mail" bind:value={channelForm.name} required />
      </FormField>
      <FormField label="Type" for="ch-type">
        <Select id="ch-type" options={channelTypeOptions} value={channelForm.channel_type} onchange={(v) => (channelForm.channel_type = v as AlertChannelType)} />
      </FormField>
    </div>

    {#if channelForm.channel_type === 'smtp'}
      <div class="grid grid-cols-1 gap-3 md:grid-cols-2">
        <FormField label="SMTP host" for="ch-host" required>
          <Input id="ch-host" placeholder="smtp.example.com" bind:value={channelForm.smtp_host} required />
        </FormField>
        <FormField label="Port" for="ch-port">
          <Input id="ch-port" type="number" min="1" max="65535" bind:value={channelForm.smtp_port} />
        </FormField>
        <FormField label="Username" for="ch-user">
          <Input id="ch-user" bind:value={channelForm.smtp_username} autocomplete="off" />
        </FormField>
        <FormField label="Password" for="ch-pass" hint={editingChannel?.has_secret ? 'Leave empty to keep the stored password.' : undefined}>
          <Input id="ch-pass" type="password" bind:value={channelForm.smtp_password} autocomplete="new-password" />
        </FormField>
        <FormField label="From email" for="ch-from" required>
          <Input id="ch-from" type="email" placeholder="alerts@company.com" bind:value={channelForm.smtp_from_email} required />
        </FormField>
        <FormField label="From name" for="ch-from-name">
          <Input id="ch-from-name" placeholder="CH-UI Alerts" bind:value={channelForm.smtp_from_name} />
        </FormField>
      </div>
      <div class="flex flex-wrap items-center gap-4">
        <label class="ds-checkbox-label">
          <input type="checkbox" class="ds-checkbox" bind:checked={channelForm.smtp_use_tls} />
          TLS
        </label>
        <label class="ds-checkbox-label">
          <input type="checkbox" class="ds-checkbox" bind:checked={channelForm.smtp_starttls} />
          STARTTLS
        </label>
      </div>
    {:else}
      <div class="grid grid-cols-1 gap-3 md:grid-cols-2">
        <FormField label="API key" for="ch-key" required={!editingChannel} class="md:col-span-2" hint={editingChannel?.has_secret ? 'Leave empty to keep the stored key.' : undefined}>
          <Input id="ch-key" type="password" bind:value={channelForm.api_key} required={!editingChannel} autocomplete="new-password" />
        </FormField>
        <FormField label="From email" for="ch-api-from" required>
          <Input id="ch-api-from" type="email" placeholder="alerts@company.com" bind:value={channelForm.api_from_email} required />
        </FormField>
        <FormField label="From name" for="ch-api-from-name">
          <Input id="ch-api-from-name" placeholder="CH-UI Alerts" bind:value={channelForm.api_from_name} />
        </FormField>
        <FormField label="API base URL (advanced)" for="ch-base" hint="Leave empty. CH-UI calls https://api.resend.com or https://api.brevo.com. Only set this for a proxy of the provider's API, never a website: the value is used as the API endpoint." class="md:col-span-2">
          <Input id="ch-base" mono placeholder="Default: provider API" bind:value={channelForm.api_base_url} />
        </FormField>
      </div>
    {/if}

    <label class="ds-checkbox-label">
      <input type="checkbox" class="ds-checkbox" bind:checked={channelForm.is_active} />
      Active
    </label>
  </form>
  {#snippet footer()}
    <Button variant="ghost" size="sm" onclick={() => (channelSheetOpen = false)}>Cancel</Button>
    <Button size="sm" type="submit" loading={channelSaving} disabled={!channelForm.name.trim()} onclick={() => void saveChannel()}>
      {editingChannel ? 'Save changes' : 'Create channel'}
    </Button>
  {/snippet}
</Sheet>

<!-- Rule sheet -->
<Sheet
  open={ruleSheetOpen}
  title={editingRule ? 'Edit rule' : 'New rule'}
  description="Which events to send, from which severity, and to whom."
  size="md"
  onclose={() => (ruleSheetOpen = false)}
>
  <form
    class="space-y-4"
    onsubmit={(e) => { e.preventDefault(); void saveRule() }}
  >
    <FormField label="Name" for="rule-name" required>
      <Input id="rule-name" placeholder="Critical policy violations" bind:value={ruleForm.name} required />
    </FormField>
    <div class="grid grid-cols-1 gap-3 md:grid-cols-2">
      <FormField label="Event" for="rule-event">
        <Select id="rule-event" options={eventTypeOptions} bind:value={ruleForm.event_type} />
      </FormField>
      <FormField label="Minimum severity" for="rule-sev" hint="Events below this are ignored.">
        <Select id="rule-sev" options={severityOptions} bind:value={ruleForm.severity_min} />
      </FormField>
      <FormField label="Cooldown (seconds)" for="rule-cooldown" hint="Same fingerprint is not re-sent within this window.">
        <Input id="rule-cooldown" type="number" min="0" bind:value={ruleForm.cooldown_seconds} />
      </FormField>
      <FormField label="Max attempts" for="rule-attempts">
        <Input id="rule-attempts" type="number" min="1" bind:value={ruleForm.max_attempts} />
      </FormField>
    </div>
    <FormField label="Subject template" for="rule-subject" hint="Optional. Leave empty for the default subject.">
      <Input id="rule-subject" bind:value={ruleForm.subject_template} />
    </FormField>
    <FormField label="Body template" for="rule-body" hint="Optional. Leave empty for the default body.">
      <Textarea id="rule-body" rows={3} bind:value={ruleForm.body_template} />
    </FormField>

    <div class="space-y-2 border-t border-edge-subtle pt-4">
      <div class="text-[13px] font-semibold text-fg">Deliver to</div>
      {#if channels.length === 0}
        <p class="text-xs text-warning">Create a channel first; a rule needs at least one.</p>
      {:else}
        <div class="divide-y divide-edge-subtle rounded-md border border-edge-subtle">
          {#each bindings as b, i (b.channel_id)}
            {@const ch = channels.find((c) => c.id === b.channel_id)}
            <div class="px-3 py-2">
              <label class="ds-checkbox-label">
                <input type="checkbox" class="ds-checkbox" bind:checked={bindings[i].checked} />
                <span class="font-medium text-fg">{ch?.name ?? b.channel_id}</span>
                <span class="text-xs text-fg-4">{ch?.channel_type.toUpperCase()}</span>
              </label>
              {#if b.checked}
                <div class="mt-2 pl-6">
                  <Input size="sm" placeholder="Recipients, comma-separated: ops@company.com, data@company.com" bind:value={bindings[i].recipients} />
                </div>
              {/if}
            </div>
          {/each}
        </div>
      {/if}
    </div>

    <label class="ds-checkbox-label">
      <input type="checkbox" class="ds-checkbox" bind:checked={ruleForm.enabled} />
      Enabled
    </label>
  </form>
  {#snippet footer()}
    <Button variant="ghost" size="sm" onclick={() => (ruleSheetOpen = false)}>Cancel</Button>
    <Button size="sm" loading={ruleSaving} disabled={!ruleForm.name.trim()} onclick={() => void saveRule()}>
      {editingRule ? 'Save changes' : 'Create rule'}
    </Button>
  {/snippet}
</Sheet>

<!-- Test send -->
<Modal open={testChannel !== null} title="Send a test alert" description={testChannel ? `Through "${testChannel.name}" (${testChannel.channel_type.toUpperCase()}).` : ''} size="sm" onclose={() => (testChannel = null)}>
  <FormField label="Recipients" for="test-recipients" hint="Comma-separated email addresses.">
    <Input id="test-recipients" placeholder="you@company.com" bind:value={testRecipients} onkeydown={(e) => e.key === 'Enter' && void sendTest()} />
  </FormField>
  {#snippet footer()}
    <Button variant="ghost" size="sm" onclick={() => (testChannel = null)}>Cancel</Button>
    <Button size="sm" loading={testSending} onclick={() => void sendTest()}><Send size={13} /> Send</Button>
  {/snippet}
</Modal>

<!-- Event detail -->
<Sheet open={selectedEvent !== null} title={selectedEvent?.title ?? 'Alert event'} description={selectedEvent ? `${eventLabel(selectedEvent.event_type)} · ${formatDate(selectedEvent.created_at)}` : ''} size="md" onclose={() => (selectedEvent = null)}>
  {#if selectedEvent}
    <div class="space-y-4">
      <div class="flex flex-wrap items-center gap-2">
        <Badge tone={severityTone(selectedEvent.severity)}>{selectedEvent.severity}</Badge>
        <Badge tone={statusTone(selectedEvent.status)}>{selectedEvent.status}</Badge>
        {#if selectedEvent.processed_at}<span class="text-xs text-fg-3">processed {formatDate(selectedEvent.processed_at)}</span>{/if}
      </div>
      <p class="whitespace-pre-wrap text-[13px] leading-relaxed text-fg-2">{selectedEvent.message}</p>
      {#if selectedEvent.source_ref}
        <div class="text-xs text-fg-3">Source: <span class="font-mono text-fg-2">{selectedEvent.source_ref}</span></div>
      {/if}
      {#if selectedEvent.payload_json}
        <pre class="overflow-x-auto rounded-md border border-edge-subtle bg-surface-2 p-3 font-mono text-xs leading-relaxed text-fg">{prettyPayload(selectedEvent.payload_json)}</pre>
      {/if}
    </div>
  {/if}
</Sheet>

<ConfirmDialog
  open={deletingChannel !== null}
  title="Delete channel?"
  description={deletingChannel ? `"${deletingChannel.name}" will be removed. Rules that deliver to it lose that binding.` : ''}
  confirmLabel="Delete"
  destructive
  onconfirm={confirmDeleteChannel}
  oncancel={() => (deletingChannel = null)}
/>

<ConfirmDialog
  open={deletingRule !== null}
  title="Delete rule?"
  description={deletingRule ? `"${deletingRule.name}" will stop delivering alerts.` : ''}
  confirmLabel="Delete"
  destructive
  onconfirm={confirmDeleteRule}
  oncancel={() => (deletingRule = null)}
/>
