<script lang="ts">
  import { errorText } from '../../utils/errors'
  import { onMount } from 'svelte'
  import Stat from '../common/Stat.svelte'
  import Badge from '../common/Badge.svelte'
  import Skeleton from '../common/Skeleton.svelte'
  import { getIngestStats } from '../../api/ingest'
  import { formatAge, formatRate, rejectedEntries, type IngestStats } from '../../types/ingest'
  import { formatBytes, formatNumber, formatRelativeTime } from '../../utils/format'
  import { Gauge, Archive, Ban } from 'lucide-svelte'

  interface Props {
    pipelineId: string
    /** One line for the status bar instead of the tile grid. */
    compact?: boolean
  }

  let { pipelineId, compact = false }: Props = $props()

  let stats = $state<IngestStats | null>(null)
  let error = $state('')

  const rejected = $derived(rejectedEntries(stats?.rejected))
  const spoolTone = $derived(stats && stats.spool.oldest_age_ms > 60_000 ? 'warning' : 'default')

  async function fetchStats() {
    if (typeof document !== 'undefined' && document.hidden) return
    try {
      stats = await getIngestStats(pipelineId)
      error = ''
    } catch (e: unknown) {
      error = errorText(e)
    }
  }

  onMount(() => {
    void fetchStats()
    const timer = setInterval(fetchStats, 5000)
    const onVisible = () => {
      if (!document.hidden) void fetchStats()
    }
    document.addEventListener('visibilitychange', onVisible)
    return () => {
      clearInterval(timer)
      document.removeEventListener('visibilitychange', onVisible)
    }
  })
</script>

{#if compact}
  {#if stats}
    <span class="flex items-center gap-1" title="Records received per second, last minute ({formatBytes(stats.rate.bytes_per_sec_1m)}/s)">
      <Gauge size={12} />
      {formatRate(stats.rate.records_per_sec_1m)} in
    </span>
    <span class="flex items-center gap-1 {spoolTone === 'warning' ? 'text-warning' : ''}" title="Spooled, not yet written: {formatNumber(stats.spool.segments)} segments">
      <Archive size={12} />
      spool {formatBytes(stats.spool.bytes)}{stats.spool.bytes > 0 ? `, oldest ${formatAge(stats.spool.oldest_age_ms)}` : ''}
    </span>
    {#if rejected.length > 0}
      <span class="flex items-center gap-1 text-warning" title="Rejected requests since the server started">
        <Ban size={12} />
        {rejected.map((r) => `${formatNumber(r.count)} ${r.label}`).join(', ')}
      </span>
    {/if}
  {/if}
{:else}
  <section class="space-y-3">
    <div class="flex items-center justify-between gap-2">
      <h3 class="text-[13px] font-semibold text-fg">Ingest</h3>
      <span class="text-xs text-fg-4">Since the server started. Updates every 5 s.</span>
    </div>
    {#if error && !stats}
      <p class="text-[13px] text-danger">{error}</p>
    {:else if stats}
      <div class="grid grid-cols-2 gap-2 md:grid-cols-4">
        <Stat variant="muted" label="Received" value={formatRate(stats.rate.records_per_sec_1m)} hint="{formatNumber(stats.received.records)} records, {formatBytes(stats.rate.bytes_per_sec_1m)}/s" />
        <Stat variant="muted" label="Written rows" value={formatNumber(stats.written.rows)} hint={stats.written.last_write_at ? `last write ${formatRelativeTime(stats.written.last_write_at)}` : 'nothing written yet'} />
        <Stat variant="muted" label="Spool" value={formatBytes(stats.spool.bytes)} tone={spoolTone} hint={stats.spool.bytes > 0 ? `oldest ${formatAge(stats.spool.oldest_age_ms)}, ${formatNumber(stats.spool.segments)} segments` : 'empty'} />
        <Stat variant="muted" label="Requests" value={formatNumber(stats.received.requests)} hint="{formatBytes(stats.received.bytes)} received" />
      </div>
      {#if rejected.length > 0}
        <div class="flex flex-wrap items-center gap-1.5 text-xs text-fg-3">
          <span>Rejected:</span>
          {#each rejected as r (r.reason)}
            <Badge tone={r.reason === 'spool_full' || r.reason === 'malformed' ? 'danger' : 'warning'}>{formatNumber(r.count)} {r.label}</Badge>
          {/each}
        </div>
      {/if}
      {#if stats.last_error}
        <p class="break-words rounded-md bg-danger-soft px-3 py-2 text-[13px] text-danger">{stats.last_error}</p>
      {/if}
    {:else}
      <Skeleton variant="block" label="Loading ingest stats" />
    {/if}
  </section>
{/if}
