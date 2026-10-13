<script lang="ts">
  import type { ColumnMeta } from '../../utils/chart-transform'

  interface Props {
    data: Record<string, unknown>[]
    meta: ColumnMeta[]
    /** Rows drawn in the panel; the fullscreen view shows every row. */
    limit?: number
  }

  let { data, meta, limit = 100 }: Props = $props()

  const shown = $derived(data.slice(0, limit))
  const capped = $derived(data.length > limit)

  function cell(v: unknown): string {
    if (v == null) return '--'
    if (typeof v === 'object') return JSON.stringify(v)
    return String(v)
  }
</script>

{#if meta.length > 0}
  <div class="flex h-full min-h-0 flex-col">
    <div class="min-h-0 flex-1 overflow-auto">
      <table class="w-full text-xs">
        <thead>
          <tr class="border-b border-edge-subtle">
            {#each meta as col (col.name)}
              <th class="whitespace-nowrap px-2 py-1 text-left font-medium text-fg-3">{col.name}</th>
            {/each}
          </tr>
        </thead>
        <tbody>
          {#each shown as row, i (i)}
            <tr class="border-b border-edge-subtle">
              {#each meta as col (col.name)}
                <td class="whitespace-nowrap px-2 py-1 text-fg-2">{cell(row[col.name])}</td>
              {/each}
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
    {#if capped}
      <p class="shrink-0 border-t border-edge-subtle px-2 pt-1 text-xs text-fg-4">
        Showing {limit.toLocaleString()} of {data.length.toLocaleString()} rows
      </p>
    {/if}
  </div>
{:else}
  <p class="p-2 text-xs text-fg-3">No data</p>
{/if}
