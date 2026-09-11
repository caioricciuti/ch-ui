<script lang="ts">
  import { X, Copy, Filter, FilterX, ExternalLink } from 'lucide-svelte'
  import Button from '../common/Button.svelte'
  import Badge from '../common/Badge.svelte'
  import Tabs from '../common/Tabs.svelte'
  import Spinner from '../common/Spinner.svelte'
  import type { LogRow, TraceDetail } from '../../types/telemetry'
  import { logsContext, logsByTrace, getTrace } from '../../api/telemetry'
  import TraceWaterfall from './TraceWaterfall.svelte'
  import { success as toastSuccess, error as toastError } from '../../stores/toast.svelte'
  import { severityTone } from './severity'
  import { formatClock, formatFull } from './time'

  interface Props {
    row: LogRow
    sourceId: string
    /** Traces source to draw the waterfall from; '' when none is configured. */
    traceSourceId?: string
    width: number
    onclose: () => void
    onwidth: (w: number) => void
    onfilter: (key: string, value: string, negate: boolean) => void
  }

  let { row, sourceId, traceSourceId = '', width, onclose, onwidth, onfilter }: Props = $props()

  type Tab = 'overview' | 'attributes' | 'trace' | 'context' | 'raw'
  let tab = $state<Tab>('overview')
  const tabs = [
    { id: 'overview', label: 'Overview' },
    { id: 'attributes', label: 'Attributes' },
    { id: 'trace', label: 'Trace' },
    { id: 'context', label: 'Context' },
    { id: 'raw', label: 'Raw' },
  ]

  // ── Lazy tabs ─────────────────────────────────────────────
  let contextRows = $state<LogRow[]>([])
  let contextAnchor = $state(-1)
  let contextLoading = $state(false)
  let contextFor = $state('')
  let traceRows = $state<LogRow[]>([])
  let traceLoading = $state(false)
  let traceFor = $state('')
  let traceDetail = $state<TraceDetail | null>(null)
  let traceDetailFor = $state('')
  const waterfallServices = $derived([...new Set((traceDetail?.spans ?? []).map((s) => s.service))])

  $effect(() => {
    const key = row.timestamp_ns + row.service
    if (tab === 'context' && contextFor !== key) {
      contextFor = key
      contextLoading = true
      logsContext({ source_id: sourceId, timestamp_ns: row.timestamp_ns, service: row.service, before: 50, after: 50 })
        .then((res) => { contextRows = res.rows; contextAnchor = res.anchor_index })
        .catch((e: unknown) => toastError(e instanceof Error ? e.message : String(e)))
        .finally(() => (contextLoading = false))
    }
    if (tab === 'trace' && row.trace_id && traceFor !== row.trace_id) {
      traceFor = row.trace_id
      traceLoading = true
      logsByTrace(sourceId, row.trace_id)
        .then((rows) => (traceRows = rows))
        .catch((e: unknown) => toastError(e instanceof Error ? e.message : String(e)))
        .finally(() => (traceLoading = false))
    }
    if (tab === 'trace' && row.trace_id && traceSourceId && traceDetailFor !== row.trace_id) {
      traceDetailFor = row.trace_id
      getTrace(traceSourceId, row.trace_id)
        .then((d) => (traceDetail = d))
        .catch(() => (traceDetail = null))
    }
  })

  async function copy(text: string, what = 'Copied') {
    try {
      await navigator.clipboard.writeText(text)
      toastSuccess(what)
    } catch {
      toastError('Clipboard unavailable')
    }
  }

  function copyLink() {
    const url = new URL(window.location.href)
    url.searchParams.set('row', row.timestamp_ns)
    void copy(url.toString(), 'Link copied')
  }

  // ── Resize by dragging the left edge ─────────────────────
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
    ['Timestamp', formatFull(row.timestamp)],
    ['Severity', row.severity || '—'],
    ['Service', row.service || '—'],
    ['Scope', row.scope_name || '—'],
    ['Event', row.event_name || '—'],
    ['Trace id', row.trace_id || '—'],
    ['Span id', row.span_id || '—'],
  ])

  const groups = $derived<[string, Record<string, string>][]>([
    ['Log attributes', row.attributes ?? {}],
    ['Resource', row.resource ?? {}],
    ['Scope', row.scope ?? {}],
  ])
</script>

