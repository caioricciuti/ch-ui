<script lang="ts">
  import type { Snippet } from 'svelte'
  import Spinner from './Spinner.svelte'

  interface Props {
    variant?: 'primary' | 'secondary' | 'outline' | 'ghost' | 'danger'
    /** xs 24 px, sm 28 px, md 32 px, lg 36 px */
    size?: 'xs' | 'sm' | 'md' | 'lg'
    /** Square button holding only an icon; pass aria-label. */
    icon?: boolean
    disabled?: boolean
    loading?: boolean
    type?: 'button' | 'submit'
    title?: string
    'aria-label'?: string
    'aria-pressed'?: boolean
    'aria-expanded'?: boolean
    class?: string
    onclick?: (e: MouseEvent) => void
    children: Snippet
  }

  let {
    variant = 'primary',
    size = 'md',
    icon = false,
    disabled = false,
    loading = false,
    type = 'button',
    title,
    'aria-label': ariaLabel,
    'aria-pressed': ariaPressed,
    'aria-expanded': ariaExpanded,
    class: cls = '',
    onclick,
    children,
  }: Props = $props()

  const base =
    'inline-flex shrink-0 items-center justify-center gap-1.5 rounded-md font-medium whitespace-nowrap transition-colors duration-100 disabled:pointer-events-none disabled:opacity-50'

  const variants: Record<string, string> = {
    primary: 'bg-accent text-accent-fg hover:brightness-110 active:brightness-95',
    secondary: 'bg-surface-2 text-fg hover:bg-active',
    outline: 'border border-edge bg-transparent text-fg-2 hover:border-edge-strong hover:bg-hover hover:text-fg',
    ghost: 'text-fg-3 hover:bg-hover hover:text-fg',
    danger: 'bg-danger text-white hover:brightness-110 active:brightness-95',
  }

  const sizes: Record<string, [string, string]> = {
    xs: ['h-6 w-6', 'h-6 px-2 text-xs'],
    sm: ['h-7 w-7', 'h-7 px-2.5 text-xs'],
    md: ['h-8 w-8', 'h-8 px-3 text-[13px]'],
    lg: ['h-9 w-9', 'h-9 px-4 text-[13px]'],
  }
  const sizeClass = $derived(sizes[size][icon ? 0 : 1])
</script>

<button
  {type}
  {title}
  aria-label={ariaLabel}
  aria-pressed={ariaPressed}
  aria-expanded={ariaExpanded}
  aria-busy={loading || undefined}
  class="{base} {variants[variant]} {sizeClass} {cls}"
  disabled={disabled || loading}
  {onclick}
>
  {#if loading}
    <Spinner size="sm" class="h-3.5 w-3.5" />
  {/if}
  {@render children()}
</button>
