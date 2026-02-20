<script lang="ts">
  import type { Snippet } from 'svelte'
  import { fly, fade } from 'svelte/transition'
  import { X } from 'lucide-svelte'

  interface Props {
    open: boolean
    title?: string
    size?: 'sm' | 'md' | 'lg' | 'xl'
    onclose: () => void
    children: Snippet
  }

  let { open, title = '', size = 'md', onclose, children }: Props = $props()

  const sizeClasses: Record<string, string> = {
    sm: 'max-w-md',
    md: 'max-w-xl',
    lg: 'max-w-3xl',
    xl: 'max-w-6xl',
  }

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') onclose()
  }
</script>

<svelte:window onkeydown={handleKeydown} />

{#if open}
  <!-- Backdrop -->
  <div
    class="fixed inset-0 z-40 bg-black/60 backdrop-blur-sm"
    onclick={onclose}
    role="presentation"
    transition:fade={{ duration: 150 }}
  ></div>

  <!-- Sheet -->
  <div
    class="fixed inset-y-0 right-0 z-50 w-full {sizeClasses[size]} flex flex-col bg-gray-50/95 dark:bg-gray-900/95 backdrop-blur-xl border-l border-gray-200/80 dark:border-gray-800/80 shadow-2xl"
    role="dialog"
    aria-modal="true"
    transition:fly={{ x: 300, duration: 200 }}
  >
    {#if title}
      <div class="flex items-center justify-between px-5 py-4 border-b border-gray-200 dark:border-gray-800 shrink-0">
        <h2 class="text-lg font-semibold text-gray-900 dark:text-gray-100">{title}</h2>
        <button class="text-gray-500 hover:text-gray-700 dark:hover:text-gray-300" onclick={onclose}>
          <X size={18} />
        </button>
      </div>
    {/if}
    <div class="flex-1 overflow-auto p-5">
      {@render children()}
    </div>
  </div>
{/if}
