<script lang="ts">
  import { Braces, Lock, Play } from 'lucide-svelte'
  import Button from '../common/Button.svelte'
  import type { QueryParam } from '../../utils/query-params'

  interface Props {
    params: QueryParam[]
    values: Record<string, string>
    proActive: boolean
    /** Name of the parameter to focus on open (the first missing one when Run opened us). */
    focusParam?: string | null
    x: number
    y: number
    onclose: () => void
    onrun: () => void
    onupgrade: () => void
  }

  let { params, values = $bindable(), proActive, focusParam = null, x, y, onclose, onrun, onupgrade }: Props = $props()

  let panelEl = $state<HTMLDivElement | null>(null)
  let inputs = $state<Record<string, HTMLInputElement>>({})

  const PANEL_WIDTH = 340
  const left = $derived(Math.max(8, Math.min(x, window.innerWidth - PANEL_WIDTH - 8)))
  const top = $derived(Math.min(y, window.innerHeight - 120))

  $effect(() => {
    const target = (focusParam && inputs[focusParam]) || Object.values(inputs)[0]
    target?.focus()
  })

  function handleWindowMousedown(e: MouseEvent) {
    if (panelEl && !panelEl.contains(e.target as Node)) onclose()
  }
  function handleWindowKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') onclose()
  }
</script>

<svelte:window onmousedown={handleWindowMousedown} onkeydown={handleWindowKeydown} />

<div
  bind:this={panelEl}
  class="surface-card fixed z-50 rounded-md p-3"
  style="left:{left}px;top:{top}px;width:{PANEL_WIDTH}px"
  role="dialog"
  aria-label="Query parameters"
>
  {#if proActive}
    <div class="mb-2 flex items-center gap-1.5 text-xs font-medium text-fg-2">
      <Braces size={13} class="text-accent" />
      Parameters
      <span class="font-normal text-fg-4">· {params.length}</span>
    </div>
    <div class="space-y-2">
      {#each params as p (p.name)}
        <label class="block">
          <span class="mb-1 block font-mono text-[11px] text-fg-3">{p.name}<span class="text-fg-4">:{p.type}</span></span>
          <input
            bind:this={inputs[p.name]}
            class="ds-input-sm"
            bind:value={values[p.name]}
            placeholder={p.type}
            spellcheck="false"
            onkeydown={(e) => { if (e.key === 'Enter') { e.preventDefault(); onrun() } }}
          />
        </label>
      {/each}
    </div>
    <div class="mt-3 flex items-center justify-between gap-3">
      <span class="text-[11px] text-fg-4">Bound by ClickHouse; saved with the query.</span>
      <Button size="sm" onclick={onrun}>
        <Play size={12} />
        Run
      </Button>
    </div>
  {:else}
    <div class="flex items-start gap-2 text-xs text-fg-2">
      <Lock size={14} class="mt-0.5 shrink-0 text-accent" />
      <span>
        This query uses {params.length} parameter{params.length === 1 ? '' : 's'}
        (<span class="font-mono">{params.map((p) => p.name).join(', ')}</span>).
        Query parameters are a <strong>Pro</strong> feature.
      </span>
    </div>
    <div class="mt-3 flex justify-end">
      <Button size="sm" onclick={onupgrade}>Upgrade</Button>
    </div>
  {/if}
</div>
