<script lang="ts">
  import { tick, onMount, untrack } from 'svelte'
  import {
    Search, Plus, Table2, Sparkles, LayoutDashboard, Bookmark, Clock,
    Brain, Shield, Settings, Moon, Sun, LogOut, SquareTerminal, Home,
    Workflow, Boxes, Activity, FileText, GitBranch, ChartBar,
    Network, KeyRound, Scale, MessageSquare, HeartPulse, Gauge,
    Cpu, Info, Hash, Zap,
  } from 'lucide-svelte'
  import { closeCommandPalette, isCommandPaletteOpen } from '../../stores/command-palette.svelte'
  import {
    openQueryTab, openSingletonTab, openTableTab, openDashboardTab,
    getTabs, openHomeTab,
  } from '../../stores/tabs.svelte'
  import type { SingletonTab } from '../../stores/tabs.svelte'
  import { getDatabases, loadDatabases, loadTables } from '../../stores/schema.svelte'
  import { getSession, logout } from '../../stores/session.svelte'
  import { getTheme, toggleTheme } from '../../stores/theme.svelte'
  import { isProActive } from '../../stores/license.svelte'
  import { listWorkspaceDashboards, listWorkspaceSavedQueries } from '../../api/workspace'
  import { listModels } from '../../api/models'
  import { listPipelines } from '../../api/pipelines'
  import { listBrainChats } from '../../api/brain'

  type Group =
    | 'recent' | 'page' | 'table' | 'saved' | 'dashboard'
    | 'model' | 'pipeline' | 'brainchat' | 'telemetry' | 'action' | 'help'

  interface CommandItem {
    id: string
    group: Group
    label: string
    sub?: string
    icon: typeof Search
    shortcut?: string
    keywords?: string
    weight?: number
    run: () => void
  }

  const GROUP_LABEL: Record<Group, string> = {
    recent: 'Recent',
    page: 'Pages',
    action: 'Actions',
    saved: 'Saved queries',
    dashboard: 'Dashboards',
    model: 'Models',
    pipeline: 'Pipelines',
    brainchat: 'Brain chats',
    telemetry: 'Telemetry',
    table: 'Tables',
    help: 'Help',
  }
  const GROUP_ORDER: Group[] = [
    'recent', 'help', 'page', 'telemetry',
    'saved', 'dashboard', 'model', 'pipeline', 'brainchat',
    'table', 'action',
  ]

  const PREFIXES: Record<string, Group> = {
    '>': 'action',
    't:': 'table',
    'q:': 'saved',
    'd:': 'dashboard',
    'm:': 'model',
    'p:': 'pipeline',
    'b:': 'brainchat',
    'tel:': 'telemetry',
    '?': 'help',
  }

  let inputEl: HTMLInputElement | undefined = $state()
  let query = $state('')
  let selectedIdx = $state(0)
  let scopeGroup = $state<Group | null>(null)

  let savedQueries = $state<Array<{ id: string; name: string; description?: string | null }>>([])
  let dashboards = $state<Array<{ id: string; name: string; description?: string | null }>>([])
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
      mkPage('saved-queries', 'Saved Queries', Bookmark, () => openSingletonTab('saved-queries', 'Saved Queries'), { keywords: 'bookmarks queries history' }),
      mkPage('dashboards', 'Dashboards', LayoutDashboard, () => openSingletonTab('dashboards', 'Dashboards'), { keywords: 'charts panels metrics dash' }),
      mkPage('schedules', 'Schedules', Clock, () => openSingletonTab('schedules', 'Schedules'), { keywords: 'cron runs scheduled jobs' }),
      mkPage('brain', 'Brain AI', Brain, () => openSingletonTab('brain', 'Brain'), { keywords: 'ai assistant chat agent llm' }),
      mkPage('pipelines', 'Pipelines', Workflow, () => openSingletonTab('pipelines', 'Pipelines'), { keywords: 'ingest etl streams' }),
      mkPage('models', 'Models', Boxes, () => openSingletonTab('models', 'Models'), { keywords: 'dbt models materialize' }),
      mkPage('governance', 'Governance', Scale, () => openSingletonTab('governance', 'Governance'), { keywords: 'access policies rules audit' }),
      mkPage('cluster-health', 'Cluster Health', HeartPulse, () => openSingletonTab('cluster-health', 'Cluster Health'), { keywords: 'replication merges mutations parts keeper backups monitoring' }),
      mkPage('query-insights', 'Query Insights', Gauge, () => openSingletonTab('query-insights', 'Query Insights'), { keywords: 'query log latency slow p95 errors memory insights analytics' }),
      mkPage('telemetry', 'Telemetry', Activity, () => openSingletonTab('telemetry', 'Telemetry'), { keywords: 'otel observability logs traces metrics' }),
      mkPage('admin', 'Admin', Shield, () => openSingletonTab('admin', 'Admin'), { keywords: 'users audit query log' }),
      mkPage('settings', 'Settings', Settings, () => openSingletonTab('settings', 'Settings'), { keywords: 'config preferences license' }),
    )

    if (pro) {
      const telTabs: Array<[string, string, typeof Search, string]> = [
        ['logs', 'Telemetry · Logs', FileText, 'log records ingest'],
        ['services', 'Telemetry · Services', Network, 'services rps p95 errors'],
        ['traces', 'Telemetry · Traces', GitBranch, 'spans waterfall trace'],
        ['metrics', 'Telemetry · Metrics', ChartBar, 'metrics gauges counters histogram'],
        ['endpoints', 'Telemetry · Endpoints', KeyRound, 'otlp ingest tokens endpoints'],
      ]
      for (const [slug, label, icon, kw] of telTabs) {
        items.push({
          id: `tel-${slug}`,
          group: 'telemetry',
          label,
          sub: 'Open telemetry tab',
          icon,
          keywords: kw,
          run: () => {
            setTelemetryTab(slug)
            openSingletonTab('telemetry', 'Telemetry')
          },
        })
      }
    }

    items.push(
      mkAction('new-query', 'New Query', Plus, `${cmd}⇧N`, () => openQueryTab(), 'create sql blank editor'),
    )

    if (pro) {
      items.push(
        mkAction('new-dashboard', 'New Dashboard', Plus, undefined, () => openSingletonTab('dashboards', 'Dashboards'), 'create dashboard'),
        mkAction('new-model', 'New Model', Plus, undefined, () => openSingletonTab('models', 'Models'), 'create model dbt'),
        mkAction('new-pipeline', 'New Pipeline', Plus, undefined, () => openSingletonTab('pipelines', 'Pipelines'), 'create pipeline'),
        mkAction('new-brain-chat', 'New Brain Chat', Plus, undefined, () => openSingletonTab('brain', 'Brain'), 'new chat brain ai'),
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
      items.push(
        mkHelp('help-prefixes', 'Prefixes — scope to one kind',
          '> actions · t: tables · q: saved queries · d: dashboards · m: models · p: pipelines · b: brain chats · tel: telemetry · ? help'),
        mkHelp('help-shortcuts', 'Keyboard shortcuts',
          `${cmd}K open palette · ${cmd}⇧N new query · ↑↓ select · Enter run · Esc close`),
        mkHelp('help-tip-brain', 'Type a question — Brain answers it',
          'End your query with "?" and hit Enter to seed a Brain chat with the prompt.'),
      )
    }

    return items
  }

  function mkPage(slug: SingletonTab['type'] | 'home', label: string, icon: typeof Search,
    run: () => void, extras: Partial<CommandItem> = {}): CommandItem {
    return { id: `page-${slug}`, group: 'page', label, sub: 'Open page', icon, run, ...extras }
  }
  function mkAction(id: string, label: string, icon: typeof Search, shortcut: string | undefined,
    run: () => void, keywords?: string): CommandItem {
    return { id: `act-${id}`, group: 'action', label, sub: 'Action', icon, shortcut, keywords, run }
  }
  function mkHelp(id: string, label: string, sub: string): CommandItem {
    return { id, group: 'help', label, sub, icon: Info, run: () => {} }
  }

  function setTelemetryTab(slug: string) {
    if (typeof window === 'undefined') return
    const url = new URL(window.location.href)
    url.searchParams.set('tab', slug)
    window.history.replaceState(null, '', url.toString())
  }

  const dynamic = $derived.by<CommandItem[]>(() => {
    if (!pro) return []
    const items: CommandItem[] = []

    for (const q of savedQueries) {
      items.push({
        id: `saved-${q.id}`,
        group: 'saved',
        label: q.name,
        sub: q.description || 'Saved query',
        icon: Bookmark,
        run: () => openSingletonTab('saved-queries', 'Saved Queries'),
      })
    }

    for (const d of dashboards) {
      items.push({
        id: `dash-${d.id}`,
        group: 'dashboard',
        label: d.name,
        sub: d.description || 'Dashboard',
        icon: LayoutDashboard,
        run: () => openDashboardTab(d.id, d.name),
      })
    }

    for (const m of models) {
      items.push({
        id: `model-${m.id}`,
        group: 'model',
        label: m.name,
        sub: m.target_database ? `Model · ${m.target_database}` : (m.description || 'Model'),
        icon: Cpu,
        run: () => openSingletonTab('models', 'Models'),
      })
    }

    for (const p of pipelines) {
      items.push({
        id: `pipe-${p.id}`,
        group: 'pipeline',
        label: p.name,
        sub: p.status ? `Pipeline · ${p.status}` : (p.description || 'Pipeline'),
        icon: Workflow,
        run: () => openSingletonTab('pipelines', 'Pipelines'),
      })
    }

    for (const c of brainChats) {
      items.push({
        id: `chat-${c.id}`,
        group: 'brainchat',
        label: c.title || 'Untitled chat',
        sub: 'Brain chat',
        icon: MessageSquare,
        run: () => openSingletonTab('brain', 'Brain'),
      })
    }

    return items
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
          : tab.type === 'dashboard'
            ? LayoutDashboard
            : tab.type === 'brain'
              ? Brain
              : tab.type === 'telemetry'
                ? Activity
                : Sparkles
      items.push({
        id,
        group: 'recent',
        label: tab.name,
        sub: 'Recent tab',
        icon,
        run: () => {
          if (tab.type === 'table') openTableTab(tab.database, tab.table)
          else if (tab.type === 'dashboard') openDashboardTab(tab.dashboardId, tab.name)
          else if (tab.type === 'query') openQueryTab(tab.sql)
          else openSingletonTab(tab.type as SingletonTab['type'], tab.name)
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

  const brainSuggestion = $derived.by<CommandItem | null>(() => {
    if (!pro) return null
    const t = parsed.term.trim()
    if (parsed.scope) return null
    if (!t) return null
    const wordCount = t.split(/\s+/).length
    const looksLikeQuestion =
      t.endsWith('?') ||
      /^(how|why|what|where|when|show|find|give|tell|explain|list|count|top|do|does|can|should|is|are)\b/i.test(t)
    if (wordCount < 4 && !looksLikeQuestion) return null
    return {
      id: 'brain-ask',
      group: 'action',
      label: `Ask Brain: ${t}`,
      sub: 'Open Brain chat with this prompt',
      icon: Zap,
      shortcut: '↵',
      weight: 100,
      run: () => {
        try {
          sessionStorage.setItem('ch-ui-brain-prompt-seed', t)
        } catch {}
        openSingletonTab('brain', 'Brain')
      },
    }
  })

  type Grouped = Array<{ group: Group; items: Array<{ item: CommandItem; score: number }> }>

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

    if (inputEmpty) {
      const recentOnly = ranked.filter(x => x.item.group === 'recent').slice(0, 5)
      const curatedIds = new Set([
        'page-home', 'act-new-query', 'page-telemetry', 'page-brain',
        'page-saved-queries', 'page-dashboards',
      ])
      const curated = ranked.filter(x => curatedIds.has(x.item.id))
      const combined: Grouped = []
      if (recentOnly.length > 0) combined.push({ group: 'recent', items: recentOnly })
      if (curated.length > 0) combined.push({ group: 'page', items: curated })
      return combined
    }

    const buckets: Partial<Record<Group, Array<{ item: CommandItem; score: number }>>> = {}
    for (const r of ranked) {
      const g = r.item.group
      if (!buckets[g]) buckets[g] = []
      buckets[g]!.push(r)
    }
    const perGroupCap = scope ? 50 : 6
    const out: Grouped = []
    for (const g of GROUP_ORDER) {
      const arr = buckets[g]
      if (!arr || arr.length === 0) continue
      arr.sort((a, b) => b.score - a.score)
      out.push({ group: g, items: arr.slice(0, perGroupCap) })
    }
    return out
  })

  const flat = $derived.by<CommandItem[]>(() => grouped.flatMap(g => g.items.map(x => x.item)))

  function highlight(label: string, term: string): Array<{ ch: string; on: boolean }> {
    if (!term || !pro) return [{ ch: label, on: false }]
    const lt = label.toLowerCase()
    const lq = term.toLowerCase()
    const out: Array<{ ch: string; on: boolean }> = []
    let ti = 0
    for (let i = 0; i < label.length; i++) {
      if (ti < lq.length && lt[i] === lq[ti]) {
        out.push({ ch: label[i], on: true })
        ti++
      } else {
        out.push({ ch: label[i], on: false })
      }
    }
    return out
  }

  async function loadAll() {
    if (databases.length === 0) await loadDatabases()
    const dbs = getDatabases()
    const needTables = dbs.filter(d => !d.tables).slice(0, 16)
    if (needTables.length > 0) {
      await Promise.allSettled(needTables.map(d => loadTables(d.name)))
    }
    if (!pro) return
    const results = await Promise.allSettled([
      listWorkspaceSavedQueries().catch(() => []),
      listWorkspaceDashboards().catch(() => []),
      listModels().then(r => r.models ?? []).catch(() => []),
      listPipelines().then(r => r.pipelines ?? []).catch(() => []),
      listBrainChats(false).catch(() => []),
    ])
    if (results[0].status === 'fulfilled') savedQueries = results[0].value
    if (results[1].status === 'fulfilled') dashboards = results[1].value
    if (results[2].status === 'fulfilled') models = results[2].value
    if (results[3].status === 'fulfilled') pipelines = results[3].value
    if (results[4].status === 'fulfilled') brainChats = results[4].value
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
    const raw = query
    for (const [prefix, group] of Object.entries(PREFIXES)) {
      if (raw === prefix || raw.startsWith(prefix + ' ')) {
        scopeGroup = group
        query = raw.slice(prefix.length).trimStart()
        return
      }
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
    class="fixed inset-0 z-[80] bg-gray-950/45 backdrop-blur-sm"
    aria-label="Close command palette"
    onclick={() => closeCommandPalette()}
  ></button>
  <div class="fixed inset-0 z-[81] flex items-start justify-center pt-[10vh] px-4 pointer-events-none">
    <div class="surface-card w-full max-w-2xl rounded-2xl overflow-hidden pointer-events-auto">
      <!-- Input row -->
      <div class="flex items-center gap-2 px-3 py-2.5 border-b border-gray-200/80 dark:border-gray-800/80">
        <Search size={14} class="text-gray-500 shrink-0" />
        {#if scopeChip}
          <span class="inline-flex items-center gap-1 px-1.5 py-0.5 rounded-md bg-ch-blue/10 text-ch-blue text-[10px] font-medium shrink-0">
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
          class="w-full bg-transparent text-sm text-gray-800 dark:text-gray-200 placeholder:text-gray-400 outline-none"
        />
        <span class="text-[10px] text-gray-400 px-2 py-1 rounded border border-gray-300/70 dark:border-gray-700/80 shrink-0">ESC</span>
      </div>

      <!-- Results -->
      <div class="max-h-[60vh] overflow-y-auto p-1.5">
        {#if grouped.length === 0}
          <div class="px-3 py-10 text-center text-sm text-gray-500">
            <Search size={20} class="mx-auto mb-2 text-gray-400" />
            No match for "{parsed.term || query}"
            {#if pro}
              <div class="mt-3 text-[11px] text-gray-400">
                Try <code class="text-[10px]">?</code> for help · <code class="text-[10px]">&gt;</code> for actions · <code class="text-[10px]">t:</code> for tables
              </div>
            {/if}
          </div>
        {:else if pro}
          {#each grouped as g (g.group)}
            <div class="px-2 pt-2 pb-1 flex items-center gap-2">
              <span class="text-[10px] uppercase tracking-wide text-gray-500 dark:text-gray-400 font-medium">
                {GROUP_LABEL[g.group]}
              </span>
              <span class="text-[10px] text-gray-400">{g.items.length}</span>
            </div>
            {#each g.items as entry (entry.item.id)}
              {@const item = entry.item}
              {@const flatIdx = flat.indexOf(item)}
              {@const active = flatIdx === selectedIdx}
              <button
                class="w-full flex items-center gap-3 px-3 py-2 rounded-lg text-left transition-colors
                  {active ? 'bg-ch-blue/10 text-ch-blue' : 'hover:bg-gray-200/55 dark:hover:bg-gray-800/60 text-gray-700 dark:text-gray-300'}"
                onclick={() => runCommand(item)}
                onmouseenter={() => (selectedIdx = flatIdx)}
              >
                <item.icon size={15} class={active ? 'text-ch-blue' : 'text-gray-500'} />
                <span class="flex-1 min-w-0">
                  <span class="block text-sm font-medium truncate">
                    {#each highlight(item.label, parsed.term) as h}
                      <span class={h.on ? 'text-ch-blue font-semibold' : ''}>{h.ch}</span>
                    {/each}
                  </span>
                  {#if item.sub}
                    <span class="block text-[11px] text-gray-500 dark:text-gray-400 truncate">{item.sub}</span>
                  {/if}
                </span>
                {#if item.shortcut}
                  <span class="text-[10px] text-gray-500 px-1.5 py-0.5 rounded border border-gray-300/70 dark:border-gray-700/80 font-mono shrink-0">
                    {item.shortcut}
                  </span>
                {/if}
                {#if active}
                  <span class="text-[10px] text-gray-500 px-2 py-1 rounded border border-gray-300/70 dark:border-gray-700/80 shrink-0">ENTER</span>
                {/if}
              </button>
            {/each}
          {/each}
        {:else}
          {#each flat as item, idx (item.id)}
            <button
              class="w-full flex items-center gap-3 px-3 py-2 rounded-lg text-left transition-colors {idx === selectedIdx ? 'bg-ch-blue/10 text-ch-blue' : 'hover:bg-gray-200/55 dark:hover:bg-gray-800/60 text-gray-700 dark:text-gray-300'}"
              onclick={() => runCommand(item)}
              onmouseenter={() => selectedIdx = idx}
            >
              <item.icon size={15} class={idx === selectedIdx ? 'text-ch-blue' : 'text-gray-500'} />
              <span class="flex-1 min-w-0">
                <span class="block text-sm font-medium truncate">{item.label}</span>
                {#if item.sub}
                  <span class="block text-[11px] text-gray-500 dark:text-gray-400 truncate">{item.sub}</span>
                {/if}
              </span>
              {#if idx === selectedIdx}
                <span class="text-[10px] text-gray-500 px-2 py-1 rounded border border-gray-300/70 dark:border-gray-700/80">ENTER</span>
              {/if}
            </button>
          {/each}
        {/if}
      </div>

      <!-- Footer -->
      <div class="px-3 py-2 border-t border-gray-200/80 dark:border-gray-800/80 text-[11px] text-gray-500 dark:text-gray-400 flex items-center justify-between">
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
