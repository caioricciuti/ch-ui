<!-- SPDX-License-Identifier: BUSL-1.1 -->
<script lang="ts">
  import { onMount } from 'svelte'
  import { getSession } from '../../stores/session.svelte'
  import { setSection } from '../../stores/nav.svelte'
  import type { AlertChannel, AlertRule } from '../../types/alerts'
  import {
    adminListAlertChannels, adminListAlertRules, adminCreateAlertRule, adminUpdateAlertRule,
    adminDeleteAlertRule, type AlertRuleChannelPayload,
  } from '../../api/alerts'
  import { listConnections } from '../../api/auth'
  import type { Connection } from '../../types/api'
  import { EVENT_TYPE_OPTIONS as eventTypeOptions, eventTypeDescription, eventTypeLabel as eventLabel } from './alerting-silences'
  import { isSupportedChannelType, isWebhook, severityTone } from './alerting-admin'
  import { success as toastSuccess, error as toastError, errorFrom as toastErrorFrom } from '../../stores/toast.svelte'
  import Button from '../common/Button.svelte'
  import Badge from '../common/Badge.svelte'
  import Select from '../common/Select.svelte'
  import Input from '../common/Input.svelte'
  import Textarea from '../common/Textarea.svelte'
  import FormField from '../common/FormField.svelte'
  import Sheet from '../common/Sheet.svelte'
  import ConfirmDialog from '../common/ConfirmDialog.svelte'
  import Spinner from '../common/Spinner.svelte'
  import DataTable, { type DataColumn } from '../common/DataTable.svelte'
  import { Pencil, Trash2, Plus } from 'lucide-svelte'

  // Alert rules route events to channels. Reading is open to everyone;
  // changing them is admin only (server enforced).
  const isAdmin = $derived(getSession()?.role === 'admin')
  type Row = Record<string, unknown>
  const asRows = <T,>(rows: T[]): Row[] => rows as unknown as Row[]

  let loading = $state(true)
  let channels = $state<AlertChannel[]>([])
  let rules = $state<AlertRule[]>([])
  let connections = $state<Connection[]>([])
  // Without the list nothing can be called deleted, so rule scopes are kept as they are.
  let connectionsLoaded = $state(false)

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
  // Connection ids the rule is scoped to; empty = every connection.
  let ruleConnections = $state<string[]>([])
  let ruleSaving = $state(false)
  let deletingRule = $state<AlertRule | null>(null)

  const severityOptions = [
    { value: 'info', label: 'Info' },
    { value: 'warn', label: 'Warning' },
    { value: 'error', label: 'Error' },
    { value: 'critical', label: 'Critical' },
  ]

  function ruleChannelNames(rule: AlertRule): string {
    return rule.channels.map((b) => b.channel_name).join(', ') || '-'
  }
  const connectionExists = (id: string) => !connectionsLoaded || connections.some((c) => c.id === id)
  function ruleConnectionNames(rule: AlertRule): string {
    const ids = rule.connection_ids ?? []
    if (ids.length === 0) return 'All'
    if (!connectionsLoaded) return `${ids.length} selected`
    return ids.map((id) => connections.find((c) => c.id === id)?.name).filter(Boolean).join(', ')
  }

  const ruleColumns: DataColumn<Row>[] = [
    { key: 'name', label: 'Name' },
    { key: 'event_type', label: 'Event', width: '160px', format: (v) => eventLabel(String(v)) },
    { key: 'severity_min', label: 'Min severity', width: '120px' },
    { key: 'channel_names', label: 'Channels', truncate: true, sortable: false },
    { key: 'connection_names', label: 'Connections', truncate: true, sortable: false },
    { key: 'enabled', label: 'Status', width: '110px' },
  ]

  const ruleRows = $derived(asRows(rules.map((r) => ({ ...r, channel_names: ruleChannelNames(r), connection_names: ruleConnectionNames(r) }))))

  async function loadAll() {
    loading = true
    try {
      const [c, r, conns] = await Promise.all([
        adminListAlertChannels(),
        adminListAlertRules(),
        // Only names and the rule scope list need it; keep the page usable without.
        listConnections().then((list) => {
          connectionsLoaded = true
          return list
        }).catch(() => connections),
      ])
      channels = c
      rules = r
      connections = conns
    } catch (e: unknown) {
      toastErrorFrom(e)
    } finally {
      loading = false
    }
  }

  onMount(loadAll)

  // Rules cannot be saved without a channel; send the admin to make one.
  function goToChannels() {
    ruleSheetOpen = false
    setSection('alert-channels')
  }

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
    ruleConnections = []
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
    ruleConnections = [...(rule.connection_ids ?? [])]
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
    const missing = bindings.find((b) => b.checked && !isWebhook(channels.find((c) => c.id === b.channel_id)?.channel_type) && !b.recipients.split(',').some((s) => s.trim()))
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
        // Deleted connections are dropped; the server rejects unknown ids.
        connection_ids: liveRuleConnections.length ? liveRuleConnections : null,
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
    } catch (e: unknown) {
      toastErrorFrom(e)
    } finally {
      ruleSaving = false
    }
  }

  const liveRuleConnections = $derived(ruleConnections.filter(connectionExists))
  const deletedRuleConnections = $derived(ruleConnections.filter((id) => !connectionExists(id)))

  function toggleRuleConnection(id: string, checked: boolean) {
    ruleConnections = checked ? [...ruleConnections, id] : ruleConnections.filter((x) => x !== id)
  }

  async function confirmDeleteRule() {
    if (!deletingRule) return
    const rule = deletingRule
    deletingRule = null
    try {
      await adminDeleteAlertRule(rule.id)
      toastSuccess('Rule deleted')
      await loadAll()
    } catch (e: unknown) {
      toastErrorFrom(e)
    }
  }
