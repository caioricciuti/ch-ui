<script lang="ts">
  import { Handle, Position } from '@xyflow/svelte'
  import { Eye, Table2 } from 'lucide-svelte'
  import type { Materialization, ModelStatus } from '../../types/models'

  interface Props {
    data: {
      name: string
      materialization: Materialization
      status: ModelStatus
      target_database: string
    }
  }

  let { data }: Props = $props()

  const statusColor = $derived(
    data.status === 'success'
      ? 'border-success/60'
      : data.status === 'error'
        ? 'border-danger/60'
        : 'border-edge'
  )

  const statusBg = $derived(
    data.status === 'success'
      ? 'bg-success-soft'
      : data.status === 'error'
        ? 'bg-danger-soft'
        : 'bg-surface '
  )

  const statusDot = $derived(
    data.status === 'success'
      ? 'bg-success'
      : data.status === 'error'
        ? 'bg-danger'
        : 'bg-fg-4'
  )

  const Icon = $derived(data.materialization === 'table' ? Table2 : Eye)
</script>

<div class="rounded-lg border-2 {statusColor} {statusBg} shadow-sm min-w-[180px] cursor-pointer hover:shadow-md transition-shadow">
  <div class="flex items-center gap-2 px-3 py-2">
    <Icon size={14} class="text-fg-2 shrink-0" />
    <div class="min-w-0 flex-1">
      <div class="text-xs font-medium text-fg truncate">{data.name}</div>
      <div class="text-[10px] text-fg-3 flex items-center gap-1">
        <span>{data.materialization}</span>
        <span class="opacity-50">|</span>
        <span>{data.target_database}</span>
      </div>
    </div>
    <span class="w-2 h-2 rounded-full {statusDot} shrink-0"></span>
  </div>
  <Handle type="target" position={Position.Left} class="!bg-accent !w-3 !h-3 !border-2 !border-canvas" />
  <Handle type="source" position={Position.Right} class="!bg-accent !w-3 !h-3 !border-2 !border-canvas" />
</div>
