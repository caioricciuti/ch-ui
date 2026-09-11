<script lang="ts">
  import type { Snippet } from 'svelte'

  interface Props {
    label: string
    /** Explains what the setting does. Shown under the label in row layout, under the control in stack layout. */
    hint?: string
    error?: string
    for?: string
    required?: boolean
    /** stack: label above control (forms). row: label left, control right (settings pages). */
    layout?: 'stack' | 'row'
    /** Width of the control column in row layout. */
    controlWidth?: 'sm' | 'md' | 'lg' | 'full'
    class?: string
    children: Snippet
  }

  let { label, hint, error, for: htmlFor, required = false, layout = 'stack', controlWidth = 'md', class: cls = '', children }: Props = $props()

  const widths: Record<string, string> = { sm: 'max-w-[160px]', md: 'max-w-[320px]', lg: 'max-w-[480px]', full: '' }
</script>

{#if layout === 'row'}
  <div class="grid grid-cols-1 gap-2 py-3 md:grid-cols-[minmax(0,1fr)_minmax(0,1.4fr)] md:gap-6 {cls}">
    <div class="min-w-0">
      <label for={htmlFor} class="block text-[13px] font-medium text-fg">
        {label}{#if required}<span class="ml-0.5 text-danger" aria-hidden="true">*</span>{/if}
      </label>
      {#if hint}
        <p class="mt-0.5 text-xs leading-relaxed text-fg-3">{hint}</p>
      {/if}
    </div>
    <div class="min-w-0 {widths[controlWidth]}">
      {@render children()}
      {#if error}
        <p class="mt-1 text-xs text-danger" role="alert">{error}</p>
      {/if}
    </div>
  </div>
{:else}
  <div class="min-w-0 {widths[controlWidth]} {cls}">
    <label for={htmlFor} class="mb-1 block text-xs font-medium text-fg-2">
      {label}{#if required}<span class="ml-0.5 text-danger" aria-hidden="true">*</span>{/if}
    </label>
    {@render children()}
    {#if error}
      <p class="mt-1 text-xs text-danger" role="alert">{error}</p>
    {:else if hint}
      <p class="mt-1 text-xs text-fg-3">{hint}</p>
    {/if}
  </div>
{/if}
