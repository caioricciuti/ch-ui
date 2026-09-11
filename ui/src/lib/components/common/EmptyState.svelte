<script lang="ts">
  import type { Snippet } from 'svelte'
  import { Info } from 'lucide-svelte'
  import Button from './Button.svelte'

  /** Any lucide-svelte icon; they all share this component type. */
  type IconComponent = typeof Info

  interface Action {
    label: string
    onclick: () => void
  }

  interface Props {
    icon?: IconComponent
    title: string
    description?: string
    primary?: Action
    secondary?: Action
    /** compact: inline inside a panel. page: centered in a large area. */
    size?: 'compact' | 'page'
    class?: string
    children?: Snippet
  }

  let { icon: Icon, title, description, primary, secondary, size = 'page', class: cls = '', children }: Props = $props()
</script>

<div class="flex flex-col items-center justify-center text-center {size === 'page' ? 'px-6 py-16' : 'px-4 py-8'} {cls}">
  {#if Icon}
    <div class="mb-4 flex h-10 w-10 items-center justify-center rounded-md bg-surface-2 text-fg-3">
      <Icon size={18} strokeWidth={1.75} />
    </div>
  {/if}
  <h3 class="text-[14px] font-semibold text-fg">{title}</h3>
  {#if description}
    <p class="mt-1 max-w-[46ch] text-[13px] leading-relaxed text-fg-3">{description}</p>
  {/if}
  {#if primary || secondary || children}
    <div class="mt-5 flex items-center gap-2">
      {#if primary}<Button size="sm" onclick={primary.onclick}>{primary.label}</Button>{/if}
      {#if secondary}<Button size="sm" variant="outline" onclick={secondary.onclick}>{secondary.label}</Button>{/if}
      {#if children}{@render children()}{/if}
    </div>
  {/if}
</div>
