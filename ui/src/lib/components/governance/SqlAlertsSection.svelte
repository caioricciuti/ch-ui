<!-- SPDX-License-Identifier: BUSL-1.1 -->
<script lang="ts">
  import { onMount } from 'svelte'
  import { getSession } from '../../stores/session.svelte'
  import { goTo } from '../../stores/router.svelte'
  import { setSection } from '../../stores/nav.svelte'
  import { success as toastSuccess, errorFrom as toastErrorFrom } from '../../stores/toast.svelte'
  import { fetchSqlAlerts } from '../../api/alerting'
  import { createAck, deleteAck, listFiring } from '../../api/alerts'
  import type { FiringIncident, SqlAlert, SqlAlertWorkerMode } from '../../types/alerts'
  import { formatRelativeTime } from '../../utils/format'
  import { conditionLabel, formatNumber, formatSeconds, stateTone } from './alerting-format'
  import { ACK_EXPIRY_OPTIONS, ackTitle, indexFiring } from './alerting-silences'
  import PageBody from '../common/PageBody.svelte'
  import SectionHeader from '../common/SectionHeader.svelte'
  import Panel from '../common/Panel.svelte'
  import DataTable, { type DataColumn } from '../common/DataTable.svelte'
  import Badge from '../common/Badge.svelte'
  import Button from '../common/Button.svelte'
  import Spinner from '../common/Spinner.svelte'
  import Sheet from '../common/Sheet.svelte'
  import FormField from '../common/FormField.svelte'
  import Select from '../common/Select.svelte'
  import Textarea from '../common/Textarea.svelte'
  import EmptyState from '../common/EmptyState.svelte'
  import SqlAlertEditor from './SqlAlertEditor.svelte'
  import { Plus, RefreshCw } from 'lucide-svelte'

  // SQL threshold alerts: saved queries run on an interval by the
  // dedicated background account. Admins and analysts write; viewers read.
  type Row = Record<string, unknown>
  const session = $derived(getSession())
  const isAdmin = $derived(session?.role === 'admin')
  const canWrite = $derived(session?.role === 'admin' || session?.role === 'analyst')

  let loading = $state(true)
  let refreshing = $state(false)
  let alerts = $state<SqlAlert[]>([])
  let workerMode = $state<SqlAlertWorkerMode>('service_account')
  let firing = $state<FiringIncident[]>([])
  const firingIdx = $derived(indexFiring(firing))

  let ackTarget = $state<{ incident: FiringIncident; name: string } | null>(null)
  let ackComment = $state('')
  let ackExpiry = $state('')
  let ackSaving = $state(false)
  let unacking = $state<string | null>(null)

  let editorOpen = $state(false)
  let editingId = $state<string | null>(null)
  const editing = $derived(editingId ? alerts.find((a) => a.id === editingId) ?? null : null)
  // Only the creator or an admin may change an existing alert (server enforced).
  const canEditSelected = $derived(canWrite && (!editing || isAdmin || editing.created_by === session?.user))

  const columns: DataColumn<Row>[] = [
    { key: 'name', label: 'Name', truncate: true },
    { key: 'state', label: 'State', width: '200px' },
    { key: 'last_value', label: 'Last value', width: '120px', align: 'right', format: (v) => formatNumber(v as number | null) },
    { key: 'condition', label: 'Condition', mono: true, width: '160px', sortable: false },
    { key: 'interval_seconds', label: 'Interval', width: '90px', align: 'right', format: (v) => formatSeconds(Number(v)) },
    {
      key: 'last_run_at', label: 'Last run', width: '110px',
      format: (v) => (v ? formatRelativeTime(v) : 'Never'),
      sortValue: (r) => (r.last_run_at ? new Date(String(r.last_run_at)).getTime() : 0),
    },
    { key: 'enabled', label: 'Enabled', width: '100px' },
  ]

  const rows = $derived(alerts.map((a) => ({ ...a, condition: conditionLabel(a.mode, a.comparator, a.threshold) })) as unknown as Row[])

  async function load(initial = false) {
    if (initial) loading = true
    else refreshing = true
    try {
      const [res] = await Promise.all([fetchSqlAlerts(), loadFiring()])
      alerts = res.alerts
      workerMode = res.worker_mode
    } catch (e: unknown) {
      toastErrorFrom(e)
    } finally {
      loading = false
      refreshing = false
    }
  }

  // Ack state only decorates the rows; a failure here keeps the alerts visible.
  async function loadFiring() {
    try {
      firing = await listFiring()
    } catch (e: unknown) {
      toastErrorFrom(e)
    }
  }

  function openAck(incident: FiringIncident, name: string) {
    ackTarget = { incident, name }
    ackComment = ''
    ackExpiry = ''
  }

  function closeAck() {
    if (!ackSaving) ackTarget = null
  }

  async function submitAck() {
    const eventId = ackTarget?.incident.event_id
    if (!eventId || ackSaving) return
    ackSaving = true
    try {
      await createAck({
        event_id: eventId,
        comment: ackComment.trim() || undefined,
        expires_in: ackExpiry ? Number(ackExpiry) : undefined,
      })
      toastSuccess('Alert acknowledged')
      ackTarget = null
      await loadFiring()
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
      await loadFiring()
    } catch (e: unknown) {
      toastErrorFrom(e)
    } finally {
      unacking = null
    }
  }

  onMount(() => {
    void load(true)
  })

  function openNew() {
    editingId = null
    editorOpen = true
  }

  function openAlert(id: string) {
    editingId = id
    editorOpen = true
  }

  function upsert(alert: SqlAlert) {
    alerts = alerts.some((a) => a.id === alert.id) ? alerts.map((a) => (a.id === alert.id ? alert : a)) : [...alerts, alert]
  }

  function removed(id: string) {
    alerts = alerts.filter((a) => a.id !== id)
  }

  function openConnections() {
    goTo('admin')
    setSection('tunnels')
  }
