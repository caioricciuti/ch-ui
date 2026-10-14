<!-- SPDX-License-Identifier: BUSL-1.1 -->
<script lang="ts">
  import { onMount } from 'svelte'
  import type { AlertEvent } from '../../types/alerts'
  import { adminListAlertEvents } from '../../api/alerts'
  import { eventTypeLabel as eventLabel } from './alerting-silences'
  import { severityTone, statusTone } from './alerting-admin'
  import { error as toastError } from '../../stores/toast.svelte'
  import { formatDate } from '../../utils/format'
  import Button from '../common/Button.svelte'
  import Badge from '../common/Badge.svelte'
  import Select from '../common/Select.svelte'
  import Sheet from '../common/Sheet.svelte'
  import Spinner from '../common/Spinner.svelte'
  import DataTable, { type DataColumn } from '../common/DataTable.svelte'
  import { RefreshCw } from 'lucide-svelte'

  // Alerts: the events CH-UI fired, newest first. Rules and channels have
  // their own sections (alert-rules, alert-channels).
  type Row = Record<string, unknown>
  const asRows = <T,>(rows: T[]): Row[] => rows as unknown as Row[]

  let loading = $state(true)
  let events = $state<AlertEvent[]>([])
  let eventLimit = $state('50')
  let selectedEvent = $state<AlertEvent | null>(null)

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
      toastError(e instanceof Error ? e.message : String(e))
    } finally {
      loading = false
    }
  }

  onMount(loadEvents)

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
    <Button icon variant="ghost" size="sm" aria-label="Refresh events" title="Refresh" onclick={() => void loadEvents()}>
      <RefreshCw size={14} />
    </Button>
  </div>

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
