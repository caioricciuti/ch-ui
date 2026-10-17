<script lang="ts">
  import { onMount } from 'svelte'
  import {
    ArrowUpRight,
    BookOpen,
    Boxes,
    Home,
    Plug,
    Bot,
    Rocket,
    SquareTerminal,
    Table2,
    ExternalLink,
  } from 'lucide-svelte'
  import { getSession } from '../lib/stores/session.svelte'
  import { getTabs, openQueryTab, setActiveTab } from '../lib/stores/tabs.svelte'
  import { goTo } from '../lib/stores/router.svelte'
  import { setSection } from '../lib/stores/nav.svelte'
  import { hasProReadAccess } from '../lib/stores/license.svelte'
  import { listWorkspaceSavedQueries } from '../lib/api/workspace'
  import { runQuery } from '../lib/api/query'
  import { PAGE_ROUTES, ADMIN_ONLY_ROUTES, DOCS_ORIGIN, type PageRoute, type IconComponent } from '../lib/routes'
  import { prefetchPage } from '../lib/components/layout/pageLoaders'
  import EmptyState from '../lib/components/common/EmptyState.svelte'
  import Button from '../lib/components/common/Button.svelte'
  import type { Tab } from '../lib/stores/tabs.svelte'

  interface QuickLink {
    id: string
    title: string
    description: string
    icon: IconComponent
    /** Pro page without a license: shown with a badge, opens ProRequired. */
    locked?: boolean
    route?: PageRoute
    run: () => void
  }

  interface ResourceLink {
    id: string
    title: string
    description: string
    href: string
  }

  interface StarterQuery {
    id: string
    title: string
    description: string
    sql: string
    /** Cheap check run before opening; on failure the card shows `unavailable`. */
    probe?: string
    unavailable?: string
  }

  const session = $derived(getSession())
  const tabs = $derived(getTabs())
  const isAdmin = $derived(session?.role === 'admin')
  const proRead = $derived(hasProReadAccess())

  const recentTabs = $derived.by(() =>
    tabs.filter((tab) => tab.type !== 'home').slice(-8).reverse(),
  )

  // null until known; a failed load counts as unknown, not as "none".
  let savedQueryCount = $state<number | null>(null)
  onMount(() => {
    listWorkspaceSavedQueries().then(
      (list) => (savedQueryCount = list.length),
      () => (savedQueryCount = null),
    )
  })
  const firstRun = $derived(recentTabs.length === 0 && savedQueryCount === 0)

  // The pages Home links to, in order. Admin-only ones only for admins;
  // Pro ones always, with a badge when unlicensed (they open ProRequired).
  const HOME_ROUTES: PageRoute[] = ['saved-queries', 'dashboards', 'brain', 'models', 'pipelines', 'schedules', 'admin']

  const quickLinks = $derived<QuickLink[]>([
    {
      id: 'new-query',
      title: 'New Query',
      description: 'Write and run SQL',
      icon: SquareTerminal,
      run: () => openQueryTab(),
    },
    ...HOME_ROUTES
      .filter((r) => !ADMIN_ONLY_ROUTES.has(r) || isAdmin)
      .map((r) => ({
        id: r,
        title: PAGE_ROUTES[r].label,
        description: PAGE_ROUTES[r].description,
        icon: PAGE_ROUTES[r].icon,
        locked: !!PAGE_ROUTES[r].pro && !proRead,
        route: r,
        run: () => goTo(r),
      })),
  ])

  const starters: StarterQuery[] = [
    {
      id: 'largest-tables',
      title: 'Largest tables',
      description: 'Size and rows per table, from system.tables',
      sql: `SELECT
    database,
    name,
    formatReadableSize(total_bytes) AS size,
    total_rows
FROM system.tables
WHERE total_bytes > 0
  AND database NOT IN ('system', 'INFORMATION_SCHEMA', 'information_schema')
ORDER BY total_bytes DESC
LIMIT 20`,
    },
    {
      id: 'slowest-queries',
      title: 'Slowest queries today',
      description: 'The last 24 hours of system.query_log',
      probe: 'SELECT 1 FROM system.query_log LIMIT 0',
      unavailable:
        'Not available here: it needs read access to system.query_log, and query logging turned on in ClickHouse.',
      sql: `SELECT
    event_time,
    query_duration_ms,
    user,
    formatReadableSize(memory_usage) AS memory,
    substring(query, 1, 200) AS query
FROM system.query_log
WHERE type = 'QueryFinish'
  AND event_time > now() - INTERVAL 1 DAY
ORDER BY query_duration_ms DESC
LIMIT 20`,
    },
    {
      id: 'server-version',
      title: 'Server version and uptime',
      description: 'A first query that always works',
      sql: `SELECT
    version() AS version,
    uptime() AS uptime_seconds`,
    },
  ]

  // Starter id -> explanation shown when its probe failed.
  let starterNotes = $state<Record<string, string>>({})
  let probing = $state<string | null>(null)

  async function openStarter(q: StarterQuery): Promise<void> {
    if (q.probe) {
      probing = q.id
      try {
        await runQuery({ query: q.probe })
      } catch {
        starterNotes = { ...starterNotes, [q.id]: q.unavailable ?? 'This query cannot run on this connection.' }
        return
      } finally {
        probing = null
      }
    }
    openQueryTab(q.sql)
  }

  function openAdminSection(section: string): void {
    goTo('admin')
    setSection(section)
  }

  const resources: ResourceLink[] = [
    {
      id: 'chui-docs',
      title: 'CH-UI Docs',
      description: 'Guides for every page in this app',
      href: `${DOCS_ORIGIN}/docs/`,
    },
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

  /** The tab is still open: switch to it rather than opening a copy. */
  function openTab(tab: Tab): void {
    setActiveTab(tab.id)
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
          <div class="flex items-center gap-2">
            <div class="inline-flex items-center gap-1.5 rounded-full bg-accent-soft px-2.5 py-0.5 text-[11px] font-medium text-accent">
              <Home size={11} />
              Workspace Home
            </div>
            <a
              class="ds-badge border border-edge bg-transparent transition-colors hover:border-edge-strong hover:bg-hover hover:text-fg"
              href="{DOCS_ORIGIN}/docs/"
              target="_blank"
              rel="noreferrer"
              title="Open the docs"
            >
              Docs <ExternalLink size={11} />
            </a>
          </div>
          <h1 class="text-[20px] font-semibold text-fg">
            Welcome back{session?.user ? `, ${session.user}` : ''}
          </h1>
          <p class="text-[13px] text-fg-3">
            Start a new query, jump into saved work, or open tools quickly. Press
            <kbd class="rounded border border-edge bg-surface-2 px-1 font-mono text-[11px] text-fg-2">?</kbd>
            for keyboard shortcuts.
          </p>
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
          onpointerenter={() => item.route && prefetchPage(item.route)}
          onfocus={() => item.route && prefetchPage(item.route)}
        >
          <div class="flex items-center gap-3">
            <div class="inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-md bg-surface-2 text-accent">
              <item.icon size={15} />
            </div>
            <div class="min-w-0 flex-1">
              <p class="text-[13px] font-semibold text-fg">{item.title}</p>
              <p class="text-xs text-fg-3">{item.description}</p>
            </div>
            {#if item.locked}
              <span class="shrink-0 self-start rounded-sm bg-surface-2 px-1 text-[9px] font-semibold uppercase tracking-wider text-fg-4">Pro</span>
            {/if}
          </div>
        </button>
      {/each}
    </section>

    <section>
      <div class="mb-3 flex items-center gap-2">
        <BookOpen size={14} class="text-accent" />
        <h2 class="text-[13px] font-semibold text-fg">Resources</h2>
      </div>
      <div class="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-4 gap-3">
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

    {#if firstRun}
      <section class="rounded-lg border border-edge-subtle bg-surface">
        <EmptyState
          icon={Rocket}
          title="Run your first query"
          description="Nothing saved and no tabs open yet. Each of these opens in a new query tab, ready to run."
        >
          <div class="flex w-full max-w-3xl flex-col gap-4">
            <div class="grid grid-cols-1 gap-3 text-left md:grid-cols-3">
              {#each starters as q (q.id)}
                <button
                  class="rounded-md border border-edge-subtle bg-canvas px-3.5 py-3 text-left transition-colors hover:border-edge-strong hover:bg-hover"
                  disabled={probing === q.id}
                  onclick={() => openStarter(q)}
                >
                  <p class="flex items-center gap-1.5 text-[13px] font-semibold text-fg">
                    <SquareTerminal size={13} class="text-accent" />
                    {q.title}
                  </p>
                  <p class="mt-1 text-xs text-fg-3">{q.description}</p>
                  {#if starterNotes[q.id]}
                    <p class="mt-1.5 text-xs text-warning">{starterNotes[q.id]}</p>
                  {/if}
                </button>
              {/each}
            </div>
            {#if isAdmin}
              <div class="flex flex-wrap items-center justify-center gap-2">
                <Button variant="outline" size="sm" onclick={() => openAdminSection('tunnels')}>
                  <Plug size={13} />
                  Add a connection
                </Button>
                <Button variant="outline" size="sm" onclick={() => openAdminSection('mcp')}>
                  <Bot size={13} />
                  Connect an AI client (MCP)
                </Button>
              </div>
            {/if}
          </div>
        </EmptyState>
      </section>
    {:else}
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
    {/if}
  </div>
</div>
