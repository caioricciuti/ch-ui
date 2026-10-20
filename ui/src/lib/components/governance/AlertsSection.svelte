<!-- SPDX-License-Identifier: BUSL-1.1 -->
<script lang="ts">
  import { onMount } from 'svelte'
  import type { AlertEvent, FiringIncident } from '../../types/alerts'
  import { adminListAlertEvents, createAck, deleteAck, listFiring } from '../../api/alerts'
  import { ACK_EXPIRY_OPTIONS, ackTitle, eventTypeLabel as eventLabel } from './alerting-silences'
  import { severityTone, statusTone } from './alerting-admin'
  import { getSession } from '../../stores/session.svelte'
  import { setSection } from '../../stores/nav.svelte'
  import { navigate } from '../../stores/router.svelte'
  import { success as toastSuccess, errorFrom as toastErrorFrom } from '../../stores/toast.svelte'
  import { formatDate } from '../../utils/format'
  import Button from '../common/Button.svelte'
  import Badge from '../common/Badge.svelte'
  import FormField from '../common/FormField.svelte'
  import Panel from '../common/Panel.svelte'
  import Select from '../common/Select.svelte'
  import Sheet from '../common/Sheet.svelte'
  import Spinner from '../common/Spinner.svelte'
  import Textarea from '../common/Textarea.svelte'
  import DataTable, { type DataColumn } from '../common/DataTable.svelte'
  import { RefreshCw } from 'lucide-svelte'

  // Alerts: the events CH-UI fired, newest first. Rules and channels have
  // their own sections (alert-rules, alert-channels). Open pipeline
  // incidents (ingest stalls, spool backlog, rejections, failing sinks) are
  // listed above the events for everyone; writers acknowledge them. Admins
  // also see the open access drift incidents, and acknowledge them there
  // (only admins can, server enforced).
  type Row = Record<string, unknown>
  const asRows = <T,>(rows: T[]): Row[] => rows as unknown as Row[]
  const isAdmin = $derived(getSession()?.role === 'admin')
  const canAck = $derived(getSession()?.role === 'admin' || getSession()?.role === 'analyst')

  let loading = $state(true)
  let events = $state<AlertEvent[]>([])
  let eventLimit = $state('50')
  let selectedEvent = $state<AlertEvent | null>(null)

  let drift = $state<FiringIncident[]>([])
  let ingest = $state<FiringIncident[]>([])
  let ackTarget = $state<FiringIncident | null>(null)
  let ackComment = $state('')
  let ackExpiry = $state('')
  let ackSaving = $state(false)
  let unacking = $state<string | null>(null)

  const driftColumns: DataColumn<Row>[] = [
    { key: 'name', label: 'Incident', width: '220px', truncate: true },
    { key: 'severity', label: 'Severity', width: '110px' },
    { key: 'since', label: 'First seen', mono: true, width: '170px', format: (v) => formatDate(v) },
    { key: 'summary', label: 'Latest change', truncate: true },
  ]

  const ingestColumns: DataColumn<Row>[] = [
    { key: 'name', label: 'Incident', width: '260px', truncate: true },
    { key: 'severity', label: 'Severity', width: '110px' },
    { key: 'since', label: 'Since', mono: true, width: '170px', format: (v) => formatDate(v) },
    { key: 'summary', label: 'Details', truncate: true },
  ]

  const limitOptions = ['25', '50', '100', '200'].map((v) => ({ value: v, label: `${v} events` }))

  const eventColumns: DataColumn<Row>[] = [
    { key: 'created_at', label: 'Time', mono: true, width: '170px', format: (v) => formatDate(v) },
    { key: 'event_type', label: 'Event', width: '160px', format: (v) => eventLabel(String(v)) },
    { key: 'severity', label: 'Severity', width: '110px' },
    { key: 'status', label: 'Status', width: '110px' },
    { key: 'title', label: 'Message', truncate: true },
  ]

  async function loadEvents() {
    try {
      events = await adminListAlertEvents({ limit: Number(eventLimit) })
    } catch (e: unknown) {
      toastErrorFrom(e)
    } finally {
      loading = false
    }
  }

  // Incident rows only add to the events; a failure here keeps the list visible.
  async function loadDrift() {
    try {
      const open = await listFiring()
      drift = isAdmin ? open.filter((inc) => inc.source === 'drift') : []
      ingest = open.filter((inc) => inc.source === 'ingest')
    } catch (e: unknown) {
      toastErrorFrom(e)
    }
  }

  function refresh() {
    void loadEvents()
    void loadDrift()
  }

  function openAck(inc: FiringIncident) {
    ackTarget = inc
    ackComment = ''
    ackExpiry = ''
  }

  async function submitAck() {
    if (!ackTarget?.event_id || ackSaving) return
    ackSaving = true
    try {
      await createAck({
        event_id: ackTarget.event_id,
        comment: ackComment.trim() || undefined,
        expires_in: ackExpiry ? Number(ackExpiry) : undefined,
      })
      toastSuccess(ackTarget.source === 'drift' ? 'Access drift acknowledged' : 'Incident acknowledged')
      ackTarget = null
      await loadDrift()
    } catch (e: unknown) {
      toastErrorFrom(e)
    } finally {
      ackSaving = false
    }
  }

  async function unacknowledge(ackId: string) {
    unacking = ackId
    try {
      await deleteAck(ackId)
      toastSuccess('Acknowledgement removed')
      await loadDrift()
    } catch (e: unknown) {
      toastErrorFrom(e)
    } finally {
      unacking = null
    }
  }

  onMount(refresh)

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
    <div class="flex-1"></div>
    <Select size="sm" class="w-32" options={limitOptions} bind:value={eventLimit} onchange={() => void loadEvents()} />
    <Button icon variant="ghost" size="sm" aria-label="Refresh events" title="Refresh" onclick={refresh}>
      <RefreshCw size={14} />
    </Button>
  </div>

  {#if isAdmin && drift.length > 0}
    <div class="shrink-0 px-5 pb-3">
      <Panel title="Open access drift" description="Access changed outside CH-UI. Acknowledging stops further notifications until the incident resolves, escalates or the acknowledgement expires." padding="none">
        <DataTable columns={driftColumns} rows={asRows(drift)} rowKey={(r) => String(r.alert_key)} framed={false}>
          {#snippet cell(row, col, value)}
            {#if col.key === 'name'}
              {@const ack = (row as unknown as FiringIncident).ack}
              <span class="inline-flex items-center gap-1">
                {value}
                {#if ack}<Badge tone="info" title={ackTitle(ack)}>Acknowledged</Badge>{/if}
              </span>
            {:else if col.key === 'severity'}
              <Badge tone={severityTone(String(value))}>{value}</Badge>
            {:else if col.key === 'summary'}
              <span title={row.last_seen ? `Last seen ${formatDate(String(row.last_seen))}` : undefined}>{value}</span>
            {:else}
              {value}
            {/if}
          {/snippet}
          {#snippet actions(row)}
            {@const inc = row as unknown as FiringIncident}
            <Button size="xs" variant="ghost" onclick={() => setSection('access-history')}>Access history</Button>
            {#if inc.ack}
              {@const ackId = inc.ack.id}
              <Button size="xs" variant="ghost" loading={unacking === ackId} onclick={() => void unacknowledge(ackId)}>Unacknowledge</Button>
            {:else if inc.event_id}
              <Button size="xs" variant="ghost" onclick={() => openAck(inc)}>Acknowledge</Button>
            {/if}
          {/snippet}
        </DataTable>
      </Panel>
    </div>
  {/if}

  {#if ingest.length > 0}
    <div class="shrink-0 px-5 pb-3">
      <Panel title="Open pipeline incidents" description="Ingest stalls, spool backlog, rejected requests and failing sinks. Each resolves on its own once the pipeline recovers or stops." padding="none">
        <DataTable columns={ingestColumns} rows={asRows(ingest)} rowKey={(r) => String(r.alert_key)} framed={false}>
          {#snippet cell(row, col, value)}
            {#if col.key === 'name'}
              {@const ack = (row as unknown as FiringIncident).ack}
              <span class="inline-flex items-center gap-1">
                {value}
                {#if ack}<Badge tone="info" title={ackTitle(ack)}>Acknowledged</Badge>{/if}
              </span>
            {:else if col.key === 'severity'}
              <Badge tone={severityTone(String(value))}>{value}</Badge>
            {:else if col.key === 'summary'}
              <span title={String(value ?? '')}>{value}</span>
            {:else}
              {value}
            {/if}
          {/snippet}
          {#snippet actions(row)}
            {@const inc = row as unknown as FiringIncident}
            {#if inc.link_path}
              {@const link = inc.link_path}
              <Button size="xs" variant="ghost" onclick={() => navigate(link)}>Open pipeline</Button>
            {/if}
            {#if canAck}
              {#if inc.ack}
                {@const ackId = inc.ack.id}
                <Button size="xs" variant="ghost" loading={unacking === ackId} onclick={() => void unacknowledge(ackId)}>Unacknowledge</Button>
              {:else if inc.event_id}
                <Button size="xs" variant="ghost" onclick={() => openAck(inc)}>Acknowledge</Button>
              {/if}
            {/if}
          {/snippet}
        </DataTable>
      </Panel>
    </div>
  {/if}

  <div class="min-h-0 flex-1 px-5 pb-4">
    {#if loading}
      <div class="flex h-full items-center justify-center"><Spinner /></div>
    {:else}
      <div class="h-full overflow-hidden rounded-lg border border-edge-subtle bg-surface">
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
              {#if value === 'silenced'}
                <Badge tone="neutral" title={row.suppressed_by ? `Held back by ${row.suppressed_by}` : undefined}>Silenced</Badge>
              {:else if value === 'acknowledged'}
                <Badge tone="info" title={row.suppressed_by ? `Held back by ${row.suppressed_by}` : undefined}>Acknowledged</Badge>
              {:else}
                <Badge tone={statusTone(String(value))}>{value}</Badge>
              {/if}
            {:else}
              {value}
            {/if}
          {/snippet}
        </DataTable>
      </div>
    {/if}
  </div>
</div>

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
      {#if selectedEvent.suppressed_by}
        <div class="text-xs text-fg-3">Held back by <span class="font-mono text-fg-2">{selectedEvent.suppressed_by}</span></div>
      {/if}
      {#if selectedEvent.source_ref}
        <div class="text-xs text-fg-3">Source: <span class="font-mono text-fg-2">{selectedEvent.source_ref}</span></div>
      {/if}
      {#if selectedEvent.payload_json}
        <pre class="overflow-x-auto rounded-md border border-edge-subtle bg-surface-2 p-3 font-mono text-xs leading-relaxed text-fg">{prettyPayload(selectedEvent.payload_json)}</pre>
      {/if}
    </div>
  {/if}
</Sheet>

<!-- Acknowledge an access drift or pipeline incident -->
<Sheet open={ackTarget !== null} title={ackTarget?.source === 'drift' ? 'Acknowledge access drift' : 'Acknowledge incident'} description={ackTarget?.source === 'drift' ? 'Admins only.' : ''} size="md" onclose={() => { if (!ackSaving) ackTarget = null }}>
  {#if ackTarget}
    <form id="drift-ack-form" class="space-y-4" onsubmit={(e) => { e.preventDefault(); void submitAck() }}>
      <p class="text-[13px] text-fg-2">
        {#if ackTarget.source === 'drift'}
          Acknowledge {ackTarget.name}. Notifications for further drift on this connection stop until the access state is back
          where it was, the drift escalates, or the acknowledgement expires.
        {:else}
          Acknowledge {ackTarget.name}. Further notifications for this incident stop until it resolves, escalates, or the
          acknowledgement expires.
        {/if}
      </p>
      <FormField label="Comment" for="drift-ack-comment" hint="Optional. Shown to everyone who sees this incident.">
        <Textarea id="drift-ack-comment" rows={3} placeholder="Expected change, ticket OPS-123" bind:value={ackComment} />
      </FormField>
      <FormField label="Expires" for="drift-ack-expiry">
        <Select id="drift-ack-expiry" options={ACK_EXPIRY_OPTIONS} bind:value={ackExpiry} />
      </FormField>
    </form>
  {/if}
  {#snippet footer()}
    <Button variant="ghost" size="sm" disabled={ackSaving} onclick={() => (ackTarget = null)}>Cancel</Button>
    <Button size="sm" type="submit" form="drift-ack-form" loading={ackSaving}>Acknowledge</Button>
  {/snippet}
</Sheet>
