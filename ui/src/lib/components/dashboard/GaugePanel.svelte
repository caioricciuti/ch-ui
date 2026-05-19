<script lang="ts">
  import type { ComputedStat } from '../../utils/chart-transform'
  import { getTheme } from '../../stores/theme.svelte'

  interface Props {
    stat: ComputedStat
    min?: number
    max?: number
  }

  let { stat, min = 0, max = 100 }: Props = $props()

  let containerEl = $state<HTMLDivElement>(undefined!)
  let width = $state(0)
  let height = $state(0)

  $effect(() => {
    if (!containerEl) return
    const ro = new ResizeObserver(() => {
      width = containerEl.clientWidth
      height = containerEl.clientHeight
    })
    ro.observe(containerEl)
    return () => ro.disconnect()
  })

  const minVal = $derived(min ?? 0)
  const maxVal = $derived(max ?? 100)
  const clampedRaw = $derived(stat.raw != null ? Math.max(minVal, Math.min(maxVal, stat.raw)) : minVal)
  const fraction = $derived(maxVal > minVal ? (clampedRaw - minVal) / (maxVal - minVal) : 0)

  const dark = $derived(getTheme() === 'dark')
  const bgColor = $derived(dark ? '#374151' : '#e5e7eb')

  const strokeW = $derived(Math.max(6, Math.min(width, height) * 0.07))
  const topPad = $derived(strokeW / 2 + 4)
  const bottomPad = $derived(strokeW / 2 + 18)
  const sidePad = $derived(strokeW / 2 + 4)
  const maxRW = $derived((width - 2 * sidePad) / 2)
  const maxRH = $derived(height - topPad - bottomPad)
  const radius = $derived(Math.max(0, Math.min(maxRW, maxRH)))
  const cx = $derived(width / 2)
  const cy = $derived(topPad + radius)

  function arcPath(startFrac: number, endFrac: number): string {
    const startAngle = Math.PI + startFrac * Math.PI
    const endAngle = Math.PI + endFrac * Math.PI
    const x1 = cx + radius * Math.cos(startAngle)
    const y1 = cy + radius * Math.sin(startAngle)
    const x2 = cx + radius * Math.cos(endAngle)
    const y2 = cy + radius * Math.sin(endAngle)
    const large = (endFrac - startFrac) > 0.5 ? 1 : 0
    return `M ${x1} ${y1} A ${radius} ${radius} 0 ${large} 1 ${x2} ${y2}`
  }

  const thresholdSegments = $derived.by(() => {
    const thresholds = stat.colorMode !== 'none'
      ? ([] as { value: number; color: string }[]).concat(
          ...(stat as any).__thresholds ?? [{ value: minVal, color: '#22c55e' }]
        )
      : []

    if (thresholds.length === 0) {
      return [{ path: arcPath(0, 1), color: stat.color || (dark ? '#6b7280' : '#d1d5db') }]
    }

    const sorted = [...thresholds].sort((a, b) => a.value - b.value)
    const segments: { path: string; color: string }[] = []
    const range = maxVal - minVal

    for (let i = 0; i < sorted.length; i++) {
      const start = Math.max(0, (sorted[i].value - minVal) / range)
      const end = i < sorted.length - 1 ? Math.max(0, (sorted[i + 1].value - minVal) / range) : 1
      if (end > start) {
        segments.push({ path: arcPath(start, end), color: sorted[i].color })
      }
    }

    return segments.length > 0 ? segments : [{ path: arcPath(0, 1), color: stat.color || '#6b7280' }]
  })

  const needleAngle = $derived(Math.PI + fraction * Math.PI)
  const needleX = $derived(cx + radius * Math.cos(needleAngle))
  const needleY = $derived(cy + radius * Math.sin(needleAngle))
  const dotR = $derived(Math.max(3, strokeW * 0.35))

  const fontSize = $derived.by(() => {
    if (radius <= 0) return 16
    const fullText = (stat.prefix || '') + stat.value + (stat.suffix || '')
    const charCount = Math.max(fullText.length, 1)
    const maxByWidth = (radius * 1.8) / charCount
    const maxByHeight = radius * 0.4
    return Math.max(14, Math.min(maxByWidth, maxByHeight, 120))
  })

  const affixSize = $derived(Math.max(10, fontSize * 0.45))
  const isBg = $derived(stat.colorMode === 'background')
  const isValue = $derived(stat.colorMode === 'value')
</script>

<div bind:this={containerEl} class="w-full h-full overflow-hidden relative">
  {#if width > 0 && height > 0 && radius > 10}
    <svg class="absolute inset-0 w-full h-full" viewBox="0 0 {width} {height}">
      <!-- Background arc -->
      <path d={arcPath(0, 1)} stroke={bgColor} stroke-width={strokeW} fill="none" stroke-linecap="round" />
      <!-- Threshold segments -->
      {#each thresholdSegments as seg}
        <path d={seg.path} stroke={seg.color} stroke-width={strokeW} fill="none" stroke-linecap="butt" />
      {/each}
      <!-- Needle dot -->
      <circle cx={needleX} cy={needleY} r={dotR} fill={stat.color || '#f97316'} />
      <!-- Min/Max labels -->
      <text x={cx - radius} y={cy + strokeW / 2 + 14} text-anchor="middle" class="fill-gray-400" style="font-size: 10px">{minVal}</text>
      <text x={cx + radius} y={cy + strokeW / 2 + 14} text-anchor="middle" class="fill-gray-400" style="font-size: 10px">{maxVal}</text>
    </svg>
    <!-- Value text -->
    <div class="absolute inset-0 flex items-center justify-center" style="padding-top: {radius * 0.3}px">
      <div class="flex items-baseline gap-0 leading-none select-none" style="font-size: {fontSize}px">
        {#if stat.prefix}
          <span
            class="font-semibold opacity-80"
            style="font-size: {affixSize}px; {isValue ? `color: ${stat.color}` : ''}"
            class:text-gray-900={!isValue && !isBg}
            class:dark:text-gray-100={!isValue && !isBg}
          >{stat.prefix}</span>
        {/if}
        <span
          class="font-bold"
          style={isValue ? `color: ${stat.color}` : ''}
          class:text-gray-900={!isValue && !isBg}
          class:dark:text-gray-100={!isValue && !isBg}
        >{stat.value}</span>
        {#if stat.suffix}
          <span
            class="font-semibold opacity-80"
            style="font-size: {affixSize}px; {isValue ? `color: ${stat.color}` : ''}"
            class:text-gray-900={!isValue && !isBg}
            class:dark:text-gray-100={!isValue && !isBg}
          >{stat.suffix}</span>
        {/if}
      </div>
    </div>
  {/if}
</div>
