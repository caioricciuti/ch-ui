<script lang="ts">
  import type { ColumnMeta } from '../../types/query'
  import { getDisplayType, type DisplayType } from '../../utils/ch-types'
  import DataTable, { type DataColumn } from '../common/DataTable.svelte'
  import Badge from '../common/Badge.svelte'

  interface Props {
    meta: ColumnMeta[]
  }

  let { meta }: Props = $props()

  type SchemaRow = { position: number; name: string; type: string; category: DisplayType }

  const tones: Record<DisplayType, 'neutral' | 'success' | 'warning' | 'info' | 'brand'> = {
    number: 'brand',
    string: 'success',
    date: 'info',
    bool: 'warning',
    json: 'brand',
    null: 'neutral',
    unknown: 'neutral',
  }

  const columns: DataColumn<SchemaRow>[] = [
    { key: 'position', label: '#', align: 'right', width: '56px' },
    { key: 'name', label: 'Column name', mono: true },
    { key: 'type', label: 'ClickHouse type', mono: true },
    { key: 'category', label: 'Category' },
  ]

  const rows = $derived<SchemaRow[]>(
    meta.map((col, i) => ({ position: i + 1, name: col.name, type: col.type, category: getDisplayType(col.type) })),
  )
</script>

<div class="flex-1 min-h-0">
  <DataTable {columns} {rows} rowKey={(r) => r.position} fill emptyTitle="No columns">
    {#snippet cell(row, col, value)}
      {#if col.key === 'category'}
        <Badge tone={tones[row.category]}>{value}</Badge>
      {:else}
        {value}
      {/if}
    {/snippet}
  </DataTable>
</div>
