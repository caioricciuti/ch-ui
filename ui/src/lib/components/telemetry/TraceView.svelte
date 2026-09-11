<script lang="ts">
  import { ArrowLeft, Copy, AlertTriangle } from 'lucide-svelte'
  import Button from '../common/Button.svelte'
  import Badge from '../common/Badge.svelte'
  import Spinner from '../common/Spinner.svelte'
  import EmptyState from '../common/EmptyState.svelte'
  import TraceWaterfall from './TraceWaterfall.svelte'
  import SpanPanel from './SpanPanel.svelte'
  import { getTrace } from '../../api/telemetry'
  import type { TraceDetail, Span } from '../../types/telemetry'
  import { success as toastSuccess, error as toastError } from '../../stores/toast.svelte'
  import { serviceColor, formatDuration } from './services'
  import { formatFull } from './time'

  interface Props {
    sourceId: string
    traceId: string
    onback: () => void
    onfilter?: (key: string, value: string) => void
  }

  let { sourceId, traceId, onback, onfilter }: Props = $props()

  let detail = $state<TraceDetail | null>(null)
  let loading = $state(true)
  let error = $state<string | null>(null)
  let selected = $state<Span | null>(null)
  let panelWidth = $state(520)
  let seq = 0

  $effect(() => {
    const mySeq = ++seq
    const sid = sourceId
    const tid = traceId
    loading = true
    error = null
    selected = null
    getTrace(sid, tid)
      .then((d) => { if (mySeq === seq) detail = d })
      .catch((e: unknown) => { if (mySeq === seq) { error = e instanceof Error ? e.message : String(e); detail = null } })
      .finally(() => { if (mySeq === seq) loading = false })
  })

  const services = $derived(detail ? detail.trace.services.map((s) => s.name) : [])
  const root = $derived(detail?.spans.find((s) => s.depth === 0) ?? detail?.spans[0])

  async function copyId() {
    try {
      await navigator.clipboard.writeText(traceId)
      toastSuccess('Trace id copied')
    } catch {
      toastError('Clipboard unavailable')
    }
  }

  function onKey(e: KeyboardEvent) {
    const target = e.target as HTMLElement | null
    if (target && (target.tagName === 'INPUT' || target.tagName === 'TEXTAREA' || target.isContentEditable)) return
    if (!detail) return
    if (e.key === 'Escape') {
      selected = null
    } else if (e.key === 'j' || e.key === 'ArrowDown' || e.key === 'k' || e.key === 'ArrowUp') {
      e.preventDefault()
      const spans = detail.spans
      const idx = selected ? spans.findIndex((s) => s.span_id === selected!.span_id) : -1
      const next = e.key === 'j' || e.key === 'ArrowDown' ? Math.min(spans.length - 1, idx + 1) : Math.max(0, idx - 1)
      selected = spans[next] ?? null
    }
  }
</script>

<svelte:window onkeydown={onKey} />

<div class="flex h-10 shrink-0 items-center gap-2 border-b border-edge-subtle px-5 text-[13px]">
  <Button icon variant="ghost" size="sm" aria-label="Back to traces" title="Back (Esc closes the panel)" onclick={onback}><ArrowLeft size={15} /></Button>
  <button class="text-fg-3 hover:text-fg" onclick={onback}>Traces</button>
  <span class="text-fg-4">›</span>
  <span class="truncate font-mono font-medium text-fg" title={root?.name}>{root?.name ?? traceId}</span>
</div>

{#if loading}
  <div class="flex flex-1 items-center justify-center gap-2 text-[13px] text-fg-3"><Spinner size="sm" /> Loading trace…</div>
{:else if error || !detail}
  <EmptyState icon={AlertTriangle} title="Could not load the trace" description={error ?? 'No spans returned.'} secondary={{ label: 'Back', onclick: onback }} />
{:else}
  <div class="flex shrink-0 flex-wrap items-center gap-x-6 gap-y-2 border-b border-edge-subtle px-5 py-3 text-xs">
    <div class="flex items-center gap-1.5">
      <span class="text-fg-3">Trace</span>
      <span class="font-mono text-fg">{traceId}</span>
      <Button icon variant="ghost" size="xs" aria-label="Copy trace id" title="Copy" onclick={copyId}><Copy size={12} /></Button>
    </div>
    <div><span class="text-fg-3">Start</span> <span class="font-mono text-fg">{formatFull(detail.trace.start)}</span></div>
    <div><span class="text-fg-3">Duration</span> <span class="font-mono text-fg">{formatDuration(detail.trace.duration_ms)}</span></div>
    <div><span class="text-fg-3">Spans</span> <span class="font-mono text-fg">{detail.trace.span_count}</span></div>
    <div class="flex items-center gap-1.5">
      <span class="text-fg-3">Errors</span>
      {#if detail.trace.error_count > 0}<Badge tone="danger">{detail.trace.error_count}</Badge>{:else}<span class="font-mono text-fg">0</span>{/if}
    </div>
  </div>

  <div class="flex shrink-0 flex-wrap items-center gap-2 border-b border-edge-subtle px-5 py-2">
    {#each detail.trace.services as svc (svc.name)}
      <button
        class="inline-flex h-6 items-center gap-1.5 rounded-md bg-surface-2 px-2 text-xs text-fg-2 transition-colors hover:bg-active hover:text-fg"
        title={`${svc.spans} spans · ${svc.errors} errors · ${formatDuration(svc.duration_ms)} in ${svc.name}`}
        onclick={() => onfilter?.('service', svc.name)}
      >
        <span class="h-2 w-2 rounded-sm" style="background:{serviceColor(svc.name, services)}"></span>
        {svc.name}
        <span class="tabular-nums text-fg-4">{svc.spans}</span>
        {#if svc.errors > 0}<span class="tabular-nums text-danger">{svc.errors}</span>{/if}
      </button>
    {/each}
  </div>

  <div class="flex min-h-0 flex-1">
    <TraceWaterfall spans={detail.spans} {services} selectedSpanId={selected?.span_id ?? null} onselect={(s) => (selected = s)} />
    {#if selected}
      <SpanPanel span={selected} spans={detail.spans} logs={detail.logs} {services} width={panelWidth} onclose={() => (selected = null)} onwidth={(w) => (panelWidth = w)} {onfilter} />
    {/if}
  </div>
{/if}
