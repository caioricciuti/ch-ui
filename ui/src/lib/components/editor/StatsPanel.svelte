<script lang="ts">
  import type { ColumnStats } from '../../utils/stats'
  import { formatNumber } from '../../utils/format'

  interface Props {
    stats: ColumnStats[]
  }

  let { stats }: Props = $props()

  function fmt(v: number | undefined): string {
    if (v === undefined) return '\u2014'
    if (Number.isInteger(v)) return formatNumber(v)
    return v.toLocaleString(undefined, { maximumFractionDigits: 2 })
  }

  function pct(v: number): string {
    return v.toFixed(1) + '%'
  }
</script>

<div class="flex-1 overflow-auto min-h-0">
  <table class="w-full text-sm border-collapse">
    <thead class="sticky top-0 z-10 bg-surface">
      <tr class="border-b border-edge">
        <th class="px-3 py-1.5 text-left text-xs font-medium uppercase tracking-wider text-fg-3">Name</th>
        <th class="px-3 py-1.5 text-left text-xs font-medium uppercase tracking-wider text-fg-3">Type</th>
        <th class="px-3 py-1.5 text-right text-xs font-medium uppercase tracking-wider text-fg-3">Count</th>
        <th class="px-3 py-1.5 text-right text-xs font-medium uppercase tracking-wider text-fg-3">Nulls</th>
        <th class="px-3 py-1.5 text-right text-xs font-medium uppercase tracking-wider text-fg-3">Null%</th>
        <th class="px-3 py-1.5 text-right text-xs font-medium uppercase tracking-wider text-fg-3">Min</th>
        <th class="px-3 py-1.5 text-right text-xs font-medium uppercase tracking-wider text-fg-3">Max</th>
        <th class="px-3 py-1.5 text-right text-xs font-medium uppercase tracking-wider text-fg-3">Avg</th>
        <th class="px-3 py-1.5 text-right text-xs font-medium uppercase tracking-wider text-fg-3">Sum</th>
        <th class="px-3 py-1.5 text-right text-xs font-medium uppercase tracking-wider text-fg-3">Distinct</th>
      </tr>
    </thead>
    <tbody>
      {#each stats as col, i}
        <tr class="border-b border-edge-subtle {i % 2 === 1 ? 'bg-surface ' : ''}">
          <td class="px-3 py-1.5 font-mono text-xs">{col.name}</td>
          <td class="px-3 py-1.5 text-fg-3 text-xs">{col.displayType}</td>
          <td class="px-3 py-1.5 text-right tabular-nums">{formatNumber(col.count)}</td>
          <td class="px-3 py-1.5 text-right tabular-nums">{formatNumber(col.nulls)}</td>
          <td class="px-3 py-1.5 text-right tabular-nums">{pct(col.nullPct)}</td>
          <td class="px-3 py-1.5 text-right tabular-nums">
            {#if col.displayType === 'number'}
              {fmt(col.min)}
            {:else if col.displayType === 'string'}
              {col.minLen !== undefined ? fmt(col.minLen) + ' ch' : '\u2014'}
            {:else if col.displayType === 'date'}
              {col.earliest ?? '\u2014'}
            {:else}
              {'\u2014'}
            {/if}
          </td>
          <td class="px-3 py-1.5 text-right tabular-nums">
            {#if col.displayType === 'number'}
              {fmt(col.max)}
            {:else if col.displayType === 'string'}
              {col.maxLen !== undefined ? fmt(col.maxLen) + ' ch' : '\u2014'}
            {:else if col.displayType === 'date'}
              {col.latest ?? '\u2014'}
            {:else}
              {'\u2014'}
            {/if}
          </td>
          <td class="px-3 py-1.5 text-right tabular-nums">
            {#if col.displayType === 'number'}
              {fmt(col.avg)}
            {:else if col.displayType === 'string'}
              {col.avgLen !== undefined ? fmt(col.avgLen) + ' ch' : '\u2014'}
            {:else}
              {'\u2014'}
            {/if}
          </td>
          <td class="px-3 py-1.5 text-right tabular-nums">
            {fmt(col.sum)}
          </td>
          <td class="px-3 py-1.5 text-right tabular-nums">
            {col.distinct !== undefined ? formatNumber(col.distinct) : '\u2014'}
          </td>
        </tr>
      {/each}
    </tbody>
  </table>
</div>
