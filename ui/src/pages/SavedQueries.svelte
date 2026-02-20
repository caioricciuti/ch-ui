<script lang="ts">
  import { onMount } from 'svelte'
  import type { SavedQuery } from '../lib/types/api'
  import { apiGet, apiDel, apiPost } from '../lib/api/client'
  import { openQueryTab, openSavedQueryTab } from '../lib/stores/tabs.svelte'
  import { success as toastSuccess, error as toastError } from '../lib/stores/toast.svelte'
  import Button from '../lib/components/common/Button.svelte'
  import Spinner from '../lib/components/common/Spinner.svelte'
  import Combobox from '../lib/components/common/Combobox.svelte'
  import type { ComboboxOption } from '../lib/components/common/Combobox.svelte'
  import Sheet from '../lib/components/common/Sheet.svelte'
  import ContextMenu, { type ContextMenuItem } from '../lib/components/common/ContextMenu.svelte'
  import ConfirmDialog from '../lib/components/common/ConfirmDialog.svelte'
  import {
    Play,
    Trash2,
    Search,
    X,
    RefreshCw,
    MoreHorizontal,
    Copy,
    CopyPlus,
    CalendarClock,
    Hash,
    AlignLeft,
    Eye,
  } from 'lucide-svelte'

  let queries = $state<SavedQuery[]>([])
  let loading = $state(true)

  let searchTerm = $state('')
  type SortMode = 'updated-desc' | 'updated-asc' | 'name-asc' | 'name-desc' | 'length-desc'
  type FilterMode = 'all' | 'described' | 'undescribed'
  type DensityMode = 'comfortable' | 'compact'
  let sortMode = $state<SortMode>('updated-desc')
  let filterMode = $state<FilterMode>('all')
  let densityMode = $state<DensityMode>('comfortable')

  let detailsOpen = $state(false)
  let selectedQuery = $state<SavedQuery | null>(null)

  let contextMenu = $state<{ query: SavedQuery; x: number; y: number } | null>(null)

  let confirmOpen = $state(false)
  let confirmLoading = $state(false)
  let pendingDeleteId = $state<string | null>(null)
  let pendingDeleteName = $state('')

  onMount(loadQueries)

  const filterModeOptions: ComboboxOption[] = [
    { value: 'all', label: 'All' },
    { value: 'described', label: 'With description' },
    { value: 'undescribed', label: 'No description' },
  ]

  const sortModeOptions: ComboboxOption[] = [
    { value: 'updated-desc', label: 'Recently updated' },
    { value: 'updated-asc', label: 'Oldest updated' },
    { value: 'name-asc', label: 'Name A-Z' },
    { value: 'name-desc', label: 'Name Z-A' },
    { value: 'length-desc', label: 'Longest SQL' },
  ]

  const totalCount = $derived(queries.length)
  const describedCount = $derived.by(() => queries.filter((q) => !!q.description?.trim()).length)
  const recentCount = $derived.by(() => {
    const now = Date.now()
    return queries.filter((q) => {
      const ts = parseTime(q.updated_at)
      return ts > 0 && now - ts <= 7 * 24 * 60 * 60 * 1000
    }).length
  })
  const totalSqlChars = $derived.by(() => queries.reduce((acc, q) => acc + q.query.length, 0))

  const visibleQueries = $derived.by(() => {
    const term = searchTerm.trim().toLowerCase()
    let rows = queries.filter((q) => {
      const hasDesc = !!q.description?.trim()
      if (filterMode === 'described' && !hasDesc) return false
      if (filterMode === 'undescribed' && hasDesc) return false
      if (!term) return true
      return (
        q.name.toLowerCase().includes(term) ||
        (q.description ?? '').toLowerCase().includes(term) ||
        q.query.toLowerCase().includes(term)
      )
    })

    rows = rows.slice().sort((a, b) => {
      switch (sortMode) {
        case 'updated-asc':
          return parseTime(a.updated_at) - parseTime(b.updated_at)
        case 'name-asc':
          return a.name.localeCompare(b.name)
        case 'name-desc':
          return b.name.localeCompare(a.name)
        case 'length-desc':
          return b.query.length - a.query.length
        case 'updated-desc':
        default:
          return parseTime(b.updated_at) - parseTime(a.updated_at)
      }
    })

    return rows
  })

  async function loadQueries() {
    loading = true
    try {
      const res = await apiGet<{ saved_queries: SavedQuery[] }>('/api/saved-queries')
      queries = res.saved_queries ?? []
    } catch (e: any) {
      toastError(e.message)
    } finally {
      loading = false
    }
  }

  function openInEditor(query: SavedQuery) {
    closeContextMenu()
    openSavedQueryTab(query)
  }

  function openDetails(query: SavedQuery) {
    selectedQuery = query
    detailsOpen = true
    closeContextMenu()
  }

  function closeDetails() {
    detailsOpen = false
    selectedQuery = null
  }

  async function copySQL(query: SavedQuery) {
    try {
      await navigator.clipboard.writeText(query.query)
      toastSuccess(`Copied SQL for "${query.name}"`)
    } catch {
      toastError('Clipboard unavailable')
    } finally {
      closeContextMenu()
    }
  }

  async function duplicateQuery(query: SavedQuery) {
    try {
      const created = await apiPost<SavedQuery>(`/api/saved-queries/${query.id}/duplicate`)
      if (created?.id) {
        queries = [created, ...queries]
      } else {
        await loadQueries()
      }
      toastSuccess('Query duplicated')
    } catch (e: any) {
      toastError(e.message)
    } finally {
      closeContextMenu()
    }
  }

  async function deleteQuery(id: string) {
    try {
      await apiDel(`/api/saved-queries/${id}`)
      queries = queries.filter((q) => q.id !== id)
      if (selectedQuery?.id === id) closeDetails()
      toastSuccess('Query deleted')
    } catch (e: any) {
      toastError(e.message)
    }
  }

  function requestDelete(query: SavedQuery) {
    pendingDeleteName = query.name
    pendingDeleteId = query.id
    confirmOpen = true
    closeContextMenu()
  }

  function cancelDelete() {
    confirmOpen = false
    pendingDeleteId = null
    pendingDeleteName = ''
  }

  async function confirmDelete() {
    if (!pendingDeleteId) return
    confirmLoading = true
    try {
      await deleteQuery(pendingDeleteId)
      cancelDelete()
    } finally {
      confirmLoading = false
    }
  }

  function parseTime(value: string): number {
    const t = Date.parse(value)
    return Number.isFinite(t) ? t : 0
  }

  function formatDate(value: string): string {
    const t = parseTime(value)
    if (!t) return value
    return new Date(t).toLocaleString()
  }

  function formatRelative(value: string): string {
    const t = parseTime(value)
    if (!t) return 'unknown'
    const delta = Math.max(0, Date.now() - t)
    const minutes = Math.floor(delta / 60000)
    if (minutes < 1) return 'just now'
    if (minutes < 60) return `${minutes}m ago`
    const hours = Math.floor(minutes / 60)
    if (hours < 24) return `${hours}h ago`
    const days = Math.floor(hours / 24)
    if (days < 30) return `${days}d ago`
    const months = Math.floor(days / 30)
    if (months < 12) return `${months}mo ago`
    return `${Math.floor(months / 12)}y ago`
  }

  function countLines(sql: string): number {
    if (!sql.trim()) return 0
    return sql.split(/\r?\n/).length
  }

  function sqlPreview(sql: string, maxLines = 3): string {
    const lines = sql.split(/\r?\n/).slice(0, maxLines)
    const suffix = countLines(sql) > maxLines ? '\n...' : ''
    return lines.join('\n') + suffix
  }

  function clearSearch() {
    searchTerm = ''
  }

  function closeContextMenu() {
    contextMenu = null
  }

  function openContextMenu(event: MouseEvent, query: SavedQuery) {
    event.preventDefault()
    event.stopPropagation()
    contextMenu = {
      query,
      x: Math.min(window.innerWidth - 240, event.clientX),
      y: Math.min(window.innerHeight - 220, event.clientY),
    }
  }

  function openContextMenuFromButton(event: MouseEvent, query: SavedQuery) {
    event.preventDefault()
    event.stopPropagation()
    const rect = (event.currentTarget as HTMLElement).getBoundingClientRect()
    contextMenu = {
      query,
      x: Math.min(window.innerWidth - 240, rect.left - 170),
      y: Math.min(window.innerHeight - 220, rect.bottom + 6),
    }
  }

  function getContextItems(): ContextMenuItem[] {
    const row = contextMenu?.query
    if (!row) return []
    return [
      {
        id: 'open',
        label: 'Open in Editor',
        icon: Play,
        shortcut: 'Enter',
        onSelect: () => openInEditor(row),
      },
      {
        id: 'details',
        label: 'View Details',
        icon: Eye,
        shortcut: 'Space',
        onSelect: () => openDetails(row),
      },
      { id: 'sep-main', separator: true },
      {
        id: 'copy',
        label: 'Copy SQL',
        icon: Copy,
        shortcut: 'Cmd/Ctrl+C',
        onSelect: () => copySQL(row),
      },
      {
        id: 'duplicate',
        label: 'Duplicate',
        icon: CopyPlus,
        shortcut: 'Cmd/Ctrl+D',
        onSelect: () => duplicateQuery(row),
      },
      { id: 'sep-danger', separator: true },
      {
        id: 'delete',
        label: 'Delete Saved Query',
        icon: Trash2,
        danger: true,
        onSelect: () => requestDelete(row),
      },
    ]
  }
