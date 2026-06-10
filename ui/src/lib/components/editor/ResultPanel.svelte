<script lang="ts">
  import type { ColumnMeta, QueryPlanNode, QueryStats, QueryEstimateResult } from '../../types/query'
  import type { ColumnFilter, ResultSort } from '../../utils/result-filters'
  import { OPERATOR_LABELS, isUnaryOperator } from '../../utils/result-filters'
  import VirtualTable from '../table/VirtualTable.svelte'
  import Spinner from '../common/Spinner.svelte'
  import ResultFooter from './ResultFooter.svelte'
  import StatsPanel from './StatsPanel.svelte'
  import SchemaPanel from './SchemaPanel.svelte'
  import InsightsPanel from './InsightsPanel.svelte'
  import { computeColumnStats } from '../../utils/stats'
  import { parseCHError } from '../../utils/ch-error'
  import { SquareTerminal, AlertTriangle, X, Filter, Lightbulb, Crosshair } from 'lucide-svelte'

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
    estimate?: QueryEstimateResult | null
    /** Unfiltered row count of the base result (for "X of Y rows"). */
    totalRows?: number | null
    sort?: ResultSort | null
    filters?: ColumnFilter[]
    serverApplied?: boolean
    /** Result was truncated and can't be re-queried — view is partial. */
    partialView?: boolean
    onsort?: (column: string) => void
    onfilterchange?: (column: string, filter: ColumnFilter | null) => void
    onclearfilters?: () => void
    /** Present only when the error position can be mapped into the editor. */
    ongotoerror?: () => void
    /** Document-relative target for the button label (ClickHouse's own
     * line/col are relative to the executed fragment). */
    errorTargetLine?: number | null
    errorTargetCol?: number | null
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
    estimate = null,
    totalRows = null,
    sort = null,
    filters = [],
    serverApplied = false,
    partialView = false,
    onsort,
    onfilterchange,
    onclearfilters,
    ongotoerror,
    errorTargetLine = null,
    errorTargetCol = null,
  }: Props = $props()

  const parsedError = $derived(error ? parseCHError(error) : null)

  function filterChipLabel(f: ColumnFilter): string {
    const op = OPERATOR_LABELS[f.operator]
    return isUnaryOperator(f.operator) ? `${f.column} ${op}` : `${f.column} ${op} ${f.value}`
  }

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
  <!-- Active filters stay visible in every state (including errors from a
       server-side re-query) so a failing filter can always be removed. -->
  {#if filters.length > 0 || partialView}
    <div class="flex items-center gap-1.5 flex-wrap px-2 py-1 border-b border-gray-200 dark:border-gray-800 bg-gray-50/70 dark:bg-gray-900/40 shrink-0">
      {#each filters as f (f.column)}
        <span class="inline-flex items-center gap-1 pl-1.5 pr-0.5 py-0.5 text-[11px] rounded-md border border-orange-200/70 dark:border-orange-500/25 bg-orange-100/60 dark:bg-orange-500/10 text-orange-800 dark:text-orange-300">
          <Filter size={10} class="shrink-0" />
          <span class="font-mono max-w-72 truncate" title={filterChipLabel(f)}>{filterChipLabel(f)}</span>
          <button
            class="p-0.5 rounded hover:bg-orange-200/70 dark:hover:bg-orange-500/20"
            onclick={() => onfilterchange?.(f.column, null)}
            title={`Remove filter on ${f.column}`}
            aria-label={`Remove filter on ${f.column}`}
          >
            <X size={10} />
          </button>
        </span>
      {/each}
      {#if filters.length > 1}
        <button
          class="px-1.5 py-0.5 text-[11px] text-gray-500 dark:text-gray-400 hover:text-gray-800 dark:hover:text-gray-200 rounded hover:bg-gray-200 dark:hover:bg-gray-800"
          onclick={() => onclearfilters?.()}
        >Clear all</button>
      {/if}
      {#if partialView}
        <span class="flex items-center gap-1 text-[11px] text-amber-600 dark:text-amber-400" title="The result hit the row limit and this query can't be re-run as a subquery, so sorting and filtering only apply to the loaded rows.">
          <AlertTriangle size={11} />
          partial view — only loaded rows are sorted/filtered
        </span>
      {/if}
    </div>
  {/if}

  {#if loading}
    <div class="flex flex-col items-center justify-center flex-1 gap-2 text-gray-500">
      <Spinner size="sm" />
      <span class="text-sm">Executing query...</span>
    </div>
  {:else if error && parsedError}
    <div class="flex-1 p-4 overflow-auto">
      <div class="flex items-start gap-3 bg-red-100/20 dark:bg-red-900/20 border border-red-300/50 dark:border-red-800/50 rounded-lg p-3.5">
        <AlertTriangle size={16} class="text-red-400 shrink-0 mt-0.5" />
        <div class="min-w-0 flex-1">
          {#if parsedError.name !== null || parsedError.code !== null}
            <div class="flex items-center gap-2 mb-1.5 flex-wrap">
              <span class="inline-flex items-center rounded px-1.5 py-0.5 text-[10px] font-semibold font-mono border border-red-300/60 dark:border-red-700/60 bg-red-100/60 dark:bg-red-900/40 text-red-700 dark:text-red-300">
                {parsedError.name ?? 'ERROR'}{parsedError.code !== null ? ` · ${parsedError.code}` : ''}
              </span>
              {#if ongotoerror && parsedError.position !== null}
                <button
                  class="inline-flex items-center gap-1 px-1.5 py-0.5 text-[11px] rounded border border-red-300/60 dark:border-red-700/60 text-red-700 dark:text-red-300 hover:bg-red-100/60 dark:hover:bg-red-900/40 transition-colors"
                  onclick={ongotoerror}
                >
                  <Crosshair size={11} />
                  {errorTargetLine !== null ? `Go to line ${errorTargetLine}, col ${errorTargetCol}` : 'Go to error'}
                </button>
              {/if}
            </div>
          {/if}

          <div class="text-sm text-red-700 dark:text-red-300 whitespace-pre-wrap break-words">{parsedError.message}</div>

          {#if parsedError.hint}
            <p class="mt-2 flex items-start gap-1.5 text-xs text-amber-700 dark:text-amber-400">
              <Lightbulb size={12} class="shrink-0 mt-0.5" />
              {parsedError.hint}
            </p>
          {/if}

          {#if parsedError.message !== parsedError.raw}
            <details class="mt-2">
              <summary class="text-xs text-red-500/80 dark:text-red-400/70 cursor-pointer select-none hover:text-red-600 dark:hover:text-red-300">Full error</summary>
              <pre class="mt-1 text-[11px] text-red-600/90 dark:text-red-300/80 whitespace-pre-wrap break-words font-mono">{parsedError.raw}</pre>
            </details>
          {/if}
        </div>
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
      <VirtualTable {meta} {data} sortColumn={sort?.column ?? ''} sortDir={sort?.dir ?? 'asc'} {onsort} {filters} {onfilterchange} />
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
        {estimate}
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
      {totalRows}
      {serverApplied}
    />
  {/if}
</div>
