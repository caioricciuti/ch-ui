<script lang="ts">
  import * as api from '../../api/pipelines'
  import { formatBytes, formatDate, formatNumber, formatTime } from '../../utils/format'
  import Sheet from '../common/Sheet.svelte'
  import Button from '../common/Button.svelte'
  import Badge from '../common/Badge.svelte'
  import Spinner from '../common/Spinner.svelte'
  import EmptyState from '../common/EmptyState.svelte'
  import DataTable, { type DataColumn } from '../common/DataTable.svelte'
  import { ArrowLeft, RefreshCw, AlertTriangle } from 'lucide-svelte'

  interface Props {
    open: boolean
    pipelineId: string
    onclose: () => void
  }

  let { open, pipelineId, onclose }: Props = $props()

  type RunRow = {
    id: string
    status: string
    started_at: string
    finished_at: string | null
    rows_ingested: number
    bytes_ingested: number
    errors_count: number
    last_error: string | null
  }
  type LogRow = { id: string; level: string; message: string; created_at: string }

  let runs = $state<RunRow[]>([])
  let logs = $state<LogRow[]>([])
  let selected = $state<RunRow | null>(null)
  let loading = $state(false)
  let error = $state('')
  // Drops responses from a request that a newer one replaced.
  let requestSeq = 0

  const runColumns: DataColumn<RunRow>[] = [
    { key: 'status', label: 'Status', width: '110px' },
    { key: 'started_at', label: 'Started', format: (v) => formatDate(v) },
    { key: 'finished_at', label: 'Finished', format: (v) => (v ? formatDate(v) : 'running') },
    { key: 'rows_ingested', label: 'Rows', align: 'right', format: (v) => formatNumber(Number(v)) },
    { key: 'bytes_ingested', label: 'Bytes', align: 'right', format: (v) => formatBytes(Number(v)) },
    { key: 'errors_count', label: 'Errors', align: 'right', format: (v) => formatNumber(Number(v)) },
  ]
  const logColumns: DataColumn<LogRow>[] = [
    { key: 'created_at', label: 'Time', width: '110px', format: (v) => formatTime(v) },
    { key: 'level', label: 'Level', width: '80px' },
    { key: 'message', label: 'Message', mono: true },
  ]

  $effect(() => {
    if (open) {
      selected = null
      void loadRuns()
    }
  })

  function errorText(e: unknown, fallback: string): string {
    return (e instanceof Error ? e.message : String(e)) || fallback
  }

  async function loadRuns() {
    const seq = ++requestSeq
    loading = true
    error = ''
    try {
      const res = await api.listRuns(pipelineId)
      if (seq !== requestSeq) return
      runs = (res.runs ?? []).map((r) => ({
        id: r.id,
        status: r.status,
        started_at: r.started_at,
        finished_at: r.finished_at,
        rows_ingested: r.rows_ingested,
        bytes_ingested: r.bytes_ingested,
        errors_count: r.errors_count,
        last_error: r.last_error,
      }))
    } catch (e: unknown) {
      if (seq === requestSeq) error = errorText(e, 'Failed to load runs')
    } finally {
      if (seq === requestSeq) loading = false
    }
  }

  async function openRun(run: RunRow) {
    const seq = ++requestSeq
    selected = run
    logs = []
    loading = true
    error = ''
    try {
      const res = await api.getRunLogs(pipelineId, run.id)
      if (seq !== requestSeq) return
      logs = (res.logs ?? []).map((l) => ({ id: l.id, level: l.level, message: l.message, created_at: l.created_at }))
    } catch (e: unknown) {
      if (seq === requestSeq) error = errorText(e, 'Failed to load run logs')
    } finally {
      if (seq === requestSeq) loading = false
    }
  }

  function statusTone(status: string): 'neutral' | 'success' | 'warning' | 'danger' {
    if (status === 'running') return 'success'
    if (status === 'error') return 'danger'
    return 'neutral'
  }
</script>

<Sheet
  {open}
  {onclose}
  size="lg"
  title={selected ? 'Run logs' : 'Runs'}
  description={selected ? `Run started ${formatDate(selected.started_at)}` : 'Each start of the pipeline is a run. Select one to see its logs.'}
>
  <div class="space-y-3">
    <div class="flex items-center gap-2">
      {#if selected}
        <Button size="sm" variant="ghost" onclick={() => { requestSeq++; selected = null; loading = false }}>
          <ArrowLeft size={13} /> All runs
        </Button>
      {/if}
      <div class="flex-1"></div>
      <Button size="sm" variant="outline" onclick={() => (selected ? openRun(selected) : loadRuns())} disabled={loading}>
        <RefreshCw size={13} /> Refresh
      </Button>
    </div>

    {#if selected?.last_error}
      <div class="flex items-start gap-2 rounded-md bg-danger-soft px-3 py-2 text-[13px] text-danger">
        <AlertTriangle size={14} class="mt-0.5 shrink-0" />
        <span class="min-w-0 break-words">{selected.last_error}</span>
      </div>
    {/if}

    {#if error}
      <EmptyState size="compact" icon={AlertTriangle} title="Could not load" description={error} />
    {:else if loading}
      <div class="flex justify-center py-10"><Spinner /></div>
    {:else if selected}
      <DataTable
        columns={logColumns}
        rows={logs}
        rowKey={(r) => r.id}
        emptyTitle="No logs for this run"
      >
        {#snippet cell(row, col, value)}
          {#if col.key === 'level'}
            <Badge tone={row.level === 'error' ? 'danger' : row.level === 'warn' ? 'warning' : 'neutral'}>{value}</Badge>
          {:else if col.key === 'message'}
            <span class="whitespace-pre-wrap break-words font-mono text-[12px] text-fg-2">{value}</span>
          {/if}
        {/snippet}
      </DataTable>
    {:else}
      <DataTable
        columns={runColumns}
        rows={runs}
        rowKey={(r) => r.id}
        sort={{ key: 'started_at', dir: 'desc' }}
        emptyTitle="No runs yet"
        emptyDescription="Runs appear here once the pipeline has been started."
        onrowclick={openRun}
      >
        {#snippet cell(row, col, value)}
          {#if col.key === 'status'}
            <Badge tone={statusTone(row.status)}>{value}</Badge>
          {/if}
        {/snippet}
      </DataTable>
    {/if}
  </div>
</Sheet>
