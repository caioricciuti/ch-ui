<script lang="ts">
  import { goTo } from '../../stores/router.svelte'
  import { tick, onMount, untrack } from 'svelte'
  import {
    Search, Plus, Table2, Sparkles, LayoutDashboard, Bookmark, Clock,
    Brain, Shield, Settings, Moon, Sun, LogOut, SquareTerminal, Home,
    Workflow, Boxes, Activity, Scale, HeartPulse, Gauge, Coins, Hash,
  } from 'lucide-svelte'
  import { closeCommandPalette, isCommandPaletteOpen } from '../../stores/command-palette.svelte'
  import { openQueryTab, openTableTab, getTabs, openHomeTab } from '../../stores/tabs.svelte'
  import type { PageRoute } from '../../routes'
  import { getDatabases, loadDatabases, loadTables } from '../../stores/schema.svelte'
  import { getSession, logout } from '../../stores/session.svelte'
  import { getTheme, toggleTheme } from '../../stores/theme.svelte'
  import { isProActive } from '../../stores/license.svelte'
  import type { DashboardFolder } from '../../types/api'
  import {
    GROUP_LABEL, matchPrefix, telemetryItems, helpItems, entityItems, loadProEntities,
    brainSuggestion as brainSuggestionFor, groupResults, highlight,
  } from './commandPaletteSearch.pro'
  import type { CommandItem, Group, Grouped } from './commandPaletteSearch.pro'

  let inputEl: HTMLInputElement | undefined = $state()
  let query = $state('')
  let selectedIdx = $state(0)
  let scopeGroup = $state<Group | null>(null)

  let savedQueries = $state<Array<{ id: string; name: string; description?: string | null }>>([])
  let dashboards = $state<Array<{ id: string; name: string; description?: string | null; folder_id?: string | null }>>([])
  let dashboardFolders = $state<DashboardFolder[]>([])
  let models = $state<Array<{ id: string; name: string; description?: string | null; target_database?: string }>>([])
  let pipelines = $state<Array<{ id: string; name: string; description?: string | null; status?: string }>>([])
  let brainChats = $state<Array<{ id: string; title: string }>>([])
  let recentIds = $state<string[]>([])
  const RECENT_KEY = 'ch-ui-palette-recent'
  const MAX_RECENT = 8

  const open = $derived(isCommandPaletteOpen())
  const tabs = $derived(getTabs())
  const databases = $derived(getDatabases())
  const session = $derived(getSession())
  const pro = $derived(isProActive())
  const isMac = typeof navigator !== 'undefined' && /Mac/.test(navigator.platform)
  const cmd = isMac ? '⌘' : 'Ctrl'

  function scoreMatch(text: string, term: string): number {
    if (!term) return 1
    let ti = 0, score = 0
    const lt = text.toLowerCase(), lq = term.toLowerCase()
    for (let i = 0; i < lt.length && ti < lq.length; i++) {
      if (lt[i] === lq[ti]) {
        score += i > 0 && (lt[i - 1] === ' ' || lt[i - 1] === '.') ? 5 : 2
        ti++
      }
    }
    if (ti !== lq.length) return -1
    if (lt.startsWith(lq)) score += 25
    if (lt.includes(` ${lq}`) || lt.includes(`.${lq}`)) score += 10
    return score
  }

  function buildStatic(): CommandItem[] {
    const items: CommandItem[] = []

    items.push(
      mkPage('home', 'Home', Home, () => openHomeTab(), { weight: 10, keywords: 'home start workspace' }),
      mkPage('saved-queries', 'Saved Queries', Bookmark, () => goTo('saved-queries', 'Saved Queries'), { keywords: 'bookmarks queries history' }),
      mkPage('dashboards', 'Dashboards', LayoutDashboard, () => goTo('dashboards', 'Dashboards'), { keywords: 'charts panels metrics dash' }),
      mkPage('schedules', 'Schedules', Clock, () => goTo('schedules', 'Schedules'), { keywords: 'cron runs scheduled jobs' }),
      mkPage('brain', 'Brain AI', Brain, () => goTo('brain', 'Brain'), { keywords: 'ai assistant chat agent llm' }),
      mkPage('pipelines', 'Pipelines', Workflow, () => goTo('pipelines', 'Pipelines'), { keywords: 'ingest etl streams' }),
      mkPage('models', 'Models', Boxes, () => goTo('models', 'Models'), { keywords: 'dbt models materialize' }),
      mkPage('governance', 'Governance', Scale, () => goTo('governance', 'Governance'), { keywords: 'access policies rules audit' }),
      mkPage('cluster-health', 'Cluster Health', HeartPulse, () => goTo('cluster-health', 'Cluster Health'), { keywords: 'replication merges mutations parts keeper backups monitoring' }),
      mkPage('query-insights', 'Query Insights', Gauge, () => goTo('query-insights', 'Query Insights'), { keywords: 'query log latency slow p95 errors memory insights analytics' }),
      mkPage('cost-center', 'Cost Center', Coins, () => goTo('cost-center', 'Cost Center'), { keywords: 'costs chargeback showback spend budget billing finops teams' }),
      mkPage('telemetry', 'Telemetry', Activity, () => goTo('telemetry', 'Telemetry'), { keywords: 'otel observability logs traces metrics' }),
      mkPage('admin', 'Admin', Shield, () => goTo('admin', 'Admin'), { keywords: 'users audit query log' }),
      mkPage('settings', 'Settings', Settings, () => goTo('settings', 'Settings'), { keywords: 'config preferences license' }),
    )

    if (pro) {
      items.push(...telemetryItems())
    }

    items.push(
      mkAction('new-query', 'New Query', Plus, `${cmd}⇧N`, () => openQueryTab(), 'create sql blank editor'),
    )

    // Dashboards, models, pipelines and Brain chat are free.
    {
      items.push(
        mkAction('new-dashboard', 'New Dashboard', Plus, undefined, () => goTo('dashboards', 'Dashboards'), 'create dashboard'),
        mkAction('new-model', 'New Model', Plus, undefined, () => goTo('models', 'Models'), 'create model dbt'),
        mkAction('new-pipeline', 'New Pipeline', Plus, undefined, () => goTo('pipelines', 'Pipelines'), 'create pipeline'),
        mkAction('new-brain-chat', 'New Brain Chat', Plus, undefined, () => goTo('brain', 'Brain'), 'new chat brain ai'),
      )
    }

    items.push(
      mkAction('theme', getTheme() === 'dark' ? 'Switch to Light theme' : 'Switch to Dark theme',
        getTheme() === 'dark' ? Sun : Moon, undefined, () => toggleTheme(), 'theme appearance dark light'),
    )

    if (session) {
      items.push(mkAction('sign-out', 'Sign out', LogOut, undefined, () => logout(), 'logout sign out session'))
    }

    if (pro) {
      items.push(...helpItems(cmd))
    }

    return items
  }

  function mkPage(slug: PageRoute | 'home', label: string, icon: typeof Search,
    run: () => void, extras: Partial<CommandItem> = {}): CommandItem {
    return { id: `page-${slug}`, group: 'page', label, sub: 'Open page', icon, run, ...extras }
  }
  function mkAction(id: string, label: string, icon: typeof Search, shortcut: string | undefined,
    run: () => void, keywords?: string): CommandItem {
    return { id: `act-${id}`, group: 'action', label, sub: 'Action', icon, shortcut, keywords, run }
  }

  const dynamic = $derived.by<CommandItem[]>(() => {
    if (!pro) return []
    return entityItems({ savedQueries, dashboards, dashboardFolders, models, pipelines, brainChats })
  })

  const tableCatalog = $derived.by<CommandItem[]>(() => {
    const items: CommandItem[] = []
    for (const db of databases.slice(0, 16)) {
      if (!db.tables) continue
      for (const t of db.tables.slice(0, 24)) {
        items.push({
          id: `tbl-${db.name}.${t.name}`,
          group: 'table',
          label: `${db.name}.${t.name}`,
          sub: 'Open table',
          icon: Table2,
          keywords: `${db.name} ${t.name} schema column`,
          run: () => openTableTab(db.name, t.name),
        })
      }
    }
    return items
  })

  const recentTabs = $derived.by<CommandItem[]>(() => {
    const items: CommandItem[] = []
    const seen = new Set<string>()
    const tabsArr = tabs.filter(t => t.type !== 'home').slice(-8).reverse()
    for (const tab of tabsArr) {
      const id = `tab-${tab.id}`
      if (seen.has(id)) continue
      seen.add(id)
      const icon = tab.type === 'query'
        ? SquareTerminal
        : tab.type === 'table'
          ? Table2
          : Sparkles
      items.push({
        id,
        group: 'recent',
        label: tab.name,
        sub: 'Recent tab',
        icon,
        run: () => {
          if (tab.type === 'table') openTableTab(tab.database, tab.table)
          else if (tab.type === 'query') openQueryTab(tab.sql)
          else goTo(tab.type as unknown as PageRoute)
        },
      })
    }
    return items
  })

  const allItems = $derived.by<CommandItem[]>(() => {
    return [...recentTabs, ...buildStatic(), ...dynamic, ...tableCatalog]
  })

  const parsed = $derived.by<{ scope: Group | null; term: string }>(() => {
    if (!pro) return { scope: null, term: query.trim().toLowerCase() }
    if (scopeGroup) return { scope: scopeGroup, term: query.trim() }
    return { scope: null, term: query.trim() }
  })

  const brainSuggestion = $derived<CommandItem | null>(pro ? brainSuggestionFor(parsed) : null)

  const grouped = $derived.by<Grouped>(() => {
    const { scope, term } = parsed
    const inputEmpty = term === '' && !scope

    let pool = allItems
    if (scope) pool = pool.filter(i => i.group === scope)
    if (brainSuggestion) pool = [brainSuggestion, ...pool]

    const ranked = pool
      .map(item => {
        const haystack = `${item.label} ${item.sub ?? ''} ${item.keywords ?? ''}`
        const base = scoreMatch(haystack, term)
        if (base < 0) return null
        const score = base + (item.weight ?? 0)
        return { item, score }
      })
      .filter((x): x is { item: CommandItem; score: number } => x !== null)

    if (!pro) {
      ranked.sort((a, b) => b.score - a.score)
      const flat = ranked.slice(0, 28)
      if (flat.length === 0) return []
      return [{ group: 'page' as Group, items: flat }]
    }

    return groupResults(ranked, scope, inputEmpty)
  })

  const flat = $derived.by<CommandItem[]>(() => grouped.flatMap(g => g.items.map(x => x.item)))

  async function loadAll() {
    if (databases.length === 0) await loadDatabases()
    const dbs = getDatabases()
    const needTables = dbs.filter(d => !d.tables).slice(0, 16)
    if (needTables.length > 0) {
      await Promise.allSettled(needTables.map(d => loadTables(d.name)))
    }
    if (!pro) return
    const r = await loadProEntities()
    if (r.savedQueries) savedQueries = r.savedQueries
    if (r.dashboards) dashboards = r.dashboards
    if (r.models) models = r.models
    if (r.pipelines) pipelines = r.pipelines
    if (r.brainChats) brainChats = r.brainChats
    if (r.dashboardFolders) dashboardFolders = r.dashboardFolders
  }

  function persistRecent(id: string) {
    const next = [id, ...recentIds.filter(x => x !== id)].slice(0, MAX_RECENT)
    recentIds = next
    try { localStorage.setItem(RECENT_KEY, JSON.stringify(next)) } catch {}
  }

  async function runCommand(item: CommandItem) {
    if (item.group !== 'help') persistRecent(item.id)
    item.run()
    closeCommandPalette()
    query = ''
    selectedIdx = 0
    scopeGroup = null
  }

  function handleKeydown(e: KeyboardEvent) {
    if (!open) return
    if (e.key === 'Escape') {
      e.preventDefault()
      closeCommandPalette()
      return
    }
    if (e.key === 'Backspace' && query === '' && scopeGroup) {
      e.preventDefault()
      scopeGroup = null
      return
    }
    if (e.key === 'ArrowDown') {
      e.preventDefault()
      selectedIdx = Math.min(flat.length - 1, selectedIdx + 1)
      return
    }
    if (e.key === 'ArrowUp') {
      e.preventDefault()
      selectedIdx = Math.max(0, selectedIdx - 1)
      return
    }
    if (e.key === 'Enter' && flat[selectedIdx]) {
      e.preventDefault()
      runCommand(flat[selectedIdx])
    }
  }

  function handleInput() {
    if (!pro || scopeGroup) return
    const m = matchPrefix(query)
    if (m) {
      scopeGroup = m.group
      query = m.rest
    }
  }

  $effect(() => {
    void flat.length
    selectedIdx = 0
  })

  onMount(() => {
    try {
      const raw = localStorage.getItem(RECENT_KEY)
      if (raw) recentIds = JSON.parse(raw)
    } catch {}
  })

  $effect(() => {
    if (!open) return
    // Run the open routine untracked: loadAll() mutates the databases/data
    // stores, and without untrack the effect would subscribe to its own writes
    // and re-run forever (effect_update_depth_exceeded). The only dependency we
    // want here is `open` flipping to true.
    untrack(() => {
      query = ''
      selectedIdx = 0
      scopeGroup = null
      loadAll()
      tick().then(() => inputEl?.focus())
    })
  })

  const scopeChip = $derived.by(() => {
    if (!pro || !scopeGroup) return null
    return { group: scopeGroup, label: GROUP_LABEL[scopeGroup] }
  })
