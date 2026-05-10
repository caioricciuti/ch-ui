<script lang="ts">
  import type { LogHistogramBucket } from '../../types/telemetry'

  interface Props {
    buckets: LogHistogramBucket[]
    loading?: boolean
  }

  let { buckets, loading = false }: Props = $props()

  const maxCount = $derived(Math.max(...buckets.map(b => Number(b.count) || 0), 1))

  const bars = $derived(buckets.map(b => ({
    time: b.bucket_time,
    count: Number(b.count) || 0,
    height: Math.max(2, ((Number(b.count) || 0) / maxCount) * 100),
  })))

  const totalCount = $derived(buckets.reduce((sum, b) => sum + (Number(b.count) || 0), 0))
</script>

<div class="w-full">
  <div class="flex items-center justify-between mb-1">
    <span class="text-[10px] text-gray-500">Log Volume</span>
    <span class="text-[10px] text-gray-400 tabular-nums">
      {totalCount.toLocaleString()} logs
    </span>
  </div>
  <div class="relative h-12 bg-gray-50 dark:bg-gray-900/50 rounded-md border border-gray-200 dark:border-gray-800 overflow-hidden">
    {#if loading}
      <div class="absolute inset-0 flex items-center justify-center">
        <span class="text-[10px] text-gray-400">Loading...</span>
      </div>
    {:else if bars.length === 0}
      <div class="absolute inset-0 flex items-center justify-center">
        <span class="text-[10px] text-gray-400">No data</span>
      </div>
    {:else}
      <div class="flex items-end h-full gap-px px-0.5 py-0.5">
        {#each bars as bar}
          <div
            class="flex-1 min-w-[2px] bg-ch-blue/60 hover:bg-ch-blue/90 rounded-t-sm transition-colors cursor-default"
            style="height: {bar.height}%"
            title="{bar.count} logs"
          ></div>
        {/each}
      </div>
    {/if}
  </div>
</div>
