<script lang="ts">
  import DatabaseTree from '../explorer/DatabaseTree.svelte'
  import Button from '../common/Button.svelte'
  import { openTableTab } from '../../stores/tabs.svelte'
  import { goTo, getRouteType } from '../../stores/router.svelte'
  import { getSession } from '../../stores/session.svelte'
  import { isProActive } from '../../stores/license.svelte'
  import { getSection, setSection, isPanelCollapsed, setPanelCollapsed } from '../../stores/nav.svelte'
  import {
    groupForRoute, isPageRouteType, PAGE_ROUTES, PAGE_SECTIONS, ADMIN_ONLY_ROUTES, type PageRoute,
  } from '../../routes'
  import { PanelLeftClose, PanelLeftOpen, BookOpen, ExternalLink } from 'lucide-svelte'

  // Second column. On the workspace it is the schema explorer: resizable,
  // collapsible, width remembered. On a page it is a plain sidebar for the
  // active rail group: fixed width, always open, an icon per page, the
  // active page's sections nested under it.
  const MIN_WIDTH = 220
  const MAX_WIDTH = 560
  const DEFAULT_WIDTH = 300
  const NAV_WIDTH = 224
  const COLLAPSED_WIDTH = 32
  const NARROW_QUERY = '(max-width: 1100px)'
  const WIDTH_KEY = 'ch-ui-explorer-width'

  const savedWidth = parseInt(localStorage.getItem(WIDTH_KEY) ?? String(DEFAULT_WIDTH), 10)
  let explorerWidth = $state(isNaN(savedWidth) ? DEFAULT_WIDTH : savedWidth)
  let dragging = $state(false)
  let narrow = $state(typeof matchMedia !== 'undefined' && matchMedia(NARROW_QUERY).matches)
  let panelEl: HTMLDivElement | undefined = $state()

  const session = $derived(getSession())
  const routeType = $derived(getRouteType())
  const onWorkspace = $derived(!isPageRouteType(routeType))
  const group = $derived(groupForRoute(routeType))
  // Only the explorer collapses (button, ⌘B, drag, narrow screens). Page
  // sidebars are always open: there is nothing else in that column.
  const collapsed = $derived(onWorkspace && (isPanelCollapsed(group.id) || narrow))
  const width = $derived(collapsed ? COLLAPSED_WIDTH : onWorkspace ? explorerWidth : NAV_WIDTH)
  const licensedPro = $derived(isProActive())
  const routes = $derived(
    group.routes.filter((r) => !ADMIN_ONLY_ROUTES.has(r) || session?.role === 'admin'),
  )

  $effect(() => {
    const mq = matchMedia(NARROW_QUERY)
    const update = () => (narrow = mq.matches)
    mq.addEventListener('change', update)
    return () => mq.removeEventListener('change', update)
  })

  function sectionsFor(route: PageRoute) {
    return PAGE_SECTIONS[route] ?? []
  }

  function activeSection(route: PageRoute): string | null {
    const sections = sectionsFor(route)
    if (sections.length === 0) return null
    const current = getSection()
    return sections.some((s) => s.id === current) ? current : sections[0].id
  }

  function openSection(route: PageRoute, id: string) {
    if (routeType !== route) goTo(route)
    setSection(id)
  }

  function onDragStart(e: MouseEvent) {
    e.preventDefault()
    dragging = true
    document.addEventListener('mousemove', onDragMove)
    document.addEventListener('mouseup', onDragEnd)
  }

  function onDragMove(e: MouseEvent) {
    if (!panelEl) return
    const next = e.clientX - panelEl.getBoundingClientRect().left
    if (next < MIN_WIDTH / 2) {
      setPanelCollapsed(group.id, true)
      return
    }
    setPanelCollapsed(group.id, false)
    explorerWidth = Math.max(MIN_WIDTH, Math.min(MAX_WIDTH, next))
  }

  function onDragEnd() {
    dragging = false
    document.removeEventListener('mousemove', onDragMove)
    document.removeEventListener('mouseup', onDragEnd)
    localStorage.setItem(WIDTH_KEY, String(explorerWidth))
  }
</script>

<div
  bind:this={panelEl}
  class="flex h-full shrink-0 flex-col overflow-hidden border-r border-edge-subtle bg-sidebar transition-[width] {dragging ? 'duration-0' : 'duration-150'}"
  style="width: {width}px"
  aria-label={onWorkspace ? 'Schema explorer' : `${group.label} navigation`}
