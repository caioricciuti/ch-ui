<script lang="ts">
  import { onMount } from 'svelte'
  import { GitFork, AlertTriangle, RefreshCw, Search, X } from 'lucide-svelte'
  import { fetchLineage, lineageFailure, LINEAGE_FAILURE_TITLES, type LineageFailure } from '../lib/api/lineage'
  import { listDatabases } from '../lib/api/query'
  import { error as toastError, warning as toastWarning } from '../lib/stores/toast.svelte'
  import { formatDate } from '../lib/utils/format'
  import { DEFAULT_HOPS, isolatedCount, KIND_LABELS, layoutLineage, MAX_VISIBLE_NODES } from '../lib/utils/lineage-layout'
  import { buildLineageSearch, databaseOf, MAX_HOPS, MIN_HOPS, parseLineageSearch } from '../lib/utils/lineage-url'
  import { LINEAGE_NODE_KINDS, type LineageGraph as Graph, type LineageNodeKind } from '../lib/types/lineage'
  import PageHeader from '../lib/components/common/PageHeader.svelte'
  import Button from '../lib/components/common/Button.svelte'
  import Badge from '../lib/components/common/Badge.svelte'
  import Select from '../lib/components/common/Select.svelte'
  import EmptyState from '../lib/components/common/EmptyState.svelte'
  import Spinner from '../lib/components/common/Spinner.svelte'
  import CheckMenu from '../lib/components/lineage/CheckMenu.svelte'
  import LineageGraph from '../lib/components/lineage/LineageGraph.svelte'

  const SYSTEM_DATABASES = new Set(['system', 'INFORMATION_SCHEMA', 'information_schema'])
  // With no databases in the URL, start with every user database when there are this few.
  const AUTO_SELECT_LIMIT = 10

  let allDatabases = $state<string[]>([])
  let databases = $state<string[]>([])
  let focus = $state<string | null>(null)
  let hops = $state(DEFAULT_HOPS)
  let hiddenKinds = $state<Set<LineageNodeKind>>(new Set())
  let expanded = $state<Set<string>>(new Set())
  let center = $state<{ id: string; seq: number } | null>(null)
  let search = $state('')

  let graph = $state<Graph | null>(null)
  let loading = $state(false)
  let failure = $state<LineageFailure | null>(null)
  let booting = $state(true)
  let seq = 0

  const presentKinds = $derived.by(() => {
    if (!graph) return [] as LineageNodeKind[]
    const seen = new Set(graph.nodes.map((n) => n.kind))
    return LINEAGE_NODE_KINDS.filter((k) => seen.has(k))
  })
  const kindFilter = $derived.by(() => {
    if (hiddenKinds.size === 0) return null
    return new Set(presentKinds.filter((k) => !hiddenKinds.has(k)))
  })

  // Layout runs here, on the last response: refocus, hops, kinds and
  // expanding a database never refetch.
  const layout = $derived(
    graph
      ? layoutLineage(graph, { focus, up: hops, down: hops, kinds: kindFilter, expanded, hideIsolated: true })
      : null,
  )
  const isolated = $derived(graph && !focus ? isolatedCount(graph) : 0)
  const nodeIds = $derived(graph ? graph.nodes.map((n) => n.id) : [])

  const databaseOptions = $derived(allDatabases.map((d) => ({ value: d, label: d })))
  const databaseLabel = $derived(
    databases.length === 0 ? 'Pick databases' : databases.length <= 2 ? databases.join(', ') : `${databases.length} databases`,
  )
  const kindOptions = $derived(presentKinds.map((k) => ({ value: k, label: KIND_LABELS[k] })))
  const kindLabel = $derived(hiddenKinds.size === 0 ? 'All kinds' : `${kindFilter?.size ?? 0} of ${presentKinds.length} kinds`)
  const hopOptions = Array.from({ length: MAX_HOPS - MIN_HOPS + 1 }, (_, i) => {
    const n = MIN_HOPS + i
    return { value: String(n), label: n === 1 ? '1 hop' : `${n} hops` }
  })

  const countLabel = $derived.by(() => {
    if (!graph || !layout) return ''
    const total = graph.nodes.length
    if (layout.tooLarge) return `${layout.count} of ${total} nodes`
    const extra = isolated > 0 ? `, ${isolated} without lineage hidden` : ''
    return `${layout.nodes.length} of ${total} nodes${extra}`
  })

  function writeUrl() {
    const next = buildLineageSearch({ databases, focus, hops }, window.location.search)
    if (next === window.location.search) return
    history.replaceState(history.state, '', window.location.pathname + next + window.location.hash)
  }

  async function load(refresh = false) {
    if (databases.length === 0) {
      graph = null
      return
    }
    const mine = ++seq
    loading = true
    failure = null
    try {
      const next = await fetchLineage(databases, refresh)
      if (mine !== seq) return
      graph = next
      if (focus && !next.nodes.some((n) => n.id === focus)) {
        toastWarning(`${focus} is not in the selected databases`)
        focus = null
        center = null
        writeUrl()
      }
    } catch (e) {
      if (mine !== seq) return
      failure = lineageFailure(e)
      // Too many objects is a prompt to choose databases, not an error.
      if (failure.kind !== 'pick') toastError(failure.message)
      else if (graph) toastWarning(failure.message)
    } finally {
      if (mine === seq) loading = false
    }
  }

  function setDatabases(values: string[]) {
    databases = values
    expanded = new Set()
    // The old graph belongs to the old selection; do not show it if this fetch fails.
    graph = null
    writeUrl()
    void load()
  }

  function setFocus(id: string | null, pan = false) {
    focus = id
    center = id && pan ? { id, seq: (center?.seq ?? 0) + 1 } : null
    writeUrl()
  }

  function setHops(value: string) {
    hops = Number(value)
    writeUrl()
  }

  function setKinds(values: string[]) {
    hiddenKinds = new Set(presentKinds.filter((k) => !values.includes(k)))
  }

  function expand(database: string) {
    expanded = new Set([...expanded, database])
  }

  /** Exact id first, then the first id containing the text. */
  function runSearch() {
    const term = search.trim().toLowerCase()
    if (!term || !graph) return
    const hit = nodeIds.find((id) => id.toLowerCase() === term) ?? nodeIds.find((id) => id.toLowerCase().includes(term))
    if (!hit) {
      toastWarning(`Nothing matches "${search.trim()}"`)
      return
    }
    search = hit
    setFocus(hit, true)
  }

  onMount(() => {
    const url = parseLineageSearch(window.location.search)
    focus = url.focus
    hops = url.hops
    databases = url.databases.length > 0 ? url.databases : url.focus && databaseOf(url.focus) ? [databaseOf(url.focus)] : []
    if (url.focus) center = { id: url.focus, seq: 1 }

    listDatabases().then(
      (list) => {
        allDatabases = list
        if (databases.length === 0) {
          const user = list.filter((d) => !SYSTEM_DATABASES.has(d))
          databases = user.length <= AUTO_SELECT_LIMIT ? user : user.includes('default') ? ['default'] : user.slice(0, 1)
        }
        booting = false
        void load()
      },
      (e: unknown) => {
        booting = false
        // The graph still works for databases named in the URL.
        toastError(e instanceof Error ? e.message : String(e))
        allDatabases = [...databases]
        void load()
      },
    )
  })
