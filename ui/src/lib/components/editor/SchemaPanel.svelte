<script lang="ts">
  import type { ColumnMeta } from '../../types/query'
  import { getDisplayType } from '../../utils/ch-types'

  interface Props {
    meta: ColumnMeta[]
  }

  let { meta }: Props = $props()

  const badgeColors: Record<string, string> = {
    number: 'bg-orange-100 text-orange-700 dark:bg-orange-900/40 dark:text-orange-300',
    string: 'bg-green-100 text-green-700 dark:bg-green-900/40 dark:text-green-300',
    date: 'bg-purple-100 text-purple-700 dark:bg-purple-900/40 dark:text-purple-300',
    bool: 'bg-yellow-100 text-yellow-700 dark:bg-yellow-900/40 dark:text-yellow-300',
    json: 'bg-orange-100 text-orange-700 dark:bg-orange-900/40 dark:text-orange-300',
    null: 'bg-gray-100 text-gray-500 dark:bg-gray-800 dark:text-gray-400',
    unknown: 'bg-gray-100 text-gray-500 dark:bg-gray-800 dark:text-gray-400',
  }
</script>

<div class="flex-1 overflow-auto min-h-0">
  <table class="w-full text-sm border-collapse">
    <thead class="sticky top-0 z-10 bg-gray-50 dark:bg-gray-900">
      <tr class="border-b border-gray-300 dark:border-gray-700">
        <th class="px-3 py-1.5 text-left text-xs font-medium uppercase tracking-wider text-gray-500 dark:text-gray-400 w-10">#</th>
        <th class="px-3 py-1.5 text-left text-xs font-medium uppercase tracking-wider text-gray-500 dark:text-gray-400">Column Name</th>
        <th class="px-3 py-1.5 text-left text-xs font-medium uppercase tracking-wider text-gray-500 dark:text-gray-400">ClickHouse Type</th>
        <th class="px-3 py-1.5 text-left text-xs font-medium uppercase tracking-wider text-gray-500 dark:text-gray-400">Category</th>
      </tr>
    </thead>
    <tbody>
      {#each meta as col, i}
        {@const dt = getDisplayType(col.type)}
        <tr class="border-b border-gray-100 dark:border-gray-900 {i % 2 === 1 ? 'bg-gray-50/50 dark:bg-gray-900/30' : ''}">
          <td class="px-3 py-1.5 text-gray-400 tabular-nums">{i + 1}</td>
          <td class="px-3 py-1.5 font-mono text-xs">{col.name}</td>
          <td class="px-3 py-1.5 text-gray-600 dark:text-gray-400 font-mono text-xs">{col.type}</td>
          <td class="px-3 py-1.5">
            <span class="inline-block px-2 py-0.5 text-xs font-medium rounded {badgeColors[dt]}">{dt}</span>
          </td>
        </tr>
      {/each}
    </tbody>
  </table>
</div>
