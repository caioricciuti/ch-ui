<script lang="ts">
  import { onMount } from 'svelte'
  import Button from '../common/Button.svelte'
  import { goTo, goWorkspace, getRouteType } from '../../stores/router.svelte'
  import { groupForRoute, NAV_GROUPS, ADMIN_ONLY_ROUTES, type NavGroup } from '../../routes'
  import { getSession, logout } from '../../stores/session.svelte'
  import { toggleTheme, getTheme } from '../../stores/theme.svelte'
  import { openCommandPalette } from '../../stores/command-palette.svelte'
  import { loadLicense } from '../../stores/license.svelte'
  import { checkForUpdate, hasUpdate, getLatestVersion } from '../../stores/update-check.svelte'
  import { fetchNodeInfo, fetchClusterInfo } from '../../api/query'
  import {
    Compass, LayoutDashboard, Hammer, Activity, Settings, SquareTerminal,
    Sun, Moon, LogOut, Search, ArrowUpCircle,
  } from 'lucide-svelte'

  const session = $derived(getSession())

  let nodeHostname = $state<string | null>(null)
  let clusterName = $state<string | null>(null)
  let clusterNodeCount = $state(0)
  const updateAvailable = $derived(session?.appVersion ? hasUpdate(session.appVersion) : false)

  // One icon per group. Six is learnable; fourteen is not.
  const GROUP_ICONS: Record<NavGroup['id'], typeof Compass> = {
    query: SquareTerminal,
    explore: Compass,
    visualize: LayoutDashboard,
    build: Hammer,
    operate: Activity,
    settings: Settings,
  }

  const activeGroup = $derived(groupForRoute(getRouteType()))
  const railGroups = NAV_GROUPS.filter((g) => g.id !== 'settings' && g.id !== 'query')
  const settingsGroup = NAV_GROUPS.find((g) => g.id === 'settings')!
  const settingsFirst = $derived(
    settingsGroup.routes.find((r) => !ADMIN_ONLY_ROUTES.has(r) || session?.role === 'admin') ?? 'settings',
  )
  const connectionTitle = $derived(
    session
      ? [
          `${session.connectionName ?? 'CH-UI'} as ${session.user}`,
          nodeHostname ?? '',
          clusterName ? `${clusterName} (${clusterNodeCount} nodes)` : '',
          session.connectionOnline ? 'Connected' : 'Disconnected',
        ].filter(Boolean).join('\n')
      : '',
  )

  function openGroup(g: NavGroup) {
    // Land on the group's first page; the panel lists the rest.
    const first = g.routes.find((r) => !ADMIN_ONLY_ROUTES.has(r) || session?.role === 'admin')
    if (first) goTo(first)
  }

  async function loadNodeAndClusterInfo() {
    try {
      const info = await fetchNodeInfo()
      nodeHostname = info.node?.hostname ?? null
    } catch {
      nodeHostname = null
    }
    try {
      const cluster = await fetchClusterInfo()
      if (cluster.is_cluster && cluster.clusters.length > 0) {
        clusterName = cluster.clusters[0].name
        clusterNodeCount = cluster.clusters[0].total_nodes
      }
    } catch {
      clusterName = null
    }
  }

  onMount(() => {
    void loadLicense()
    if (session?.appVersion) void checkForUpdate(session.appVersion)
    if (session) void loadNodeAndClusterInfo()
  })

</script>

<nav class="flex h-full w-12 shrink-0 flex-col items-center border-r border-edge-subtle bg-sidebar py-2" aria-label="Primary">
  <Button icon variant="ghost" size="sm" onclick={openCommandPalette} title="Search or run a command (⌘K)" aria-label="Command menu">
    <Search size={15} />
  </Button>

  <span class="my-1.5 h-px w-5 bg-edge"></span>

  <button
    class="relative inline-flex h-8 w-8 items-center justify-center rounded-md transition-colors {activeGroup.id === 'query' ? 'bg-active text-accent' : 'text-fg-3 hover:bg-hover hover:text-fg'}"
    onclick={goWorkspace}
    title="Query workspace (⌥N for a new query)"
    aria-label="Query"
    aria-current={activeGroup.id === 'query' ? 'page' : undefined}
  >
    {#if activeGroup.id === 'query'}<span class="absolute -left-2 top-1.5 bottom-1.5 w-0.5 rounded-full bg-accent"></span>{/if}
    <SquareTerminal size={16} />
  </button>

  {#each railGroups as g}
    {@const Icon = GROUP_ICONS[g.id]}
    {@const active = activeGroup.id === g.id}
    <button
      class="relative mt-1 inline-flex h-8 w-8 items-center justify-center rounded-md transition-colors {active ? 'bg-active text-fg' : 'text-fg-3 hover:bg-hover hover:text-fg'}"
      onclick={() => openGroup(g)}
      title={g.label}
      aria-label={g.label}
      aria-current={active ? 'page' : undefined}
    >
      {#if active}<span class="absolute -left-2 top-1.5 bottom-1.5 w-0.5 rounded-full bg-accent"></span>{/if}
      <Icon size={16} />
    </button>
  {/each}

  <div class="flex-1"></div>

  {#if updateAvailable}
    <a
      href="https://github.com/caioricciuti/ch-ui/releases/latest" target="_blank" rel="noopener noreferrer"
      class="mb-1 inline-flex h-7 w-7 items-center justify-center rounded-md text-accent hover:bg-hover"
      title="Update available: {getLatestVersion()} (running {session?.appVersion})"
      aria-label="Update available"
    >
      <ArrowUpCircle size={15} />
    </a>
  {/if}

  <button
    class="relative inline-flex h-8 w-8 items-center justify-center rounded-md transition-colors {activeGroup.id === 'settings' ? 'bg-active text-fg' : 'text-fg-3 hover:bg-hover hover:text-fg'}"
    onclick={() => goTo(settingsFirst)}
    title="Settings"
    aria-label="Settings"
    aria-current={activeGroup.id === 'settings' ? 'page' : undefined}
  >
    {#if activeGroup.id === 'settings'}<span class="absolute -left-2 top-1.5 bottom-1.5 w-0.5 rounded-full bg-accent"></span>{/if}
    <Settings size={16} />
  </button>

  <span class="my-1.5 h-px w-5 bg-edge"></span>

  {#if session}
    <span
      class="my-1 h-1.5 w-1.5 rounded-full {session.connectionOnline ? 'bg-success' : 'bg-danger'}"
      title={connectionTitle}
      role="img"
      aria-label={session.connectionOnline ? 'Connected' : 'Disconnected'}
    ></span>
  {/if}
  <Button icon variant="ghost" size="sm" onclick={toggleTheme} title="Toggle theme" aria-label="Toggle theme">
    {#if getTheme() === 'dark'}<Sun size={15} />{:else}<Moon size={15} />{/if}
  </Button>
  {#if session}
    <Button icon variant="ghost" size="sm" class="hover:text-danger" onclick={logout} title="Log out ({session.user})" aria-label="Log out">
      <LogOut size={15} />
    </Button>
  {/if}
</nav>
