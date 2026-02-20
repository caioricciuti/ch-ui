<script lang="ts">
  import { tick } from 'svelte'

  export interface ContextMenuItem {
    id: string
    label?: string
    icon?: any
    shortcut?: string
    disabled?: boolean
    danger?: boolean
    separator?: boolean
    onSelect?: () => void
  }

  interface Props {
    open?: boolean
    x?: number
    y?: number
    items?: ContextMenuItem[]
    onclose?: () => void
  }

  let {
    open = false,
    x = 0,
    y = 0,
    items = [],
    onclose,
  }: Props = $props()

  let menuEl = $state<HTMLDivElement | null>(null)
  let left = $state(0)
  let top = $state(0)
  let highlightedIndex = $state(-1)

  function closeMenu() {
    onclose?.()
  }

  function firstEnabledIndex(): number {
    return items.findIndex((item) => !item.separator && !item.disabled)
  }

  function moveHighlight(direction: 1 | -1) {
    if (!items.length) return
    let idx = highlightedIndex
    for (let i = 0; i < items.length; i += 1) {
      idx = (idx + direction + items.length) % items.length
      const item = items[idx]
      if (!item.separator && !item.disabled) {
        highlightedIndex = idx
        return
      }
    }
  }

  function activateIndex(index: number) {
    const item = items[index]
    if (!item || item.separator || item.disabled) return
    item.onSelect?.()
    closeMenu()
  }

  function reposition() {
    if (!menuEl) return
    const rect = menuEl.getBoundingClientRect()
    const pad = 8
    left = Math.min(Math.max(x, pad), window.innerWidth - rect.width - pad)
    top = Math.min(Math.max(y, pad), window.innerHeight - rect.height - pad)
  }

  function handleKeydown(e: KeyboardEvent) {
    if (!open) return
    if (e.key === 'Escape') {
      e.preventDefault()
      closeMenu()
      return
    }
    if (e.key === 'ArrowDown') {
      e.preventDefault()
      moveHighlight(1)
      return
    }
    if (e.key === 'ArrowUp') {
      e.preventDefault()
      moveHighlight(-1)
      return
    }
    if (e.key === 'Enter') {
      e.preventDefault()
      if (highlightedIndex >= 0) activateIndex(highlightedIndex)
    }
  }

  $effect(() => {
    if (!open) {
      highlightedIndex = -1
      return
    }
    left = x
    top = y
    tick().then(() => {
      reposition()
      highlightedIndex = firstEnabledIndex()
    })
  })
</script>

<svelte:window onkeydown={handleKeydown} onresize={reposition} />

{#if open}
  <div
    class="fixed inset-0 z-[95]"
    role="button"
    tabindex="-1"
    onclick={closeMenu}
    onkeydown={(e) => (e.key === 'Escape' || e.key === 'Enter') && closeMenu()}
    oncontextmenu={(e) => {
      e.preventDefault()
      closeMenu()
    }}
  ></div>
  <div
    bind:this={menuEl}
    class="fixed z-[96] min-w-[230px] max-w-[320px] rounded-xl border border-gray-200/80 dark:border-gray-700/80 bg-white/96 dark:bg-gray-900/96 backdrop-blur-xl shadow-[0_16px_40px_rgba(0,0,0,0.35)] py-1.5"
    style={`left:${left}px;top:${top}px`}
    role="menu"
  >
    {#each items as item, i (item.id)}
      {#if item.separator}
        <div class="my-1 h-px bg-gray-200/90 dark:bg-gray-800/90"></div>
      {:else}
        {@const Icon = item.icon}
        <button
          class="group/menuitem flex w-full items-center justify-between gap-3 px-3 py-2 text-[13px] transition-colors
            {item.disabled
              ? 'text-gray-400 cursor-not-allowed'
              : item.danger
                ? (highlightedIndex === i ? 'bg-red-500/12 text-red-500 dark:text-red-400' : 'text-red-500 dark:text-red-400 hover:bg-red-500/10')
                : (highlightedIndex === i ? 'bg-ch-blue/12 text-gray-900 dark:text-gray-100' : 'text-gray-700 dark:text-gray-300 hover:bg-gray-200/70 dark:hover:bg-gray-800/70')}"
          role="menuitem"
          disabled={item.disabled}
          onmouseenter={() => !item.disabled && (highlightedIndex = i)}
          onclick={() => activateIndex(i)}
        >
          <span class="inline-flex min-w-0 items-center gap-2">
            {#if Icon}
              <Icon size={13} class="shrink-0 opacity-85" />
            {/if}
            <span class="truncate">{item.label}</span>
          </span>
          {#if item.shortcut}
            <kbd class="text-[11px] px-1.5 py-0.5 rounded border border-gray-300 dark:border-gray-700 text-gray-500 dark:text-gray-400 font-medium">{item.shortcut}</kbd>
          {/if}
        </button>
      {/if}
    {/each}
  </div>
{/if}
