<script lang="ts">
  import { ChevronLeft, ChevronRight } from 'lucide-svelte'
  import { formatNumber } from '../../utils/format'

  interface Props {
    page: number
    pageSize: number
    totalRows: number
    onchange: (page: number) => void
  }

  let { page, pageSize, totalRows, onchange }: Props = $props()

  const totalPages = $derived(Math.max(1, Math.ceil(totalRows / pageSize)))
  const from = $derived(page * pageSize + 1)
  const to = $derived(Math.min((page + 1) * pageSize, totalRows))
</script>

<div class="flex items-center justify-between px-3 py-2 border-t border-gray-200 dark:border-gray-800 bg-gray-100/50 dark:bg-gray-900/50 text-xs text-gray-500 dark:text-gray-400">
  <span>
    {formatNumber(from)}-{formatNumber(to)} of {formatNumber(totalRows)} rows
  </span>

  <div class="flex items-center gap-1">
    <button
      class="p-1 rounded hover:bg-gray-200 dark:hover:bg-gray-800 disabled:opacity-30 disabled:cursor-not-allowed"
      disabled={page === 0}
      onclick={() => onchange(page - 1)}
    >
      <ChevronLeft size={14} />
    </button>
    <span class="px-2">
      Page {page + 1} of {totalPages}
    </span>
    <button
      class="p-1 rounded hover:bg-gray-200 dark:hover:bg-gray-800 disabled:opacity-30 disabled:cursor-not-allowed"
      disabled={page >= totalPages - 1}
      onclick={() => onchange(page + 1)}
    >
      <ChevronRight size={14} />
    </button>
  </div>
</div>
