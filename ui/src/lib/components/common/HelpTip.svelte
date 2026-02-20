<script lang="ts">
  import { onDestroy, tick } from 'svelte'
  import { HelpCircle } from 'lucide-svelte'

  interface Props {
    text: string
    side?: 'top' | 'bottom'
  }

  let { text, side = 'top' }: Props = $props()

  let triggerEl = $state<HTMLButtonElement | null>(null)
  let tooltipEl = $state<HTMLDivElement | null>(null)
  let open = $state(false)
  let x = $state(0)
  let y = $state(0)
  let placement = $state<'top' | 'bottom'>('top')
  let rafId: number | null = null

  const GAP = 10
  const VIEWPORT_PADDING = 8

  function clamp(value: number, min: number, max: number): number {
    return Math.min(Math.max(value, min), max)
  }

  function clearPositionRaf() {
    if (rafId !== null) {
      cancelAnimationFrame(rafId)
      rafId = null
    }
  }

  function detachViewportListeners() {
    window.removeEventListener('scroll', schedulePosition, true)
    window.removeEventListener('resize', schedulePosition)
  }

  function attachViewportListeners() {
    window.addEventListener('scroll', schedulePosition, true)
    window.addEventListener('resize', schedulePosition)
  }

  async function positionTooltip() {
    if (!open || !triggerEl || !tooltipEl) return
    await tick()
    if (!open || !triggerEl || !tooltipEl) return

    const triggerRect = triggerEl.getBoundingClientRect()
    const tooltipRect = tooltipEl.getBoundingClientRect()
    const preferred = side

    const topY = triggerRect.top - tooltipRect.height - GAP
    const bottomY = triggerRect.bottom + GAP
    const fitsTop = topY >= VIEWPORT_PADDING
    const fitsBottom = bottomY + tooltipRect.height <= window.innerHeight - VIEWPORT_PADDING

    let nextPlacement: 'top' | 'bottom' = preferred
    if (preferred === 'top' && !fitsTop && fitsBottom) nextPlacement = 'bottom'
    if (preferred === 'bottom' && !fitsBottom && fitsTop) nextPlacement = 'top'

    let nextY = nextPlacement === 'top' ? topY : bottomY
    nextY = clamp(nextY, VIEWPORT_PADDING, window.innerHeight - tooltipRect.height - VIEWPORT_PADDING)

    let nextX = triggerRect.left + (triggerRect.width / 2) - (tooltipRect.width / 2)
    nextX = clamp(nextX, VIEWPORT_PADDING, window.innerWidth - tooltipRect.width - VIEWPORT_PADDING)

    placement = nextPlacement
    x = Math.round(nextX)
    y = Math.round(nextY)
  }

  function schedulePosition() {
    clearPositionRaf()
    rafId = requestAnimationFrame(() => {
      void positionTooltip()
    })
  }

  function openTip() {
    placement = side
    open = true
    attachViewportListeners()
    schedulePosition()
  }

  function closeTip() {
    open = false
    detachViewportListeners()
    clearPositionRaf()
  }

  function handleKeydown(event: KeyboardEvent) {
    if (event.key === 'Escape') closeTip()
  }

  onDestroy(() => {
    detachViewportListeners()
    clearPositionRaf()
  })
</script>

<span class="inline-flex items-center align-middle">
  <button
    bind:this={triggerEl}
    type="button"
    class="inline-flex h-4 w-4 items-center justify-center rounded-full text-gray-400 hover:text-ch-blue focus:outline-none focus:ring-2 focus:ring-ch-blue/35"
    aria-label="Help"
    onmouseenter={openTip}
    onmouseleave={closeTip}
    onfocus={openTip}
    onblur={closeTip}
    onkeydown={handleKeydown}
  >
    <HelpCircle size={13} />
  </button>
</span>

{#if open}
  <div
    bind:this={tooltipEl}
    class="pointer-events-none fixed z-[260] w-[min(22rem,calc(100vw-16px))] rounded-md border border-gray-200 dark:border-gray-700 bg-gray-50/98 dark:bg-gray-900/98 px-2.5 py-2 text-[11px] leading-relaxed text-gray-600 dark:text-gray-300 shadow-2xl"
    style={`left:${x}px;top:${y}px;`}
    role="tooltip"
  >
    {text}
    <span
      class={`absolute left-1/2 h-2 w-2 -translate-x-1/2 rotate-45 border-gray-200 dark:border-gray-700 bg-gray-50 dark:bg-gray-900 ${
        placement === 'top'
          ? '-bottom-1 border-r border-b'
          : '-top-1 border-l border-t'
      }`}
    ></span>
  </div>
{/if}
