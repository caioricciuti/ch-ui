<!-- SPDX-License-Identifier: BUSL-1.1 -->
<script lang="ts">
  import { onMount } from 'svelte'
  import { getSession } from '../../stores/session.svelte'
  import type { AlertChannel, AlertChannelType, WebhookFormat } from '../../types/alerts'
  import {
    adminListAlertChannels, adminCreateAlertChannel, adminUpdateAlertChannel, adminDeleteAlertChannel,
    adminTestAlertChannel,
  } from '../../api/alerts'
  import { channelTarget, isWebhook } from './alerting-admin'
  import { success as toastSuccess, error as toastError } from '../../stores/toast.svelte'
  import Button from '../common/Button.svelte'
  import Badge from '../common/Badge.svelte'
  import Select from '../common/Select.svelte'
  import Input from '../common/Input.svelte'
  import FormField from '../common/FormField.svelte'
  import Sheet from '../common/Sheet.svelte'
  import Modal from '../common/Modal.svelte'
  import ConfirmDialog from '../common/ConfirmDialog.svelte'
  import Spinner from '../common/Spinner.svelte'
  import DataTable, { type DataColumn } from '../common/DataTable.svelte'
  import { Send, Pencil, Trash2, Plus } from 'lucide-svelte'

  // Alert channels hold delivery credentials. Reading is open to everyone;
  // changing them and sending a test is admin only (server enforced).
  const isAdmin = $derived(getSession()?.role === 'admin')
  type Row = Record<string, unknown>
  const asRows = <T,>(rows: T[]): Row[] => rows as unknown as Row[]

  let loading = $state(true)
  let channels = $state<AlertChannel[]>([])

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
    webhook_url: '',
    webhook_format: 'generic' as WebhookFormat,
    webhook_header_name: '',
    webhook_header_value: '',
    webhook_signing_secret: '',
    webhook_allow_http: false,
    webhook_allow_private: false,
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

  const channelTypeOptions = [
    { value: 'smtp', label: 'SMTP' },
    { value: 'resend', label: 'Resend' },
    { value: 'brevo', label: 'Brevo' },
    { value: 'webhook', label: 'Webhook (JSON, Slack, Teams)' },
  ]
  const webhookFormatOptions = [
    { value: 'generic', label: 'Generic JSON' },
    { value: 'slack', label: 'Slack incoming webhook' },
    { value: 'teams', label: 'Microsoft Teams workflow' },
  ]

  const channelColumns: DataColumn<Row>[] = [
    { key: 'name', label: 'Name' },
    { key: 'channel_type', label: 'Type', width: '110px' },
    { key: 'target', label: 'Sends from', truncate: true, sortable: false },
    { key: 'is_active', label: 'Status', width: '110px' },
  ]

  const channelRows = $derived(asRows(channels.map((c) => ({ ...c, target: channelTarget(c) }))))

  async function loadChannels() {
    loading = true
    try {
      channels = await adminListAlertChannels()
    } catch (e: unknown) {
      toastError(e instanceof Error ? e.message : String(e))
    } finally {
      loading = false
    }
  }

  onMount(loadChannels)

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
      webhook_url: String(cfg.url ?? ''),
      webhook_format: (cfg.format === 'slack' || cfg.format === 'teams' ? cfg.format : 'generic') as WebhookFormat,
      webhook_header_name: String(cfg.secret_header_name ?? ''),
      webhook_allow_http: cfg.allow_http === true,
      webhook_allow_private: cfg.allow_private_network === true,
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
    } else if (f.channel_type === 'webhook') {
      config.url = f.webhook_url.trim()
      config.format = f.webhook_format
      config.secret_header_name = f.webhook_header_name.trim()
      // Secrets are only sent when typed; an empty field keeps the stored one.
      if (f.webhook_header_value) config.secret_header_value = f.webhook_header_value
      if (f.webhook_signing_secret) config.signing_secret = f.webhook_signing_secret
      config.allow_http = f.webhook_allow_http
      config.allow_private_network = f.webhook_allow_private
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
      await loadChannels()
    } catch (e: unknown) {
      toastError(e instanceof Error ? e.message : String(e))
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
      await loadChannels()
    } catch (e: unknown) {
      toastError(e instanceof Error ? e.message : String(e))
    }
  }

  function openTest(ch: AlertChannel) {
    testChannel = ch
    testRecipients = ''
  }
  const testIsWebhook = $derived(isWebhook(testChannel?.channel_type))

  async function sendTest() {
    if (!testChannel) return
    const recipients = testRecipients.split(',').map((r) => r.trim()).filter(Boolean)
    if (recipients.length === 0 && !testIsWebhook) {
      toastError('Enter at least one recipient')
      return
    }
    testSending = true
    try {
      await adminTestAlertChannel(testChannel.id, testIsWebhook ? {} : { recipients })
      toastSuccess(`Test alert sent through "${testChannel.name}"`)
      testChannel = null
    } catch (e: unknown) {
      toastError(e instanceof Error ? e.message : String(e))
    } finally {
      testSending = false
    }
  }
</script>

