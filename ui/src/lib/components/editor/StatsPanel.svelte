<script lang="ts">
  import type { ColumnStats } from '../../utils/stats'
  import { formatNumber } from '../../utils/format'
  import DataTable, { type DataColumn } from '../common/DataTable.svelte'

  interface Props {
    stats: ColumnStats[]
  }

  let { stats }: Props = $props()

  const DASH = '\u2014'

  // One row per result column. Min, max and avg mean value for numbers,
  // length for strings and (min/max only) time for dates.
  type StatRow = {
    position: number
    name: string
    displayType: string
    count: number
    nulls: number
    nullPct: number
    min: string
    max: string
    avg: string
    minSort: number | string
    maxSort: number | string
    avgSort: number
    sum: number | undefined
    distinct: number | undefined
  }

  function fmt(v: number | undefined): string {
    if (v === undefined) return DASH
    if (Number.isInteger(v)) return formatNumber(v)
    return v.toLocaleString(undefined, { maximumFractionDigits: 2 })
  }

  function len(v: number | undefined): string {
    return v !== undefined ? fmt(v) + ' ch' : DASH
  }

  function toRow(col: ColumnStats, i: number): StatRow {
    const base = {
      position: i,
      name: col.name,
      displayType: col.displayType,
      count: col.count,
      nulls: col.nulls,
      nullPct: col.nullPct,
      sum: col.sum,
      distinct: col.distinct,
    }
    if (col.displayType === 'number') {
      return { ...base, min: fmt(col.min), max: fmt(col.max), avg: fmt(col.avg), minSort: col.min ?? -Infinity, maxSort: col.max ?? -Infinity, avgSort: col.avg ?? -Infinity }
    }
    if (col.displayType === 'string') {
      return { ...base, min: len(col.minLen), max: len(col.maxLen), avg: len(col.avgLen), minSort: col.minLen ?? -1, maxSort: col.maxLen ?? -1, avgSort: col.avgLen ?? -1 }
    }
    if (col.displayType === 'date') {
      return { ...base, min: col.earliest ?? DASH, max: col.latest ?? DASH, avg: DASH, minSort: col.earliest ?? '', maxSort: col.latest ?? '', avgSort: -Infinity }
    }
    return { ...base, min: DASH, max: DASH, avg: DASH, minSort: '', maxSort: '', avgSort: -Infinity }
  }

  const optionalNumber = (v: unknown): string => (typeof v === 'number' ? formatNumber(v) : DASH)

  const columns: DataColumn<StatRow>[] = [
    { key: 'name', label: 'Name', mono: true },
    { key: 'displayType', label: 'Type' },
    { key: 'count', label: 'Count', align: 'right', format: (v) => formatNumber(Number(v)) },
    { key: 'nulls', label: 'Nulls', align: 'right', format: (v) => formatNumber(Number(v)) },
    { key: 'nullPct', label: 'Null%', align: 'right', format: (v) => Number(v).toFixed(1) + '%' },
    { key: 'min', label: 'Min', align: 'right', sortValue: (r) => r.minSort },
    { key: 'max', label: 'Max', align: 'right', sortValue: (r) => r.maxSort },
    { key: 'avg', label: 'Avg', align: 'right', sortValue: (r) => r.avgSort },
    { key: 'sum', label: 'Sum', align: 'right', format: (v) => (typeof v === 'number' ? fmt(v) : DASH), sortValue: (r) => r.sum ?? -Infinity },
    { key: 'distinct', label: 'Distinct', align: 'right', format: optionalNumber, sortValue: (r) => r.distinct ?? -1 },
  ]

  const rows = $derived<StatRow[]>(stats.map(toRow))
</script>

<div class="flex-1 min-h-0">
  <DataTable {columns} {rows} rowKey={(r) => r.position} fill emptyTitle="No columns" />
</div>
