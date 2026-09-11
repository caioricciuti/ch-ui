<script lang="ts">
  import type { Snippet } from 'svelte'
  import { fly, fade } from 'svelte/transition'
  import { X } from 'lucide-svelte'
  import Button from './Button.svelte'
  import { trapFocus } from '../../utils/focus-trap'

  interface Props {
    open: boolean
    title?: string
    description?: string
    size?: 'sm' | 'md' | 'lg' | 'xl'
    onclose: () => void
    children: Snippet
    footer?: Snippet
  }

  let { open, title = '', description = '', size = 'md', onclose, children, footer }: Props = $props()

  const sizeClasses: Record<string, string> = {
    sm: 'max-w-md',
    md: 'max-w-xl',
    lg: 'max-w-3xl',
    xl: 'max-w-6xl',
  }
  const titleId = `sheet-title-${Math.random().toString(36).slice(2, 8)}`

  function handleKeydown(e: KeyboardEvent) {
    if (open && e.key === 'Escape') onclose()
  }
</script>

<svelte:window onkeydown={handleKeydown} />

{#if open}
  <div class="fixed inset-0 z-40 bg-black/50" onclick={onclose} role="presentation" transition:fade={{ duration: 120 }}></div>

  <div
    class="fixed inset-y-0 right-0 z-50 flex w-full flex-col border-l border-edge bg-elevated {sizeClasses[size]}"
    style="box-shadow: var(--shadow-modal)"
    role="dialog"
    aria-modal="true"
    aria-labelledby={title ? titleId : undefined}
    tabindex="-1"
    use:trapFocus
    transition:fly={{ x: 320, duration: 180 }}
  >
    {#if title}
      <div class="flex shrink-0 items-start justify-between gap-4 border-b border-edge-subtle px-5 py-4">
        <div class="min-w-0">
          <h2 id={titleId} class="text-[15px] font-semibold text-fg">{title}</h2>
          {#if description}
            <p class="mt-0.5 text-[13px] text-fg-3">{description}</p>
          {/if}
        </div>
        <Button icon variant="ghost" size="sm" class="-mr-1.5 -mt-1" onclick={onclose} aria-label="Close">
          <X size={15} />
        </Button>
      </div>
    {/if}
    <div class="min-h-0 flex-1 overflow-auto p-5">
      {@render children()}
    </div>
    {#if footer}
      <div class="flex shrink-0 items-center justify-end gap-2 border-t border-edge-subtle px-5 py-3">
        {@render footer()}
      </div>
    {/if}
  </div>
{/if}
