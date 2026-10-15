<script lang="ts" module>
  import type { LineageNodeKind } from '../../types/lineage'

  export interface LineageCardData {
    name: string
    /** Second line: database, or what an external node points at. */
    sub: string
    kindLabel: string
    /** Null for a collapsed database. */
    kind: LineageNodeKind | null
    stats: string
    focus: boolean
    selected: boolean
    [key: string]: unknown
  }
</script>

<script lang="ts">
  import { Handle, Position } from '@xyflow/svelte'
  import { NODE_HEIGHT, NODE_WIDTH } from '../../utils/lineage-layout'

  interface Props {
    data: LineageCardData
  }

  let { data }: Props = $props()

  const inert = $derived(data.kind === 'hidden')
  const frame = $derived.by(() => {
    if (data.selected) return 'border-accent ring-1 ring-accent'
    if (data.kind === 'missing') return 'border-dashed border-edge-strong'
    if (data.kind === null) return 'border-dashed border-edge'
    if (data.focus) return 'border-fg-3'
    return 'border-edge'
  })
  const fill = $derived(inert || data.kind === null ? 'bg-surface-2' : 'bg-surface')
</script>

<div
  class="flex flex-col justify-center gap-0.5 rounded-md border px-3 {frame} {fill} {inert ? 'cursor-default' : 'cursor-pointer hover:border-edge-strong'}"
  style="width: {NODE_WIDTH}px; height: {NODE_HEIGHT}px"
  title={inert ? 'You do not have access to this object' : undefined}
>
  <div class="flex min-w-0 items-baseline gap-2">
    <span class="min-w-0 flex-1 truncate text-[13px] {data.focus ? 'font-semibold' : 'font-medium'} {inert ? 'text-fg-4' : 'text-fg'}">{data.name}</span>
    <span class="shrink-0 text-[10px] uppercase tracking-wide {inert ? 'text-fg-4' : 'text-fg-3'}">{data.kindLabel}</span>
  </div>
  <div class="flex min-w-0 items-baseline gap-2 text-[11px] text-fg-4">
    <span class="min-w-0 flex-1 truncate">{data.sub}</span>
    {#if data.stats}<span class="shrink-0 tabular-nums text-fg-3">{data.stats}</span>{/if}
  </div>
  <Handle type="target" position={Position.Left} isConnectable={false} class="!h-1.5 !w-1.5 !min-w-0 !border-0 !bg-transparent" />
  <Handle type="source" position={Position.Right} isConnectable={false} class="!h-1.5 !w-1.5 !min-w-0 !border-0 !bg-transparent" />
</div>
