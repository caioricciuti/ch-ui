<script lang="ts">
  import { onMount } from 'svelte'
  import type { PipelineStatus } from '../../types/pipelines'
  import * as api from '../../api/pipelines'
  import { formatBytes, formatNumber } from '../../utils/format'
  import { Activity, Rows3, HardDrive, AlertTriangle, Timer } from 'lucide-svelte'

  interface Props {
    pipelineId: string
    status: PipelineStatus
    onStatusChange?: (status: PipelineStatus) => void
  }

  let { pipelineId, status, onStatusChange }: Props = $props()

  let rowsIngested = $state(0)
  let bytesIngested = $state(0)
  let batchesSent = $state(0)
  let errorsCount = $state(0)
  let pollTimer = $state<ReturnType<typeof setInterval> | null>(null)

  const isRunning = $derived(status === 'running' || status === 'starting')

  onMount(() => {
    if (isRunning) {
      startPolling()
    }
    return () => {
      if (pollTimer) clearInterval(pollTimer)
    }
  })

  $effect(() => {
    if (isRunning && !pollTimer) {
      startPolling()
    } else if (!isRunning && pollTimer) {
      clearInterval(pollTimer)
      pollTimer = null
    }
  })

  function startPolling() {
    fetchStatus()
    pollTimer = setInterval(fetchStatus, 5000)
  }

  async function fetchStatus() {
    try {
      const res = await api.getPipelineStatus(pipelineId)
      rowsIngested = res.rows_ingested ?? 0
      bytesIngested = res.bytes_ingested ?? 0
      batchesSent = res.batches_sent ?? 0
      errorsCount = res.errors_count ?? 0

      if (res.status !== status && onStatusChange) {
        onStatusChange(res.status as PipelineStatus)
      }
    } catch {
      // Silently handle polling errors
    }
  }

</script>

{#if isRunning || rowsIngested > 0}
  <div class="flex shrink-0 items-center gap-4 border-t border-edge-subtle bg-surface px-5 py-1.5 text-[11px] tabular-nums text-fg-3">
    {#if isRunning}
      <span class="flex items-center gap-1 text-success">
        <Activity size={12} class="animate-pulse" />
        Running
      </span>
    {/if}

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

    {#if errorsCount > 0}
      <span class="flex items-center gap-1 text-danger" title="Errors">
        <AlertTriangle size={12} />
        {formatNumber(errorsCount)} errors
      </span>
    {/if}
  </div>
{/if}
