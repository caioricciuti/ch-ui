<script lang="ts">
  import { untrack } from 'svelte'
  import { Folder, FolderOpen, FolderPlus, ChevronRight, ChevronDown, MoreHorizontal } from 'lucide-svelte'
  import type { DashboardFolder } from '../../types/api'
  import { readScoped, writeScoped, type ScopedKey } from '../../stores/workspace-scope'
  import Button from './Button.svelte'

  /** Any lucide-svelte icon; they all share this component type. */
  type IconComponent = typeof Folder

  /**
   * The nested folder list beside a browser (dashboards, saved queries):
   * a root row, folders that expand and collapse (remembered per
   * user@connection), item counts, a per-folder menu, and drop targets
   * for dragging items into a folder.
   */
  interface Props {
    folders: DashboardFolder[]
    /** Selected folder id; null is the root row. */
    selected: string | null
    onselect: (folderId: string | null) => void
    rootLabel: string
    rootIcon: IconComponent
    /** Count shown on the root row. */
    total: number
    /** Count shown on a folder row (usually its whole subtree). */
    countIn: (folderId: string) => number
    /** Scoped localStorage key that remembers which folders are open. */
    storageKey: ScopedKey
    canWrite: boolean
    ariaLabel: string
    onfoldermenu?: (e: MouseEvent, folder: DashboardFolder) => void
    /** Shown as "Create a folder" while there are no folders. */
    onnewfolder?: () => void
    /** dataTransfer type the tree accepts; with ondropitem, rows become drop targets. */
    dragType?: string
    ondropitem?: (itemId: string, folderId: string | null) => void
  }

  let {
    folders, selected, onselect, rootLabel, rootIcon: RootIcon, total, countIn, storageKey, canWrite, ariaLabel,
    onfoldermenu, onnewfolder, dragType, ondropitem,
  }: Props = $props()

  let expanded = $state<Set<string>>(untrack(() => loadExpanded(storageKey)))
  let dragOver = $state<string | null | undefined>(undefined)

  function loadExpanded(key: ScopedKey): Set<string> {
    try {
      const raw = readScoped(key)
      const parsed: unknown = raw ? JSON.parse(raw) : []
      return new Set(Array.isArray(parsed) ? parsed.filter((v): v is string => typeof v === 'string') : [])
    } catch {
      return new Set()
    }
  }

  function setExpanded(next: Set<string>) {
    expanded = next
    writeScoped(storageKey, JSON.stringify([...next]))
  }

  function toggle(id: string) {
    const next = new Set(expanded)
    if (next.has(id)) next.delete(id)
    else next.add(id)
    setExpanded(next)
  }

  const byId = $derived(new Map(folders.map((f) => [f.id, f])))

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

  // A selected folder is always reachable: open its ancestors.
  $effect(() => {
    if (!selected) return
    const missing: string[] = []
    let current = byId.get(selected)
    for (let i = 0; current?.parent_id && i < 64; i++) {
      if (!expanded.has(current.parent_id)) missing.push(current.parent_id)
      current = byId.get(current.parent_id)
    }
    if (missing.length > 0) setExpanded(new Set([...expanded, ...missing]))
  })

  const droppable = $derived(canWrite && !!dragType && !!ondropitem)

  function onDragOver(e: DragEvent, folderId: string | null) {
    if (!droppable || !dragType || !e.dataTransfer?.types.includes(dragType)) return
    e.preventDefault()
    e.dataTransfer.dropEffect = 'move'
    dragOver = folderId
  }

  function onDragLeave(folderId: string | null) {
    if (dragOver === folderId) dragOver = undefined
  }

  function onDrop(e: DragEvent, folderId: string | null) {
    dragOver = undefined
    if (!droppable || !dragType) return
    const id = e.dataTransfer?.getData(dragType)
    if (!id) return
    e.preventDefault()
    ondropitem?.(id, folderId)
  }

  function rowTone(active: boolean, over: boolean): string {
    if (active) return 'bg-active text-fg'
    if (over) return 'bg-accent-soft text-fg'
    return 'text-fg-2 hover:bg-hover hover:text-fg'
  }
