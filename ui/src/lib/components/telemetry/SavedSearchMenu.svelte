<script lang="ts">
  import { onMount } from 'svelte'
  import { Bookmark, MoreHorizontal, Plus } from 'lucide-svelte'
  import Button from '../common/Button.svelte'
  import Badge from '../common/Badge.svelte'
  import InputDialog from '../common/InputDialog.svelte'
  import ConfirmDialog from '../common/ConfirmDialog.svelte'
  import ContextMenu, { type ContextMenuItem } from '../common/ContextMenu.svelte'
  import { getSession } from '../../stores/session.svelte'
  import { success as toastSuccess, error as toastError } from '../../stores/toast.svelte'
  import { listSavedSearches, createSavedSearch, updateSavedSearch, deleteSavedSearch } from '../../api/telemetry'
  import type { SavedSearch, SearchKind } from '../../types/telemetry'

  /**
   * "Saved" button with a popover listing the saved searches of one kind.
   * Loading one hands query + range preset back to the owner; saving stores
   * the owner's current query. Writers only for save, rename, delete.
   */
  interface Props {
    kind: SearchKind
    sourceId: string
    currentQuery: string
    currentRange: string
    onload: (query: string, rangePreset: string) => void
  }

  let { kind, sourceId, currentQuery, currentRange, onload }: Props = $props()

  const canWrite = $derived(getSession()?.role !== 'viewer')

  let open = $state(false)
  let searches = $state<SavedSearch[]>([])
  let loading = $state(false)
  let root = $state<HTMLDivElement | null>(null)

  let saveOpen = $state(false)
  let saveName = $state('')
  let saving = $state(false)

  let renameTarget = $state<SavedSearch | null>(null)
  let renameValue = $state('')
  let renaming = $state(false)

  let deleteTarget = $state<SavedSearch | null>(null)
  let deleting = $state(false)

  let menu = $state<{ search: SavedSearch; x: number; y: number } | null>(null)

  const mine = $derived(searches.filter((s) => s.kind === kind))

  async function load() {
    loading = true
    try {
      searches = await listSavedSearches()
    } catch (e: unknown) {
      toastError(e instanceof Error ? e.message : String(e))
    } finally {
      loading = false
    }
  }

  onMount(load)

  function toggle() {
    open = !open
    if (open) void load()
  }

  function onDocClick(e: MouseEvent) {
    if (!open || !root) return
    if (!root.contains(e.target as Node)) open = false
  }

  function pick(s: SavedSearch) {
    open = false
    onload(s.query, s.range_preset)
  }

  async function save(name: string) {
    const n = name.trim()
    if (!n) return
    saving = true
    try {
      const created = await createSavedSearch({ kind, name: n, query: currentQuery, range_preset: currentRange, source_id: sourceId || null })
      searches = [created, ...searches]
      toastSuccess(`Saved "${created.name}"`)
      saveOpen = false
      saveName = ''
    } catch (e: unknown) {
      toastError(e instanceof Error ? e.message : String(e))
    } finally {
      saving = false
    }
  }

  async function rename(name: string) {
    const t = renameTarget
    const n = name.trim()
    if (!t || !n) return
    renaming = true
    try {
      const updated = await updateSavedSearch(t.id, { kind: t.kind, name: n, query: t.query, range_preset: t.range_preset, source_id: t.source_id })
      searches = searches.map((s) => (s.id === t.id ? updated : s))
      toastSuccess('Search renamed')
      renameTarget = null
    } catch (e: unknown) {
      toastError(e instanceof Error ? e.message : String(e))
    } finally {
      renaming = false
    }
  }

  async function remove() {
    const t = deleteTarget
    if (!t) return
    deleting = true
    try {
      await deleteSavedSearch(t.id)
      searches = searches.filter((s) => s.id !== t.id)
      toastSuccess('Search deleted')
      deleteTarget = null
    } catch (e: unknown) {
      toastError(e instanceof Error ? e.message : String(e))
    } finally {
      deleting = false
    }
  }

  function openMenu(e: MouseEvent, s: SavedSearch) {
    e.preventDefault()
    e.stopPropagation()
    const rect = (e.currentTarget as HTMLElement).getBoundingClientRect()
    menu = { search: s, x: Math.min(window.innerWidth - 200, rect.left - 140), y: rect.bottom + 4 }
  }

  function menuItems(): ContextMenuItem[] {
    const s = menu?.search
    if (!s) return []
    return [
      { id: 'load', label: 'Load', onSelect: () => pick(s) },
      { id: 'sep', separator: true },
      { id: 'rename', label: 'Rename', onSelect: () => { renameTarget = s; renameValue = s.name } },
      { id: 'delete', label: 'Delete', danger: true, onSelect: () => (deleteTarget = s) },
    ]
  }
