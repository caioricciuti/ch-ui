<script lang="ts">
  import { onMount } from 'svelte'
  import type { DatabaseTab } from '../../../stores/tabs.svelte'
  import type { ColumnMeta } from '../../../types/query'
  import { fetchDatabaseInfo, fetchDatabaseTables } from '../../../api/query'
  import { formatBytes, formatNumber } from '../../../utils/format'
  import Spinner from '../../common/Spinner.svelte'
  import VirtualTable from '../../table/VirtualTable.svelte'
  import { Database, HardDrive, Rows3, Table2, Clock, RefreshCw, FolderOpen } from 'lucide-svelte'

  interface Props {
    tab: DatabaseTab
  }

  let { tab }: Props = $props()

  type SubTab = 'overview' | 'tables'
  let activeSubTab = $state<SubTab>('overview')

  let dbInfo = $state<Record<string, any>>({})
  let infoLoading = $state(true)
  let infoError = $state<string | null>(null)

  let tablesMeta = $state<ColumnMeta[]>([])
  let tablesData = $state<unknown[][]>([])
  let tablesLoading = $state(false)
  let tablesError = $state<string | null>(null)
  let tablesLoaded = $state(false)

  const subTabs: { id: SubTab; label: string }[] = [
    { id: 'overview', label: 'Overview' },
    { id: 'tables', label: 'Tables' },
  ]

  const metrics = $derived.by(() => {
    if (!dbInfo || Object.keys(dbInfo).length === 0) return []
    return [
      { label: 'Tables', value: formatNumber(Number(dbInfo.table_count ?? 0)), icon: Table2, color: 'text-ch-orange' },
      { label: 'Rows', value: formatNumber(Number(dbInfo.total_rows ?? 0)), icon: Rows3, color: 'text-ch-orange' },
      { label: 'Total Size', value: formatBytes(Number(dbInfo.total_bytes ?? 0)), icon: HardDrive, color: 'text-ch-green' },
      { label: 'Engine', value: dbInfo.engine ?? '—', icon: Database, color: 'text-gray-500' },
    ]
  })

  function formatDate(value: unknown): string {
    if (!value) return '—'
    const date = new Date(String(value))
    if (Number.isNaN(date.getTime())) return String(value)
    return date.toLocaleString()
  }

  async function loadInfo() {
    infoLoading = true
    infoError = null
    try {
      dbInfo = await fetchDatabaseInfo(tab.database)
    } catch (e: any) {
      infoError = e.message
    } finally {
      infoLoading = false
    }
  }

  async function loadTables() {
    if (tablesLoaded) return
    tablesLoading = true
    tablesError = null
    try {
      const res = await fetchDatabaseTables(tab.database)
      tablesMeta = res.meta ?? []
      tablesData = (res.data ?? []).map((row: any) => {
        if (Array.isArray(row)) return row
        return (res.meta ?? []).map((col: any) => row[col.name])
      })
      tablesLoaded = true
    } catch (e: any) {
      tablesError = e.message
    } finally {
      tablesLoading = false
    }
  }

  function switchTab(next: SubTab) {
    activeSubTab = next
    if (next === 'tables') loadTables()
  }

  function refresh() {
    tablesLoaded = false
    if (activeSubTab === 'tables') loadTables()
    loadInfo()
  }

  onMount(() => {
    loadInfo()
  })
</script>

