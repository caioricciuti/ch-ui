<script lang="ts">
  /**
   * Placeholder for content that is still loading, shaped like what will
   * appear: `rows` for a table or list (one bar per column, same height as
   * a DataTable row), `text` for a paragraph, `block` for a chart or panel.
   * Replaces "Loading…" text; a Spinner is for an action in flight.
   */
  interface Props {
    variant?: 'rows' | 'text' | 'block'
    /** Row or line count for `rows` and `text`. */
    rows?: number
    /** Accessible description of what is loading ("Loading keys"). */
    label?: string
    class?: string
  }

  let { variant = 'rows', rows = 5, label = 'Loading', class: cls = '' }: Props = $props()

  // Widths cycle so the rows do not line up into a grid.
  const rowWidths = [
    ['w-1/4', 'w-1/2', 'w-1/6'],
    ['w-1/3', 'w-1/3', 'w-1/5'],
    ['w-1/5', 'w-2/5', 'w-1/6'],
  ]
  const textWidths = ['w-11/12', 'w-4/5', 'w-2/3']
</script>

<div role="status" aria-live="polite" aria-busy="true" class="animate-pulse {cls}">
  <span class="sr-only">{label}</span>
  {#if variant === 'rows'}
    {#each Array.from({ length: rows }, (_, i) => i) as i (i)}
      <div class="flex h-9 items-center gap-3 border-b border-edge-subtle px-4 last:border-b-0">
        {#each rowWidths[i % rowWidths.length] as w, j (j)}
          <div class="h-3 rounded bg-surface-2 {w}"></div>
        {/each}
      </div>
    {/each}
  {:else if variant === 'text'}
    <div class="flex flex-col gap-2">
      {#each Array.from({ length: rows }, (_, i) => i) as i (i)}
        <div class="h-3 rounded bg-surface-2 {textWidths[i % textWidths.length]}"></div>
      {/each}
    </div>
  {:else}
    <div class="h-24 w-full rounded-md bg-surface-2"></div>
  {/if}
</div>
