<script lang="ts">
  import type { PanelConfig } from '../../types/api'
  import { heatmapMatrix, isDateType, parseTimeValue, type ColumnMeta } from '../../utils/chart-transform'
  import { buildClickContext, bucketStep, bucketWindow, type PanelClick } from '../../utils/drilldown'
  import { dashboardIsDark } from './dashboard-theme.svelte'

  interface Props {
    data: Record<string, unknown>[]
    meta: ColumnMeta[]
    config: PanelConfig
    /** A click on a cell; set only when the panel has links. */
    onpointclick?: (click: PanelClick) => void
  }

  let { data, meta, config, onpointclick }: Props = $props()

  let canvasEl = $state<HTMLCanvasElement>(undefined!)
  let containerEl = $state<HTMLDivElement>(undefined!)
  let width = $state(0)
  let height = $state(0)

  const dark = $derived(dashboardIsDark())
  const matrix = $derived(heatmapMatrix(data, meta, config))
  const empty = $derived(!matrix || matrix.x.length === 0 || matrix.y.length === 0)

  const FONT = '11px ui-sans-serif, system-ui, sans-serif'
  const MAX_LABEL_CHARS = 18
  const X_LABEL_H = 18

  $effect(() => {
    if (!containerEl) return
    const ro = new ResizeObserver(() => {
      width = containerEl.clientWidth
      height = containerEl.clientHeight
    })
    ro.observe(containerEl)
    return () => ro.disconnect()
  })

  function shorten(label: string): string {
    return label.length > MAX_LABEL_CHARS ? label.slice(0, MAX_LABEL_CHARS - 3) + '...' : label
  }

  let measureCtx: CanvasRenderingContext2D | null = null
  function textWidth(text: string): number {
    measureCtx ??= document.createElement('canvas').getContext('2d')
    if (!measureCtx) return text.length * 6
    measureCtx.font = FONT
    return measureCtx.measureText(text).width
  }

  // Cell grid geometry, shared by the drawing and the hover hit-test.
  const geom = $derived.by(() => {
    if (!matrix || empty) return null
    let labelW = 0
    for (const l of matrix.y) labelW = Math.max(labelW, textWidth(shorten(l)))
    const left = Math.min(Math.ceil(labelW) + 8, Math.floor(width * 0.4))
    const plotW = Math.max(0, width - left - 4)
    const plotH = Math.max(0, height - X_LABEL_H - 4)
    return {
      left,
      top: 4,
      plotW,
      plotH,
      cellW: plotW / matrix.x.length,
      cellH: plotH / matrix.y.length,
    }
  })

  /** Screen row of matrix row `yi`: numbers and dates run upwards. */
  function rowPos(yi: number): number {
    if (!matrix) return yi
    return matrix.yAscending ? matrix.y.length - 1 - yi : yi
  }

  /** Opacity of a cell: the value's place between min and max, never quite invisible. */
  function shade(v: number): number {
    if (!matrix || matrix.max === matrix.min) return 1
    return 0.08 + 0.92 * ((v - matrix.min) / (matrix.max - matrix.min))
  }

  // Hover tooltip, styled like the shared uPlot chart tooltip.
  let hover = $state<{ xi: number; yi: number; x: number; y: number } | null>(null)
  let tipEl = $state<HTMLDivElement>()
  const hovered = $derived.by(() => {
    if (!hover || !matrix) return null
    const value = matrix.values[hover.yi]?.[hover.xi]
    if (value == null) return null
    return { x: matrix.x[hover.xi], y: matrix.y[hover.yi], value }
  })

  /** The cell under a mouse event, or null between and around the cells. */
  function cellAt(e: MouseEvent): { xi: number; yi: number; x: number; y: number } | null {
    if (!geom || !matrix) return null
    const rect = containerEl.getBoundingClientRect()
    const x = e.clientX - rect.left
    const y = e.clientY - rect.top
    const col = Math.floor((x - geom.left) / geom.cellW)
    const row = Math.floor((y - geom.top) / geom.cellH)
    if (col < 0 || col >= matrix.x.length || row < 0 || row >= matrix.y.length) return null
    return { xi: col, yi: rowPos(row), x, y }
  }

  function handleMove(e: MouseEvent) {
    hover = cellAt(e)
  }

  // A click on a cell passes on the first raw x and y values of the cell;
  // a time column opens on the cell's span.
  function handleClick(e: MouseEvent) {
    const cell = cellAt(e)
    if (!cell || !matrix || !onpointclick) return
    const value = matrix.values[cell.yi]?.[cell.xi]
    if (value == null) return
    const { xColumn, yColumn, valueColumn } = matrix
    const row = { [xColumn]: matrix.xValues[cell.xi], [yColumn]: matrix.yValues[cell.yi], [valueColumn ?? 'count']: value }
    const xType = meta.find(m => m.name === xColumn)?.type ?? ''
    let bucket: { from: number; to: number } | null = null
    if (isDateType(xType)) {
      const secs = matrix.xValues.map(v => (parseTimeValue(v) ?? NaN) / 1000)
      if (Number.isFinite(secs[cell.xi])) bucket = bucketWindow(secs[cell.xi], bucketStep(secs, cell.xi))
    }
    const ctx = buildClickContext(row, xColumn, String(matrix.yValues[cell.yi] ?? ''), meta, bucket)
    onpointclick({ ctx, x: e.clientX, y: e.clientY })
  }

  // Keep the tooltip inside the panel, like the uPlot tooltip does.
  const tipPos = $derived.by(() => {
    if (!hover) return { left: 0, top: 0 }
    const pad = 12
    const tw = tipEl?.offsetWidth ?? 0
    const th = tipEl?.offsetHeight ?? 0
    let left = hover.x + pad
    let top = hover.y - th - pad
    if (left + tw > width) left = hover.x - tw - pad
    if (top < 0) top = hover.y + pad
    return { left, top }
  })

  function fmtValue(v: number): string {
    return v.toLocaleString(undefined, { maximumFractionDigits: 2 })
  }

  // The scale color, from the theme tokens; `dark` re-reads it on a switch.
  const scaleColor = $derived.by(() => {
    void dark
    if (!containerEl) return '#2563eb'
    return getComputedStyle(containerEl).getPropertyValue('--info').trim() || '#2563eb'
  })

  $effect(() => {
    if (!canvasEl || !matrix || !geom || width <= 0 || height <= 0) return

    const dpr = window.devicePixelRatio || 1
    canvasEl.width = width * dpr
    canvasEl.height = height * dpr
    canvasEl.style.width = width + 'px'
    canvasEl.style.height = height + 'px'

    const ctx = canvasEl.getContext('2d')!
    ctx.scale(dpr, dpr)
    ctx.clearRect(0, 0, width, height)

    void dark
    const css = getComputedStyle(containerEl)
    const labelColor = css.getPropertyValue('--fg-3').trim() || '#6b7280'
    const color = scaleColor

    const { left, top, plotH, cellW, cellH } = geom
    const gapX = cellW > 4 ? 1 : 0
    const gapY = cellH > 4 ? 1 : 0

    ctx.fillStyle = color
    for (let yi = 0; yi < matrix.y.length; yi++) {
      const line = matrix.values[yi]
      const y = top + rowPos(yi) * cellH
      for (let xi = 0; xi < line.length; xi++) {
        const v = line[xi]
        if (v == null) continue
        ctx.globalAlpha = shade(v)
        ctx.fillRect(left + xi * cellW, y, Math.max(1, cellW - gapX), Math.max(1, cellH - gapY))
      }
    }
    ctx.globalAlpha = 1

    ctx.font = FONT
    ctx.fillStyle = labelColor

    // Row labels: right-aligned in the gutter, thinned out to stay legible.
    ctx.textAlign = 'right'
    ctx.textBaseline = 'middle'
    const rowStep = Math.max(1, Math.ceil(14 / cellH))
    for (let row = 0; row < matrix.y.length; row += rowStep) {
      const yi = matrix.yAscending ? matrix.y.length - 1 - row : row
      ctx.fillText(shorten(matrix.y[yi]), left - 6, top + row * cellH + cellH / 2)
    }

    // Column labels: centered under their columns, every nth one when crowded.
    ctx.textAlign = 'center'
    ctx.textBaseline = 'top'
    let widest = 0
    for (const l of matrix.x) widest = Math.max(widest, ctx.measureText(shorten(l)).width)
    const colStep = Math.max(1, Math.ceil((widest + 12) / Math.max(cellW, 1)))
    for (let xi = 0; xi < matrix.x.length; xi += colStep) {
      ctx.fillText(shorten(matrix.x[xi]), left + xi * cellW + cellW / 2, top + plotH + 4)
    }
  })
