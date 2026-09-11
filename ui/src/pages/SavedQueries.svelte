<script lang="ts">
  import { onMount } from 'svelte'
  import type { SavedQuery } from '../lib/types/api'
  import { apiGet, apiDel, apiPost, apiPut } from '../lib/api/client'
  import { openQueryTab, openSavedQueryTab, renameSavedQueryTabs } from '../lib/stores/tabs.svelte'
  import { detectQueryParams } from '../lib/utils/query-params'
  import { formatDate, formatRelativeTime } from '../lib/utils/format'
  import { success as toastSuccess, error as toastError } from '../lib/stores/toast.svelte'
  import Button from '../lib/components/common/Button.svelte'
  import Spinner from '../lib/components/common/Spinner.svelte'
  import Input from '../lib/components/common/Input.svelte'
  import Sheet from '../lib/components/common/Sheet.svelte'
  import ContextMenu, { type ContextMenuItem } from '../lib/components/common/ContextMenu.svelte'
  import ConfirmDialog from '../lib/components/common/ConfirmDialog.svelte'
  import InputDialog from '../lib/components/common/InputDialog.svelte'
  import PageHeader from '../lib/components/common/PageHeader.svelte'
  import PageBody from '../lib/components/common/PageBody.svelte'
  import Panel from '../lib/components/common/Panel.svelte'
  import Badge from '../lib/components/common/Badge.svelte'
  import EmptyState from '../lib/components/common/EmptyState.svelte'
  import Tabs from '../lib/components/common/Tabs.svelte'
  import {
    Play,
    Trash2,
    Search,
    X,
    RefreshCw,
    MoreHorizontal,
    Copy,
    CopyPlus,
    FileCode2,
    Eye,
    Pencil,
    BadgeCheck,
    ChevronUp,
    ChevronDown,
  } from 'lucide-svelte'

  let queries = $state<SavedQuery[]>([])
  let loading = $state(true)

  let searchTerm = $state('')
  type SortKey = 'name' | 'updated'
  type Scope = 'all' | 'verified'
  let sortKey = $state<SortKey>('updated')
  let sortDir = $state<'asc' | 'desc'>('desc')
  let scope = $state<Scope>('all')

  let detailsOpen = $state(false)
  let selectedQuery = $state<SavedQuery | null>(null)

  let contextMenu = $state<{ query: SavedQuery; x: number; y: number } | null>(null)

  let confirmOpen = $state(false)
  let confirmLoading = $state(false)
  let pendingDeleteId = $state<string | null>(null)
  let pendingDeleteName = $state('')

  let renameOpen = $state(false)
  let renameLoading = $state(false)
  let renameId = $state<string | null>(null)
  let renameValue = $state('')

  onMount(loadQueries)

  const verifiedCount = $derived(queries.filter((q) => q.verified).length)

  const visibleQueries = $derived.by(() => {
    const term = searchTerm.trim().toLowerCase()
    const rows = queries.filter((q) => {
      if (scope === 'verified' && !q.verified) return false
      if (!term) return true
      return (
        q.name.toLowerCase().includes(term) ||
        (q.description ?? '').toLowerCase().includes(term) ||
        q.query.toLowerCase().includes(term)
      )
    })
    const dir = sortDir === 'asc' ? 1 : -1
    return rows.slice().sort((a, b) =>
      sortKey === 'name'
        ? a.name.localeCompare(b.name) * dir
        : (parseTime(a.updated_at) - parseTime(b.updated_at)) * dir,
    )
  })

  // Click a column header to sort by it; click again to flip. Name defaults
  // to A-Z, Updated to newest first, the way a file manager does it.
  function sortBy(key: SortKey) {
    if (sortKey === key) {
      sortDir = sortDir === 'asc' ? 'desc' : 'asc'
    } else {
      sortKey = key
      sortDir = key === 'name' ? 'asc' : 'desc'
    }
  }

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

  function requestRename(query: SavedQuery) {
    renameId = query.id
    renameValue = query.name
    renameOpen = true
    closeContextMenu()
  }

  function cancelRename() {
    renameOpen = false
    renameId = null
    renameValue = ''
  }

  // Verified marks a query a human reviewed as correct. The MCP server tells
  // AI clients to prefer verified queries over writing new SQL.
  async function toggleVerified(q: SavedQuery) {
    const next = !q.verified
    try {
      await apiPut<SavedQuery>(`/api/saved-queries/${q.id}`, { verified: next })
      queries = queries.map((row) => (row.id === q.id ? { ...row, verified: next } : row))
      if (selectedQuery?.id === q.id) selectedQuery = { ...selectedQuery, verified: next }
      toastSuccess(next ? 'Marked as verified' : 'Verified mark removed')
    } catch (e: any) {
      toastError(e.message)
    }
  }

  async function confirmRename(newName: string) {
    const name = newName.trim()
    if (!renameId || !name) return
    renameLoading = true
    try {
      const updated = await apiPut<SavedQuery>(`/api/saved-queries/${renameId}`, { name })
      const finalName = updated?.name ?? name
      queries = queries.map((q) => (q.id === renameId ? { ...q, name: finalName } : q))
      if (selectedQuery?.id === renameId) selectedQuery = { ...selectedQuery, name: finalName }
      // Keep any open tab linked to this saved query in sync.
      renameSavedQueryTabs(renameId, finalName)
      toastSuccess('Query renamed')
      cancelRename()
    } catch (e: any) {
      toastError(e.message)
    } finally {
      renameLoading = false
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

  function countLines(sql: string): number {
    if (!sql.trim()) return 0
    return sql.split(/\r?\n/).length
  }

  function clearSearch() {
    searchTerm = ''
  }

  // Stored default values for a saved query's parameters ({name: value}).
  function storedParamDefaults(q: SavedQuery): Record<string, string> {
    if (!q.parameters) return {}
    try {
      const parsed = JSON.parse(q.parameters)
      return parsed && typeof parsed === 'object' ? parsed : {}
    } catch {
      return {}
    }
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
        id: 'rename',
        label: 'Rename',
        icon: Pencil,
        shortcut: 'F2',
        onSelect: () => requestRename(row),
      },
      {
        id: 'verify',
        label: row.verified ? 'Remove verified mark' : 'Mark as verified',
        icon: BadgeCheck,
        onSelect: () => toggleVerified(row),
      },
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

<div class="flex h-full min-h-0 flex-col">
  <PageHeader title="Saved Queries" subtitle="Saved from the editor, reusable by people and MCP clients.">
    {#snippet meta()}
      {#if !loading}<Badge>{queries.length}</Badge>{/if}
    {/snippet}
    {#snippet actions()}
      <Button icon variant="ghost" size="sm" aria-label="Refresh" title="Refresh" onclick={() => { void loadQueries() }}>
        <RefreshCw size={14} />
      </Button>
    {/snippet}
  </PageHeader>

  <PageBody width="md">
    {#if loading}
      <div class="flex items-center justify-center py-14"><Spinner /></div>
    {:else if queries.length === 0}
      <EmptyState
        icon={FileCode2}
        title="No saved queries yet"
        description="Save a query from the SQL editor and it will show up here."
        primary={{ label: 'Open the editor', onclick: () => openQueryTab() }}
      />
    {:else}
      <div class="space-y-3">
        <div class="flex items-center gap-2">
          <div class="relative flex-1">
            <Search size={14} class="pointer-events-none absolute left-2.5 top-1/2 -translate-y-1/2 text-fg-4" />
            <Input type="search" class="pl-8 pr-8" placeholder="Search by name, description or SQL" bind:value={searchTerm} spellcheck={false} />
            {#if searchTerm}
              <Button icon variant="ghost" size="xs" class="absolute right-1 top-1/2 -translate-y-1/2" aria-label="Clear search" onclick={clearSearch}>
                <X size={13} />
              </Button>
            {/if}
          </div>
          <Tabs
            variant="segmented"
            items={[{ id: 'all', label: 'All', count: queries.length }, { id: 'verified', label: 'Verified', count: verifiedCount }]}
            value={scope}
            onchange={(id) => scope = id as Scope}
          />
        </div>

        {#if visibleQueries.length === 0}
          <EmptyState
            icon={Search}
            title="Nothing matches"
            description={scope === 'verified' ? 'No verified query matches. Verified queries are the ones a person reviewed.' : 'Try another search.'}
            secondary={{ label: 'Clear search', onclick: () => { clearSearch(); scope = 'all' } }}
          />
        {:else}
          <div class="overflow-hidden rounded-lg border border-edge-subtle bg-surface">
            <table class="w-full table-fixed text-[13px]">
              <colgroup>
                <col style="width: 38%" />
                <col />
                <col style="width: 120px" />
                <col style="width: 88px" />
              </colgroup>
              <thead>
                <tr class="border-b border-edge-subtle">
                  <th class="h-8 px-3 text-left text-xs font-medium text-fg-3" aria-sort={sortKey === 'name' ? (sortDir === 'asc' ? 'ascending' : 'descending') : undefined}>
                    <button class="inline-flex items-center gap-1 hover:text-fg" onclick={() => sortBy('name')}>
                      Name
                      {#if sortKey === 'name'}{#if sortDir === 'asc'}<ChevronUp size={12} />{:else}<ChevronDown size={12} />{/if}{/if}
                    </button>
                  </th>
                  <th class="h-8 px-3 text-left text-xs font-medium text-fg-3">Description</th>
                  <th class="h-8 px-3 text-left text-xs font-medium text-fg-3" aria-sort={sortKey === 'updated' ? (sortDir === 'asc' ? 'ascending' : 'descending') : undefined}>
                    <button class="inline-flex items-center gap-1 hover:text-fg" onclick={() => sortBy('updated')}>
                      Updated
                      {#if sortKey === 'updated'}{#if sortDir === 'asc'}<ChevronUp size={12} />{:else}<ChevronDown size={12} />{/if}{/if}
                    </button>
                  </th>
                  <th class="h-8 px-3"><span class="sr-only">Actions</span></th>
                </tr>
              </thead>
              <tbody>
                {#each visibleQueries as query (query.id)}
                  <tr
                    class="group cursor-default border-b border-edge-subtle last:border-b-0 transition-colors hover:bg-hover"
                    ondblclick={() => openInEditor(query)}
                    oncontextmenu={(e) => openContextMenu(e, query)}
                  >
                    <td class="px-3 py-2">
                      <button class="flex w-full min-w-0 items-center gap-2.5 text-left" onclick={() => openInEditor(query)} title="Open in the editor">
                        <FileCode2 size={15} strokeWidth={1.75} class="shrink-0 text-fg-3" />
                        <span class="truncate font-medium text-fg">{query.name}</span>
                        {#if query.verified}
                          <BadgeCheck size={13} class="shrink-0 text-success" aria-label="Verified" />
                        {/if}
                      </button>
                    </td>
                    <td class="truncate px-3 py-2 text-fg-3">{query.description?.trim() || ''}</td>
                    <td class="px-3 py-2 text-fg-3 tabular-nums" title={formatDate(query.updated_at)}>{formatRelativeTime(query.updated_at)}</td>
                    <td class="px-2 py-1.5">
                      <div class="flex items-center justify-end gap-0.5 opacity-0 transition-opacity group-hover:opacity-100 focus-within:opacity-100">
                        <Button icon variant="ghost" size="sm" aria-label="Open in the editor" title="Open" onclick={() => openInEditor(query)}>
                          <Play size={14} />
                        </Button>
                        <Button icon variant="ghost" size="sm" aria-label="More actions" onclick={(e) => openContextMenuFromButton(e, query)}>
                          <MoreHorizontal size={15} />
                        </Button>
                      </div>
                    </td>
                  </tr>
                {/each}
              </tbody>
            </table>
          </div>
        {/if}
      </div>
    {/if}
  </PageBody>
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

<InputDialog
  open={renameOpen}
  title="Rename saved query"
  placeholder="Query name"
  bind:value={renameValue}
  confirmLabel="Rename"
  loading={renameLoading}
  onconfirm={confirmRename}
  oncancel={cancelRename}
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
  description={selectedQuery ? `Updated ${formatDate(selectedQuery.updated_at)} · created ${formatDate(selectedQuery.created_at)} · ${countLines(selectedQuery.query)} lines` : undefined}
  size="lg"
  onclose={closeDetails}
>
  {#if selectedQuery}
    <div class="space-y-4">
      {#if selectedQuery.description?.trim()}
        <p class="text-[13px] leading-relaxed text-fg-2">{selectedQuery.description}</p>
      {/if}

      <Panel variant="muted" padding="sm" title="Verified for AI">
        {#snippet actions()}
          <Button size="sm" variant="outline" onclick={() => selectedQuery && toggleVerified(selectedQuery)}>
            <BadgeCheck size={13} /> {selectedQuery?.verified ? 'Unmark' : 'Mark verified'}
          </Button>
        {/snippet}
        <p class="text-xs text-fg-2">
          {selectedQuery.verified ? 'Reviewed by a person. MCP clients prefer this query over writing new SQL.' : 'Not reviewed. Mark it once the SQL is known to be correct.'}
        </p>
      </Panel>

      {#if detectQueryParams(selectedQuery.query).length > 0}
        {@const defaults = storedParamDefaults(selectedQuery)}
        <Panel variant="muted" padding="sm" title="Parameters">
          {#snippet actions()}<Badge tone="brand">Pro</Badge>{/snippet}
          <div class="space-y-1.5">
            {#each detectQueryParams(selectedQuery.query) as p (p.name)}
              <div class="flex items-center gap-2 text-xs">
                <span class="font-mono text-fg-2">{p.name}<span class="text-fg-4">:{p.type}</span></span>
                {#if defaults[p.name]}
                  <span class="text-fg-4">default</span>
                  <span class="font-mono text-fg-2">{defaults[p.name]}</span>
                {/if}
              </div>
            {/each}
          </div>
          <p class="mt-2 text-[11px] text-fg-4">Open in the editor to set values and run, or call <span class="font-mono">POST /api/saved-queries/{selectedQuery.id}/run</span>.</p>
        </Panel>
      {/if}

      <pre class="overflow-x-auto whitespace-pre rounded-md border border-edge-subtle bg-surface-2 p-3 font-mono text-xs leading-relaxed text-fg">{selectedQuery.query}</pre>
    </div>
  {/if}
  {#snippet footer()}
    <div class="flex w-full flex-wrap items-center gap-2">
      <Button size="sm" variant="danger" onclick={() => selectedQuery && requestDelete(selectedQuery)}>
        <Trash2 size={13} /> Delete
      </Button>
      <div class="ml-auto flex items-center gap-2">
        <Button size="sm" variant="outline" onclick={() => selectedQuery && requestRename(selectedQuery)}>
          <Pencil size={13} /> Rename
        </Button>
        <Button size="sm" variant="outline" onclick={() => selectedQuery && duplicateQuery(selectedQuery)}>
          <CopyPlus size={13} /> Duplicate
        </Button>
        <Button size="sm" variant="outline" onclick={() => selectedQuery && copySQL(selectedQuery)}>
          <Copy size={13} /> Copy SQL
        </Button>
        <Button size="sm" onclick={() => selectedQuery && openInEditor(selectedQuery)}>
          <Play size={13} /> Open
        </Button>
      </div>
    </div>
  {/snippet}
</Sheet>
