<script lang="ts">
  import { tick } from 'svelte'
  import { Search, Plus, Table2, Sparkles, LayoutDashboard, Bookmark, Clock, Brain, Shield, Settings, Moon, Sun, LogOut, SquareTerminal, Home } from 'lucide-svelte'
  import { closeCommandPalette, isCommandPaletteOpen } from '../../stores/command-palette.svelte'
  import { openQueryTab, openSingletonTab, openTableTab, openDashboardTab, getTabs, openHomeTab } from '../../stores/tabs.svelte'
  import { getDatabases, loadDatabases } from '../../stores/schema.svelte'
  import { getSession, logout } from '../../stores/session.svelte'
  import { getTheme, toggleTheme } from '../../stores/theme.svelte'

  interface CommandItem {
    id: string
    title: string
    subtitle: string
    keywords: string
    icon: typeof Search
    run: () => void
  }

  let inputEl: HTMLInputElement | undefined = $state()
  let query = $state('')
  let selectedIdx = $state(0)

  const open = $derived(isCommandPaletteOpen())
  const tabs = $derived(getTabs())
  const databases = $derived(getDatabases())
  const session = $derived(getSession())

  function sequentialMatchScore(text: string, term: string): number {
    if (!term) return 1
    let ti = 0
    let score = 0
    const lowerText = text.toLowerCase()
    const lowerTerm = term.toLowerCase()

    for (let i = 0; i < lowerText.length && ti < lowerTerm.length; i++) {
      if (lowerText[i] === lowerTerm[ti]) {
        score += i > 0 && lowerText[i - 1] === ' ' ? 5 : 2
        ti++
      }
    }

    if (ti !== lowerTerm.length) return -1
    if (lowerText.startsWith(lowerTerm)) score += 25
    if (lowerText.includes(` ${lowerTerm}`)) score += 10
    return score
  }

  const actions = $derived.by(() => {
    const items: CommandItem[] = [
      {
        id: 'action-home',
        title: 'Home',
        subtitle: 'Navigate',
        keywords: 'home workspace start',
        icon: Home,
        run: () => openHomeTab(),
      },
      {
        id: 'action-new-query',
        title: 'New Query',
        subtitle: 'Workspace action',
        keywords: 'new query sql run editor',
        icon: Plus,
        run: () => openQueryTab(),
      },
      {
        id: 'action-saved',
        title: 'Saved Queries',
        subtitle: 'Navigate',
        keywords: 'saved queries history bookmarks',
        icon: Bookmark,
        run: () => openSingletonTab('saved-queries', 'Saved Queries'),
      },
      {
        id: 'action-dashboards',
        title: 'Dashboards',
        subtitle: 'Navigate',
        keywords: 'charts panels dashboards metrics',
        icon: LayoutDashboard,
        run: () => openSingletonTab('dashboards', 'Dashboards'),
      },
      {
        id: 'action-schedules',
        title: 'Schedules',
        subtitle: 'Navigate',
        keywords: 'cron scheduled jobs runs',
        icon: Clock,
        run: () => openSingletonTab('schedules', 'Schedules'),
      },
      {
        id: 'action-brain',
        title: 'Brain AI',
        subtitle: 'Navigate',
        keywords: 'brain ai assistant sql helper',
        icon: Brain,
        run: () => openSingletonTab('brain', 'Brain'),
      },
      {
        id: 'action-admin',
        title: 'Admin',
        subtitle: 'Navigate',
        keywords: 'admin users audit logs query log',
        icon: Shield,
        run: () => openSingletonTab('admin', 'Admin'),
      },
      {
        id: 'action-settings',
        title: 'License',
        subtitle: 'Navigate',
        keywords: 'license settings config entitlements',
        icon: Settings,
        run: () => openSingletonTab('settings', 'License'),
      },
      {
        id: 'action-theme',
        title: getTheme() === 'dark' ? 'Switch to Light Theme' : 'Switch to Dark Theme',
        subtitle: 'Appearance',
        keywords: 'theme dark light appearance',
        icon: getTheme() === 'dark' ? Sun : Moon,
        run: () => toggleTheme(),
      },
    ]

    if (session) {
      items.push({
        id: 'action-logout',
        title: 'Logout',
        subtitle: 'Session',
        keywords: 'logout sign out session',
        icon: LogOut,
        run: () => logout(),
      })
    }

    for (const tab of tabs.filter((entry) => entry.type !== 'home').slice(-8).reverse()) {
      const icon = tab.type === 'query'
        ? SquareTerminal
        : (tab.type === 'table'
            ? Table2
            : (tab.type === 'dashboard' ? LayoutDashboard : Sparkles))
      items.push({
        id: `tab-${tab.id}`,
        title: `Open ${tab.name}`,
        subtitle: 'Recent tab',
        keywords: `${tab.name} tab ${tab.type}`,
        icon,
        run: () => {
          if (tab.type === 'table') openTableTab(tab.database, tab.table)
          if (tab.type === 'saved-queries') openSingletonTab('saved-queries', 'Saved Queries')
          if (tab.type === 'dashboard') openDashboardTab(tab.dashboardId, tab.name)
          if (tab.type === 'dashboards') openSingletonTab('dashboards', 'Dashboards')
          if (tab.type === 'schedules') openSingletonTab('schedules', 'Schedules')
          if (tab.type === 'brain') openSingletonTab('brain', 'Brain')
          if (tab.type === 'admin') openSingletonTab('admin', 'Admin')
          if (tab.type === 'settings') openSingletonTab('settings', 'License')
          if (tab.type === 'governance') openSingletonTab('governance', 'Governance')
          if (tab.type === 'query') openQueryTab(tab.sql)
        },
      })
    }

    for (const db of databases.slice(0, 12)) {
      if (!db.tables) continue
      for (const t of db.tables.slice(0, 12)) {
        items.push({
          id: `table-${db.name}.${t.name}`,
          title: `${db.name}.${t.name}`,
          subtitle: 'Open table',
          keywords: `${db.name} ${t.name} table schema`,
          icon: Table2,
          run: () => openTableTab(db.name, t.name),
        })
      }
    }

    return items
  })

  const filtered = $derived.by(() => {
    const term = query.trim().toLowerCase()
    const ranked = actions
      .map((item) => {
        const text = `${item.title} ${item.subtitle} ${item.keywords}`
        return { item, score: sequentialMatchScore(text, term) }
      })
      .filter((x) => x.score >= 0)
      .sort((a, b) => b.score - a.score)
      .slice(0, 28)
      .map((x) => x.item)

    return ranked
  })

  async function runCommand(item: CommandItem) {
    item.run()
    closeCommandPalette()
    query = ''
    selectedIdx = 0
  }

  function handleKeydown(e: KeyboardEvent) {
    if (!open) return

    if (e.key === 'Escape') {
      e.preventDefault()
      closeCommandPalette()
      return
    }

    if (e.key === 'ArrowDown') {
      e.preventDefault()
      selectedIdx = Math.min(filtered.length - 1, selectedIdx + 1)
      return
    }

    if (e.key === 'ArrowUp') {
      e.preventDefault()
      selectedIdx = Math.max(0, selectedIdx - 1)
      return
    }

    if (e.key === 'Enter' && filtered[selectedIdx]) {
      e.preventDefault()
      runCommand(filtered[selectedIdx])
    }
  }

  $effect(() => {
    if (!open) return

    if (databases.length === 0) {
      loadDatabases()
    }

    query = ''
    selectedIdx = 0

    tick().then(() => inputEl?.focus())
  })
