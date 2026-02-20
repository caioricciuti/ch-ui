<script lang="ts">
  import {
    ArrowUpRight,
    BookOpen,
    Bookmark,
    Brain,
    Clock3,
    Home,
    LayoutDashboard,
    Shield,
    SquareTerminal,
    Table2,
  } from 'lucide-svelte'
  import { getSession } from '../lib/stores/session.svelte'
  import { getTabs, openDashboardTab, openQueryTab, openSingletonTab } from '../lib/stores/tabs.svelte'
  import type { Tab } from '../lib/stores/tabs.svelte'

  interface QuickLink {
    id: string
    title: string
    description: string
    icon: typeof SquareTerminal
    run: () => void
  }

  interface ResourceLink {
    id: string
    title: string
    description: string
    href: string
  }

  const session = $derived(getSession())
  const tabs = $derived(getTabs())

  const recentTabs = $derived.by(() =>
    tabs.filter((tab) => tab.type !== 'home').slice(-8).reverse(),
  )

  const quickLinks: QuickLink[] = [
    {
      id: 'new-query',
      title: 'New Query',
      description: 'Write and run SQL',
      icon: SquareTerminal,
      run: () => openQueryTab(),
    },
    {
      id: 'saved-queries',
      title: 'Saved Queries',
      description: 'Browse and manage saved SQL',
      icon: Bookmark,
      run: () => openSingletonTab('saved-queries', 'Saved Queries'),
    },
    {
      id: 'schedules',
      title: 'Schedules',
      description: 'Manage cron jobs and run history',
      icon: Clock3,
      run: () => openSingletonTab('schedules', 'Schedules'),
    },
    {
      id: 'dashboards',
      title: 'Dashboards',
      description: 'Open visual dashboards',
      icon: LayoutDashboard,
      run: () => openSingletonTab('dashboards', 'Dashboards'),
    },
    {
      id: 'brain',
      title: 'Brain',
      description: 'AI assistant for ClickHouse workflows',
      icon: Brain,
      run: () => openSingletonTab('brain', 'Brain'),
    },
    {
      id: 'admin',
      title: 'Admin',
      description: 'Users, alerts, and audit controls',
      icon: Shield,
      run: () => openSingletonTab('admin', 'Admin'),
    },
  ]

  const resources: ResourceLink[] = [
    {
      id: 'clickhouse-docs',
      title: 'ClickHouse Docs',
      description: 'Official docs and SQL reference',
      href: 'https://clickhouse.com/docs',
    },
    {
      id: 'project-releases',
      title: 'CH-UI Releases',
      description: 'Binary downloads and release notes',
      href: 'https://github.com/caioricciuti/ch-ui-cloud/releases',
    },
    {
      id: 'project-issues',
      title: 'Report Issue',
      description: 'Open bug reports and feature requests',
      href: 'https://github.com/caioricciuti/ch-ui-cloud/issues',
    },
  ]

  function openTab(tab: Tab): void {
    if (tab.type === 'query') {
      openQueryTab(tab.sql)
      return
    }
    if (tab.type === 'table') {
      openQueryTab(`SELECT *\nFROM \`${tab.database}\`.\`${tab.table}\`\nLIMIT 1000`)
      return
    }
    if (tab.type === 'database') {
      openQueryTab(`SHOW TABLES FROM \`${tab.database}\``)
      return
    }
    if (tab.type === 'dashboard') {
      openDashboardTab(tab.dashboardId, tab.name)
      return
    }
    if (tab.type === 'saved-queries') openSingletonTab('saved-queries', 'Saved Queries')
    if (tab.type === 'dashboards') openSingletonTab('dashboards', 'Dashboards')
    if (tab.type === 'schedules') openSingletonTab('schedules', 'Schedules')
    if (tab.type === 'brain') openSingletonTab('brain', 'Brain')
    if (tab.type === 'admin') openSingletonTab('admin', 'Admin')
    if (tab.type === 'settings') openSingletonTab('settings', 'License')
    if (tab.type === 'governance') openSingletonTab('governance', 'Governance')
  }

  function recentSubtitle(tab: Tab): string {
    switch (tab.type) {
      case 'query':
        return 'SQL query'
      case 'table':
        return `${tab.database}.${tab.table}`
      case 'database':
        return `${tab.database} database`
      case 'dashboard':
        return 'Dashboard'
      case 'saved-queries':
      case 'dashboards':
      case 'schedules':
      case 'brain':
      case 'admin':
      case 'settings':
      case 'governance':
        return tab.name
      default:
        return 'Workspace item'
    }
  }

  function recentPreview(tab: Tab): string {
    if (tab.type === 'query') {
      return tab.sql.trim().split('\n')[0] || 'Empty query'
    }
    if (tab.type === 'table') return `Open table ${tab.database}.${tab.table}`
    if (tab.type === 'database') return `Open database ${tab.database}`
    if (tab.type === 'dashboard') return `Dashboard ${tab.dashboardId}`
    return `Open ${tab.name}`
  }

  function recentIcon(tab: Tab): typeof SquareTerminal {
    if (tab.type === 'query') return SquareTerminal
    if (tab.type === 'table' || tab.type === 'database') return Table2
    if (tab.type === 'dashboard' || tab.type === 'dashboards') return LayoutDashboard
    if (tab.type === 'saved-queries') return Bookmark
    if (tab.type === 'schedules') return Clock3
    if (tab.type === 'brain') return Brain
    if (tab.type === 'admin' || tab.type === 'governance') return Shield
    return Home
  }
