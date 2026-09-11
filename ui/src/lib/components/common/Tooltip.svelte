<script lang="ts">
  import { onDestroy, tick } from 'svelte'
  import type { Snippet } from 'svelte'

  interface Props {
    text: string
    side?: 'top' | 'bottom'
    /** Delay before showing, so tooltips do not flash while the pointer crosses controls. */
    delay?: number
    class?: string
    children: Snippet
  }

  let { text, side = 'top', delay = 350, class: cls = '', children }: Props = $props()

  let triggerEl = $state<HTMLSpanElement | null>(null)
  let tipEl = $state<HTMLDivElement | null>(null)
  let open = $state(false)
  let x = $state(0)
  let y = $state(0)
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
    const topY = t.top - r.height - GAP
    const bottomY = t.bottom + GAP
    let next = side
    if (side === 'top' && topY < PAD && bottomY + r.height <= window.innerHeight - PAD) next = 'bottom'
    if (side === 'bottom' && bottomY + r.height > window.innerHeight - PAD && topY >= PAD) next = 'top'
    y = Math.round(next === 'top' ? topY : bottomY)
    x = Math.round(Math.min(Math.max(t.left + t.width / 2 - r.width / 2, PAD), window.innerWidth - r.width - PAD))
  }

  function schedule() {
    if (raf !== null) cancelAnimationFrame(raf)
    raf = requestAnimationFrame(() => void position())
  }

  function show() {
    if (timer) clearTimeout(timer)
    timer = setTimeout(() => {
      open = true
      window.addEventListener('scroll', schedule, true)
      window.addEventListener('resize', schedule)
      schedule()
    }, delay)
  }

  function hide() {
    if (timer) clearTimeout(timer)
    timer = null
    open = false
    window.removeEventListener('scroll', schedule, true)
    window.removeEventListener('resize', schedule)
  }

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
    class="pointer-events-none fixed z-[260] max-w-[22rem] rounded-md bg-elevated px-2.5 py-1.5 text-[12px] leading-snug text-fg-2"
    style="left:{x}px;top:{y}px;box-shadow: var(--shadow-popover)"
    role="tooltip"
  >
    {text}
  </div>
{/if}
