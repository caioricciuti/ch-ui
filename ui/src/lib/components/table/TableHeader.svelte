<script lang="ts">
  import type { ColumnMeta } from '../../types/query'
  import { getDisplayType, isRightAligned } from '../../utils/ch-types'
  import { ArrowUp, ArrowDown } from 'lucide-svelte'

  interface Props {
    columns: ColumnMeta[]
    widths: number[]
    sortColumn?: string
    sortDir?: 'asc' | 'desc'
    onsort?: (column: string) => void
    onresize?: (index: number, width: number) => void
    onfitcolumn?: (index: number) => void
    onfitall?: () => void
  }

  let { columns, widths, sortColumn = '', sortDir = 'asc', onsort, onresize, onfitcolumn, onfitall }: Props = $props()

  let resizing = $state<{ index: number; startX: number; startWidth: number } | null>(null)

  function handleMouseDown(e: MouseEvent, index: number) {
    e.preventDefault()
    e.stopPropagation()
    resizing = { index, startX: e.clientX, startWidth: widths[index] }
    window.addEventListener('mousemove', handleMouseMove)
    window.addEventListener('mouseup', handleMouseUp)
  }

  function handleMouseMove(e: MouseEvent) {
    if (!resizing) return
    const delta = e.clientX - resizing.startX
    const newWidth = Math.max(50, resizing.startWidth + delta)
    onresize?.(resizing.index, newWidth)
  }

  function handleMouseUp() {
    resizing = null
    window.removeEventListener('mousemove', handleMouseMove)
    window.removeEventListener('mouseup', handleMouseUp)
  }

  function handleFitColumn(e: MouseEvent, index: number) {
    e.preventDefault()
    e.stopPropagation()
    onfitcolumn?.(index)
  }

  function compactTypeLabel(type: string): string {
    const normalized = type
      .replace(/^Nullable\((.+)\)$/, '$1')
      .replace(/^LowCardinality\((.+)\)$/, '$1')
      .replace(/\s+/g, '')

    if (normalized.length <= 16) return normalized
    return `${normalized.slice(0, 15)}…`
  }

  function typeTone(type: string): string {
    switch (getDisplayType(type)) {
      case 'number':
        return 'text-orange-700 dark:text-orange-300 bg-orange-100/70 dark:bg-orange-500/15 border-orange-200/70 dark:border-orange-500/25'
      case 'date':
        return 'text-emerald-700 dark:text-emerald-300 bg-emerald-100/70 dark:bg-emerald-500/15 border-emerald-200/70 dark:border-emerald-500/25'
      case 'bool':
        return 'text-fuchsia-700 dark:text-fuchsia-300 bg-fuchsia-100/70 dark:bg-fuchsia-500/12 border-fuchsia-200/70 dark:border-fuchsia-500/20'
      case 'json':
        return 'text-sky-700 dark:text-sky-300 bg-sky-100/70 dark:bg-sky-500/12 border-sky-200/70 dark:border-sky-500/20'
      default:
        return 'text-gray-600 dark:text-gray-300 bg-gray-100/80 dark:bg-gray-800/80 border-gray-200/75 dark:border-gray-700/75'
    }
  }
</script>

<thead class="sticky top-0 z-10 bg-gray-100 dark:bg-gray-900">
  <tr class="border-b border-gray-300 dark:border-gray-700">
    <!-- Row number header -->
    <th
      class="sticky left-0 z-20 px-2.5 py-2 text-xs font-semibold text-gray-700 dark:text-gray-200 border-r border-gray-200 dark:border-gray-800 text-center select-none bg-gray-100 dark:bg-gray-900"
      style="width:60px;max-width:60px;min-width:60px"
      ondblclick={() => onfitall?.()}
      title="Double-click to auto-fit all columns"
    >#</th>
    {#each columns as col, i}
      <th
        class="px-2.5 py-2 text-xs font-medium text-gray-500 dark:text-gray-400 border-r border-gray-200/60 dark:border-gray-800/60 select-none relative group bg-gray-100 dark:bg-gray-900
          {isRightAligned(col.type) ? 'text-right' : 'text-left'}"
        style="width:{widths[i]}px;max-width:{widths[i]}px;min-width:{widths[i]}px"
      >
        {#if onsort}
          <button
            type="button"
            class="w-full flex items-center gap-2 min-w-0 {isRightAligned(col.type) ? 'justify-end' : ''} hover:text-gray-800 dark:hover:text-gray-200"
            onclick={() => onsort?.(col.name)}
          >
            <span class="truncate font-semibold text-[11px]" title={col.name}>{col.name}</span>
            <span class="inline-flex shrink-0 items-center rounded px-1.5 py-0.5 text-[10px] font-semibold border {typeTone(col.type)}">
              {compactTypeLabel(col.type)}
            </span>
            {#if sortColumn === col.name}
              {#if sortDir === 'asc'}
                <ArrowUp size={12} class="shrink-0" />
              {:else}
                <ArrowDown size={12} class="shrink-0" />
              {/if}
            {/if}
          </button>
        {:else}
          <div class="w-full flex items-center gap-2 min-w-0 {isRightAligned(col.type) ? 'justify-end' : ''}">
            <span class="truncate font-semibold text-[11px]" title={col.name}>{col.name}</span>
            <span class="inline-flex shrink-0 items-center rounded px-1.5 py-0.5 text-[10px] font-semibold border {typeTone(col.type)}">
              {compactTypeLabel(col.type)}
            </span>
          </div>
        {/if}

        <!-- Resize handle -->
        {#if onresize}
          <button
            type="button"
            class="absolute right-0 top-0 bottom-0 w-2.5 cursor-col-resize opacity-0 group-hover:opacity-100 hover:bg-ch-orange/35 active:bg-ch-orange/45 transition-opacity"
            onmousedown={(e) => handleMouseDown(e, i)}
            ondblclick={(e) => handleFitColumn(e, i)}
            onclick={(e) => e.stopPropagation()}
            title="Drag to resize, double-click to auto-fit"
            aria-label={`Resize ${col.name} column`}
          ></button>
        {/if}
      </th>
    {/each}
  </tr>
</thead>
