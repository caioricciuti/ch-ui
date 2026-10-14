<script lang="ts">
  import { onMount, onDestroy } from 'svelte'
  import uPlot from 'uplot'
  import 'uplot/dist/uPlot.min.css'
  import type { PanelConfig } from '../../types/api'
  import {
    toUPlotData, DEFAULT_COLORS, isDateType, isCategoricalColumn, resolveChartColumns, pivotSeries, stackSeries,
    histogramBins, resolveHistogramColumn, toScatterData, plotPoints, type ColumnMeta,
  } from '../../utils/chart-transform'
  import { formatNumber } from '../../utils/format'
  import { chartTheme, tooltipPlugin } from '../../utils/uplot-tooltip'
  import { clickPlugin, type ChartPoint } from '../../utils/uplot-click'
  import { buildClickContext, bucketStep, bucketWindow, type ClickContext, type PanelClick } from '../../utils/drilldown'

  interface Props {
    data: Record<string, unknown>[]
    meta: ColumnMeta[]
    config: PanelConfig
    /** Panel type; histogram and scatter draw their own data. Defaults to the config's chart type. */
    type?: string
    /** A click on a point; set only when the panel has links. */
    onpointclick?: (click: PanelClick) => void
  }

  let { data, meta, config, type, onpointclick }: Props = $props()

  const kind = $derived(type ?? config.chartType)
  const isHistogram = $derived(kind === 'histogram')
  const isScatter = $derived(kind === 'scatter')
  const isBar = $derived(!isHistogram && !isScatter && config.chartType === 'bar')
  // Stacked areas: time series only. Saved panels without the field draw as before.
  const isStacked = $derived(!isHistogram && !isScatter && !isBar && config.stacked === true)

  let container: HTMLDivElement
  let chart: uPlot | undefined
  let themeObserver: MutationObserver | undefined
  let measuredWidth = $state(0)
  let measuredHeight = $state(0)

  // A series column splits the first y column into one series per value:
  // the long rows are pivoted to wide rows before anything is plotted.
  const pivot = $derived.by(() => {
    if (isHistogram || isScatter) return null
    const series = config.seriesColumn
    if (!series || !meta.some(m => m.name === series)) return null
    const base = resolveChartColumns(meta, config)
    if (!base.xColumn || base.yColumns.length === 0) return null
    return pivotSeries(data, meta, base.xColumn, base.yColumns[0], series)
  })
  const plotRows = $derived(pivot?.rows ?? data)
  const plotMeta = $derived(pivot?.meta ?? meta)
  const plotConfig = $derived<PanelConfig>(
    pivot ? { ...config, seriesColumn: undefined, yColumns: pivot.yColumns } : config,
  )

  // Axes as plotted: saved columns, or a sensible pick when none are saved.
  const axes = $derived(resolveChartColumns(plotMeta, plotConfig))

  const histogram = $derived.by(() => {
    if (!isHistogram) return null
    const column = resolveHistogramColumn(meta, config)
    return column ? { column, ...histogramBins(data, column, config.histogramBuckets) } : null
  })
  const scatter = $derived(isScatter ? toScatterData(data, meta, config) : null)

  // What uPlot draws: x, then one array per series, with the series labels.
  const plot = $derived.by<{ data: uPlot.AlignedData; labels: string[] }>(() => {
    if (histogram) {
      const { edges, counts } = histogram
      const centers = new Float64Array(counts.length)
      for (let i = 0; i < counts.length; i++) centers[i] = (edges[i] + edges[i + 1]) / 2
      return { data: [centers, counts], labels: ['count'] }
    }
    if (scatter) return { data: scatter.data, labels: scatter.labels }
    if (isHistogram || isScatter) return { data: [new Float64Array(0)], labels: [] }
    return { data: toUPlotData(plotRows, plotMeta, plotConfig), labels: axes.yColumns }
  })
  const plotData = $derived(plot.data)

  const isCategorical = $derived(!isHistogram && !isScatter && isCategoricalColumn(plotMeta, axes.xColumn))
  const isTime = $derived.by(() => {
    if (histogram) return false
    if (scatter) return scatter.xIsTime
    const xMeta = plotMeta.find(m => m.name === axes.xColumn)
    return xMeta ? isDateType(xMeta.type) : false
  })

  const xLabels = $derived(
    isCategorical ? plotRows.map(row => String(row[axes.xColumn ?? ''] ?? '')) : []
  )

  // Legend: our own token-styled row (or column), never the uPlot legend.
  const legendPosition = $derived(config.legendPosition ?? 'bottom')
  const seriesColors = $derived(config.colors?.length ? config.colors : DEFAULT_COLORS)
  // A histogram has one series, the bucket counts: nothing to tell apart.
  const legendItems = $derived(
    isHistogram ? [] : plot.labels.map((label, i) => ({ label, color: seriesColors[i % seriesColors.length] })),
  )
  let hiddenSeries = $state<Set<string>>(new Set())
  // A string, so a refresh with the same series does not count as a change.
  const seriesKey = $derived(plot.labels.join('\u0000'))

  // A different set of series starts with every series visible again.
  $effect(() => {
    void seriesKey
    hiddenSeries = new Set()
  })

  function toggleSeries(index: number, label: string) {
    const next = new Set(hiddenSeries)
    if (next.has(label)) next.delete(label)
    else next.add(label)
    hiddenSeries = next
    // A stack is summed without the hidden series, so it is drawn again.
    if (isStacked) createChart()
    else chart?.setSeries(index + 1, { show: !next.has(label) })
  }

  /** The data as drawn: running totals for a stacked chart, hidden series left out. */
  function drawData(): uPlot.AlignedData {
    if (!isStacked) return plotData
    const skip = new Set<number>()
    plot.labels.forEach((label, i) => {
      if (hiddenSeries.has(label)) skip.add(i + 1)
    })
    return stackSeries(plotData, skip)
  }

  // Scatter points: circles, sized by the size column (square root, so area
  // follows the value) or all the same size.
  function scatterPaths(sizes: (number | null)[] | null): uPlot.Series.PathBuilder {
    let lo = Infinity
    let hi = -Infinity
    for (const v of sizes ?? []) {
      if (v == null) continue
      lo = Math.min(lo, v)
      hi = Math.max(hi, v)
    }
    const radius = (i: number): number => {
      if (!sizes) return 3
      const v = sizes[i]
      if (v == null) return 2
      if (hi === lo) return 5
      return 2 + 10 * Math.sqrt((v - lo) / (hi - lo))
    }
    return (u, sidx, i0, i1) => {
      const fill = new Path2D()
      const xs = u.data[0]
      const ys = u.data[sidx]
      for (let i = i0; i <= i1; i++) {
        const y = ys[i]
        if (y == null) continue
        const cx = u.valToPos(xs[i], 'x', true)
        const cy = u.valToPos(y, 'y', true)
        const r = radius(i) * uPlot.pxRatio
        fill.moveTo(cx + r, cy)
        fill.arc(cx, cy, r, 0, Math.PI * 2)
      }
      return { fill, stroke: fill }
    }
  }

  // The scatter point under the cursor: the nearest within a small radius,
  // found among the points whose x is close enough (x is sorted).
  function nearestPoint(u: uPlot): number | null {
    const { left, top } = u.cursor
    if (left == null || top == null || left < 0 || top < 0) return null
    const reach = 16
    const xs = u.data[0]
    const from = u.posToVal(left - reach, 'x')
    const to = u.posToVal(left + reach, 'x')
    let lo = 0
    let hi = xs.length
    while (lo < hi) {
      const mid = (lo + hi) >> 1
      if (xs[mid] < from) lo = mid + 1
      else hi = mid
    }
    let best: number | null = null
    let bestDist = reach * reach
    for (let i = lo; i < xs.length && xs[i] <= to; i++) {
      const dx = u.valToPos(xs[i], 'x') - left
      for (let s = 1; s < u.series.length; s++) {
        const y = u.data[s][i]
        if (y == null || !u.series[s].show) continue
        const dy = u.valToPos(y, 'y') - top
        const dist = dx * dx + dy * dy
        if (dist < bestDist) {
          bestDist = dist
          best = i
        }
      }
    }
    return best
  }

  // The context of a click on point `idx` of series `s`: the source row, or
  // the bucket for a histogram.
  function clickContext({ idx, series: s }: ChartPoint, u: uPlot): ClickContext | null {
    const xs = u.data[0]
    const label = plot.labels[s - 1] ?? ''
    const bucket = isTime ? bucketWindow(xs[idx], bucketStep(xs, idx)) : null
    if (histogram) {
      const { column, edges, counts } = histogram
      const row = { [column]: edges[idx], from: edges[idx], to: edges[idx + 1], count: counts[idx] }
      return buildClickContext(row, column, label, [])
    }
    if (scatter) {
      const row = scatter.rows[idx]
      return row ? buildClickContext(row, scatter.xColumn, label, meta, bucket) : null
    }
    const point = plotPoints(plotRows, plotMeta, plotConfig)[idx]
    if (!point || !axes.xColumn) return null
    // A pivoted row has one column per series: the series column comes back as the clicked series.
    const row = pivot && config.seriesColumn ? { ...point.row, [config.seriesColumn]: label } : point.row
    return buildClickContext(row, axes.xColumn, label, plotMeta, bucket)
  }

  // Only whether a handler is set redraws the chart (it sets the cursor), not each new handler.
  const clickable = $derived(!!onpointclick)

  function handlePointClick(point: ChartPoint, u: uPlot, e: MouseEvent) {
    const ctx = clickContext(point, u)
    if (ctx) onpointclick?.({ ctx, x: e.clientX, y: e.clientY })
  }

  const missingColumns = $derived((isHistogram && !histogram) || (isScatter && !scatter))

  // Rows the chart could not show.
  const note = $derived.by(() => {
    if (histogram && histogram.skipped > 0) {
      return `${formatNumber(histogram.skipped)} ${histogram.skipped === 1 ? 'row' : 'rows'} without a number left out`
    }
    if (scatter && scatter.total > plotData[0].length) {
      return `Showing the first ${formatNumber(plotData[0].length)} of ${formatNumber(scatter.total)} points`
    }
    return ''
  })

  function fmt(v: number): string {
    return v.toLocaleString(undefined, { maximumFractionDigits: 2 })
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

  function stackedBarPaths(): uPlot.Series.PathBuilder {
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

      const barPx = Math.max(2, slotPx * 0.7)
      const offset = -barPx / 2

      for (let i = i0; i <= i1; i++) {
        const yVal = u.data[sidx][i]
        if (yVal == null) continue

        let baseline = 0
        for (let s = 1; s < sidx; s++) {
          baseline += Number(u.data[s][i]) || 0
        }

        const cx = u.valToPos(u.data[0][i], 'x', true)
        const baseY = u.valToPos(baseline, 'y', true)
        const topY = u.valToPos(baseline + (yVal as number), 'y', true)

        const x = cx + offset
        const y = Math.min(baseY, topY)
        const h = Math.abs(baseY - topY)

        if (barPx > 0 && h > 0) {
          fill.rect(x, y, barPx, h)
          stroke.rect(x, y, barPx, h)
        }
      }

      return { fill, stroke }
    }
  }

  function buildOpts(w: number, h: number): uPlot.Options {
    const isCat = isCategorical
    const catLabels = xLabels
    const colors = seriesColors
    const theme = chartTheme(container)

    const xLabel = histogram?.column ?? scatter?.xColumn ?? axes.xColumn ?? 'X'
    const series: uPlot.Series[] = [
      { label: xLabel },
    ]

    const yColumns = plot.labels
    for (let i = 0; i < yColumns.length; i++) {
      const color = colors[i % colors.length]
      const s: uPlot.Series = {
        label: yColumns[i],
        stroke: color,
        width: 2,
        show: !hiddenSeries.has(yColumns[i]),
        spanGaps: false,
      }

      if (histogram) {
        s.fill = color + '80'
        s.paths = groupedBarPaths(0, 1)
      } else if (scatter) {
        s.fill = color + '66'
        s.width = 1
        s.paths = scatterPaths(scatter.sizes)
        s.points = { show: false }
      } else if (isBar) {
        s.fill = color + '80'
        s.paths = config.barMode === 'stacked' ? stackedBarPaths() : groupedBarPaths(i, yColumns.length)
      } else {
        s.fill = color + (config.fill === 'area' ? '4D' : '1A')
      }

      series.push(s)
    }

    let xAxisConfig: Partial<uPlot.Axis> = {}
    if (isTime) {
      // default time formatting
    } else if (isCat) {
      const maxLen = Math.max(...catLabels.map(l => l.length), 0)
      const needsRotation = catLabels.length > 4 || maxLen > 8
      const axisSize = needsRotation ? Math.min(maxLen * 6, 120) : 40

      xAxisConfig = {
        splits: (_u: uPlot) => catLabels.map((_: string, i: number) => i),
        values: (_u: uPlot, splits: number[]) => splits.map(i => catLabels[i] ?? ''),
        gap: 4,
        size: axisSize,
        rotate: needsRotation ? -45 : 0,
      }
    } else {
      xAxisConfig = {
        values: (_u: uPlot, vals: number[]) => vals.map(v => String(v)),
      }
    }

    // Stacked areas: each series fills down to the visible series below it.
    const bands: uPlot.Band[] = []
    if (isStacked) {
      let below = 0
      for (let i = 1; i <= yColumns.length; i++) {
        if (hiddenSeries.has(yColumns[i - 1])) continue
        if (below > 0) bands.push({ series: [i, below] })
        below = i
      }
    }

    const formatTime = (xVal: number) => new Date(xVal * 1000).toLocaleString()
    let tooltip: uPlot.Plugin
    if (histogram) {
      const { edges } = histogram
      tooltip = tooltipPlugin({ formatX: (_x, idx) => `${fmt(edges[idx])} to ${fmt(edges[idx + 1])}` })
    } else if (scatter) {
      const { sizes, sizeColumn } = scatter
      tooltip = tooltipPlugin({
        formatX: (xVal, idx) => {
          const x = `${scatter.xColumn} ${isTime ? formatTime(xVal) : fmt(xVal)}`
          const size = sizes?.[idx]
          return sizeColumn && size != null ? `${x}, ${sizeColumn} ${fmt(size)}` : x
        },
        dataIndex: nearestPoint,
        skipNull: true,
      })
    } else {
      tooltip = tooltipPlugin({
        formatX: (xVal, idx) => isTime
          ? formatTime(xVal)
          : isCat ? (catLabels[idx] ?? String(xVal)) : xVal.toLocaleString(),
        // A stack draws running totals; the tooltip shows each series' own value.
        formatValue: isStacked ? (_v, s, idx) => fmt(Number(plotData[s][idx])) : undefined,
      })
    }

    return {
      width: w,
      height: h,
      series,
      bands,
      plugins: [
        tooltip,
        clickPlugin(() => (onpointclick ? handlePointClick : undefined), {
          dataIndex: scatter ? nearestPoint : undefined,
          stacked: isBar && config.barMode === 'stacked',
        }),
      ],
      axes: [
        {
          stroke: theme.axis,
          grid: { stroke: theme.grid, width: 1 },
          ticks: { stroke: theme.grid, width: 1 },
          ...xAxisConfig,
        },
        {
          stroke: theme.axis,
          grid: { stroke: theme.grid, width: 1 },
          ticks: { stroke: theme.grid, width: 1 },
          size: (_self: uPlot, values: string[] | null) => {
            if (!values || values.length === 0) return 50
            const maxLen = Math.max(...values.map(v => v.length))
            return Math.max(40, maxLen * 8 + 12)
          },
        },
      ],
      scales: {
        x: isTime
          ? { time: true }
          : isCat
            ? { time: false, distr: 2, range: (_u: uPlot, min: number, max: number) => [min - 0.5, max + 0.5] }
            : histogram
              ? { time: false, range: () => [histogram.edges[0], histogram.edges[histogram.edges.length - 1]] as [number, number] }
              : { time: false },
        ...(histogram ? {
          y: { range: (_u: uPlot, _min: number, max: number) => [0, (max || 1) * 1.05] as [number, number] },
        } : {}),
        ...(isBar && config.barMode === 'stacked' ? {
          y: {
            range: () => {
              let stackMax = 0
              for (let i = 0; i < plotData[0].length; i++) {
                let sum = 0
                for (let s = 1; s < plotData.length; s++) {
                  sum += Number(plotData[s][i]) || 0
                }
                if (sum > stackMax) stackMax = sum
              }
              return [0, stackMax * 1.05] as [number, number]
            }
          }
        } : {}),
      },
      legend: { show: false },
      // Scatter: the tooltip marks the nearest point, not one point per series.
      cursor: { drag: { x: true, y: false }, ...(scatter ? { points: { show: false } } : {}) },
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
    chart = new uPlot(opts, drawData(), container)
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
    void config.seriesColumn
    void config.fill
    void config.stacked
    void clickable

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

<div class="flex h-full w-full min-h-0 min-w-0 {legendPosition === 'right' ? 'flex-row gap-2' : 'flex-col gap-1'}">
  <div class="flex min-h-0 min-w-0 flex-1 flex-col">
    {#if missingColumns}
      <div class="flex flex-1 items-center justify-center text-xs text-fg-4">
        {isHistogram ? 'Needs a numeric column' : 'Needs two numeric columns'}
      </div>
    {/if}
    <div bind:this={container} class="min-h-0 min-w-0 flex-1 overflow-hidden" hidden={missingColumns}></div>
    {#if note}
      <p class="shrink-0 truncate px-1 pt-1 text-xs text-fg-4">{note}</p>
    {/if}
  </div>
  {#if legendPosition !== 'none' && legendItems.length > 0}
    <ul
      class="flex shrink-0 text-xs text-fg-3 {legendPosition === 'right'
        ? 'max-w-[40%] flex-col gap-1 overflow-y-auto'
        : 'max-h-12 flex-wrap gap-x-3 gap-y-0.5 overflow-y-auto px-1'}"
      aria-label="Legend"
    >
      {#each legendItems as item, i (item.label)}
        {@const hidden = hiddenSeries.has(item.label)}
        <li class="min-w-0">
          <button
            type="button"
            class="inline-flex max-w-full items-center gap-1.5 rounded px-1 transition-colors hover:text-fg {hidden ? 'opacity-40' : ''}"
            aria-pressed={!hidden}
            title={hidden ? `Show ${item.label}` : `Hide ${item.label}`}
            onclick={() => toggleSeries(i, item.label)}
          >
            <span class="h-2 w-2 shrink-0 rounded-[2px]" style="background:{item.color}"></span>
            <span class="truncate">{item.label}</span>
          </button>
        </li>
      {/each}
      {#if pivot && pivot.dropped > 0}
        <li class="px-1 text-fg-4">+{pivot.dropped} more series not shown</li>
      {/if}
    </ul>
  {/if}
</div>
