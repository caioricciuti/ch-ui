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
    source_kafka: 'border-info/60',
    source_webhook: 'border-info/60',
    source_database: 'border-success/60',
    source_s3: 'border-warning/60',
  }

  const bgMap: Record<string, string> = {
    source_kafka: 'bg-info-soft',
    source_webhook: 'bg-info-soft',
    source_database: 'bg-success-soft',
    source_s3: 'bg-warning-soft',
  }

  const Icon = $derived(iconMap[data.node_type] || Radio)
  const borderColor = $derived(colorMap[data.node_type] || 'border-edge')
  const bgColor = $derived(bgMap[data.node_type] || 'bg-surface')
</script>

<div class="rounded-lg border-2 {borderColor} {bgColor} shadow-sm min-w-[160px]">
  <div class="flex items-center gap-2 px-3 py-2">
    <Icon size={16} class="text-fg-2 shrink-0" />
    <div class="min-w-0">
      <div class="text-xs font-medium text-fg truncate">{data.label}</div>
      <div class="text-[10px] text-fg-3">{data.node_type.replace('source_', '')}</div>
    </div>
  </div>
  <Handle type="source" position={Position.Right} class="!bg-accent !w-3 !h-3 !border-2 !border-canvas" />
</div>