</script>

<svelte:window onkeydown={handleKeydown} />

{#if open}
  <button
    type="button"
    class="fixed inset-0 z-[80] bg-gray-950/45 backdrop-blur-sm"
    aria-label="Close command palette"
    onclick={() => closeCommandPalette()}
  ></button>
  <div class="fixed inset-0 z-[81] flex items-start justify-center pt-[12vh] px-4">
    <div class="surface-card w-full max-w-2xl rounded-2xl overflow-hidden">
      <div class="flex items-center gap-2 px-3 py-2.5 border-b border-gray-200/80 dark:border-gray-800/80">
        <Search size={14} class="text-gray-500" />
        <input
          bind:this={inputEl}
          bind:value={query}
          type="text"
          placeholder="Search actions, tables, tabs..."
          class="w-full bg-transparent text-sm text-gray-800 dark:text-gray-200 placeholder:text-gray-400 outline-none"
        />
        <span class="text-[10px] text-gray-400 px-2 py-1 rounded border border-gray-300/70 dark:border-gray-700/80">ESC</span>
      </div>

      <div class="max-h-[55vh] overflow-y-auto p-1.5">
        {#if filtered.length === 0}
          <div class="px-3 py-8 text-center text-sm text-gray-500">No command found</div>
        {:else}
          {#each filtered as item, idx (item.id)}
            <button
              class="w-full flex items-center gap-3 px-3 py-2 rounded-lg text-left transition-colors {idx === selectedIdx ? 'bg-ch-blue/10 text-ch-blue' : 'hover:bg-gray-200/55 dark:hover:bg-gray-800/60 text-gray-700 dark:text-gray-300'}"
              onclick={() => runCommand(item)}
              onmouseenter={() => selectedIdx = idx}
            >
              <item.icon size={15} class={idx === selectedIdx ? 'text-ch-blue' : 'text-gray-500'} />
              <span class="flex-1 min-w-0">
                <span class="block text-sm font-medium truncate">{item.title}</span>
                <span class="block text-[11px] text-gray-500 dark:text-gray-400 truncate">{item.subtitle}</span>
              </span>
              {#if idx === selectedIdx}
                <span class="text-[10px] text-gray-500 px-2 py-1 rounded border border-gray-300/70 dark:border-gray-700/80">ENTER</span>
              {/if}
            </button>
          {/each}
        {/if}
      </div>

      <div class="px-3 py-2 border-t border-gray-200/80 dark:border-gray-800/80 text-[11px] text-gray-500 dark:text-gray-400">
        Use <span class="font-medium">↑ ↓</span> to navigate, <span class="font-medium">Enter</span> to run.
      </div>
    </div>
  </div>
{/if}
