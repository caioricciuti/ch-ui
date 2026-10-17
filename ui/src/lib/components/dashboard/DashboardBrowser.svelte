<script lang="ts">
  import { onMount } from 'svelte'
  import type { Dashboard, DashboardFolder } from '../../types/api'
  import {
    listDashboards, listDashboardFolders, createDashboardFolder, updateDashboardFolder, deleteDashboardFolder,
    createDashboard, moveDashboard, setDashboardTags, setDashboardStar, deleteDashboard, renameDashboard, duplicateDashboard, folderPath, type DashboardDetail,
  } from '../../api/dashboards'
  import { pushDashboardDetail } from '../../stores/router.svelte'
  import { getSession } from '../../stores/session.svelte'
  import { success as toastSuccess, error as toastError } from '../../stores/toast.svelte'
  import { formatDate, formatRelativeTime } from '../../utils/format'
  import PageHeader from '../common/PageHeader.svelte'
  import { docsURL } from '../../routes'
  import Button from '../common/Button.svelte'
  import Badge from '../common/Badge.svelte'
  import Input from '../common/Input.svelte'
  import Textarea from '../common/Textarea.svelte'
  import Select from '../common/Select.svelte'
  import FormField from '../common/FormField.svelte'
  import Tabs from '../common/Tabs.svelte'
  import Sheet from '../common/Sheet.svelte'
  import Modal from '../common/Modal.svelte'
  import InputDialog from '../common/InputDialog.svelte'
  import ConfirmDialog from '../common/ConfirmDialog.svelte'
  import EmptyState from '../common/EmptyState.svelte'
  import Spinner from '../common/Spinner.svelte'
  import DataTable, { type DataColumn } from '../common/DataTable.svelte'
  import ContextMenu, { type ContextMenuItem } from '../common/ContextMenu.svelte'
  import FolderTree from '../common/FolderTree.svelte'
  import TemplatePicker from './TemplatePicker.svelte'
  import {
    LayoutDashboard, Plus, FolderPlus, Star, FolderInput,
    MoreHorizontal, Pencil, Trash2, Tag, ExternalLink, Search, Copy, LayoutTemplate,
  } from 'lucide-svelte'

  // Grafana-style browser: folder tree on the left, dashboards of the
  // selected folder on the right, stars per user, tags for search.
  type Row = Record<string, unknown> & Dashboard & { folder_path: string; owner: string }

  let dashboards = $state<Dashboard[]>([])
  let folders = $state<DashboardFolder[]>([])
  let loading = $state(true)
  let search = $state('')
  let scope = $state<'all' | 'starred'>('all')
  let selectedFolder = $state<string | null>(null)

  const session = $derived(getSession())
  const canWrite = $derived(session?.role !== 'viewer')

  async function loadAll() {
    try {
      const [d, f] = await Promise.all([listDashboards(), listDashboardFolders()])
      dashboards = d
      folders = f
      if (selectedFolder && !f.some((x) => x.id === selectedFolder)) selectedFolder = null
    } catch (e: unknown) {
      toastError(e instanceof Error ? e.message : String(e))
    } finally {
      loading = false
    }
  }

  onMount(() => {
    const wanted = new URLSearchParams(window.location.search).get('folder')
    if (wanted) selectedFolder = wanted
    void loadAll()
  })

  // ── Tree helpers ───────────────────────────────────────────
  const byParent = $derived.by(() => {
    const map = new Map<string | null, DashboardFolder[]>()
    for (const f of folders) {
      const key = f.parent_id ?? null
      const list = map.get(key) ?? []
      list.push(f)
      map.set(key, list)
    }
    for (const list of map.values()) list.sort((a, b) => a.name.localeCompare(b.name))
    return map
  })

  function childrenOf(parentId: string | null): DashboardFolder[] {
    return byParent.get(parentId) ?? []
  }

  function descendantIds(folderId: string | null): Set<string | null> {
    const out = new Set<string | null>([folderId])
    const stack = [...childrenOf(folderId)]
    while (stack.length) {
      const f = stack.pop()!
      out.add(f.id)
      stack.push(...childrenOf(f.id))
    }
    return out
  }

  function countIn(folderId: string | null): number {
    const ids = descendantIds(folderId)
    return dashboards.filter((d) => ids.has(d.folder_id ?? null)).length
  }

  const folderOptions = $derived.by(() => {
    const opts: { value: string; label: string }[] = [{ value: '', label: 'All dashboards (root)' }]
    const walk = (parentId: string | null, prefix: string) => {
      for (const f of childrenOf(parentId)) {
        const label = prefix ? `${prefix} / ${f.name}` : f.name
        opts.push({ value: f.id, label })
        walk(f.id, label)
      }
    }
    walk(null, '')
    return opts
  })

  function pathLabel(folderId: string | null | undefined): string {
    return folderPath(folders, folderId).join(' / ')
  }


  // ── Rows ───────────────────────────────────────────────────
  const rows = $derived.by<Row[]>(() => {
    const term = search.trim().toLowerCase()
    // A folder lists its whole subtree; the Folder column says where each
    // dashboard actually lives. The root therefore lists everything.
    const inScope = descendantIds(selectedFolder)
    return dashboards
      .filter((d) => inScope.has(d.folder_id ?? null))
      .filter((d) => scope === 'all' || d.starred)
      .map((d) => ({ ...d, folder_path: pathLabel(d.folder_id), owner: d.created_by || '' }))
      .filter((r) => {
        if (!term) return true
        return (
          r.name.toLowerCase().includes(term) ||
          (r.description ?? '').toLowerCase().includes(term) ||
          r.tags.some((t) => t.toLowerCase().includes(term)) ||
          r.folder_path.toLowerCase().includes(term)
        )
      })
  })

  const starredCount = $derived(dashboards.filter((d) => d.starred).length)

  const columns: DataColumn<Row>[] = [
    { key: 'name', label: 'Name', width: '38%' },
    { key: 'folder_path', label: 'Folder', truncate: true, format: (v) => String(v || '') },
    { key: 'tags', label: 'Tags', sortable: false, format: (v) => (Array.isArray(v) ? v.join(', ') : '') },
    { key: 'owner', label: 'Owner', width: '120px' },
    { key: 'updated_at', label: 'Updated', width: '120px', format: (v) => formatRelativeTime(v) },
  ]

  // ── Stars ──────────────────────────────────────────────────
  async function toggleStar(d: Dashboard) {
    const next = !d.starred
    dashboards = dashboards.map((x) => (x.id === d.id ? { ...x, starred: next } : x))
    try {
      await setDashboardStar(d.id, next)
    } catch (e: unknown) {
      dashboards = dashboards.map((x) => (x.id === d.id ? { ...x, starred: !next } : x))
      toastError(e instanceof Error ? e.message : String(e))
    }
  }

  // ── Context menu ───────────────────────────────────────────
  let menu = $state<{ dashboard: Dashboard; x: number; y: number } | null>(null)
  let folderMenu = $state<{ folder: DashboardFolder; x: number; y: number } | null>(null)

  function openRowMenu(e: MouseEvent, d: Dashboard) {
    e.preventDefault()
    e.stopPropagation()
    const el = e.currentTarget as HTMLElement | null
    const rect = el && e.type === 'click' ? el.getBoundingClientRect() : null
    menu = {
      dashboard: d,
      x: Math.min(window.innerWidth - 240, rect ? rect.left - 180 : e.clientX),
      y: Math.min(window.innerHeight - 260, rect ? rect.bottom + 6 : e.clientY),
    }
  }

  function openFolderMenu(e: MouseEvent, f: DashboardFolder) {
    e.preventDefault()
    e.stopPropagation()
    const rect = (e.currentTarget as HTMLElement).getBoundingClientRect()
    folderMenu = { folder: f, x: Math.min(window.innerWidth - 240, rect.left), y: rect.bottom + 4 }
  }

  function rowMenuItems(): ContextMenuItem[] {
    const d = menu?.dashboard
    if (!d) return []
    const items: ContextMenuItem[] = [
      { id: 'open', label: 'Open', icon: ExternalLink, onSelect: () => pushDashboardDetail(d.id) },
      { id: 'star', label: d.starred ? 'Unstar' : 'Star', icon: Star, onSelect: () => void toggleStar(d) },
    ]
    if (canWrite) {
      items.push(
        { id: 'sep1', separator: true },
        { id: 'move', label: 'Move to folder…', icon: FolderInput, onSelect: () => openMove(d) },
        { id: 'tags', label: 'Edit tags…', icon: Tag, onSelect: () => openTags(d) },
        { id: 'rename', label: 'Rename…', icon: Pencil, onSelect: () => openRename(d) },
        { id: 'duplicate', label: 'Duplicate', icon: Copy, onSelect: () => void doDuplicate(d) },
        { id: 'sep2', separator: true },
        { id: 'delete', label: 'Delete', icon: Trash2, danger: true, onSelect: () => (deleting = d) },
      )
    }
    return items
  }

  function folderMenuItems(): ContextMenuItem[] {
    const f = folderMenu?.folder
    if (!f) return []
    return [
      { id: 'sub', label: 'New subfolder…', icon: FolderPlus, onSelect: () => openNewFolder(f.id) },
      { id: 'rename', label: 'Rename…', icon: Pencil, onSelect: () => (renamingFolder = f) },
      { id: 'sep', separator: true },
      { id: 'delete', label: 'Delete', icon: Trash2, danger: true, onSelect: () => (deletingFolder = f) },
    ]
  }

  // ── Drag and drop ──────────────────────────────────────────
  const DRAG_TYPE = 'text/ch-ui-dashboard'

  function onDragStart(e: DragEvent, d: Dashboard) {
    if (!canWrite || !e.dataTransfer) return
    e.dataTransfer.setData(DRAG_TYPE, d.id)
    e.dataTransfer.effectAllowed = 'move'
  }

  async function doMove(id: string, folderId: string | null) {
    try {
      await moveDashboard(id, folderId)
      toastSuccess(folderId ? `Moved to ${pathLabel(folderId)}` : 'Moved to the root')
      await loadAll()
    } catch (e: unknown) {
      toastError(e instanceof Error ? e.message : String(e))
    }
  }

  // ── Dialogs ────────────────────────────────────────────────
  let createOpen = $state(false)
  let creating = $state(false)
  let createForm = $state({ name: '', description: '', folder: '', tags: '' })

  function openCreate() {
    createForm = { name: '', description: '', folder: selectedFolder ?? '', tags: '' }
    createOpen = true
  }

  function parseTags(raw: string): string[] {
    return raw.split(',').map((t) => t.trim()).filter(Boolean)
  }

  async function submitCreate() {
    if (!createForm.name.trim()) {
      toastError('Name is required')
      return
    }
    creating = true
    try {
      const d = await createDashboard({
        name: createForm.name.trim(),
        description: createForm.description.trim(),
        folder_id: createForm.folder || null,
        tags: parseTags(createForm.tags),
      })
      createOpen = false
      pushDashboardDetail(d.id)
    } catch (e: unknown) {
      toastError(e instanceof Error ? e.message : String(e))
    } finally {
      creating = false
    }
  }

  let templateOpen = $state(false)

  function onTemplateCreated(detail: DashboardDetail) {
    templateOpen = false
    pushDashboardDetail(detail.dashboard.id)
  }

  async function doDuplicate(d: Dashboard) {
    try {
      const detail = await duplicateDashboard(d.id)
      toastSuccess(`Duplicated as "${detail.dashboard.name}"`)
      await loadAll()
    } catch (e: unknown) {
      toastError(e instanceof Error ? e.message : String(e))
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
      const f = await createDashboardFolder(name.trim(), newFolderParent)
      newFolderOpen = false
      await loadAll()
      selectedFolder = f.id
    } catch (e: unknown) {
      toastError(e instanceof Error ? e.message : String(e))
    } finally {
      folderSaving = false
    }
  }

  let renamingFolder = $state<DashboardFolder | null>(null)
  let renameFolderValue = $state('')
  $effect(() => {
    renameFolderValue = renamingFolder?.name ?? ''
  })

  async function submitRenameFolder(name: string) {
    if (!renamingFolder || !name.trim()) return
    folderSaving = true
    try {
      await updateDashboardFolder(renamingFolder.id, { name: name.trim() })
      renamingFolder = null
      await loadAll()
    } catch (e: unknown) {
      toastError(e instanceof Error ? e.message : String(e))
    } finally {
      folderSaving = false
    }
  }

  let deletingFolder = $state<DashboardFolder | null>(null)

  async function confirmDeleteFolder() {
    if (!deletingFolder) return
    folderSaving = true
    try {
      const parent = deletingFolder.parent_id
      await deleteDashboardFolder(deletingFolder.id)
      if (selectedFolder === deletingFolder.id) selectedFolder = parent
      deletingFolder = null
      toastSuccess('Folder deleted, its contents moved up one level')
      await loadAll()
    } catch (e: unknown) {
      toastError(e instanceof Error ? e.message : String(e))
    } finally {
      folderSaving = false
    }
  }

  let moving = $state<Dashboard | null>(null)
  let moveTarget = $state('')
  let moveSaving = $state(false)

  function openMove(d: Dashboard) {
    moving = d
    moveTarget = d.folder_id ?? ''
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

  let tagging = $state<Dashboard | null>(null)
  let tagsValue = $state('')
  let tagsSaving = $state(false)

  function openTags(d: Dashboard) {
    tagging = d
    tagsValue = d.tags.join(', ')
  }

  async function submitTags() {
    if (!tagging) return
    tagsSaving = true
    try {
      await setDashboardTags(tagging.id, parseTags(tagsValue))
      tagging = null
      await loadAll()
    } catch (e: unknown) {
      toastError(e instanceof Error ? e.message : String(e))
    } finally {
      tagsSaving = false
    }
  }

  let renaming = $state<Dashboard | null>(null)
  let renameValue = $state('')
  let renameSaving = $state(false)

  function openRename(d: Dashboard) {
    renaming = d
    renameValue = d.name
  }

  async function submitRename(name: string) {
    if (!renaming || !name.trim()) return
    renameSaving = true
    try {
      await renameDashboard(renaming.id, name.trim())
      renaming = null
      toastSuccess('Dashboard renamed')
      await loadAll()
    } catch (e: unknown) {
      toastError(e instanceof Error ? e.message : String(e))
    } finally {
      renameSaving = false
    }
  }

  let deleting = $state<Dashboard | null>(null)
  let deleteSaving = $state(false)

  async function confirmDeleteDashboard() {
    if (!deleting) return
    deleteSaving = true
    try {
      await deleteDashboard(deleting.id)
      deleting = null
      toastSuccess('Dashboard deleted')
      await loadAll()
    } catch (e: unknown) {
      toastError(e instanceof Error ? e.message : String(e))
    } finally {
      deleteSaving = false
    }
  }
</script>

<svelte:window onkeydown={(e) => { if (e.key === 'Escape') { menu = null; folderMenu = null } }} />


<div class="flex h-full min-h-0 flex-col">
  <PageHeader title="Dashboards" subtitle="Panels over saved queries, organized in folders." docs={docsURL('dashboards')}>
    {#snippet meta()}
      {#if !loading}<Badge>{dashboards.length}</Badge>{/if}
    {/snippet}
    {#snippet actions()}
      {#if canWrite}
        <Button size="sm" variant="outline" onclick={() => openNewFolder(selectedFolder)}>
          <FolderPlus size={14} /> New folder
        </Button>
        <Button size="sm" variant="outline" onclick={() => (templateOpen = true)}>
          <LayoutTemplate size={14} /> New from template
        </Button>
        <Button size="sm" onclick={openCreate}>
          <Plus size={14} /> New dashboard
        </Button>
      {/if}
    {/snippet}
  </PageHeader>

  <div class="flex h-10 shrink-0 items-center gap-3 border-b border-edge-subtle px-5">
    <div class="relative">
      <Search size={13} class="pointer-events-none absolute left-2.5 top-1/2 -translate-y-1/2 text-fg-4" />
      <Input size="sm" type="search" class="w-72 pl-8" placeholder="Search dashboards, tags, folders" bind:value={search} spellcheck={false} />
    </div>
    <Tabs
      variant="segmented"
      size="sm"
      items={[{ id: 'all', label: 'All', count: dashboards.length }, { id: 'starred', label: 'Starred', count: starredCount }]}
      value={scope}
      onchange={(id) => (scope = id as 'all' | 'starred')}
    />
    <span class="ml-auto text-xs text-fg-4">{rows.length} {rows.length === 1 ? 'dashboard' : 'dashboards'}</span>
  </div>

  {#if loading}
    <div class="flex flex-1 items-center justify-center"><Spinner /></div>
  {:else if dashboards.length === 0 && folders.length === 0}
    <EmptyState
      icon={LayoutDashboard}
      title="No dashboards yet"
      description="Create a dashboard to visualize your ClickHouse data. Folders keep them organized as the list grows."
      primary={canWrite ? { label: 'New dashboard', onclick: openCreate } : undefined}
      secondary={canWrite ? { label: 'New from template', onclick: () => (templateOpen = true) } : undefined}
    />
  {:else}
    <div class="flex min-h-0 flex-1">
      <FolderTree
        {folders}
        selected={selectedFolder}
        onselect={(id) => (selectedFolder = id)}
        rootLabel="All dashboards"
        rootIcon={LayoutDashboard}
        total={dashboards.length}
        {countIn}
        storageKey="ch-ui-dash-folders-open"
        {canWrite}
        ariaLabel="Dashboard folders"
        onfoldermenu={openFolderMenu}
        onnewfolder={() => openNewFolder(null)}
        dragType={DRAG_TYPE}
        ondropitem={(id, folderId) => void doMove(id, folderId)}
      />

      <div class="flex min-h-0 flex-1 flex-col">
        <div class="min-h-0 flex-1 p-4 pl-5">
          <div class="h-full overflow-hidden rounded-lg border border-edge-subtle bg-surface">
            <DataTable
              fill
              {columns}
              {rows}
              rowKey={(r) => r.id}
              sort={{ key: 'updated_at', dir: 'desc' }}
              emptyTitle={search.trim() ? 'Nothing matches' : scope === 'starred' ? 'No starred dashboards here' : 'Nothing in this folder'}
              emptyDescription={search.trim() ? 'Try another search.' : scope === 'starred' ? 'Star a dashboard to keep it at hand.' : canWrite ? 'Create a dashboard here or drag one onto this folder.' : undefined}
              onrowclick={(r) => pushDashboardDetail(r.id)}
            >
              {#snippet cell(row, col, value)}
                {#if col.key === 'name'}
                  <div
                    class="flex min-w-0 items-center gap-2"
                    draggable={canWrite}
                    ondragstart={(e) => onDragStart(e, row)}
                    oncontextmenu={(e) => openRowMenu(e, row)}
                    role="presentation"
                  >
                    <button
                      class="shrink-0 rounded p-0.5 transition-colors {row.starred ? 'text-warning' : 'text-fg-4 hover:text-fg'}"
                      onclick={(e) => { e.stopPropagation(); void toggleStar(row) }}
                      aria-label={row.starred ? 'Unstar' : 'Star'}
                      aria-pressed={row.starred}
                    >
                      <Star size={14} fill={row.starred ? 'currentColor' : 'none'} />
                    </button>
                    <div class="min-w-0">
                      <div class="truncate font-medium text-fg">{row.name}</div>
                      {#if row.description}
                        <div class="truncate text-xs text-fg-3">{row.description}</div>
                      {/if}
                    </div>
                  </div>
                {:else if col.key === 'tags'}
                  {#if row.tags.length > 0}
                    <span class="inline-flex flex-wrap items-center gap-1">
                      {#each row.tags.slice(0, 4) as t (t)}<Badge>{t}</Badge>{/each}
                      {#if row.tags.length > 4}<span class="text-xs text-fg-4">+{row.tags.length - 4}</span>{/if}
                    </span>
                  {:else}
                    <span class="text-fg-4">—</span>
                  {/if}
                {:else if col.key === 'folder_path'}
                  <span class="text-fg-3">{value || 'Root'}</span>
                {:else if col.key === 'updated_at'}
                  <span title={formatDate(row.updated_at)}>{value}</span>
                {:else}
                  {value}
                {/if}
              {/snippet}
              {#snippet actions(row)}
                {#if canWrite}
                  <Button icon variant="ghost" size="xs" aria-label="Move to folder" title="Move to folder" onclick={(e) => { e.stopPropagation(); openMove(row) }}>
                    <FolderInput size={13} />
                  </Button>
                {/if}
                <Button icon variant="ghost" size="xs" aria-label="More actions" onclick={(e) => openRowMenu(e, row)}>
                  <MoreHorizontal size={14} />
                </Button>
              {/snippet}
            </DataTable>
          </div>
        </div>
      </div>
    </div>
  {/if}
</div>

<ContextMenu open={!!menu} x={menu?.x ?? 0} y={menu?.y ?? 0} items={rowMenuItems()} onclose={() => (menu = null)} />
<ContextMenu open={!!folderMenu} x={folderMenu?.x ?? 0} y={folderMenu?.y ?? 0} items={folderMenuItems()} onclose={() => (folderMenu = null)} />

<Sheet open={createOpen} title="New dashboard" description="Pick a folder now or move it later." size="md" onclose={() => (createOpen = false)}>
  <div class="space-y-4">
    <FormField label="Name" for="dash-name" required controlWidth="full">
      <Input id="dash-name" placeholder="e.g. Ingestion health" bind:value={createForm.name} />
    </FormField>
    <FormField label="Description" for="dash-desc" controlWidth="full">
      <Textarea id="dash-desc" rows={3} placeholder="What this dashboard answers" bind:value={createForm.description} />
    </FormField>
    <FormField label="Folder" for="dash-folder" controlWidth="full">
      <Select id="dash-folder" options={folderOptions} bind:value={createForm.folder} />
    </FormField>
    <FormField label="Tags" for="dash-tags" controlWidth="full" hint="Comma-separated, e.g. ops, kafka. Searchable from the list and the palette.">
      <Input id="dash-tags" placeholder="ops, kafka" bind:value={createForm.tags} />
    </FormField>
  </div>
  {#snippet footer()}
    <Button variant="ghost" size="sm" onclick={() => (createOpen = false)}>Cancel</Button>
    <Button size="sm" loading={creating} onclick={submitCreate}>Create dashboard</Button>
  {/snippet}
</Sheet>

<TemplatePicker
  open={templateOpen}
  folderId={selectedFolder}
  {folderOptions}
  onclose={() => (templateOpen = false)}
  oncreated={onTemplateCreated}
/>

<InputDialog
  open={newFolderOpen}
  title={newFolderParent ? `New folder in ${pathLabel(newFolderParent)}` : 'New folder'}
  description="Folders nest. A dashboard can live in one folder at a time."
  placeholder="Folder name"
  bind:value={newFolderName}
  confirmLabel="Create"
  loading={folderSaving}
  onconfirm={submitNewFolder}
  oncancel={() => (newFolderOpen = false)}
/>

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
  description={deletingFolder ? `"${deletingFolder.name}" will be removed. Its dashboards and subfolders move up one level; nothing is deleted.` : ''}
  confirmLabel="Delete folder"
  destructive={true}
  loading={folderSaving}
  onconfirm={confirmDeleteFolder}
  oncancel={() => (deletingFolder = null)}
/>

<Modal open={!!moving} title="Move to folder" description={moving ? `Where "${moving.name}" should live.` : ''} size="sm" onclose={() => (moving = null)}>
  <FormField label="Folder" for="move-folder" controlWidth="full">
    <Select id="move-folder" options={folderOptions} bind:value={moveTarget} />
  </FormField>
  {#snippet footer()}
    <Button variant="ghost" size="sm" onclick={() => (moving = null)}>Cancel</Button>
    <Button size="sm" loading={moveSaving} onclick={submitMove}>Move</Button>
  {/snippet}
</Modal>

<Modal open={!!tagging} title="Edit tags" description={tagging ? tagging.name : ''} size="sm" onclose={() => (tagging = null)}>
  <FormField label="Tags" for="edit-tags" controlWidth="full" hint="Comma-separated. Up to 20 tags of 40 characters.">
    <Input id="edit-tags" placeholder="ops, kafka" bind:value={tagsValue} onkeydown={(e) => { if (e.key === 'Enter') void submitTags() }} />
  </FormField>
  {#snippet footer()}
    <Button variant="ghost" size="sm" onclick={() => (tagging = null)}>Cancel</Button>
    <Button size="sm" loading={tagsSaving} onclick={submitTags}>Save tags</Button>
  {/snippet}
</Modal>

<InputDialog
  open={!!renaming}
  title="Rename dashboard"
  placeholder="Dashboard name"
  bind:value={renameValue}
  confirmLabel="Rename"
  loading={renameSaving}
  onconfirm={submitRename}
  oncancel={() => (renaming = null)}
/>

<ConfirmDialog
  open={!!deleting}
  title="Delete dashboard?"
  description={deleting ? `"${deleting.name}" and all its panels will be permanently removed.` : ''}
  confirmLabel="Delete"
  destructive={true}
  loading={deleteSaving}
  onconfirm={confirmDeleteDashboard}
  oncancel={() => (deleting = null)}
/>
