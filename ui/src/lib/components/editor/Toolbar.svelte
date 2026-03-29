<script lang="ts">
  import Button from '../common/Button.svelte'
  import { Play, Square, AlignLeft, BookOpen, Save, Zap } from 'lucide-svelte'
  import type { QueryEstimateResult } from '../../types/query'
  import { formatNumber } from '../../utils/format'

  interface Props {
    running?: boolean
    onrun: () => void
    oncancel?: () => void
    onformat?: () => void
    onexplain?: () => void
    onsave?: () => void
    estimate?: QueryEstimateResult | null
    estimateLoading?: boolean
  }

  let { running = false, onrun, oncancel, onformat, onexplain, onsave, estimate = null, estimateLoading = false }: Props = $props()

  const estimateLabel = $derived.by(() => {
    if (estimateLoading) return 'Estimating...'
    if (!estimate || !estimate.success || estimate.error) return null
    if (estimate.total_rows === 0 && estimate.total_parts === 0) return null
    const rows = formatNumber(estimate.total_rows)
    const parts = estimate.total_parts
    return `~${rows} rows · ${parts} part${parts !== 1 ? 's' : ''}`
  })
</script>

<div class="flex items-center gap-2 px-2 py-1.5 border-b border-gray-200 dark:border-gray-800 bg-gray-100/50 dark:bg-gray-900/50">
  {#if running && oncancel}
    <Button size="sm" variant="ghost" onclick={oncancel}>
      <Square size={14} class="text-red-400" />
      <span class="text-red-400">Cancel</span>
    </Button>
  {:else}
    <Button size="sm" onclick={onrun} loading={running}>
      <Play size={14} />
      Run
    </Button>
  {/if}

  {#if onformat}
    <Button size="sm" variant="ghost" onclick={onformat}>
      <AlignLeft size={14} />
      Format
    </Button>
  {/if}

  {#if onexplain}
    <Button size="sm" variant="ghost" onclick={onexplain}>
      <BookOpen size={14} />
      Explain
    </Button>
  {/if}

  {#if estimateLabel}
    <div class="flex items-center gap-1.5 px-2.5 py-1 rounded-md bg-ch-blue/10 dark:bg-ch-blue/15 text-ch-blue text-xs font-medium border border-ch-blue/20">
      <Zap size={12} />
      {estimateLabel}
    </div>
  {:else if estimateLoading}
    <div class="flex items-center gap-1.5 px-2.5 py-1 rounded-md bg-gray-100 dark:bg-gray-800 text-gray-500 text-xs">
      <Zap size={12} class="animate-pulse" />
      Estimating...
    </div>
  {/if}

  <div class="flex-1"></div>

  {#if onsave}
    <Button size="sm" variant="ghost" onclick={onsave}>
      <Save size={14} />
      Save
    </Button>
  {/if}
</div>
