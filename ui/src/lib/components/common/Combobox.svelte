<script lang="ts">
  import { tick } from 'svelte'
  import { Search, ChevronDown, Check } from 'lucide-svelte'

  export interface ComboboxOption {
    value: string
    label: string
    hint?: string
    keywords?: string
    disabled?: boolean
  }

  interface Props {
    options: ComboboxOption[]
    value?: string
    placeholder?: string
    emptyText?: string
    disabled?: boolean
    onChange?: (value: string) => void
  }

  let {
    options,
    value = '',
    placeholder = 'Select...',
    emptyText = 'No matches',
    disabled = false,
    onChange,
  }: Props = $props()

  let open = $state(false)
  let query = $state('')
  let highlighted = $state(0)
  let inputEl: HTMLInputElement | undefined = $state()
  let rootEl: HTMLDivElement | undefined = $state()
  let openUpward = $state(false)

  const selected = $derived(options.find((o) => o.value === value))

  const filtered = $derived.by(() => {
    const term = query.trim().toLowerCase()
    if (!term) return options
    return options.filter((o) => {
      const pool = `${o.label} ${o.hint ?? ''} ${o.keywords ?? ''}`.toLowerCase()
      return pool.includes(term)
    })
  })

  async function openMenu(e?: MouseEvent) {
    if (disabled) return
    const rect = (e?.currentTarget as HTMLElement | null)?.getBoundingClientRect?.()
    if (rect) {
      const estimatedMenuHeight = 280
      const spaceBelow = window.innerHeight - rect.bottom
      const spaceAbove = rect.top
      openUpward = spaceBelow < estimatedMenuHeight && spaceAbove > spaceBelow
    } else {
      openUpward = false
    }
    open = true
    query = ''
    highlighted = Math.max(0, filtered.findIndex((o) => o.value === value))
    await tick()
    inputEl?.focus()
  }

  function closeMenu() {
    open = false
    query = ''
    highlighted = 0
  }

  function selectOption(opt: ComboboxOption) {
    if (opt.disabled) return
    onChange?.(opt.value)
    closeMenu()
  }

  function onKeydown(e: KeyboardEvent) {
    if (!open) return
    if (e.key === 'Escape') {
      e.preventDefault()
      closeMenu()
      return
    }
    if (e.key === 'ArrowDown') {
      e.preventDefault()
      highlighted = Math.min(filtered.length - 1, highlighted + 1)
      return
    }
    if (e.key === 'ArrowUp') {
      e.preventDefault()
      highlighted = Math.max(0, highlighted - 1)
      return
    }
    if (e.key === 'Enter' && filtered[highlighted]) {
      e.preventDefault()
      selectOption(filtered[highlighted])
    }
  }

  function onWindowPointerDown(e: PointerEvent) {
    if (!open) return
    const target = e.target as Node | null
    if (!target) return
    if (rootEl?.contains(target)) return
    closeMenu()
  }

</script>

<svelte:window onkeydown={onKeydown} onpointerdown={onWindowPointerDown} />

<div class="relative" bind:this={rootEl}>
  <button
    type="button"
    class="ds-input flex items-center gap-2 text-left"
    onclick={open ? closeMenu : openMenu}
    disabled={disabled}
  >
    <span class="flex-1 truncate {selected ? '' : 'text-gray-400 dark:text-gray-500'}">
      {selected?.label ?? placeholder}
    </span>
    <ChevronDown size={14} class="text-gray-500 {open ? 'rotate-180' : ''} transition-transform" />
  </button>

  {#if open}
    <div
      class="fixed inset-0 z-[65]"
      role="button"
      tabindex="-1"
      onclick={closeMenu}
      onkeydown={(e) => (e.key === 'Escape' || e.key === 'Enter') && closeMenu()}
    ></div>
    <div class="absolute z-[66] w-full min-w-[220px] rounded-xl surface-card overflow-hidden shadow-2xl {openUpward ? 'bottom-full mb-1' : 'mt-1'}">
      <div class="flex items-center gap-2 px-2.5 py-2 border-b border-gray-200/70 dark:border-gray-800/70">
        <Search size={13} class="text-gray-500" />
        <input
          bind:this={inputEl}
          bind:value={query}
          class="w-full bg-transparent text-sm text-gray-800 dark:text-gray-200 placeholder:text-gray-400 outline-none"
          type="text"
          placeholder="Type to search"
        />
      </div>

      <div class="max-h-56 overflow-y-auto p-1.5">
        {#if filtered.length === 0}
          <div class="px-2.5 py-8 text-center text-xs text-gray-500">{emptyText}</div>
        {:else}
          {#each filtered as opt, idx (opt.value)}
            <button
              type="button"
              class="w-full flex items-center gap-2 px-2.5 py-2 rounded-lg text-left transition-colors {idx === highlighted ? 'bg-ch-blue/10 text-ch-blue' : 'text-gray-700 dark:text-gray-300 hover:bg-gray-200/55 dark:hover:bg-gray-800/55'}"
              onclick={() => selectOption(opt)}
              onmouseenter={() => highlighted = idx}
              disabled={opt.disabled}
            >
              <span class="flex-1 min-w-0">
                <span class="block text-sm truncate">{opt.label}</span>
                {#if opt.hint}
                  <span class="block text-[11px] text-gray-500 dark:text-gray-400 truncate">{opt.hint}</span>
                {/if}
              </span>
              {#if value === opt.value}
                <Check size={13} class="text-ch-blue" />
              {/if}
            </button>
          {/each}
        {/if}
      </div>
    </div>
  {/if}
</div>