>
  {#if collapsed}
    <Button icon variant="ghost" size="sm" class="mx-auto mt-1.5" onclick={() => setPanelCollapsed(group.id, false)} title="Show panel (⌘B)" aria-label="Show panel">
      <PanelLeftOpen size={15} />
    </Button>
  {:else if onWorkspace}
    <div class="flex h-9 shrink-0 items-center justify-between pl-3 pr-1.5">
      <span class="text-[10px] font-medium uppercase tracking-[0.1em] text-fg-4">Explorer</span>
      <Button icon variant="ghost" size="xs" onclick={() => setPanelCollapsed(group.id, true)} title="Hide panel (⌘B)" aria-label="Hide panel">
        <PanelLeftClose size={14} />
      </Button>
    </div>
    <div class="min-h-0 flex-1 overflow-hidden border-t border-edge-subtle">
      <DatabaseTree onSelectTable={(database, table) => openTableTab(database, table)} />
    </div>
  {:else}
    <div class="flex h-12 shrink-0 items-center pl-4 pr-2">
      <span class="text-[13px] font-semibold tracking-[-0.01em] text-fg">{group.label}</span>
    </div>

    <nav class="flex min-h-0 flex-1 flex-col overflow-y-auto px-2 pb-2" aria-label={group.label}>
      <div class="space-y-px">
        {#each routes as route}
          {@const active = routeType === route}
          {@const meta = PAGE_ROUTES[route]}
          {@const sections = sectionsFor(route)}
          <button
            class="flex h-8 w-full items-center gap-2.5 rounded-md px-2 text-[13px] transition-colors {active ? 'bg-active font-medium text-fg' : 'text-fg-2 hover:bg-hover hover:text-fg'}"
            onclick={() => goTo(route)}
            aria-current={active ? 'page' : undefined}
          >
            <meta.icon size={15} strokeWidth={1.75} class="shrink-0 {active ? 'text-accent' : 'text-fg-3'}" />
            <span class="truncate">{meta.label}</span>
            {#if meta.pro && !licensedPro}
              <span class="ml-auto rounded-sm bg-surface-2 px-1 text-[9px] font-semibold uppercase tracking-wider text-fg-4">Pro</span>
            {/if}
          </button>
          {#if active && sections.length > 0}
            {@const current = activeSection(route)}
            <div class="relative my-1 ml-[17px] pl-3 before:absolute before:bottom-1 before:left-0 before:top-1 before:w-px before:bg-edge">
              {#each sections as s}
                <button
                  class="flex h-7 w-full items-center rounded-md px-2 text-[12.5px] transition-colors {current === s.id ? 'bg-hover font-medium text-fg' : 'text-fg-3 hover:bg-hover hover:text-fg'}"
                  onclick={() => openSection(route, s.id)}
                  aria-current={current === s.id ? 'true' : undefined}
                >
                  <span class="truncate">{s.label}</span>
                  {#if s.pro && !licensedPro}
                    <span class="ml-auto rounded-sm bg-surface-2 px-1 text-[9px] font-semibold uppercase tracking-wider text-fg-4">Pro</span>
                  {/if}
                </button>
              {/each}
            </div>
          {/if}
        {/each}
      </div>

      {#if group.id === 'settings'}
        <div class="mt-auto border-t border-edge-subtle pt-2">
          <a
            class="flex h-8 w-full items-center gap-2.5 rounded-md px-2 text-[13px] text-fg-2 transition-colors hover:bg-hover hover:text-fg"
            href="https://ch-ui.com/docs" target="_blank" rel="noopener noreferrer"
          >
            <BookOpen size={15} strokeWidth={1.75} class="shrink-0 text-fg-3" />
            <span class="truncate">Docs</span>
            <ExternalLink size={12} class="ml-auto text-fg-4" />
          </a>
        </div>
      {/if}
    </nav>
  {/if}
</div>

{#if !collapsed && onWorkspace}
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div
    class="w-1 shrink-0 cursor-col-resize transition-colors hover:bg-active {dragging ? 'bg-accent/60' : ''}"
    onmousedown={onDragStart}
  ></div>
{/if}

{#if dragging}
  <div class="fixed inset-0 z-50 cursor-col-resize"></div>
{/if}
