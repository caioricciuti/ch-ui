<script lang="ts">
  import type { BrainChat } from '../../types/brain'
  import { formatRelativeTime } from '../../utils/format'
  import Button from '../common/Button.svelte'
  import Input from '../common/Input.svelte'
  import Spinner from '../common/Spinner.svelte'
  import { Plus, Edit3, Trash2 } from 'lucide-svelte'

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
</script>

<aside class="flex w-64 shrink-0 flex-col border-r border-edge-subtle bg-sidebar">
  <div class="flex h-12 shrink-0 items-center gap-2 border-b border-edge-subtle px-3">
    <Input size="sm" type="search" bind:value={search} placeholder="Search chats" class="flex-1" />
    <Button icon size="sm" variant="ghost" onclick={onCreateChat} title="New chat" aria-label="New chat">
      <Plus size={15} />
    </Button>
  </div>

  <div class="min-h-0 flex-1 space-y-0.5 overflow-auto p-2">
    {#if loading}
      <div class="flex items-center justify-center py-6"><Spinner size="sm" /></div>
    {:else if filtered.length === 0}
      <p class="px-2 py-3 text-xs text-fg-3">{search.trim() ? 'No chats match' : 'No chats yet'}</p>
    {:else}
      {#each filtered as chat (chat.id)}
        <div
          class="group w-full cursor-pointer rounded-md px-2.5 py-2 text-left transition-colors {selectedChatId === chat.id ? 'bg-active' : 'hover:bg-hover'}"
          onclick={() => onSelectChat(chat.id)}
          onkeydown={(e) => (e.key === 'Enter' || e.key === ' ') && onSelectChat(chat.id)}
          role="button"
          tabindex="0"
          aria-current={selectedChatId === chat.id ? 'true' : undefined}
        >
          <div class="truncate text-[13px] font-medium text-fg">{chat.title}</div>
          <div class="mt-0.5 flex items-center justify-between">
            <span class="text-[11px] text-fg-3">{formatRelativeTime(chat.last_message_at ?? chat.updated_at)}</span>
            <div class="flex items-center gap-0.5 opacity-0 transition-opacity group-hover:opacity-100 group-focus-within:opacity-100">
              <button
                class="rounded-sm p-0.5 text-fg-4 transition-colors hover:text-fg"
                onclick={(e) => { e.stopPropagation(); onRenameChat(chat) }}
                title="Rename"
                aria-label="Rename chat"
              >
                <Edit3 size={12} />
              </button>
              <button
                class="rounded-sm p-0.5 text-fg-4 transition-colors hover:text-danger"
                onclick={(e) => { e.stopPropagation(); onDeleteChat(chat) }}
                title="Delete"
                aria-label="Delete chat"
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
