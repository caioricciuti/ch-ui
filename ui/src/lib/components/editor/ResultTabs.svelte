<script lang="ts">
  import type { ResultEntry } from '../../editor/result-tabs'
  import { formatDuration, formatNumber } from '../../utils/format'
  import { CircleCheck, CircleX, LoaderCircle, Pin, X } from 'lucide-svelte'

  interface Props {
    results: ResultEntry[]
    activeIndex: number
    onselect: (id: string) => void
    onpin: (id: string) => void
    onclose: (id: string) => void
  }

  let { results, activeIndex, onselect, onpin, onclose }: Props = $props()

  function rowsLabel(r: ResultEntry): string {
    if (r.error) return 'error'
    if (r.meta.length === 0) return r.running ? '' : 'done'
    const n = r.rowsDropped ? (r.savedRowCount ?? r.data.length) : r.data.length
    return `${formatNumber(n)} ${n === 1 ? 'row' : 'rows'}`
  }

  function onKeydown(e: KeyboardEvent) {
    if (e.key !== 'ArrowRight' && e.key !== 'ArrowLeft') return
    if (results.length < 2) return
    e.preventDefault()
    const step = e.key === 'ArrowRight' ? 1 : results.length - 1
    const next = results[(Math.max(activeIndex, 0) + step) % results.length]
    if (next) onselect(next.id)
  }
</script>

<div
  class="flex items-stretch gap-0.5 h-8 px-1.5 border-b border-edge-subtle bg-surface shrink-0 overflow-x-auto select-none"
  role="tablist"
  aria-label="Results"
  tabindex="-1"
  onkeydown={onKeydown}
>
  {#each results as r, i (r.id)}
    {@const active = i === activeIndex}
    <div
      class="group flex items-center gap-1 my-1 pl-2 pr-0.5 rounded-md text-xs whitespace-nowrap transition-colors
        {active ? 'bg-surface-2 text-fg' : 'text-fg-3 hover:bg-hover hover:text-fg'}"
    >
      <button
        class="flex items-center gap-1.5 h-full outline-none"
        role="tab"
        aria-selected={active}
        tabindex={active ? 0 : -1}
        title={r.sql || r.label}
        onclick={() => onselect(r.id)}
      >
        {#if r.running}
          <LoaderCircle size={12} class="animate-spin text-fg-3 shrink-0" aria-label="Running" />
        {:else if r.error}
          <CircleX size={12} class="text-danger shrink-0" aria-label="Failed" />
        {:else}
          <CircleCheck size={12} class="text-success shrink-0" aria-label="Done" />
        {/if}
        <span class="font-mono max-w-56 truncate">{r.label}</span>
        {#if rowsLabel(r)}
          <span class="text-fg-4 tabular-nums">{rowsLabel(r)}</span>
        {/if}
        {#if !r.running && r.elapsedMs > 0}
          <span class="text-fg-4 tabular-nums">{formatDuration(r.elapsedMs)}</span>
        {/if}
      </button>
      <button
        class="p-0.5 rounded transition-colors hover:bg-hover
          {r.pinned ? 'text-accent' : 'text-fg-4 opacity-0 group-hover:opacity-100 focus-visible:opacity-100 hover:text-fg'}"
        onclick={() => onpin(r.id)}
        title={r.pinned ? 'Unpin: the next run may replace this result' : 'Pin: keep this result when you run again'}
        aria-label={r.pinned ? `Unpin ${r.label}` : `Pin ${r.label}`}
        aria-pressed={r.pinned}
      >
        <Pin size={11} class={r.pinned ? 'fill-current' : ''} />
      </button>
      <button
        class="p-0.5 rounded text-fg-4 transition-colors hover:bg-hover hover:text-fg"
        onclick={() => onclose(r.id)}
        title="Close result"
        aria-label={`Close ${r.label}`}
      >
        <X size={11} />
      </button>
    </div>
  {/each}
</div>
