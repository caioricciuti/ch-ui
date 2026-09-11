<script lang="ts">
  import { onMount } from 'svelte'
  import uPlot from 'uplot'
  import 'uplot/dist/uPlot.min.css'
  import { getTheme } from '../../stores/theme.svelte'
  import { tooltipPlugin } from '../../utils/uplot-tooltip'

  /** A sparkline with a tooltip: no axes, one series. */
  interface Props {
    x: number[]
    y: number[]
    /** Name shown in the tooltip row. */
    label?: string
    /** Header text for a hovered point; defaults to the raw x value. */
    formatX?: (xVal: number, idx: number) => string
    formatY?: (v: number) => string
    color?: string
    fill?: string
    height?: number
    strokeWidth?: number
  }

  let {
    x, y, label = 'Value',
    formatX = (v) => String(v),
    formatY = (v) => v.toLocaleString(),
    color = 'var(--accent)', fill = 'var(--accent-soft)', height = 120, strokeWidth = 1.5,
  }: Props = $props()

  let container: HTMLDivElement
  let chart: uPlot | null = null

  function destroyChart() {
    if (chart) {
      chart.destroy()
      chart = null
    }
  }

  // uPlot draws on canvas, which cannot read CSS variables; resolve them once.
  function resolve(value: string): string {
    if (!value.startsWith('var(')) return value
    const name = value.slice(4, -1).trim()
    return getComputedStyle(container).getPropertyValue(name).trim() || value
  }

  function draw() {
    destroyChart()
    if (!container) return
    if (!x || !y || x.length === 0 || y.length === 0) return

    const opts: uPlot.Options = {
      width: container.clientWidth || 320,
      height,
      legend: { show: false },
      cursor: { points: { show: true, size: 6, width: 1.5 }, x: true, y: false },
      plugins: [tooltipPlugin({ formatX, formatValue: (v) => formatY(v) })],
      axes: [{ show: false }, { show: false }],
      scales: { x: { time: false } },
      series: [
        { label: 'x' },
        { label, stroke: resolve(color), width: strokeWidth, fill: resolve(fill), points: { show: false } },
      ],
      padding: [4, 6, 4, 6],
    }

    chart = new uPlot(opts, [x, y], container)
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
    y
    getTheme()
    draw()
  })
</script>

<div bind:this={container} class="w-full" style="min-height:{height}px"></div>