</script>

<div class="h-full overflow-auto bg-gradient-to-b from-transparent via-gray-100/20 to-gray-100/35 dark:from-transparent dark:via-gray-900/20 dark:to-gray-900/35">
  <div class="mx-auto w-full max-w-6xl p-5 lg:p-7 space-y-5">
    <section class="surface-card rounded-2xl border border-gray-200/80 dark:border-gray-800/80 p-5 lg:p-6">
      <div class="flex flex-col gap-3 md:flex-row md:items-center md:justify-between">
        <div class="space-y-1">
          <div class="inline-flex items-center gap-2 rounded-full border border-orange-300/45 dark:border-orange-700/45 bg-orange-100/60 dark:bg-orange-500/12 px-3 py-1 text-xs font-medium text-ch-orange">
            <Home size={12} />
            Workspace Home
          </div>
          <h1 class="text-xl font-semibold text-gray-900 dark:text-gray-100">
            Welcome back{session?.user ? `, ${session.user}` : ''}
          </h1>
          <p class="text-sm text-gray-500 dark:text-gray-400">Start a new query, jump into saved work, or open tools quickly.</p>
        </div>
        <button
          class="inline-flex items-center justify-center gap-2 rounded-lg bg-ch-orange px-4 py-2 text-sm font-semibold text-white hover:bg-orange-500 transition-colors"
          onclick={() => openQueryTab()}
        >
          <SquareTerminal size={15} />
          Run Query
        </button>
      </div>
    </section>

    <section class="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-3.5">
      {#each quickLinks as item (item.id)}
        <button
          class="surface-card rounded-xl border border-gray-200/70 dark:border-gray-800/70 px-4 py-3.5 text-left hover:border-ch-orange/45 hover:bg-orange-50/35 dark:hover:bg-orange-500/8 transition-colors"
          onclick={item.run}
        >
          <div class="flex items-center gap-2.5">
            <div class="inline-flex h-8 w-8 items-center justify-center rounded-lg bg-gray-100 dark:bg-gray-900 text-ch-orange">
              <item.icon size={15} />
            </div>
            <div>
              <p class="text-sm font-semibold text-gray-900 dark:text-gray-100">{item.title}</p>
              <p class="text-xs text-gray-500 dark:text-gray-400">{item.description}</p>
            </div>
          </div>
        </button>
      {/each}
    </section>

    <section class="surface-card rounded-2xl border border-gray-200/80 dark:border-gray-800/80 p-4">
      <div class="flex items-center gap-2 mb-3">
        <BookOpen size={14} class="text-ch-orange" />
        <h2 class="text-sm font-semibold text-gray-900 dark:text-gray-100">Resources</h2>
      </div>
      <div class="grid grid-cols-1 md:grid-cols-3 gap-3">
        {#each resources as resource (resource.id)}
          <a
            class="rounded-xl border border-gray-200/70 dark:border-gray-800/70 px-3.5 py-3 hover:border-ch-blue/45 hover:bg-ch-blue/5 transition-colors"
            href={resource.href}
            target="_blank"
            rel="noopener noreferrer"
          >
            <p class="flex items-center gap-1.5 text-sm font-semibold text-gray-900 dark:text-gray-100">
              {resource.title}
              <ArrowUpRight size={13} class="text-gray-400" />
            </p>
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{resource.description}</p>
          </a>
        {/each}
      </div>
    </section>

    <section class="space-y-3">
      <div class="flex items-center justify-between">
        <h2 class="text-sm font-semibold text-gray-900 dark:text-gray-100">Recently Opened</h2>
      </div>
      {#if recentTabs.length === 0}
        <div class="surface-card rounded-xl border border-dashed border-gray-300 dark:border-gray-700 px-4 py-8 text-center">
          <p class="text-sm text-gray-500 dark:text-gray-400">No recent workspace items yet.</p>
          <button
            class="mt-3 inline-flex items-center gap-2 rounded-lg border border-gray-300 dark:border-gray-700 px-3 py-1.5 text-xs font-semibold text-gray-700 dark:text-gray-200 hover:border-ch-orange hover:text-ch-orange transition-colors"
            onclick={() => openQueryTab()}
          >
            <SquareTerminal size={13} />
            Create first query
          </button>
        </div>
      {:else}
        <div class="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-3">
          {#each recentTabs as tab (tab.id)}
            {@const Icon = recentIcon(tab)}
            <button
              class="surface-card rounded-xl border border-gray-200/75 dark:border-gray-800/75 px-4 py-3 text-left hover:border-ch-orange/45 hover:bg-orange-50/30 dark:hover:bg-orange-500/8 transition-colors"
              onclick={() => openTab(tab)}
            >
              <div class="flex items-center gap-2 text-xs text-gray-500 dark:text-gray-400">
                <Icon size={13} />
                <span>{recentSubtitle(tab)}</span>
              </div>
              <p class="mt-2 text-sm font-semibold text-gray-900 dark:text-gray-100 truncate">{tab.name}</p>
              <p class="mt-1 text-xs text-gray-500 dark:text-gray-400 truncate">{recentPreview(tab)}</p>
            </button>
          {/each}
        </div>
      {/if}
    </section>
  </div>
</div>
