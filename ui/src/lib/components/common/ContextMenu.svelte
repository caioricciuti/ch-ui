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
    class="surface-card fixed z-[96] min-w-[220px] max-w-[320px] rounded-md py-1"
    style={`left:${left}px;top:${top}px`}
    role="menu"
  >
    {#each items as item, i (item.id)}
      {#if item.separator}
        <div class="my-1 h-px bg-edge-subtle"></div>
      {:else}
        {@const Icon = item.icon}
        <button
          class="group/menuitem mx-1 flex w-[calc(100%-8px)] items-center justify-between gap-3 rounded-sm px-2 py-1.5 text-[13px] transition-colors
            {item.disabled
              ? 'cursor-not-allowed text-fg-4'
              : item.danger
                ? (highlightedIndex === i ? 'bg-danger-soft text-danger' : 'text-danger hover:bg-danger-soft')
                : (highlightedIndex === i ? 'bg-hover text-fg' : 'text-fg-2 hover:bg-hover')}"
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
            <kbd class="rounded-sm border border-edge px-1 font-sans text-[10px] leading-4 text-fg-4">{item.shortcut}</kbd>
          {/if}
        </button>
      {/if}
    {/each}
  </div>
{/if}
