<script lang="ts">
  import { onMount, tick } from 'svelte'
  import { openQueryTab } from '../lib/stores/tabs.svelte'
  import { success as toastSuccess, error as toastError } from '../lib/stores/toast.svelte'
  import { getDatabases, loadDatabases, loadTables, loadColumns } from '../lib/stores/schema.svelte'
  import type { BrainArtifact, BrainChat, BrainMessage as BrainMessageType, BrainModelOption, MentionRef, EntityContext } from '../lib/types/brain'
  import {
    createBrainChat,
    deleteBrainChat,
    listBrainArtifacts,
    listBrainChats,
    listBrainMessages,
    listBrainModels,
    runBrainQueryArtifact,
    streamBrainMessage,
    updateBrainChat,
  } from '../lib/api/brain'

  import BrainSidebar from '../lib/components/brain/BrainSidebar.svelte'
  import BrainHeader from '../lib/components/brain/BrainHeader.svelte'
  import BrainMessage from '../lib/components/brain/BrainMessage.svelte'
  import BrainInput from '../lib/components/brain/BrainInput.svelte'
  import BrainEmptyState from '../lib/components/brain/BrainEmptyState.svelte'
  import ConfirmDialog from '../lib/components/common/ConfirmDialog.svelte'
  import InputDialog from '../lib/components/common/InputDialog.svelte'

  // ── State ──────────────────────────────────────────────────
  let loading = $state(true)
  let chats = $state<BrainChat[]>([])
  let models = $state<BrainModelOption[]>([])
  let selectedChatId = $state<string>('')
  let messages = $state<BrainMessageType[]>([])
  let artifacts = $state<BrainArtifact[]>([])
  let streaming = $state(false)
  let streamController: AbortController | null = $state(null)

  function stopStreaming() {
    if (streamController) {
      streamController.abort()
      streamController = null
    }
  }
  let selectedModelId = $state('')
  let messagesEl: HTMLDivElement | undefined = $state()
  let runningSql = $state<string | null>(null)
  let brainInput: { setText: (t: string) => void; focus: () => void } | undefined = $state()

  // Dialog state
  let renamingChat = $state<BrainChat | null>(null)
  let renameValue = $state('')
  let deletingChat = $state<BrainChat | null>(null)

  // ── Derived ────────────────────────────────────────────────
  const artifactsByMessageId = $derived.by(() => {
    const map = new Map<string, BrainArtifact[]>()
    for (const art of artifacts) {
      const key = art.message_id ?? '__orphan__'
      const list = map.get(key) ?? []
      list.push(art)
      map.set(key, list)
    }
    return map
  })

  // ── Lifecycle ──────────────────────────────────────────────
  onMount(async () => {
    const dbs = getDatabases()
    if (dbs.length === 0) await loadDatabases()

    await Promise.all([loadModels(), loadChats()])
    if (!selectedChatId) {
      await createChat('New Chat')
    }
    loading = false

    try {
      const seed = sessionStorage.getItem('ch-ui-brain-prompt-seed')
      if (seed) {
        sessionStorage.removeItem('ch-ui-brain-prompt-seed')
        await tick()
        brainInput?.setText(seed)
      }
    } catch { /* private mode etc. */ }
  })

  // ── API functions ──────────────────────────────────────────
  async function loadModels() {
    try {
      models = await listBrainModels()
      const def = models.find(m => m.is_default) ?? models[0]
      if (def && !selectedModelId) selectedModelId = def.id
    } catch (e: any) {
      toastError(e.message)
    }
  }

  async function loadChats() {
    try {
      chats = await listBrainChats(false)
      if (chats.length > 0 && !selectedChatId) {
        await selectChat(chats[0].id)
      }
    } catch (e: any) {
      toastError(e.message)
    }
  }

  async function createChat(title = 'New Chat') {
    try {
      const chat = await createBrainChat({ title, modelId: selectedModelId || undefined })
      chats = [chat, ...chats]
      await selectChat(chat.id)
    } catch (e: any) {
      toastError(e.message)
    }
  }

  async function selectChat(chatId: string) {
    selectedChatId = chatId
    const chat = chats.find(c => c.id === chatId)
    if (chat?.model_id) selectedModelId = chat.model_id

    try {
      const [msgs, arts] = await Promise.all([
        listBrainMessages(chatId),
        listBrainArtifacts(chatId),
      ])
      messages = msgs
      artifacts = arts
      await tick()
      scrollToBottom()
    } catch (e: any) {
      toastError(e.message)
    }
  }

  async function resolveColumns(dbName: string, tableName: string): Promise<{ name: string; type: string }[]> {
    const db = getDatabases().find(d => d.name === dbName)
    if (!db?.tables) await loadTables(dbName)
    await loadColumns(dbName, tableName)
    const freshDb = getDatabases().find(d => d.name === dbName)
    const table = freshDb?.tables?.find(t => t.name === tableName)
    return (table?.columns ?? []).map(c => ({ name: c.name, type: c.type }))
  }

  function renameChat(chat: BrainChat) {
    renameValue = chat.title
    renamingChat = chat
  }

  async function confirmRename(newTitle: string) {
    if (!renamingChat) return
    const chat = renamingChat
    renamingChat = null
    try {
      await updateBrainChat(chat.id, { title: newTitle })
      chats = chats.map(c => c.id === chat.id ? { ...c, title: newTitle } : c)
      if (selectedChatId === chat.id) {
        await selectChat(chat.id)
      }
    } catch (e: any) {
      toastError(e.message)
    }
  }

  function removeChat(chat: BrainChat) {
    deletingChat = chat
  }

  async function confirmDelete() {
    if (!deletingChat) return
    const chat = deletingChat
    deletingChat = null
    try {
      await deleteBrainChat(chat.id)
      chats = chats.filter(c => c.id !== chat.id)
      if (selectedChatId === chat.id) {
        selectedChatId = ''
        messages = []
        artifacts = []
        if (chats.length > 0) {
          await selectChat(chats[0].id)
        } else {
          await createChat('New Chat')
        }
      }
    } catch (e: any) {
      toastError(e.message)
    }
  }

  // ── Chat actions ───────────────────────────────────────────
  async function sendMessage(text: string, mentions: MentionRef[]) {
    if (!text.trim() || streaming) return

    if (!selectedChatId) {
      await createChat('New Chat')
      if (!selectedChatId) return
    }

    const userPrompt = text.trim()

    const tempUser: BrainMessageType = {
      id: `tmp-user-${Date.now()}`,
      chat_id: selectedChatId,
      role: 'user',
      content: userPrompt,
      status: 'complete',
      created_at: new Date().toISOString(),
      updated_at: new Date().toISOString(),
    }
    const tempAssistant: BrainMessageType = {
      id: `tmp-assistant-${Date.now()}`,
      chat_id: selectedChatId,
      role: 'assistant',
      content: '',
      status: 'streaming',
      created_at: new Date().toISOString(),
      updated_at: new Date().toISOString(),
    }

    messages = [...messages, tempUser, tempAssistant]
    const assistantIdx = messages.length - 1

    // Build contexts from inline mentions
    const tableMentions = mentions.filter((m): m is MentionRef & { type: 'table' } => m.type === 'table')
    const entityMentions = mentions.filter((m): m is Exclude<MentionRef, { type: 'table' }> => m.type !== 'table')

    const schemaContexts = tableMentions.length > 0
      ? await Promise.all(tableMentions.map(async (m) => {
          const cols = await resolveColumns(m.database, m.table)
          return { database: m.database, table: m.table, columns: cols }
        }))
      : undefined

    const entityContexts: EntityContext[] | undefined = entityMentions.length > 0
      ? entityMentions.map(m => ({ type: m.type, id: m.id, name: m.name }))
      : undefined

    streaming = true
    streamController = new AbortController()
    await tick()
    scrollToBottom()

    // Track stream-level errors so we don't reload messages from the server
    // afterwards (which would discard a client-only error message — see #113).
    let streamErrored = false

    try {
      await streamBrainMessage(
        selectedChatId,
        {
          content: userPrompt,
          modelId: selectedModelId || undefined,
          schemaContexts,
          entityContexts,
        },
        (event) => {
          if (event.type === 'delta') {
            const delta = event.delta ?? ''
            messages = messages.map((m, i) => i === assistantIdx ? { ...m, content: m.content + delta } : m)
          } else if (event.type === 'tool_call_start') {
            const tc = { id: event.toolCallId ?? '', tool: event.tool ?? '', args: event.args, status: 'pending' as const }
            messages = messages.map((m, i) => {
              if (i !== assistantIdx) return m
              const list = m.toolCalls ?? []
              const idx = list.findIndex(x => x.id === tc.id)
              if (idx >= 0) {
                const copy = list.slice()
                copy[idx] = { ...copy[idx], status: 'pending', args: tc.args ?? copy[idx].args }
                return { ...m, toolCalls: copy }
              }
              return { ...m, toolCalls: [...list, tc] }
            })
          } else if (event.type === 'tool_call_pending_approval') {
            const tc = {
              id: event.toolCallId ?? '',
              tool: event.tool ?? '',
              args: event.args,
              status: 'pending_approval' as const,
              approvalId: event.approvalId,
            }
            messages = messages.map((m, i) => {
              if (i !== assistantIdx) return m
              const list = m.toolCalls ?? []
              return { ...m, toolCalls: [...list, tc] }
            })
          } else if (event.type === 'tool_call_result') {
            messages = messages.map((m, i) => {
              if (i !== assistantIdx) return m
              const list = (m.toolCalls ?? []).map(tc =>
                tc.id === event.toolCallId
                  ? { ...tc, status: (event.status ?? 'success') as 'success' | 'error' | 'declined', result: event.result }
                  : tc
              )
              return { ...m, toolCalls: list }
            })
          } else if (event.type === 'error') {
            streamErrored = true
            messages = messages.map((m, i) => i === assistantIdx ? { ...m, content: m.content || `Error: ${event.error ?? 'Unknown error'}`, status: 'error' } : m)
          } else if (event.type === 'done') {
            messages = messages.map((m, i) => i === assistantIdx ? { ...m, status: 'complete' } : m)
          }
        },
        streamController?.signal,
      )

      // On a stream error the error message is client-only (not persisted), so
      // reloading the conversation from the server would wipe it (#113). Refresh
      // only the chat list in that case; otherwise reload the canonical messages.
      if (streamErrored) {
        await loadChats()
      } else {
        await Promise.all([
          selectChat(selectedChatId),
          loadChats(),
        ])
      }
    } catch (e: any) {
      if (e?.name === 'AbortError') {
        messages = messages.map((m, i) => i === assistantIdx ? { ...m, content: m.content || '_Stopped by user._', status: 'complete' } : m)
      } else {
        messages = messages.map((m, i) => {
          if (i !== assistantIdx) return m
          const list = (m.toolCalls ?? []).map(tc =>
            tc.status === 'pending' || tc.status === 'pending_approval'
              ? { ...tc, status: 'declined' as const, result: { error: 'Connection dropped before approval.' } }
              : tc
          )
          return { ...m, content: m.content || `Error: ${e.message}`, status: 'error' as const, toolCalls: list }
        })
      }
    } finally {
      streaming = false
      streamController = null
      await tick()
      scrollToBottom()
    }
  }

  async function runSqlInChat(sql: string, messageId?: string) {
    if (!selectedChatId) return
    runningSql = sql
    try {
      await runBrainQueryArtifact(selectedChatId, {
        query: sql,
        title: 'Query Result',
        messageId,
      })
      toastSuccess('Query executed and saved as artifact')
      artifacts = await listBrainArtifacts(selectedChatId)
    } catch (e: any) {
      toastError(e.message)
    } finally {
      runningSql = null
    }
  }

  function scrollToBottom() {
    if (messagesEl) messagesEl.scrollTop = messagesEl.scrollHeight
  }
