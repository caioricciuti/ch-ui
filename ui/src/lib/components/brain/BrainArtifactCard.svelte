<script lang="ts">
  import type { BrainArtifact } from '../../types/brain'
  import type { PanelConfig } from '../../types/api'
  import { isDateType, isNumericType } from '../../utils/chart-transform'
  import ChartPanel from '../dashboard/ChartPanel.svelte'
  import Badge from '../common/Badge.svelte'
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

<div class="mt-2 rounded-lg border border-edge-subtle bg-surface overflow-hidden">
  <div class="flex items-center gap-2 px-3 py-2">
    <button
      class="flex items-center gap-2 flex-1 text-left hover:bg-hover -mx-1 px-1 rounded transition-colors"
      onclick={() => expanded = !expanded}
    >
      <ChevronRight size={14} class="text-fg-4 transition-transform {expanded ? 'rotate-90' : ''}" />
      <span class="text-xs font-semibold text-fg truncate">{artifact.title}</span>
      {#if rows.length > 0}
        <Badge tone="neutral">{rows.length} rows</Badge>
      {/if}
      {#if elapsed}
        <span class="text-[11px] text-fg-3">{elapsed}s</span>
      {/if}
    </button>

    {#if chartConfig && expanded}
      <div class="flex items-center rounded-md border border-edge-subtle overflow-hidden shrink-0">
        <button
          class="p-1 transition-colors {viewMode === 'chart' ? 'bg-accent-soft text-accent' : 'text-fg-4 hover:text-fg'}"
          onclick={() => viewMode = 'chart'}
          title="Chart view"
        >
          <BarChart3 size={13} />
        </button>
        <button
          class="p-1 transition-colors border-l border-edge-subtle {viewMode === 'table' ? 'bg-accent-soft text-accent' : 'text-fg-4 hover:text-fg'}"
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
        <div class="border-t border-edge-subtle h-[220px]">
          <ChartPanel data={rows} meta={payload.meta} config={chartConfig} />
        </div>
      {:else if cols.length > 0}
        <div class="border-t border-edge-subtle max-h-[240px] overflow-auto">
          <table class="min-w-full text-[11px] font-mono">
            <thead class="bg-surface-2 sticky top-0">
              <tr>
                {#each cols as col}
                  <th class="px-2 py-1 text-left text-fg-2 border-b border-edge-subtle whitespace-nowrap">{col}</th>
                {/each}
              </tr>
            </thead>
            <tbody>
              {#each rows as row}
                <tr class="odd:bg-surface even:bg-surface-2">
                  {#each cols as col}
                    <td class="px-2 py-1 border-b border-edge-subtle align-top whitespace-nowrap">{String(row[col] ?? '')}</td>
                  {/each}
                </tr>
              {/each}
            </tbody>
          </table>
        </div>
      {/if}

      {#if payload?.query}
        <details class="border-t border-edge-subtle px-3 py-2">
          <summary class="text-[11px] text-accent cursor-pointer">View query</summary>
          <pre class="mt-1 text-[11px] whitespace-pre-wrap bg-surface-2 rounded p-2 max-h-40 overflow-auto">{payload.query}</pre>
        </details>
      {/if}

      <details class="border-t border-edge-subtle px-3 py-2">
        <summary class="text-[11px] text-accent cursor-pointer">View raw payload</summary>
        <pre class="mt-1 text-[11px] whitespace-pre-wrap bg-surface-2 rounded p-2 max-h-52 overflow-auto">{artifact.content}</pre>
      </details>
    {:else}
      <details class="border-t border-edge-subtle px-3 py-2">
        <summary class="text-[11px] text-accent cursor-pointer">View payload</summary>
        <pre class="mt-1 text-[11px] whitespace-pre-wrap bg-surface-2 rounded p-2 max-h-52 overflow-auto">{artifact.content}</pre>
      </details>
    {/if}
  {/if}
</div>