<div class="flex flex-col h-full">
  <div class="flex items-center gap-3 px-4 py-3 border-b border-gray-200 dark:border-gray-800 bg-gray-100/40 dark:bg-gray-900/45 shrink-0">
    <Database size={16} class="text-ch-orange shrink-0" />
    <div class="min-w-0">
      <span class="text-sm font-medium text-gray-800 dark:text-gray-200 truncate">{tab.database}</span>
    </div>
    <button
      class="ml-1 p-1 rounded hover:bg-gray-200 dark:hover:bg-gray-800 transition-colors text-gray-400 hover:text-gray-600 dark:hover:text-gray-300"
      onclick={refresh}
      title="Refresh database info"
    >
      <RefreshCw size={13} class={infoLoading || tablesLoading ? 'animate-spin' : ''} />
    </button>
  </div>

  <div class="flex items-center gap-1 px-3 py-1.5 border-b border-gray-200 dark:border-gray-800 bg-white dark:bg-gray-950 shrink-0">
    {#each subTabs as st}
      <button
        class="px-3 py-1.5 text-xs rounded-md transition-colors whitespace-nowrap {activeSubTab === st.id
          ? 'bg-gray-200 dark:bg-gray-800 text-gray-800 dark:text-gray-200 font-medium'
          : 'text-gray-500 hover:text-gray-700 dark:hover:text-gray-300 hover:bg-gray-200/50 dark:hover:bg-gray-800/50'}"
        onclick={() => switchTab(st.id)}
      >{st.label}</button>
    {/each}
  </div>

  <div class="flex-1 min-h-0 overflow-auto">
    {#if activeSubTab === 'overview'}
      {#if infoLoading}
        <div class="flex items-center justify-center py-12 gap-2">
          <Spinner size="sm" />
          <span class="text-sm text-gray-500">Loading database info...</span>
        </div>
      {:else if infoError}
        <div class="p-4">
          <div class="bg-red-100/20 dark:bg-red-900/20 border border-red-300/50 dark:border-red-800/50 rounded-lg p-3 text-sm text-red-700 dark:text-red-300">{infoError}</div>
        </div>
      {:else}
        <div class="p-4 space-y-4">
          <div class="grid grid-cols-2 xl:grid-cols-4 gap-3">
            {#each metrics as m}
              {@const Icon = m.icon}
              <div class="bg-gray-50 dark:bg-gray-900 border border-gray-200 dark:border-gray-800 rounded-xl p-4">
                <div class="flex items-center gap-2 mb-2">
                  <Icon size={14} class={m.color} />
                  <span class="text-xs text-gray-500 uppercase tracking-wider">{m.label}</span>
                </div>
                <div class="text-xl font-semibold text-gray-800 dark:text-gray-200 truncate">{m.value}</div>
              </div>
            {/each}
          </div>

          <div class="bg-gray-50 dark:bg-gray-900 border border-gray-200 dark:border-gray-800 rounded-xl p-4">
            <h3 class="text-xs text-gray-500 uppercase tracking-wider mb-3">Details</h3>
            <div class="grid grid-cols-[140px_1fr] gap-y-2 gap-x-3 text-sm">
              <span class="text-gray-500">Name</span>
              <code class="font-mono text-gray-800 dark:text-gray-200 break-all">{dbInfo.name ?? tab.database}</code>

              <span class="text-gray-500">Engine</span>
              <span class="text-gray-800 dark:text-gray-200">{dbInfo.engine ?? '—'}</span>

              <span class="text-gray-500">Data Path</span>
              <span class="flex items-center gap-2 text-gray-800 dark:text-gray-200 break-all">
                <FolderOpen size={13} class="text-gray-500 shrink-0" />
                <code class="font-mono text-xs">{dbInfo.data_path ?? '—'}</code>
              </span>

              <span class="text-gray-500">Metadata Path</span>
              <code class="font-mono text-xs text-gray-800 dark:text-gray-200 break-all">{dbInfo.metadata_path ?? '—'}</code>

              <span class="text-gray-500">Last Modified</span>
              <span class="flex items-center gap-2 text-gray-800 dark:text-gray-200">
                <Clock size={13} class="text-gray-500 shrink-0" />
                {formatDate(dbInfo.last_modified)}
              </span>
            </div>
          </div>
        </div>
      {/if}
    {:else if activeSubTab === 'tables'}
      {#if tablesLoading}
        <div class="flex items-center justify-center py-12 gap-2">
          <Spinner size="sm" />
          <span class="text-sm text-gray-500">Loading tables...</span>
        </div>
      {:else if tablesError}
        <div class="p-4">
          <div class="bg-red-100/20 dark:bg-red-900/20 border border-red-300/50 dark:border-red-800/50 rounded-lg p-3 text-sm text-red-700 dark:text-red-300">{tablesError}</div>
        </div>
      {:else if tablesMeta.length > 0}
        <VirtualTable meta={tablesMeta} data={tablesData} />
      {:else}
        <div class="flex items-center justify-center py-12 text-gray-400 dark:text-gray-600 text-sm">No tables found</div>
      {/if}
    {/if}
  </div>
</div>
