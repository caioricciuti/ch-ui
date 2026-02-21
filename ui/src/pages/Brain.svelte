<script lang="ts">
  import { onMount, tick } from 'svelte'
  import { openQueryTab } from '../lib/stores/tabs.svelte'
  import { success as toastSuccess, error as toastError } from '../lib/stores/toast.svelte'
  import { getDatabases, loadDatabases, loadTables, loadColumns } from '../lib/stores/schema.svelte'
  import type { Database } from '../lib/types/schema'
  import type { BrainArtifact, BrainChat, BrainMessage, BrainModelOption } from '../lib/types/brain'
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
  import Spinner from '../lib/components/common/Spinner.svelte'
  import Button from '../lib/components/common/Button.svelte'
  import Combobox from '../lib/components/common/Combobox.svelte'
  import type { ComboboxOption } from '../lib/components/common/Combobox.svelte'
  import { Brain, Plus, Send, Search, Trash2, Edit3, Play, Database as DbIcon, MoveHorizontal } from 'lucide-svelte'

  let loading = $state(true)
  let chats = $state<BrainChat[]>([])
  let models = $state<BrainModelOption[]>([])
  let selectedChatId = $state<string>('')
  let messages = $state<BrainMessage[]>([])
  let artifacts = $state<BrainArtifact[]>([])
  let input = $state('')
  let streaming = $state(false)
  let search = $state('')
  let selectedModelId = $state('')
  let messagesEl: HTMLDivElement | undefined = $state()

  // Schema context
  let showContext = $state(false)
  let selectedDb = $state('')
  let selectedTable = $state('')
  let contextColumns = $state<{ name: string; type: string }[]>([])

  // Artifacts pane resizing
  let artifactsPaneWidth = $state(360)
  let resizingArtifactsPane = $state(false)
  let paneResizeStartX = $state(0)
  let paneResizeStartWidth = $state(360)

  onMount(async () => {
    const dbs = getDatabases()
    if (dbs.length === 0) loadDatabases()

    await Promise.all([loadModels(), loadChats()])
    if (!selectedChatId) {
      await createChat('New Chat')
    }
    loading = false
  })

  const modelOptions = $derived.by<ComboboxOption[]>(() =>
    models.map(m => ({
      value: m.id,
      label: `${m.display_name || m.name}`,
      hint: `${m.provider_name} · ${m.provider_kind}`,
      keywords: `${m.name} ${m.display_name || ''} ${m.provider_name} ${m.provider_kind}`,
    }))
  )

  const databaseOptions = $derived.by<ComboboxOption[]>(() =>
    databases().map(db => ({
      value: db.name,
      label: db.name,
      hint: `${db.tables?.length ?? 0} tables`,
      keywords: db.name,
    }))
  )

  const tableOptions = $derived.by<ComboboxOption[]>(() =>
    tablesForDb().map(t => ({
      value: t,
      label: t,
      hint: selectedDb,
      keywords: `${selectedDb}.${t}`,
    }))
  )

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
    if (!confirm(`Delete chat \"${chat.title}\"?`)) return
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

  function filteredChats(): BrainChat[] {
    const term = search.trim().toLowerCase()
    if (!term) return chats
    return chats.filter(c => c.title.toLowerCase().includes(term))
  }

  function databases(): Database[] {
    return getDatabases()
  }

  function tablesForDb(): string[] {
    const db = getDatabases().find(d => d.name === selectedDb)
    return db?.tables?.map(t => t.name) ?? []
  }

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

  async function sendMessage() {
    if (!input.trim() || streaming) return

    if (!selectedChatId) {
      await createChat('New Chat')
      if (!selectedChatId) return
    }

    const prompt = input.trim()
    input = ''

    const tempUser: BrainMessage = {
      id: `tmp-user-${Date.now()}`,
      chat_id: selectedChatId,
      role: 'user',
      content: prompt,
      status: 'complete',
      created_at: new Date().toISOString(),
      updated_at: new Date().toISOString(),
    }
    const tempAssistant: BrainMessage = {
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
          content: prompt,
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

  function scrollToBottom() {
    if (messagesEl) messagesEl.scrollTop = messagesEl.scrollHeight
  }

  function escapeHtml(s: string): string {
    return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;')
  }

  function renderMarkdown(content: string): string {
    let html = escapeHtml(content)
    html = html.replace(/```(\w+)\n([\s\S]*?)```/g, (_m, lang, code) =>
      `<pre class="bg-gray-100 dark:bg-gray-800 rounded p-3 my-2 overflow-x-auto text-xs font-mono"><code data-lang="${escapeHtml(lang)}">${code}</code></pre>`
    )
    html = html.replace(/```\n?([\s\S]*?)```/g, (_m, code) =>
      `<pre class="bg-gray-100 dark:bg-gray-800 rounded p-3 my-2 overflow-x-auto text-xs font-mono"><code>${code}</code></pre>`
    )
    html = html.replace(/`([^`]+)`/g, '<code class="bg-gray-200 dark:bg-gray-700 px-1 py-0.5 rounded text-xs">$1</code>')
    html = html.replace(/\*\*(.+?)\*\*/g, '<strong>$1</strong>')
    html = html.replace(/\n/g, '<br/>')
    return html
  }

  function extractSqlBlocks(content: string): string[] {
    const regex = /```sql\n([\s\S]*?)```/g
    const blocks: string[] = []
    let match
    while ((match = regex.exec(content)) !== null) {
      blocks.push(match[1].trim())
    }
    return blocks
  }

  async function runSqlInChat(sql: string, messageId?: string) {
    if (!selectedChatId) return
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
    }
  }

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      sendMessage()
    }
  }

  function formatTime(ts?: string | null): string {
    if (!ts) return ''
    try {
      return new Date(ts).toLocaleString()
    } catch {
      return ts
    }
  }

  function parseArtifactPayload(artifact: BrainArtifact): any | null {
    if (artifact.type !== 'query_result') return null
    try {
      return JSON.parse(artifact.content)
    } catch {
      return null
    }
  }

  function artifactRows(artifact: BrainArtifact): Record<string, any>[] {
    const payload = parseArtifactPayload(artifact)
    if (!payload || !Array.isArray(payload.data)) return []
    return payload.data
  }

  function artifactColumns(artifact: BrainArtifact): string[] {
    const payload = parseArtifactPayload(artifact)
    if (payload?.meta && Array.isArray(payload.meta) && payload.meta.length > 0) {
      return payload.meta.map((m: any) => String(m?.name ?? '')).filter(Boolean)
    }
    const rows = artifactRows(artifact)
    if (rows.length === 0) return []
    return Object.keys(rows[0])
  }

  function startArtifactsPaneResize(e: MouseEvent) {
    resizingArtifactsPane = true
    paneResizeStartX = e.clientX
    paneResizeStartWidth = artifactsPaneWidth
    e.preventDefault()
  }

  function onWindowMouseMove(e: MouseEvent) {
    if (!resizingArtifactsPane) return
    const delta = paneResizeStartX - e.clientX
    artifactsPaneWidth = Math.min(760, Math.max(300, paneResizeStartWidth + delta))
  }

  function onWindowMouseUp() {
    resizingArtifactsPane = false
  }
</script>

<svelte:window onmousemove={onWindowMouseMove} onmouseup={onWindowMouseUp} />

<div class="flex h-full">
  <!-- Sidebar -->
  <aside class="w-72 border-r border-gray-200 dark:border-gray-800 flex flex-col">
    <div class="p-3 border-b border-gray-200 dark:border-gray-800">
      <button
        class="w-full inline-flex items-center justify-center gap-1.5 px-3 py-1.5 text-xs rounded bg-ch-blue text-white hover:bg-ch-blue/80"
        onclick={() => createChat('New Chat')}
      >
        <Plus size={14} />
        <span>New Chat</span>
      </button>
      <div class="mt-2 flex items-center gap-2 border border-gray-300 dark:border-gray-700 rounded px-2 py-1">
        <Search size={14} class="text-gray-400" />
        <input class="w-full bg-transparent text-xs" placeholder="Search chats" bind:value={search} />
      </div>
    </div>

    <div class="flex-1 overflow-auto p-2 space-y-1">
      {#if loading}
        <div class="flex items-center justify-center py-6"><Spinner size="sm" /></div>
      {:else if filteredChats().length === 0}
        <p class="text-xs text-gray-500 px-2 py-3">No chats</p>
      {:else}
        {#each filteredChats() as chat}
          <div
            class="w-full text-left rounded px-2 py-2 hover:bg-gray-100 dark:hover:bg-gray-800 {selectedChatId === chat.id ? 'bg-gray-100 dark:bg-gray-800' : ''}"
            onclick={() => selectChat(chat.id)}
            onkeydown={(e) => (e.key === 'Enter' || e.key === ' ') && selectChat(chat.id)}
            role="button"
            tabindex="0"
          >
            <div class="text-sm text-gray-800 dark:text-gray-200 truncate">{chat.title}</div>
            <div class="text-[11px] text-gray-500 mt-0.5">{formatTime(chat.last_message_at ?? chat.updated_at)}</div>
            <div class="mt-1 flex items-center gap-2">
              <button class="text-[11px] text-gray-500 hover:text-ch-blue" onclick={(e) => { e.stopPropagation(); renameChat(chat) }}>
                <Edit3 size={12} />
              </button>
              <button class="text-[11px] text-gray-500 hover:text-red-500" onclick={(e) => { e.stopPropagation(); removeChat(chat) }}>
                <Trash2 size={12} />
              </button>
            </div>
          </div>
        {/each}
      {/if}
    </div>
  </aside>

  <!-- Main chat -->
  <main class="flex-1 flex flex-col min-w-0">
    <div class="border-b border-gray-200 dark:border-gray-800 px-4 py-3 flex items-center gap-3">
      <Brain size={18} class="text-ch-blue" />
      <h1 class="text-lg font-semibold text-gray-900 dark:text-gray-100">Brain</h1>
      <div class="ml-auto w-80 max-w-[45%]">
        <Combobox
          options={modelOptions}
          value={selectedModelId}
          placeholder="Select model"
          onChange={(v) => selectedModelId = v}
        />
      </div>
    </div>

    <div class="flex-1 overflow-auto p-4 space-y-4" bind:this={messagesEl}>
      {#if messages.length === 0}
        <div class="h-full flex items-center justify-center text-center text-gray-400">
          <div>
            <Brain size={40} class="mx-auto mb-2 text-gray-300 dark:text-gray-700" />
            <p class="text-sm">Start a chat to generate SQL and insights.</p>
          </div>
        </div>
      {:else}
        {#each messages as msg, i}
          {#if msg.role === 'user'}
            <div class="flex justify-end">
              <div class="max-w-[80%] bg-ch-blue text-white rounded-lg rounded-br-sm px-4 py-2.5 text-sm whitespace-pre-wrap">{msg.content}</div>
            </div>
          {:else}
            <div class="flex justify-start">
              <div class="max-w-[85%] bg-gray-100 dark:bg-gray-800 text-gray-800 dark:text-gray-200 rounded-lg rounded-bl-sm px-4 py-2.5 text-sm">
                {#if msg.content}
                  {@html renderMarkdown(msg.content)}
                  {#each extractSqlBlocks(msg.content) as sql}
                    <div class="mt-2 flex items-center gap-2">
                      <button class="text-xs text-ch-blue hover:text-orange-400 font-medium flex items-center gap-1" onclick={() => openQueryTab(sql)}>
                        <Play size={12} /> Open in Editor
                      </button>
                      <button class="text-xs text-ch-blue hover:text-orange-400 font-medium flex items-center gap-1" onclick={() => runSqlInChat(sql, msg.id)}>
                        <Play size={12} /> Run in Chat
                      </button>
                    </div>
                  {/each}
                {:else if streaming && i === messages.length - 1}
                  <Spinner size="sm" />
                {/if}
              </div>
            </div>
          {/if}
        {/each}
      {/if}
    </div>

    <div class="border-t border-gray-200 dark:border-gray-800 p-4">
      <div class="flex items-end gap-2">
        <textarea
          class="flex-1 text-sm bg-transparent border border-gray-300 dark:border-gray-700 rounded-lg px-3 py-2 text-gray-800 dark:text-gray-200 resize-none"
          placeholder="Ask Brain about your data..."
          rows="2"
          bind:value={input}
          onkeydown={handleKeydown}
          disabled={streaming}
        ></textarea>
        <Button size="sm" onclick={sendMessage} loading={streaming} disabled={!input.trim() || streaming}>
          <Send size={14} />
        </Button>
      </div>
    </div>
  </main>

  <!-- Resizer -->
  <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
  <div
    class="w-2 cursor-col-resize border-l border-gray-200 dark:border-gray-800 bg-transparent hover:bg-gray-200/50 dark:hover:bg-gray-700/50 flex items-center justify-center"
    onmousedown={startArtifactsPaneResize}
    title="Resize artifacts panel"
    role="separator"
    aria-orientation="vertical"
  >
    <MoveHorizontal size={12} class="text-gray-400" />
  </div>

  <!-- Right context/artifacts pane -->
  <aside class="border-l border-gray-200 dark:border-gray-800 flex flex-col" style={`width: ${artifactsPaneWidth}px;`}>
    <div class="px-4 py-3 border-b border-gray-200 dark:border-gray-800 flex items-center gap-2">
      <DbIcon size={14} class="text-gray-500" />
      <div class="text-sm font-medium">Context & Artifacts</div>
    </div>

    <div class="p-3 border-b border-gray-200 dark:border-gray-800 space-y-2">
      <button
        class="text-xs text-gray-500 hover:text-gray-700 dark:hover:text-gray-300"
        onclick={() => showContext = !showContext}
      >
        {showContext ? 'Hide schema context' : 'Show schema context'}
      </button>

      {#if showContext}
        <Combobox
          options={databaseOptions}
          value={selectedDb}
          placeholder="Database..."
          onChange={(v) => { void onDbChange(v) }}
        />

        {#if selectedDb}
          <Combobox
            options={tableOptions}
            value={selectedTable}
            placeholder="Table..."
            onChange={(v) => { void onTableChange(v) }}
          />
        {/if}

        {#if contextColumns.length > 0}
          <div class="text-xs px-2 py-1 bg-ch-blue/10 text-ch-blue rounded">
            {selectedDb}.{selectedTable} ({contextColumns.length} columns)
          </div>
        {/if}
      {/if}
    </div>

    <div class="flex-1 overflow-auto p-3 space-y-3">
      {#if artifacts.length === 0}
        <p class="text-xs text-gray-500">No artifacts yet.</p>
      {:else}
        {#each artifacts as artifact}
          {@const payload = parseArtifactPayload(artifact)}
          {@const rows = artifactRows(artifact)}
          {@const cols = artifactColumns(artifact)}

          <div class="border border-gray-200 dark:border-gray-700 rounded p-2.5 bg-gray-50/70 dark:bg-gray-900/50">
            <div class="text-xs font-semibold text-gray-800 dark:text-gray-200">{artifact.title}</div>
            <div class="text-[11px] text-gray-500 mt-1">{artifact.type} · {formatTime(artifact.created_at)}</div>

            {#if artifact.type === 'query_result' && payload}
              <div class="mt-2 text-[11px] text-gray-600 dark:text-gray-400">
                {rows.length} rows
                {#if payload?.statistics?.elapsed}
                  · elapsed {payload.statistics.elapsed}s
                {/if}
              </div>

              {#if cols.length > 0}
                <div class="mt-2 border border-gray-200 dark:border-gray-700 rounded overflow-auto resize-y min-h-32 max-h-96">
                  <table class="min-w-full text-[11px] font-mono">
                    <thead class="bg-gray-100 dark:bg-gray-800 sticky top-0">
                      <tr>
                        {#each cols as col}
                          <th class="px-2 py-1 text-left text-gray-600 dark:text-gray-300 border-b border-gray-200 dark:border-gray-700 whitespace-nowrap">{col}</th>
                        {/each}
                      </tr>
                    </thead>
                    <tbody>
                      {#each rows as row}
                        <tr class="odd:bg-white/70 even:bg-gray-50/70 dark:odd:bg-gray-900/30 dark:even:bg-gray-800/30">
                          {#each cols as col}
                            <td class="px-2 py-1 border-b border-gray-200/70 dark:border-gray-700/70 align-top whitespace-nowrap">{String(row[col] ?? '')}</td>
                          {/each}
                        </tr>
                      {/each}
                    </tbody>
                  </table>
                </div>
              {/if}

              {#if payload?.query}
                <details class="mt-2">
                  <summary class="text-[11px] text-ch-blue cursor-pointer">View query</summary>
                  <pre class="mt-1 text-[11px] whitespace-pre-wrap bg-gray-100 dark:bg-gray-800 rounded p-2 max-h-40 overflow-auto">{payload.query}</pre>
                </details>
              {/if}

              <details class="mt-2">
                <summary class="text-[11px] text-ch-blue cursor-pointer">View raw payload</summary>
                <pre class="mt-1 text-[11px] whitespace-pre-wrap bg-gray-100 dark:bg-gray-800 rounded p-2 max-h-52 overflow-auto">{artifact.content}</pre>
              </details>
            {:else}
              <details class="mt-2">
                <summary class="text-[11px] text-ch-blue cursor-pointer">View payload</summary>
                <pre class="mt-1 text-[11px] whitespace-pre-wrap bg-gray-100 dark:bg-gray-800 rounded p-2 max-h-52 overflow-auto">{artifact.content}</pre>
              </details>
            {/if}
          </div>
        {/each}
      {/if}
    </div>
  </aside>
</div>
