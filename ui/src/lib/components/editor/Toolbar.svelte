<script lang="ts">
  import { Play, Square, AlignLeft, BookOpen, Save, Zap, Braces, History, Sparkles } from 'lucide-svelte'
  import Button from '../common/Button.svelte'

  const isMac = typeof navigator !== 'undefined' && /Mac|iPhone|iPad/.test(navigator.platform)
  const runShortcut = isMac ? '⌘↵' : 'Ctrl+↵'
  import type { QueryEstimateResult } from '../../types/query'
  import { formatNumber } from '../../utils/format'

  interface Props {
    running?: boolean
    onrun: () => void
    oncancel?: () => void
    onformat?: () => void
    onexplain?: () => void
    onask?: () => void
    askActive?: boolean
    askPro?: boolean
    onsave?: () => void
    onparams?: () => void
    onhistory?: () => void
    paramCount?: number
    paramsActive?: boolean
    /** The Parameters button element, so the parent can anchor its popover to it. */
    paramsEl?: HTMLButtonElement | null
    estimate?: QueryEstimateResult | null
    estimateLoading?: boolean
  }

  let { running = false, onrun, oncancel, onformat, onexplain, onask, askActive = false, askPro = true, onsave, onparams, onhistory, paramCount = 0, paramsActive = false, paramsEl = $bindable(null), estimate = null, estimateLoading = false }: Props = $props()

  const estimateLabel = $derived.by(() => {
    if (estimateLoading) return 'Estimating...'
    if (!estimate || !estimate.success || estimate.error) return null
    if (estimate.total_rows === 0 && estimate.total_parts === 0) return null
    const rows = formatNumber(estimate.total_rows)
    const parts = estimate.total_parts
    return `~${rows} rows · ${parts} part${parts !== 1 ? 's' : ''}`
  })
</script>

<div class="flex h-10 items-center gap-1 border-b border-edge-subtle bg-surface px-2">
  <!-- Primary: the one thing you press all the time -->
  {#if running && oncancel}
    <button class="inline-flex h-7 items-center gap-1.5 rounded-md bg-danger-soft px-3 text-[13px] font-medium text-danger transition-colors hover:brightness-110" onclick={oncancel}>
      <Square size={13} />
      Cancel
    </button>
  {:else}
    <Button size="sm" onclick={onrun} title="Run query ({runShortcut})">
      <Play size={13} />
      Run
      <kbd class="ml-1 rounded-sm bg-black/15 px-1 font-sans text-[10px] font-medium leading-4 text-accent-fg/85">{runShortcut}</kbd>
    </Button>
  {/if}

  <span class="mx-1 h-4 w-px bg-edge"></span>

  <!-- One-shot actions -->
  {#if onformat}
    <Button variant="ghost" size="sm" onclick={onformat} title="Format SQL">
      <AlignLeft size={13} />
      Format
    </Button>
  {/if}
  {#if onexplain}
    <Button variant="ghost" size="sm" onclick={onexplain} title="Explain query plan">
      <BookOpen size={13} />
      Explain
    </Button>
  {/if}

  <!-- Modes: pressed state so they read as toggles -->
  {#if onask}
    <button
      class="inline-flex h-7 items-center gap-1.5 rounded-md px-2 text-xs font-medium transition-colors {askActive ? 'bg-accent-soft text-accent' : 'text-fg-3 hover:bg-hover hover:text-fg'}"
      onclick={onask}
      aria-pressed={askActive}
      title="Ask AI to write or edit the query"
    >
      <Sparkles size={13} />
      Ask AI
      {#if !askPro}
        <span class="text-[10px] font-semibold uppercase tracking-wider text-accent">Pro</span>
      {/if}
    </button>
  {/if}
  {#if onparams && paramCount > 0}
    <button
      bind:this={paramsEl}
      class="inline-flex h-7 items-center gap-1.5 rounded-md px-2 text-xs font-medium transition-colors {paramsActive ? 'bg-accent-soft text-accent' : 'text-fg-3 hover:bg-hover hover:text-fg'}"
      onclick={onparams}
      aria-pressed={paramsActive}
      title="Set values for the {'{name:Type}'} parameters in this query"
    >
      <Braces size={13} />
      Parameters
      <span class="inline-flex h-4 min-w-4 items-center justify-center rounded-full bg-accent/15 px-1 text-[10px] font-semibold tabular-nums text-accent">{paramCount}</span>
    </button>
  {/if}

  <!-- Readout, not a control -->
  {#if estimateLabel}
    <span class="ml-2 inline-flex items-center gap-1 text-xs text-fg-3" title="Estimated scan from EXPLAIN ESTIMATE">
      <Zap size={12} />
      {estimateLabel}
    </span>
  {:else if estimateLoading}
    <span class="ml-2 inline-flex items-center gap-1 text-xs text-fg-4">
      <Zap size={12} class="animate-pulse" />
      Estimating…
    </span>
  {/if}

  <div class="flex-1"></div>

  <!-- Utility: icons only -->
  {#if onhistory}
    <Button icon variant="ghost" size="sm" onclick={onhistory} title="Query history" aria-label="Query history">
      <History size={14} />
    </Button>
  {/if}
  {#if onsave}
    <Button icon variant="ghost" size="sm" onclick={onsave} title="Save query" aria-label="Save query">
      <Save size={14} />
    </Button>
  {/if}
</div>
