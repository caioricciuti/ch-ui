<!-- SPDX-License-Identifier: BUSL-1.1 -->
<script lang="ts">
  import { onMount } from 'svelte'
  import { getSession } from '../../stores/session.svelte'
  import { success as toastSuccess, error as toastError } from '../../stores/toast.svelte'
  import { listSilences, createSilence, updateSilence, expireSilence } from '../../api/alerts'
  import { fetchSqlAlerts } from '../../api/alerting'
  import { listConnections } from '../../api/auth'
  import type { Connection } from '../../types/api'
  import type { Silence, SilenceInput, SqlAlert } from '../../types/alerts'
  import { HEALTH_CHECK_LABELS, HEALTH_CHECK_ORDER } from './alerting-format'
  import {
    EVENT_TYPE_OPTIONS, SILENCE_DURATION_OPTIONS, durationPreset, formatWindow, hasMatcher, healthSourceRef,
    localInputToIso, silenceMatchSummary, silenceStatusLabel, silenceStatusTone, toLocalInput,
  } from './alerting-silences'
  import Button from '../common/Button.svelte'
  import Badge from '../common/Badge.svelte'
  import Input from '../common/Input.svelte'
  import Select from '../common/Select.svelte'
  import FormField from '../common/FormField.svelte'
  import Sheet from '../common/Sheet.svelte'
  import ConfirmDialog from '../common/ConfirmDialog.svelte'
  import Spinner from '../common/Spinner.svelte'
  import DataTable, { type DataColumn } from '../common/DataTable.svelte'
  import { Pencil, Plus, RefreshCw, TimerOff } from 'lucide-svelte'

  // Silences hold alert events back for a window. Everyone reads them;
  // only admins create, edit or end one early (server enforced).
  type Row = Record<string, unknown>
  const isAdmin = $derived(getSession()?.role === 'admin')

  let loading = $state(true)
  let refreshing = $state(false)
  let silences = $state<Silence[]>([])
  let connections = $state<Connection[]>([])
  let sqlAlerts = $state<SqlAlert[]>([])

  const emptyForm = () => ({
    reason: '',
    connection_id: '',
    event_type: '',
    source_ref: '',
    node: '',
    starts: toLocalInput(new Date()),
    duration: '3600',
    ends: '',
  })
  let sheetOpen = $state(false)
  let editing = $state<Silence | null>(null)
  let form = $state(emptyForm())
  let formError = $state('')
  let saving = $state(false)

  let expiring = $state<Silence | null>(null)
  let expireBusy = $state(false)

  const connectionName = (id: string) => connections.find((c) => c.id === id)?.name
  const sqlAlertName = (id: string) => sqlAlerts.find((a) => a.id === id)?.name

  const columns: DataColumn<Row>[] = [
    { key: 'reason', label: 'Reason', truncate: true },
    { key: 'matches', label: 'Matches', truncate: true, sortable: false },
    {
      key: 'window', label: 'Window', width: '260px',
      sortValue: (r) => new Date(String(r.starts_at)).getTime(),
    },
    { key: 'status', label: 'Status', width: '110px' },
    { key: 'created_by', label: 'Created by', width: '160px', format: (v) => (v ? String(v) : '-') },
  ]

  const rows = $derived(silences.map((s) => ({
    ...s,
    matches: silenceMatchSummary(s, { connectionName, sqlAlertName }),
    window: formatWindow(s.starts_at, s.ends_at),
  })) as unknown as Row[])

  const connectionOptions = $derived.by(() => {
    const opts = [{ value: '', label: 'Any connection' }, ...connections.map((c) => ({ value: c.id, label: c.name }))]
    if (form.connection_id && !connections.some((c) => c.id === form.connection_id)) {
      opts.push({ value: form.connection_id, label: 'Deleted connection' })
    }
    return opts
  })
  const eventOptions = [{ value: '', label: 'Any alert type' }, ...EVENT_TYPE_OPTIONS]
  const sourceOptions = $derived.by(() => {
    const opts = [
      { value: '', label: 'Any alert' },
      ...HEALTH_CHECK_ORDER.map((k) => ({ value: healthSourceRef(k), label: `Health: ${HEALTH_CHECK_LABELS[k]}` })),
      ...sqlAlerts
        .filter((a) => !form.connection_id || a.connection_id === form.connection_id)
        .map((a) => ({ value: a.id, label: `SQL: ${a.name}` })),
    ]
    if (form.source_ref && !opts.some((o) => o.value === form.source_ref)) {
      opts.push({ value: form.source_ref, label: sqlAlertName(form.source_ref) ?? `SQL alert ${form.source_ref}` })
    }
    return opts
  })

  async function load(initial = false) {
    if (initial) loading = true
    else refreshing = true
    try {
      // Names only decorate the table; a failure there should not hide the silences.
      const [s, c, a] = await Promise.all([
        listSilences(),
        listConnections().catch(() => [] as Connection[]),
        fetchSqlAlerts().then((r) => r.alerts).catch(() => [] as SqlAlert[]),
      ])
      silences = s
      connections = c
      sqlAlerts = a
    } catch (e: unknown) {
      toastError(e instanceof Error ? e.message : String(e))
    } finally {
      loading = false
      refreshing = false
    }
  }

  onMount(() => {
    void load(true)
  })

  function openNew() {
    editing = null
    form = emptyForm()
    formError = ''
    sheetOpen = true
  }

  function openEdit(s: Silence) {
    editing = s
    const preset = durationPreset(s.starts_at, s.ends_at)
    form = {
      reason: s.reason,
      connection_id: s.connection_id ?? '',
      event_type: s.event_type ?? '',
      source_ref: s.source_ref ?? '',
      node: s.node ?? '',
      starts: toLocalInput(new Date(s.starts_at)),
      duration: preset,
      ends: toLocalInput(new Date(s.ends_at)),
    }
    formError = ''
    sheetOpen = true
  }

  function closeSheet() {
    if (saving) return
    sheetOpen = false
  }

  function onDurationChange(v: string) {
    // Seed the custom end from the current preset so the field is never blank.
    if (v === 'custom' && !form.ends) {
      const start = new Date(form.starts)
      if (!Number.isNaN(start.getTime())) form.ends = toLocalInput(new Date(start.getTime() + 3600_000))
    }
  }

  function buildInput(): SilenceInput | string {
    const reason = form.reason.trim()
    if (!reason) return 'Enter a reason.'
    const input: SilenceInput = {
      reason,
      connection_id: form.connection_id || null,
      event_type: form.event_type || null,
      source_ref: form.source_ref || null,
      node: form.node.trim() || null,
    }
    if (!hasMatcher(input)) return 'Pick at least one of connection, alert type, specific alert or node.'
    const startsAt = localInputToIso(form.starts)
    if (!startsAt) return 'Enter a valid start time.'
    input.starts_at = startsAt
    if (form.duration === 'custom') {
      const endsAt = localInputToIso(form.ends)
      if (!endsAt) return 'Enter a valid end time.'
      if (new Date(endsAt).getTime() <= new Date(startsAt).getTime()) return 'The end must be after the start.'
      input.ends_at = endsAt
    } else {
      input.duration_seconds = Number(form.duration)
    }
    return input
  }

  async function save() {
    if (saving) return
    const input = buildInput()
    if (typeof input === 'string') {
      formError = input
      return
    }
    formError = ''
    saving = true
    try {
      if (editing) {
        await updateSilence(editing.id, input)
        toastSuccess('Silence updated')
      } else {
        await createSilence(input)
        toastSuccess('Silence created')
      }
      sheetOpen = false
      await load()
    } catch (e: unknown) {
      toastError(e instanceof Error ? e.message : String(e))
    } finally {
      saving = false
    }
  }

  async function confirmExpire() {
    if (!expiring) return
    const s = expiring
    expireBusy = true
    try {
      await expireSilence(s.id)
      toastSuccess('Silence ended')
      expiring = null
      await load()
    } catch (e: unknown) {
      toastError(e instanceof Error ? e.message : String(e))
    } finally {
      expireBusy = false
    }
  }
