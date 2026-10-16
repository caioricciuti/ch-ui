<script lang="ts">
  import { Handle, Position } from '@xyflow/svelte'
  import { Database } from 'lucide-svelte'
  import { getNodeError, getSinkCounters } from '../transforms/counters.svelte'
  import { formatCompactNumber, formatRelativeTime } from '../../../utils/format'

  interface Props {
    id: string
    data: {
      label: string
      node_type: string
      config?: Record<string, unknown>
    }
  }

  let { id, data }: Props = $props()

  const counters = $derived(getSinkCounters(id))
  const nodeError = $derived(getNodeError(id))
  const table = $derived(typeof data.config?.table === 'string' && data.config.table ? `${String(data.config.database || 'default')}.${data.config.table}` : 'ClickHouse')
</script>

<div class="rounded-lg border-2 {nodeError ? 'border-danger' : 'border-accent/60'} bg-accent-soft min-w-[160px] max-w-[240px]" title={nodeError || undefined}>
  <div class="flex items-center gap-2 px-3 py-2">
    <Database size={16} class="text-accent shrink-0" />
    <div class="min-w-0">
      <div class="text-xs font-medium text-fg truncate">{data.label}</div>
      <div class="max-w-[180px] truncate text-[10px] text-fg-3" title={table}>{table}</div>
    </div>
  </div>
  {#if nodeError}
    <div class="truncate border-t border-accent/30 px-3 py-1 text-[10px] text-danger">{nodeError}</div>
  {/if}
  {#if counters}
    <div
      class="flex items-center gap-2 border-t border-accent/30 px-3 py-1 text-[10px] tabular-nums text-fg-3"
      title={counters.last_write_at ? `Last write ${formatRelativeTime(counters.last_write_at)}` : 'No write in this run yet'}
    >
      <span>rows {formatCompactNumber(counters.rows)}</span>
      <span>batches {formatCompactNumber(counters.batches)}</span>
      {#if counters.errors}
        <span class="text-danger">err {formatCompactNumber(counters.errors)}</span>
      {/if}
    </div>
  {/if}
  <Handle type="target" position={Position.Left} class="!bg-accent !w-3 !h-3 !border-2 !border-canvas" />
</div>
