<script lang="ts">
  import {
    ArrowUpRight,
    BookOpen,
    Bookmark,
    Brain,
    Clock3,
    Boxes,
    Home,
    LayoutDashboard,
    Shield,
    SquareTerminal,
    Table2,
    Workflow,
  } from 'lucide-svelte'
  import { getSession } from '../lib/stores/session.svelte'
  import { getTabs, openQueryTab } from '../lib/stores/tabs.svelte'
  import { goTo } from '../lib/stores/router.svelte'
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
      run: () => goTo('saved-queries', 'Saved Queries'),
    },
    {
      id: 'schedules',
      title: 'Schedules',
      description: 'Manage cron jobs and run history',
      icon: Clock3,
      run: () => goTo('schedules', 'Schedules'),
    },
    {
      id: 'dashboards',
      title: 'Dashboards',
      description: 'Open visual dashboards',
      icon: LayoutDashboard,
      run: () => goTo('dashboards', 'Dashboards'),
    },
    {
      id: 'brain',
      title: 'Brain',
      description: 'AI assistant for ClickHouse workflows',
      icon: Brain,
      run: () => goTo('brain', 'Brain'),
    },
    {
      id: 'pipelines',
      title: 'Pipelines',
      description: 'Visual data pipeline builder',
      icon: Workflow,
      run: () => goTo('pipelines', 'Pipelines'),
    },
    {
      id: 'admin',
      title: 'Admin',
      description: 'Users, alerts, and audit controls',
      icon: Shield,
      run: () => goTo('admin', 'Admin'),
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
      href: 'https://github.com/caioricciuti/ch-ui/releases',
    },
    {
      id: 'project-issues',
      title: 'Report Issue',
      description: 'Open bug reports and feature requests',
      href: 'https://github.com/caioricciuti/ch-ui/issues',
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
  }

  function recentSubtitle(tab: Tab): string {
    switch (tab.type) {
      case 'query':
        return 'SQL query'
      case 'table':
        return `${tab.database}.${tab.table}`
      case 'database':
        return `${tab.database} database`
      case 'model':
        return 'Model'
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
    return `Open ${tab.name}`
  }

  function recentIcon(tab: Tab): typeof SquareTerminal {
    if (tab.type === 'query') return SquareTerminal
    if (tab.type === 'table' || tab.type === 'database') return Table2
    if (tab.type === 'model') return Boxes
    return Home
  }
</script>

<div class="h-full overflow-auto bg-canvas">
  <div class="mx-auto w-full max-w-6xl p-5 lg:p-7 space-y-6">
    <!-- Hero: one surface, quiet border, one primary action -->
    <section class="rounded-lg border border-edge-subtle bg-surface p-5 lg:p-6">
      <div class="flex flex-col gap-3 md:flex-row md:items-center md:justify-between">
        <div class="space-y-1">
          <div class="inline-flex items-center gap-1.5 rounded-full bg-accent-soft px-2.5 py-0.5 text-[11px] font-medium text-accent">
            <Home size={11} />
            Workspace Home
          </div>
          <h1 class="text-[20px] font-semibold text-fg">
            Welcome back{session?.user ? `, ${session.user}` : ''}
          </h1>
          <p class="text-[13px] text-fg-3">Start a new query, jump into saved work, or open tools quickly.</p>
        </div>
        <button
          class="inline-flex h-8 items-center justify-center gap-2 rounded-md bg-accent px-3.5 text-[13px] font-medium text-accent-fg transition-[filter] hover:brightness-110"
          onclick={() => openQueryTab()}
        >
          <SquareTerminal size={14} />
          Run Query
        </button>
      </div>
    </section>

    <section class="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-3">
      {#each quickLinks as item (item.id)}
        <button
          class="rounded-md border border-edge-subtle bg-surface px-4 py-3.5 text-left transition-colors hover:border-edge-strong hover:bg-hover"
          onclick={item.run}
        >
          <div class="flex items-center gap-3">
            <div class="inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-md bg-surface-2 text-accent">
              <item.icon size={15} />
            </div>
            <div class="min-w-0">
              <p class="text-[13px] font-semibold text-fg">{item.title}</p>
              <p class="text-xs text-fg-3">{item.description}</p>
            </div>
          </div>
        </button>
      {/each}
    </section>

    <section>
      <div class="mb-3 flex items-center gap-2">
        <BookOpen size={14} class="text-accent" />
        <h2 class="text-[13px] font-semibold text-fg">Resources</h2>
      </div>
      <div class="grid grid-cols-1 md:grid-cols-3 gap-3">
        {#each resources as resource (resource.id)}
          <a
            class="rounded-md border border-edge-subtle bg-surface px-3.5 py-3 transition-colors hover:border-edge-strong hover:bg-hover"
            href={resource.href}
            target="_blank"
            rel="noopener noreferrer"
          >
            <p class="flex items-center gap-1.5 text-[13px] font-semibold text-fg">
              {resource.title}
              <ArrowUpRight size={13} class="text-fg-4" />
            </p>
            <p class="mt-1 text-xs text-fg-3">{resource.description}</p>
          </a>
        {/each}
      </div>
    </section>

    <section class="space-y-3">
      <h2 class="text-[13px] font-semibold text-fg">Recently opened</h2>
      {#if recentTabs.length === 0}
        <div class="rounded-md border border-dashed border-edge px-4 py-10 text-center">
          <p class="text-[13px] text-fg-3">No recent workspace items yet.</p>
          <button
            class="mt-4 inline-flex h-7 items-center gap-1.5 rounded-md border border-edge px-3 text-xs font-medium text-fg-2 transition-colors hover:bg-hover hover:text-fg"
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
              class="rounded-md border border-edge-subtle bg-surface px-4 py-3 text-left transition-colors hover:border-edge-strong hover:bg-hover"
              onclick={() => openTab(tab)}
            >
              <div class="flex items-center gap-2 text-xs text-fg-3">
                <Icon size={13} />
                <span>{recentSubtitle(tab)}</span>
              </div>
              <p class="mt-2 truncate text-[13px] font-semibold text-fg">{tab.name}</p>
              <p class="mt-1 truncate text-xs text-fg-3">{recentPreview(tab)}</p>
            </button>
          {/each}
        </div>
      {/if}
    </section>
  </div>
</div>