</script>

<div class="flex h-full">
  <BrainSidebar
    {chats}
    {selectedChatId}
    {loading}
    onSelectChat={(id) => selectChat(id)}
    onCreateChat={() => createChat('New Chat')}
    onRenameChat={renameChat}
    onDeleteChat={removeChat}
  />

  <main class="flex-1 flex flex-col min-w-0">
    <BrainHeader
      {models}
      {selectedModelId}
      onModelChange={(v) => selectedModelId = v}
    />

    <div class="flex-1 overflow-auto p-6 space-y-5" bind:this={messagesEl}>
      {#if messages.length === 0}
        <BrainEmptyState onPick={(p) => brainInput?.setText(p)} />
      {:else}
        {#each messages as msg, i (msg.id)}
          <BrainMessage
            message={msg}
            artifacts={artifactsByMessageId.get(msg.id) ?? []}
            {streaming}
            isLastMessage={i === messages.length - 1}
            {runningSql}
            onRunSql={runSqlInChat}
            onOpenInEditor={(sql) => openQueryTab(sql)}
          />
        {/each}
      {/if}
    </div>

    <BrainInput
      {streaming}
      onSend={sendMessage}
      onStop={stopStreaming}
      bind:this={brainInput}
    />
  </main>
</div>

<InputDialog
  open={renamingChat !== null}
  title="Rename chat"
  placeholder="Chat name"
  bind:value={renameValue}
  onconfirm={confirmRename}
  oncancel={() => renamingChat = null}
/>

<ConfirmDialog
  open={deletingChat !== null}
  title="Delete chat"
  description={`Are you sure you want to delete "${deletingChat?.title}"? This cannot be undone.`}
  confirmLabel="Delete"
  destructive
  onconfirm={confirmDelete}
  oncancel={() => deletingChat = null}
/>
