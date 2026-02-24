<script lang="ts">
  import type { BrainArtifact } from '../../types/brain'
  import type { PanelConfig } from '../../types/api'
  import { isDateType, isNumericType } from '../../utils/chart-transform'
  import ChartPanel from '../dashboard/ChartPanel.svelte'
  import { ChevronRight, BarChart3, Table } from 'lucide-svelte'

  interface Props {
    artifact: BrainArtifact
  }

  let { artifact }: Props = $props()

  let expanded = $state(false)
  let viewMode = $state<'table' | 'chart'>('chart')

  function parsePayload(): any | null {
    if (artifact.type !== 'query_result') return null
    try {
      return JSON.parse(artifact.content)
    } catch {
      return null
    }
  }

  function getRows(): Record<string, any>[] {
    const payload = parsePayload()
    if (!payload || !Array.isArray(payload.data)) return []
    return payload.data
  }

  function getColumns(): string[] {
    const payload = parsePayload()
    if (payload?.meta && Array.isArray(payload.meta) && payload.meta.length > 0) {
      return payload.meta.map((m: any) => String(m?.name ?? '')).filter(Boolean)
    }
    const rows = getRows()
    if (rows.length === 0) return []
    return Object.keys(rows[0])
  }

  const payload = $derived(parsePayload())
  const rows = $derived(getRows())
  const cols = $derived(getColumns())
  const elapsed = $derived(payload?.statistics?.elapsed)

  /** Auto-detect chart configuration from column types */
  const chartConfig = $derived.by<PanelConfig | null>(() => {
    if (!payload?.meta || rows.length < 2) return null
    const meta = payload.meta as { name: string; type: string }[]
    const dateCol = meta.find(m => isDateType(m.type))
    const numericCols = meta.filter(m => isNumericType(m.type))
    const xColumn = dateCol?.name ?? meta[0]?.name
    const yColumns = numericCols.filter(m => m.name !== xColumn).map(m => m.name)
    if (!xColumn || yColumns.length === 0) return null
    return {
      chartType: dateCol ? 'timeseries' : 'bar',
      xColumn,
      yColumns: yColumns.slice(0, 5),
    }
  })
</script>

<div class="mt-2 rounded-lg border border-gray-200 dark:border-gray-700 bg-gray-50/70 dark:bg-gray-900/50 overflow-hidden">
  <div class="flex items-center gap-2 px-3 py-2">
    <button
      class="flex items-center gap-2 flex-1 text-left hover:bg-gray-100 dark:hover:bg-gray-800/50 -mx-1 px-1 rounded transition-colors"
      onclick={() => expanded = !expanded}
    >
      <ChevronRight size={14} class="text-gray-400 transition-transform {expanded ? 'rotate-90' : ''}" />
      <span class="text-xs font-semibold text-gray-800 dark:text-gray-200 truncate">{artifact.title}</span>
      {#if rows.length > 0}
        <span class="ds-badge ds-badge-neutral">{rows.length} rows</span>
      {/if}
      {#if elapsed}
        <span class="text-[11px] text-gray-500">{elapsed}s</span>
      {/if}
    </button>

    {#if chartConfig && expanded}
      <div class="flex items-center rounded-md border border-gray-200 dark:border-gray-700 overflow-hidden shrink-0">
        <button
          class="p-1 transition-colors {viewMode === 'chart' ? 'bg-ch-blue/10 text-ch-blue' : 'text-gray-400 hover:text-gray-600 dark:hover:text-gray-300'}"
          onclick={() => viewMode = 'chart'}
          title="Chart view"
        >
          <BarChart3 size={13} />
        </button>
        <button
          class="p-1 transition-colors border-l border-gray-200 dark:border-gray-700 {viewMode === 'table' ? 'bg-ch-blue/10 text-ch-blue' : 'text-gray-400 hover:text-gray-600 dark:hover:text-gray-300'}"
          onclick={() => viewMode = 'table'}
          title="Table view"
        >
          <Table size={13} />
        </button>
      </div>
    {/if}
  </div>

  {#if expanded}
    {#if artifact.type === 'query_result' && payload}
      {#if viewMode === 'chart' && chartConfig}
        <div class="border-t border-gray-200 dark:border-gray-700 h-[220px]">
          <ChartPanel data={rows} meta={payload.meta} config={chartConfig} />
        </div>
      {:else if cols.length > 0}
        <div class="border-t border-gray-200 dark:border-gray-700 max-h-[240px] overflow-auto">
          <table class="min-w-full text-[11px] font-mono">
            <thead class="bg-gray-100 dark:bg-gray-800 sticky top-0">
              <tr>
                {#each cols as col}
                  <th class="px-2 py-1 text-left text-gray-600 dark:text-gray-300 border-b border-gray-200 dark:border-gray-700 whitespace-nowrap">{col}</th>
                {/each}
              </tr>
            </thead>
            <tbody>
              {#each rows as row}
                <tr class="odd:bg-white/70 even:bg-gray-50/70 dark:odd:bg-gray-900/30 dark:even:bg-gray-800/30">
                  {#each cols as col}
                    <td class="px-2 py-1 border-b border-gray-200/70 dark:border-gray-700/70 align-top whitespace-nowrap">{String(row[col] ?? '')}</td>
                  {/each}
                </tr>
              {/each}
            </tbody>
          </table>
        </div>
      {/if}

      {#if payload?.query}
        <details class="border-t border-gray-200 dark:border-gray-700 px-3 py-2">
          <summary class="text-[11px] text-ch-blue cursor-pointer">View query</summary>
          <pre class="mt-1 text-[11px] whitespace-pre-wrap bg-gray-100 dark:bg-gray-800 rounded p-2 max-h-40 overflow-auto">{payload.query}</pre>
        </details>
      {/if}

      <details class="border-t border-gray-200 dark:border-gray-700 px-3 py-2">
        <summary class="text-[11px] text-ch-blue cursor-pointer">View raw payload</summary>
        <pre class="mt-1 text-[11px] whitespace-pre-wrap bg-gray-100 dark:bg-gray-800 rounded p-2 max-h-52 overflow-auto">{artifact.content}</pre>
      </details>
    {:else}
      <details class="border-t border-gray-200 dark:border-gray-700 px-3 py-2">
        <summary class="text-[11px] text-ch-blue cursor-pointer">View payload</summary>
        <pre class="mt-1 text-[11px] whitespace-pre-wrap bg-gray-100 dark:bg-gray-800 rounded p-2 max-h-52 overflow-auto">{artifact.content}</pre>
      </details>
    {/if}
  {/if}
</div>
