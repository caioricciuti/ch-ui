<script lang="ts">
  import { X, Copy, Filter } from 'lucide-svelte'
  import Button from '../common/Button.svelte'
  import Badge from '../common/Badge.svelte'
  import Tabs from '../common/Tabs.svelte'
  import type { Span, LogRow } from '../../types/telemetry'
  import { success as toastSuccess, error as toastError } from '../../stores/toast.svelte'
  import { severityTone } from './severity'
  import { formatClock, formatFull } from './time'
  import { serviceColor, formatDuration } from './services'

  interface Props {
    span: Span
    spans: Span[]
    logs: LogRow[]
    services: string[]
    width: number
    onclose: () => void
    onwidth: (w: number) => void
    onfilter?: (key: string, value: string) => void
  }

  let { span, spans, logs, services, width, onclose, onwidth, onfilter }: Props = $props()

  type Tab = 'overview' | 'attributes' | 'events' | 'links' | 'logs'
  let tab = $state<Tab>('overview')
  const spanLogs = $derived.by(() => {
    const own = logs.filter((l) => l.span_id === span.span_id)
    return own.length ? own : logs
  })

  const tabs = $derived([
    { id: 'overview', label: 'Overview' },
    { id: 'attributes', label: 'Attributes' },
    { id: 'events', label: 'Events', count: span.events.length || undefined },
    { id: 'links', label: 'Links', count: span.links.length || undefined },
    { id: 'logs', label: 'Logs', count: spanLogs.length || undefined },
  ])

  const parent = $derived(spans.find((s) => s.span_id === span.parent_span_id))
  const logsAreOwn = $derived(logs.some((l) => l.span_id === span.span_id))

  function statusTone(v: string): 'success' | 'danger' | 'neutral' {
    const u = v.toLowerCase()
    return u === 'error' ? 'danger' : u === 'ok' ? 'success' : 'neutral'
  }

  async function copy(text: string, what = 'Copied') {
    try {
      await navigator.clipboard.writeText(text)
      toastSuccess(what)
    } catch {
      toastError('Clipboard unavailable')
    }
  }

  let dragging = $state(false)
  function startDrag(e: MouseEvent) {
    e.preventDefault()
    dragging = true
    const startX = e.clientX
    const startW = width
    const move = (ev: MouseEvent) => onwidth(Math.max(360, Math.min(960, startW + (startX - ev.clientX))))
    const up = () => {
      dragging = false
      document.removeEventListener('mousemove', move)
      document.removeEventListener('mouseup', up)
    }
    document.addEventListener('mousemove', move)
    document.addEventListener('mouseup', up)
  }

  const overview = $derived<[string, string][]>([
    ['Name', span.name],
    ['Service', span.service],
    ['Kind', span.kind || '—'],
    ['Status', span.status || 'Unset'],
    ['Message', span.status_message || '—'],
    ['Start', formatFull(span.start)],
    ['Duration', formatDuration(span.duration_ms)],
    ['Span id', span.span_id],
    ['Parent', parent ? `${parent.name} (${parent.service})` : span.parent_span_id ? span.parent_span_id : 'root'],
  ])

  const groups = $derived<[string, Record<string, string>][]>([
    ['Span attributes', span.attributes ?? {}],
    ['Resource', span.resource ?? {}],
  ])
</script>

