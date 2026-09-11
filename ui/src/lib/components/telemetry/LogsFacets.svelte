<script lang="ts">
  import { ChevronRight, ChevronDown } from 'lucide-svelte'
  import Badge from '../common/Badge.svelte'
  import Spinner from '../common/Spinner.svelte'
  import type { LogFacets } from '../../types/telemetry'
  import { severityTone } from './severity'
  import { formatNumber } from '../../utils/format'

  interface Props {
    facets: LogFacets | null
    loading: boolean
    selectedSeverity: string[]
    selectedServices: string[]
    expandedKeys: string[]
    ontoggleseverity: (v: string) => void
    ontoggleservice: (v: string) => void
    onexpandkey: (key: string) => void
    onaddfilter: (key: string, value: string) => void
  }

  let {
    facets, loading, selectedSeverity, selectedServices, expandedKeys,
    ontoggleseverity, ontoggleservice, onexpandkey, onaddfilter,
  }: Props = $props()
</script>

<aside class="flex h-full min-h-0 w-60 shrink-0 flex-col overflow-y-auto border-r border-edge-subtle bg-surface">
  {#if loading && !facets}
    <div class="flex items-center justify-center py-10"><Spinner size="sm" /></div>
  {:else if facets}
    <section class="px-3 pt-3">
      <h3 class="mb-1 text-[11px] font-medium uppercase tracking-wider text-fg-4">Severity</h3>
      {#each facets.severity as f (f.value)}
        {@const active = selectedSeverity.includes(f.value)}
        <button
          class="flex h-7 w-full items-center gap-2 rounded-md px-1.5 text-xs transition-colors hover:bg-hover {active ? 'bg-active' : ''}"
          onclick={() => ontoggleseverity(f.value)}
          aria-pressed={active}
        >
          <Badge tone={severityTone(f.value)}>{f.value}</Badge>
          <span class="ml-auto tabular-nums text-fg-3">{formatNumber(f.count)}</span>
        </button>
      {/each}
      {#if facets.severity.length === 0}<p class="px-1.5 text-xs text-fg-4">None</p>{/if}
    </section>

    <section class="px-3 pt-4">
      <h3 class="mb-1 text-[11px] font-medium uppercase tracking-wider text-fg-4">Services</h3>
      {#each facets.services as f (f.value)}
        {@const active = selectedServices.includes(f.value)}
        <button
          class="flex h-7 w-full items-center gap-2 rounded-md px-1.5 text-xs transition-colors hover:bg-hover {active ? 'bg-active text-fg' : 'text-fg-2'}"
          onclick={() => ontoggleservice(f.value)}
          aria-pressed={active}
          title={f.value}
        >
          <span class="truncate">{f.value}</span>
          <span class="ml-auto shrink-0 tabular-nums text-fg-3">{formatNumber(f.count)}</span>
        </button>
      {/each}
      {#if facets.services.length === 0}<p class="px-1.5 text-xs text-fg-4">None</p>{/if}
    </section>

    <section class="px-3 pb-4 pt-4">
      <h3 class="mb-1 text-[11px] font-medium uppercase tracking-wider text-fg-4">Attributes</h3>
      {#each facets.attribute_keys as k (k.key)}
        {@const open = expandedKeys.includes(k.key)}
        <button
          class="flex h-7 w-full items-center gap-1 rounded-md px-1 text-xs text-fg-2 transition-colors hover:bg-hover"
          onclick={() => onexpandkey(k.key)}
          aria-expanded={open}
          title={`${k.key} (${k.source})`}
        >
          {#if open}<ChevronDown size={12} class="shrink-0 text-fg-4" />{:else}<ChevronRight size={12} class="shrink-0 text-fg-4" />{/if}
          <span class="truncate font-mono">{k.key}</span>
          <span class="ml-auto shrink-0 tabular-nums text-fg-3">{formatNumber(k.count)}</span>
        </button>
        {#if open}
          <div class="mb-1 ml-3 border-l border-edge pl-1">
            {#each facets.attributes[k.key] ?? [] as v (v.value)}
              <button
                class="flex h-6 w-full items-center gap-2 rounded-md px-1.5 text-[11px] text-fg-3 transition-colors hover:bg-hover hover:text-fg"
                onclick={() => onaddfilter(k.key, v.value)}
                title={`Filter ${k.key}:${v.value}`}
              >
                <span class="truncate font-mono">{v.value || '(empty)'}</span>
                <span class="ml-auto shrink-0 tabular-nums">{formatNumber(v.count)}</span>
              </button>
            {:else}
              <p class="px-1.5 py-1 text-[11px] text-fg-4">Loading…</p>
            {/each}
          </div>
        {/if}
      {/each}
      {#if facets.attribute_keys.length === 0}<p class="px-1.5 text-xs text-fg-4">None</p>{/if}
    </section>
  {/if}
</aside>
