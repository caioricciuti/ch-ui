<script lang="ts">
  import { Handle, Position, useUpdateNodeInternals } from '@xyflow/svelte'
  import { Split } from 'lucide-svelte'
  import { readRoute, routeOutputs, UNMATCHED_HANDLE } from '../../../types/transforms'
  import { getNodeError, getTransformCounters } from '../transforms/counters.svelte'
  import { isProActive } from '../../../stores/license.svelte'
  import { formatCompactNumber } from '../../../utils/format'

  interface Props {
    id: string
    data: {
      label: string
      node_type: string
      config?: Record<string, unknown>
    }
  }

  let { id, data }: Props = $props()

  const updateNodeInternals = useUpdateNodeInternals()

  const route = $derived(readRoute(data.config ?? {}))
  const outputs = $derived(routeOutputs(route))
  const counters = $derived(getTransformCounters(id))
  const outBy = $derived(new Map((counters?.outputs ?? []).map((r) => [r.handle, r.out])))
  const saveError = $derived(getNodeError(id))
  const needsPro = $derived(!isProActive())
  const handleKey = $derived(outputs.map((o) => o.handle).join('|'))

  // Handles come and go with the routes; Svelte Flow has to measure them again.
  $effect(() => {
    void handleKey
    updateNodeInternals(id)
  })
</script>

<div class="min-w-[180px] max-w-[240px] rounded-lg border-2 {saveError ? 'border-danger' : 'border-edge-strong'} bg-surface shadow-sm" title={saveError || undefined}>
  <Handle type="target" position={Position.Left} class="!bg-accent !w-3 !h-3 !border-2 !border-canvas" />
  <div class="flex items-center gap-2 px-3 py-2">
    <Split size={16} class="shrink-0 text-fg-2" />
    <div class="min-w-0">
      <div class="truncate text-xs font-medium text-fg">{data.label}</div>
      <div class="flex items-center gap-1 text-[10px] text-fg-3">
        route, {route.mode === 'all' ? 'every match' : 'first match'}
        {#if needsPro}
          <span class="rounded-sm bg-surface-2 px-1 text-[9px] font-semibold uppercase tracking-wider text-fg-4">Pro</span>
        {/if}
      </div>
    </div>
  </div>
  {#if saveError}
    <div class="truncate border-t border-edge-subtle px-3 py-1 text-[10px] text-danger">{saveError}</div>
  {/if}
  {#if counters}
    <div class="flex items-center gap-2 border-t border-edge-subtle px-3 py-1 text-[10px] tabular-nums text-fg-3" title="Records since start">
      <span>in {formatCompactNumber(counters.in)}</span>
      {#if counters.unmatched_dropped}
        <span class="text-warning">unmatched dropped {formatCompactNumber(counters.unmatched_dropped)}</span>
      {/if}
      {#if counters.errors}
        <span class="text-danger">err {formatCompactNumber(counters.errors)}</span>
      {/if}
    </div>
  {/if}
  <div class="border-t border-edge-subtle py-1">
    {#each outputs as o (o.handle)}
      <div class="relative flex h-6 items-center justify-end gap-2 pl-3 pr-4 text-[10px]">
        <span class="min-w-0 truncate {o.handle === UNMATCHED_HANDLE ? 'italic text-fg-4' : 'text-fg-2'}" title={o.label}>{o.label}</span>
        {#if counters}
          <span class="shrink-0 tabular-nums text-fg-3" title="Records sent to {o.label} since start">{formatCompactNumber(outBy.get(o.handle) ?? 0)}</span>
        {/if}
        <Handle id={o.handle} type="source" position={Position.Right} class="!bg-accent !w-3 !h-3 !border-2 !border-canvas" />
      </div>
    {:else}
      <div class="px-3 text-[10px] text-fg-4">No routes; nothing matches</div>
    {/each}
  </div>
</div>
