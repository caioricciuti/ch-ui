<!-- SPDX-License-Identifier: BUSL-1.1 -->
<script lang="ts">
  import Badge from '../common/Badge.svelte'
  import type { TimelineEvent, TimelineSource } from '../../api/incidentTimeline'
  import { formatFull } from '../telemetry/time'

  /**
   * Windowed list of timeline events, same approach as the telemetry
   * LogList: fixed 28 px rows, only the visible slice is in the DOM, so a
   * full window (a few thousand events) scrolls without lag.
   */
  interface Props {
    events: TimelineEvent[]
    labels: Record<TimelineSource, string>
    selectedIndex: number
    /** Prefix the time with the date when the window spans several days. */
    withDate: boolean
    onopen: (index: number) => void
  }

  let { events, labels, selectedIndex, withDate, onopen }: Props = $props()

  const ROW_H = 28
  const OVERSCAN = 12

  let scroller = $state<HTMLDivElement | null>(null)
  let scrollTop = $state(0)
  let viewport = $state(600)

  const start = $derived(Math.max(0, Math.floor(scrollTop / ROW_H) - OVERSCAN))
  const end = $derived(Math.min(events.length, Math.ceil((scrollTop + viewport) / ROW_H) + OVERSCAN))
  const visible = $derived(events.slice(start, end))

  function onScroll() {
    if (!scroller) return
    scrollTop = scroller.scrollTop
    viewport = scroller.clientHeight
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

  const pad = (n: number) => String(n).padStart(2, '0')

  /** Local time for a row: HH:MM:SS, or MM-DD HH:MM:SS on multi-day windows. */
  function rowTime(iso: string): string {
    const d = new Date(iso)
    if (Number.isNaN(d.getTime())) return iso
    const clock = `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
    return withDate ? `${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${clock}` : clock
  }

  const flagged = (e: TimelineEvent) => ['warn', 'error', 'critical'].includes(e.severity)
</script>

<div
  bind:this={scroller}
  class="min-h-0 flex-1 overflow-y-auto"
  onscroll={onScroll}
  role="listbox"
  aria-label="Timeline events"
  tabindex="0"
>
  <div style="height:{events.length * ROW_H}px; position: relative">
    {#each visible as event, i (event.id)}
      {@const idx = start + i}
      {@const selected = idx === selectedIndex}
      <div
        class="absolute left-0 right-0 flex h-7 cursor-default items-center gap-3 border-b border-edge-subtle px-5 transition-colors {selected ? 'bg-active' : 'hover:bg-hover'}"
        style="top:{idx * ROW_H}px"
        role="option"
        aria-selected={selected}
        onclick={() => onopen(idx)}
        onkeydown={(e) => { if (e.key === 'Enter') onopen(idx) }}
        tabindex="-1"
      >
        <time class="{withDate ? 'w-[104px]' : 'w-[60px]'} shrink-0 font-mono text-xs tabular-nums text-fg-3" datetime={event.at} title={formatFull(event.at)}>{rowTime(event.at)}</time>
        <span class="w-36 shrink-0"><Badge tone={flagged(event) ? 'warning' : 'neutral'}>{labels[event.source]}</Badge></span>
        <span class="max-w-[45%] shrink-0 truncate text-[13px] font-medium text-fg" title={event.title}>{event.title}</span>
        <span class="min-w-0 flex-1 truncate text-xs text-fg-3" title={event.details}>{event.details ?? ''}</span>
        {#if event.actor}<span class="max-w-40 shrink-0 truncate text-xs text-fg-4" title={event.actor}>{event.actor}</span>{/if}
      </div>
    {/each}
  </div>
  {#if events.length > 0}
    <div class="py-3 text-center text-[11px] text-fg-4">End of window</div>
  {/if}
</div>
