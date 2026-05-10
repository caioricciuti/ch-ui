<script lang="ts">
  import { onMount, onDestroy } from 'svelte'
  import uPlot from 'uplot'
  import 'uplot/dist/uPlot.min.css'
  import type { PanelConfig } from '../../types/api'
  import { toUPlotData, DEFAULT_COLORS, isDateType, isNumericType, type ColumnMeta } from '../../utils/chart-transform'
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

  const isCategorical = $derived.by(() => {
    const xMeta = meta.find(m => m.name === config.xColumn)
    if (!xMeta) return false
    return !isDateType(xMeta.type) && !isNumericType(xMeta.type)
  })

  const xLabels = $derived(
    isCategorical ? data.map(row => String(row[config.xColumn!] ?? '')) : []
  )

  function isDark(): boolean {
    return getTheme() === 'dark'
  }

  function axisColor(): string {
    return isDark() ? '#6b7280' : '#9ca3af'
  }

  function gridColor(): string {
    return isDark() ? 'rgba(75,85,99,0.3)' : 'rgba(209,213,219,0.5)'
  }

  function tooltipPlugin(isTime: boolean, isCat: boolean, catLabels: string[]): uPlot.Plugin {
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
        : isCat
          ? (catLabels[idx] ?? String(xVal))
          : xVal.toLocaleString()

      tooltip.textContent = ''
      const headerDiv = document.createElement('div')
      Object.assign(headerDiv.style, { fontWeight: '600', marginBottom: '4px', color: '#e4e4e7' })
      headerDiv.textContent = String(header)
      tooltip.appendChild(headerDiv)

      for (let i = 1; i < u.series.length; i++) {
        const s = u.series[i]
        if (!s.show) continue
        const val = u.data[i][idx]
        const display = val == null
          ? '—'
          : Number(val).toLocaleString(undefined, { maximumFractionDigits: 2 })
        const color = typeof s.stroke === 'function' ? (s.stroke as Function)(u, i) : s.stroke

        const row = document.createElement('div')
        Object.assign(row.style, { display: 'flex', alignItems: 'center', gap: '6px' })

        const dot = document.createElement('span')
        Object.assign(dot.style, { width: '8px', height: '8px', borderRadius: '50%', background: String(color ?? ''), flexShrink: '0' })
        row.appendChild(dot)

        const label = document.createElement('span')
        Object.assign(label.style, { color: '#a1a1aa', flex: '1' })
        label.textContent = String(s.label ?? '')
        row.appendChild(label)

        const value = document.createElement('span')
        Object.assign(value.style, { fontWeight: '600', marginLeft: '12px' })
        value.textContent = display
        row.appendChild(value)

        tooltip.appendChild(row)
      }

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

  function groupedBarPaths(seriesIdx: number, totalSeries: number): uPlot.Series.PathBuilder {
    return (u, sidx, i0, i1) => {
      const fill = new Path2D()
      const stroke = new Path2D()

      const n = u.data[0].length
      if (n === 0) return { fill, stroke }

      let slotPx: number
      if (n > 1) {
        const p0 = u.valToPos(u.data[0][0], 'x', true)
        const p1 = u.valToPos(u.data[0][1], 'x', true)
        slotPx = Math.abs(p1 - p0)
      } else {
        slotPx = u.over.clientWidth * 0.8
      }

      const groupPx = slotPx * 0.8
      const barPx = Math.max(2, (groupPx / totalSeries) - 1)
      const offset = totalSeries === 1
        ? -barPx / 2
        : -groupPx / 2 + seriesIdx * (groupPx / totalSeries) + 0.5

      const zeroY = u.valToPos(0, 'y', true)

      for (let i = i0; i <= i1; i++) {
        const yVal = u.data[sidx][i]
        if (yVal == null) continue

        const cx = u.valToPos(u.data[0][i], 'x', true)
        const yPos = u.valToPos(yVal as number, 'y', true)

        const x = cx + offset
        const y = Math.min(yPos, zeroY)
        const h = Math.abs(zeroY - yPos)

        if (barPx > 0 && h > 0) {
          fill.rect(x, y, barPx, h)
          stroke.rect(x, y, barPx, h)
        }
      }

      return { fill, stroke }
    }
  }

  function buildOpts(w: number, h: number): uPlot.Options {
    const xMeta = meta.find(m => m.name === config.xColumn)
    const isTime = xMeta ? isDateType(xMeta.type) : false
    const isCat = isCategorical
    const catLabels = xLabels
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
        s.paths = groupedBarPaths(i, yColumns.length)
      } else {
        s.fill = color + '1A'
      }

      series.push(s)
    }

    let xAxisConfig: Record<string, any> = {}
    if (isTime) {
      // default time formatting
    } else if (isCat) {
      xAxisConfig = {
        splits: (_u: uPlot) => catLabels.map((_: string, i: number) => i),
        values: (_u: uPlot, splits: number[]) => splits.map(i => catLabels[i] ?? ''),
        gap: 8,
        size: catLabels.length > 6 ? 60 : 40,
        rotate: catLabels.length > 6 ? -45 : 0,
      }
    } else {
      xAxisConfig = {
        values: (_u: uPlot, vals: number[]) => vals.map(v => String(v)),
      }
    }

    return {
      width: w,
      height: h,
      series,
      plugins: [tooltipPlugin(isTime, isCat, catLabels)],
      axes: [
        {
          stroke: axisColor(),
          grid: { stroke: gridColor(), width: 1 },
          ticks: { stroke: gridColor(), width: 1 },
          ...xAxisConfig,
        },
        {
          stroke: axisColor(),
          grid: { stroke: gridColor(), width: 1 },
          ticks: { stroke: gridColor(), width: 1 },
        },
      ],
      scales: {
        x: isTime
          ? { time: true }
          : isCat
            ? { time: false, distr: 2 }
            : { time: false },
      },
      legend: { show: false },
      cursor: { drag: { x: true, y: false } },
    }
  }

  function destroyChart() {
    if (chart) {
      chart.destroy()
      chart = undefined
    }
  }

  function createChart() {
    destroyChart()
    if (!container || !plotData[0]?.length || measuredWidth <= 0 || measuredHeight <= 0) return

    const opts = buildOpts(measuredWidth, measuredHeight)
    chart = new uPlot(opts, plotData, container)
  }

  // Measure container using clientWidth/clientHeight (accounts for padding)
  $effect(() => {
    if (!container) return
    const ro = new ResizeObserver(() => {
      const w = container.clientWidth
      const h = container.clientHeight
      if (w > 0 && h > 0 && (w !== measuredWidth || h !== measuredHeight)) {
        measuredWidth = w
        measuredHeight = h
      }
    })
    ro.observe(container)
    return () => ro.disconnect()
  })

  // Resize chart efficiently when only container dimensions change
  $effect(() => {
    if (!chart || measuredWidth <= 0 || measuredHeight <= 0) return
    chart.setSize({ width: measuredWidth, height: measuredHeight })
  })

  // Recreate chart when data or config changes (not dimensions)
  let recreateTimer: ReturnType<typeof setTimeout> | undefined

  $effect(() => {
    void plotData
    void config.chartType
    void config.xColumn
    void config.yColumns
    void config.colors

    if (!container || measuredWidth <= 0 || measuredHeight <= 0) return

    clearTimeout(recreateTimer)
    recreateTimer = setTimeout(() => createChart(), 16)
  })

  onMount(() => {
    themeObserver = new MutationObserver(() => {
      createChart()
    })
    themeObserver.observe(document.documentElement, { attributes: true, attributeFilter: ['class'] })
  })

  onDestroy(() => {
    clearTimeout(recreateTimer)
    themeObserver?.disconnect()
    destroyChart()
  })
</script>

<div bind:this={container} class="w-full h-full overflow-hidden"></div>
