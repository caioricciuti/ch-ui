<script lang="ts">
  import type { ComputedStat } from '../../utils/chart-transform'

  interface Props {
    stat: ComputedStat
  }

  let { stat }: Props = $props()

  let containerEl = $state<HTMLDivElement>(undefined!)
  let fontSize = $state(48)

  $effect(() => {
    if (!containerEl) return
    // Re-run when stat changes
    void stat.value
    void stat.prefix
    void stat.suffix

    const ro = new ResizeObserver(() => fitText())
    ro.observe(containerEl)
    fitText()
    return () => ro.disconnect()
  })

  function fitText() {
    if (!containerEl) return
    const w = containerEl.clientWidth
    const h = containerEl.clientHeight
    if (w === 0 || h === 0) return

    const fullText = (stat.prefix || '') + stat.value + (stat.suffix || '')
    const charCount = Math.max(fullText.length, 1)

    const maxByWidth = (w * 1.6) / charCount
    const maxByHeight = h * 0.55
    fontSize = Math.max(16, Math.min(maxByWidth, maxByHeight, 200))
  }

  const isBg = $derived(stat.colorMode === 'background')
  const isValue = $derived(stat.colorMode === 'value')
  const affixSize = $derived(Math.max(12, fontSize * 0.45))
</script>

<div
  bind:this={containerEl}
  class="flex flex-col items-center justify-center h-full w-full rounded-lg overflow-hidden px-3 py-2 transition-colors"
  style={isBg ? `background: linear-gradient(180deg, ${stat.color} 0%, color-mix(in oklab, ${stat.color}, black 18%) 100%)` : ''}
>
  <div class="flex items-baseline gap-0 leading-none select-none" style="font-size: {fontSize}px">
    {#if stat.prefix}
      <span
        class="font-semibold opacity-80"
        style="font-size: {affixSize}px; {isValue ? `color: ${stat.color}` : isBg ? 'color: rgba(255,255,255,0.75)' : ''}"
        class:text-gray-900={!isValue && !isBg}
        class:dark:text-gray-100={!isValue && !isBg}
      >{stat.prefix}</span>
    {/if}
    <span
      class="font-bold"
      style={isValue ? `color: ${stat.color}` : isBg ? 'color: white' : ''}
      class:text-gray-900={!isValue && !isBg}
      class:dark:text-gray-100={!isValue && !isBg}
    >{stat.value}</span>
    {#if stat.suffix}
      <span
        class="font-semibold opacity-80"
        style="font-size: {affixSize}px; {isValue ? `color: ${stat.color}` : isBg ? 'color: rgba(255,255,255,0.75)' : ''}"
        class:text-gray-900={!isValue && !isBg}
        class:dark:text-gray-100={!isValue && !isBg}
      >{stat.suffix}</span>
    {/if}
  </div>
</div>
