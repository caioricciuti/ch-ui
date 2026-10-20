<script lang="ts">
  import { errorText } from '../../../utils/errors'
  import { onMount } from 'svelte'
  import type { DatabaseTab } from '../../../stores/tabs.svelte'
  import type { ColumnMeta } from '../../../types/query'
  import { fetchDatabaseInfo, fetchDatabaseTables } from '../../../api/query'
  import { formatBytes, formatDate, formatNumber } from '../../../utils/format'
  import Skeleton from '../../common/Skeleton.svelte'
  import EmptyState from '../../common/EmptyState.svelte'
  import PageHeader from '../../common/PageHeader.svelte'
  import Tabs from '../../common/Tabs.svelte'
  import Stat from '../../common/Stat.svelte'
  import Panel from '../../common/Panel.svelte'
  import Button from '../../common/Button.svelte'
  import VirtualTable from '../../table/VirtualTable.svelte'
  import { RefreshCw } from 'lucide-svelte'

  interface Props {
    tab: DatabaseTab
  }

  let { tab }: Props = $props()

  type SubTab = 'overview' | 'tables'
  let activeSubTab = $state<SubTab>('overview')

  let dbInfo = $state<Record<string, unknown>>({})
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
      { label: 'Tables', value: formatNumber(Number(dbInfo.table_count ?? 0)), size: 'lg' as const },
      { label: 'Rows', value: formatNumber(Number(dbInfo.total_rows ?? 0)), size: 'lg' as const },
      { label: 'Total Size', value: formatBytes(Number(dbInfo.total_bytes ?? 0)), size: 'lg' as const },
      { label: 'Engine', value: String(dbInfo.engine ?? '—'), size: 'md' as const },
    ]
  })

  async function loadInfo() {
    infoLoading = true
    infoError = null
    try {
      dbInfo = await fetchDatabaseInfo(tab.database)
    } catch (e) {
      infoError = errorText(e)
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
      tablesData = (res.data ?? []).map((row: unknown) => {
        if (Array.isArray(row)) return row as unknown[]
        const record = (row ?? {}) as Record<string, unknown>
        return (res.meta ?? []).map((col) => record[col.name])
      })
      tablesLoaded = true
    } catch (e) {
      tablesError = errorText(e)
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
  <PageHeader title={tab.database} subtitle="Database">
    {#snippet actions()}
      <Button
        icon
        size="sm"
        variant="ghost"
        onclick={refresh}
        title="Refresh database info"
        aria-label="Refresh database info"
      >
        <RefreshCw size={13} class={infoLoading || tablesLoading ? 'animate-spin' : ''} />
      </Button>
    {/snippet}
  </PageHeader>

  <Tabs items={subTabs} value={activeSubTab} onchange={(id) => switchTab(id as SubTab)} class="shrink-0 px-3" />

  <div class="flex-1 min-h-0 overflow-auto">
    {#if activeSubTab === 'overview'}
      {#if infoLoading}
        <div class="p-4">
          <Skeleton variant="block" label="Loading database info" />
        </div>
      {:else if infoError}
        <div class="p-4">
          <div class="rounded-md bg-danger-soft p-3 text-[13px] text-fg">{infoError}</div>
        </div>
      {:else}
        <div class="p-4 space-y-4">
          <div class="grid grid-cols-2 xl:grid-cols-4 gap-3">
            {#each metrics as m (m.label)}
              <Stat label={m.label} value={m.value} size={m.size} />
            {/each}
          </div>

          <Panel title="Details">
            <div class="grid grid-cols-[140px_1fr] gap-y-2 gap-x-3 text-[13px]">
              <span class="text-fg-3">Name</span>
              <code class="font-mono text-fg break-all">{dbInfo.name ?? tab.database}</code>

              <span class="text-fg-3">Engine</span>
              <span class="text-fg">{dbInfo.engine ?? '—'}</span>

              <span class="text-fg-3">Data Path</span>
              <code class="font-mono text-xs text-fg break-all">{dbInfo.data_path ?? '—'}</code>

              <span class="text-fg-3">Metadata Path</span>
              <code class="font-mono text-xs text-fg break-all">{dbInfo.metadata_path ?? '—'}</code>

              <span class="text-fg-3">Last Modified</span>
              <span class="text-fg">{formatDate(dbInfo.last_modified)}</span>
            </div>
          </Panel>
        </div>
      {/if}
    {:else if activeSubTab === 'tables'}
      {#if tablesLoading}
        <Skeleton variant="rows" rows={6} label="Loading tables" />
      {:else if tablesError}
        <div class="p-4">
          <div class="rounded-md bg-danger-soft p-3 text-[13px] text-fg">{tablesError}</div>
        </div>
      {:else if tablesMeta.length > 0}
        <VirtualTable meta={tablesMeta} data={tablesData} />
      {:else}
        <EmptyState size="compact" title="No tables" description="This database has no tables yet." />
      {/if}
    {/if}
  </div>
</div>