</script>

<svelte:document onclick={onDocClick} />

<div bind:this={root} class="relative">
  <Button size="sm" variant="outline" aria-expanded={open} title="Saved searches" onclick={toggle}>
    <Bookmark size={13} /> Saved
  </Button>

  {#if open}
    <div class="surface-card absolute right-0 z-[120] mt-1 w-80 rounded-md py-1" role="menu">
      {#if canWrite}
        <button
          class="flex w-full items-center gap-2 px-3 py-1.5 text-left text-[13px] text-fg transition-colors hover:bg-hover"
          onclick={() => { open = false; saveName = ''; saveOpen = true }}
        >
          <Plus size={13} class="text-fg-3" /> Save current search…
        </button>
        <div class="my-1 border-t border-edge-subtle"></div>
      {/if}
      {#if loading && mine.length === 0}
        <div class="px-3 py-2 text-xs text-fg-4">Loading…</div>
      {:else if mine.length === 0}
        <div class="px-3 py-2 text-xs text-fg-4">No saved searches yet.</div>
      {:else}
        <div class="max-h-80 overflow-y-auto">
          {#each mine as s (s.id)}
            <div class="group flex items-center gap-2 px-3 py-1.5 transition-colors hover:bg-hover">
              <button class="min-w-0 flex-1 text-left" onclick={() => pick(s)} title={s.query || '(empty query)'}>
                <div class="flex items-center gap-2">
                  <span class="truncate text-[13px] text-fg">{s.name}</span>
                  <Badge>{s.range_preset}</Badge>
                </div>
                <div class="truncate font-mono text-[11px] text-fg-4">{s.query || '(empty query)'}</div>
              </button>
              {#if canWrite}
                <Button icon variant="ghost" size="xs" class="opacity-0 group-hover:opacity-100 focus-visible:opacity-100" aria-label="Saved search actions" onclick={(e) => openMenu(e, s)}>
                  <MoreHorizontal size={13} />
                </Button>
              {/if}
            </div>
          {/each}
        </div>
      {/if}
    </div>
  {/if}
</div>

<ContextMenu open={!!menu} x={menu?.x ?? 0} y={menu?.y ?? 0} items={menuItems()} onclose={() => (menu = null)} />

<InputDialog
  open={saveOpen}
  title="Save search"
  description={currentQuery ? `Query: ${currentQuery}` : 'Saves the current query and time range.'}
  placeholder="Checkout errors"
  bind:value={saveName}
  confirmLabel="Save"
  loading={saving}
  onconfirm={save}
  oncancel={() => (saveOpen = false)}
/>

<InputDialog
  open={!!renameTarget}
  title="Rename saved search"
  placeholder="Name"
  bind:value={renameValue}
  confirmLabel="Rename"
  loading={renaming}
  onconfirm={rename}
  oncancel={() => (renameTarget = null)}
/>

<ConfirmDialog
  open={!!deleteTarget}
  title="Delete saved search?"
  description={deleteTarget ? `"${deleteTarget.name}" is removed for everyone on this connection.` : ''}
  confirmLabel="Delete"
  destructive
  loading={deleting}
  onconfirm={remove}
  oncancel={() => (deleteTarget = null)}
/>
