<script lang="ts">
  import { onMount } from 'svelte'
  import { getActiveTab, openQueryTab, openSingletonTab, openTableTab } from '../../stores/tabs.svelte'
  import type { SingletonTab } from '../../stores/tabs.svelte'
  import { getSession, logout } from '../../stores/session.svelte'
  import { toggleTheme, getTheme } from '../../stores/theme.svelte'
  import { openCommandPalette } from '../../stores/command-palette.svelte'
  import { isProActive, loadLicense } from '../../stores/license.svelte'
  import DatabaseTree from '../explorer/DatabaseTree.svelte'
  import logo from '../../../assets/logo.png'
  import {
    Plus,
    Bookmark,
    LayoutDashboard,
    Clock,
    Brain,
    Shield,
    Scale,
    Settings,
    PanelLeftClose,
    PanelLeftOpen,
    Database,
    Sun,
    Moon,
    LogOut,
    Search,
    ChevronDown,
    ExternalLink,
  } from 'lucide-svelte'

  const session = $derived(getSession())

  interface NavItemInternal {
    type: SingletonTab['type']
    label: string
    icon: typeof Bookmark
    pro?: boolean
  }
  interface NavItemExternal {
    type: 'external'
    label: string
    icon: typeof Bookmark
    href: string
  }
  type NavItem = NavItemInternal | NavItemExternal

  const navItems: NavItem[] = [
    { type: 'saved-queries', label: 'Saved Queries', icon: Bookmark },
    { type: 'dashboards', label: 'Dashboards', icon: LayoutDashboard },
    { type: 'schedules', label: 'Schedules', icon: Clock, pro: true },
    { type: 'brain', label: 'Brain', icon: Brain },
    { type: 'governance', label: 'Governance', icon: Scale, pro: true },
    { type: 'admin', label: 'Admin', icon: Shield },
    { type: 'settings', label: 'License', icon: Settings },
    { type: 'external', label: 'CH-UI Docs', icon: ExternalLink, href: 'https://ch-ui.com/docs' },
  ]

  const MIN_WIDTH = 200
  const MAX_WIDTH = 500
  const COLLAPSE_THRESHOLD = 120
  const COLLAPSED_WIDTH = 40
  const DEFAULT_WIDTH = 244

  // Restore from localStorage
  const savedCollapsed = localStorage.getItem('ch-ui-sidebar-collapsed') === 'true'
  const savedWidth = parseInt(localStorage.getItem('ch-ui-sidebar-width') ?? String(DEFAULT_WIDTH), 10)
  const savedMenuCollapsed = localStorage.getItem('ch-ui-sidebar-menu-collapsed') === 'true'

  let collapsed = $state(savedCollapsed)
  let sidebarWidth = $state(isNaN(savedWidth) ? DEFAULT_WIDTH : savedWidth)
  let menuCollapsed = $state(savedMenuCollapsed)
  let dragging = $state(false)
  const licensedPro = $derived(isProActive())
  const activeTab = $derived(getActiveTab())

  onMount(() => {
    loadLicense()
    return () => {
      document.removeEventListener('mousemove', onDragMove)
      document.removeEventListener('mouseup', onDragEnd)
    }
  })

  function toggleCollapse() {
    collapsed = !collapsed
    localStorage.setItem('ch-ui-sidebar-collapsed', String(collapsed))
  }

  function toggleMenuCollapsed() {
    menuCollapsed = !menuCollapsed
    localStorage.setItem('ch-ui-sidebar-menu-collapsed', String(menuCollapsed))
  }

  function handleSelectTable(database: string, table: string) {
    openTableTab(database, table)
  }

  function isNavItemActive(type: SingletonTab['type']): boolean {
    if (!activeTab) return false
    if (type === 'dashboards') return activeTab.type === 'dashboards' || activeTab.type === 'dashboard'
    return activeTab.type === type
  }

  // ── Drag handle logic ──
  function onDragStart(e: MouseEvent) {
    e.preventDefault()
    dragging = true
    document.addEventListener('mousemove', onDragMove)
    document.addEventListener('mouseup', onDragEnd)
  }

  function onDragMove(e: MouseEvent) {
    const newWidth = e.clientX
    if (newWidth < COLLAPSE_THRESHOLD) {
      collapsed = true
      localStorage.setItem('ch-ui-sidebar-collapsed', 'true')
    } else {
      collapsed = false
      localStorage.setItem('ch-ui-sidebar-collapsed', 'false')
      sidebarWidth = Math.max(MIN_WIDTH, Math.min(MAX_WIDTH, newWidth))
    }
  }

  function onDragEnd() {
    dragging = false
    document.removeEventListener('mousemove', onDragMove)
    document.removeEventListener('mouseup', onDragEnd)
    localStorage.setItem('ch-ui-sidebar-width', String(sidebarWidth))
  }
</script>

