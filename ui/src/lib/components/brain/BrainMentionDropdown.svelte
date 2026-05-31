<script lang="ts">
  import { onMount } from 'svelte'
  import { getDatabases, loadTables } from '../../stores/schema.svelte'
  import { listWorkspaceDashboards, listWorkspaceSavedQueries } from '../../api/workspace'
  import { listModels } from '../../api/models'
  import { listPipelines } from '../../api/pipelines'
  import type { MentionRef } from '../../types/brain'
  import type { Dashboard, SavedQuery } from '../../types/api'
  import type { Model } from '../../types/models'
  import type { Pipeline } from '../../types/pipelines'
  import { Database, LayoutDashboard, GitBranch, Box, FileText, Loader2 } from 'lucide-svelte'

  interface Props {
    query: string
    onSelect: (mention: MentionRef) => void
    onNavigate: (newQuery: string) => void
    onDismiss: () => void
  }

  let { query, onSelect, onNavigate, onDismiss }: Props = $props()

  let selectedIndex = $state(0)
  let loadingDb = $state<string | null>(null)
  let listEl: HTMLDivElement | undefined = $state()

  let dashboards = $state<Dashboard[]>([])
  let savedQueries = $state<SavedQuery[]>([])
  let models = $state<Model[]>([])
  let pipelines = $state<Pipeline[]>([])
  let entitiesLoaded = $state(false)

  onMount(async () => {
    const [d, q, m, p] = await Promise.all([
      listWorkspaceDashboards().catch((): Dashboard[] => []),
      listWorkspaceSavedQueries().catch((): SavedQuery[] => []),
      listModels().then(r => r.models ?? []).catch((): Model[] => []),
      listPipelines().then(r => r.pipelines ?? []).catch((): Pipeline[] => []),
    ])
    dashboards = d
    savedQueries = q
    models = m
    pipelines = p
    entitiesLoaded = true
  })

  type CategoryKey = 'tables' | 'dashboards' | 'pipelines' | 'models' | 'queries'

  const CATEGORIES: { key: CategoryKey; label: string; icon: typeof Database }[] = [
    { key: 'tables', label: 'Tables', icon: Database },
    { key: 'dashboards', label: 'Dashboards', icon: LayoutDashboard },
    { key: 'pipelines', label: 'Pipelines', icon: GitBranch },
    { key: 'models', label: 'Models', icon: Box },
    { key: 'queries', label: 'Saved Queries', icon: FileText },
  ]

  interface Option {
    key: string
    label: string
    hint?: string
    icon: typeof Database
    action: () => void
  }

  function matchCategory(text: string): CategoryKey | undefined {
    const lower = text.toLowerCase()
    return CATEGORIES.find(c => c.key === lower)?.key
  }

  const options = $derived.by<Option[]>(() => {
    const q = query
    const slashIdx = q.indexOf('/')

    if (slashIdx === -1) {
      const filter = q.toLowerCase()
      return CATEGORIES
        .filter(c => !filter || c.label.toLowerCase().includes(filter) || c.key.includes(filter))
        .map(c => ({
          key: c.key,
          label: c.label,
          hint: categoryHint(c.key),
          icon: c.icon,
          action: () => onNavigate(c.key + '/'),
        }))
    }

    const catText = q.slice(0, slashIdx)
    const cat = matchCategory(catText)
    if (!cat) return []
    const rest = q.slice(slashIdx + 1)

    if (cat === 'tables') return buildTableOptions(rest)
    return buildEntityOptions(cat, rest)
  })

  function categoryHint(key: CategoryKey): string {
    switch (key) {
      case 'tables': return `${getDatabases().length} databases`
      case 'dashboards': return entitiesLoaded ? `${dashboards.length}` : '...'
      case 'pipelines': return entitiesLoaded ? `${pipelines.length}` : '...'
      case 'models': return entitiesLoaded ? `${models.length}` : '...'
      case 'queries': return entitiesLoaded ? `${savedQueries.length}` : '...'
    }
  }

  function buildTableOptions(rest: string): Option[] {
    const slashIdx = rest.indexOf('/')

    if (slashIdx === -1) {
      const filter = rest.toLowerCase()
      const dbs = getDatabases()
      const results: Option[] = []
      for (const db of dbs) {
        if (filter && !db.name.toLowerCase().includes(filter)) continue
        if (!db.tables || db.tables.length === 0) {
          results.push({
            key: `db:${db.name}`,
            label: db.name,
            hint: 'load tables...',
            icon: Database,
            action: async () => {
              loadingDb = db.name
              await loadTables(db.name)
              loadingDb = null
              onNavigate(`tables/${db.name}/`)
            },
          })
        } else {
          results.push({
            key: `db:${db.name}`,
            label: db.name,
            hint: `${db.tables.length} tables`,
            icon: Database,
            action: () => onNavigate(`tables/${db.name}/`),
          })
        }
        if (results.length >= 30) break
      }
      return results
    }

    const dbName = rest.slice(0, slashIdx)
    const tableFilter = rest.slice(slashIdx + 1).toLowerCase()
    const db = getDatabases().find(d => d.name === dbName)
    if (!db?.tables) return []

    return db.tables
      .filter(t => !tableFilter || t.name.toLowerCase().includes(tableFilter))
      .slice(0, 50)
      .map(t => ({
        key: `table:${dbName}.${t.name}`,
        label: t.name,
        hint: dbName,
        icon: Database,
        action: () => onSelect({ type: 'table', database: dbName, table: t.name }),
      }))
  }

  function buildEntityOptions(cat: CategoryKey, filter: string): Option[] {
    const f = filter.toLowerCase()

    if (cat === 'dashboards') {
      return dashboards
        .filter(d => !f || d.name.toLowerCase().includes(f))
        .slice(0, 30)
        .map(d => ({
          key: `dash:${d.id}`,
          label: d.name,
          hint: d.description ?? undefined,
          icon: LayoutDashboard,
          action: () => onSelect({ type: 'dashboard', id: d.id, name: d.name }),
        }))
    }
    if (cat === 'pipelines') {
      return pipelines
        .filter(p => !f || p.name.toLowerCase().includes(f))
        .slice(0, 30)
        .map(p => ({
          key: `pipe:${p.id}`,
          label: p.name,
          hint: p.status,
          icon: GitBranch,
          action: () => onSelect({ type: 'pipeline', id: p.id, name: p.name }),
        }))
    }
    if (cat === 'models') {
      return models
        .filter(m => !f || m.name.toLowerCase().includes(f))
        .slice(0, 30)
        .map(m => ({
          key: `model:${m.id}`,
          label: m.name,
          hint: m.materialization,
          icon: Box,
          action: () => onSelect({ type: 'model', id: m.id, name: m.name }),
        }))
    }
    if (cat === 'queries') {
      return savedQueries
        .filter(q => !f || q.name.toLowerCase().includes(f))
        .slice(0, 30)
        .map(q => ({
          key: `query:${q.id}`,
          label: q.name,
          hint: q.description ?? undefined,
          icon: FileText,
          action: () => onSelect({ type: 'saved_query', id: q.id, name: q.name }),
        }))
    }
    return []
  }

  $effect(() => {
    void options
    selectedIndex = 0
  })

  function scrollSelectedIntoView() {
    if (!listEl) return
    const buttons = listEl.querySelectorAll('button')
    buttons[selectedIndex]?.scrollIntoView({ block: 'nearest' })
  }

  export function handleKeydown(e: KeyboardEvent): boolean {
    if (options.length === 0) return false

    if (e.key === 'ArrowDown') {
      e.preventDefault()
      selectedIndex = (selectedIndex + 1) % options.length
      scrollSelectedIntoView()
      return true
    }
    if (e.key === 'ArrowUp') {
      e.preventDefault()
      selectedIndex = (selectedIndex - 1 + options.length) % options.length
      scrollSelectedIntoView()
      return true
    }
    if (e.key === 'Enter' || e.key === 'Tab') {
      e.preventDefault()
      const opt = options[selectedIndex]
      if (opt) opt.action()
      return true
    }
    if (e.key === 'Escape') {
      e.preventDefault()
      onDismiss()
      return true
    }
    return false
  }
