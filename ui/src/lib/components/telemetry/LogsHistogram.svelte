<script lang="ts">
  import { onMount } from 'svelte'
  import uPlot from 'uplot'
  import 'uplot/dist/uPlot.min.css'
  import { getTheme } from '../../stores/theme.svelte'
  import { tooltipPlugin, chartTheme } from '../../utils/uplot-tooltip'
  import type { HistogramBucket } from '../../types/telemetry'
  import { severityVar, normalizeSeverity } from './severity'
  import { SEVERITY_LEVELS } from '../../types/telemetry'

  /** Stacked bars per severity. Drag on the chart to narrow the range. */
  interface Props {
    buckets: HistogramBucket[]
    bucketSeconds: number
    height?: number
    onrange?: (fromISO: string, toISO: string) => void
  }

  let { buckets, bucketSeconds, height = 120, onrange }: Props = $props()

  let container: HTMLDivElement
  let chart: uPlot | null = null

  function resolve(name: string): string {
    return getComputedStyle(container).getPropertyValue(name).trim() || '#888'
  }

  const levels = $derived.by(() => {
    const present = new Set<string>()
    for (const b of buckets) for (const k of Object.keys(b.counts)) present.add(normalizeSeverity(k))
    const ordered = SEVERITY_LEVELS.filter((l) => present.has(l))
    for (const p of present) if (!ordered.includes(p as never)) ordered.push(p as never)
    return ordered
  })

  function draw() {
    if (chart) {
      chart.destroy()
      chart = null
    }
    if (!container || buckets.length === 0) return
    const { axis, grid } = chartTheme(container)
    const x = buckets.map((b) => Math.floor(new Date(b.t).getTime() / 1000))
    // Stack: each series value = its count + everything below it.
    const raw = levels.map((lvl) => buckets.map((b) => {
      let n = 0
      for (const [k, v] of Object.entries(b.counts)) if (normalizeSeverity(k) === lvl) n += v
      return n
    }))
    const stacked = raw.map((_, i) => raw[i].map((_, j) => raw.slice(0, i + 1).reduce((s, r) => s + r[j], 0)))
    const bars = uPlot.paths.bars!({ size: [0.8, 40], align: 1 })
    const opts: uPlot.Options = {
      width: container.clientWidth || 600,
      height,
      legend: { show: false },
      cursor: { drag: { x: true, y: false }, points: { show: false } },
      plugins: [tooltipPlugin({
        formatX: (v) => new Date(v * 1000).toLocaleString(undefined, { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit', second: '2-digit' }),
        formatValue: (v, i) => {
          // Un-stack for the tooltip: show the series' own count.
          const idx = chart?.cursor.idx ?? 0
          const own = raw[i - 1]?.[idx] ?? v
          return String(own)
        },
      })],
      scales: { x: { time: true }, y: { range: (_u, _min, max) => [0, Math.max(1, max * 1.05)] } },
      axes: [
        { stroke: axis, grid: { show: false }, ticks: { show: false }, font: '11px var(--font-sans)', space: 90 },
        { stroke: axis, grid: { show: true, stroke: grid, width: 1 }, ticks: { show: false }, font: '11px var(--font-sans)', size: 40 },
      ],
      // Draw the tallest stack first so smaller ones paint on top.
      series: [
        { label: 'Time' },
        ...[...levels].reverse().map((lvl) => ({
          label: lvl,
          stroke: resolve(severityVar(lvl)),
          fill: resolve(severityVar(lvl)),
          width: 0,
          paths: bars,
          points: { show: false },
        })),
      ],
      padding: [6, 8, 0, 0],
      hooks: {
        setSelect: [(u) => {
          if (!onrange || u.select.width < 4) return
          const from = u.posToVal(u.select.left, 'x')
          const to = u.posToVal(u.select.left + u.select.width, 'x')
          u.setSelect({ left: 0, width: 0, top: 0, height: 0 }, false)
          onrange(new Date(from * 1000).toISOString(), new Date((to + bucketSeconds) * 1000).toISOString())
        }],
      },
    }
    // Series are reversed for painting order; data must follow.
    chart = new uPlot(opts, [x, ...[...stacked].reverse()], container)
  }

  onMount(() => {
    draw()
    const ro = new ResizeObserver(() => {
      if (chart && container) chart.setSize({ width: container.clientWidth, height })
    })
    ro.observe(container)
    return () => {
      ro.disconnect()
      chart?.destroy()
    }
  })

  $effect(() => {
    buckets
    getTheme()
    draw()
  })
</script>

<div class="w-full" style="min-height:{height}px">
  <div bind:this={container} class="w-full"></div>
  {#if buckets.length === 0}
    <div class="flex items-center justify-center text-xs text-fg-4" style="height:{height}px">No data in this range</div>
  {/if}
</div>
