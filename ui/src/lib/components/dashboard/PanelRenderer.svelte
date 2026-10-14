<script lang="ts">
  import type { PanelConfig } from '../../types/api'
  import { computeStat, type ColumnMeta } from '../../utils/chart-transform'
  import VirtualTable from '../table/VirtualTable.svelte'
  import ChartPanel from './ChartPanel.svelte'
  import StatPanel from './StatPanel.svelte'
  import GaugePanel from './GaugePanel.svelte'
  import PiePanel from './PiePanel.svelte'
  import PanelTable from './PanelTable.svelte'
  import MarkdownPanel from './MarkdownPanel.svelte'

  interface Props {
    /** Panel type; unknown types render as a table. */
    type: string
    data: Record<string, unknown>[]
    meta: ColumnMeta[]
    config: PanelConfig
    /** compact: a light table for grid cells. virtual: every row, virtualized. */
    table?: 'compact' | 'virtual'
  }

  let { type, data, meta, config, table = 'compact' }: Props = $props()

  const stat = $derived(type === 'stat' || type === 'gauge' ? computeStat(data, meta, config) : null)
  const positional = $derived(table === 'virtual' ? data.map(row => meta.map(col => row[col.name])) : [])
</script>

{#if type === 'text'}
  <MarkdownPanel content={config.content ?? ''} />
{:else if type === 'stat' && stat}
  <StatPanel {stat} />
{:else if type === 'gauge' && stat}
  <GaugePanel {stat} min={config.gaugeMin} max={config.gaugeMax} />
{:else if type === 'pie'}
  <PiePanel {data} {meta} {config} />
{:else if type === 'timeseries' || type === 'bar'}
  <ChartPanel {data} {meta} {config} />
{:else if table === 'virtual'}
  <VirtualTable {meta} data={positional} />
{:else}
  <PanelTable {data} {meta} />
{/if}
