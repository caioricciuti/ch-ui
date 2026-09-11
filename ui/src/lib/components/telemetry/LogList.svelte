<script lang="ts">
  import Badge from '../common/Badge.svelte'
  import Spinner from '../common/Spinner.svelte'
  import type { LogRow } from '../../types/telemetry'
  import { severityTone } from './severity'
  import { formatClock, formatFull } from './time'

  /**
   * Windowed list of log rows: fixed 28 px rows, only the visible slice is
   * in the DOM. Emits `onnearend` when the user scrolls close to the bottom.
   */
  interface Props {
    rows: LogRow[]
    selectedIndex: number
    highlightTerms: string[]
    loadingMore: boolean
    hasMore: boolean
    onselect: (index: number) => void
    onopen: (index: number) => void
    onnearend: () => void
    onscrolltop?: (atTop: boolean) => void
    /** Attribute keys shown as extra columns after the body. */
    extraColumns?: string[]
  }

  let { rows, selectedIndex, highlightTerms, loadingMore, hasMore, onselect, onopen, onnearend, onscrolltop, extraColumns = [] }: Props = $props()

  /** Value of an attribute key on a row, searching log then resource attributes. */
  function attrValue(row: LogRow, key: string): string {
    return row.attributes?.[key] ?? row.resource?.[key] ?? row.scope?.[key] ?? ''
  }

  const ROW_H = 28
  const OVERSCAN = 12

  let scroller = $state<HTMLDivElement | null>(null)
  let scrollTop = $state(0)
  let viewport = $state(600)

  const start = $derived(Math.max(0, Math.floor(scrollTop / ROW_H) - OVERSCAN))
  const end = $derived(Math.min(rows.length, Math.ceil((scrollTop + viewport) / ROW_H) + OVERSCAN))
  const visible = $derived(rows.slice(start, end))

  function onScroll() {
    if (!scroller) return
    scrollTop = scroller.scrollTop
    viewport = scroller.clientHeight
    onscrolltop?.(scroller.scrollTop < 4)
    if (hasMore && !loadingMore && scroller.scrollTop + scroller.clientHeight > scroller.scrollHeight - ROW_H * 20) {
      onnearend()
    }
  }

  $effect(() => {
    if (!scroller) return
    const ro = new ResizeObserver(() => { viewport = scroller!.clientHeight })
    ro.observe(scroller)
    viewport = scroller.clientHeight
    return () => ro.disconnect()
  })

  /** Keep the selected row in view when it moves by keyboard. */
  export function scrollToIndex(i: number) {
    if (!scroller) return
    const top = i * ROW_H
    if (top < scroller.scrollTop) scroller.scrollTop = top
    else if (top + ROW_H > scroller.scrollTop + scroller.clientHeight) scroller.scrollTop = top + ROW_H - scroller.clientHeight
  }

  function escapeRegExp(s: string): string {
    return s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  }

  function escapeHtml(s: string): string {
    return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
  }

  /** Body with search words wrapped in <mark>; HTML-escaped first. */
  function highlighted(body: string): string {
    const safe = escapeHtml(body)
    const terms = highlightTerms.filter((t) => t.length > 1)
    if (terms.length === 0) return safe
    const re = new RegExp(`(${terms.map(escapeRegExp).join('|')})`, 'gi')
    return safe.replace(re, '<mark class="rounded-sm bg-accent-soft text-fg">$1</mark>')
  }
</script>

<div
  bind:this={scroller}
  class="min-h-0 flex-1 overflow-y-auto font-mono text-xs"
  onscroll={onScroll}
  role="listbox"
  aria-label="Log rows"
  tabindex="0"
>
  <div style="height:{rows.length * ROW_H}px; position: relative">
    {#each visible as row, i (start + i)}
      {@const idx = start + i}
      {@const selected = idx === selectedIndex}
      <div
        class="absolute left-0 right-0 flex h-7 cursor-default items-center gap-2 border-b border-edge-subtle px-3 transition-colors {selected ? 'bg-active' : 'hover:bg-hover'}"
        style="top:{idx * ROW_H}px"
        role="option"
        aria-selected={selected}
        onclick={() => { onselect(idx); onopen(idx) }}
        onkeydown={(e) => { if (e.key === 'Enter') onopen(idx) }}
        tabindex="-1"
      >
        <span class="w-[92px] shrink-0 tabular-nums text-fg-3" title={formatFull(row.timestamp)}>{formatClock(row.timestamp)}</span>
        <span class="w-14 shrink-0"><Badge tone={severityTone(row.severity)}>{row.severity || '—'}</Badge></span>
        <span class="w-32 shrink-0 truncate text-fg-2" title={row.service}>{row.service}</span>
        <span class="min-w-0 flex-1 truncate text-fg" title={row.body}>{@html highlighted(row.body)}</span>
        {#each extraColumns as key (key)}
          {@const v = attrValue(row, key)}
          <span class="w-36 shrink-0 truncate text-fg-2" title={v ? `${key}=${v}` : key}>{v || '—'}</span>
        {/each}
      </div>
    {/each}
  </div>
  {#if loadingMore}
    <div class="flex items-center justify-center gap-2 py-3 text-xs text-fg-3"><Spinner size="sm" /> Loading more…</div>
  {:else if !hasMore && rows.length > 0}
    <div class="py-3 text-center text-[11px] text-fg-4">End of results</div>
  {/if}
</div>
