<script lang="ts">
  import { onMount, tick } from 'svelte'
  import { openQueryTab } from '../lib/stores/tabs.svelte'
  import { success as toastSuccess, error as toastError } from '../lib/stores/toast.svelte'
  import { getDatabases, loadDatabases, loadTables, loadColumns } from '../lib/stores/schema.svelte'
  import type { Database } from '../lib/types/schema'
  import type { BrainArtifact, BrainChat, BrainMessage as BrainMessageType, BrainModelOption } from '../lib/types/brain'
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

  // Schema context
  let selectedDb = $state('')
  let selectedTable = $state('')
  let contextColumns = $state<{ name: string; type: string }[]>([])

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

  const contextLabel = $derived(
    selectedDb && selectedTable && contextColumns.length > 0
      ? `${selectedDb}.${selectedTable} (${contextColumns.length} cols)`
      : ''
  )

  const databaseOptions = $derived.by<ComboboxOption[]>(() =>
    getDatabases().map(db => ({
      value: db.name,
      label: db.name,
      hint: `${db.tables?.length ?? 0} tables`,
      keywords: db.name,
    }))
  )

  const tableOptions = $derived.by<ComboboxOption[]>(() => {
    const db = getDatabases().find(d => d.name === selectedDb)
    const tables = db?.tables?.map(t => t.name) ?? []
    return tables.map(t => ({
      value: t,
      label: t,
      hint: selectedDb,
      keywords: `${selectedDb}.${t}`,
    }))
  })

  // ── Lifecycle ──────────────────────────────────────────────
  onMount(async () => {
    const dbs = getDatabases()
    if (dbs.length === 0) loadDatabases()

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

  async function renameChat(chat: BrainChat) {
    const next = prompt('New chat name', chat.title)
    if (!next) return
    try {
      await updateBrainChat(chat.id, { title: next })
      chats = chats.map(c => c.id === chat.id ? { ...c, title: next } : c)
      if (selectedChatId === chat.id) {
        await selectChat(chat.id)
      }
    } catch (e: any) {
      toastError(e.message)
    }
  }

  async function removeChat(chat: BrainChat) {
    if (!confirm(`Delete chat "${chat.title}"?`)) return
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

  // ── Schema context ─────────────────────────────────────────
  async function onDbChange(dbName: string) {
    selectedDb = dbName
    selectedTable = ''
    contextColumns = []
    if (dbName) {
      const db = getDatabases().find(d => d.name === dbName)
      if (!db?.tables) await loadTables(dbName)
    }
  }

  async function onTableChange(tableName: string) {
    selectedTable = tableName
    contextColumns = []
    if (selectedDb && tableName) {
      await loadColumns(selectedDb, tableName)
      const db = getDatabases().find(d => d.name === selectedDb)
      const table = db?.tables?.find(t => t.name === tableName)
      contextColumns = (table?.columns ?? []).map(c => ({ name: c.name, type: c.type }))
    }
  }

  function clearContext() {
    selectedDb = ''
    selectedTable = ''
    contextColumns = []
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

    let schemaContext: any = undefined
    if (selectedDb && selectedTable && contextColumns.length > 0) {
      schemaContext = {
        database: selectedDb,
        table: selectedTable,
        columns: contextColumns,
      }
    }

    streaming = true
    await tick()
    scrollToBottom()

    try {
      await streamBrainMessage(
        selectedChatId,
        {
          content: userPrompt,
          modelId: selectedModelId || undefined,
          schemaContext,
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
      {selectedDb}
      {selectedTable}
      {contextColumns}
      {databaseOptions}
      {tableOptions}
      onModelChange={(v) => selectedModelId = v}
      onDbChange={(v) => { void onDbChange(v) }}
      onTableChange={(v) => { void onTableChange(v) }}
      onClearContext={clearContext}
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
      contextActive={!!contextLabel}
      {contextLabel}
      onSend={sendMessage}
      onInput={(v) => input = v}
    />
  </main>
</div>
