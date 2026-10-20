<script lang="ts">
  import type { SavedQuery, SavedQueryFolder, SavedQueryVisibility } from '../lib/types/api'
  import {
    listSavedQueries, listSavedQueryFolders, createSavedQueryFolder, renameSavedQueryFolder, deleteSavedQueryFolder,
    updateSavedQuery, duplicateSavedQuery, deleteSavedQuery, scopeForSection, folderOptions, folderLabel, VISIBILITY_LABELS,
  } from '../lib/api/savedQueries'
  import { openQueryTab, openSavedQueryTab, renameSavedQueryTabs } from '../lib/stores/tabs.svelte'
  import { getSection } from '../lib/stores/nav.svelte'
  import { getSession } from '../lib/stores/session.svelte'
  import { PAGE_SECTIONS, docsURL } from '../lib/routes'
  import { formatDate, formatRelativeTime } from '../lib/utils/format'
  import { success as toastSuccess, error as toastError, errorFrom as toastErrorFrom } from '../lib/stores/toast.svelte'
  import Button from '../lib/components/common/Button.svelte'
  import Spinner from '../lib/components/common/Spinner.svelte'
  import Input from '../lib/components/common/Input.svelte'
  import Select from '../lib/components/common/Select.svelte'
  import FormField from '../lib/components/common/FormField.svelte'
  import Sheet from '../lib/components/common/Sheet.svelte'
  import ContextMenu, { type ContextMenuItem } from '../lib/components/common/ContextMenu.svelte'
  import ConfirmDialog from '../lib/components/common/ConfirmDialog.svelte'
  import InputDialog from '../lib/components/common/InputDialog.svelte'
  import PageHeader from '../lib/components/common/PageHeader.svelte'
  import Badge from '../lib/components/common/Badge.svelte'
  import EmptyState from '../lib/components/common/EmptyState.svelte'
  import DataTable, { type DataColumn } from '../lib/components/common/DataTable.svelte'
  import FolderTree from '../lib/components/common/FolderTree.svelte'
  import SavedQuerySheet from '../lib/components/saved/SavedQuerySheet.svelte'
  import SavedQueryHistorySheet from '../lib/components/saved/SavedQueryHistorySheet.svelte'
  import ReportEditor from '../lib/components/reports/ReportEditor.svelte'
  import { fetchReports, type ReportChannel, type ReportInput, type ReportWorkerMode } from '../lib/api/reports'
  import { isProActive } from '../lib/stores/license.svelte'
  import {
    Play, Trash2, Search, MoreHorizontal, Copy, CopyPlus, FileCode2, Pencil, BadgeCheck, FolderInput, FolderPlus, History, Mail,
  } from 'lucide-svelte'

  type Row = Record<string, unknown> & SavedQuery & { folder_path: string; owner_label: string }

  const session = $derived(getSession())
  const isAdmin = $derived(session?.role === 'admin')
  const canWrite = $derived(session?.role === 'admin' || session?.role === 'analyst')

  // The sidebar drives the section through ?section=.
  const scope = $derived(scopeForSection(getSection()))
  const sectionLabel = $derived((PAGE_SECTIONS['saved-queries'] ?? []).find((s) => s.id === scope)?.label ?? 'All')

  let queries = $state<SavedQuery[]>([])
  let folders = $state<SavedQueryFolder[]>([])
  let loading = $state(true)
  let search = $state('')
  let tagFilter = $state('')
  let owners = $state<'visible' | 'all'>('visible')
  let selectedFolder = $state<string | null>(null)

  async function loadAll() {
    try {
      const [q, f] = await Promise.all([
        listSavedQueries({ scope, allOwners: isAdmin && owners === 'all' }),
        listSavedQueryFolders(),
      ])
      queries = q
      folders = f
      if (selectedFolder && !f.some((x) => x.id === selectedFolder)) selectedFolder = null
    } catch (e: unknown) {
      toastErrorFrom(e)
    } finally {
      loading = false
    }
  }

  // Reload when the section or the owner filter changes.
  $effect(() => {
    void scope
    void owners
    void loadAll()
  })

  // ── Tree and rows ──────────────────────────────────────────
  function subtree(folderId: string | null): Set<string | null> {
    const out = new Set<string | null>([folderId])
    let added = true
    while (added) {
      added = false
      for (const f of folders) {
        if (f.parent_id && out.has(f.parent_id) && !out.has(f.id)) {
          out.add(f.id)
          added = true
        }
      }
    }
    return out
  }

  function countIn(folderId: string): number {
    const ids = subtree(folderId)
    return queries.filter((q) => ids.has(q.folder_id ?? null)).length
  }

  const allTags = $derived([...new Set(queries.flatMap((q) => q.tags))].sort((a, b) => a.localeCompare(b)))
  const tagOptions = $derived([{ value: '', label: 'Any tag' }, ...allTags.map((t) => ({ value: t, label: t }))])

  const rows = $derived.by<Row[]>(() => {
    const term = search.trim().toLowerCase()
    // A folder lists its whole subtree; the root lists everything.
    const inScope = selectedFolder ? subtree(selectedFolder) : null
    return queries
      .filter((q) => !inScope || inScope.has(q.folder_id ?? null))
      .filter((q) => !tagFilter || q.tags.includes(tagFilter))
      .map((q) => ({ ...q, folder_path: folderLabel(folders, q.folder_id), owner_label: q.owner || 'Legacy' }))
      .filter((r) => {
        if (!term) return true
        return (
          r.name.toLowerCase().includes(term) ||
          (r.description ?? '').toLowerCase().includes(term) ||
          r.query.toLowerCase().includes(term) ||
          r.tags.some((t) => t.toLowerCase().includes(term))
        )
      })
  })

  const columns: DataColumn<Row>[] = [
    { key: 'name', label: 'Name', width: '36%' },
    { key: 'folder_path', label: 'Folder', truncate: true, format: (v) => String(v || '') },
    { key: 'owner_label', label: 'Owner', width: '160px', truncate: true },
    { key: 'visibility', label: 'Visibility', width: '110px' },
    { key: 'updated_at', label: 'Updated', width: '120px', format: (v) => formatRelativeTime(v) },
  ]

  const visibilityTone: Record<SavedQueryVisibility, 'neutral' | 'info' | 'warning'> = {
    shared: 'neutral',
    private: 'info',
    admins: 'warning',
  }

  const filtered = $derived(search.trim() !== '' || tagFilter !== '')

  // ── Row actions ────────────────────────────────────────────
  let menu = $state<{ query: SavedQuery; x: number; y: number } | null>(null)
  let folderMenu = $state<{ folder: SavedQueryFolder; x: number; y: number } | null>(null)

  function openRowMenu(e: MouseEvent, q: SavedQuery) {
    e.preventDefault()
    e.stopPropagation()
    const el = e.currentTarget as HTMLElement | null
    const rect = el && e.type === 'click' ? el.getBoundingClientRect() : null
    menu = {
      query: q,
      x: Math.min(window.innerWidth - 240, rect ? rect.left - 180 : e.clientX),
      y: Math.min(window.innerHeight - 300, rect ? rect.bottom + 6 : e.clientY),
    }
  }

  function openFolderMenu(e: MouseEvent, f: SavedQueryFolder) {
    e.preventDefault()
    e.stopPropagation()
    const rect = (e.currentTarget as HTMLElement).getBoundingClientRect()
    folderMenu = { folder: f, x: Math.min(window.innerWidth - 240, rect.left), y: rect.bottom + 4 }
  }

  function canVerify(q: SavedQuery): boolean {
    return isAdmin || (session?.role === 'analyst' && q.owner !== '' && q.owner === session.user)
  }

  function rowMenuItems(): ContextMenuItem[] {
    const q = menu?.query
    if (!q) return []
    const locked = !q.can_edit
    const items: ContextMenuItem[] = [
      { id: 'open', label: 'Open', icon: Play, onSelect: () => openSavedQueryTab(q) },
      { id: 'details', label: 'Edit details', icon: Pencil, disabled: locked, onSelect: () => (editing = q) },
      { id: 'move', label: 'Move', icon: FolderInput, disabled: locked, onSelect: () => openMove(q) },
      { id: 'history', label: 'History', icon: History, onSelect: () => (historyFor = q) },
      { id: 'copy', label: 'Copy SQL', icon: Copy, onSelect: () => void copySQL(q) },
    ]
    if (canWrite) {
      items.push({ id: 'duplicate', label: 'Duplicate', icon: CopyPlus, onSelect: () => void doDuplicate(q) })
    }
    if (canWrite && isProActive()) {
      items.push({ id: 'subscribe', label: 'Subscribe', icon: Mail, onSelect: () => void openSubscribe(q) })
    }
    if (canVerify(q)) {
      items.push({ id: 'verify', label: q.verified ? 'Remove verified mark' : 'Mark as verified', icon: BadgeCheck, onSelect: () => void toggleVerified(q) })
    }
    items.push(
      { id: 'sep', separator: true },
      { id: 'delete', label: 'Delete', icon: Trash2, danger: true, disabled: locked, onSelect: () => (deleting = q) },
    )
    return items
  }

  function folderMenuItems(): ContextMenuItem[] {
    const f = folderMenu?.folder
    if (!f) return []
    return [
      { id: 'sub', label: 'New subfolder', icon: FolderPlus, onSelect: () => openNewFolder(f.id) },
      { id: 'rename', label: 'Rename', icon: Pencil, onSelect: () => (renamingFolder = f) },
      { id: 'sep', separator: true },
      { id: 'delete', label: 'Delete', icon: Trash2, danger: true, onSelect: () => (deletingFolder = f) },
    ]
  }

  async function copySQL(q: SavedQuery) {
    try {
      await navigator.clipboard.writeText(q.query)
      toastSuccess(`Copied SQL for "${q.name}"`)
    } catch {
      toastError('Clipboard unavailable')
    }
  }

  // Subscribe: a scheduled report of this query's result (Pro).
  let subscribeOpen = $state(false)
  let subscribePrefill = $state<Partial<ReportInput> | null>(null)
  let subscribeChannels = $state<ReportChannel[]>([])
  let subscribeModes = $state<Record<string, ReportWorkerMode>>({})
  let subscribeDomains = $state<string[]>([])

  async function openSubscribe(q: SavedQuery) {
    try {
      const res = await fetchReports()
      subscribeChannels = res.channels
      subscribeModes = res.worker_modes
      subscribeDomains = res.recipient_domains
      subscribePrefill = {
        name: q.name,
        source_type: 'saved_query',
        saved_query_id: q.id,
        connection_id: q.connection_id || session?.connectionId || '',
      }
      subscribeOpen = true
    } catch (e: unknown) {
      toastErrorFrom(e)
    }
  }

  async function doDuplicate(q: SavedQuery) {
    try {
      const copy = await duplicateSavedQuery(q.id)
      toastSuccess(`Duplicated as "${copy.name}", visible only to you`)
      await loadAll()
    } catch (e: unknown) {
      toastErrorFrom(e)
    }
  }

  // Verified marks a query a human reviewed as correct. The MCP server tells
  // AI clients to prefer verified queries over writing new SQL.
  async function toggleVerified(q: SavedQuery) {
    try {
      await updateSavedQuery(q.id, { verified: !q.verified })
      toastSuccess(q.verified ? 'Verified mark removed' : 'Marked as verified')
      await loadAll()
    } catch (e: unknown) {
      toastErrorFrom(e)
    }
  }

  // ── Drag and drop into folders ─────────────────────────────
  const DRAG_TYPE = 'text/ch-ui-saved-query'

  function onDragStart(e: DragEvent, q: SavedQuery) {
    if (!q.can_edit || !e.dataTransfer) return
    e.dataTransfer.setData(DRAG_TYPE, q.id)
    e.dataTransfer.effectAllowed = 'move'
  }

  async function doMove(id: string, folderId: string | null) {
    try {
      await updateSavedQuery(id, { folder_id: folderId })
      toastSuccess(folderId ? `Moved to ${folderLabel(folders, folderId)}` : 'Moved to the root')
      await loadAll()
    } catch (e: unknown) {
      toastErrorFrom(e)
    }
  }

  // ── Sheets and dialogs ─────────────────────────────────────
  let editing = $state<SavedQuery | null>(null)
  let historyFor = $state<SavedQuery | null>(null)

  function onDetailsSaved(updated: SavedQuery) {
    editing = null
    renameSavedQueryTabs(updated.id, updated.name)
    void loadAll()
  }

  let moving = $state<SavedQuery | null>(null)
  let moveTarget = $state('')
  let moveSaving = $state(false)

  function openMove(q: SavedQuery) {
    moving = q
    moveTarget = q.folder_id ?? ''
  }

  async function submitMove() {
    if (!moving) return
    moveSaving = true
    try {
      await doMove(moving.id, moveTarget || null)
      moving = null
    } finally {
      moveSaving = false
    }
  }

  let deleting = $state<SavedQuery | null>(null)
  let deleteSaving = $state(false)

  async function confirmDelete() {
    if (!deleting) return
    deleteSaving = true
    try {
      await deleteSavedQuery(deleting.id)
      toastSuccess('Saved query deleted')
      deleting = null
      await loadAll()
    } catch (e: unknown) {
      toastErrorFrom(e)
    } finally {
      deleteSaving = false
    }
  }

  let newFolderOpen = $state(false)
  let newFolderParent = $state<string | null>(null)
  let newFolderName = $state('')
  let folderSaving = $state(false)

  function openNewFolder(parentId: string | null) {
    newFolderParent = parentId
    newFolderName = ''
    newFolderOpen = true
  }

  async function submitNewFolder(name: string) {
    if (!name.trim()) return
    folderSaving = true
    try {
      const f = await createSavedQueryFolder(name.trim(), newFolderParent)
      newFolderOpen = false
      await loadAll()
      selectedFolder = f.id
    } catch (e: unknown) {
      toastErrorFrom(e)
    } finally {
      folderSaving = false
    }
  }

  let renamingFolder = $state<SavedQueryFolder | null>(null)
  let renameFolderValue = $state('')
  $effect(() => {
    renameFolderValue = renamingFolder?.name ?? ''
  })

  async function submitRenameFolder(name: string) {
    if (!renamingFolder || !name.trim()) return
    folderSaving = true
    try {
      await renameSavedQueryFolder(renamingFolder.id, name.trim())
      renamingFolder = null
      await loadAll()
    } catch (e: unknown) {
      toastErrorFrom(e)
    } finally {
      folderSaving = false
    }
  }

  let deletingFolder = $state<SavedQueryFolder | null>(null)
  const deletingFolderCounts = $derived.by(() => {
    if (!deletingFolder) return { queries: 0, subfolders: 0 }
    const ids = subtree(deletingFolder.id)
    return {
      queries: queries.filter((q) => ids.has(q.folder_id ?? null)).length,
      subfolders: ids.size - 1,
    }
  })

  function plural(n: number, one: string, many: string): string {
    return `${n} ${n === 1 ? one : many}`
  }

  async function confirmDeleteFolder() {
    if (!deletingFolder) return
    folderSaving = true
    try {
      const moved = await deleteSavedQueryFolder(deletingFolder.id)
      if (selectedFolder && subtree(deletingFolder.id).has(selectedFolder)) selectedFolder = null
      deletingFolder = null
      toastSuccess(moved > 0 ? `Folder deleted, ${plural(moved, 'query', 'queries')} moved to the root` : 'Folder deleted')
      await loadAll()
    } catch (e: unknown) {
      toastErrorFrom(e)
    } finally {
      folderSaving = false
    }
  }
