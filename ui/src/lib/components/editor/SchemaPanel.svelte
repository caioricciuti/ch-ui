<script lang="ts">
  import type { ColumnMeta } from '../../types/query'
  import { getDisplayType } from '../../utils/ch-types'

  interface Props {
    meta: ColumnMeta[]
  }

  let { meta }: Props = $props()

  const badgeColors: Record<string, string> = {
    number: 'bg-accent-soft text-accent',
    string: 'bg-success-soft text-success',
    date: 'bg-info-soft text-info',
    bool: 'bg-warning-soft text-warning',
    json: 'bg-accent-soft text-accent',
    null: 'bg-surface-2 text-fg-3 ',
    unknown: 'bg-surface-2 text-fg-3 ',
  }
</script>

<div class="flex-1 overflow-auto min-h-0">
  <table class="w-full text-sm border-collapse">
    <thead class="sticky top-0 z-10 bg-surface">
      <tr class="border-b border-edge">
        <th class="px-3 py-1.5 text-left text-xs font-medium uppercase tracking-wider text-fg-3 w-10">#</th>
        <th class="px-3 py-1.5 text-left text-xs font-medium uppercase tracking-wider text-fg-3">Column Name</th>
        <th class="px-3 py-1.5 text-left text-xs font-medium uppercase tracking-wider text-fg-3">ClickHouse Type</th>
        <th class="px-3 py-1.5 text-left text-xs font-medium uppercase tracking-wider text-fg-3">Category</th>
      </tr>
    </thead>
    <tbody>
      {#each meta as col, i}
        {@const dt = getDisplayType(col.type)}
        <tr class="border-b border-edge-subtle {i % 2 === 1 ? 'bg-surface ' : ''}">
          <td class="px-3 py-1.5 text-fg-4 tabular-nums">{i + 1}</td>
          <td class="px-3 py-1.5 font-mono text-xs">{col.name}</td>
          <td class="px-3 py-1.5 text-fg-2 font-mono text-xs">{col.type}</td>
          <td class="px-3 py-1.5">
            <span class="inline-block px-2 py-0.5 text-xs font-medium rounded {badgeColors[dt]}">{dt}</span>
          </td>
        </tr>
      {/each}
    </tbody>
  </table>
</div>
