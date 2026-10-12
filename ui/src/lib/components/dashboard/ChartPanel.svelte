<script lang="ts">
  import { onMount, onDestroy } from 'svelte'
  import uPlot from 'uplot'
  import 'uplot/dist/uPlot.min.css'
  import type { PanelConfig } from '../../types/api'
  import { toUPlotData, DEFAULT_COLORS, isDateType, isCategoricalColumn, resolveChartColumns, type ColumnMeta } from '../../utils/chart-transform'
  import { chartTheme, tooltipPlugin } from '../../utils/uplot-tooltip'

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

  // Axes as plotted: saved columns, or a sensible pick when none are saved.
  const axes = $derived(resolveChartColumns(meta, config))
  const plotData = $derived(toUPlotData(data, meta, config))

  const isCategorical = $derived(isCategoricalColumn(meta, axes.xColumn))

  const xLabels = $derived(
    isCategorical ? data.map(row => String(row[axes.xColumn ?? ''] ?? '')) : []
  )

  // Legend: our own token-styled row (or column), never the uPlot legend.
  const legendPosition = $derived(config.legendPosition ?? 'bottom')
  const seriesColors = $derived(config.colors?.length ? config.colors : DEFAULT_COLORS)
  const legendItems = $derived(
    axes.yColumns.map((label, i) => ({ label, color: seriesColors[i % seriesColors.length] })),
  )
  let hiddenSeries = $state<Set<string>>(new Set())
  // A string, so a refresh with the same series does not count as a change.
  const seriesKey = $derived(axes.yColumns.join('\u0000'))

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
    chart?.setSeries(index + 1, { show: !next.has(label) })
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
    const xMeta = meta.find(m => m.name === axes.xColumn)
    const isTime = xMeta ? isDateType(xMeta.type) : false
    const isCat = isCategorical
    const catLabels = xLabels
    const colors = seriesColors
    const theme = chartTheme(container)

    const series: uPlot.Series[] = [
      { label: axes.xColumn ?? 'X' },
    ]

    const yColumns = axes.yColumns
    for (let i = 0; i < yColumns.length; i++) {
      const color = colors[i % colors.length]
      const s: uPlot.Series = {
        label: yColumns[i],
        stroke: color,
        width: 2,
        show: !hiddenSeries.has(yColumns[i]),
        spanGaps: false,
      }

      if (config.chartType === 'bar') {
        s.fill = color + '80'
        s.paths = config.barMode === 'stacked' ? stackedBarPaths() : groupedBarPaths(i, yColumns.length)
      } else {
        s.fill = color + '1A'
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

    return {
      width: w,
      height: h,
      series,
      plugins: [tooltipPlugin({
        formatX: (xVal, idx) => isTime
          ? new Date(xVal * 1000).toLocaleString()
          : isCat ? (catLabels[idx] ?? String(xVal)) : xVal.toLocaleString(),
      })],
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
            : { time: false },
        ...(config.chartType === 'bar' && config.barMode === 'stacked' ? {
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

<div class="flex h-full w-full min-h-0 min-w-0 {legendPosition === 'right' ? 'flex-row gap-2' : 'flex-col gap-1'}">
  <div bind:this={container} class="min-h-0 min-w-0 flex-1 overflow-hidden"></div>
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
    </ul>
  {/if}
</div>
