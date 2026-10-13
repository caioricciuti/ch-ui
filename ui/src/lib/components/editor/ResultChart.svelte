<script lang="ts" module>
  import type { PanelConfig } from '../../types/api'

  // Chart settings per editor tab, kept across result switches and remounts.
  // Reset when the result's column names or types change.
  const savedConfigs = new Map<string, { signature: string; config: PanelConfig }>()
</script>

<script lang="ts">
  import { untrack } from 'svelte'
  import type { ColumnMeta } from '../../types/query'
  import { suggestChart, isNumericType } from '../../utils/chart-transform'
  import { detectQueryParams } from '../../utils/query-params'
  import { formatNumber } from '../../utils/format'
  import { getSession } from '../../stores/session.svelte'
  import PanelRenderer from '../dashboard/PanelRenderer.svelte'
  import ChartConfigFields from '../dashboard/ChartConfigFields.svelte'
  import EmptyState from '../common/EmptyState.svelte'
  import Button from '../common/Button.svelte'
  import AddToDashboardSheet from './AddToDashboardSheet.svelte'
  import { ChartLine, LayoutDashboard } from 'lucide-svelte'

  interface Props {
    meta: ColumnMeta[]
    /** Rows as shown in the Data view, client filters applied. */
    data: unknown[][]
    /** The statement that produced the result. */
    sql?: string
    connectionId?: string
    /** Key for the per-tab chart settings (the editor tab id). */
    configKey?: string
    /** Client-side filters hide rows of the result. */
    filtered?: boolean
  }

  let { meta, data, sql = '', connectionId = '', configKey = '', filtered = false }: Props = $props()

  const MAX_CHART_ROWS = 10_000

  const signature = $derived(meta.map((m) => `${m.name}\u0000${m.type}`).join('\u0001'))

  function initialConfig(): PanelConfig {
    const saved = configKey ? savedConfigs.get(configKey) : undefined
    if (saved && saved.signature === signature) return saved.config
    return suggestChart(meta, data.length)
  }

  let config = $state<PanelConfig>(untrack(initialConfig))
  let configSignature = untrack(() => signature)

  // New columns: start over from a suggestion.
  $effect(() => {
    const sig = signature
    if (sig === configSignature) return
    configSignature = sig
    config = untrack(() => suggestChart(meta, data.length))
  })

  $effect(() => {
    const snapshot = $state.snapshot(config)
    if (configKey) savedConfigs.set(configKey, { signature: untrack(() => signature), config: snapshot })
  })

  const capped = $derived(data.length > MAX_CHART_ROWS)
  const rows = $derived.by(() => {
    const shown = capped ? data.slice(0, MAX_CHART_ROWS) : data
    return shown.map((row) => {
      const obj: Record<string, unknown> = {}
      for (let i = 0; i < meta.length; i++) obj[meta[i].name] = row[i]
      return obj
    })
  })

  const hasNumeric = $derived(meta.some((m) => isNumericType(m.type)))
  const canWrite = $derived(getSession()?.role !== 'viewer')
  const hasParams = $derived(detectQueryParams(sql).length > 0)
  const canAdd = $derived(sql.trim() !== '' && !hasParams)

  let sheetOpen = $state(false)
</script>

<div class="flex min-h-0 flex-1">
  <div class="flex min-w-0 flex-1 flex-col">
    {#if canWrite || capped}
      <div class="flex shrink-0 items-center gap-3 border-b border-edge-subtle px-3 py-1.5 text-xs text-fg-3">
        <span class="min-w-0 flex-1 truncate">
          {#if capped}
            Charting the first {formatNumber(MAX_CHART_ROWS)} of {formatNumber(data.length)} rows.
          {/if}
        </span>
        {#if canWrite}
          {#if hasParams}
            <span class="truncate text-fg-4">Queries with bind parameters cannot go on a dashboard.</span>
          {/if}
          <Button
            variant="secondary"
            size="xs"
            disabled={!canAdd || data.length === 0}
            title={hasParams ? 'Dashboards have no bind parameters' : undefined}
            onclick={() => (sheetOpen = true)}
          >
            <LayoutDashboard size={12} />
            Add to dashboard
          </Button>
        {/if}
      </div>
    {/if}

    <div class="min-h-0 flex-1 overflow-hidden p-3">
      {#if data.length === 0}
        <EmptyState icon={ChartLine} size="compact" title="Nothing to chart" description="The result has no rows." />
      {:else if config.chartType === 'table' && !hasNumeric}
        <EmptyState icon={ChartLine} size="compact" title="No numeric column" description="Charts need at least one numeric column. Add an aggregate such as count() to the query." />
      {:else}
        <PanelRenderer type={config.chartType} data={rows} {meta} {config} />
      {/if}
    </div>
  </div>

  <aside class="flex w-64 shrink-0 flex-col gap-4 overflow-y-auto border-l border-edge-subtle bg-surface p-3">
    <ChartConfigFields bind:config {meta} compact />
  </aside>
</div>

{#if canWrite}
  <AddToDashboardSheet open={sheetOpen} onclose={() => (sheetOpen = false)} {sql} {connectionId} {config} {filtered} />
{/if}