</script>

<PageBody width="md">
  <div class="space-y-8">
    <section>
      <SectionHeader
        title="SQL alerts"
        description="Queries checked on an interval. When the result crosses its threshold, a sql.alert event goes to your alert rules."
      >
        {#snippet actions()}
          <Button icon variant="ghost" size="sm" aria-label="Refresh" title="Refresh" onclick={() => void load()}>
            <RefreshCw size={14} class={refreshing ? 'animate-spin' : ''} />
          </Button>
          {#if canWrite}
            <Button size="sm" onclick={openNew}><Plus size={13} /> New SQL alert</Button>
          {/if}
        {/snippet}
      </SectionHeader>

      {#if !loading && workerMode !== 'service_account'}
        <Panel variant="muted" padding="sm" class="mb-3">
          <p class="text-[13px] text-fg-2">
            SQL alerts run with a dedicated background account. Configure it in
            <button type="button" class="font-medium text-accent hover:underline" onclick={openConnections}>Admin, Connections</button>.
            Until then only Test run and Run now execute.
          </p>
        </Panel>
      {/if}

      {#if loading}
        <div class="flex justify-center py-10"><Spinner /></div>
      {:else if alerts.length === 0}
        <Panel>
          <EmptyState
            size="compact"
            title="No SQL alerts yet"
            description="Write a query that returns a number or a set of rows, and choose when it should fire."
            primary={canWrite ? { label: 'New SQL alert', onclick: openNew } : undefined}
          />
        </Panel>
      {:else}
        <DataTable
          {columns}
          {rows}
          rowKey={(r) => String(r.id)}
          sort={{ key: 'name', dir: 'asc' }}
          onrowclick={(r) => openAlert(String(r.id))}
        >
          {#snippet cell(row, col, value)}
            {#if col.key === 'name'}
              <span class="font-medium text-fg">{value}</span>
            {:else if col.key === 'state'}
              {@const inc = firingIdx.sql.get(String(row.id))}
              <span class="inline-flex items-center gap-1">
                <Badge tone={stateTone(String(row.state))} title={row.last_error ? String(row.last_error) : undefined}>{value}</Badge>
                {#if inc?.ack}<Badge tone="info" title={ackTitle(inc.ack)}>Acknowledged</Badge>{/if}
              </span>
            {:else if col.key === 'enabled'}
              <Badge dot tone={row.enabled ? 'success' : 'neutral'}>{row.enabled ? 'Enabled' : 'Off'}</Badge>
            {:else}
              {value}
            {/if}
          {/snippet}
          {#snippet actions(row)}
            {@const inc = firingIdx.sql.get(String(row.id))}
            {#if canWrite && inc?.ack}
              {@const ackId = inc.ack.id}
              <Button size="xs" variant="ghost" loading={unacking === ackId} onclick={(e: MouseEvent) => { e.stopPropagation(); void unacknowledge(ackId) }}>Unacknowledge</Button>
            {:else if canWrite && inc?.event_id}
              <Button size="xs" variant="ghost" onclick={(e: MouseEvent) => { e.stopPropagation(); openAck(inc, String(row.name)) }}>Acknowledge</Button>
            {/if}
          {/snippet}
        </DataTable>
      {/if}
    </section>
  </div>
</PageBody>

<SqlAlertEditor
  open={editorOpen}
  alert={editing}
  canEdit={canEditSelected}
  canRun={canWrite}
  onclose={() => (editorOpen = false)}
  onchanged={upsert}
  ondeleted={removed}
/>

<Sheet
  open={ackTarget !== null}
  title="Acknowledge alert"
  description={ackTarget ? `"${ackTarget.name}" is firing. Notifications for it stop until it resolves or the acknowledgement expires.` : ''}
  size="sm"
  onclose={closeAck}
>
  <form id="sql-ack-form" class="space-y-4" onsubmit={(e) => { e.preventDefault(); void submitAck() }}>
    <FormField label="Comment" for="sql-ack-comment" hint="Optional. Shown to everyone who sees this alert.">
      <Textarea id="sql-ack-comment" rows={3} placeholder="Looking into it" bind:value={ackComment} />
    </FormField>
    <FormField label="Expires" for="sql-ack-expiry">
      <Select id="sql-ack-expiry" options={ACK_EXPIRY_OPTIONS} bind:value={ackExpiry} />
    </FormField>
  </form>
  {#snippet footer()}
    <Button variant="ghost" size="sm" disabled={ackSaving} onclick={closeAck}>Cancel</Button>
    <Button size="sm" type="submit" form="sql-ack-form" loading={ackSaving}>Acknowledge</Button>
  {/snippet}
</Sheet>
