<script lang="ts">
  import type { Snippet } from 'svelte'

  /**
   * The scrolling area under a PageHeader. Centers content at a readable
   * width; `full` is for pages that manage their own layout (canvases,
   * split panes) and only want the scroll container.
   */
  interface Props {
    /** md 1152 px (settings, lists), lg 1280 px (dense dashboards), full: no cap. */
    width?: 'md' | 'lg' | 'full'
    padding?: boolean
    class?: string
    children: Snippet
  }

  let { width = 'md', padding = true, class: cls = '', children }: Props = $props()

  const widths: Record<string, string> = { md: 'max-w-6xl', lg: 'max-w-7xl', full: '' }
</script>

<div class="min-h-0 flex-1 overflow-auto {cls}">
  <div class="mx-auto w-full {widths[width]} {padding ? 'px-5 py-5' : ''}">
    {@render children()}
  </div>
</div>
