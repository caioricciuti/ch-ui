<script lang="ts">
  import {
    getGroupTabs,
    getGroupActiveTabId,
    setActiveTab,
    openQueryTab,
    closeTab,
    renameTab,
    reorderTab,
    splitTab,
    moveTabToGroup,
    isSplit,
    setFocusedGroup,
    getTabs,
    getFocusedGroupId,
  } from '../../stores/tabs.svelte'
  import type { QueryTab, Tab } from '../../stores/tabs.svelte'
  import {
    Plus,
    X,
    SquareTerminal,
    Table2,
    Bookmark,
    LayoutDashboard,
    Clock,
    Brain,
    Shield,
    Settings,
    Columns2,
    Database,
    CopyPlus,
    Pencil,
    PanelLeft,
    PanelRight,
    House,
  } from 'lucide-svelte'
  import ContextMenu, { type ContextMenuItem } from '../common/ContextMenu.svelte'
  import ConfirmDialog from '../common/ConfirmDialog.svelte'

  interface Props {
    groupId: string
  }

  let { groupId }: Props = $props()

  const tabs = $derived(getGroupTabs(groupId))
  const activeId = $derived(getGroupActiveTabId(groupId))
  const split = $derived(isSplit())
  const focusedGroupId = $derived(getFocusedGroupId())

  let editingTabId = $state<string | null>(null)
  let editingName = $state('')

  let dragTabId = $state<string | null>(null)
  let dropTargetIndex = $state<number | null>(null)
  let splitDropActive = $state(false)

  let tabMenu = $state<{ tabId: string; x: number; y: number } | null>(null)
  let pendingCloseIds = $state<string[]>([])
  let closeConfirmOpen = $state(false)
  let closeConfirmDescription = $state('')

  const iconMap: Record<string, typeof SquareTerminal> = {
    'home': House,
    'query': SquareTerminal,
    'table': Table2,
    'database': Database,
    'dashboard': LayoutDashboard,
    'saved-queries': Bookmark,
    'dashboards': LayoutDashboard,
    'schedules': Clock,
    'brain': Brain,
    'admin': Shield,
    'governance': Shield,
    'settings': Settings,
  }

  function isHomeTab(tab: Tab): boolean {
    return tab.type === 'home'
  }

  function getIcon(tab: Tab) {
    if (tab.type === 'query' && (tab as QueryTab).savedQueryId) return Bookmark
    return iconMap[tab.type] ?? SquareTerminal
  }

  function hideTabMenu() {
    tabMenu = null
  }

  function openTabMenu(e: MouseEvent, tabId: string) {
    const tab = getTabs().find((entry) => entry.id === tabId)
    if (tab && isHomeTab(tab)) return
    e.preventDefault()
    e.stopPropagation()
    tabMenu = { tabId, x: e.clientX, y: e.clientY }
  }

  function getMenuTab(): Tab | undefined {
    if (!tabMenu) return undefined
    return getTabs().find(t => t.id === tabMenu?.tabId)
  }

  function startRename(tab: Tab) {
    editingTabId = tab.id
    editingName = tab.name
    hideTabMenu()
  }

  function commitRename() {
    if (editingTabId && editingName.trim()) {
      renameTab(editingTabId, editingName.trim())
    }
    editingTabId = null
  }

  function handleRenameKeydown(e: KeyboardEvent) {
    if (e.key === 'Enter') commitRename()
    if (e.key === 'Escape') editingTabId = null
  }

  function handleMiddleClick(e: MouseEvent, tabId: string) {
    if (e.button === 1) {
      const tab = getTabs().find((entry) => entry.id === tabId)
      if (!tab || isHomeTab(tab)) return
      e.preventDefault()
      requestCloseTabs([tabId])
    }
  }

  function handleTabClick(tabId: string) {
    setFocusedGroup(groupId)
    setActiveTab(tabId, groupId)
  }

  function duplicateQueryTab(tab: Tab | undefined) {
    if (!tab || tab.type !== 'query') return
    openQueryTab((tab as QueryTab).sql, groupId)
    hideTabMenu()
  }

  function closeOthers(tabId: string) {
    const otherIds = getTabs().map(t => t.id).filter(id => id !== tabId)
    requestCloseTabs(otherIds)
    hideTabMenu()
  }

  function closeTabsToSide(tabId: string, side: 'left' | 'right') {
    const idx = tabs.findIndex(t => t.id === tabId)
    if (idx === -1) return
    const targets = side === 'left' ? tabs.slice(0, idx) : tabs.slice(idx + 1)
    requestCloseTabs(targets.map((tab) => tab.id))
    hideTabMenu()
  }

  function splitFromMenu(tabId: string) {
    splitTab(tabId)
    hideTabMenu()
  }

  function resetPendingClose() {
    pendingCloseIds = []
    closeConfirmDescription = ''
    closeConfirmOpen = false
  }

  function requestCloseTabs(tabIds: string[]) {
    const uniqueIds = Array.from(new Set(tabIds)).filter((id) => {
      const tab = getTabs().find((entry) => entry.id === id)
      return !!tab && !isHomeTab(tab)
    })
    if (uniqueIds.length === 0) return

    const dirtyTabs = uniqueIds
      .map((id) => getTabs().find((tab) => tab.id === id))
      .filter((tab): tab is QueryTab => !!tab && tab.type === 'query' && tab.dirty)

    if (dirtyTabs.length === 0) {
      uniqueIds.forEach((id) => closeTab(id))
      return
    }

    pendingCloseIds = uniqueIds
    if (dirtyTabs.length === 1 && uniqueIds.length === 1) {
      closeConfirmDescription = `"${dirtyTabs[0].name}" has unsaved changes. Close without saving?`
    } else {
      closeConfirmDescription = `${dirtyTabs.length} tab(s) have unsaved changes. Close selected tabs anyway?`
    }
    closeConfirmOpen = true
  }

  function confirmCloseTabs() {
    pendingCloseIds.forEach((id) => closeTab(id))
    resetPendingClose()
  }

  // ── Drag-to-reorder ──────────────────────────────────────────
  function handleDragStart(e: DragEvent, tab: Tab) {
    if (isHomeTab(tab)) {
      e.preventDefault()
      return
    }
    if (!e.dataTransfer) return
    dragTabId = tab.id
    splitDropActive = false
    e.dataTransfer.effectAllowed = 'move'
    e.dataTransfer.setData('text/plain', tab.id)
    e.dataTransfer.setData('application/x-tab-group', groupId)
  }

  function handleDragOver(e: DragEvent, index: number) {
    e.preventDefault()
    if (e.dataTransfer) e.dataTransfer.dropEffect = 'move'
    dropTargetIndex = index
  }

  function handleDrop(e: DragEvent, toIndex: number) {
    e.preventDefault()
    const tabId = e.dataTransfer?.getData('text/plain')
    const sourceGroup = e.dataTransfer?.getData('application/x-tab-group')

    if (!tabId) return

    if (sourceGroup && sourceGroup !== groupId) {
      moveTabToGroup(tabId, groupId)
    } else if (dragTabId) {
      const fromIndex = tabs.findIndex(t => t.id === dragTabId)
      if (fromIndex !== -1 && fromIndex !== toIndex) {
        reorderTab(groupId, fromIndex, toIndex)
      }
    }

    dragTabId = null
    dropTargetIndex = null
    splitDropActive = false
  }

  function handleContainerDrop(e: DragEvent) {
    e.preventDefault()
    const tabId = e.dataTransfer?.getData('text/plain')
    const sourceGroup = e.dataTransfer?.getData('application/x-tab-group')
    if (tabId && sourceGroup && sourceGroup !== groupId) {
      moveTabToGroup(tabId, groupId)
    }
    dragTabId = null
    dropTargetIndex = null
  }

  function handleContainerDragOver(e: DragEvent) {
    e.preventDefault()
    if (e.dataTransfer) e.dataTransfer.dropEffect = 'move'
  }

  function handleDragEnd() {
    dragTabId = null
    dropTargetIndex = null
    splitDropActive = false
  }

  function handleSplit(e: MouseEvent, tabId: string) {
    e.stopPropagation()
    splitTab(tabId)
  }

  function handleSplitDropOver(e: DragEvent) {
    e.preventDefault()
    splitDropActive = true
    if (e.dataTransfer) e.dataTransfer.dropEffect = 'move'
  }

  function handleSplitDropLeave() {
    splitDropActive = false
  }

  function handleSplitDrop(e: DragEvent) {
    e.preventDefault()
    const tabId = e.dataTransfer?.getData('text/plain')
    if (!tabId) return
    splitTab(tabId)
    dragTabId = null
    dropTargetIndex = null
    splitDropActive = false
  }

  function shouldIgnoreShortcutTarget(target: EventTarget | null): boolean {
    if (!(target instanceof HTMLElement)) return false
    return target.tagName === 'INPUT' ||
      target.tagName === 'TEXTAREA' ||
      target.tagName === 'SELECT' ||
      target.isContentEditable
  }

  function handleGlobalTabShortcuts(e: KeyboardEvent) {
    if (focusedGroupId !== groupId) return
    if (editingTabId) return

    const mod = e.metaKey || e.ctrlKey
    const currentTab = tabs.find((t) => t.id === activeId)
    if (!currentTab) return

    if ((e.altKey && e.key.toLowerCase() === 'w') || (mod && e.shiftKey && e.key.toLowerCase() === 'w')) {
      if (isHomeTab(currentTab)) return
      e.preventDefault()
      requestCloseTabs([currentTab.id])
      return
    }

    if (((e.altKey && e.key.toLowerCase() === 'd') || (mod && e.shiftKey && e.key.toLowerCase() === 'd')) && currentTab.type === 'query') {
      e.preventDefault()
      duplicateQueryTab(currentTab)
      return
    }

    if (e.altKey && e.key.toLowerCase() === 's') {
      e.preventDefault()
      splitFromMenu(currentTab.id)
      return
    }

    if (e.key === 'F2' && !shouldIgnoreShortcutTarget(e.target)) {
      e.preventDefault()
      startRename(currentTab)
    }
  }

  function getTabMenuItems(): ContextMenuItem[] {
    if (!tabMenu) return []
    const menuTab = getTabs().find(t => t.id === tabMenu?.tabId)
    if (!menuTab) return []
    if (isHomeTab(menuTab)) return []

    const idxInGroup = tabs.findIndex(t => t.id === menuTab.id)
    const canCloseLeft = idxInGroup > 0 && tabs.slice(0, idxInGroup).some((tab) => !isHomeTab(tab))
    const canCloseRight = idxInGroup >= 0 && tabs.slice(idxInGroup + 1).some((tab) => !isHomeTab(tab))
    const canCloseOthers = getTabs().filter((tab) => !isHomeTab(tab) && tab.id !== menuTab.id).length > 0

    const items: ContextMenuItem[] = []

    if (menuTab.type === 'query') {
      items.push({
        id: 'duplicate',
        label: 'Duplicate Query',
        icon: CopyPlus,
        shortcut: 'Alt+D',
        onSelect: () => duplicateQueryTab(menuTab),
      })
    }

    items.push({
      id: 'rename',
      label: 'Rename',
      icon: Pencil,
      shortcut: 'F2',
      onSelect: () => startRename(menuTab),
    })

    items.push({
      id: 'split',
      label: split ? 'Move To Other Pane' : 'Split To New Pane',
      icon: Columns2,
      shortcut: 'Alt+S',
      onSelect: () => splitFromMenu(menuTab.id),
    })

    if (canCloseLeft || canCloseRight || canCloseOthers) {
      items.push({ id: 'sep-close', separator: true })
    }

    if (canCloseLeft) {
      items.push({
        id: 'close-left',
        label: 'Close Tabs To Left',
        icon: PanelLeft,
        onSelect: () => closeTabsToSide(menuTab.id, 'left'),
      })
    }
    if (canCloseRight) {
      items.push({
        id: 'close-right',
        label: 'Close Tabs To Right',
        icon: PanelRight,
        onSelect: () => closeTabsToSide(menuTab.id, 'right'),
      })
    }
    if (canCloseOthers) {
      items.push({
        id: 'close-others',
        label: 'Close Other Tabs',
        onSelect: () => closeOthers(menuTab.id),
      })
    }

    items.push({ id: 'sep-end', separator: true })
    items.push({
      id: 'close',
      label: 'Close',
      icon: X,
      shortcut: 'Alt+W',
      danger: true,
      onSelect: () => {
        requestCloseTabs([menuTab.id])
        hideTabMenu()
      },
    })

    return items
  }
