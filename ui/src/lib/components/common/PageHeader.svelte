<script lang="ts">
  import type { Snippet } from 'svelte'
  import { ArrowLeft, ChevronRight } from 'lucide-svelte'
  import Button from './Button.svelte'
  import DocsPill from './DocsPill.svelte'

  export interface Crumb {
    label: string
    onclick: () => void
  }

  /**
   * The one header every page route renders: a 48 px bar with the page
   * name, an optional muted subtitle (the active section, or a one-line
   * description), badges next to it, and the controls on the right edge.
   * No icon: the rail already says where you are. A detail page (one
   * dashboard, one pipeline) adds `back` or `crumbs` so the list it came
   * from is one click away.
   */
  interface Props {
    title: string
    subtitle?: string
    /** Back arrow before the title, for a detail page under a list. */
    back?: Crumb
    /** Path shown before the title: the list, then folders. */
    crumbs?: Crumb[]
    /** Double-clicking the title opens a rename. */
    onrename?: () => void
    /** Badges and counts that describe the page state (cluster name, degraded, totals). */
    meta?: Snippet
    /** Filters, range switches and the primary action, right-aligned. */
    actions?: Snippet
    /** ch-ui.com docs URL for this screen (see `docsURL` in routes.ts), shown as a pill next to the badges. */
    docs?: string
    class?: string
  }

  let { title, subtitle, back, crumbs, onrename, meta, actions, docs, class: cls = '' }: Props = $props()
</script>

<header class="flex h-12 shrink-0 items-center gap-4 border-b border-edge-subtle px-5 {cls}">
  {#if back}
    <Button icon size="sm" variant="ghost" class="-ml-2 -mr-2" onclick={back.onclick} title={back.label} aria-label={back.label}>
      <ArrowLeft size={15} />
    </Button>
  {/if}
  <div class="flex min-w-0 items-baseline gap-3">
    {#if crumbs?.length}
      <nav class="flex min-w-0 items-center gap-1 text-[13px]" aria-label="Breadcrumb">
        {#each crumbs as crumb, i (i)}
          <button type="button" class="shrink-0 truncate text-fg-3 transition-colors hover:text-fg" onclick={crumb.onclick}>{crumb.label}</button>
          <ChevronRight size={13} class="shrink-0 text-fg-4" />
        {/each}
        <h1
          class="truncate text-[15px] font-semibold tracking-[-0.01em] text-fg {onrename ? 'cursor-text' : ''}"
          ondblclick={onrename}
          title={onrename ? 'Double-click to rename' : undefined}
        >{title}</h1>
      </nav>
    {:else}
      <h1
        class="truncate text-[15px] font-semibold tracking-[-0.01em] text-fg {onrename ? 'cursor-text' : ''}"
        ondblclick={onrename}
        title={onrename ? 'Double-click to rename' : undefined}
      >{title}</h1>
    {/if}
    {#if subtitle && subtitle !== title}<span class="truncate text-[13px] text-fg-3">{subtitle}</span>{/if}
  </div>
  {#if meta || docs}
    <div class="flex shrink-0 items-center gap-1.5">
      {#if meta}{@render meta()}{/if}
      {#if docs}<DocsPill href={docs} />{/if}
    </div>
  {/if}
  {#if actions}
    <div class="ml-auto flex shrink-0 items-center gap-2">{@render actions()}</div>
  {/if}
</header>
