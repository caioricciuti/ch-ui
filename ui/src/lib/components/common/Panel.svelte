<script lang="ts">
  import type { Snippet } from 'svelte'

  interface Props {
    title?: string
    description?: string
    /** surface: card on the canvas. muted: inset well. plain: no background, just grouping. */
    variant?: 'surface' | 'muted' | 'plain'
    padding?: 'none' | 'sm' | 'md'
    class?: string
    actions?: Snippet
    children: Snippet
  }

  let { title, description, variant = 'surface', padding = 'md', class: cls = '', actions, children }: Props = $props()

  const variants: Record<string, string> = {
    surface: 'rounded-lg border border-edge-subtle bg-surface',
    muted: 'rounded-md bg-surface-2',
    plain: '',
  }
  const paddings: Record<string, string> = { none: '', sm: 'p-3', md: 'p-4' }
</script>

<section class="{variants[variant]} {cls}">
  {#if title || actions}
    <header class="flex items-start justify-between gap-3 {padding === 'none' ? 'px-4 pt-4' : paddings[padding]} {title && padding !== 'none' ? 'pb-0' : ''}">
      <div class="min-w-0">
        {#if title}<h3 class="text-[13px] font-semibold text-fg">{title}</h3>{/if}
        {#if description}<p class="mt-0.5 text-xs text-fg-3">{description}</p>{/if}
      </div>
      {#if actions}<div class="flex shrink-0 items-center gap-1.5">{@render actions()}</div>{/if}
    </header>
  {/if}
  <div class="{paddings[padding]} {title && padding !== 'none' ? 'pt-3' : ''}">
    {@render children()}
  </div>
</section>
