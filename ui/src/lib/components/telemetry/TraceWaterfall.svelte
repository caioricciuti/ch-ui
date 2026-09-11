<script lang="ts">
  import { ChevronRight, ChevronDown, AlertTriangle } from 'lucide-svelte'
  import type { Span } from '../../types/telemetry'
  import { serviceColor, formatDuration } from './services'

  /**
   * Span tree on the left, time bars on the right. Spans arrive pre-ordered
   * (depth/order from the server); collapsing a span hides its subtree.
   */
  interface Props {
    spans: Span[]
    services: string[]
    selectedSpanId?: string | null
    highlightSpanId?: string | null
    compact?: boolean
    onselect?: (span: Span) => void
  }

  let { spans, services, selectedSpanId = null, highlightSpanId = null, compact = false, onselect }: Props = $props()

  let collapsed = $state<Set<string>>(new Set())

  const parentOf = $derived(new Map(spans.map((s) => [s.span_id, s.parent_span_id])))
  const childCount = $derived.by(() => {
    const m = new Map<string, number>()
    for (const s of spans) if (s.parent_span_id) m.set(s.parent_span_id, (m.get(s.parent_span_id) ?? 0) + 1)
    return m
  })

  function hidden(span: Span): boolean {
    let p = span.parent_span_id
    for (let i = 0; p && i < 64; i++) {
      if (collapsed.has(p)) return true
      p = parentOf.get(p) ?? ''
    }
    return false
  }

  const visible = $derived(spans.filter((s) => !hidden(s)))

  // Timeline scale in nanoseconds, from the earliest span start.
  const startNs = $derived(spans.length ? spans.map((s) => BigInt(s.start_ns || '0')).reduce((a, b) => (a < b ? a : b)) : 0n)
  const endNs = $derived(spans.length
    ? spans.map((s) => BigInt(s.start_ns || '0') + BigInt(Math.round(s.duration_ms * 1e6))).reduce((a, b) => (a > b ? a : b))
    : 0n)
  const totalNs = $derived(endNs > startNs ? endNs - startNs : 1n)
  const totalMs = $derived(Number(totalNs) / 1e6)

  function pct(ns: bigint): number {
    return Math.max(0, Math.min(100, (Number(ns) / Number(totalNs)) * 100))
  }

  function left(span: Span): number {
    return pct(BigInt(span.start_ns || '0') - startNs)
  }

  function width(span: Span): number {
    return Math.max(0.3, pct(BigInt(Math.round(span.duration_ms * 1e6))))
  }

  function eventLeft(span: Span, time: string): number {
    const t = new Date(time).getTime()
    const s = new Date(span.start).getTime()
    if (!Number.isFinite(t) || !Number.isFinite(s) || span.duration_ms <= 0) return 0
    return Math.max(0, Math.min(100, ((t - s) / span.duration_ms) * 100))
  }

  const ticks = $derived([0, 0.25, 0.5, 0.75, 1].map((f) => ({ at: f * 100, label: formatDuration(totalMs * f) })))

  function toggle(id: string) {
    const next = new Set(collapsed)
    if (next.has(id)) next.delete(id)
    else next.add(id)
    collapsed = next
  }

  const rowH = $derived(compact ? 24 : 28)
  const treeW = $derived(compact ? 200 : 280)

  let hover = $state<{ span: Span; x: number; y: number } | null>(null)
</script>

