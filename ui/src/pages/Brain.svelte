<script lang="ts">
  import { onMount, tick } from 'svelte'
  import { openQueryTab } from '../lib/stores/tabs.svelte'
  import { success as toastSuccess, error as toastError } from '../lib/stores/toast.svelte'
  import { getDatabases, loadDatabases, loadTables, loadColumns } from '../lib/stores/schema.svelte'
  import type { BrainArtifact, BrainChat, BrainMessage as BrainMessageType, BrainModelOption, SchemaContextEntry } from '../lib/types/brain'
  import type { ComboboxOption } from '../lib/components/common/Combobox.svelte'
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

  const MAX_CONTEXTS = 10

  // ── State ──────────────────────────────────────────────────
  let loading = $state(true)
  let chats = $state<BrainChat[]>([])
  let models = $state<BrainModelOption[]>([])
  let selectedChatId = $state<string>('')
  let messages = $state<BrainMessageType[]>([])
  let artifacts = $state<BrainArtifact[]>([])
  let input = $state('')
  let streaming = $state(false)
  let selectedModelId = $state('')
  let messagesEl: HTMLDivElement | undefined = $state()
  let runningSql = $state<string | null>(null)

  // Multi-context state
  let contexts = $state<SchemaContextEntry[]>([])
  let headerDb = $state('')
  let headerTable = $state('')

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

  const databaseOptions = $derived.by<ComboboxOption[]>(() =>
    getDatabases().map(db => ({
      value: db.name,
      label: db.name,
      keywords: db.name,
    }))
  )

  const tableOptions = $derived.by<ComboboxOption[]>(() => {
    const db = getDatabases().find(d => d.name === headerDb)
    const tables = db?.tables?.map(t => t.name) ?? []
    return tables.map(t => ({
      value: t,
      label: t,
      hint: headerDb,
      keywords: `${headerDb}.${t}`,
    }))
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

    // Restore contexts
    await restoreContexts(chat)

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

  async function restoreContexts(chat: BrainChat | undefined) {
    if (!chat) {
      contexts = []
      return
    }

    // Try new multi-context format first
    if (chat.context_tables) {
      try {
        const parsed = JSON.parse(chat.context_tables) as { database: string; table: string }[]
        const restored: SchemaContextEntry[] = []
        for (const entry of parsed) {
          const cols = await resolveColumns(entry.database, entry.table)
          restored.push({ database: entry.database, table: entry.table, columns: cols })
        }
        contexts = restored
        return
      } catch {
        // fall through to legacy
      }
    }

    // Legacy single-context fallback
    if (chat.context_database && chat.context_table) {
      const cols = await resolveColumns(chat.context_database, chat.context_table)
      contexts = [{ database: chat.context_database, table: chat.context_table, columns: cols }]
    } else {
      contexts = []
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

  // ── Multi-context management ──────────────────────────────
  async function addContext(dbName: string, tableName: string) {
    // Dedupe check
    if (contexts.some(c => c.database === dbName && c.table === tableName)) return

    if (contexts.length >= MAX_CONTEXTS) {
      toastError(`Maximum ${MAX_CONTEXTS} table contexts allowed`)
      return
    }

    const cols = await resolveColumns(dbName, tableName)
    contexts = [...contexts, { database: dbName, table: tableName, columns: cols }]
    persistContexts()
  }

  function removeContext(dbName: string, tableName: string) {
    contexts = contexts.filter(c => !(c.database === dbName && c.table === tableName))
    persistContexts()
  }

  function clearAllContexts() {
    contexts = []
    persistContexts()
  }

  function persistContexts() {
    if (!selectedChatId) return
    const serialized = contexts.length > 0
      ? JSON.stringify(contexts.map(c => ({ database: c.database, table: c.table })))
      : ''
    updateBrainChat(selectedChatId, { contextTables: serialized }).catch(() => {})
    chats = chats.map(c => c.id === selectedChatId ? { ...c, context_tables: serialized || null } : c)
  }

  // Header combobox handlers — additive workflow
  async function onHeaderDbChange(dbName: string) {
    headerDb = dbName
    headerTable = ''
    if (dbName) {
      const db = getDatabases().find(d => d.name === dbName)
      if (!db?.tables) await loadTables(dbName)
    }
  }

  async function onHeaderTableChange(tableName: string) {
    if (headerDb && tableName) {
      await addContext(headerDb, tableName)
      // Reset comboboxes after adding
      headerDb = ''
      headerTable = ''
    } else {
      headerTable = tableName
    }
  }

  // ── Chat actions ───────────────────────────────────────────
  async function sendMessage() {
    if (!input.trim() || streaming) return

    if (!selectedChatId) {
      await createChat('New Chat')
      if (!selectedChatId) return
    }

    const userPrompt = input.trim()
    input = ''

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

    // Build multi-schema contexts for the API
    const schemaContexts = contexts.length > 0
      ? contexts.map(c => ({
          database: c.database,
          table: c.table,
          columns: c.columns,
        }))
      : undefined

    streaming = true
    await tick()
    scrollToBottom()

    try {
      await streamBrainMessage(
        selectedChatId,
        {
          content: userPrompt,
          modelId: selectedModelId || undefined,
          schemaContexts,
        },
        (event) => {
          if (event.type === 'delta') {
            const delta = event.delta ?? ''
            messages = messages.map((m, i) => i === assistantIdx ? { ...m, content: m.content + delta } : m)
          } else if (event.type === 'error') {
            messages = messages.map((m, i) => i === assistantIdx ? { ...m, content: m.content || `Error: ${event.error ?? 'Unknown error'}`, status: 'error' } : m)
          }
        },
      )

      await Promise.all([
        selectChat(selectedChatId),
        loadChats(),
      ])
    } catch (e: any) {
      messages = messages.map((m, i) => i === assistantIdx ? { ...m, content: m.content || `Error: ${e.message}`, status: 'error' } : m)
    } finally {
      streaming = false
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
      selectedDb={headerDb}
      selectedTable={headerTable}
      {databaseOptions}
      {tableOptions}
      onModelChange={(v) => selectedModelId = v}
      onDbChange={(v) => { void onHeaderDbChange(v) }}
      onTableChange={(v) => { void onHeaderTableChange(v) }}
    />

    <div class="flex-1 overflow-auto p-6 space-y-5" bind:this={messagesEl}>
      {#if messages.length === 0}
        <BrainEmptyState />
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
      value={input}
      {streaming}
      {contexts}
      onSend={sendMessage}
      onInput={(v) => input = v}
      onAddContext={addContext}
      onRemoveContext={removeContext}
      onClearAllContexts={clearAllContexts}
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
