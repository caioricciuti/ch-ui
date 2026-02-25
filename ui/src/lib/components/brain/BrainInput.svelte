<script lang="ts">
  import Button from '../common/Button.svelte'
  import { Send, Database as DbIcon, X } from 'lucide-svelte'
  import BrainMentionDropdown from './BrainMentionDropdown.svelte'
  import type { SchemaContextEntry } from '../../types/brain'

  interface Props {
    value: string
    streaming: boolean
    contexts: SchemaContextEntry[]
    onSend: () => void
    onInput: (value: string) => void
    onAddContext: (database: string, table: string) => void
    onRemoveContext: (database: string, table: string) => void
    onClearAllContexts?: () => void
  }

  let { value, streaming, contexts, onSend, onInput, onAddContext, onRemoveContext, onClearAllContexts }: Props = $props()

  let textareaEl: HTMLTextAreaElement | undefined = $state()
  let mentionActive = $state(false)
  let mentionQuery = $state('')
  let dropdownRef: { handleKeydown: (e: KeyboardEvent) => boolean } | undefined = $state()

  function handleInput(e: Event) {
    const target = e.target as HTMLTextAreaElement
    onInput(target.value)
    target.style.height = 'auto'
    target.style.height = `${Math.min(200, target.scrollHeight)}px`
    detectMention(target.value, target.selectionStart)
  }

  function detectMention(text: string, cursorPos: number) {
    // Look backwards from cursor for @ preceded by whitespace/start
    const before = text.slice(0, cursorPos)
    const match = before.match(/(^|[\s])@([^\s]*)$/)
    if (match) {
      mentionActive = true
      mentionQuery = match[2]
    } else {
      mentionActive = false
      mentionQuery = ''
    }
  }

  function handleMentionSelect(database: string, table: string) {
    // Remove the @query text from input
    if (textareaEl) {
      const cursorPos = textareaEl.selectionStart
      const before = value.slice(0, cursorPos)
      const after = value.slice(cursorPos)
      const match = before.match(/(^|[\s])@([^\s]*)$/)
      if (match) {
        const start = before.length - match[0].length + (match[1] ? match[1].length : 0)
        const newValue = before.slice(0, start) + after
        onInput(newValue.trimStart() === '' ? '' : newValue)
      }
    }
    mentionActive = false
    mentionQuery = ''
    onAddContext(database, table)
    textareaEl?.focus()
  }

  function handleKeydown(e: KeyboardEvent) {
    // When mention dropdown is active, route keyboard events there
    if (mentionActive && dropdownRef) {
      const handled = dropdownRef.handleKeydown(e)
      if (handled) return
    }

    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      onSend()
      if (textareaEl) textareaEl.style.height = 'auto'
    }
  }
</script>

<div class="border-t border-gray-200 dark:border-gray-800 p-4">
  {#if contexts.length > 0}
    <div class="mb-2 flex flex-wrap items-center gap-1">
      {#each contexts as ctx (`${ctx.database}.${ctx.table}`)}
        <span class="inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-[10px] font-medium bg-ch-blue/10 text-ch-blue">
          <DbIcon size={10} />
          {ctx.database}.{ctx.table}
          <button
            class="ml-0.5 rounded-full p-0.5 hover:bg-ch-blue/20 transition-colors"
            onclick={() => onRemoveContext(ctx.database, ctx.table)}
            title="Remove context"
          >
            <X size={10} />
          </button>
        </span>
      {/each}
      {#if contexts.length > 1 && onClearAllContexts}
        <button
          class="text-[10px] text-gray-400 hover:text-gray-600 dark:hover:text-gray-300 ml-1 transition-colors"
          onclick={onClearAllContexts}
        >
          Clear all
        </button>
      {/if}
    </div>
  {/if}
  <div class="relative flex items-end gap-2">
    {#if mentionActive}
      <BrainMentionDropdown
        query={mentionQuery}
        onSelect={handleMentionSelect}
        onDismiss={() => { mentionActive = false; mentionQuery = '' }}
        bind:this={dropdownRef}
      />
    {/if}
    <textarea
      class="flex-1 text-sm bg-transparent border border-gray-300 dark:border-gray-700 rounded-lg px-3 py-2 text-gray-800 dark:text-gray-200 resize-none min-h-[44px] max-h-[200px] focus:outline-none focus:ring-2 focus:ring-ch-blue/40 focus:border-ch-blue/50 transition-colors"
      placeholder="Ask Brain about your data... (type @ to add table context)"
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
