<script lang="ts">
  import { Handle, Position } from '@xyflow/svelte'
  import { Radio, Webhook, Database, HardDrive } from 'lucide-svelte'

  interface Props {
    data: {
      label: string
      node_type: string
      config?: Record<string, unknown>
    }
  }

  let { data }: Props = $props()

  const iconMap: Record<string, typeof Radio> = {
    source_kafka: Radio,
    source_webhook: Webhook,
    source_database: Database,
    source_s3: HardDrive,
  }

  const colorMap: Record<string, string> = {
    source_kafka: 'border-purple-400 dark:border-purple-600',
    source_webhook: 'border-blue-400 dark:border-blue-600',
    source_database: 'border-emerald-400 dark:border-emerald-600',
    source_s3: 'border-amber-400 dark:border-amber-600',
  }

  const bgMap: Record<string, string> = {
    source_kafka: 'bg-purple-50 dark:bg-purple-900/20',
    source_webhook: 'bg-blue-50 dark:bg-blue-900/20',
    source_database: 'bg-emerald-50 dark:bg-emerald-900/20',
    source_s3: 'bg-amber-50 dark:bg-amber-900/20',
  }

  const Icon = $derived(iconMap[data.node_type] || Radio)
  const borderColor = $derived(colorMap[data.node_type] || 'border-gray-300 dark:border-gray-700')
  const bgColor = $derived(bgMap[data.node_type] || 'bg-gray-50 dark:bg-gray-900')
</script>

<div class="rounded-lg border-2 {borderColor} {bgColor} shadow-sm min-w-[160px]">
  <div class="flex items-center gap-2 px-3 py-2">
    <Icon size={16} class="text-gray-600 dark:text-gray-300 shrink-0" />
    <div class="min-w-0">
      <div class="text-xs font-medium text-gray-800 dark:text-gray-200 truncate">{data.label}</div>
      <div class="text-[10px] text-gray-500 dark:text-gray-400">{data.node_type.replace('source_', '')}</div>
    </div>
  </div>
  <Handle type="source" position={Position.Right} class="!bg-orange-500 !w-3 !h-3 !border-2 !border-white dark:!border-gray-900" />
</div>
