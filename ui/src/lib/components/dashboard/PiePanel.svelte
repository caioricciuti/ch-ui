<script lang="ts">
  import type { PanelConfig } from '../../types/api'
  import type { ColumnMeta } from '../../utils/chart-transform'
  import { isNumericType, DEFAULT_COLORS } from '../../utils/chart-transform'
  import { dashboardIsDark } from './dashboard-theme.svelte'
  import { buildClickContext, type PanelClick } from '../../utils/drilldown'

  interface Props {
    data: Record<string, unknown>[]
    meta: ColumnMeta[]
    config: PanelConfig
    /** A click on a slice; set only when the panel has links. */
    onpointclick?: (click: PanelClick) => void
  }

  let { data, meta, config, onpointclick }: Props = $props()

  let canvasEl = $state<HTMLCanvasElement>(undefined!)
  let containerEl = $state<HTMLDivElement>(undefined!)
  let width = $state(0)
  let height = $state(0)

  const dark = $derived(dashboardIsDark())

  const labelCol = $derived.by(() => {
    if (config.pieLabelColumn && meta.some(m => m.name === config.pieLabelColumn)) return config.pieLabelColumn
    const strCol = meta.find(m => !isNumericType(m.type))
    return strCol?.name ?? meta[0]?.name ?? ''
  })

  const valueCol = $derived.by(() => {
    if (config.pieValueColumn && meta.some(m => m.name === config.pieValueColumn)) return config.pieValueColumn
    const numCol = meta.find(m => isNumericType(m.type) && m.name !== labelCol)
    return numCol?.name ?? meta[1]?.name ?? ''
  })

  const slices = $derived.by(() => {
    if (!labelCol || !valueCol || data.length === 0) return []
    const colors = config.colors?.length ? config.colors : DEFAULT_COLORS
    const items: { label: string; value: number; color: string; row: Record<string, unknown> }[] = []
    let total = 0
    for (const row of data) {
      const v = Number(row[valueCol]) || 0
      if (v <= 0) continue
      total += v
      items.push({
        label: String(row[labelCol] ?? ''),
        value: v,
        color: colors[(items.length) % colors.length],
        row,
      })
    }
    return items.map(s => ({ ...s, pct: total > 0 ? s.value / total : 0 }))
  })

  $effect(() => {
    if (!containerEl) return
    const ro = new ResizeObserver(() => {
      width = containerEl.clientWidth
      height = containerEl.clientHeight
    })
    ro.observe(containerEl)
    return () => ro.disconnect()
  })

  // Pie geometry, shared by the drawing and the hover hit-test.
  const geom = $derived.by(() => {
    const legendW = Math.min(160, width * 0.35)
    const chartArea = width - legendW - 16
    const radius = Math.min(chartArea, height) * 0.4
    return {
      legendW,
      chartArea,
      cx: chartArea / 2,
      cy: height / 2,
      radius,
      innerR: (config.pieDonut ?? false) ? radius * 0.55 : 0,
    }
  })

  // Hover tooltip, styled like the shared uPlot chart tooltip.
  let hover = $state<{ index: number; x: number; y: number } | null>(null)
  let tipEl = $state<HTMLDivElement>()
  const hovered = $derived(hover ? slices[hover.index] : undefined)

  function sliceAt(x: number, y: number): number {
    const { cx, cy, radius, innerR } = geom
    const dx = x - cx
    const dy = y - cy
    const dist = Math.hypot(dx, dy)
    if (dist > radius || dist < innerR) return -1
    // Angle measured clockwise from 12 o'clock, matching the draw order.
    let a = Math.atan2(dy, dx) + Math.PI / 2
    if (a < 0) a += Math.PI * 2
    let acc = 0
    for (let i = 0; i < slices.length; i++) {
      acc += slices[i].pct * Math.PI * 2
      if (a <= acc) return i
    }
    return -1
  }

  function handleMove(e: MouseEvent) {
    const rect = containerEl.getBoundingClientRect()
    const x = e.clientX - rect.left
    const y = e.clientY - rect.top
    const index = sliceAt(x, y)
    hover = index >= 0 ? { index, x, y } : null
  }

  function handleClick(e: MouseEvent) {
    if (!onpointclick) return
    const rect = containerEl.getBoundingClientRect()
    const slice = slices[sliceAt(e.clientX - rect.left, e.clientY - rect.top)]
    if (!slice) return
    onpointclick({ ctx: buildClickContext(slice.row, labelCol, valueCol, meta), x: e.clientX, y: e.clientY })
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

  $effect(() => {
    if (!canvasEl || width <= 0 || height <= 0 || slices.length === 0) return

    const dpr = window.devicePixelRatio || 1
    canvasEl.width = width * dpr
    canvasEl.height = height * dpr
    canvasEl.style.width = width + 'px'
    canvasEl.style.height = height + 'px'

    const ctx = canvasEl.getContext('2d')!
    ctx.scale(dpr, dpr)
    ctx.clearRect(0, 0, width, height)

    const { legendW, chartArea, cx, cy, radius, innerR } = geom
    // Legend text follows the theme tokens; `dark` re-runs this on a switch.
    void dark
    const css = getComputedStyle(containerEl)
    const labelColor = css.getPropertyValue('--fg-2').trim() || '#374151'
    const pctColor = css.getPropertyValue('--fg-3').trim() || '#6b7280'

    let startAngle = -Math.PI / 2
    for (const s of slices) {
      const sweep = s.pct * Math.PI * 2
      ctx.beginPath()
      ctx.moveTo(cx + innerR * Math.cos(startAngle), cy + innerR * Math.sin(startAngle))
      ctx.arc(cx, cy, radius, startAngle, startAngle + sweep)
      ctx.arc(cx, cy, innerR, startAngle + sweep, startAngle, true)
      ctx.closePath()
      ctx.fillStyle = s.color
      ctx.fill()
      startAngle += sweep
    }

    const legendX = chartArea + 16
    const lineH = 20
    const maxItems = Math.floor(height / lineH)
    const items = slices.slice(0, maxItems)
    const legendStartY = Math.max(8, (height - items.length * lineH) / 2)

    ctx.textBaseline = 'middle'
    for (let i = 0; i < items.length; i++) {
      const s = items[i]
      const y = legendStartY + i * lineH + lineH / 2

      ctx.fillStyle = s.color
      ctx.beginPath()
      ctx.arc(legendX + 5, y, 4, 0, Math.PI * 2)
      ctx.fill()

      ctx.fillStyle = labelColor
      ctx.font = '11px ui-sans-serif, system-ui, sans-serif'

      const pctStr = (s.pct * 100).toFixed(1) + '%'
      const maxLabelW = legendW - 50
      let label = s.label
      while (ctx.measureText(label).width > maxLabelW && label.length > 3) {
        label = label.slice(0, -1)
      }
      if (label !== s.label) label += '...'

      ctx.fillText(label, legendX + 16, y)

      ctx.fillStyle = pctColor
      ctx.font = '10px ui-monospace, monospace'
      ctx.fillText(pctStr, legendX + legendW - ctx.measureText(pctStr).width - 4, y)
    }
  })
</script>

<!-- svelte-ignore a11y_no_static_element_interactions, a11y_click_events_have_key_events -->
<div
  bind:this={containerEl}
  class="w-full h-full overflow-hidden relative {onpointclick && hover ? 'cursor-pointer' : ''}"
  onmousemove={slices.length > 0 ? handleMove : undefined}
  onmouseleave={() => (hover = null)}
  onclick={onpointclick ? handleClick : undefined}
>
  {#if slices.length === 0}
    <div class="flex items-center justify-center h-full text-xs text-fg-4">No data</div>
  {:else}
    <canvas bind:this={canvasEl} class="absolute inset-0"></canvas>
    {#if hover && hovered}
      <div
        bind:this={tipEl}
        class="pointer-events-none absolute z-[100] whitespace-nowrap rounded-md border border-edge bg-elevated px-[9px] py-1.5 text-[11px] leading-normal text-fg"
        style="left:{tipPos.left}px; top:{tipPos.top}px; box-shadow: var(--shadow-popover)"
      >
        <div class="mb-0.5 font-semibold text-fg-2">{hovered.label}</div>
        <div class="flex items-center gap-1.5">
          <span class="h-[7px] w-[7px] shrink-0 rounded-[2px]" style="background:{hovered.color}"></span>
          <span class="flex-1 text-fg-3">{valueCol}</span>
          <span class="ml-3 font-semibold tabular-nums">{fmtValue(hovered.value)}</span>
          <span class="tabular-nums text-fg-3">{(hovered.pct * 100).toFixed(1)}%</span>
        </div>
      </div>
    {/if}
  {/if}
</div>
