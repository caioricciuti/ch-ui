<script lang="ts">
  import { Play, Square, AlignLeft, BookOpen, Save, Zap, Braces, History, Sparkles } from 'lucide-svelte'
  import Button from '../common/Button.svelte'

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

  // Shortcuts are bound in SqlEditor and work while the editor has focus.
  const isMac = typeof navigator !== 'undefined' && /Mac|iPhone|iPad/.test(navigator.platform)
  const runShortcut = isMac ? '⌘↵' : 'Ctrl+↵'
  const runAllShortcut = isMac ? '⇧⌘↵' : 'Shift+Ctrl+↵'
  const saveShortcut = isMac ? '⌘S' : 'Ctrl+S'
  const formatShortcut = isMac ? '⇧⌥F' : 'Shift+Alt+F'
  const cancelShortcut = isMac ? 'Esc or ⌘.' : 'Esc or Ctrl+.'

  let { running = false, onrun, oncancel, onformat, onexplain, onask, askActive = false, askPro = true, onsave, onparams, onhistory, paramCount = 0, paramsActive = false, paramsEl = $bindable(null), estimate = null, estimateLoading = false }: Props = $props()

  // Toggle buttons: ghost at rest, accent-soft while aria-pressed. The variant form wins over
  // the ghost rest colours by specificity, where a plain text-accent would lose on source order.
  const pressedClass = 'aria-pressed:bg-accent-soft aria-pressed:text-accent aria-pressed:hover:bg-accent-soft aria-pressed:hover:text-accent'

  // Parameters stays a raw <button> because the parent anchors its popover to the element
  // via bind:this, which Button does not expose. Classes mirror Button ghost sm.
  const ghostSm =
    'inline-flex h-7 shrink-0 items-center justify-center gap-1.5 whitespace-nowrap rounded-md px-2.5 text-xs font-medium text-fg-3 transition-colors duration-100 hover:bg-hover hover:text-fg disabled:pointer-events-none disabled:opacity-50'

  const estimateLabel = $derived.by(() => {
    if (estimateLoading) return 'Estimating...'
    if (!estimate || !estimate.success || estimate.error) return null
    if (estimate.total_rows === 0 && estimate.total_parts === 0) return null
    const rows = formatNumber(estimate.total_rows)
    const parts = estimate.total_parts
    return `~${rows} rows · ${parts} part${parts !== 1 ? 's' : ''}`
  })
</script>

<div class="flex h-10 shrink-0 items-center gap-2 border-b border-edge-subtle bg-surface px-4">
  <!-- Primary: the one thing you press all the time -->
  {#if running && oncancel}
    <Button variant="danger" size="sm" onclick={oncancel} title="Cancel query ({cancelShortcut})">
      <Square size={13} />
      Cancel
    </Button>
  {:else}
    <Button size="sm" onclick={onrun} title="Run the selection, or the statement under the cursor ({runShortcut}). Run all statements: {runAllShortcut}">
      <Play size={13} />
      Run
      <kbd class="ml-1 rounded-sm bg-accent-fg/20 px-1 font-sans text-[10px] font-medium leading-4 text-accent-fg/85">{runShortcut}</kbd>
    </Button>
  {/if}

  <span class="h-4 w-px bg-edge"></span>

  <!-- One-shot actions -->
  {#if onformat}
    <Button variant="ghost" size="sm" onclick={onformat} title="Format SQL ({formatShortcut})">
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
    <Button variant="ghost" size="sm" class={pressedClass} onclick={onask} aria-pressed={askActive} title="Ask AI to write or edit the query">
      <Sparkles size={13} />
      Ask AI
      {#if !askPro}
        <span class="text-[10px] font-semibold uppercase tracking-wider text-accent">Pro</span>
      {/if}
    </Button>
  {/if}
  {#if onparams && paramCount > 0}
    <button
      bind:this={paramsEl}
      type="button"
      class="{ghostSm} {pressedClass}"
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
    <span class="inline-flex items-center gap-1 text-xs tabular-nums text-fg-3" title="Estimated scan from EXPLAIN ESTIMATE">
      <Zap size={12} />
      {estimateLabel}
    </span>
  {:else if estimateLoading}
    <span class="inline-flex items-center gap-1 text-xs text-fg-4">
      <Zap size={12} class="animate-pulse" />
      Estimating…
    </span>
  {/if}

  <div class="flex-1"></div>

  <!-- Utility: history as an icon, save with a label so a first-time user finds it -->
  {#if onhistory}
    <Button icon variant="ghost" size="sm" onclick={onhistory} title="Query history" aria-label="Query history">
      <History size={14} />
    </Button>
  {/if}
  {#if onsave}
    <Button variant="ghost" size="sm" onclick={onsave} title="Save query ({saveShortcut})">
      <Save size={13} />
      Save
    </Button>
  {/if}
</div>
