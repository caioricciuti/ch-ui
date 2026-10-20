<script lang="ts">
  import type { Snippet } from 'svelte'
  import { X } from 'lucide-svelte'
  import Button from './Button.svelte'
  import { trapFocus } from '../../utils/focus-trap'
  import { isEditorTarget, openLayer, primaryAction, shouldSubmitOnEnter } from '../../utils/dialog-keys'

  interface Props {
    open: boolean
    title?: string
    description?: string
    /** sm 400 px, md 520 px, lg 720 px */
    size?: 'sm' | 'md' | 'lg'
    onclose: () => void
    children: Snippet
    footer?: Snippet
  }

  let { open, title = '', description = '', size = 'md', onclose, children, footer }: Props = $props()

  const sizes: Record<string, string> = { sm: 'max-w-[400px]', md: 'max-w-[520px]', lg: 'max-w-[720px]' }
  const titleId = `modal-title-${Math.random().toString(36).slice(2, 8)}`

  let footerEl: HTMLDivElement | undefined = $state()
  let dialogEl: HTMLDivElement | undefined = $state()
  let layer: ReturnType<typeof openLayer> | null = null

  $effect(() => {
    if (!open) return
    const l = openLayer()
    layer = l
    return () => {
      l.release()
      if (layer === l) layer = null
    }
  })

  /**
   * Esc closes the dialog unless something opened on top of it or already
   * used the key; Enter in a single-line field runs the primary button.
   * Keys stop here so a sheet or page under the dialog never sees them.
   */
  function onDialogKeydown(e: KeyboardEvent) {
    e.stopPropagation()
    if (e.key === 'Escape') {
      if (e.defaultPrevented || !layer?.isTop()) return
      // First Esc leaves a code editor; the next one closes.
      if (isEditorTarget(e.target)) {
        e.preventDefault()
        e.stopPropagation()
        dialogEl?.focus()
        return
      }
      e.preventDefault()
      onclose()
      return
    }
    if (shouldSubmitOnEnter(e)) {
      const button = primaryAction(footerEl)
      if (!button) return
      e.preventDefault()
      button.click()
    }
  }

  // Focus can end up on the body (the focused control was removed); Esc
  // still closes the top dialog then.
  function onWindowKeydown(e: KeyboardEvent) {
    if (open && e.key === 'Escape' && !e.defaultPrevented && layer?.isTop()) onclose()
  }
</script>

<svelte:window onkeydown={onWindowKeydown} />

{#if open}
  <div class="fixed inset-0 z-[70] bg-scrim" onclick={onclose} role="presentation"></div>

  <div class="pointer-events-none fixed inset-0 z-[80] flex items-center justify-center p-4">
    <div
      class="surface-card pointer-events-auto flex max-h-[85vh] w-full flex-col rounded-lg {sizes[size]}"
      style="box-shadow: var(--shadow-modal)"
      bind:this={dialogEl}
      role="dialog"
      aria-modal="true"
      aria-labelledby={title ? titleId : undefined}
      tabindex="-1"
      use:trapFocus
      onkeydown={onDialogKeydown}
    >
      {#if title}
        <div class="flex items-start justify-between gap-4 px-5 pt-4 pb-3">
          <div class="min-w-0">
            <h2 id={titleId} class="text-[15px] font-semibold text-fg">{title}</h2>
            {#if description}
              <p class="mt-0.5 text-[13px] text-fg-3">{description}</p>
            {/if}
          </div>
          <Button icon variant="ghost" size="sm" class="-mr-1.5 -mt-1" onclick={onclose} aria-label="Close" autofocusSkip>
            <X size={15} />
          </Button>
        </div>
      {/if}
      <div class="min-h-0 overflow-auto px-5 {title ? 'pb-5' : 'py-5'}">
        {@render children()}
      </div>
      {#if footer}
        <div bind:this={footerEl} class="flex items-center justify-end gap-2 border-t border-edge-subtle px-5 py-3">
          {@render footer()}
        </div>
      {/if}
    </div>
  </div>
{/if}
