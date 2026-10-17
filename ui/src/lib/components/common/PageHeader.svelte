<script lang="ts">
  import type { Snippet } from 'svelte'
  import { ExternalLink } from 'lucide-svelte'

  /**
   * The one header every page route renders: a 48 px bar with the page
   * name, an optional muted subtitle (the active section, or a one-line
   * description), badges next to it, and the controls on the right edge.
   * No icon: the rail already says where you are.
   */
  interface Props {
    title: string
    subtitle?: string
    /** Badges and counts that describe the page state (cluster name, degraded, totals). */
    meta?: Snippet
    /** Filters, range switches and the primary action, right-aligned. */
    actions?: Snippet
    /** ch-ui.com docs URL for this screen (see `docsURL` in routes.ts), shown as a pill next to the badges. */
    docs?: string
    class?: string
  }

  let { title, subtitle, meta, actions, docs, class: cls = '' }: Props = $props()
</script>

<header class="flex h-12 shrink-0 items-center gap-4 border-b border-edge-subtle px-5 {cls}">
  <div class="flex min-w-0 items-baseline gap-3">
    <h1 class="shrink-0 text-[15px] font-semibold tracking-[-0.01em] text-fg">{title}</h1>
    {#if subtitle && subtitle !== title}<span class="truncate text-[13px] text-fg-3">{subtitle}</span>{/if}
  </div>
  {#if meta || docs}
    <div class="flex shrink-0 items-center gap-1.5">
      {#if meta}{@render meta()}{/if}
      {#if docs}
        <a
          class="ds-badge border border-edge bg-transparent transition-colors hover:border-edge-strong hover:bg-hover hover:text-fg"
          href={docs}
          target="_blank"
          rel="noreferrer"
          title="Open the docs for this page"
        >
          Docs <ExternalLink size={11} />
        </a>
      {/if}
    </div>
  {/if}
  {#if actions}
    <div class="ml-auto flex shrink-0 items-center gap-2">{@render actions()}</div>
  {/if}
</header>
