<script lang="ts">
  import { onMount } from 'svelte'
  import Button from '../common/Button.svelte'
  import Tooltip from '../common/Tooltip.svelte'
  import ConnectionSwitcher from './ConnectionSwitcher.svelte'
  import { isConnectionSwitcherOpen, openConnectionSwitcher, closeConnectionSwitcher } from '../../stores/connection-switcher.svelte'
  import { goTo, goWorkspace, getRouteType } from '../../stores/router.svelte'
  import { groupForRoute, NAV_GROUPS, ADMIN_ONLY_ROUTES, type NavGroup } from '../../routes'
  import { getSession, logout } from '../../stores/session.svelte'
  import { cycleThemePreference, getThemePreference } from '../../stores/theme.svelte'
  import { ALT, MOD, combo } from '../../utils/shortcuts'
  import { openCommandPalette } from '../../stores/command-palette.svelte'
  import { loadLicense } from '../../stores/license.svelte'
  import { CACHE_TTL, checkForUpdate, hasUpdate, getLatestVersion } from '../../stores/update-check.svelte'
  import { fetchNodeInfo, fetchClusterInfo } from '../../api/query'
  import {
    Compass, LayoutDashboard, Hammer, Activity, Settings, SquareTerminal,
    Sun, Moon, Monitor, LogOut, Search, ArrowUpCircle, CircleCheck, Info,
  } from 'lucide-svelte'

  const session = $derived(getSession())

  let nodeHostname = $state<string | null>(null)
  let clusterName = $state<string | null>(null)
  let clusterNodeCount = $state(0)
  // Version indicator, always shown: behind, on the latest, or unknown
  // (dev build, or the update check failed or has not answered).
  const RELEASES_URL = 'https://github.com/caioricciuti/ch-ui/releases'
  const versionInfo = $derived.by(() => {
    const running = session?.appVersion
    if (!running) return null
    const label = /^v/.test(running) ? running : `v${running}`
    const latest = getLatestVersion()
    const dev = !/^v?\d+\.\d+/.test(running)
    if (!dev && latest && hasUpdate(running)) {
      return {
        state: 'behind' as const,
        text: `Running ${label}\nLatest ${latest}\nClick for release notes`,
        href: `${RELEASES_URL}/latest`,
      }
    }
    if (!dev && latest) {
      return { state: 'latest' as const, text: `Running ${label}\nYou are on the latest version`, href: /^v\d+\.\d+\.\d+$/.test(label) ? `${RELEASES_URL}/tag/${label}` : RELEASES_URL }
    }
    return {
      state: 'unknown' as const,
      text: `Running ${dev ? running : label}\n${dev ? 'Development build' : 'Could not check for updates'}`,
      href: RELEASES_URL,
    }
  })

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

  let connectionButton = $state<HTMLButtonElement | null>(null)
  const switcherOpen = $derived(isConnectionSwitcherOpen())
  // Measured when the switcher opens (from here or the command palette).
  const switcherAnchor = $derived(switcherOpen ? connectionButton?.getBoundingClientRect() ?? null : null)

  const themePref = $derived(getThemePreference())
  const THEME_LABEL = { system: 'System theme', light: 'Light theme', dark: 'Dark theme' } as const
  const NEXT_THEME = { system: 'light', light: 'dark', dark: 'system' } as const

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
    // A tab can stay open for days: check again when it comes back into view
    // and every hour; the cache keeps this to one request per hour.
    const recheck = () => {
      if (document.visibilityState === 'visible' && session?.appVersion) void checkForUpdate(session.appVersion)
    }
    document.addEventListener('visibilitychange', recheck)
    const timer = setInterval(recheck, CACHE_TTL)
    return () => {
      document.removeEventListener('visibilitychange', recheck)
      clearInterval(timer)
    }
  })

</script>

<nav class="flex h-full w-12 shrink-0 flex-col items-center border-r border-edge-subtle bg-sidebar py-2" aria-label="Primary">
  <Button icon variant="ghost" size="sm" onclick={openCommandPalette} title="Search or run a command ({combo(MOD, 'K')})" aria-label="Command menu">
    <Search size={15} />
  </Button>

  <span class="my-1.5 h-px w-5 bg-edge"></span>

  <button
    class="relative inline-flex h-8 w-8 items-center justify-center rounded-md transition-colors {activeGroup.id === 'query' ? 'bg-active text-accent' : 'text-fg-3 hover:bg-hover hover:text-fg'}"
    onclick={goWorkspace}
    title="Query workspace ({combo(ALT, 'N')} for a new query)"
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

  {#if versionInfo}
    <Tooltip text={versionInfo.text} class="mb-1">
      <a
        href={versionInfo.href}
        target="_blank"
        rel="noopener noreferrer"
        class="inline-flex h-7 w-7 items-center justify-center rounded-md transition-colors hover:bg-hover {versionInfo.state === 'behind' ? 'text-accent' : 'text-fg-4 hover:text-fg-2'}"
        aria-label={versionInfo.text.replaceAll('\n', '. ')}
      >
        {#if versionInfo.state === 'behind'}
          <ArrowUpCircle size={15} />
        {:else if versionInfo.state === 'latest'}
          <CircleCheck size={15} />
        {:else}
          <Info size={15} />
        {/if}
      </a>
    </Tooltip>
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
    <button
      bind:this={connectionButton}
      class="inline-flex h-6 w-6 items-center justify-center rounded-md transition-colors hover:bg-hover {switcherOpen ? 'bg-active' : ''}"
      onclick={() => (switcherOpen ? closeConnectionSwitcher() : openConnectionSwitcher())}
      onmousedown={(e) => e.stopPropagation()}
      title={`${connectionTitle}\nClick to switch connection`}
      aria-label="Switch connection (now {session.connectionName ?? 'CH-UI'})"
      aria-expanded={switcherOpen}
      aria-haspopup="dialog"
    >
      <span class="h-1.5 w-1.5 rounded-full {session.connectionOnline ? 'bg-success' : 'bg-danger'}"></span>
    </button>
    <span class="sr-only" role="status">
      {session.connectionName ?? 'Connection'}: {session.connectionOnline ? 'connected' : 'disconnected'}
    </span>
  {/if}
  <Button
    icon
    variant="ghost"
    size="sm"
    onclick={cycleThemePreference}
    title="{THEME_LABEL[themePref]}. Click for {THEME_LABEL[NEXT_THEME[themePref]].toLowerCase()}"
    aria-label="Theme: {THEME_LABEL[themePref]}. Switch to {THEME_LABEL[NEXT_THEME[themePref]].toLowerCase()}"
  >
    {#if themePref === 'system'}<Monitor size={15} />{:else if themePref === 'dark'}<Moon size={15} />{:else}<Sun size={15} />{/if}
  </Button>
  {#if session}
    <Button icon variant="ghost" size="sm" class="hover:text-danger" onclick={logout} title="Log out ({session.user})" aria-label="Log out">
      <LogOut size={15} />
    </Button>
  {/if}
</nav>

{#if session && switcherOpen}
  <ConnectionSwitcher anchor={switcherAnchor} onclose={closeConnectionSwitcher} />
{/if}
