<script lang="ts">
  import type { PanelConfig } from '../../types/api'
  import type { ColumnMeta } from '../../utils/chart-transform'
  import { isNumericType, DEFAULT_COLORS } from '../../utils/chart-transform'
  import { getTheme } from '../../stores/theme.svelte'

  interface Props {
    data: Record<string, unknown>[]
    meta: ColumnMeta[]
    config: PanelConfig
  }

  let { data, meta, config }: Props = $props()

  let canvasEl = $state<HTMLCanvasElement>(undefined!)
  let containerEl = $state<HTMLDivElement>(undefined!)
  let width = $state(0)
  let height = $state(0)

  const dark = $derived(getTheme() === 'dark')

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
    const items: { label: string; value: number; color: string }[] = []
    let total = 0
    for (const row of data) {
      const v = Number(row[valueCol]) || 0
      if (v <= 0) continue
      total += v
      items.push({
        label: String(row[labelCol] ?? ''),
        value: v,
        color: colors[(items.length) % colors.length],
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

    const legendW = Math.min(160, width * 0.35)
    const chartArea = width - legendW - 16
    const cx = chartArea / 2
    const cy = height / 2
    const radius = Math.min(chartArea, height) * 0.4
    const isDonut = config.pieDonut ?? false
    const innerR = isDonut ? radius * 0.55 : 0

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

      ctx.fillStyle = dark ? '#d1d5db' : '#374151'
      ctx.font = '11px ui-sans-serif, system-ui, sans-serif'

      const pctStr = (s.pct * 100).toFixed(1) + '%'
      const maxLabelW = legendW - 50
      let label = s.label
      while (ctx.measureText(label).width > maxLabelW && label.length > 3) {
        label = label.slice(0, -1)
      }
      if (label !== s.label) label += '...'

      ctx.fillText(label, legendX + 16, y)

      ctx.fillStyle = dark ? '#9ca3af' : '#6b7280'
      ctx.font = '10px ui-monospace, monospace'
      ctx.fillText(pctStr, legendX + legendW - ctx.measureText(pctStr).width - 4, y)
    }
  })
</script>

<div bind:this={containerEl} class="w-full h-full overflow-hidden relative">
  {#if slices.length === 0}
    <div class="flex items-center justify-center h-full text-xs text-gray-400">No data</div>
  {:else}
    <canvas bind:this={canvasEl} class="absolute inset-0"></canvas>
  {/if}
</div>
