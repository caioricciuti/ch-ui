<script lang="ts">
  import { onMount } from 'svelte'
  import type { PipelineStatus, PipelineStatusInfo } from '../../types/pipelines'
  import * as api from '../../api/pipelines'
  import { formatBytes, formatNumber, formatRelativeTime } from '../../utils/format'
  import { openQueryTab } from '../../stores/tabs.svelte'
  import { goWorkspace } from '../../stores/router.svelte'
  import { quoteIdent } from '../../types/pipelines'
  import IngestStats from '../ingest/IngestStats.svelte'
  import { Activity, Rows3, HardDrive, AlertTriangle, Timer, Clock, Inbox } from 'lucide-svelte'

  interface Props {
    pipelineId: string
    status: PipelineStatus
    /** Last error stored on the pipeline (also set while a running pipeline retries). */
    lastError: string | null
    /** The source is an ingest receiver: show its live rate, spool and rejections. */
    ingest?: boolean
    onStatus?: (info: PipelineStatusInfo) => void
  }

  let { pipelineId, status, lastError, ingest = false, onStatus }: Props = $props()

  let rowsIngested = $state(0)
  let bytesIngested = $state(0)
  let batchesSent = $state(0)
  let errorsCount = $state(0)
  let lastBatchAt = $state<string | null>(null)
  let dlqRows = $state(0)
  let dlqDatabase = $state('')
  let dlqTable = $state('')
  let pollTimer: ReturnType<typeof setInterval> | null = null

  const isRunning = $derived(status === 'running' || status === 'starting')

  onMount(() => {
    // Once, so a stopped pipeline still shows its dead-letter count.
    if (!isRunning) void fetchStatus()
    return () => {
      if (pollTimer) clearInterval(pollTimer)
    }
  })

  $effect(() => {
    if (isRunning && !pollTimer) {
      fetchStatus()
      pollTimer = setInterval(fetchStatus, 5000)
    } else if (!isRunning && pollTimer) {
      clearInterval(pollTimer)
      pollTimer = null
    }
  })

  async function fetchStatus() {
    try {
      const res = await api.getPipelineStatus(pipelineId)
      rowsIngested = res.rows_ingested ?? 0
      bytesIngested = res.bytes_ingested ?? 0
      batchesSent = res.batches_sent ?? 0
      errorsCount = res.errors_count ?? 0
      lastBatchAt = res.last_batch_at ?? lastBatchAt
      dlqRows = res.dlq_rows ?? 0
      dlqDatabase = res.dlq_database ?? ''
      dlqTable = res.dlq_table ?? ''
      onStatus?.(res)
    } catch {
      // Polling errors are transient; the next tick retries.
    }
  }

  /** Opens a query tab on this pipeline's rows in the dead-letter table. */
  function openDeadLetters() {
    if (!dlqTable) return
    const id = pipelineId.replace(/\\/g, '\\\\').replace(/'/g, "\\'")
    openQueryTab(`SELECT ts, error, source_position, raw\nFROM ${quoteIdent(dlqDatabase || 'default')}.${quoteIdent(dlqTable)}\nWHERE pipeline_id = '${id}'\nORDER BY ts DESC\nLIMIT 100`)
    goWorkspace()
  }
</script>

{#if isRunning || rowsIngested > 0 || lastError || dlqRows > 0}
  <div class="flex shrink-0 items-center gap-4 border-t border-edge-subtle bg-surface px-5 py-1.5 text-[12px] tabular-nums text-fg-3">
    {#if isRunning}
      <span class="flex items-center gap-1 {lastError ? 'text-warning' : 'text-success'}">
        <Activity size={12} class="animate-pulse" />
        {lastError ? 'Running, retrying' : 'Running'}
      </span>
    {/if}

    {#if isRunning || rowsIngested > 0}
      <span class="flex items-center gap-1" title="Rows ingested">
        <Rows3 size={12} />
        {formatNumber(rowsIngested)} rows
      </span>

      <span class="flex items-center gap-1" title="Bytes ingested">
        <HardDrive size={12} />
        {formatBytes(bytesIngested)}
      </span>

      <span class="flex items-center gap-1" title="Batches sent">
        <Timer size={12} />
        {formatNumber(batchesSent)} batches
      </span>

      <span class="flex items-center gap-1" title={lastBatchAt ? `Last batch written ${lastBatchAt}` : 'No batch written in this run yet'}>
        <Clock size={12} />
        {lastBatchAt ? `last batch ${formatRelativeTime(lastBatchAt)}` : 'no batch yet'}
      </span>
    {/if}

    {#if ingest && isRunning}
      <IngestStats {pipelineId} compact />
    {/if}

    {#if errorsCount > 0}
      <span class="flex items-center gap-1 text-danger" title="Failed write attempts in this run">
        <AlertTriangle size={12} />
        {formatNumber(errorsCount)} errors
      </span>
    {/if}

    {#if dlqRows > 0}
      <button
        class="flex items-center gap-1 text-warning hover:underline"
        title="Rows ClickHouse refused, kept in {dlqDatabase}.{dlqTable}. Open a query on them."
        onclick={openDeadLetters}
        disabled={!dlqTable}
      >
        <Inbox size={12} />
        {formatNumber(dlqRows)} dead-lettered
      </button>
    {/if}

    {#if lastError}
      <span class="min-w-0 flex-1 truncate text-danger" title={lastError}>{lastError}</span>
    {/if}
  </div>
{/if}
