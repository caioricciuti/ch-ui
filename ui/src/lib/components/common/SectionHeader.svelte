<script lang="ts">
  import type { Snippet } from 'svelte'

  interface Props {
    title: string
    description?: string
    /** page: 15 px, for the top of a page. section: 13 px, for groups inside a page. */
    level?: 'page' | 'section'
    class?: string
    actions?: Snippet
  }

  let { title, description, level = 'section', class: cls = '', actions }: Props = $props()
</script>

<div class="flex items-start justify-between gap-4 {level === 'page' ? 'mb-5' : 'mb-3'} {cls}">
  <div class="min-w-0">
    {#if level === 'page'}
      <h1 class="text-[15px] font-semibold tracking-[-0.01em] text-fg">{title}</h1>
    {:else}
      <h2 class="text-[13px] font-semibold text-fg">{title}</h2>
    {/if}
    {#if description}
      <p class="mt-0.5 max-w-[64ch] text-xs leading-relaxed text-fg-3">{description}</p>
    {/if}
  </div>
  {#if actions}
    <div class="flex shrink-0 items-center gap-1.5">{@render actions()}</div>
  {/if}
</div>
