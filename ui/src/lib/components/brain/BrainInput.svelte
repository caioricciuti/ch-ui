<script lang="ts">
  import Button from '../common/Button.svelte'
  import { Send, Database as DbIcon } from 'lucide-svelte'

  interface Props {
    value: string
    streaming: boolean
    contextActive: boolean
    contextLabel?: string
    onSend: () => void
    onInput: (value: string) => void
  }

  let { value, streaming, contextActive, contextLabel, onSend, onInput }: Props = $props()

  let textareaEl: HTMLTextAreaElement | undefined = $state()

  function handleInput(e: Event) {
    const target = e.target as HTMLTextAreaElement
    onInput(target.value)
    target.style.height = 'auto'
    target.style.height = `${Math.min(200, target.scrollHeight)}px`
  }

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      onSend()
      // Reset textarea height after send
      if (textareaEl) textareaEl.style.height = 'auto'
    }
  }
</script>

<div class="border-t border-gray-200 dark:border-gray-800 p-4">
  {#if contextActive && contextLabel}
    <div class="mb-2">
      <span class="inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-[10px] font-medium bg-ch-blue/10 text-ch-blue">
        <DbIcon size={10} />
        {contextLabel}
      </span>
    </div>
  {/if}
  <div class="flex items-end gap-2">
    <textarea
      class="flex-1 text-sm bg-transparent border border-gray-300 dark:border-gray-700 rounded-lg px-3 py-2 text-gray-800 dark:text-gray-200 resize-none min-h-[44px] max-h-[200px] focus:outline-none focus:ring-2 focus:ring-ch-blue/40 focus:border-ch-blue/50 transition-colors"
      placeholder="Ask Brain about your data..."
      {value}
      oninput={handleInput}
      onkeydown={handleKeydown}
      disabled={streaming}
      bind:this={textareaEl}
    ></textarea>
    <Button size="sm" onclick={onSend} loading={streaming} disabled={!value.trim() || streaming}>
      <Send size={14} />
    </Button>
  </div>
</div>
