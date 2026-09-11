<script lang="ts">
  import type { Snippet } from 'svelte'

  interface Props {
    tone?: 'neutral' | 'success' | 'warning' | 'danger' | 'info' | 'brand'
    /** A leading status dot instead of a filled background. */
    dot?: boolean
    title?: string
    class?: string
    children: Snippet
  }

  let { tone = 'neutral', dot = false, title, class: cls = '', children }: Props = $props()

  const tones: Record<string, string> = {
    neutral: 'bg-surface-2 text-fg-2',
    success: 'bg-success-soft text-success',
    warning: 'bg-warning-soft text-warning',
    danger: 'bg-danger-soft text-danger',
    info: 'bg-info-soft text-info',
    brand: 'bg-accent-soft text-accent',
  }
  const dots: Record<string, string> = {
    neutral: 'bg-fg-4',
    success: 'bg-success',
    warning: 'bg-warning',
    danger: 'bg-danger',
    info: 'bg-info',
    brand: 'bg-accent',
  }
</script>

{#if dot}
  <span class="inline-flex items-center gap-1.5 text-xs text-fg-2 {cls}" {title}>
    <span class="h-1.5 w-1.5 shrink-0 rounded-full {dots[tone]}"></span>
    {@render children()}
  </span>
{:else}
  <span class="ds-badge {tones[tone]} {cls}" {title}>
    {@render children()}
  </span>
{/if}
