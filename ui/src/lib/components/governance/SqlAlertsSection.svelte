<!-- SPDX-License-Identifier: BUSL-1.1 -->
<script lang="ts">
  import { onMount } from 'svelte'
  import { getSession } from '../../stores/session.svelte'
  import { goTo } from '../../stores/router.svelte'
  import { setSection } from '../../stores/nav.svelte'
  import { error as toastError } from '../../stores/toast.svelte'
  import { fetchSqlAlerts } from '../../api/alerting'
  import type { SqlAlert, SqlAlertWorkerMode } from '../../types/alerts'
  import { formatRelativeTime } from '../../utils/format'
  import { conditionLabel, formatNumber, formatSeconds, stateTone } from './alerting-format'
  import PageBody from '../common/PageBody.svelte'
  import SectionHeader from '../common/SectionHeader.svelte'
  import Panel from '../common/Panel.svelte'
  import DataTable, { type DataColumn } from '../common/DataTable.svelte'
  import Badge from '../common/Badge.svelte'
  import Button from '../common/Button.svelte'
  import Spinner from '../common/Spinner.svelte'
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

  let editorOpen = $state(false)
  let editingId = $state<string | null>(null)
  const editing = $derived(editingId ? alerts.find((a) => a.id === editingId) ?? null : null)
  // Only the creator or an admin may change an existing alert (server enforced).
  const canEditSelected = $derived(canWrite && (!editing || isAdmin || editing.created_by === session?.user))

  const columns: DataColumn<Row>[] = [
    { key: 'name', label: 'Name', truncate: true },
    { key: 'state', label: 'State', width: '100px' },
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
      const res = await fetchSqlAlerts()
      alerts = res.alerts
      workerMode = res.worker_mode
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
              <Badge tone={stateTone(String(row.state))} title={row.last_error ? String(row.last_error) : undefined}>{value}</Badge>
            {:else if col.key === 'enabled'}
              <Badge dot tone={row.enabled ? 'success' : 'neutral'}>{row.enabled ? 'Enabled' : 'Off'}</Badge>
            {:else}
              {value}
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