<aside class="relative flex h-full min-h-0 shrink-0 flex-col border-l border-edge-subtle bg-surface" style="width:{width}px">
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div class="absolute -left-0.5 top-0 z-10 h-full w-1 cursor-col-resize hover:bg-active {dragging ? 'bg-accent/60' : ''}" onmousedown={startDrag}></div>

  <header class="flex h-12 shrink-0 items-center gap-2 border-b border-edge-subtle px-4">
    <Badge tone={severityTone(row.severity)}>{row.severity || '—'}</Badge>
    <span class="truncate text-[13px] font-medium text-fg" title={row.service}>{row.service}</span>
    <span class="font-mono text-xs text-fg-3" title={formatFull(row.timestamp)}>{formatClock(row.timestamp)}</span>
    <div class="ml-auto flex items-center gap-0.5">
      <Button icon variant="ghost" size="sm" aria-label="Copy link to this row" title="Copy link" onclick={copyLink}><Copy size={14} /></Button>
      <Button icon variant="ghost" size="sm" aria-label="Close" title="Close (Esc)" onclick={onclose}><X size={15} /></Button>
    </div>
  </header>

  <div class="shrink-0 px-4 pt-3">
    <Tabs variant="segmented" size="sm" items={tabs} value={tab} onchange={(id) => (tab = id as Tab)} />
  </div>

  <div class="min-h-0 flex-1 overflow-y-auto px-4 py-3">
    {#if tab === 'overview'}
      <p class="mb-4 whitespace-pre-wrap break-words rounded-md bg-surface-2 p-3 font-mono text-xs leading-relaxed text-fg">{row.body}</p>
      <dl class="divide-y divide-edge-subtle">
        {#each overview as [label, value] (label)}
          <div class="flex items-start gap-3 py-1.5 text-xs">
            <dt class="w-24 shrink-0 text-fg-3">{label}</dt>
            <dd class="min-w-0 flex-1 break-all font-mono text-fg">
              {value}
              {#if label === 'Trace id' && row.trace_id}
                <a class="ml-2 inline-flex items-center gap-1 text-accent hover:underline" href={`/telemetry?section=traces&trace=${encodeURIComponent(row.trace_id)}`}>
                  View trace <ExternalLink size={11} />
                </a>
              {/if}
            </dd>
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
                    <Button icon variant="ghost" size="xs" aria-label="Filter for this value" title="Filter for" onclick={() => onfilter(k, v, false)}><Filter size={12} /></Button>
                    <Button icon variant="ghost" size="xs" aria-label="Filter out this value" title="Filter out" onclick={() => onfilter(k, v, true)}><FilterX size={12} /></Button>
                    <Button icon variant="ghost" size="xs" aria-label="Copy value" title="Copy" onclick={() => copy(v)}><Copy size={12} /></Button>
                  </span>
                </div>
              {/each}
            </div>
          {/if}
        </section>
      {/each}
    {:else if tab === 'trace'}
      {#if !row.trace_id}
        <p class="text-xs text-fg-3">This row carries no trace id.</p>
      {:else}
        {#if traceDetail && traceDetail.spans.length}
          <div class="mb-3 max-h-64 overflow-auto rounded-md border border-edge-subtle">
            <TraceWaterfall spans={traceDetail.spans} services={waterfallServices} highlightSpanId={row.span_id} compact />
          </div>
        {/if}
        <p class="mb-2 text-xs text-fg-4">Logs sharing trace <span class="font-mono text-fg-2">{row.trace_id}</span>:</p>
        {#if traceLoading}
          <div class="flex items-center gap-2 text-xs text-fg-3"><Spinner size="sm" /> Loading…</div>
        {:else}
          <div class="divide-y divide-edge-subtle font-mono text-xs">
            {#each traceRows as r (r.timestamp_ns + r.span_id)}
              <div class="flex items-center gap-2 py-1 {r.timestamp_ns === row.timestamp_ns ? 'bg-accent-soft' : ''}">
                <span class="w-[92px] shrink-0 tabular-nums text-fg-3">{formatClock(r.timestamp)}</span>
                <Badge tone={severityTone(r.severity)}>{r.severity}</Badge>
                <span class="w-28 shrink-0 truncate text-fg-2">{r.service}</span>
                <span class="min-w-0 flex-1 truncate text-fg" title={r.body}>{r.body}</span>
              </div>
            {:else}
              <p class="py-2 text-fg-4">No other logs for this trace.</p>
            {/each}
          </div>
        {/if}
      {/if}
    {:else if tab === 'context'}
      {#if contextLoading}
        <div class="flex items-center gap-2 text-xs text-fg-3"><Spinner size="sm" /> Loading…</div>
      {:else}
        <div class="divide-y divide-edge-subtle font-mono text-xs">
          {#each contextRows as r, i (r.timestamp_ns + i)}
            <div class="flex items-center gap-2 py-1 {i === contextAnchor ? 'bg-accent-soft' : ''}">
              <span class="w-[92px] shrink-0 tabular-nums text-fg-3" title={formatFull(r.timestamp)}>{formatClock(r.timestamp)}</span>
              <Badge tone={severityTone(r.severity)}>{r.severity}</Badge>
              <span class="min-w-0 flex-1 truncate text-fg" title={r.body}>{r.body}</span>
            </div>
          {:else}
            <p class="py-2 text-fg-4">No surrounding rows.</p>
          {/each}
        </div>
      {/if}
    {:else}
      <pre class="overflow-x-auto whitespace-pre-wrap break-all rounded-md bg-surface-2 p-3 font-mono text-xs leading-relaxed text-fg">{JSON.stringify(row, null, 2)}</pre>
    {/if}
  </div>
</aside>