<div class="flex h-full min-h-0 flex-col">
  <div class="flex h-10 shrink-0 items-center gap-2 px-5">
    <span class="text-xs text-fg-4">{channels.length} {channels.length === 1 ? 'channel' : 'channels'}</span>
    <div class="flex-1"></div>
    {#if isAdmin}
      <Button size="sm" onclick={openNewChannel}><Plus size={13} /> New channel</Button>
    {/if}
  </div>

  <div class="min-h-0 flex-1 px-5 pb-4">
    {#if loading}
      <div class="flex h-full items-center justify-center"><Spinner /></div>
    {:else}
      <div class="h-full overflow-hidden rounded-lg border border-edge-subtle bg-surface">
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
            {#if isAdmin}
              <Button icon variant="ghost" size="xs" aria-label="Send a test alert" title="Send test" onclick={() => openTest(ch)}><Send size={13} /></Button>
              <Button icon variant="ghost" size="xs" aria-label="Edit channel" title="Edit" onclick={() => openEditChannel(ch)}><Pencil size={13} /></Button>
              <Button icon variant="ghost" size="xs" aria-label="Delete channel" title="Delete" onclick={() => (deletingChannel = ch)}><Trash2 size={13} /></Button>
            {/if}
          {/snippet}
        </DataTable>
      </div>
    {/if}
  </div>
</div>

<!-- Channel sheet -->
<Sheet
  open={channelSheetOpen}
  title={editingChannel ? 'Edit channel' : 'New channel'}
  description="Where alerts are sent. SMTP for your own relay, Resend or Brevo for API delivery, or a webhook for Slack, Teams or your own endpoint."
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
    {:else if channelForm.channel_type === 'webhook'}
      <div class="grid grid-cols-1 gap-3 md:grid-cols-2">
        <FormField label="URL" for="ch-hook-url" required class="md:col-span-2" hint="CH-UI POSTs JSON here for every alert. HTTPS is required unless you allow plain HTTP below.">
          <Input id="ch-hook-url" mono placeholder="https://hooks.slack.com/services/..." bind:value={channelForm.webhook_url} required />
        </FormField>
        <FormField label="Format" for="ch-hook-format" class="md:col-span-2" hint="Generic sends event_type, severity, title, message, fingerprint, timestamp and link. Slack and Teams shape the body for their incoming webhooks.">
          <Select id="ch-hook-format" options={webhookFormatOptions} value={channelForm.webhook_format} onchange={(v) => (channelForm.webhook_format = v as WebhookFormat)} />
        </FormField>
        <FormField label="Secret header name" for="ch-hook-hname" hint="Optional, for example Authorization or X-Api-Key.">
          <Input id="ch-hook-hname" mono bind:value={channelForm.webhook_header_name} autocomplete="off" />
        </FormField>
        <FormField label="Secret header value" for="ch-hook-hval" hint={editingChannel?.has_secret ? 'Leave empty to keep the stored value.' : undefined}>
          <Input id="ch-hook-hval" type="password" bind:value={channelForm.webhook_header_value} autocomplete="new-password" />
        </FormField>
        <FormField label="Signing secret" for="ch-hook-sign" class="md:col-span-2" hint={`Optional. Adds X-CHUI-Timestamp and X-CHUI-Signature: sha256=HMAC(secret, "timestamp.body").${editingChannel?.has_secret ? ' Leave empty to keep the stored secret.' : ''}`}>
          <Input id="ch-hook-sign" type="password" bind:value={channelForm.webhook_signing_secret} autocomplete="new-password" />
        </FormField>
      </div>
      <div class="flex flex-wrap items-center gap-4">
        <label class="ds-checkbox-label">
          <input type="checkbox" class="ds-checkbox" bind:checked={channelForm.webhook_allow_http} />
          Allow plain HTTP
        </label>
        <label class="ds-checkbox-label">
          <input type="checkbox" class="ds-checkbox" bind:checked={channelForm.webhook_allow_private} />
          Allow private network addresses
        </label>
      </div>
      {#if channelForm.webhook_allow_private}
        <p class="text-xs text-warning">CH-UI will post to loopback and internal addresses through this channel. Only enable it for an endpoint you control.</p>
      {/if}
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

<!-- Test send -->
<Modal open={testChannel !== null} title="Send a test alert" description={testChannel ? `Through "${testChannel.name}" (${testChannel.channel_type.toUpperCase()}).` : ''} size="sm" onclose={() => (testChannel = null)}>
  {#if testIsWebhook}
    <p class="text-[13px] text-fg-2">Posts a test event to the channel URL.</p>
  {:else}
    <FormField label="Recipients" for="test-recipients" hint="Comma-separated email addresses.">
      <Input id="test-recipients" placeholder="you@company.com" bind:value={testRecipients} onkeydown={(e) => e.key === 'Enter' && void sendTest()} />
    </FormField>
  {/if}
  {#snippet footer()}
    <Button variant="ghost" size="sm" onclick={() => (testChannel = null)}>Cancel</Button>
    <Button size="sm" loading={testSending} onclick={() => void sendTest()}><Send size={13} /> Send</Button>
  {/snippet}
</Modal>

<ConfirmDialog
  open={deletingChannel !== null}
  title="Delete channel?"
  description={deletingChannel ? `"${deletingChannel.name}" will be removed. Rules that deliver to it lose that binding.` : ''}
  confirmLabel="Delete"
  destructive
  onconfirm={confirmDeleteChannel}
  oncancel={() => (deletingChannel = null)}
/>

