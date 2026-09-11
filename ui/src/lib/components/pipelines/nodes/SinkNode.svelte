<script lang="ts">
  import { Handle, Position } from '@xyflow/svelte'
  import { Database } from 'lucide-svelte'

  interface Props {
    data: {
      label: string
      node_type: string
      config?: Record<string, unknown>
    }
  }

  let { data }: Props = $props()
</script>

<div class="rounded-lg border-2 border-accent/60 bg-accent-soft min-w-[160px]">
  <div class="flex items-center gap-2 px-3 py-2">
    <Database size={16} class="text-accent shrink-0" />
    <div class="min-w-0">
      <div class="text-xs font-medium text-fg truncate">{data.label}</div>
      <div class="text-[10px] text-fg-3">ClickHouse</div>
    </div>
  </div>
  <Handle type="target" position={Position.Left} class="!bg-accent !w-3 !h-3 !border-2 !border-canvas" />
</div>