<div class="flex shrink-0 h-full">
  <nav
    class="flex flex-col border-r border-gray-200/80 dark:border-gray-800/80 bg-white dark:bg-gray-950 overflow-hidden transition-[width] {dragging ? 'duration-0' : 'duration-200'}"
    style="width: {collapsed ? COLLAPSED_WIDTH : sidebarWidth}px"
  >
    <!-- Collapse toggle + New query -->
    <div class="flex items-center {collapsed ? 'justify-center' : 'justify-between'} px-2 py-2 border-b border-gray-200 dark:border-gray-800 shrink-0">
      {#if !collapsed}
        <div class="flex items-center gap-1.5">
          <img src={logo} alt="CH-UI" class="w-6 h-6 rounded-md ring-1 ring-white/20" />
          <button
            class="flex items-center gap-2 px-3 py-2 text-[13px] font-semibold text-gray-700 dark:text-gray-300 bg-gray-200/60 dark:bg-gray-800/60 hover:bg-gray-200 dark:hover:bg-gray-800 rounded-md transition-colors"
            onclick={() => openQueryTab()}
          >
            <Plus size={15} />
            New Query
          </button>
        </div>
      {/if}
      <button
        class="p-1 rounded text-gray-500 hover:text-gray-700 dark:hover:text-gray-300 hover:bg-gray-200 dark:hover:bg-gray-800/60 transition-colors"
        onclick={toggleCollapse}
        title={collapsed ? 'Expand sidebar' : 'Collapse sidebar'}
      >
        {#if collapsed}
          <PanelLeftOpen size={16} />
        {:else}
          <PanelLeftClose size={16} />
        {/if}
      </button>
    </div>

    {#if collapsed}
      <!-- Collapsed: icon-only buttons -->
      <div class="flex flex-col items-center py-2 gap-1">
        <button
          class="p-2 rounded text-gray-500 dark:text-gray-400 hover:text-gray-800 dark:hover:text-gray-200 hover:bg-gray-200/40 dark:hover:bg-gray-800/40"
          onclick={() => openQueryTab()}
          title="New Query"
        >
          <Plus size={17} />
        </button>
      </div>
      <div class="flex-1"></div>
      <div class="flex flex-col items-center py-2 gap-1 border-t border-gray-200 dark:border-gray-800">
        {#each navItems as item}
          {#if item.type === 'external'}
            <a
              class="p-2 rounded transition-colors text-gray-500 dark:text-gray-400 hover:text-gray-800 dark:hover:text-gray-200 hover:bg-gray-200/40 dark:hover:bg-gray-800/40"
              href={item.href}
              target="_blank"
              rel="noopener noreferrer"
              title={item.label}
            >
              <item.icon size={17} />
            </a>
          {:else}
            <button
              class="p-2 rounded transition-colors
                {isNavItemActive(item.type)
                  ? 'text-ch-orange bg-orange-100/70 dark:bg-orange-500/18'
                  : 'text-gray-500 dark:text-gray-400 hover:text-gray-800 dark:hover:text-gray-200 hover:bg-gray-200/40 dark:hover:bg-gray-800/40'}"
              onclick={() => openSingletonTab(item.type, item.label)}
              title={item.label}
            >
              <item.icon size={17} />
            </button>
          {/if}
        {/each}
      </div>
      <!-- Connection status + actions (collapsed) -->
      <div class="flex flex-col items-center py-2 gap-1 border-t border-gray-200 dark:border-gray-800">
        {#if session}
          <span
            class="w-2 h-2 rounded-full my-1 {session.connectionOnline ? 'bg-green-500' : 'bg-red-500'}"
            title={session.connectionOnline ? 'Connected' : 'Disconnected'}
          ></span>
        {/if}
        <button
          class="p-2 rounded text-gray-500 dark:text-gray-400 hover:text-gray-800 dark:hover:text-gray-200 hover:bg-gray-200/40 dark:hover:bg-gray-800/40"
          onclick={toggleTheme}
          title="Toggle theme"
        >
          {#if getTheme() === 'dark'}
            <Sun size={17} />
          {:else}
            <Moon size={17} />
          {/if}
        </button>
        {#if session}
          <button
            class="p-2 rounded text-gray-500 dark:text-gray-400 hover:text-red-400 hover:bg-gray-200/40 dark:hover:bg-gray-800/40"
            onclick={logout}
            title="Logout"
          >
            <LogOut size={17} />
          </button>
        {/if}
      </div>
    {:else}
      <!-- Expanded: full sidebar -->
      <div class="px-2.5 py-1.5 border-b border-gray-200 dark:border-gray-800">
        <button
          class="w-full flex items-center gap-2 px-3 py-2 text-[12px] font-medium rounded-md border border-gray-300/70 dark:border-gray-700/70 text-gray-600 dark:text-gray-300 bg-gray-100/60 dark:bg-gray-900/55 hover:border-ch-blue/50 hover:text-ch-blue transition-colors"
          onclick={openCommandPalette}
          title="Open command palette"
        >
          <Search size={14} />
          <span class="flex-1 text-left">Command Menu</span>
          <kbd class="text-[10px] px-1.5 py-0.5 rounded border border-gray-300 dark:border-gray-700 text-gray-500">⌘K / ⌥K</kbd>
        </button>
      </div>
      <div class="flex-1 min-h-0 flex flex-col">
        <div class="flex-1 min-h-0 overflow-auto border-b border-gray-200 dark:border-gray-800">
          <DatabaseTree onSelectTable={handleSelectTable} />
        </div>

        <div class="shrink-0 border-t border-gray-200 dark:border-gray-800">
          <button
            class="w-full flex items-center gap-2 px-3.5 py-2 text-[12px] font-medium text-gray-500 dark:text-gray-400 hover:text-gray-800 dark:hover:text-gray-200 hover:bg-gray-100/70 dark:hover:bg-gray-900/60 transition-colors"
            onclick={toggleMenuCollapsed}
            title={menuCollapsed ? 'Expand menu section' : 'Collapse menu section'}
            aria-expanded={!menuCollapsed}
          >
            <span class="uppercase tracking-wide">Menu</span>
            <ChevronDown size={14} class="ml-auto transition-transform {menuCollapsed ? '' : 'rotate-180'}" />
          </button>

          {#if !menuCollapsed}
            <div class="py-1.5">
              {#each navItems as item}
                {#if item.type === 'external'}
                  <a
                    class="flex items-center gap-2.5 w-full px-3.5 py-2 text-[13px] font-medium transition-colors text-gray-500 dark:text-gray-400 hover:text-gray-800 dark:hover:text-gray-200 hover:bg-gray-200/40 dark:hover:bg-gray-800/40"
                    href={item.href}
                    target="_blank"
                    rel="noopener noreferrer"
                  >
                    <item.icon size={15} />
                    <span class="truncate">{item.label}</span>
                  </a>
                {:else}
                  <button
                    class="flex items-center gap-2.5 w-full px-3.5 py-2 text-[13px] font-medium transition-colors
                      {isNavItemActive(item.type)
                        ? 'text-ch-orange bg-orange-100/70 dark:bg-orange-500/18 border-l-2 border-ch-orange'
                        : 'text-gray-500 dark:text-gray-400 hover:text-gray-800 dark:hover:text-gray-200 hover:bg-gray-200/40 dark:hover:bg-gray-800/40'}"
                    onclick={() => openSingletonTab(item.type, item.label)}
                  >
                    <item.icon size={15} />
                    <span class="truncate">{item.label}</span>
                    {#if item.pro && !licensedPro}
                      <span class="ml-auto text-[10px] uppercase tracking-wider text-ch-orange font-semibold">Pro</span>
                    {/if}
                  </button>
                {/if}
              {/each}
            </div>

            <!-- Connection info + actions (expanded) -->
            <div class="px-3.5 py-2.5 border-t border-gray-200 dark:border-gray-800 shrink-0">
              {#if session}
                <div class="flex items-center gap-2 mb-1.5">
                  <Database size={14} class="text-ch-blue shrink-0" />
                  <span class="text-[13px] font-semibold text-gray-700 dark:text-gray-300 truncate">{session.connectionName ?? 'CH-UI'}</span>
                  <span
                    class="w-1.5 h-1.5 rounded-full shrink-0 {session.connectionOnline ? 'bg-green-500' : 'bg-red-500'}"
                    title={session.connectionOnline ? 'Connected' : 'Disconnected'}
                  ></span>
                  <span class="text-[10px] text-gray-400 truncate">{session.user}</span>
                </div>
              {/if}
              <div class="flex items-center gap-1">
                {#if session?.appVersion}
                  <span class="text-[10px] text-gray-400 dark:text-gray-600 mr-auto">ch-ui {session.appVersion}</span>
                {/if}
                <div class="flex items-center gap-0.5 ml-auto">
                  <button
                    class="p-1 rounded text-gray-500 dark:text-gray-400 hover:text-gray-800 dark:hover:text-gray-200 hover:bg-gray-200/40 dark:hover:bg-gray-800/40 transition-colors"
                    onclick={toggleTheme}
                    title="Toggle theme"
                  >
                    {#if getTheme() === 'dark'}
                      <Sun size={14} />
                    {:else}
                      <Moon size={14} />
                    {/if}
                  </button>
                  {#if session}
                    <button
                      class="p-1 rounded text-gray-500 dark:text-gray-400 hover:text-red-400 hover:bg-gray-200/40 dark:hover:bg-gray-800/40 transition-colors"
                      onclick={logout}
                      title="Logout"
                    >
                      <LogOut size={14} />
                    </button>
                  {/if}
                </div>
              </div>
            </div>
          {/if}
        </div>
      </div>
    {/if}
  </nav>

  <!-- Drag handle (horizontal width) -->
  {#if !collapsed}
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div
      class="w-1 shrink-0 cursor-col-resize group flex items-center justify-center hover:bg-ch-blue/20 transition-colors {dragging ? 'bg-ch-blue/30' : ''}"
      onmousedown={onDragStart}
    >
      <div class="h-8 w-0.5 rounded-full {dragging ? 'bg-ch-blue' : 'bg-gray-300 dark:bg-gray-700 group-hover:bg-ch-blue/60'} transition-colors"></div>
    </div>
  {/if}
</div>

<!-- Drag overlay -->
{#if dragging}
  <div class="fixed inset-0 z-50 cursor-col-resize"></div>
{/if}
