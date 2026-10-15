<script lang="ts">
  import { onMount } from 'svelte'
  import { GitFork, AlertTriangle, RefreshCw } from 'lucide-svelte'
  import { fetchLineage, lineageFailure, LINEAGE_FAILURE_TITLES, type LineageFailure } from '../../api/lineage'
  import { error as toastError } from '../../stores/toast.svelte'
  import { navigate } from '../../stores/router.svelte'
  import { PAGE_ROUTES } from '../../routes'
  import { DEFAULT_HOPS, layoutLineage } from '../../utils/lineage-layout'
  import { MAX_HOPS, MIN_HOPS, buildLineageSearch } from '../../utils/lineage-url'
  import type { LineageGraph as Graph } from '../../types/lineage'
  import LineageGraph from './LineageGraph.svelte'
  import Button from '../common/Button.svelte'
  import Badge from '../common/Badge.svelte'
  import Select from '../common/Select.svelte'
  import EmptyState from '../common/EmptyState.svelte'
  import Spinner from '../common/Spinner.svelte'

  /** The Lineage view of a table page: the graph around this table, fetched when the view first opens. */
  interface Props {
    database: string
    table: string
  }

  let { database, table }: Props = $props()

  const home = $derived(`${database}.${table}`)
  let graph = $state<Graph | null>(null)
  let loading = $state(false)
  let failure = $state<LineageFailure | null>(null)
  let focus = $state<string | null>(null)
  let hops = $state(DEFAULT_HOPS)
  let expanded = $state<Set<string>>(new Set())
  let seq = 0

  const current = $derived(focus ?? home)
  const layout = $derived(graph ? layoutLineage(graph, { focus: current, up: hops, down: hops, expanded }) : null)
  const known = $derived(!!graph && graph.nodes.some((n) => n.id === current))
  const lonely = $derived(!!layout && !layout.tooLarge && layout.nodes.length <= 1)

  const hopOptions = Array.from({ length: MAX_HOPS - MIN_HOPS + 1 }, (_, i) => {
    const n = MIN_HOPS + i
    return { value: String(n), label: n === 1 ? '1 hop' : `${n} hops` }
  })

  async function load(refresh = false) {
    const mine = ++seq
    loading = true
    failure = null
    try {
      const next = await fetchLineage([database], refresh)
      if (mine === seq) graph = next
    } catch (e) {
      if (mine !== seq) return
      failure = lineageFailure(e)
      // Too many objects is a prompt to choose databases, not an error.
      if (failure.kind !== 'pick') toastError(failure.message)
    } finally {
      if (mine === seq) loading = false
    }
  }

  function expand(db: string) {
    expanded = new Set([...expanded, db])
  }

  function openPage() {
    const search = buildLineageSearch({ databases: [database], focus: current, hops })
    navigate(PAGE_ROUTES.lineage.path + search)
  }

  onMount(() => {
    void load()
  })
</script>

<div class="flex h-full min-h-0 flex-col">
  <div class="flex h-10 shrink-0 items-center gap-2 border-b border-edge-subtle px-3">
    <Select size="sm" class="w-24" options={hopOptions} value={String(hops)} onchange={(v) => (hops = Number(v))} />
    {#if focus && focus !== home}
      <Badge tone="neutral" class="font-mono">{focus}</Badge>
      <Button variant="ghost" size="xs" onclick={() => (focus = null)}>Back to {table}</Button>
    {/if}
    {#if graph?.meta.multi_node}
      <Badge tone="neutral" title="Lineage reads the metadata of the node this session is connected to">Connected node {graph.meta.host}</Badge>
    {/if}
    {#if graph && graph.meta.warnings.length > 0}
      <Badge tone="warning" title={graph.meta.warnings.join('\n')}>{graph.meta.warnings.length === 1 ? '1 warning' : `${graph.meta.warnings.length} warnings`}</Badge>
    {/if}
    <div class="ml-auto flex items-center gap-2">
      <Button variant="ghost" size="sm" disabled={loading} onclick={() => load(true)} title="Read the metadata again">
        <RefreshCw size={13} class={loading ? 'animate-spin' : ''} /> Refresh
      </Button>
      <Button variant="outline" size="sm" onclick={openPage}>Open in Lineage</Button>
    </div>
  </div>

  <div class="min-h-0 flex-1">
    {#if !graph && loading}
      <div class="flex h-full items-center justify-center"><Spinner /></div>
    {:else if !graph && failure?.kind === 'pick'}
      <EmptyState icon={GitFork} title={LINEAGE_FAILURE_TITLES.pick} description={failure.message} />
    {:else if !graph && failure}
      <EmptyState icon={AlertTriangle} title={LINEAGE_FAILURE_TITLES[failure.kind]} description={failure.message} primary={{ label: 'Retry', onclick: () => load() }} />
    {:else if layout && !known}
      <EmptyState icon={GitFork} title="Not in the lineage graph" description="{current} was not found in the metadata. It may have been dropped or renamed. Refresh to read the metadata again." />
    {:else if layout?.tooLarge}
      <EmptyState icon={GitFork} title="Narrow the view" description="{layout.count} nodes are within {hops} hops, more than the graph shows at once. Use fewer hops." />
    {:else if layout && lonely}
      <EmptyState icon={GitFork} title="No lineage for {table}" description="Nothing reads from or writes into this table that ClickHouse metadata or its definitions show." />
    {:else if layout}
      <LineageGraph nodes={layout.nodes} edges={layout.edges} onfocus={(id) => (focus = id)} onexpand={expand} />
    {/if}
  </div>
</div>