</script>

{#if options.length > 0 || (!entitiesLoaded && query.includes('/'))}
  <div class="absolute bottom-full left-0 right-0 mb-1 max-h-60 overflow-auto rounded-lg border border-gray-200 dark:border-gray-700 bg-white dark:bg-gray-900 shadow-lg z-50" bind:this={listEl}>
    {#if !entitiesLoaded && query.includes('/') && options.length === 0}
      <div class="px-3 py-2 text-xs text-muted-foreground flex items-center gap-2">
        <Loader2 size={12} class="animate-spin" />
        Loading...
      </div>
    {/if}
    {#each options as opt, i (opt.key)}
      <button
        class="w-full text-left px-3 py-1.5 text-sm flex items-center gap-2 transition-colors
          {i === selectedIndex ? 'bg-ch-blue/10 text-ch-blue' : 'text-gray-700 dark:text-gray-300 hover:bg-gray-100 dark:hover:bg-gray-800'}"
        onmouseenter={() => selectedIndex = i}
        onclick={() => opt.action()}
      >
        {#if loadingDb && opt.key === `db:${loadingDb}`}
          <Loader2 size={14} class="animate-spin shrink-0" />
        {:else}
          <opt.icon size={14} class="shrink-0 opacity-50" />
        {/if}
        <span class="truncate">{opt.label}</span>
        {#if opt.hint}
          <span class="ml-auto text-[10px] text-muted-foreground truncate max-w-[40%]">{opt.hint}</span>
        {/if}
      </button>
    {/each}
  </div>
{/if}
