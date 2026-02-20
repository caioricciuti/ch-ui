<script lang="ts">
  import type { Snippet } from 'svelte'
  import { X } from 'lucide-svelte'

  interface Props {
    open: boolean
    title?: string
    onclose: () => void
    children: Snippet
  }

  let { open, title = '', onclose, children }: Props = $props()

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') onclose()
  }
</script>

<svelte:window onkeydown={handleKeydown} />

{#if open}
  <!-- Backdrop -->
  <div
    class="fixed inset-0 z-[70] bg-black/60 backdrop-blur-sm"
    onclick={onclose}
    role="presentation"
  ></div>

  <!-- Dialog -->
  <div class="fixed inset-0 z-[80] flex items-center justify-center p-4">
    <div
      class="surface-card bg-gray-50/95 dark:bg-gray-900/95 backdrop-blur-xl rounded-xl max-w-lg w-full max-h-[80vh] overflow-auto"
      role="dialog"
      aria-modal="true"
      tabindex="0"
      onclick={(e: MouseEvent) => e.stopPropagation()}
      onkeydown={(e: KeyboardEvent) => e.stopPropagation()}
    >
      {#if title}
        <div class="flex items-center justify-between px-5 py-4 border-b border-gray-200 dark:border-gray-800">
          <h2 class="text-lg font-semibold text-gray-900 dark:text-gray-100">{title}</h2>
          <button class="text-gray-500 hover:text-gray-700 dark:hover:text-gray-300" onclick={onclose}>
            <X size={18} />
          </button>
        </div>
      {/if}
      <div class="p-5">
        {@render children()}
      </div>
    </div>
  </div>
{/if}