</script>

{#snippet headerMeta()}
  {#if graph?.meta.multi_node}
    <Badge tone="neutral" title="Lineage reads the metadata of the node this session is connected to">Connected node {graph.meta.host}</Badge>
  {/if}
  {#if graph && graph.meta.warnings.length > 0}
    <Badge tone="warning" title={graph.meta.warnings.join('\n')}>
      {graph.meta.warnings.length === 1 ? '1 warning' : `${graph.meta.warnings.length} warnings`}
    </Badge>
  {/if}
  {#if graph?.meta.cached}
    <Badge tone="neutral" title="Read from the server cache. Refresh reads the metadata again.">Cached {formatDate(graph.meta.generated_at)}</Badge>
  {/if}
{/snippet}

<div class="flex h-full min-h-0 flex-col">
  <PageHeader title="Lineage" subtitle="How data flows between tables" meta={headerMeta} />

  <div class="flex h-10 shrink-0 items-center gap-2 border-b border-edge-subtle px-5">
    <CheckMenu label={databaseLabel} options={databaseOptions} selected={databases} searchable onapply={setDatabases} />
    <div class="relative">
      <Search size={13} class="pointer-events-none absolute left-2.5 top-1/2 -translate-y-1/2 text-fg-4" />
      <input
        class="ds-input-sm w-64 pl-8 pr-7"
        type="search"
        placeholder="Find a table to focus"
        list="lineage-node-ids"
        spellcheck={false}
        aria-label="Find a table to focus"
        disabled={!graph}
        bind:value={search}
        onchange={runSearch}
        onkeydown={(e) => { if (e.key === 'Enter') runSearch() }}
      />
      <datalist id="lineage-node-ids">
        {#each nodeIds as id (id)}<option value={id}></option>{/each}
      </datalist>
    </div>
    <Select
      size="sm"
      class="w-24"
      options={hopOptions}
      value={String(hops)}
      disabled={!focus}
      onchange={setHops}
    />
    <CheckMenu label={kindLabel} options={kindOptions} selected={presentKinds.filter((k) => !hiddenKinds.has(k))} class="w-40" onapply={setKinds} />
    {#if focus}
      <Badge tone="neutral" class="pr-0.5">
        <span class="font-mono">{focus}</span>
        <button class="rounded-sm p-0.5 hover:bg-hover" onclick={() => { search = ''; setFocus(null) }} aria-label="Clear focus" title="Show the whole graph">
          <X size={10} />
        </button>
      </Badge>
    {/if}
    <div class="ml-auto flex items-center gap-3">
      {#if countLabel}<span class="text-xs text-fg-4 tabular-nums">{countLabel}</span>{/if}
      <Button variant="ghost" size="sm" disabled={loading || databases.length === 0} onclick={() => load(true)} title="Read the metadata again">
        <RefreshCw size={13} class={loading ? 'animate-spin' : ''} /> Refresh
      </Button>
    </div>
  </div>

  <div class="min-h-0 flex-1">
    {#if booting || (!graph && loading)}
      <div class="flex h-full items-center justify-center text-fg-3" aria-busy="true"><Spinner /></div>
    {:else if databases.length === 0}
      <EmptyState icon={GitFork} title="Pick databases" description="Choose one or more databases in the toolbar to see how data flows between their tables." />
    {:else if !graph && failure?.kind === 'pick'}
      <EmptyState icon={GitFork} title={LINEAGE_FAILURE_TITLES.pick} description="{failure.message} Choose fewer databases in the toolbar to map them." />
    {:else if !graph && failure}
      <EmptyState icon={AlertTriangle} title={LINEAGE_FAILURE_TITLES[failure.kind]} description={failure.message} primary={{ label: 'Retry', onclick: () => load() }} />
    {:else if layout?.tooLarge}
      <EmptyState
        icon={GitFork}
        title="Narrow the view"
        description="{layout.count} nodes is more than the {MAX_VISIBLE_NODES} the graph draws at once. Pick fewer databases, search for a table to focus on{focus ? ', or use fewer hops' : ''}."
      />
    {:else if layout && layout.nodes.length === 0}
      <EmptyState
        icon={GitFork}
        title="No lineage here"
        description={graph && graph.nodes.length > 0
          ? 'None of these objects reads from or writes into another. Pick more databases or another kind filter.'
          : 'No tables, views or dictionaries were found in the selected databases.'}
      />
    {:else if layout}
      <LineageGraph nodes={layout.nodes} edges={layout.edges} {center} onfocus={(id) => setFocus(id)} onexpand={expand} />
    {/if}
  </div>
</div>
