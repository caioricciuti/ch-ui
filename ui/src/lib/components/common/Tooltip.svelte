<script lang="ts">
  import { onDestroy, tick } from 'svelte'
  import type { Snippet } from 'svelte'

  interface Props {
    text: string
    /** `right` suits a vertical rail; top and bottom flip when there is no room. */
    side?: 'top' | 'bottom' | 'right'
    /** Delay before showing, so tooltips do not flash while the pointer crosses controls. 0 shows at once. */
    delay?: number
    /** Suppress the tooltip, e.g. while the trigger's own popover is open. */
    disabled?: boolean
    class?: string
    children: Snippet
  }

  let { text, side = 'top', delay = 350, disabled = false, class: cls = '', children }: Props = $props()

  let triggerEl = $state<HTMLSpanElement | null>(null)
  let tipEl = $state<HTMLDivElement | null>(null)
  let open = $state(false)
  let x = $state(0)
  let y = $state(0)
  // Hidden until the first position lands, so it never flashes at 0,0.
  let placed = $state(false)
  let timer: ReturnType<typeof setTimeout> | null = null
  let raf: number | null = null

  const GAP = 8
  const PAD = 8

  async function position() {
    if (!open || !triggerEl || !tipEl) return
    await tick()
    if (!open || !triggerEl || !tipEl) return
    const t = triggerEl.getBoundingClientRect()
    const r = tipEl.getBoundingClientRect()
    if (side === 'right') {
      x = Math.round(t.right + GAP)
      y = Math.round(Math.min(Math.max(t.top + t.height / 2 - r.height / 2, PAD), window.innerHeight - r.height - PAD))
    } else {
      const topY = t.top - r.height - GAP
      const bottomY = t.bottom + GAP
      let next = side
      if (side === 'top' && topY < PAD && bottomY + r.height <= window.innerHeight - PAD) next = 'bottom'
      if (side === 'bottom' && bottomY + r.height > window.innerHeight - PAD && topY >= PAD) next = 'top'
      y = Math.round(next === 'top' ? topY : bottomY)
      x = Math.round(Math.min(Math.max(t.left + t.width / 2 - r.width / 2, PAD), window.innerWidth - r.width - PAD))
    }
    placed = true
  }

  function schedule() {
    if (raf !== null) cancelAnimationFrame(raf)
    raf = requestAnimationFrame(() => void position())
  }

  function reveal() {
    placed = false
    open = true
    window.addEventListener('scroll', schedule, true)
    window.addEventListener('resize', schedule)
    schedule()
  }

  function show() {
    if (disabled) return
    if (timer) clearTimeout(timer)
    timer = null
    if (delay <= 0) reveal()
    else timer = setTimeout(reveal, delay)
  }

  function hide() {
    if (timer) clearTimeout(timer)
    timer = null
    open = false
    window.removeEventListener('scroll', schedule, true)
    window.removeEventListener('resize', schedule)
  }

  $effect(() => {
    if (disabled) hide()
  })

  onDestroy(hide)
</script>

<span
  bind:this={triggerEl}
  role="presentation"
  class="inline-flex {cls}"
  onmouseenter={show}
  onmouseleave={hide}
  onfocusin={show}
  onfocusout={hide}
  onkeydown={(e) => e.key === 'Escape' && hide()}
>
  {@render children()}
</span>

{#if open}
  <div
    bind:this={tipEl}
    class="pointer-events-none fixed z-[260] max-w-[22rem] whitespace-pre-line rounded-md bg-elevated px-2.5 py-1.5 text-[12px] leading-snug text-fg-2"
    style="left:{x}px;top:{y}px;visibility:{placed ? 'visible' : 'hidden'};box-shadow: var(--shadow-popover)"
    role="tooltip"
  >
    {text}
  </div>
{/if}