</script>

<svelte:window
  onkeydown={(e) => {
    if (e.key === 'Escape') hideTabMenu()
    handleGlobalTabShortcuts(e)
  }}
/>

<!-- svelte-ignore a11y_no_static_element_interactions -->
<div
  class="relative flex items-center border-b border-gray-200 dark:border-gray-800 bg-white dark:bg-gray-950 overflow-x-auto shrink-0 whitespace-nowrap"
  ondragover={handleContainerDragOver}
  ondrop={handleContainerDrop}
>
  {#each tabs as tab, i (tab.id)}
    {@const Icon = getIcon(tab)}
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div
      class="group/tab flex items-center gap-2 h-9 text-[13px] border-r border-gray-200 dark:border-gray-800 shrink-0 cursor-pointer select-none
        {isHomeTab(tab) ? 'px-2.5 w-10 justify-center' : 'px-3.5 max-w-[260px]'}
        {tab.id === activeId
          ? 'bg-gray-50 dark:bg-gray-900 text-gray-800 dark:text-gray-200 border-b-2 border-b-ch-blue'
          : 'text-gray-500 hover:text-gray-700 dark:hover:text-gray-300 hover:bg-gray-100/50 dark:hover:bg-gray-900/50'}
        {dragTabId === tab.id ? 'opacity-40' : ''}
        {dropTargetIndex === i && dragTabId !== tab.id ? 'border-l-2 !border-l-ch-blue' : ''}"
      draggable={!isHomeTab(tab)}
      onclick={() => handleTabClick(tab.id)}
      oncontextmenu={(e) => openTabMenu(e, tab.id)}
      ondblclick={() => tab.type === 'query' && !isHomeTab(tab) && startRename(tab)}
      onmousedown={(e) => handleMiddleClick(e, tab.id)}
      onkeydown={(e) => {
        if (e.key === 'Enter' || e.key === ' ') {
          e.preventDefault()
          handleTabClick(tab.id)
        }
      }}
      ondragstart={(e) => handleDragStart(e, tab)}
      ondragover={(e) => handleDragOver(e, i)}
      ondrop={(e) => handleDrop(e, i)}
      ondragend={handleDragEnd}
      role="tab"
      tabindex="0"
      aria-selected={tab.id === activeId}
      title={tab.name}
    >
      <Icon size={13} class="shrink-0" />

      {#if editingTabId === tab.id}
        <input
          type="text"
          class="w-full bg-transparent border-b border-ch-blue text-[13px] outline-none text-gray-800 dark:text-gray-200 leading-none"
          bind:value={editingName}
          onblur={commitRename}
          onkeydown={handleRenameKeydown}
          onclick={(e) => e.stopPropagation()}
        />
      {:else}
        {#if isHomeTab(tab)}
          <span class="sr-only">{tab.name}</span>
        {:else}
          <span class="truncate leading-none">{tab.name}</span>
        {/if}
      {/if}

      {#if tab.type === 'query' && tab.dirty}
        <span class="w-1.5 h-1.5 rounded-full bg-ch-orange shrink-0"></span>
      {/if}

      {#if getTabs().length > 1 && !isHomeTab(tab)}
        <button
          class="p-0.5 rounded text-gray-400 hover:text-ch-blue hover:bg-gray-300 dark:hover:bg-gray-700 shrink-0 opacity-0 group-hover/tab:opacity-100 transition-opacity"
          onclick={(e) => handleSplit(e, tab.id)}
          title="Split tab"
        >
          <Columns2 size={11} />
        </button>
      {/if}

      {#if !isHomeTab(tab)}
        <button
          class="ml-auto p-0.5 rounded hover:bg-gray-300 dark:hover:bg-gray-700 text-gray-500 hover:text-gray-700 dark:hover:text-gray-300 shrink-0"
          onclick={(e: MouseEvent) => { e.stopPropagation(); requestCloseTabs([tab.id]) }}
          title="Close tab"
        >
          <X size={13} />
        </button>
      {/if}
    </div>
  {/each}

  <button
    class="px-2.5 h-9 text-gray-400 dark:text-gray-600 hover:text-gray-700 dark:hover:text-gray-300 hover:bg-gray-200/50 dark:hover:bg-gray-800/50 shrink-0"
    onclick={() => openQueryTab('', groupId)}
    title="New query"
  >
    <Plus size={15} />
  </button>

  {#if !split && dragTabId}
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div
      class="mx-1 my-1 px-2.5 py-1 text-[11px] rounded-md border border-dashed transition-colors shrink-0
      {splitDropActive
        ? 'border-ch-blue text-ch-blue bg-ch-blue/10'
        : 'border-gray-300 dark:border-gray-700 text-gray-500 dark:text-gray-400'}"
      ondragover={handleSplitDropOver}
      ondragleave={handleSplitDropLeave}
      ondrop={handleSplitDrop}
      title="Drop tab to split"
    >
      Drop to Split
    </div>
  {/if}

</div>

<ContextMenu
  open={!!tabMenu}
  x={tabMenu?.x ?? 0}
  y={tabMenu?.y ?? 0}
  items={getTabMenuItems()}
  onclose={hideTabMenu}
/>

<ConfirmDialog
  open={closeConfirmOpen}
  title="Discard unsaved changes?"
  description={closeConfirmDescription}
  confirmLabel="Close Tabs"
  destructive={true}
  onconfirm={confirmCloseTabs}
  oncancel={resetPendingClose}
/>