</script>

<svelte:window onkeydown={handleKeydown} />

{#if open}
  <button
    type="button"
    class="fixed inset-0 z-[80] bg-canvas/45 backdrop-blur-sm"
    aria-label="Close command palette"
    onclick={() => closeCommandPalette()}
  ></button>
  <div class="fixed inset-0 z-[81] flex items-start justify-center pt-[10vh] px-4 pointer-events-none">
    <div class="surface-card w-full max-w-2xl rounded-lg overflow-hidden pointer-events-auto">
      <!-- Input row -->
      <div class="flex items-center gap-2 px-3 py-2.5 border-b border-edge-subtle">
        <Search size={14} class="text-fg-3 shrink-0" />
        {#if scopeChip}
          <span class="inline-flex items-center gap-1 px-1.5 py-0.5 rounded-md bg-accent-soft text-accent text-[10px] font-medium shrink-0">
            <Hash size={10} />
            {scopeChip.label}
          </span>
        {/if}
        <input
          bind:this={inputEl}
          bind:value={query}
          oninput={handleInput}
          type="text"
          placeholder={scopeChip
            ? `Search ${scopeChip.label.toLowerCase()}…`
            : pro ? 'Search anything · try ? for help' : 'Search actions, tables, tabs...'}
          class="w-full bg-transparent text-sm text-fg placeholder:text-fg-4 outline-none"
        />
        <span class="text-[10px] text-fg-4 px-2 py-1 rounded border border-edge shrink-0">ESC</span>
      </div>

      <!-- Results -->
      <div class="max-h-[60vh] overflow-y-auto p-1.5">
        {#if grouped.length === 0}
          <div class="px-3 py-10 text-center text-sm text-fg-3">
            <Search size={20} class="mx-auto mb-2 text-fg-4" />
            No match for "{parsed.term || query}"
            {#if pro}
              <div class="mt-3 text-[11px] text-fg-4">
                Try <code class="text-[10px]">?</code> for help · <code class="text-[10px]">&gt;</code> for actions · <code class="text-[10px]">t:</code> for tables
              </div>
            {/if}
          </div>
        {:else if pro}
          {#each grouped as g (g.group)}
            <div class="px-2 pt-2 pb-1 flex items-center gap-2">
              <span class="text-[10px] uppercase tracking-wide text-fg-3 font-medium">
                {GROUP_LABEL[g.group]}
              </span>
              <span class="text-[10px] text-fg-4">{g.items.length}</span>
            </div>
            {#each g.items as entry (entry.item.id)}
              {@const item = entry.item}
              {@const flatIdx = flat.indexOf(item)}
              {@const active = flatIdx === selectedIdx}
              <button
                class="w-full flex items-center gap-3 px-3 py-2 rounded-lg text-left transition-colors
                  {active ? 'bg-accent-soft text-accent' : 'hover:bg-hover text-fg-2'}"
                onclick={() => runCommand(item)}
                onmouseenter={() => (selectedIdx = flatIdx)}
              >
                <item.icon size={15} class={active ? 'text-ch-orange' : 'text-fg-3'} />
                <span class="flex-1 min-w-0">
                  <span class="block text-sm font-medium truncate">
                    {#each highlight(item.label, parsed.term) as h}
                      <span class={h.on ? 'text-ch-orange font-semibold' : ''}>{h.ch}</span>
                    {/each}
                  </span>
                  {#if item.sub}
                    <span class="block text-[11px] text-fg-3 truncate">{item.sub}</span>
                  {/if}
                </span>
                {#if item.shortcut}
                  <span class="text-[10px] text-fg-3 px-1.5 py-0.5 rounded border border-edge font-mono shrink-0">
                    {item.shortcut}
                  </span>
                {/if}
                {#if active}
                  <span class="text-[10px] text-fg-3 px-2 py-1 rounded border border-edge shrink-0">ENTER</span>
                {/if}
              </button>
            {/each}
          {/each}
        {:else}
          {#each flat as item, idx (item.id)}
            <button
              class="w-full flex items-center gap-3 px-3 py-2 rounded-lg text-left transition-colors {idx === selectedIdx ? 'bg-accent-soft text-accent' : 'hover:bg-hover text-fg-2'}"
              onclick={() => runCommand(item)}
              onmouseenter={() => selectedIdx = idx}
            >
              <item.icon size={15} class={idx === selectedIdx ? 'text-ch-orange' : 'text-fg-3'} />
              <span class="flex-1 min-w-0">
                <span class="block text-sm font-medium truncate">{item.label}</span>
                {#if item.sub}
                  <span class="block text-[11px] text-fg-3 truncate">{item.sub}</span>
                {/if}
              </span>
              {#if idx === selectedIdx}
                <span class="text-[10px] text-fg-3 px-2 py-1 rounded border border-edge">ENTER</span>
              {/if}
            </button>
          {/each}
        {/if}
      </div>

      <!-- Footer -->
      <div class="px-3 py-2 border-t border-edge-subtle text-[11px] text-fg-3 flex items-center justify-between">
        <span>
          <span class="font-medium">↑↓</span> navigate · <span class="font-medium">↵</span> run · <span class="font-medium">esc</span> close
        </span>
        {#if pro}
          <span>
            <code class="text-[10px]">?</code> for help · <code class="text-[10px]">{cmd}K</code> to toggle
          </span>
        {/if}
      </div>
    </div>
  </div>
{/if}
