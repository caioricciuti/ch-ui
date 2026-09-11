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
  <div class="px-3 py-2 border-b border-edge-subtle bg-surface text-[13px] text-fg-2">
    <span class="text-fg-3">{database}.</span><span class="font-medium">{table}</span>
    <span class="ml-2 text-xs text-fg-4">First {SAMPLE_LIMIT} rows</span>
  </div>

  {#if loading && meta.length === 0}
    <div class="flex items-center justify-center flex-1 gap-2">
      <Spinner size="sm" />
      <span class="text-[13px] text-fg-3">Loading...</span>
    </div>
  {:else if error}
    <div class="p-4">
      <div class="rounded-md bg-danger-soft p-3 text-[13px] text-fg">
        {error}
      </div>
    </div>
  {:else if meta.length > 0}
    <VirtualTable {meta} {data} />
  {:else}
    <div class="flex items-center justify-center flex-1 text-fg-4 text-[13px]">
      Select a table to preview data
    </div>
  {/if}
</div>
