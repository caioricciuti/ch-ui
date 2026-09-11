<script lang="ts">
  import Badge from '../common/Badge.svelte'
  import Spinner from '../common/Spinner.svelte'
  import type { TraceFacets, FacetValue } from '../../types/telemetry'
  import { formatNumber } from '../../utils/format'

  interface Props {
    facets: TraceFacets | null
    loading: boolean
    onaddfilter: (key: string, value: string) => void
  }

  let { facets, loading, onaddfilter }: Props = $props()

  const groups = $derived<[string, string, FacetValue[]][]>(facets ? [
    ['Services', 'service', facets.services],
    ['Span names', 'span_name', facets.span_names],
    ['Status', 'status', facets.status],
    ['Kinds', 'kind', facets.kinds],
  ] : [])

  function statusTone(v: string): 'success' | 'danger' | 'neutral' {
    const u = v.toLowerCase()
    if (u === 'error') return 'danger'
    if (u === 'ok') return 'success'
    return 'neutral'
  }
</script>

<aside class="flex h-full min-h-0 w-60 shrink-0 flex-col overflow-y-auto border-r border-edge-subtle bg-surface">
  {#if loading && !facets}
    <div class="flex items-center justify-center py-10"><Spinner size="sm" /></div>
  {:else if facets}
    {#each groups as [title, key, values] (key)}
      <section class="px-3 pt-3 last:pb-4">
        <h3 class="mb-1 text-[11px] font-medium uppercase tracking-wider text-fg-4">{title}</h3>
        {#each values as f (f.value)}
          <button
            class="flex h-7 w-full items-center gap-2 rounded-md px-1.5 text-xs text-fg-2 transition-colors hover:bg-hover hover:text-fg"
            onclick={() => onaddfilter(key, f.value)}
            title={`Filter ${key}:${f.value}`}
          >
            {#if key === 'status'}
              <Badge tone={statusTone(f.value)}>{f.value || '—'}</Badge>
            {:else}
              <span class="truncate {key === 'span_name' ? 'font-mono' : ''}">{f.value || '(empty)'}</span>
            {/if}
            <span class="ml-auto shrink-0 tabular-nums text-fg-3">{formatNumber(f.count)}</span>
          </button>
        {:else}
          <p class="px-1.5 text-xs text-fg-4">None</p>
        {/each}
      </section>
    {/each}
  {/if}
</aside>
