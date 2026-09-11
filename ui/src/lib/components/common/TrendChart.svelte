<script lang="ts">
  import { onMount } from 'svelte'
  import uPlot from 'uplot'
  import 'uplot/dist/uPlot.min.css'
  import { getTheme } from '../../stores/theme.svelte'
  import { tooltipPlugin, chartTheme } from '../../utils/uplot-tooltip'

  export interface TrendSeries {
    label: string
    /** Aligned to x; null renders as a gap in the line. */
    values: (number | null)[]
    color: string
    fill?: string
  }

  interface Props {
    /** Unix timestamps (seconds). */
    x: number[]
    series: TrendSeries[]
    height?: number
    /** Y value formatter for axis ticks and the tooltip. Put the unit here ("12s", "4.1 MB"). */
    formatY?: (v: number) => string
    /** Show the series legend above the chart. Default: only when there is more than one series. */
    legend?: boolean
    class?: string
  }

  let { x, series, height = 150, formatY = (v) => String(v), legend, class: cls = '' }: Props = $props()

  let container: HTMLDivElement
  let chart: uPlot | null = null

  const showLegend = $derived(legend ?? series.length > 1)

  function destroyChart() {
    if (chart) {
      chart.destroy()
      chart = null
    }
  }

  function formatTime(v: number): string {
    return new Date(v * 1000).toLocaleString(undefined, {
      month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit',
    })
  }

  function draw() {
    destroyChart()
    if (!container) return
    if (!x || x.length === 0 || series.length === 0) return

    const { axis, grid } = chartTheme(container)
    const fmt = formatY

    const opts: uPlot.Options = {
      width: container.clientWidth || 480,
      height,
      legend: { show: false },
      cursor: {
        points: { show: true, size: 6, width: 1.5 },
        x: true,
        y: false,
      },
      plugins: [tooltipPlugin({ formatX: formatTime, formatValue: (v) => fmt(v) })],
      scales: { x: { time: true } },
      axes: [
        {
          stroke: axis,
          grid: { show: true, stroke: grid, width: 1 },
          ticks: { show: false },
          font: '11px var(--font-sans)',
          space: 80,
        },
        {
          stroke: axis,
          grid: { show: true, stroke: grid, width: 1 },
          ticks: { show: false },
          font: '11px var(--font-sans)',
          size: (_u, values) => {
            if (!values || values.length === 0) return 40
            const maxLen = Math.max(...values.map((v) => v.length))
            return Math.max(36, maxLen * 6.5 + 12)
          },
          values: (_u, ticks) => ticks.map((v) => fmt(v)),
        },
      ],
      series: [
        { label: 'Time' },
        ...series.map((s) => ({
          label: s.label,
          stroke: s.color,
          width: 1.5,
          fill: s.fill,
          points: { show: false },
        })),
      ],
      padding: [8, 12, 0, 0],
    }

    chart = new uPlot(opts, [x, ...series.map((s) => s.values)], container)
  }

  onMount(() => {
    draw()
    const observer = new ResizeObserver(() => {
      if (chart && container) chart.setSize({ width: container.clientWidth, height })
    })
    observer.observe(container)
    return () => {
      observer.disconnect()
      destroyChart()
    }
  })

  $effect(() => {
    x
    series
    getTheme()
    draw()
  })
</script>

<div class={cls}>
  {#if showLegend}
    <div class="mb-1.5 flex flex-wrap items-center gap-x-3 gap-y-1 text-[11px] text-fg-3">
      {#each series as s (s.label)}
        <span class="inline-flex items-center gap-1.5">
          <span class="h-1.5 w-1.5 rounded-[2px]" style="background: {s.color}"></span>
          {s.label}
        </span>
      {/each}
    </div>
  {/if}
  <div bind:this={container} class="w-full" style="min-height:{height}px"></div>
</div>