</script>

<div class="flex h-full w-full min-h-0 min-w-0 flex-col gap-1">
  <!-- svelte-ignore a11y_no_static_element_interactions, a11y_click_events_have_key_events -->
  <div
    bind:this={containerEl}
    class="relative min-h-0 flex-1 overflow-hidden {onpointclick && hovered ? 'cursor-pointer' : ''}"
    onmousemove={empty ? undefined : handleMove}
    onmouseleave={() => (hover = null)}
    onclick={onpointclick && !empty ? handleClick : undefined}
  >
    {#if empty}
      <div class="flex h-full items-center justify-center text-xs text-fg-4">No data</div>
    {:else}
      <canvas bind:this={canvasEl} class="absolute inset-0"></canvas>
      {#if hovered}
        <div
          bind:this={tipEl}
          class="pointer-events-none absolute z-[100] whitespace-nowrap rounded-md border border-edge bg-elevated px-[9px] py-1.5 text-[11px] leading-normal text-fg"
          style="left:{tipPos.left}px; top:{tipPos.top}px; box-shadow: var(--shadow-popover)"
        >
          <div class="mb-0.5 font-semibold text-fg-2">{matrix?.xColumn} {hovered.x}</div>
          <div class="flex items-center gap-1.5">
            <span class="h-[7px] w-[7px] shrink-0 rounded-[2px] bg-info" style="opacity:{shade(hovered.value)}"></span>
            <span class="flex-1 text-fg-3">{matrix?.yColumn} {hovered.y}</span>
            <span class="ml-3 font-semibold tabular-nums">{fmtValue(hovered.value)}</span>
          </div>
        </div>
      {/if}
    {/if}
  </div>
  {#if matrix && !empty}
    <div class="flex shrink-0 items-center gap-2 px-1 text-xs text-fg-3" aria-label="Color scale">
      <span class="truncate">{matrix.valueColumn ?? 'rows'}</span>
      <span class="tabular-nums">{fmtValue(matrix.min)}</span>
      <span
        class="h-2 w-24 shrink-0 rounded-[2px]"
        style="background: linear-gradient(to right, color-mix(in srgb, var(--info) 8%, transparent), var(--info))"
      ></span>
      <span class="tabular-nums">{fmtValue(matrix.max)}</span>
      {#if matrix.merged}
        <span class="ml-auto truncate text-fg-4">Neighbouring values merged to fit</span>
      {/if}
    </div>
  {/if}
</div>
