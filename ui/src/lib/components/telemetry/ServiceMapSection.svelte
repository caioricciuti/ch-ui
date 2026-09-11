<script lang="ts">
  import { onMount } from 'svelte'
  import { RefreshCw, PanelRightClose, PanelRightOpen, Waypoints } from 'lucide-svelte'
  import Button from '../common/Button.svelte'
  import Select from '../common/Select.svelte'
  import Tabs from '../common/Tabs.svelte'
  import Spinner from '../common/Spinner.svelte'
  import EmptyState from '../common/EmptyState.svelte'
  import DataTable, { type DataColumn } from '../common/DataTable.svelte'
  import TimeRangeSelector from '../dashboard/TimeRangeSelector.svelte'
  import { setSection } from '../../stores/nav.svelte'
  import { serviceMap } from '../../api/telemetry'
  import type { TelemetrySource, ServiceMap, ServiceMapNode, ServiceMapEdge } from '../../types/telemetry'
  import { encodeAbsoluteDashboardRange } from '../../utils/dashboard-time'
  import { resolveRange } from './time'
  import { formatNumber } from '../../utils/format'

  interface Props {
    sources: TelemetrySource[]
  }
  let { sources }: Props = $props()

  const traceSources = $derived(sources.filter((s) => s.kind === 'traces' && s.enabled))

  // ── State (mirrored in the URL) ──────────────────────────────
  type Metric = 'calls' | 'errors' | 'p95'
  let sourceId = $state('')
  let range = $state('1h')
  let metric = $state<Metric>('calls')
  let map = $state<ServiceMap | null>(null)
  let loading = $state(false)
  let error = $state<string | null>(null)
  let tablesOpen = $state(true)
  let hovered = $state<string | null>(null)
  let seq = 0

  const metricTabs = [
    { id: 'calls', label: 'Calls' },
    { id: 'errors', label: 'Errors' },
    { id: 'p95', label: 'p95' },
  ]

  function readUrl() {
    const p = new URLSearchParams(window.location.search)
    sourceId = p.get('source') ?? ''
    const from = p.get('from')
    const to = p.get('to')
    if (from && to) range = encodeAbsoluteDashboardRange(from, to)
    else if (from) range = from
    const m = p.get('metric')
    if (m === 'calls' || m === 'errors' || m === 'p95') metric = m
  }

  function writeUrl() {
    const url = new URL(window.location.href)
    const set = (k: string, v: string) => (v ? url.searchParams.set(k, v) : url.searchParams.delete(k))
    set('section', 'service-map')
    set('source', sourceId)
    const abs = range.startsWith('abs:') ? resolveRange(range) : null
    set('from', abs ? abs.from : range)
    set('to', abs ? abs.to : '')
    set('metric', metric)
    history.replaceState(null, '', `${url.pathname}?${url.searchParams.toString()}`)
  }

  async function run() {
    if (!sourceId) return
    const mySeq = ++seq
    loading = true
    error = null
    const r = resolveRange(range)
    try {
      const res = await serviceMap({ source_id: sourceId, from: r.from, to: r.to })
      if (mySeq !== seq) return
      map = res
      writeUrl()
    } catch (e: unknown) {
      if (mySeq !== seq) return
      error = e instanceof Error ? e.message : String(e)
      map = null
    } finally {
      if (mySeq === seq) loading = false
    }
  }

  onMount(() => {
    readUrl()
    if (!sourceId || !traceSources.some((s) => s.id === sourceId)) sourceId = traceSources[0]?.id ?? ''
    void run()
  })

  function setRange(v: string) {
    range = v
    void run()
  }

  function setMetric(id: string) {
    metric = id as Metric
    writeUrl()
  }

  /** Open the traces section filtered to one service. */
  function openService(name: string) {
    const url = new URL(window.location.href)
    url.searchParams.set('section', 'traces')
    url.searchParams.set('q', `service:${name}`)
    url.searchParams.delete('metric')
    history.replaceState(null, '', `${url.pathname}?${url.searchParams.toString()}`)
    setSection('traces')
  }

  // ── Layout ───────────────────────────────────────────────────
  let width = $state(800)
  let height = $state(500)

  const NODE_W = 150
  const NODE_H = 46

  interface PlacedNode extends ServiceMapNode {
    x: number
    y: number
    tone: Tone
  }

  type Tone = 'accent' | 'info' | 'success' | 'warning' | 'danger'
  const TONES: Tone[] = ['accent', 'info', 'success', 'warning', 'danger']

  function hash(s: string): number {
    let h = 0
    for (let i = 0; i < s.length; i++) h = (h * 31 + s.charCodeAt(i)) | 0
    return Math.abs(h)
  }

  const nodes = $derived.by<PlacedNode[]>(() => {
    const list = map?.nodes ?? []
    const n = list.length
    if (n === 0) return []
    const cx = width / 2
    const cy = height / 2
    const twoRings = n > 12
    const outer = Math.max(120, Math.min(cx - NODE_W / 2 - 24, cy - NODE_H / 2 - 24))
    return list.map((node, i) => {
      const ring = twoRings && i % 2 === 1 ? outer * 0.55 : outer
      const angle = (i / n) * Math.PI * 2 - Math.PI / 2
      return {
        ...node,
        x: cx + Math.cos(angle) * ring,
        y: cy + Math.sin(angle) * ring,
        tone: TONES[hash(node.service) % TONES.length],
      }
    })
  })

  const byName = $derived(new Map(nodes.map((n) => [n.service, n])))

  interface PlacedEdge extends ServiceMapEdge {
    path: string
    lx: number
    ly: number
    label: string
    width: number
    danger: boolean
    dim: boolean
    ax: number
    ay: number
    angle: number
  }

  function edgeValue(e: ServiceMapEdge): number {
    return metric === 'calls' ? e.calls : metric === 'errors' ? e.errors : e.p95_ms
  }

  function edgeLabel(e: ServiceMapEdge): string {
    if (metric === 'calls') return `${formatNumber(e.calls)} calls`
    if (metric === 'errors') return `${formatNumber(e.errors)} errors`
    return `${e.p95_ms.toFixed(0)} ms p95`
  }

  const edges = $derived.by<PlacedEdge[]>(() => {
    const list = map?.edges ?? []
    const max = Math.max(1, ...list.map(edgeValue))
    return list.flatMap((e) => {
      const a = byName.get(e.from)
      const b = byName.get(e.to)
      if (!a || !b) return []
      // Curve away from the centre so opposite edges do not overlap.
      const mx = (a.x + b.x) / 2
      const my = (a.y + b.y) / 2
      const dx = b.x - a.x
      const dy = b.y - a.y
      const len = Math.max(1, Math.hypot(dx, dy))
      const nx = -dy / len
      const ny = dx / len
      const bend = Math.min(60, len * 0.18)
      const qx = mx + nx * bend
      const qy = my + ny * bend
      // Trim the end so the arrow head stops at the node edge.
      const tEnd = 1 - (NODE_W / 2 + 6) / len
      const ex = (1 - tEnd) * (1 - tEnd) * a.x + 2 * (1 - tEnd) * tEnd * qx + tEnd * tEnd * b.x
      const ey = (1 - tEnd) * (1 - tEnd) * a.y + 2 * (1 - tEnd) * tEnd * qy + tEnd * tEnd * b.y
      const tanx = 2 * (1 - tEnd) * (qx - a.x) + 2 * tEnd * (b.x - qx)
      const tany = 2 * (1 - tEnd) * (qy - a.y) + 2 * tEnd * (b.y - qy)
      const angle = (Math.atan2(tany, tanx) * 180) / Math.PI
      const ratio = edgeValue(e) / max
      const errRate = e.calls > 0 ? e.errors / e.calls : 0
      return [{
        ...e,
        path: `M ${a.x} ${a.y} Q ${qx} ${qy} ${ex} ${ey}`,
        lx: 0.25 * a.x + 0.5 * qx + 0.25 * ex,
        ly: 0.25 * a.y + 0.5 * qy + 0.25 * ey,
        label: edgeLabel(e),
        width: 1 + ratio * 4,
        danger: errRate > 0.05,
        dim: hovered !== null && hovered !== e.from && hovered !== e.to,
        ax: ex,
        ay: ey,
        angle,
      }]
    })
  })

  const hoveredNode = $derived(hovered ? byName.get(hovered) ?? null : null)

  function errorRate(n: ServiceMapNode): number {
    return n.spans > 0 ? n.errors / n.spans : 0
  }

  // ── Edge table ───────────────────────────────────────────────
  type EdgeRow = Record<string, unknown> & ServiceMapEdge & { route: string }
  const edgeRows = $derived<EdgeRow[]>((map?.edges ?? []).map((e) => ({ ...e, route: `${e.from} → ${e.to}` })))
  const edgeColumns: DataColumn<EdgeRow>[] = [
    { key: 'route', label: 'Route', truncate: true },
    { key: 'calls', label: 'Calls', align: 'right', format: (v) => formatNumber(Number(v)) },
    { key: 'errors', label: 'Errors', align: 'right', format: (v) => formatNumber(Number(v)) },
    { key: 'p50_ms', label: 'p50', align: 'right', format: (v) => `${Number(v).toFixed(0)} ms` },
    { key: 'p95_ms', label: 'p95', align: 'right', format: (v) => `${Number(v).toFixed(0)} ms` },
  ]