<div class="relative flex min-h-0 flex-1 flex-col overflow-auto font-mono text-xs">
  <!-- Tick header -->
  <div class="sticky top-0 z-10 flex h-6 shrink-0 border-b border-edge-subtle bg-surface">
    <div class="shrink-0 border-r border-edge-subtle px-2 text-[11px] leading-6 text-fg-4" style="width:{treeW}px">Span</div>
    <div class="relative min-w-0 flex-1">
      {#each ticks as t (t.at)}
        <span class="absolute top-0 -translate-x-1/2 text-[10px] leading-6 text-fg-4" style="left:{t.at}%">{t.label}</span>
      {/each}
    </div>
  </div>

  {#each visible as span (span.span_id)}
    {@const color = serviceColor(span.service, services)}
    {@const selected = span.span_id === selectedSpanId}
    {@const highlighted = span.span_id === highlightSpanId}
    {@const kids = childCount.get(span.span_id) ?? 0}
    {@const isError = span.status.toLowerCase() === 'error'}
    <div
      class="flex shrink-0 cursor-pointer border-b border-edge-subtle transition-colors {selected ? 'bg-active' : highlighted ? 'bg-accent-soft' : 'hover:bg-hover'}"
      style="height:{rowH}px"
      role="button"
      tabindex="-1"
      onclick={() => onselect?.(span)}
      onkeydown={(e) => { if (e.key === 'Enter') onselect?.(span) }}
    >
      <div class="flex shrink-0 items-center gap-1 overflow-hidden border-r border-edge-subtle pr-2" style="width:{treeW}px; padding-left:{8 + span.depth * 14}px">
        {#if kids > 0}
          <button class="shrink-0 rounded-sm text-fg-4 hover:text-fg" aria-label={collapsed.has(span.span_id) ? 'Expand' : 'Collapse'} onclick={(e) => { e.stopPropagation(); toggle(span.span_id) }}>
            {#if collapsed.has(span.span_id)}<ChevronRight size={12} />{:else}<ChevronDown size={12} />{/if}
          </button>
        {:else}
          <span class="w-3 shrink-0"></span>
        {/if}
        <span class="h-3 w-0.5 shrink-0 rounded-full" style="background:{color}"></span>
        <span class="truncate text-fg" title={span.name}>{span.name}</span>
        {#if !compact}<span class="ml-1 shrink-0 truncate text-[11px] text-fg-4" title={span.service}>{span.service}</span>{/if}
        {#if isError}<AlertTriangle size={11} class="ml-auto shrink-0 text-danger" />{/if}
      </div>
      <div class="relative min-w-0 flex-1">
        {#each ticks.slice(1, -1) as t (t.at)}
          <span class="absolute top-0 h-full w-px bg-edge-subtle" style="left:{t.at}%"></span>
        {/each}
        <!-- svelte-ignore a11y_no_static_element_interactions -->
        <div
          class="absolute top-1/2 h-3.5 -translate-y-1/2 rounded-sm {isError ? 'ring-1 ring-danger' : ''}"
          style="left:{left(span)}%; width:{width(span)}%; background:{color}; opacity:{selected || highlighted ? 1 : 0.85}"
          onmouseenter={(e) => (hover = { span, x: e.clientX, y: e.clientY })}
          onmousemove={(e) => (hover = { span, x: e.clientX, y: e.clientY })}
          onmouseleave={() => (hover = null)}
        >
          {#each span.events as ev, i (i)}
            <span class="absolute top-0 h-full w-px {ev.name === 'exception' ? 'bg-danger' : 'bg-fg'}" style="left:{eventLeft(span, ev.time)}%" title={ev.name}></span>
          {/each}
        </div>
        {#if width(span) < 12}
          <span class="absolute top-1/2 -translate-y-1/2 whitespace-nowrap text-[10px] text-fg-3" style="left:calc({left(span) + width(span)}% + 6px)">{formatDuration(span.duration_ms)}</span>
        {:else}
          <span class="absolute top-1/2 -translate-y-1/2 truncate text-[10px] text-white/90" style="left:calc({left(span)}% + 4px); max-width:{width(span)}%">{formatDuration(span.duration_ms)}</span>
        {/if}
      </div>
    </div>
  {/each}

  {#if hover}
    <div class="pointer-events-none fixed z-[200] rounded-md border border-edge bg-elevated px-2.5 py-1.5 text-[11px] leading-snug text-fg shadow-popover" style="left:{hover.x + 12}px; top:{hover.y + 12}px">
      <div class="font-medium">{hover.span.name}</div>
      <div class="text-fg-3">{hover.span.service} · {hover.span.kind || 'span'} · {formatDuration(hover.span.duration_ms)}</div>
      {#if hover.span.status.toLowerCase() === 'error'}<div class="text-danger">{hover.span.status_message || 'Error'}</div>{/if}
    </div>
  {/if}
</div>
