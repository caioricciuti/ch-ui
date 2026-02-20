<script lang="ts">
  import { onMount, onDestroy } from 'svelte'
  import uPlot from 'uplot'
  import 'uplot/dist/uPlot.min.css'
  import type { PanelConfig } from '../../types/api'
  import { toUPlotData, DEFAULT_COLORS, isDateType, type ColumnMeta } from '../../utils/chart-transform'
  import { getTheme } from '../../stores/theme.svelte'

  interface Props {
    data: Record<string, unknown>[]
    meta: ColumnMeta[]
    config: PanelConfig
  }

  let { data, meta, config }: Props = $props()

  let container: HTMLDivElement
  let chart: uPlot | undefined
  let themeObserver: MutationObserver | undefined
  let measuredWidth = $state(0)
  let measuredHeight = $state(0)

  const plotData = $derived(toUPlotData(data, meta, config))

  function isDark(): boolean {
    return getTheme() === 'dark'
  }

  function axisColor(): string {
    return isDark() ? '#6b7280' : '#9ca3af'
  }

  function gridColor(): string {
    return isDark() ? 'rgba(75,85,99,0.3)' : 'rgba(209,213,219,0.5)'
  }

  function tooltipPlugin(isTime: boolean): uPlot.Plugin {
    let tooltip: HTMLDivElement

    function init(u: uPlot) {
      tooltip = document.createElement('div')
      Object.assign(tooltip.style, {
        position: 'absolute',
        display: 'none',
        pointerEvents: 'none',
        background: 'rgba(24,24,27,0.94)',
        color: '#f3f4f6',
        borderRadius: '6px',
        padding: '8px 10px',
        fontSize: '11px',
        lineHeight: '1.5',
        boxShadow: '0 4px 12px rgba(0,0,0,0.3)',
        zIndex: '100',
        whiteSpace: 'nowrap',
        fontFamily: 'ui-monospace, monospace',
      })
      u.over.appendChild(tooltip)

      u.over.addEventListener('mouseleave', () => { tooltip.style.display = 'none' })
      u.over.addEventListener('mouseenter', () => { tooltip.style.display = 'block' })
    }

    function setCursor(u: uPlot) {
      const { idx, left, top } = u.cursor
      if (idx == null || left == null || top == null) {
        tooltip.style.display = 'none'
        return
      }

      const xVal = u.data[0][idx]
      const header = isTime
        ? new Date(xVal * 1000).toLocaleString()
        : xVal.toLocaleString()

      let rows = ''
      for (let i = 1; i < u.series.length; i++) {
        const s = u.series[i]
        if (!s.show) continue
        const val = u.data[i][idx]
        const display = val == null
          ? '\u2014'
          : Number(val).toLocaleString(undefined, { maximumFractionDigits: 2 })
        const color = typeof s.stroke === 'function' ? (s.stroke as Function)(u, i) : s.stroke
        rows += `<div style="display:flex;align-items:center;gap:6px;">` +
          `<span style="width:8px;height:8px;border-radius:50%;background:${color};flex-shrink:0;"></span>` +
          `<span style="color:#a1a1aa;flex:1;">${s.label ?? ''}</span>` +
          `<span style="font-weight:600;margin-left:12px;">${display}</span>` +
          `</div>`
      }

      tooltip.innerHTML =
        `<div style="font-weight:600;margin-bottom:4px;color:#e4e4e7;">${header}</div>${rows}`

      const ow = u.over.clientWidth
      const tw = tooltip.offsetWidth
      const th = tooltip.offsetHeight
      const pad = 10

      let x = left + pad
      let y = top - th - pad

      if (x + tw > ow) x = left - tw - pad
      if (y < 0) y = top + pad

      tooltip.style.left = x + 'px'
      tooltip.style.top = y + 'px'
      tooltip.style.display = 'block'
    }

    return { hooks: { init, setCursor } }
  }

  function buildOpts(w: number, h: number): uPlot.Options {
    const xMeta = meta.find(m => m.name === config.xColumn)
    const isTime = xMeta ? isDateType(xMeta.type) : false
    const colors = config.colors?.length ? config.colors : DEFAULT_COLORS

    const series: uPlot.Series[] = [
      { label: config.xColumn ?? 'X' },
    ]

    const yColumns = config.yColumns ?? []
    for (let i = 0; i < yColumns.length; i++) {
      const color = colors[i % colors.length]
      const s: uPlot.Series = {
        label: yColumns[i],
        stroke: color,
        width: 2,
      }

      if (config.chartType === 'bar') {
        s.fill = color + '80'
        s.paths = uPlot.paths.bars!({ size: [0.6, 100] })
      } else {
        s.fill = color + '1A'
      }

      series.push(s)
    }

    return {
      width: w,
      height: h,
      series,
      plugins: [tooltipPlugin(isTime)],
      axes: [
        {
          stroke: axisColor(),
          grid: { stroke: gridColor(), width: 1 },
          ticks: { stroke: gridColor(), width: 1 },
          ...(isTime ? {} : { values: (_u: uPlot, vals: number[]) => vals.map(v => String(v)) }),
        },
        {
          stroke: axisColor(),
          grid: { stroke: gridColor(), width: 1 },
          ticks: { stroke: gridColor(), width: 1 },
        },
      ],
      scales: {
        x: isTime ? { time: true } : { time: false },
      },
      legend: { show: false },
      cursor: { drag: { x: true, y: false } },
    }
  }

  function createChart() {
    if (chart) {
      chart.destroy()
      chart = undefined
    }
    if (!container || !plotData[0]?.length || measuredWidth <= 0 || measuredHeight <= 0) return

    const opts = buildOpts(measuredWidth, measuredHeight)
    chart = new uPlot(opts, plotData, container)
  }

  // ResizeObserver — measure container, use setSize for efficient resize
  $effect(() => {
    if (!container) return
    const ro = new ResizeObserver(entries => {
      const { width: w, height: h } = entries[0].contentRect
      const fw = Math.floor(w)
      const fh = Math.floor(h)
      if (fw > 0 && fh > 0 && (fw !== measuredWidth || fh !== measuredHeight)) {
        measuredWidth = fw
        measuredHeight = fh
      }
    })
    ro.observe(container)
    return () => ro.disconnect()
  })

  // Resize chart efficiently when container dimensions change
  $effect(() => {
    if (!chart || measuredWidth <= 0 || measuredHeight <= 0) return
    chart.setSize({ width: measuredWidth, height: measuredHeight })
  })

  // Recreate chart when data/config changes (not dimensions)
  $effect(() => {
    void plotData
    void config.chartType
    void config.xColumn
    void config.yColumns
    void config.colors

    if (container && measuredWidth > 0 && measuredHeight > 0) {
      createChart()
    }
  })

  onMount(() => {
    themeObserver = new MutationObserver(() => {
      createChart()
    })
    themeObserver.observe(document.documentElement, { attributes: true, attributeFilter: ['class'] })
  })

  onDestroy(() => {
    themeObserver?.disconnect()
    chart?.destroy()
  })
</script>

<div bind:this={container} class="w-full h-full"></div>
