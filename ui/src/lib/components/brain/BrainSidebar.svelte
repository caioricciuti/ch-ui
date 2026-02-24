<script lang="ts">
  import type { BrainChat } from '../../types/brain'
  import Spinner from '../common/Spinner.svelte'
  import { Plus, Search, Edit3, Trash2 } from 'lucide-svelte'

  interface Props {
    chats: BrainChat[]
    selectedChatId: string
    loading: boolean
    onSelectChat: (chatId: string) => void
    onCreateChat: () => void
    onRenameChat: (chat: BrainChat) => void
    onDeleteChat: (chat: BrainChat) => void
  }

  let { chats, selectedChatId, loading, onSelectChat, onCreateChat, onRenameChat, onDeleteChat }: Props = $props()

  let search = $state('')

  const filtered = $derived.by(() => {
    const term = search.trim().toLowerCase()
    if (!term) return chats
    return chats.filter(c => c.title.toLowerCase().includes(term))
  })

  function formatTime(ts?: string | null): string {
    if (!ts) return ''
    try {
      const d = new Date(ts)
      const now = new Date()
      const diff = now.getTime() - d.getTime()
      if (diff < 60_000) return 'just now'
      if (diff < 3_600_000) return `${Math.floor(diff / 60_000)}m ago`
      if (diff < 86_400_000) return `${Math.floor(diff / 3_600_000)}h ago`
      if (diff < 604_800_000) return `${Math.floor(diff / 86_400_000)}d ago`
      return d.toLocaleDateString()
    } catch {
      return ts
    }
  }
</script>

<aside class="w-72 border-r border-gray-200 dark:border-gray-800 flex flex-col">
  <div class="p-3 border-b border-gray-200 dark:border-gray-800">
    <button
      class="w-full inline-flex items-center justify-center gap-1.5 px-3 py-1.5 text-xs font-medium rounded-lg bg-ch-blue text-white hover:bg-orange-500 transition-colors"
      onclick={onCreateChat}
    >
      <Plus size={14} />
      <span>New Chat</span>
    </button>
    <div class="mt-2 flex items-center gap-2 border border-gray-300 dark:border-gray-700 rounded-lg px-2 py-1.5">
      <Search size={13} class="text-gray-400 shrink-0" />
      <input class="w-full bg-transparent text-xs text-gray-800 dark:text-gray-200 placeholder:text-gray-400 outline-none" placeholder="Search chats" bind:value={search} />
    </div>
  </div>

  <div class="flex-1 overflow-auto p-2 space-y-0.5">
    {#if loading}
      <div class="flex items-center justify-center py-6"><Spinner size="sm" /></div>
    {:else if filtered.length === 0}
      <p class="text-xs text-gray-500 px-2 py-3">No chats</p>
    {:else}
      {#each filtered as chat (chat.id)}
        <div
          class="group w-full text-left rounded-lg px-3 py-2.5 cursor-pointer transition-colors
            {selectedChatId === chat.id
              ? 'bg-ch-blue/10 border-l-2 border-ch-blue'
              : 'hover:bg-gray-100 dark:hover:bg-gray-800 border-l-2 border-transparent'}"
          onclick={() => onSelectChat(chat.id)}
          onkeydown={(e) => (e.key === 'Enter' || e.key === ' ') && onSelectChat(chat.id)}
          role="button"
          tabindex="0"
        >
          <div class="text-sm text-gray-800 dark:text-gray-200 truncate font-medium">{chat.title}</div>
          <div class="flex items-center justify-between mt-1">
            <span class="text-[11px] text-gray-500">{formatTime(chat.last_message_at ?? chat.updated_at)}</span>
            <div class="flex items-center gap-1 opacity-0 group-hover:opacity-100 transition-opacity">
              <button
                class="p-0.5 text-gray-400 hover:text-ch-blue rounded transition-colors"
                onclick={(e) => { e.stopPropagation(); onRenameChat(chat) }}
                title="Rename"
              >
                <Edit3 size={12} />
              </button>
              <button
                class="p-0.5 text-gray-400 hover:text-red-500 rounded transition-colors"
                onclick={(e) => { e.stopPropagation(); onDeleteChat(chat) }}
                title="Delete"
              >
                <Trash2 size={12} />
              </button>
            </div>
          </div>
        </div>
      {/each}
    {/if}
  </div>
</aside>
