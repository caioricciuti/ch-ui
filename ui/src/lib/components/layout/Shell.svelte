<script lang="ts">
  import { onMount } from 'svelte'
  import Sidebar from './Sidebar.svelte'
  import TabGroup from './TabGroup.svelte'
  import CommandPalette from './CommandPalette.svelte'
  import PageRouter from './PageRouter.svelte'
  import ContextPanel from './ContextPanel.svelte'
  import { getRouteType } from '../../stores/router.svelte'
  import { togglePanel } from '../../stores/nav.svelte'
  import { groupForRoute, isPageRouteType } from '../../routes'
  import { getGroups, isSplit, setFocusedGroup, splitTabToSide, openQueryTab } from '../../stores/tabs.svelte'
  import { openCommandPalette, toggleCommandPalette } from '../../stores/command-palette.svelte'

  const groups = $derived(getGroups())
  const split = $derived(isSplit())

  // Split pane resize state
  let splitPercent = $state(50)
  let resizing = $state(false)
  let containerEl: HTMLDivElement | undefined = $state()
  const EDGE_SPLIT_WIDTH = 72
  let edgeSplitVisible = $state(false)
  let edgeSplitSide = $state<'left' | 'right' | null>(null)

  // ── Resize handlers ──────────────────────────────────────────

  function onResizeStart(e: MouseEvent) {
    e.preventDefault()
    resizing = true
    document.addEventListener('mousemove', onResizeMove)
    document.addEventListener('mouseup', onResizeEnd)
  }

  function onResizeMove(e: MouseEvent) {
    if (!containerEl) return
    const rect = containerEl.getBoundingClientRect()
    const x = e.clientX - rect.left
    splitPercent = Math.max(20, Math.min(80, (x / rect.width) * 100))
  }

  function onResizeEnd() {
    resizing = false
    document.removeEventListener('mousemove', onResizeMove)
    document.removeEventListener('mouseup', onResizeEnd)
  }

  function handleGlobalShortcuts(e: KeyboardEvent) {
    const mod = e.metaKey || e.ctrlKey
    const target = e.target as HTMLElement | null
    const isTypingTarget = !!target && (
      target.tagName === 'INPUT' ||
      target.tagName === 'TEXTAREA' ||
      target.tagName === 'SELECT' ||
      target.isContentEditable
    )

    if (mod && e.key.toLowerCase() === 'k') {
      e.preventDefault()
      toggleCommandPalette()
      return
    }

    if (!isTypingTarget && (e.altKey && e.key.toLowerCase() === 'k' || e.key === '/')) {
      e.preventDefault()
      openCommandPalette()
      return
    }

    if (isTypingTarget) return

    if (mod && e.key.toLowerCase() === 'b' && !isPageRouteType(getRouteType())) {
      e.preventDefault()
      togglePanel(groupForRoute(getRouteType()).id)
      return
    }

    if ((mod && e.shiftKey && e.key.toLowerCase() === 'n') || (e.altKey && e.key.toLowerCase() === 'n')) {
      e.preventDefault()
      openQueryTab()
    }
  }

  onMount(() => {
    window.addEventListener('keydown', handleGlobalShortcuts, true)
    return () => window.removeEventListener('keydown', handleGlobalShortcuts, true)
  })

  function resetEdgeSplitState() {
    edgeSplitVisible = false
    edgeSplitSide = null
  }

  function isTabDrag(e: DragEvent): boolean {
    const types = e.dataTransfer?.types
    if (!types) return false
    return types.includes('text/plain')
  }

  function handleContentDragOver(e: DragEvent) {
    if (!isTabDrag(e)) return
    const dt = e.dataTransfer
    if (!dt) return
    if (!containerEl) return
    e.preventDefault()
    dt.dropEffect = 'move'

    const rect = containerEl.getBoundingClientRect()
    const x = e.clientX - rect.left
    edgeSplitVisible = true

    if (x <= EDGE_SPLIT_WIDTH) {
      edgeSplitSide = 'left'
    } else if (x >= rect.width - EDGE_SPLIT_WIDTH) {
      edgeSplitSide = 'right'
    } else {
      edgeSplitSide = null
    }
  }

  function handleEdgeDragOver(side: 'left' | 'right', e: DragEvent) {
    if (!isTabDrag(e)) return
    const dt = e.dataTransfer
    if (!dt) return
    e.preventDefault()
    dt.dropEffect = 'move'
    edgeSplitVisible = true
    edgeSplitSide = side
  }

  function handleEdgeDrop(side: 'left' | 'right', e: DragEvent) {
    const tabId = e.dataTransfer?.getData('text/plain')
    if (!tabId) {
      resetEdgeSplitState()
      return
    }
    e.preventDefault()
    splitTabToSide(tabId, side)
    resetEdgeSplitState()
  }

  function handleContentDrop(e: DragEvent) {
    const tabId = e.dataTransfer?.getData('text/plain')
    if (!tabId || !edgeSplitSide) {
      resetEdgeSplitState()
      return
    }
    e.preventDefault()
    splitTabToSide(tabId, edgeSplitSide)
    resetEdgeSplitState()
  }

  function handleContentDragLeave() {
    edgeSplitSide = null
  }

  const onPage = $derived(isPageRouteType(getRouteType()))
