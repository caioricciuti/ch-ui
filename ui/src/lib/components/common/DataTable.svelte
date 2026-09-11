<script lang="ts" module>
  import type { Snippet } from 'svelte'
  /**
   * Spec-driven table for lists of records: sticky header, click-to-sort,
   * right-aligned numbers, mono cells that truncate with the full text on
   * hover, an optional trailing actions cell. With `fill` it takes the
   * height of its container and scrolls inside, so a section can be just
   * this table under the page header.
   */
  export interface DataColumn<R> {
    key: string
    label: string
    align?: 'left' | 'right'
    mono?: boolean
    /** CSS width, e.g. "120px" or "30%". */
    width?: string
    /** Cut long text to one line; the full value goes on the title. */
    truncate?: boolean
    format?: (value: unknown, row: R) => string
    /** false disables sorting on this column. */
    sortable?: boolean
    /** Value to sort by when the raw cell value is not comparable. */
    sortValue?: (row: R) => number | string
  }

  export interface DataTableProps<R extends Record<string, unknown>> {
    columns: DataColumn<R>[]
    rows: R[]
    rowKey?: (row: R, index: number) => string | number
    /** Initial sort; the user can change it by clicking a header. */
    sort?: { key: string; dir: 'asc' | 'desc' } | null
    /** Take the container's height and scroll inside. */
    fill?: boolean
    /** Frame the table in a bordered card. Off when `fill`, which is framed by its section. */
    framed?: boolean
    emptyTitle?: string
    emptyDescription?: string
    onrowclick?: (row: R) => void
    class?: string
    /** Custom cell content; return nothing to fall back to the formatted text. */
    cell?: Snippet<[R, DataColumn<R>, string]>
    /** Trailing cell per row, for buttons. */
    actions?: Snippet<[R]>
  }
</script>

<script lang="ts" generics="Row extends Record<string, unknown>">
  import { ChevronUp, ChevronDown } from 'lucide-svelte'
  import EmptyState from './EmptyState.svelte'

  let {
    columns, rows, rowKey, sort = null, fill = false, framed = true,
    emptyTitle = 'Nothing here', emptyDescription, onrowclick, class: cls = '', cell, actions,
  }: DataTableProps<Row> = $props()

  let sortKey = $state<string | null>(null)
  let sortDir = $state<'asc' | 'desc'>('desc')
  $effect(() => {
    // Adopt the caller's initial sort once per column set change.
    columns
    sortKey = sort?.key ?? null
    sortDir = sort?.dir ?? 'desc'
  })

  function text(row: Row, col: DataColumn<Row>): string {
    const v = row[col.key]
    if (col.format) return col.format(v, row)
    return v == null ? '' : String(v)
  }

  function comparable(row: Row, col: DataColumn<Row>): number | string {
    if (col.sortValue) return col.sortValue(row)
    const v = row[col.key]
    if (typeof v === 'number') return v
    if (v == null) return ''
    const n = Number(v)
    return Number.isFinite(n) && String(v).trim() !== '' ? n : String(v)
  }

  const sorted = $derived.by(() => {
    if (!sortKey) return rows
    const col = columns.find((c) => c.key === sortKey)
    if (!col) return rows
    const dir = sortDir === 'asc' ? 1 : -1
    return rows.slice().sort((a, b) => {
      const x = comparable(a, col)
      const y = comparable(b, col)
      if (typeof x === 'number' && typeof y === 'number') return (x - y) * dir
      return String(x).localeCompare(String(y)) * dir
    })
  })

  function toggleSort(col: DataColumn<Row>) {
    if (col.sortable === false) return
    if (sortKey === col.key) {
      sortDir = sortDir === 'asc' ? 'desc' : 'asc'
    } else {
      sortKey = col.key
      sortDir = col.align === 'right' ? 'desc' : 'asc'
    }
  }

  const frame = $derived(fill ? 'h-full min-h-0 overflow-auto' : framed ? 'overflow-hidden rounded-lg border border-edge-subtle bg-surface' : '')
</script>

<div class="{frame} {cls}">
  {#if rows.length === 0}
    <EmptyState size="compact" title={emptyTitle} description={emptyDescription} />
  {:else}
    <table class="w-full border-separate border-spacing-0 text-[13px]">
      <thead>
        <tr>
          {#each columns as col (col.key)}
            <th
              class="sticky top-0 z-10 h-8 whitespace-nowrap border-b border-edge bg-surface px-3 text-xs font-medium text-fg-3 first:pl-4 {col.align === 'right' ? 'text-right' : 'text-left'}"
              style={col.width ? `width:${col.width}` : undefined}
              aria-sort={sortKey === col.key ? (sortDir === 'asc' ? 'ascending' : 'descending') : undefined}
            >
              {#if col.sortable === false}
                {col.label}
              {:else}
                <button class="inline-flex items-center gap-1 hover:text-fg {col.align === 'right' ? 'flex-row-reverse' : ''}" onclick={() => toggleSort(col)}>
                  {col.label}
                  {#if sortKey === col.key}
                    {#if sortDir === 'asc'}<ChevronUp size={12} />{:else}<ChevronDown size={12} />{/if}
                  {/if}
                </button>
              {/if}
            </th>
          {/each}
          {#if actions}
            <th class="sticky top-0 z-10 h-8 border-b border-edge bg-surface px-2 pr-3"><span class="sr-only">Actions</span></th>
          {/if}
        </tr>
      </thead>
      <tbody>
        {#each sorted as row, i (rowKey ? rowKey(row, i) : i)}
          <tr
            class="group transition-colors hover:bg-hover {onrowclick ? 'cursor-pointer' : ''}"
            onclick={onrowclick ? () => onrowclick(row) : undefined}
          >
            {#each columns as col (col.key)}
              {@const value = text(row, col)}
              <td
                class="border-b border-edge-subtle px-3 py-1.5 first:pl-4 group-last:border-b-0 {col.align === 'right' ? 'text-right tabular-nums' : ''} {col.mono ? 'font-mono text-xs text-fg-2' : 'text-fg-2'} {col.truncate ? 'max-w-0 min-w-[12rem] truncate' : 'whitespace-nowrap'}"
                title={col.truncate ? value : undefined}
              >
                {#if cell}{@render cell(row, col, value)}{:else}{value}{/if}
              </td>
            {/each}
            {#if actions}
              <td class="border-b border-edge-subtle px-2 py-1 pr-3 text-right group-last:border-b-0">
                <div class="inline-flex items-center justify-end gap-0.5 opacity-0 transition-opacity group-hover:opacity-100 focus-within:opacity-100">
                  {@render actions(row)}
                </div>
              </td>
            {/if}
          </tr>
        {/each}
      </tbody>
    </table>
  {/if}
</div>
