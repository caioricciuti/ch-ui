<script lang="ts">
  interface Props {
    value: string
    onchange: (color: string) => void
  }

  let { value, onchange }: Props = $props()

  let open = $state(false)
  let tab = $state<'palette' | 'custom'>('palette')
  let triggerEl = $state<HTMLButtonElement>(undefined!)
  let panelEl = $state<HTMLDivElement>(undefined!)
  let customHex = $state('')
  let openAbove = $state(false)

  const PALETTE: { label: string; colors: string[] }[] = [
    { label: 'Red',    colors: ['#fca5a5', '#f87171', '#ef4444', '#dc2626', '#b91c1c'] },
    { label: 'Orange', colors: ['#fdba74', '#fb923c', '#f97316', '#ea580c', '#c2410c'] },
    { label: 'Yellow', colors: ['#fde68a', '#fbbf24', '#f59e0b', '#d97706', '#b45309'] },
    { label: 'Green',  colors: ['#86efac', '#4ade80', '#22c55e', '#16a34a', '#15803d'] },
    { label: 'Blue',   colors: ['#93c5fd', '#60a5fa', '#3b82f6', '#2563eb', '#1d4ed8'] },
    { label: 'Purple', colors: ['#d8b4fe', '#c084fc', '#a855f7', '#9333ea', '#7e22ce'] },
    { label: 'Gray',   colors: ['#e5e7eb', '#d1d5db', '#9ca3af', '#6b7280', '#374151'] },
  ]

  function toggle() {
    if (open) {
      open = false
      return
    }
    const rect = triggerEl.getBoundingClientRect()
    openAbove = rect.bottom + 320 > window.innerHeight
    customHex = value
    open = true
  }

  function select(color: string) {
    onchange(color)
    open = false
  }

  function handleClickOutside(e: MouseEvent) {
    if (!triggerEl?.contains(e.target as Node) && !panelEl?.contains(e.target as Node)) {
      open = false
    }
  }

  $effect(() => {
    if (open) {
      document.addEventListener('mousedown', handleClickOutside)
      return () => document.removeEventListener('mousedown', handleClickOutside)
    }
  })
</script>

<div class="relative inline-flex">
  <button
    bind:this={triggerEl}
    class="w-6 h-6 rounded-md border border-edge cursor-pointer shrink-0 transition-shadow hover:ring-2 hover:ring-edge-strong"
    style="background-color: {value}"
    onclick={toggle}
    type="button"
    aria-label="Choose color"
    title={value}
  ></button>

  {#if open}
    <div
      bind:this={panelEl}
      class="absolute z-[100] w-64 surface-card rounded-lg overflow-hidden"
      style={openAbove ? 'bottom: 32px; left: 0;' : 'top: 32px; left: 0;'}
    >
      <!-- Tabs -->
      <div class="flex border-b border-edge-subtle">
        <button
          type="button"
          class="flex-1 text-xs font-medium py-2 transition-colors
            {tab === 'palette'
              ? 'text-fg shadow-[inset_0_-2px_0_var(--accent)]'
              : 'text-fg-3 hover:text-fg'}"
          onclick={() => tab = 'palette'}
        >Colors</button>
        <button
          type="button"
          class="flex-1 text-xs font-medium py-2 transition-colors
            {tab === 'custom'
              ? 'text-fg shadow-[inset_0_-2px_0_var(--accent)]'
              : 'text-fg-3 hover:text-fg'}"
          onclick={() => tab = 'custom'}
        >Custom</button>
      </div>

      {#if tab === 'palette'}
        <div class="p-3 space-y-2.5">
          {#each PALETTE as group}
            <div class="flex items-center gap-2.5">
              <span class="text-[10px] text-fg-4 w-11 shrink-0 text-right font-medium">{group.label}</span>
              <div class="flex gap-1.5 flex-1">
                {#each group.colors as color}
                  <button
                    type="button"
                    class="w-[22px] h-[22px] rounded-full cursor-pointer transition-all hover:scale-[1.2]
                      {value.toLowerCase() === color.toLowerCase() ? 'ring-2 ring-offset-2 ring-accent ring-offset-elevated scale-[1.1]' : ''}"
                    style="background-color: {color}"
                    onclick={() => select(color)}
                    title={color}
                  ></button>
                {/each}
              </div>
            </div>
          {/each}
        </div>
      {:else}
        <div class="p-3 space-y-3">
          <!-- Native color wheel -->
          <div class="flex justify-center">
            <input
              type="color"
              value={customHex}
              oninput={(e) => {
                customHex = (e.target as HTMLInputElement).value
              }}
              class="w-full h-32 rounded-md border border-edge cursor-pointer p-0"
            />
          </div>

          <!-- Hex input + apply -->
          <div class="flex items-center gap-2">
            <div class="flex items-center flex-1 border border-edge rounded-md overflow-hidden">
              <span class="px-2 py-1.5 text-xs text-fg-4 bg-surface-2 border-r border-edge">#</span>
              <input
                type="text"
                class="flex-1 text-xs bg-transparent px-2 py-1.5 text-fg font-mono outline-none"
                placeholder="000000"
                value={customHex.replace(/^#/, '')}
                oninput={(e) => {
                  const v = (e.target as HTMLInputElement).value.replace(/^#/, '')
                  customHex = '#' + v
                }}
                onkeydown={(e) => {
                  if (e.key === 'Enter' && customHex.match(/^#([0-9a-fA-F]{3}|[0-9a-fA-F]{6})$/)) {
                    select(customHex)
                  }
                }}
                maxlength={7}
              />
            </div>
            <button
              type="button"
              class="px-3 py-1.5 text-xs font-medium rounded-md bg-accent text-accent-fg hover:brightness-110 transition-colors disabled:opacity-40"
              disabled={!customHex.match(/^#([0-9a-fA-F]{3}|[0-9a-fA-F]{6})$/)}
              onclick={() => select(customHex)}
            >Apply</button>
          </div>

          <!-- Preview swatch -->
          <div class="flex items-center gap-2">
            <span class="text-[10px] text-fg-4">Preview</span>
            <div class="w-8 h-8 rounded-md border border-edge" style="background-color: {customHex}"></div>
          </div>
        </div>
      {/if}
    </div>
  {/if}
</div>
