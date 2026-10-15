<script lang="ts">
  import { tick } from 'svelte'
  import { ChevronDown, Search } from 'lucide-svelte'

  /**
   * A toolbar button that opens a checklist. Ticks are a draft; the choice
   * is applied once, when the menu closes, so picking five databases runs
   * one fetch, not five.
   */
  interface Option {
    value: string
    label: string
  }

  interface Props {
    label: string
    options: Option[]
    selected: readonly string[]
    /** Show a filter box (long lists). */
    searchable?: boolean
    /** Leave the menu with nothing ticked. */
    allowEmpty?: boolean
    class?: string
    onapply: (values: string[]) => void
  }

  let { label, options, selected, searchable = false, allowEmpty = false, class: cls = 'w-44', onapply }: Props = $props()

  const uid = `check-menu-${Math.random().toString(36).slice(2, 8)}`
  let open = $state(false)
  let draft = $state<string[]>([])
  let query = $state('')
  let searchEl = $state<HTMLInputElement>()

  const shown = $derived.by(() => {
    const term = query.trim().toLowerCase()
    return term ? options.filter((o) => `${o.label} ${o.value}`.toLowerCase().includes(term)) : options
  })

  async function toggleOpen() {
    if (open) {
      close()
      return
    }
    draft = [...selected]
    query = ''
    open = true
    await tick()
    searchEl?.focus()
  }

  function close() {
    if (!open) return
    open = false
    if (draft.length === 0 && !allowEmpty) return
    const same = draft.length === selected.length && draft.every((v) => selected.includes(v))
    if (!same) onapply(options.map((o) => o.value).filter((v) => draft.includes(v)))
  }

  function toggle(value: string) {
    draft = draft.includes(value) ? draft.filter((d) => d !== value) : [...draft, value]
  }

  function onWindowPointerDown(e: PointerEvent) {
    if (!open) return
    const target = e.target as Element | null
    if (target?.closest?.(`[data-check-menu="${uid}"]`)) return
    close()
  }

  function onWindowKeydown(e: KeyboardEvent) {
    if (open && e.key === 'Escape') {
      e.preventDefault()
      close()
    }
  }
</script>

<svelte:window onpointerdown={onWindowPointerDown} onkeydown={onWindowKeydown} />

<div class="relative {cls}" data-check-menu={uid}>
  <button
    type="button"
    class="ds-input-sm flex w-full items-center gap-2 text-left"
    aria-haspopup="listbox"
    aria-expanded={open}
    onclick={() => void toggleOpen()}
  >
    <span class="flex-1 truncate">{label}</span>
    <ChevronDown size={13} class="shrink-0 text-fg-4" />
  </button>
  {#if open}
    <div class="surface-card absolute left-0 z-[66] mt-1 w-full min-w-[220px] overflow-hidden rounded-md">
      {#if searchable}
        <div class="flex items-center gap-2 border-b border-edge-subtle px-2.5 py-2">
          <Search size={13} class="text-fg-3" />
          <input
            bind:this={searchEl}
            bind:value={query}
            class="w-full bg-transparent text-[13px] text-fg outline-none placeholder:text-fg-4"
            type="text"
            placeholder="Type to search"
            aria-label="Search options"
          />
        </div>
      {/if}
      <div class="max-h-64 overflow-y-auto p-1.5" role="listbox" aria-multiselectable="true" aria-label={label}>
        {#each shown as opt (opt.value)}
          <label class="ds-checkbox-label w-full cursor-pointer rounded-sm px-2.5 py-1.5 hover:bg-hover" title={opt.value}>
            <input type="checkbox" class="ds-checkbox ds-checkbox-sm" checked={draft.includes(opt.value)} onchange={() => toggle(opt.value)} />
            <span class="truncate">{opt.label}</span>
          </label>
        {:else}
          <div class="px-2.5 py-6 text-center text-xs text-fg-3">No matches</div>
        {/each}
      </div>
      {#if !allowEmpty && draft.length === 0}
        <p class="border-t border-edge-subtle px-2.5 py-1.5 text-xs text-fg-3">Pick at least one.</p>
      {/if}
    </div>
  {/if}
</div>