</script>

<div class="flex h-full min-h-0 flex-col">
  <div class="flex h-10 shrink-0 items-center gap-2 border-b border-edge-subtle px-5">
    {#if traceSources.length > 1}
      <Select size="sm" class="w-48" options={traceSources.map((s) => ({ value: s.id, label: s.name }))} value={sourceId} onchange={(v) => { sourceId = v; void run() }} />
    {/if}
    <Tabs variant="segmented" size="sm" items={metricTabs} value={metric} onchange={setMetric} />
    <div class="flex-1"></div>
    {#if map}
      <span class="text-xs text-fg-4 tabular-nums">{map.nodes.length} services · {map.edges.length} routes</span>
    {/if}
    <TimeRangeSelector value={range} onchange={setRange} />
    <Button icon variant="ghost" size="sm" aria-label="Refresh" title="Refresh" onclick={() => void run()}>
      <RefreshCw size={14} class={loading ? 'animate-spin' : ''} />
    </Button>
    <Button icon variant="ghost" size="sm" aria-label={tablesOpen ? 'Hide routes table' : 'Show routes table'} title={tablesOpen ? 'Hide routes' : 'Show routes'} aria-pressed={tablesOpen} onclick={() => (tablesOpen = !tablesOpen)}>
      {#if tablesOpen}<PanelRightClose size={14} />{:else}<PanelRightOpen size={14} />{/if}
    </Button>
  </div>

  {#if traceSources.length === 0}
    <EmptyState
      icon={Waypoints}
      title="No traces source"
      description="The service map is built from spans. Add a traces source first."
      primary={{ label: 'Configure sources', onclick: () => setSection('sources') }}
    />
  {:else if error}
    <EmptyState icon={Waypoints} title="Could not build the service map" description={error} secondary={{ label: 'Retry', onclick: () => void run() }} />
  {:else}
    <div class="flex min-h-0 flex-1">
      <div class="relative min-h-0 min-w-0 flex-1" bind:clientWidth={width} bind:clientHeight={height}>
        {#if loading && !map}
          <div class="flex h-full items-center justify-center gap-2 text-[13px] text-fg-3"><Spinner size="sm" /> Building the map…</div>
        {:else if map && map.nodes.length === 0}
          <EmptyState icon={Waypoints} title="No spans in this range" description="Widen the time range or send some traces." />
        {:else if map}
          <svg class="h-full w-full select-none" viewBox={`0 0 ${width} ${height}`} role="img" aria-label="Service map">
            <defs>
              <marker id="svcmap-arrow" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="8" markerHeight="8" orient="auto-start-reverse">
                <path d="M 0 0 L 10 5 L 0 10 z" fill="var(--fg-4)" />
              </marker>
              <marker id="svcmap-arrow-danger" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="8" markerHeight="8" orient="auto-start-reverse">
                <path d="M 0 0 L 10 5 L 0 10 z" fill="var(--danger)" />
              </marker>
            </defs>

            {#each edges as e (e.from + '>' + e.to)}
              <g class="transition-opacity" style={`opacity:${e.dim ? 0.15 : 1}`}>
                <path
                  d={e.path}
                  fill="none"
                  stroke={e.danger ? 'var(--danger)' : 'var(--fg-4)'}
                  stroke-width={e.width}
                  stroke-linecap="round"
                  marker-end={e.danger ? 'url(#svcmap-arrow-danger)' : 'url(#svcmap-arrow)'}
                />
                <text
                  x={e.lx}
                  y={e.ly - 6}
                  text-anchor="middle"
                  font-size="10"
                  font-family="var(--font-sans)"
                  fill={e.danger ? 'var(--danger)' : 'var(--fg-3)'}
                  style="paint-order: stroke; stroke: var(--canvas); stroke-width: 3px; stroke-linejoin: round"
                >{e.label}</text>
              </g>
            {/each}

            {#each nodes as n (n.service)}
              {@const rate = errorRate(n)}
              {@const dim = hovered !== null && hovered !== n.service && !edges.some((e) => !e.dim && (e.from === n.service || e.to === n.service))}
              <!-- svelte-ignore a11y_no_static_element_interactions -->
              <g
                transform={`translate(${n.x - NODE_W / 2}, ${n.y - NODE_H / 2})`}
                class="cursor-pointer transition-opacity"
                style={`opacity:${dim ? 0.25 : 1}`}
                role="button"
                tabindex="0"
                aria-label={`${n.service}: ${formatNumber(n.spans)} spans, ${formatNumber(n.errors)} errors`}
                onmouseenter={() => (hovered = n.service)}
                onmouseleave={() => (hovered = null)}
                onfocus={() => (hovered = n.service)}
                onblur={() => (hovered = null)}
                onclick={() => openService(n.service)}
                onkeydown={(e) => { if (e.key === 'Enter') openService(n.service) }}
              >
                <rect
                  width={NODE_W}
                  height={NODE_H}
                  rx="6"
                  fill={`var(--${n.tone}-soft)`}
                  stroke={hovered === n.service ? `var(--${n.tone})` : 'var(--edge)'}
                  stroke-width={hovered === n.service ? 1.5 : 1}
                />
                <rect x="0" y="0" width="4" height={NODE_H} rx="2" fill={`var(--${n.tone})`} />
                <text x="14" y="18" font-size="12" font-weight="600" font-family="var(--font-sans)" fill="var(--fg)">
                  {n.service.length > 18 ? n.service.slice(0, 17) + '…' : n.service}
                </text>
                <text x="14" y="33" font-size="10" font-family="var(--font-sans)" fill="var(--fg-3)">
                  {formatNumber(n.spans)} spans · p95 {n.p95_ms.toFixed(0)} ms
                </text>
                <rect x="14" y="39" width={NODE_W - 28} height="3" rx="1.5" fill="var(--edge-subtle)" />
                <rect x="14" y="39" width={(NODE_W - 28) * Math.min(1, rate)} height="3" rx="1.5" fill={rate > 0.05 ? 'var(--danger)' : 'var(--success)'} />
              </g>
            {/each}
          </svg>

          {#if hoveredNode}
            <div
              class="pointer-events-none absolute z-10 rounded-md border border-edge bg-elevated px-2.5 py-2 text-xs shadow-[var(--shadow-popover)]"
              style={`left:${Math.min(width - 200, hoveredNode.x + NODE_W / 2 + 8)}px; top:${Math.max(8, hoveredNode.y - 40)}px`}
            >
              <div class="mb-1 font-semibold text-fg">{hoveredNode.service}</div>
              <div class="grid grid-cols-[auto_1fr] gap-x-3 gap-y-0.5 text-fg-3">
                <span>Spans</span><span class="text-right tabular-nums text-fg">{formatNumber(hoveredNode.spans)}</span>
                <span>Errors</span><span class="text-right tabular-nums {hoveredNode.errors > 0 ? 'text-danger' : 'text-fg'}">{formatNumber(hoveredNode.errors)} ({(errorRate(hoveredNode) * 100).toFixed(1)}%)</span>
                <span>p50</span><span class="text-right tabular-nums text-fg">{hoveredNode.p50_ms.toFixed(1)} ms</span>
                <span>p95</span><span class="text-right tabular-nums text-fg">{hoveredNode.p95_ms.toFixed(1)} ms</span>
              </div>
              <div class="mt-1 text-[11px] text-fg-4">Click to search its traces</div>
            </div>
          {/if}
        {/if}
      </div>

      {#if tablesOpen && map && map.edges.length > 0}
        <div class="flex w-[360px] shrink-0 flex-col border-l border-edge-subtle">
          <div class="flex h-9 shrink-0 items-center px-3 text-xs font-medium text-fg-3">Routes</div>
          <div class="min-h-0 flex-1">
            <DataTable fill columns={edgeColumns} rows={edgeRows} rowKey={(r) => r.route} sort={{ key: metric === 'p95' ? 'p95_ms' : metric, dir: 'desc' }} emptyTitle="No routes" />
          </div>
        </div>
      {/if}
    </div>
  {/if}
</div>