</script>

<div class="flex h-full min-h-0 flex-col">
  <div class="flex h-10 shrink-0 items-center gap-2 px-5">
    <span class="text-xs text-fg-4">{silences.length} {silences.length === 1 ? 'silence' : 'silences'}</span>
    <div class="flex-1"></div>
    <Button icon variant="ghost" size="sm" aria-label="Refresh silences" title="Refresh" onclick={() => void load()}>
      <RefreshCw size={14} class={refreshing ? 'animate-spin' : ''} />
    </Button>
    {#if isAdmin}
      <Button size="sm" onclick={openNew}><Plus size={13} /> New silence</Button>
    {/if}
  </div>

  <div class="min-h-0 flex-1 px-5 pb-4">
    {#if loading}
      <div class="flex h-full items-center justify-center"><Spinner /></div>
    {:else}
      <div class="h-full overflow-hidden rounded-lg border border-edge-subtle bg-surface">
        <DataTable
          {columns}
          {rows}
          fill
          rowKey={(r) => String(r.id)}
          emptyTitle="No silences"
          emptyDescription="A silence holds back alerts that match it for a time window, for example during planned maintenance."
        >
          {#snippet cell(row, col, value)}
            {#if col.key === 'reason'}
              <span class="font-medium text-fg">{value}</span>
            {:else if col.key === 'status'}
              <Badge tone={silenceStatusTone(String(row.status))}>{silenceStatusLabel(String(row.status))}</Badge>
            {:else}
              {value}
            {/if}
          {/snippet}
          {#snippet actions(row)}
            {@const s = row as unknown as Silence}
            {#if isAdmin && s.status !== 'expired'}
              <Button icon variant="ghost" size="xs" aria-label="Edit silence" title="Edit" onclick={() => openEdit(s)}><Pencil size={13} /></Button>
              <Button icon variant="ghost" size="xs" aria-label="Expire early" title="Expire early" onclick={() => (expiring = s)}><TimerOff size={13} /></Button>
            {/if}
          {/snippet}
        </DataTable>
      </div>
    {/if}
  </div>
</div>

<Sheet
  open={sheetOpen}
  title={editing ? 'Edit silence' : 'New silence'}
  description="Alerts that match every field you set are held back during the window. Leave a field empty to match anything."
  size="md"
  onclose={closeSheet}
>
  <form id="silence-form" class="space-y-4" onsubmit={(e) => { e.preventDefault(); void save() }}>
    <FormField label="Reason" for="sil-reason" required>
      <Input id="sil-reason" placeholder="Planned upgrade of ch-prod-2" bind:value={form.reason} required />
    </FormField>
    <div class="grid grid-cols-1 gap-3 md:grid-cols-2">
      <FormField label="Connection" for="sil-conn">
        <Select id="sil-conn" options={connectionOptions} bind:value={form.connection_id} />
      </FormField>
      <FormField label="Alert type" for="sil-type">
        <Select id="sil-type" options={eventOptions} bind:value={form.event_type} />
      </FormField>
      <FormField label="Specific alert" for="sil-source">
        <Select id="sil-source" options={sourceOptions} bind:value={form.source_ref} />
      </FormField>
      <FormField label="Node" for="sil-node" hint="Exact node name. Empty matches every node.">
        <Input id="sil-node" mono bind:value={form.node} />
      </FormField>
    </div>
    <div class="grid grid-cols-1 gap-3 md:grid-cols-2">
      <FormField label="Starts" for="sil-starts" hint="Your local time.">
        <input id="sil-starts" type="datetime-local" class="ds-input" bind:value={form.starts} />
      </FormField>
      <FormField label="Duration" for="sil-duration">
        <Select id="sil-duration" options={SILENCE_DURATION_OPTIONS} bind:value={form.duration} onchange={onDurationChange} />
      </FormField>
      {#if form.duration === 'custom'}
        <FormField label="Ends" for="sil-ends" hint="Your local time.">
          <input id="sil-ends" type="datetime-local" class="ds-input" bind:value={form.ends} />
        </FormField>
      {/if}
    </div>
    {#if formError}<p class="text-xs text-danger" role="alert">{formError}</p>{/if}
  </form>
  {#snippet footer()}
    <Button variant="ghost" size="sm" disabled={saving} onclick={closeSheet}>Cancel</Button>
    <Button size="sm" type="submit" form="silence-form" loading={saving} disabled={!form.reason.trim()}>
      {editing ? 'Save changes' : 'Create'}
    </Button>
  {/snippet}
</Sheet>

<ConfirmDialog
  open={expiring !== null}
  title="Expire silence early?"
  description={expiring ? `"${expiring.reason}" ends now. Matching alerts are delivered again from this point.` : ''}
  confirmLabel="Expire now"
  destructive
  loading={expireBusy}
  onconfirm={() => void confirmExpire()}
  oncancel={() => (expiring = null)}
/>