</script>

<div class="flex h-full min-h-0 flex-col">
  <div class="flex h-10 shrink-0 items-center gap-2 px-5">
    <span class="text-xs text-fg-4">{rules.length} {rules.length === 1 ? 'rule' : 'rules'}</span>
    <div class="flex-1"></div>
    {#if isAdmin}
      <Button size="sm" onclick={openNewRule}><Plus size={13} /> New rule</Button>
    {/if}
  </div>

  <div class="min-h-0 flex-1 px-5 pb-4">
    {#if loading}
      <div class="flex h-full items-center justify-center"><Spinner /></div>
    {:else}
      <div class="h-full overflow-hidden rounded-lg border border-edge-subtle bg-surface">
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
            {:else if col.key === 'connection_names'}
              {@const deleted = ((row.connection_ids as string[] | null) ?? []).filter((id) => !connectionExists(id)).length}
              <span class="inline-flex items-center gap-1.5">
                {#if value}<span class="truncate">{value}</span>{/if}
                {#if deleted > 0}<Badge tone="warning">{deleted === 1 ? 'Deleted connection' : `${deleted} deleted connections`}</Badge>{/if}
              </span>
            {:else}
              {value}
            {/if}
          {/snippet}
          {#snippet actions(row)}
            {@const rule = row as unknown as AlertRule}
            {#if isAdmin}
              <Button icon variant="ghost" size="xs" aria-label="Edit rule" title="Edit" onclick={() => openEditRule(rule)}><Pencil size={13} /></Button>
              <Button icon variant="ghost" size="xs" aria-label="Delete rule" title="Delete" onclick={() => (deletingRule = rule)}><Trash2 size={13} /></Button>
            {/if}
          {/snippet}
        </DataTable>
      </div>
    {/if}
  </div>
</div>

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
      <FormField label="Event" for="rule-event" hint={eventTypeDescription(ruleForm.event_type) || undefined}>
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
        <p class="text-xs text-warning">
          A rule needs at least one channel.
          <button type="button" class="text-accent hover:underline" onclick={goToChannels}>Create a channel</button> first.
        </p>
      {:else}
        <div class="divide-y divide-edge-subtle rounded-md border border-edge-subtle">
          {#each bindings as b, i (b.channel_id)}
            {@const ch = channels.find((c) => c.id === b.channel_id)}
            <div class="px-3 py-2">
              <label class="ds-checkbox-label">
                <input type="checkbox" class="ds-checkbox" bind:checked={bindings[i].checked} />
                <span class="font-medium text-fg">{ch?.name ?? b.channel_id}</span>
                <span class="text-xs text-fg-4">{ch?.channel_type.toUpperCase()}</span>
                {#if ch && !isSupportedChannelType(ch.channel_type)}<Badge tone="warning">Unsupported</Badge>{/if}
              </label>
              {#if b.checked && isWebhook(ch?.channel_type)}
                <p class="mt-1 pl-6 text-xs text-fg-3">Posts to the channel URL; no recipients needed.</p>
              {:else if b.checked}
                <div class="mt-2 pl-6">
                  <Input size="sm" placeholder="Recipients, comma-separated: ops@company.com, data@company.com" bind:value={bindings[i].recipients} />
                </div>
              {/if}
            </div>
          {/each}
        </div>
      {/if}
    </div>

    <div class="space-y-2 border-t border-edge-subtle pt-4">
      <div class="text-[13px] font-semibold text-fg">Connections</div>
      <p class="text-xs text-fg-3">Only events from the checked connections match. None checked means every connection.</p>
      {#if !connectionsLoaded}
        <p class="text-xs text-warning">Could not load the connection list. Saving keeps the current connections.</p>
      {/if}
      {#if connections.length > 0 || deletedRuleConnections.length > 0}
        <div class="divide-y divide-edge-subtle rounded-md border border-edge-subtle">
          {#each connections as conn (conn.id)}
            <div class="px-3 py-2">
              <label class="ds-checkbox-label">
                <input
                  type="checkbox"
                  class="ds-checkbox"
                  checked={ruleConnections.includes(conn.id)}
                  onchange={(e) => toggleRuleConnection(conn.id, e.currentTarget.checked)}
                />
                <span class="font-medium text-fg">{conn.name}</span>
              </label>
            </div>
          {/each}
          {#each deletedRuleConnections as id (id)}
            <div class="flex items-center gap-2 px-3 py-2">
              <Badge tone="warning">Deleted connection</Badge>
              <span class="truncate font-mono text-xs text-fg-3">{id}</span>
              <span class="ml-auto text-xs text-fg-4">Removed on save</span>
            </div>
          {/each}
        </div>
      {/if}
      {#if deletedRuleConnections.length > 0 && liveRuleConnections.length === 0}
        <p class="text-xs text-warning">Every connection this rule was limited to is gone. Saving without checking one makes it match every connection.</p>
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

<ConfirmDialog
  open={deletingRule !== null}
  title="Delete rule?"
  description={deletingRule ? `"${deletingRule.name}" will stop delivering alerts.` : ''}
  confirmLabel="Delete"
  destructive
  onconfirm={confirmDeleteRule}
  oncancel={() => (deletingRule = null)}
/>
