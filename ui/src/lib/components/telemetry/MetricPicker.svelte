<script lang="ts">
  import { tick } from 'svelte'
  import { Search, ChevronDown } from 'lucide-svelte'
  import Badge from '../common/Badge.svelte'
  import type { MetricCatalogEntry, MetricType } from '../../types/telemetry'

  /**
   * Metric chooser: a search box over the catalog, grouped by metric type,
   * with unit, description and series count on each row.
   */
  interface Props {
    catalog: MetricCatalogEntry[]
    value: string
    type: MetricType | ''
    loading?: boolean
    onchange: (entry: MetricCatalogEntry) => void
  }

  let { catalog, value, type, loading = false, onchange }: Props = $props()

  let open = $state(false)
  let query = $state('')
  let highlighted = $state(0)
  let inputEl: HTMLInputElement | undefined = $state()
  let rootEl: HTMLDivElement | undefined = $state()

  const TYPE_ORDER: MetricType[] = ['gauge', 'sum', 'histogram', 'exponential_histogram', 'summary']
  const TYPE_LABEL: Record<MetricType, string> = {
    gauge: 'Gauges',
    sum: 'Sums',
    histogram: 'Histograms',
    exponential_histogram: 'Exponential histograms',
    summary: 'Summaries',
  }
  const TYPE_TONE: Record<MetricType, 'neutral' | 'info' | 'brand' | 'warning' | 'success'> = {
    gauge: 'info',
    sum: 'brand',
    histogram: 'success',
    exponential_histogram: 'warning',
    summary: 'neutral',
  }

  const selected = $derived(catalog.find((m) => m.name === value && m.type === type))

  const filtered = $derived.by(() => {
    const term = query.trim().toLowerCase()
    if (!term) return catalog
    return catalog.filter((m) => `${m.name} ${m.description} ${m.unit} ${m.type}`.toLowerCase().includes(term))
  })

  const groups = $derived.by(() =>
    TYPE_ORDER.map((t) => ({ type: t, items: filtered.filter((m) => m.type === t) })).filter((g) => g.items.length > 0),
  )

  // Flat list in render order, for keyboard navigation.
  const flat = $derived(groups.flatMap((g) => g.items))

  async function openMenu() {
    open = true
    query = ''
    highlighted = Math.max(0, flat.findIndex((m) => m.name === value && m.type === type))
    await tick()
    inputEl?.focus()
  }

  function closeMenu() {
    open = false
    query = ''
    highlighted = 0
  }

  function pick(m: MetricCatalogEntry) {
    closeMenu()
    onchange(m)
  }

  function onKeydown(e: KeyboardEvent) {
    if (!open) return
    if (e.key === 'Escape') {
      e.preventDefault()
      closeMenu()
    } else if (e.key === 'ArrowDown') {
      e.preventDefault()
      highlighted = Math.min(flat.length - 1, highlighted + 1)
    } else if (e.key === 'ArrowUp') {
      e.preventDefault()
      highlighted = Math.max(0, highlighted - 1)
    } else if (e.key === 'Enter' && flat[highlighted]) {
      e.preventDefault()
      pick(flat[highlighted])
    }
  }

  function onWindowPointerDown(e: PointerEvent) {
    if (!open) return
    const target = e.target as Node | null
    if (target && rootEl?.contains(target)) return
    closeMenu()
  }

  function indexOf(m: MetricCatalogEntry): number {
    return flat.findIndex((x) => x.name === m.name && x.type === m.type)
  }
</script>

<svelte:window onkeydown={onKeydown} onpointerdown={onWindowPointerDown} />

<div class="relative min-w-0" bind:this={rootEl}>
  <button
    type="button"
    class="ds-input-sm flex w-full items-center gap-2 text-left"
    onclick={open ? closeMenu : openMenu}
    disabled={loading || catalog.length === 0}
    title={selected ? `${selected.name}${selected.description ? ` · ${selected.description}` : ''}` : undefined}
  >
    {#if selected}
      <span class="truncate font-mono text-fg">{selected.name}</span>
      {#if selected.unit}<span class="shrink-0 text-fg-4">{selected.unit}</span>{/if}
    {:else}
      <span class="flex-1 truncate text-fg-4">{loading ? 'Loading metrics…' : catalog.length === 0 ? 'No metrics in range' : 'Pick a metric'}</span>
    {/if}
    <ChevronDown size={13} class="ml-auto shrink-0 text-fg-3 transition-transform {open ? 'rotate-180' : ''}" />
  </button>

  {#if open}
    <div class="absolute left-0 z-[66] mt-1 w-[26rem] max-w-[90vw] overflow-hidden rounded-md surface-card">
      <div class="flex items-center gap-2 border-b border-edge-subtle px-2.5 py-2">
        <Search size={13} class="text-fg-3" />
        <input
          bind:this={inputEl}
          bind:value={query}
          class="w-full bg-transparent text-[13px] text-fg outline-none placeholder:text-fg-4"
          placeholder="Search metrics"
          spellcheck="false"
        />
      </div>
      <div class="max-h-80 overflow-y-auto py-1" role="listbox">
        {#if flat.length === 0}
          <p class="px-3 py-3 text-xs text-fg-4">No metric matches.</p>
        {/if}
        {#each groups as g (g.type)}
          <div class="px-3 pb-1 pt-2 text-[10px] font-medium uppercase tracking-[0.1em] text-fg-4">{TYPE_LABEL[g.type]}</div>
          {#each g.items as m (m.type + ':' + m.name)}
            {@const i = indexOf(m)}
            <button
              type="button"
              role="option"
              aria-selected={m.name === value && m.type === type}
              class="flex w-full items-start gap-2 px-3 py-1.5 text-left transition-colors {i === highlighted ? 'bg-hover' : ''} hover:bg-hover"
              onmouseenter={() => (highlighted = i)}
              onclick={(e) => { e.preventDefault(); pick(m) }}
            >
              <div class="min-w-0 flex-1">
                <div class="flex items-center gap-2">
                  <span class="truncate font-mono text-xs text-fg">{m.name}</span>
                  {#if m.unit}<span class="shrink-0 text-[11px] text-fg-4">{m.unit}</span>{/if}
                </div>
                {#if m.description}
                  <p class="mt-0.5 truncate text-[11px] text-fg-3">{m.description}</p>
                {/if}
              </div>
              <div class="flex shrink-0 items-center gap-1.5">
                <span class="text-[11px] tabular-nums text-fg-4">{m.series} series</span>
                <Badge tone={TYPE_TONE[m.type]}>{m.type.replace('_', ' ')}</Badge>
              </div>
            </button>
          {/each}
        {/each}
      </div>
    </div>
  {/if}
</div>
