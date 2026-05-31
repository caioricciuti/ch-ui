<script lang="ts">
  import type { ColumnMeta } from '../../types/query'
  import { fetchExplorerData } from '../../api/query'
  import VirtualTable from '../table/VirtualTable.svelte'
  import Spinner from '../common/Spinner.svelte'

  interface Props {
    database: string
    table: string
  }

  let { database, table }: Props = $props()

  // Data Sample is a lightweight preview — a fixed number of rows, no pagination.
  const SAMPLE_LIMIT = 20

  let meta = $state<ColumnMeta[]>([])
  let data = $state<unknown[][]>([])
  let loading = $state(false)
  let error = $state<string | null>(null)

  async function loadData() {
    loading = true
    error = null
    try {
      const res = await fetchExplorerData({
        database,
        table,
        page: 0,
        page_size: SAMPLE_LIMIT,
      })
      meta = res.meta ?? []
      data = res.data ?? []
    } catch (e: any) {
      error = e.message
    } finally {
      loading = false
    }
  }

  // Reload the sample whenever the target database/table changes.
  $effect(() => {
    if (database && table) loadData()
  })
</script>

<div class="flex flex-col h-full">
  <div class="px-3 py-2 border-b border-gray-200 dark:border-gray-800 bg-gray-100/50 dark:bg-gray-900/50 text-sm text-gray-700 dark:text-gray-300">
    <span class="text-gray-500">{database}.</span><span class="font-medium">{table}</span>
    <span class="ml-2 text-xs text-gray-400">First {SAMPLE_LIMIT} rows</span>
  </div>

  {#if loading && meta.length === 0}
    <div class="flex items-center justify-center flex-1 gap-2">
      <Spinner size="sm" />
      <span class="text-sm text-gray-500">Loading...</span>
    </div>
  {:else if error}
    <div class="p-4">
      <div class="bg-red-100/20 dark:bg-red-900/20 border border-red-300/50 dark:border-red-800/50 rounded-lg p-3 text-sm text-red-700 dark:text-red-300">
        {error}
      </div>
    </div>
  {:else if meta.length > 0}
    <VirtualTable {meta} {data} />
  {:else}
    <div class="flex items-center justify-center flex-1 text-gray-400 dark:text-gray-600 text-sm">
      Select a table to preview data
    </div>
  {/if}
</div>