</script>

<svelte:window ondragend={resetEdgeSplitState} ondrop={resetEdgeSplitState} />

<div class="flex h-full">
  <Sidebar />
  <ContextPanel />

  <!-- Pages: full-screen product areas driven by the URL -->
  <div class="flex min-w-0 flex-1 flex-col overflow-hidden" hidden={!onPage}>
    <PageRouter />
  </div>

  <!-- Workspace: the tab groups. Stays mounted while a page is shown so
       running queries and results survive navigation. -->
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div
    hidden={onPage}
    class="relative flex flex-1 min-w-0"
    bind:this={containerEl}
    ondragover={handleContentDragOver}
    ondrop={handleContentDrop}
    ondragleave={handleContentDragLeave}
  >
    {#each groups as group, i (group.id)}
      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <div
        class="flex flex-col min-w-0 overflow-hidden"
        style={split ? `width: ${i === 0 ? splitPercent : 100 - splitPercent}%` : 'flex: 1'}
        onclick={() => setFocusedGroup(group.id)}
        onkeydown={(e) => {
          // Only treat Space/Enter as pane activation when the pane itself is focused.
          if (e.target !== e.currentTarget) return
          if (e.key === 'Enter' || e.key === ' ') {
            e.preventDefault()
            setFocusedGroup(group.id)
          }
        }}
        role="button"
        tabindex="0"
      >
        <TabGroup groupId={group.id} />
      </div>

      {#if split && i === 0}
        <!-- Resize handle between split panes -->
        <!-- svelte-ignore a11y_no_static_element_interactions -->
        <div
          class="group/split flex w-1 shrink-0 cursor-col-resize items-center justify-center transition-colors hover:bg-active {resizing ? 'bg-accent/60' : 'bg-edge-subtle'}"
          onmousedown={onResizeStart}
          ondblclick={() => splitPercent = 50}
        >
          <div class="h-8 w-0.5 rounded-full transition-colors {resizing ? 'bg-accent' : 'bg-edge-strong group-hover/split:bg-accent/60'}"></div>
        </div>
      {/if}
    {/each}

    {#if edgeSplitVisible}
      <div class="pointer-events-none absolute inset-0 z-30">
        <!-- svelte-ignore a11y_no_static_element_interactions -->
        <div
          class="pointer-events-auto absolute left-0 top-0 bottom-0 transition-colors"
          style={`width:${EDGE_SPLIT_WIDTH}px`}
          ondragover={(e) => handleEdgeDragOver('left', e)}
          ondrop={(e) => handleEdgeDrop('left', e)}
        >
          <div class="absolute inset-0 transition-colors {edgeSplitSide === 'left' ? 'bg-accent-soft border-r border-accent/50' : 'bg-transparent'}"></div>
        </div>
        <!-- svelte-ignore a11y_no_static_element_interactions -->
        <div
          class="pointer-events-auto absolute right-0 top-0 bottom-0 transition-colors"
          style={`width:${EDGE_SPLIT_WIDTH}px`}
          ondragover={(e) => handleEdgeDragOver('right', e)}
          ondrop={(e) => handleEdgeDrop('right', e)}
        >
          <div class="absolute inset-0 transition-colors {edgeSplitSide === 'right' ? 'bg-accent-soft border-l border-accent/50' : 'bg-transparent'}"></div>
        </div>
      </div>
    {/if}

  </div>
</div>

<CommandPalette />

{#if resizing}
  <div class="fixed inset-0 z-50 cursor-col-resize"></div>
{/if}
