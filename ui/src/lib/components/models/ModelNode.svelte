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
      ? 'border-green-400 dark:border-green-600'
      : data.status === 'error'
        ? 'border-red-400 dark:border-red-600'
        : 'border-gray-300 dark:border-gray-600'
  )

  const statusBg = $derived(
    data.status === 'success'
      ? 'bg-green-50 dark:bg-green-900/20'
      : data.status === 'error'
        ? 'bg-red-50 dark:bg-red-900/20'
        : 'bg-gray-50 dark:bg-gray-800/40'
  )

  const statusDot = $derived(
    data.status === 'success'
      ? 'bg-green-500'
      : data.status === 'error'
        ? 'bg-red-500'
        : 'bg-gray-400'
  )

  const Icon = $derived(data.materialization === 'table' ? Table2 : Eye)
</script>

<div class="rounded-lg border-2 {statusColor} {statusBg} shadow-sm min-w-[180px] cursor-pointer hover:shadow-md transition-shadow">
  <div class="flex items-center gap-2 px-3 py-2">
    <Icon size={14} class="text-gray-600 dark:text-gray-300 shrink-0" />
    <div class="min-w-0 flex-1">
      <div class="text-xs font-medium text-gray-800 dark:text-gray-200 truncate">{data.name}</div>
      <div class="text-[10px] text-gray-500 dark:text-gray-400 flex items-center gap-1">
        <span>{data.materialization}</span>
        <span class="opacity-50">|</span>
        <span>{data.target_database}</span>
      </div>
    </div>
    <span class="w-2 h-2 rounded-full {statusDot} shrink-0"></span>
  </div>
  <Handle type="target" position={Position.Left} class="!bg-orange-500 !w-3 !h-3 !border-2 !border-white dark:!border-gray-900" />
  <Handle type="source" position={Position.Right} class="!bg-orange-500 !w-3 !h-3 !border-2 !border-white dark:!border-gray-900" />
</div>