</script>

{#snippet folderNode(folder: DashboardFolder, depth: number)}
  {@const kids = childrenOf(folder.id)}
  {@const open = expanded.has(folder.id)}
  {@const active = selected === folder.id}
  <div
    class="group flex h-7 items-center gap-1 rounded-md pr-1 text-[13px] transition-colors {rowTone(active, dragOver === folder.id)}"
    style="padding-left: {8 + depth * 14}px"
    role="treeitem"
    aria-selected={active}
    aria-expanded={kids.length > 0 ? open : undefined}
    tabindex="-1"
    ondragover={(e) => onDragOver(e, folder.id)}
    ondragleave={() => onDragLeave(folder.id)}
    ondrop={(e) => onDrop(e, folder.id)}
  >
    <button
      class="flex h-5 w-5 shrink-0 items-center justify-center rounded text-fg-4 hover:text-fg {kids.length === 0 ? 'invisible' : ''}"
      onclick={(e) => { e.stopPropagation(); toggle(folder.id) }}
      aria-label={open ? 'Collapse' : 'Expand'}
      tabindex="-1"
    >
      {#if open}<ChevronDown size={12} />{:else}<ChevronRight size={12} />{/if}
    </button>
    <button class="flex min-w-0 flex-1 items-center gap-2 text-left" onclick={() => onselect(folder.id)}>
      {#if open && kids.length > 0}
        <FolderOpen size={14} class="shrink-0 {active ? 'text-accent' : 'text-fg-3'}" />
      {:else}
        <Folder size={14} class="shrink-0 {active ? 'text-accent' : 'text-fg-3'}" />
      {/if}
      <span class="truncate">{folder.name}</span>
    </button>
    <span class="shrink-0 text-[11px] tabular-nums text-fg-4 {canWrite && onfoldermenu ? 'group-hover:hidden' : ''}">{countIn(folder.id)}</span>
    {#if canWrite && onfoldermenu}
      <span class="hidden shrink-0 group-hover:inline-flex">
        <Button icon variant="ghost" size="xs" aria-label="Folder actions" onclick={(e) => onfoldermenu(e, folder)}>
          <MoreHorizontal size={13} />
        </Button>
      </span>
    {/if}
  </div>
  {#if open}
    {#each kids as child (child.id)}
      {@render folderNode(child, depth + 1)}
    {/each}
  {/if}
{/snippet}

<div class="flex w-60 shrink-0 flex-col overflow-y-auto border-r border-edge-subtle bg-surface px-2 py-2" role="tree" aria-label={ariaLabel}>
  <div
    class="flex h-7 items-center gap-2 rounded-md px-2 text-[13px] transition-colors {rowTone(selected === null, dragOver === null)}"
    role="treeitem"
    aria-selected={selected === null}
    tabindex="-1"
    ondragover={(e) => onDragOver(e, null)}
    ondragleave={() => onDragLeave(null)}
    ondrop={(e) => onDrop(e, null)}
  >
    <button class="flex min-w-0 flex-1 items-center gap-2 text-left" onclick={() => onselect(null)}>
      <RootIcon size={14} class="shrink-0 {selected === null ? 'text-accent' : 'text-fg-3'}" />
      <span class="truncate font-medium">{rootLabel}</span>
    </button>
    <span class="text-[11px] tabular-nums text-fg-4">{total}</span>
  </div>
  <div class="mt-1 space-y-px">
    {#each childrenOf(null) as folder (folder.id)}
      {@render folderNode(folder, 0)}
    {/each}
  </div>
  {#if folders.length === 0 && canWrite && onnewfolder}
    <button class="mt-2 flex h-7 items-center gap-2 rounded-md px-2 text-xs text-fg-4 hover:bg-hover hover:text-fg" onclick={onnewfolder}>
      <FolderPlus size={13} /> Create a folder
    </button>
  {/if}
</div>
