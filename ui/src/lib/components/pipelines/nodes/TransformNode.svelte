<script lang="ts">
  import { Handle, Position } from '@xyflow/svelte'
  import { Braces, ArrowLeftRight, Funnel, Percent, EyeOff } from 'lucide-svelte'
  import { isProTransform, transformLabel, type TransformNodeType } from '../../../types/transforms'
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

  const iconMap: Record<string, typeof Braces> = {
    transform_parse: Braces,
    transform_map: ArrowLeftRight,
    transform_filter: Funnel,
    transform_sample: Percent,
    transform_redact: EyeOff,
  }

  const Icon = $derived(iconMap[data.node_type] || Braces)
  const counters = $derived(getTransformCounters(id))
  const saveError = $derived(getNodeError(id))
  const needsPro = $derived(isProTransform(data.node_type) && !isProActive())
</script>

<div class="min-w-[160px] max-w-[240px] rounded-lg border-2 {saveError ? 'border-danger' : 'border-edge-strong'} bg-surface shadow-sm" title={saveError || undefined}>
  <Handle type="target" position={Position.Left} class="!bg-accent !w-3 !h-3 !border-2 !border-canvas" />
  <div class="flex items-center gap-2 px-3 py-2">
    <Icon size={16} class="shrink-0 text-fg-2" />
    <div class="min-w-0">
      <div class="truncate text-xs font-medium text-fg">{data.label}</div>
      <div class="flex items-center gap-1 text-[10px] text-fg-3">
        {transformLabel(data.node_type as TransformNodeType).toLowerCase()}
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
    <div class="flex items-center gap-2 border-t border-edge-subtle px-3 py-1 text-[10px] tabular-nums text-fg-3" title="Records since start: in, out, dropped{counters.errors ? ', parse errors' : ''}">
      <span>in {formatCompactNumber(counters.in)}</span>
      <span>out {formatCompactNumber(counters.out)}</span>
      <span class={counters.dropped ? 'text-warning' : ''}>dropped {formatCompactNumber(counters.dropped)}</span>
      {#if counters.errors}
        <span class="text-danger">err {formatCompactNumber(counters.errors)}</span>
      {/if}
    </div>
  {/if}
  <Handle type="source" position={Position.Right} class="!bg-accent !w-3 !h-3 !border-2 !border-canvas" />
</div>
