<script lang="ts">
  import { Info } from 'lucide-svelte'

  type IconComponent = typeof Info

  export interface TabItem {
    id: string
    label: string
    icon?: IconComponent
    count?: number | string
    disabled?: boolean
  }

  interface Props {
    items: TabItem[]
    value: string
    onchange: (id: string) => void
    /** underline: page-level sections. segmented: small in-place switches. */
    variant?: 'underline' | 'segmented'
    size?: 'sm' | 'md'
    class?: string
  }

  let { items, value, onchange, variant = 'underline', size = 'md', class: cls = '' }: Props = $props()

  function onKeydown(e: KeyboardEvent) {
    if (e.key !== 'ArrowRight' && e.key !== 'ArrowLeft') return
    e.preventDefault()
    const enabled = items.filter((t) => !t.disabled)
    const idx = enabled.findIndex((t) => t.id === value)
    const next = enabled[(idx + (e.key === 'ArrowRight' ? 1 : enabled.length - 1)) % enabled.length]
    if (next) onchange(next.id)
  }
</script>

{#if variant === 'segmented'}
  <div class="ds-segment {cls}" role="tablist" tabindex="-1" onkeydown={onKeydown}>
    {#each items as t (t.id)}
      <button
        class="ds-segment-btn inline-flex items-center gap-1.5 {value === t.id ? 'ds-segment-btn-active' : ''} {size === 'sm' ? 'h-5 px-2 text-[11px]' : ''}"
        role="tab"
        aria-selected={value === t.id}
        tabindex={value === t.id ? 0 : -1}
        disabled={t.disabled}
        onclick={() => onchange(t.id)}
      >
        {#if t.icon}<t.icon size={12} />{/if}
        {t.label}
        {#if t.count !== undefined}<span class="text-fg-4">{t.count}</span>{/if}
      </button>
    {/each}
  </div>
{:else}
  <div class="flex items-center gap-1 border-b border-edge-subtle {cls}" role="tablist" tabindex="-1" onkeydown={onKeydown}>
    {#each items as t (t.id)}
      <button
        class="relative inline-flex items-center gap-1.5 px-2 font-medium transition-colors disabled:opacity-40 {size === 'sm' ? 'h-8 text-xs' : 'h-9 text-[13px]'} {value === t.id ? 'text-fg' : 'text-fg-3 hover:text-fg'}"
        style={value === t.id ? 'box-shadow: inset 0 -2px 0 var(--accent)' : ''}
        role="tab"
        aria-selected={value === t.id}
        tabindex={value === t.id ? 0 : -1}
        disabled={t.disabled}
        onclick={() => onchange(t.id)}
      >
        {#if t.icon}<t.icon size={13} class={value === t.id ? 'text-accent' : ''} />{/if}
        {t.label}
        {#if t.count !== undefined}
          <span class="rounded-sm bg-surface-2 px-1 text-[10px] tabular-nums text-fg-3">{t.count}</span>
        {/if}
      </button>
    {/each}
  </div>
{/if}
