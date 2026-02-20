<script lang="ts">
  import type { Snippet } from 'svelte'

  interface Props {
    variant?: 'primary' | 'secondary' | 'ghost' | 'danger'
    size?: 'sm' | 'md' | 'lg'
    disabled?: boolean
    loading?: boolean
    type?: 'button' | 'submit'
    onclick?: (e: MouseEvent) => void
    children: Snippet
  }

  let { variant = 'primary', size = 'md', disabled = false, loading = false, type = 'button', onclick, children }: Props = $props()

  const base = 'inline-flex items-center justify-center font-semibold rounded-lg transition-all duration-150 focus:outline-none focus:ring-2 focus:ring-offset-2 focus:ring-offset-transparent disabled:opacity-50 disabled:cursor-not-allowed'

  const variants: Record<string, string> = {
    primary: 'bg-ch-blue text-white border border-orange-500 hover:bg-orange-500 focus:ring-orange-400 shadow-[0_1px_0_rgba(255,255,255,0.08)_inset]',
    secondary: 'bg-gray-100 dark:bg-gray-800 text-gray-800 dark:text-gray-200 hover:bg-gray-200 dark:hover:bg-gray-700 focus:ring-gray-400 dark:focus:ring-gray-600 border border-gray-300 dark:border-gray-700',
    ghost: 'text-gray-600 dark:text-gray-300 hover:text-gray-900 dark:hover:text-gray-100 hover:bg-gray-200/70 dark:hover:bg-gray-800/70 focus:ring-gray-400 dark:focus:ring-gray-600 border border-transparent',
    danger: 'bg-red-600 text-white hover:bg-red-700 focus:ring-red-500 border border-red-700',
  }

  const sizes: Record<string, string> = {
    sm: 'px-2.5 py-1.5 text-xs gap-1.5',
    md: 'px-3.5 py-2 text-sm gap-2',
    lg: 'px-5 py-2.5 text-base gap-2.5',
  }
</script>

<button
  {type}
  class="{base} {variants[variant]} {sizes[size]}"
  disabled={disabled || loading}
  {onclick}
>
  {#if loading}
    <svg class="animate-spin h-4 w-4" viewBox="0 0 24 24" fill="none">
      <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4" />
      <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z" />
    </svg>
  {/if}
  {@render children()}
</button>
