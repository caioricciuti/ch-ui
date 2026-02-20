<script lang="ts">
  import type { ColumnMeta } from '../../types/query'
  import { fetchExplorerData } from '../../api/query'
  import VirtualTable from '../table/VirtualTable.svelte'
  import Pagination from '../table/Pagination.svelte'
  import Spinner from '../common/Spinner.svelte'

  interface Props {
    database: string
    table: string
  }

  let { database, table }: Props = $props()

  let meta = $state<ColumnMeta[]>([])
  let data = $state<unknown[][]>([])
  let page = $state(0)
  let pageSize = $state(100)
  let totalRows = $state(0)
  let sortColumn = $state('')
  let sortDir = $state<'asc' | 'desc'>('asc')
  let loading = $state(false)
  let error = $state<string | null>(null)

  async function loadData() {
    loading = true
    error = null
    try {
      const res = await fetchExplorerData({
        database,
        table,
        page,
        page_size: pageSize,
        sort_column: sortColumn,
        sort_dir: sortDir,
      })
      meta = res.meta ?? []
      data = res.data ?? []
      totalRows = res.total_rows ?? 0
    } catch (e: any) {
      error = e.message
    } finally {
      loading = false
    }
  }

  function handleSort(column: string) {
    if (sortColumn === column) {
      sortDir = sortDir === 'asc' ? 'desc' : 'asc'
    } else {
      sortColumn = column
      sortDir = 'asc'
    }
    page = 0
    loadData()
  }

  function handlePageChange(newPage: number) {
    page = newPage
    loadData()
  }

  // Load when database/table changes
  $effect(() => {
    if (database && table) {
      page = 0
      sortColumn = ''
      sortDir = 'asc'
      loadData()
    }
  })
</script>

<div class="flex flex-col h-full">
  <div class="px-3 py-2 border-b border-gray-200 dark:border-gray-800 bg-gray-100/50 dark:bg-gray-900/50 text-sm text-gray-700 dark:text-gray-300">
    <span class="text-gray-500">{database}.</span><span class="font-medium">{table}</span>
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
    <VirtualTable
      {meta}
      {data}
      {sortColumn}
      {sortDir}
      onsort={handleSort}
    />
    {#if totalRows > pageSize}
      <Pagination {page} {pageSize} {totalRows} onchange={handlePageChange} />
    {/if}
  {:else}
    <div class="flex items-center justify-center flex-1 text-gray-400 dark:text-gray-600 text-sm">
      Select a table to preview data
    </div>
  {/if}
</div>
