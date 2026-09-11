<script lang="ts">
  import type { Snippet } from 'svelte'

  /** A KPI tile: small caps label, big number, optional one-line hint. */
  interface Props {
    label: string
    value: string | number
    hint?: string
    tone?: 'default' | 'accent' | 'success' | 'warning' | 'danger'
    /** surface: card on the canvas. muted: inset well inside a Panel. */
    variant?: 'surface' | 'muted'
    /** lg for numbers, md for text facts (ids, names, dates) that would truncate at 2xl. */
    size?: 'lg' | 'md'
    class?: string
    children?: Snippet
  }

  let { label, value, hint, tone = 'default', variant = 'surface', size = 'lg', class: cls = '', children }: Props = $props()

  const tones: Record<string, string> = {
    default: 'text-fg',
    accent: 'text-accent',
    success: 'text-success',
    warning: 'text-warning',
    danger: 'text-danger',
  }
  const variants: Record<string, string> = {
    surface: 'rounded-lg border border-edge-subtle bg-surface p-4',
    muted: 'rounded-md bg-surface-2 px-3 py-2.5',
  }
</script>

<div class="min-w-0 {variants[variant]} {cls}">
  <div class="truncate text-[11px] font-medium uppercase tracking-wider text-fg-3">{label}</div>
  <div class="mt-1 truncate font-semibold tabular-nums leading-tight {size === 'lg' ? 'text-2xl' : 'text-[15px]'} {tones[tone]}">{value}</div>
  {#if hint}<div class="mt-0.5 truncate text-xs text-fg-4">{hint}</div>{/if}
  {#if children}<div class="mt-2">{@render children()}</div>{/if}
</div>
