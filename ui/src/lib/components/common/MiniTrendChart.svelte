<script lang="ts">
  import { onMount } from 'svelte'
  import uPlot from 'uplot'
  import 'uplot/dist/uPlot.min.css'

  interface Props {
    x: number[]
    y: number[]
    color?: string
    fill?: string
    height?: number
    strokeWidth?: number
  }

  let { x, y, color = '#f97316', fill = 'rgba(249, 115, 22, 0.16)', height = 120, strokeWidth = 2 }: Props = $props()

  let container: HTMLDivElement
  let chart: uPlot | null = null

  function destroyChart() {
    if (chart) {
      chart.destroy()
      chart = null
    }
  }

  function draw() {
    destroyChart()
    if (!container) return
    if (!x || !y || x.length === 0 || y.length === 0) return

    const opts: uPlot.Options = {
      width: container.clientWidth || 320,
      height,
      legend: { show: false },
      cursor: { show: false },
      axes: [
        { show: false },
        { show: false },
      ],
      scales: {
        x: { time: false },
      },
      series: [
        {},
        {
          stroke: color,
          width: strokeWidth,
          fill,
          points: { show: false },
        },
      ],
      padding: [4, 6, 4, 6],
    }

    chart = new uPlot(opts, [x, y], container)
  }

  onMount(() => {
    draw()
    const resize = () => draw()
    window.addEventListener('resize', resize)
    return () => {
      window.removeEventListener('resize', resize)
      destroyChart()
    }
  })

  $effect(() => {
    x
    y
    draw()
  })
</script>

<div bind:this={container} class="w-full min-h-[100px]"></div>