</script>

<svelte:window onkeydown={(e) => { if (e.key === 'Escape') { menu = null; folderMenu = null } }} />

<div class="flex h-full min-h-0 flex-col">
  <PageHeader title="Saved Queries" subtitle={sectionLabel} docs={docsURL('saved-queries', scope)}>
    {#snippet meta()}
      {#if !loading}<Badge>{queries.length}</Badge>{/if}
    {/snippet}
    {#snippet actions()}
      {#if canWrite}
        <Button size="sm" variant="outline" onclick={() => openNewFolder(selectedFolder)}>New folder</Button>
      {/if}
      <Button size="sm" variant="ghost" onclick={() => void loadAll()}>Refresh</Button>
    {/snippet}
  </PageHeader>

  {#if loading}
    <div class="flex flex-1 items-center justify-center"><Spinner /></div>
  {:else if queries.length === 0 && folders.length === 0 && scope === 'all' && owners === 'visible'}
    <EmptyState
      icon={FileCode2}
      title="No saved queries yet"
      description="Save a query from the SQL editor and it will show up here. Folders keep them organized as the list grows."
      primary={{ label: 'Open the editor', onclick: () => openQueryTab() }}
    />
  {:else}
    <div class="flex min-h-0 flex-1">
      <FolderTree
        {folders}
        selected={selectedFolder}
        onselect={(id) => (selectedFolder = id)}
        rootLabel="All queries"
        rootIcon={FileCode2}
        total={queries.length}
        {countIn}
        storageKey="ch-ui-saved-query-folders-open"
        {canWrite}
        ariaLabel="Saved query folders"
        onfoldermenu={openFolderMenu}
        onnewfolder={() => openNewFolder(null)}
        dragType={DRAG_TYPE}
        ondropitem={(id, folderId) => {
          const q = queries.find((x) => x.id === id)
          if (q?.can_edit) void doMove(id, folderId)
        }}
      />

      <div class="flex min-h-0 min-w-0 flex-1 flex-col">
        <div class="flex h-10 shrink-0 items-center gap-2 border-b border-edge-subtle px-4">
          <div class="relative">
            <Search size={13} class="pointer-events-none absolute left-2.5 top-1/2 -translate-y-1/2 text-fg-4" />
            <Input size="sm" type="search" class="w-64 pl-8" placeholder="Search name, SQL, tags" bind:value={search} spellcheck={false} />
          </div>
          {#if allTags.length > 0}
            <Select size="sm" class="w-40" options={tagOptions} bind:value={tagFilter} />
          {/if}
          {#if isAdmin}
            <Select
              size="sm"
              class="w-44"
              options={[{ value: 'visible', label: 'Visible to me' }, { value: 'all', label: 'All owners' }]}
              value={owners}
              onchange={(v: string) => (owners = v === 'all' ? 'all' : 'visible')}
            />
          {/if}
          <span class="ml-auto text-xs text-fg-4">{plural(rows.length, 'query', 'queries')}</span>
        </div>
        <div class="min-h-0 flex-1">
          <DataTable
            fill
            {columns}
            {rows}
            rowKey={(r) => r.id}
            sort={{ key: 'updated_at', dir: 'desc' }}
            emptyTitle={filtered ? 'Nothing matches' : selectedFolder ? 'This folder is empty' : 'Nothing in this section'}
            emptyDescription={filtered ? 'Try another search or tag.' : selectedFolder ? (canWrite ? 'Move a query here from its menu or drag it onto the folder.' : undefined) : 'Queries you can see in this section show up here.'}
            onrowclick={(r) => openSavedQueryTab(r)}
            onrowcontextmenu={(r, e) => openRowMenu(e, r)}
          >
            {#snippet cell(row, col, value)}
              {#if col.key === 'name'}
                <div class="flex min-w-0 items-center gap-2" draggable={row.can_edit} ondragstart={(e) => onDragStart(e, row)} role="presentation">
                  <div class="min-w-0">
                    <div class="flex min-w-0 items-center gap-1.5">
                      <span class="truncate font-medium text-fg">{row.name}</span>
                      {#if row.verified}<Badge tone="success">Verified</Badge>{/if}
                    </div>
                    {#if row.description}
                      <div class="truncate text-xs text-fg-3">{row.description}</div>
                    {/if}
                  </div>
                </div>
              {:else if col.key === 'folder_path'}
                <span class="text-fg-3">{value || 'Root'}</span>
              {:else if col.key === 'owner_label'}
                <span class={row.owner ? '' : 'text-fg-4'}>{value}</span>
              {:else if col.key === 'visibility'}
                <Badge tone={visibilityTone[row.visibility]}>{VISIBILITY_LABELS[row.visibility]}</Badge>
              {:else if col.key === 'updated_at'}
                <span title={formatDate(row.updated_at)}>{value}</span>
              {:else}
                {value}
              {/if}
            {/snippet}
            {#snippet actions(row)}
              <Button icon variant="ghost" size="xs" aria-label="More actions" onclick={(e) => openRowMenu(e, row)}>
                <MoreHorizontal size={14} />
              </Button>
            {/snippet}
          </DataTable>
        </div>
      </div>
    </div>
  {/if}
</div>

<ContextMenu open={!!menu} x={menu?.x ?? 0} y={menu?.y ?? 0} items={rowMenuItems()} onclose={() => (menu = null)} />
<ContextMenu open={!!folderMenu} x={folderMenu?.x ?? 0} y={folderMenu?.y ?? 0} items={folderMenuItems()} onclose={() => (folderMenu = null)} />

<SavedQuerySheet query={editing} {folders} {isAdmin} onclose={() => (editing = null)} onsaved={onDetailsSaved} />
<ReportEditor
  open={subscribeOpen}
  report={null}
  prefill={subscribePrefill}
  channels={subscribeChannels}
  workerModes={subscribeModes}
  recipientDomains={subscribeDomains}
  onclose={() => (subscribeOpen = false)}
  onsaved={() => {
    subscribeOpen = false
    toastSuccess('Report scheduled. Manage it in Schedules, Report delivery.')
  }}
/>

<SavedQueryHistorySheet query={historyFor} onclose={() => (historyFor = null)} onrestored={() => void loadAll()} />

<Sheet open={!!moving} title="Move" description={moving ? `Where "${moving.name}" should live.` : undefined} size="sm" onclose={() => (moving = null)}>
  <FormField label="Folder" for="sq-move-folder" controlWidth="full">
    <Select id="sq-move-folder" options={folderOptions(folders)} bind:value={moveTarget} />
  </FormField>
  {#snippet footer()}
    <Button variant="ghost" size="sm" onclick={() => (moving = null)}>Cancel</Button>
    <Button size="sm" loading={moveSaving} onclick={() => void submitMove()}>Move</Button>
  {/snippet}
</Sheet>

<ConfirmDialog
  open={!!deleting}
  title="Delete saved query?"
  description={deleting ? `"${deleting.name}" and its history will be permanently removed. Schedules that run it stop working.` : ''}
  confirmLabel="Delete"
  destructive={true}
  loading={deleteSaving}
  onconfirm={() => void confirmDelete()}
  oncancel={() => (deleting = null)}
/>

<Sheet
  open={newFolderOpen}
  title={newFolderParent ? `New folder in ${folderLabel(folders, newFolderParent)}` : 'New folder'}
  description="The name shown in the folder tree. Folders nest and are shared by everyone."
  size="sm"
  onclose={() => (newFolderOpen = false)}
>
  <form id="sq-new-folder-form" onsubmit={(e) => { e.preventDefault(); void submitNewFolder(newFolderName) }}>
    <FormField label="Name" for="sq-new-folder-name" required controlWidth="full">
      <Input id="sq-new-folder-name" placeholder="Folder name" bind:value={newFolderName} disabled={folderSaving} />
    </FormField>
  </form>
  {#snippet footer()}
    <Button variant="ghost" size="sm" onclick={() => (newFolderOpen = false)} disabled={folderSaving}>Cancel</Button>
    <Button size="sm" type="submit" form="sq-new-folder-form" loading={folderSaving} disabled={!newFolderName.trim()}>Create</Button>
  {/snippet}
</Sheet>

<InputDialog
  open={!!renamingFolder}
  title="Rename folder"
  placeholder="Folder name"
  bind:value={renameFolderValue}
  confirmLabel="Rename"
  loading={folderSaving}
  onconfirm={submitRenameFolder}
  oncancel={() => (renamingFolder = null)}
/>

<ConfirmDialog
  open={!!deletingFolder}
  title="Delete folder?"
  description={deletingFolder
    ? `"${deletingFolder.name}"${deletingFolderCounts.subfolders > 0 ? ` and ${plural(deletingFolderCounts.subfolders, 'subfolder', 'subfolders')}` : ''} will be removed. ${plural(deletingFolderCounts.queries, 'saved query', 'saved queries')} you can see in it move to the root; no query is deleted.`
    : ''}
  confirmLabel="Delete folder"
  destructive={true}
  loading={folderSaving}
  onconfirm={() => void confirmDeleteFolder()}
  oncancel={() => (deletingFolder = null)}
/>