</script>

<svelte:window onkeydown={(e) => e.key === 'Escape' && closeContextMenu()} />

<div class="flex flex-col h-full overflow-hidden">
  <div class="ds-page-header shrink-0">
    <div class="flex items-center gap-3">
      <AlignLeft size={17} class="text-ch-orange" />
      <h1 class="ds-page-title">Saved Queries</h1>
    </div>
    <Button size="sm" variant="secondary" onclick={() => { void loadQueries() }}>
      <RefreshCw size={14} /> Refresh
    </Button>
  </div>

  <div class="flex-1 overflow-auto p-4">
    <div class="mx-auto max-w-6xl space-y-4">
      <section class="ds-panel rounded-xl p-4">
        <div class="grid grid-cols-2 lg:grid-cols-4 gap-3">
          <div class="ds-panel-muted p-3">
            <div class="text-[11px] uppercase tracking-wider text-gray-500">Total</div>
            <div class="mt-1 text-2xl font-semibold text-gray-900 dark:text-gray-100">{totalCount}</div>
          </div>
          <div class="ds-panel-muted p-3">
            <div class="text-[11px] uppercase tracking-wider text-gray-500">Updated 7d</div>
            <div class="mt-1 text-2xl font-semibold text-ch-orange">{recentCount}</div>
          </div>
          <div class="ds-panel-muted p-3">
            <div class="text-[11px] uppercase tracking-wider text-gray-500">With Description</div>
            <div class="mt-1 text-2xl font-semibold text-gray-900 dark:text-gray-100">{describedCount}</div>
          </div>
          <div class="ds-panel-muted p-3">
            <div class="text-[11px] uppercase tracking-wider text-gray-500">SQL Characters</div>
            <div class="mt-1 text-2xl font-semibold text-gray-900 dark:text-gray-100">{totalSqlChars.toLocaleString()}</div>
          </div>
        </div>
      </section>

      <section class="ds-panel rounded-xl p-3.5">
        <div class="grid grid-cols-1 lg:grid-cols-[1fr_auto_auto_auto] gap-2.5">
          <div class="flex items-center gap-2 rounded-lg border border-gray-300/80 dark:border-gray-700/80 bg-gray-100/60 dark:bg-gray-900/60 px-2.5">
            <Search size={14} class="text-gray-500 shrink-0" />
            <input
              type="text"
              class="w-full h-9 bg-transparent text-[13px] outline-none text-gray-800 dark:text-gray-200 placeholder:text-gray-500"
              placeholder="Search name, description, or SQL..."
              bind:value={searchTerm}
            />
            {#if searchTerm}
              <button
                class="rounded p-1 text-gray-500 hover:text-gray-700 dark:hover:text-gray-300 hover:bg-gray-200/70 dark:hover:bg-gray-800/70"
                onclick={clearSearch}
                title="Clear search"
              >
                <X size={13} />
              </button>
            {/if}
          </div>

          <label class="inline-flex items-center gap-2 px-0.5 text-[12px] font-medium text-gray-500">
            Filter
            <div class="min-w-44">
              <Combobox
                options={filterModeOptions}
                value={filterMode}
                onChange={(v) => filterMode = v as FilterMode}
              />
            </div>
          </label>

          <label class="inline-flex items-center gap-2 px-0.5 text-[12px] font-medium text-gray-500">
            Sort
            <div class="min-w-56">
              <Combobox
                options={sortModeOptions}
                value={sortMode}
                onChange={(v) => sortMode = v as SortMode}
              />
            </div>
          </label>

          <div class="ds-segment">
            <button
              class="ds-segment-btn {densityMode === 'comfortable' ? 'ds-segment-btn-active' : ''}"
              onclick={() => densityMode = 'comfortable'}
            >
              Comfortable
            </button>
            <button
              class="ds-segment-btn {densityMode === 'compact' ? 'ds-segment-btn-active' : ''}"
              onclick={() => densityMode = 'compact'}
            >
              Compact
            </button>
          </div>
        </div>
      </section>

      {#if loading}
        <div class="flex items-center justify-center py-14"><Spinner /></div>
      {:else if queries.length === 0}
        <div class="ds-empty rounded-xl p-8">
          <p class="text-sm text-gray-500">No saved queries yet</p>
          <p class="text-xs text-gray-400 mt-1">Save a query from the SQL editor and it will appear here.</p>
          <div class="mt-4">
            <Button size="sm" onclick={() => openQueryTab()}>
              <Play size={14} /> Open New Query
            </Button>
          </div>
        </div>
      {:else if visibleQueries.length === 0}
        <div class="ds-empty rounded-xl p-8">
          <p class="text-sm text-gray-500">No query matches your filters.</p>
          <p class="text-xs text-gray-400 mt-1">Try another search, filter, or sorting mode.</p>
        </div>
      {:else}
        <div class="grid grid-cols-1 gap-3">
          {#each visibleQueries as query (query.id)}
            <article
              class="ds-panel rounded-xl transition-colors hover:border-orange-400/45"
              oncontextmenu={(e) => openContextMenu(e, query)}
            >
              <div class="px-4 py-3">
                <div class="flex items-start gap-3">
                  <div class="min-w-0 flex-1">
                    <div class="flex items-center gap-2 flex-wrap">
                      <h3 class="text-sm font-semibold text-gray-900 dark:text-gray-100 truncate">{query.name}</h3>
                      <span class="brand-pill rounded-md px-1.5 py-0.5 text-[10px] font-semibold">
                        {countLines(query.query)} lines
                      </span>
                    </div>
                    {#if query.description?.trim()}
                      <p class="mt-1 text-xs text-gray-500 line-clamp-2">{query.description}</p>
                    {/if}
                  </div>

                  <button
                    class="rounded-md p-1.5 text-gray-500 hover:text-gray-700 dark:hover:text-gray-300 hover:bg-gray-200/70 dark:hover:bg-gray-800/70"
                    onclick={(e) => openContextMenuFromButton(e, query)}
                    title="More actions"
                  >
                    <MoreHorizontal size={15} />
                  </button>
                </div>

                <pre class="mt-3 ds-panel-muted p-3 text-[12px] leading-relaxed text-gray-700 dark:text-gray-300 font-mono overflow-x-auto whitespace-pre">{sqlPreview(query.query, densityMode === 'compact' ? 2 : 4)}</pre>

                <div class="mt-3 flex items-center justify-between gap-3 flex-wrap">
                  <div class="flex items-center gap-3 text-[11px] text-gray-500">
                    <span class="inline-flex items-center gap-1"><CalendarClock size={12} /> {formatRelative(query.updated_at)}</span>
                    <span class="inline-flex items-center gap-1"><Hash size={12} /> {query.query.length.toLocaleString()} chars</span>
                  </div>

                  <div class="inline-flex items-center gap-1.5">
                    <button
                      class="ds-btn-outline px-2.5 py-1.5"
                      onclick={() => openDetails(query)}
                    >
                      <Eye size={12} /> Details
                    </button>
                    <button
                      class="ds-btn-outline px-2.5 py-1.5"
                      onclick={() => copySQL(query)}
                    >
                      <Copy size={12} /> Copy SQL
                    </button>
                    <button
                      class="ds-btn-primary px-2.5 py-1.5"
                      onclick={() => openInEditor(query)}
                    >
                      <Play size={12} /> Open
                    </button>
                  </div>
                </div>
              </div>
            </article>
          {/each}
        </div>
      {/if}
    </div>
  </div>
</div>

<ConfirmDialog
  open={confirmOpen}
  title="Delete saved query?"
  description={pendingDeleteName ? `Delete "${pendingDeleteName}"? This action cannot be undone.` : 'This action cannot be undone.'}
  confirmLabel="Delete"
  destructive={true}
  loading={confirmLoading}
  onconfirm={confirmDelete}
  oncancel={cancelDelete}
/>

<ContextMenu
  open={!!contextMenu}
  x={contextMenu?.x ?? 0}
  y={contextMenu?.y ?? 0}
  items={getContextItems()}
  onclose={closeContextMenu}
/>

<Sheet
  open={detailsOpen}
  title={selectedQuery?.name ?? 'Saved Query'}
  size="lg"
  onclose={closeDetails}
>
  {#if selectedQuery}
    <div class="space-y-4">
      <div class="grid grid-cols-1 sm:grid-cols-2 gap-3">
        <div class="ds-panel-muted p-3">
          <div class="text-[11px] uppercase tracking-wider text-gray-500">Updated</div>
          <div class="mt-1 text-sm text-gray-900 dark:text-gray-100">{formatDate(selectedQuery.updated_at)}</div>
        </div>
        <div class="ds-panel-muted p-3">
          <div class="text-[11px] uppercase tracking-wider text-gray-500">Created</div>
          <div class="mt-1 text-sm text-gray-900 dark:text-gray-100">{formatDate(selectedQuery.created_at)}</div>
        </div>
        <div class="ds-panel-muted p-3">
          <div class="text-[11px] uppercase tracking-wider text-gray-500">Line Count</div>
          <div class="mt-1 text-sm text-gray-900 dark:text-gray-100">{countLines(selectedQuery.query)}</div>
        </div>
        <div class="ds-panel-muted p-3">
          <div class="text-[11px] uppercase tracking-wider text-gray-500">Characters</div>
          <div class="mt-1 text-sm text-gray-900 dark:text-gray-100">{selectedQuery.query.length.toLocaleString()}</div>
        </div>
      </div>

      {#if selectedQuery.description?.trim()}
        <div class="ds-panel-muted p-3">
          <div class="text-[11px] uppercase tracking-wider text-gray-500">Description</div>
          <p class="mt-1 text-sm text-gray-700 dark:text-gray-300">{selectedQuery.description}</p>
        </div>
      {/if}

      <div class="ds-panel-muted p-3">
        <div class="text-[11px] uppercase tracking-wider text-gray-500 mb-2">SQL</div>
        <pre class="text-[12px] leading-relaxed text-gray-800 dark:text-gray-200 font-mono overflow-x-auto whitespace-pre p-2 rounded-md bg-gray-100 dark:bg-gray-950 border border-gray-200 dark:border-gray-800">{selectedQuery.query}</pre>
      </div>

      <div class="flex items-center gap-2 flex-wrap">
        <Button size="sm" onclick={() => selectedQuery && openInEditor(selectedQuery)}>
          <Play size={13} /> Open in Editor
        </Button>
        <Button size="sm" variant="secondary" onclick={() => selectedQuery && copySQL(selectedQuery)}>
          <Copy size={13} /> Copy SQL
        </Button>
        <Button size="sm" variant="secondary" onclick={() => selectedQuery && duplicateQuery(selectedQuery)}>
          <CopyPlus size={13} /> Duplicate
        </Button>
        <Button size="sm" variant="danger" onclick={() => selectedQuery && requestDelete(selectedQuery)}>
          <Trash2 size={13} /> Delete
        </Button>
      </div>
    </div>
  {/if}
</Sheet>
