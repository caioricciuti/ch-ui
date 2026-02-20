<script lang="ts">
  import type { ColumnMeta, QueryPlanNode, QueryStats } from '../../types/query'
  import VirtualTable from '../table/VirtualTable.svelte'
  import Spinner from '../common/Spinner.svelte'
  import ResultFooter from './ResultFooter.svelte'
  import StatsPanel from './StatsPanel.svelte'
  import SchemaPanel from './SchemaPanel.svelte'
  import InsightsPanel from './InsightsPanel.svelte'
  import { computeColumnStats } from '../../utils/stats'
  import { SquareTerminal, AlertTriangle } from 'lucide-svelte'

  type Tab = 'data' | 'stats' | 'schema' | 'insights'

  interface Props {
    meta: ColumnMeta[]
    data: unknown[][]
    loading?: boolean
    error?: string | null
    stats?: QueryStats | null
    elapsedMs?: number
    running?: boolean
    streamRows?: number
    streamChunks?: number
    streamStartedAt?: number | null
    streamLastChunkAt?: number | null
    planNodes?: QueryPlanNode[]
    planLines?: string[]
    planSource?: string
    planLoading?: boolean
    planError?: string | null
    onLoadPlan?: () => void
    onSample?: (perShard: number) => void
    profile?: Record<string, unknown> | null
    profileAvailable?: boolean
    profileReason?: string | null
    profileLoading?: boolean
    profileError?: string | null
    samplingMode?: string | null
  }

  let {
    meta,
    data,
    loading = false,
    error = null,
    stats = null,
    elapsedMs = 0,
    running = false,
    streamRows = 0,
    streamChunks = 0,
    streamStartedAt = null,
    streamLastChunkAt = null,
    planNodes = [],
    planLines = [],
    planSource = '',
    planLoading = false,
    planError = null,
    onLoadPlan,
    onSample,
    profile = null,
    profileAvailable = false,
    profileReason = null,
    profileLoading = false,
    profileError = null,
    samplingMode = null,
  }: Props = $props()

  let activeTab = $state<Tab>('data')

  // Reset to Data tab when query results change
  $effect(() => {
    meta; // track meta changes
    activeTab = 'data'
  })

  const columnStats = $derived.by(() => {
    if (meta.length === 0 || data.length === 0) return []
    return computeColumnStats(meta, data)
  })
</script>

<div class="flex flex-col flex-1 min-h-0">
  {#if loading}
    <div class="flex flex-col items-center justify-center flex-1 gap-2 text-gray-500">
      <Spinner size="sm" />
      <span class="text-sm">Executing query...</span>
    </div>
  {:else if error}
    <div class="flex-1 p-4">
      <div class="flex items-start gap-3 bg-red-100/20 dark:bg-red-900/20 border border-red-300/50 dark:border-red-800/50 rounded-lg p-3.5">
        <AlertTriangle size={16} class="text-red-400 shrink-0 mt-0.5" />
        <div class="text-sm text-red-700 dark:text-red-300 whitespace-pre-wrap break-words">{error}</div>
      </div>
    </div>
  {:else if meta.length === 0}
    <div class="flex flex-col items-center justify-center flex-1 gap-2 text-gray-400 dark:text-gray-600">
      <SquareTerminal size={28} class="text-gray-300 dark:text-gray-700" />
      <p class="text-sm">Run a query to see results</p>
      <p class="text-xs text-gray-300 dark:text-gray-700">Cmd/Ctrl+Enter to execute</p>
    </div>
  {:else}
    <!-- Tab content -->
    {#if activeTab === 'data'}
      <VirtualTable {meta} {data} />
    {:else if activeTab === 'stats'}
      <StatsPanel stats={columnStats} />
    {:else if activeTab === 'insights'}
      <InsightsPanel
        {meta}
        {data}
        {stats}
        {elapsedMs}
        {running}
        {streamRows}
        {streamChunks}
        {streamStartedAt}
        {streamLastChunkAt}
        {planNodes}
        {planLines}
        {planSource}
        {planLoading}
        {planError}
        {onLoadPlan}
        {onSample}
        {profile}
        {profileAvailable}
        {profileReason}
        {profileLoading}
        {profileError}
        {samplingMode}
      />
    {:else}
      <SchemaPanel {meta} />
    {/if}

    <ResultFooter
      {activeTab}
      onTabChange={(tab) => activeTab = tab}
      {meta}
      {data}
      {stats}
      {elapsedMs}
      {streamRows}
      {streamChunks}
    />
  {/if}
</div>
