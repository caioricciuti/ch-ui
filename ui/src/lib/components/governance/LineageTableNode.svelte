<script lang="ts">
  import { Handle, Position } from '@xyflow/svelte'
  import { Database, ChevronDown, ChevronRight } from 'lucide-svelte'
  import type { GovColumn } from '../../types/governance'

  interface Props {
    data: {
      database: string
      table: string
      nodeType: string
      columns: GovColumn[]
      linkedColumns: string[]
    }
  }

  let { data }: Props = $props()
  let expanded = $state(false)

  function nodeColor(type: string): { border: string; bg: string } {
    switch (type) {
      case 'current':
      case 'materialized_view':
        return { border: 'border-orange-400 dark:border-orange-500', bg: 'bg-orange-50 dark:bg-orange-900/20' }
      case 'source':
        return { border: 'border-blue-400 dark:border-blue-500', bg: 'bg-blue-50 dark:bg-blue-900/20' }
      case 'view':
        return { border: 'border-purple-400 dark:border-purple-500', bg: 'bg-purple-50 dark:bg-purple-900/20' }
      default: // target
        return { border: 'border-green-400 dark:border-green-500', bg: 'bg-green-50 dark:bg-green-900/20' }
    }
  }

  const colors = $derived(nodeColor(data.nodeType))
  const borderColor = $derived(colors.border)
  const bgColor = $derived(colors.bg)

  const linkedSet = $derived(new Set(data.linkedColumns))
</script>

<div class="rounded-lg border-2 {borderColor} {bgColor} shadow-sm w-[220px]">
  <Handle type="target" position={Position.Left} class="!bg-blue-500 !w-3 !h-3 !border-2 !border-white dark:!border-gray-900" />

  <div class="px-3 py-2">
    <div class="flex items-center gap-2">
      <Database size={14} class="text-gray-500 shrink-0" />
      <div class="min-w-0 flex-1">
        <div class="text-[10px] text-gray-500 dark:text-gray-400 truncate">{data.database}</div>
        <div class="text-xs font-semibold text-gray-800 dark:text-gray-200 truncate">{data.table}</div>
        {#if data.nodeType === 'materialized_view'}
          <span class="text-[9px] px-1 py-0.5 rounded bg-orange-200/60 dark:bg-orange-800/40 text-orange-700 dark:text-orange-300">MV</span>
        {:else if data.nodeType === 'view'}
          <span class="text-[9px] px-1 py-0.5 rounded bg-purple-200/60 dark:bg-purple-800/40 text-purple-700 dark:text-purple-300">View</span>
        {/if}
      </div>
    </div>
  </div>

  {#if data.columns && data.columns.length > 0}
    <button
      class="w-full flex items-center gap-1.5 px-3 py-1 text-[10px] text-gray-500 hover:bg-gray-100 dark:hover:bg-gray-800/40 border-t border-gray-200 dark:border-gray-700/50 cursor-pointer"
      onclick={() => (expanded = !expanded)}
    >
      {#if expanded}
        <ChevronDown size={10} />
      {:else}
        <ChevronRight size={10} />
      {/if}
      {data.columns.length} columns
    </button>

    {#if expanded}
      <div class="max-h-40 overflow-auto px-2 pb-2">
        {#each data.columns as col}
          <div class="flex items-center gap-1.5 py-0.5 px-1 rounded {linkedSet.has(col.column_name) ? 'bg-orange-100/60 dark:bg-orange-900/30' : ''}">
            <span
              class="text-[10px] font-mono truncate flex-1 {linkedSet.has(col.column_name)
                ? 'text-orange-700 dark:text-orange-300 font-semibold'
                : 'text-gray-600 dark:text-gray-400'}"
            >
              {col.column_name}
            </span>
            <span class="text-[9px] text-gray-400 dark:text-gray-500 shrink-0">{col.column_type}</span>
          </div>
        {/each}
      </div>
    {/if}
  {/if}

  <Handle type="source" position={Position.Right} class="!bg-orange-500 !w-3 !h-3 !border-2 !border-white dark:!border-gray-900" />
</div>
