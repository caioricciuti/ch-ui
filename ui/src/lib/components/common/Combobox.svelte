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
    /** id of the trigger button, for <label for>. */
    id?: string
    options: ComboboxOption[]
    value?: string
    placeholder?: string
    emptyText?: string
    disabled?: boolean
    /** md = 32 px control (default); lg = 40 px, for standalone forms like sign-in */
    size?: 'md' | 'lg'
    onChange?: (value: string) => void
  }

  let {
    id,
    options,
    value = '',
    placeholder = 'Select...',
    emptyText = 'No matches',
    disabled = false,
    size = 'md',
    onChange,
  }: Props = $props()

  let open = $state(false)
  let query = $state('')
  let highlighted = $state(0)
  let inputEl: HTMLInputElement | undefined = $state()
  let rootEl: HTMLDivElement | undefined = $state()
  let openUpward = $state(false)
  let triggerEl: HTMLButtonElement | undefined = $state()

  const fallbackId = `combobox-${Math.random().toString(36).slice(2, 8)}`
  const uid = $derived(id ?? fallbackId)
  const listId = $derived(`${uid}-listbox`)
  const optionId = (idx: number) => `${uid}-option-${idx}`

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

  function closeMenu(refocus = false) {
    open = false
    query = ''
    highlighted = 0
    if (refocus) triggerEl?.focus()
  }

  function selectOption(opt: ComboboxOption) {
    if (opt.disabled) return
    closeMenu(true)
    onChange?.(opt.value)
  }

  /** Next enabled option from idx in direction dir, or idx when there is none. */
  function step(idx: number, dir: 1 | -1): number {
    for (let i = idx + dir; i >= 0 && i < filtered.length; i += dir) {
      if (!filtered[i].disabled) return i
    }
    return idx
  }

  function moveTo(idx: number) {
    highlighted = idx
    tick().then(() => document.getElementById(optionId(idx))?.scrollIntoView({ block: 'nearest' }))
  }

  /** Closed trigger: ArrowDown, ArrowUp, Enter and Space open the list. */
  function onTriggerKeydown(e: KeyboardEvent) {
    if (open || disabled) return
    if (e.key === 'ArrowDown' || e.key === 'ArrowUp' || e.key === 'Enter' || e.key === ' ') {
      e.preventDefault()
      void openMenu()
    }
  }

  function onKeydown(e: KeyboardEvent) {
    if (!open) return
    if (e.key === 'Escape') {
      e.preventDefault()
      e.stopPropagation()
      closeMenu(true)
      return
    }
    if (e.key === 'Tab') {
      closeMenu()
      return
    }
    if (e.key === 'ArrowDown') {
      e.preventDefault()
      moveTo(step(highlighted, 1))
      return
    }
    if (e.key === 'ArrowUp') {
      e.preventDefault()
      moveTo(step(highlighted, -1))
      return
    }
    if (e.key === 'Home' || e.key === 'End') {
      e.preventDefault()
      moveTo(e.key === 'Home' ? step(-1, 1) : step(filtered.length, -1))
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
    bind:this={triggerEl}
    id={uid}
    type="button"
    class="ds-input flex items-center gap-2 text-left {size === 'lg' ? 'h-10 px-3 text-[14px]' : ''}"
    onclick={(e) => (open ? closeMenu() : openMenu(e))}
    onkeydown={onTriggerKeydown}
    disabled={disabled}
    aria-haspopup="listbox"
    aria-expanded={open}
    aria-controls={open ? listId : undefined}
  >
    <span class="flex-1 truncate {selected ? '' : 'text-fg-4'}">
      {selected?.label ?? placeholder}
    </span>
    <ChevronDown size={14} class="text-fg-3 {open ? 'rotate-180' : ''} transition-transform" />
  </button>

  {#if open}
    <!-- Click catcher outside the menu; keyboard closes with Escape. -->
    <div
      class="fixed inset-0 z-[65]"
      role="presentation"
      aria-hidden="true"
      onclick={(e) => { e.preventDefault(); closeMenu() }}
    ></div>
    <div class="absolute z-[66] w-full min-w-[220px] rounded-md surface-card overflow-hidden {openUpward ? 'bottom-full mb-1' : 'mt-1'}">
      <div class="flex items-center gap-2 px-2.5 py-2 border-b border-edge-subtle">
        <Search size={13} class="text-fg-3" />
        <input
          bind:this={inputEl}
          bind:value={query}
          oninput={() => (highlighted = step(-1, 1))}
          class="w-full bg-transparent text-[13px] text-fg placeholder:text-fg-4 outline-none"
          type="text"
          placeholder="Type to search"
          role="combobox"
          aria-label="Search options"
          aria-autocomplete="list"
          aria-expanded="true"
          aria-controls={listId}
          aria-activedescendant={filtered[highlighted] ? optionId(highlighted) : undefined}
        />
      </div>

      <div class="max-h-56 overflow-y-auto p-1.5" id={listId} role="listbox" aria-labelledby={uid}>
        {#if filtered.length === 0}
          <div class="px-2.5 py-8 text-center text-xs text-fg-3" role="presentation">{emptyText}</div>
        {:else}
          {#each filtered as opt, idx (opt.value)}
            <!-- Options are reached with the arrow keys from the search input
                 (aria-activedescendant), so they are not tab stops. -->
            <div
              id={optionId(idx)}
              role="option"
              tabindex="-1"
              aria-selected={value === opt.value}
              aria-disabled={opt.disabled || undefined}
              class="w-full flex items-center gap-2 px-2.5 py-1.5 rounded-sm text-left transition-colors {opt.disabled ? 'opacity-50 cursor-not-allowed' : 'cursor-pointer'} {idx === highlighted ? 'bg-hover text-fg' : 'text-fg-2 hover:bg-hover'}"
              onclick={(e) => { e.preventDefault(); selectOption(opt) }}
              onkeydown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); selectOption(opt) } }}
              onmouseenter={() => highlighted = idx}
            >
              <span class="flex-1 min-w-0">
                <span class="block text-sm truncate">{opt.label}</span>
                {#if opt.hint}
                  <span class="block text-[11px] text-fg-3 truncate">{opt.hint}</span>
                {/if}
              </span>
              {#if value === opt.value}
                <Check size={13} class="text-accent" />
              {/if}
            </div>
          {/each}
        {/if}
      </div>
    </div>
  {/if}
</div>