<aside class="relative flex h-full min-h-0 shrink-0 flex-col border-l border-edge-subtle bg-surface" style="width:{width}px">
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div class="absolute -left-0.5 top-0 z-10 h-full w-1 cursor-col-resize hover:bg-active {dragging ? 'bg-accent/60' : ''}" onmousedown={startDrag}></div>

  <header class="flex h-12 shrink-0 items-center gap-2 border-b border-edge-subtle px-4">
    <span class="h-3 w-1 shrink-0 rounded-full" style="background:{serviceColor(span.service, services)}"></span>
    <span class="truncate font-mono text-[13px] font-medium text-fg" title={span.name}>{span.name}</span>
    <Badge tone={statusTone(span.status)}>{span.status || 'Unset'}</Badge>
    <span class="font-mono text-xs text-fg-3">{formatDuration(span.duration_ms)}</span>
    <div class="ml-auto flex items-center gap-0.5">
      <Button icon variant="ghost" size="sm" aria-label="Copy span id" title="Copy span id" onclick={() => copy(span.span_id, 'Span id copied')}><Copy size={14} /></Button>
      <Button icon variant="ghost" size="sm" aria-label="Close" title="Close (Esc)" onclick={onclose}><X size={15} /></Button>
    </div>
  </header>

  <div class="shrink-0 px-4 pt-3">
    <Tabs variant="segmented" size="sm" items={tabs} value={tab} onchange={(id) => (tab = id as Tab)} />
  </div>

  <div class="min-h-0 flex-1 overflow-y-auto px-4 py-3">
    {#if tab === 'overview'}
      <dl class="divide-y divide-edge-subtle">
        {#each overview as [label, value] (label)}
          <div class="flex items-start gap-3 py-1.5 text-xs">
            <dt class="w-24 shrink-0 text-fg-3">{label}</dt>
            <dd class="min-w-0 flex-1 break-all font-mono text-fg">{value}</dd>
          </div>
        {/each}
      </dl>
    {:else if tab === 'attributes'}
      {#each groups as [title, attrs] (title)}
        <section class="mb-4">
          <h3 class="mb-1 text-[11px] font-medium uppercase tracking-wider text-fg-4">{title}</h3>
          {#if Object.keys(attrs).length === 0}
            <p class="text-xs text-fg-4">None</p>
          {:else}
            <div class="divide-y divide-edge-subtle">
              {#each Object.entries(attrs) as [k, v] (k)}
                <div class="group flex items-start gap-3 py-1.5 text-xs">
                  <span class="w-40 shrink-0 truncate font-mono text-fg-3" title={k}>{k}</span>
                  <span class="min-w-0 flex-1 break-all font-mono text-fg">{v}</span>
                  <span class="flex shrink-0 items-center gap-0.5 opacity-0 transition-opacity group-hover:opacity-100">
                    {#if onfilter}
                      <Button icon variant="ghost" size="xs" aria-label="Filter for this value" title="Filter for" onclick={() => onfilter(k, v)}><Filter size={12} /></Button>
                    {/if}
                    <Button icon variant="ghost" size="xs" aria-label="Copy value" title="Copy" onclick={() => copy(v)}><Copy size={12} /></Button>
                  </span>
                </div>
              {/each}
            </div>
          {/if}
        </section>
      {/each}
    {:else if tab === 'events'}
      {#if span.events.length === 0}
        <p class="text-xs text-fg-4">No events on this span.</p>
      {:else}
        <div class="space-y-3">
          {#each span.events as ev, i (i)}
            {@const isException = ev.name === 'exception'}
            <section class="rounded-md border border-edge-subtle p-3">
              <div class="mb-1 flex items-center gap-2 text-xs">
                <Badge tone={isException ? 'danger' : 'neutral'}>{ev.name}</Badge>
                <span class="font-mono text-fg-3" title={formatFull(ev.time)}>{formatClock(ev.time)}</span>
              </div>
              {#if isException}
                <div class="font-mono text-xs text-fg">
                  <span class="text-danger">{ev.attributes['exception.type'] || 'Exception'}</span>
                  {#if ev.attributes['exception.message']}: {ev.attributes['exception.message']}{/if}
                </div>
                {#if ev.attributes['exception.stacktrace']}
                  <pre class="mt-2 max-h-72 overflow-auto whitespace-pre rounded-md bg-surface-2 p-2 font-mono text-[11px] leading-relaxed text-fg-2">{ev.attributes['exception.stacktrace']}</pre>
                {/if}
              {/if}
              {#each Object.entries(ev.attributes).filter(([k]) => !isException || !k.startsWith('exception.')) as [k, v] (k)}
                <div class="flex items-start gap-3 py-0.5 text-xs">
                  <span class="w-40 shrink-0 truncate font-mono text-fg-3" title={k}>{k}</span>
                  <span class="min-w-0 flex-1 break-all font-mono text-fg">{v}</span>
                </div>
              {/each}
            </section>
          {/each}
        </div>
      {/if}
    {:else if tab === 'links'}
      {#if span.links.length === 0}
        <p class="text-xs text-fg-4">No links on this span.</p>
      {:else}
        <div class="divide-y divide-edge-subtle">
          {#each span.links as l, i (i)}
            <div class="py-2 font-mono text-xs">
              <div class="flex items-center gap-2">
                <span class="text-fg-3">trace</span>
                <a class="truncate text-accent hover:underline" href={`/telemetry?section=traces&trace=${encodeURIComponent(l.trace_id)}`}>{l.trace_id}</a>
              </div>
              <div class="mt-0.5 flex items-center gap-2"><span class="text-fg-3">span</span><span class="text-fg">{l.span_id}</span></div>
              {#each Object.entries(l.attributes ?? {}) as [k, v] (k)}
                <div class="mt-0.5 flex items-start gap-3"><span class="w-32 shrink-0 truncate text-fg-3">{k}</span><span class="min-w-0 flex-1 break-all text-fg">{v}</span></div>
              {/each}
            </div>
          {/each}
        </div>
      {/if}
    {:else}
      {#if spanLogs.length === 0}
        <p class="text-xs text-fg-4">No logs for this trace.</p>
      {:else}
        {#if !logsAreOwn}<p class="mb-2 text-[11px] text-fg-4">No log carries this span id; showing every log of the trace.</p>{/if}
        <div class="divide-y divide-edge-subtle font-mono text-xs">
          {#each spanLogs as r (r.timestamp_ns + r.span_id)}
            <div class="flex items-center gap-2 py-1">
              <span class="w-[92px] shrink-0 tabular-nums text-fg-3" title={formatFull(r.timestamp)}>{formatClock(r.timestamp)}</span>
              <Badge tone={severityTone(r.severity)}>{r.severity}</Badge>
              <span class="w-24 shrink-0 truncate text-fg-2" title={r.service}>{r.service}</span>
              <span class="min-w-0 flex-1 truncate text-fg" title={r.body}>{r.body}</span>
            </div>
          {/each}
        </div>
      {/if}
    {/if}
  </div>
</aside>
