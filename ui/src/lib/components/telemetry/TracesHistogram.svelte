<script lang="ts">
  import { onMount } from 'svelte'
  import uPlot from 'uplot'
  import 'uplot/dist/uPlot.min.css'
  import { getTheme } from '../../stores/theme.svelte'
  import { tooltipPlugin, chartTheme } from '../../utils/uplot-tooltip'
  import type { TraceHistogramBucket } from '../../types/telemetry'
  import { formatDuration } from './services'
  import { tracesHistogramData, histogramSelection, histogramIndexAt, formatHistogramInterval, type HistogramRange } from './histogram'

  /** Traces per bucket (errors stacked in danger) with p50/p95 lines on a second axis. */
  interface Props {
    buckets: TraceHistogramBucket[]
    bucketSeconds: number
    range?: HistogramRange
    height?: number
    onrange?: (fromISO: string, toISO: string) => void
  }

  let { buckets, bucketSeconds, range, height = 120, onrange }: Props = $props()

  let container: HTMLDivElement
  let chart: uPlot | null = null

  function resolve(name: string): string {
    return getComputedStyle(container).getPropertyValue(name).trim() || '#888'
  }

  function draw() {
    if (chart) {
      chart.destroy()
      chart = null
    }
    if (!container || buckets.length === 0) return
    const { axis, grid } = chartTheme(container)
    const plot = tracesHistogramData(buckets, bucketSeconds, range)
    const bars = uPlot.paths.bars!({
      size: [1, Infinity, 0], align: 1, gap: 1,
      disp: {
        x0: { unit: 1, values: () => plot.starts },
        size: { unit: 1, values: () => [plot.step] },
      },
    })
    const opts: uPlot.Options = {
      width: container.clientWidth || 600,
      height,
      legend: { show: false },
      cursor: { drag: { x: true, y: false }, points: { show: false } },
      plugins: [tooltipPlugin({
        dataIndex: (u) => histogramIndexAt(plot.intervals, u.posToVal(u.cursor.left ?? -1, 'x')),
        formatX: (_v, idx) => formatHistogramInterval(plot.intervals[idx]),
        formatValue: (v, i) => (i >= 3 ? formatDuration(v) : String(v)),
      })],
      scales: {
        x: { time: true, range: () => plot.bounds },
        y: { range: (_u, _min, max) => [0, Math.max(1, max * 1.05)] },
        ms: { range: (_u, _min, max) => [0, Math.max(1, max * 1.1)] },
      },
      axes: [
        { stroke: axis, grid: { show: false }, ticks: { show: false }, font: '11px var(--font-sans)', space: 90 },
        { stroke: axis, grid: { show: true, stroke: grid, width: 1 }, ticks: { show: false }, font: '11px var(--font-sans)', size: 40 },
        { scale: 'ms', side: 1, stroke: axis, grid: { show: false }, ticks: { show: false }, font: '11px var(--font-sans)', size: 56, values: (_u, vals) => vals.map((v) => formatDuration(v)) },
      ],
      series: [
        { label: 'Time' },
        { label: 'Traces', stroke: resolve('--accent'), fill: resolve('--accent'), width: 0, paths: bars, points: { show: false } },
        { label: 'Errors', stroke: resolve('--danger'), fill: resolve('--danger'), width: 0, paths: bars, points: { show: false } },
        { label: 'p50', scale: 'ms', stroke: resolve('--info'), width: 1.5, points: { show: false } },
        { label: 'p95', scale: 'ms', stroke: resolve('--warning'), width: 1.5, points: { show: false } },
      ],
      padding: [6, 8, 0, 0],
      hooks: {
        setSelect: [(u) => {
          if (!onrange || u.select.width < 4) return
          const from = u.posToVal(u.select.left, 'x')
          const to = u.posToVal(u.select.left + u.select.width, 'x')
          u.setSelect({ left: 0, width: 0, top: 0, height: 0 }, false)
          const selected = histogramSelection(from, to, plot.bounds)
          if (selected) onrange(selected.from, selected.to)
        }],
      },
    }
    chart = new uPlot(opts, [plot.x, plot.total, plot.errors, plot.p50, plot.p95], container)
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
