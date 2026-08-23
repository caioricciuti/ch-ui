<script lang="ts">
  import type { QueryProgress, QueryStats } from '../../types/query'
  import { formatBytes, formatCompactNumber } from '../../utils/format'

  interface Props {
    /** Latest snapshot from the server; null until the first one arrives. */
    progress: QueryProgress | null
    /** Wall-clock start of the run, used to keep the timer smooth. */
    startedAt: number | null
    /** The query is still executing. */
    running?: boolean
    /** ClickHouse's accounting for a finished query. */
    stats?: QueryStats | null
    /** Client-measured duration of a finished run, in milliseconds. */
    elapsedMs?: number
  }

  let { progress, startedAt, running = false, stats = null, elapsedMs = 0 }: Props = $props()

  // Elapsed ticks locally while the query runs so the timer moves between
  // snapshots; a finished run shows its measured duration instead.
  let now = $state(Date.now())
  $effect(() => {
    if (!running) return
    const timer = setInterval(() => (now = Date.now()), 100)
    return () => clearInterval(timer)
  })

  const elapsed = $derived.by(() => {
    if (!running) return elapsedMs / 1000
    if (startedAt) return Math.max(0, (now - startedAt) / 1000)
    return progress?.elapsed ?? 0
  })

  // Short queries finish inside the sampling interval and never get a progress
  // snapshot, so the readout falls back to the final statistics.
  const readRows = $derived(running ? (progress?.read_rows ?? null) : (stats?.rows_read ?? null))
  const readBytes = $derived(running ? (progress?.read_bytes ?? null) : (stats?.bytes_read ?? null))

  // total_rows is ClickHouse's total_rows_approx: absent for sources it can't
  // size up front (system tables, remote reads), so the bar goes indeterminate.
  const rawPercent = $derived.by(() => {
    if (!running || !progress || !progress.total_rows) return null
    return Math.min(100, (progress.read_rows / progress.total_rows) * 100)
  })

  // ClickHouse revises total_rows_approx upward as it discovers parts to read,
  // so the raw ratio can drop back after climbing. Keep the highest value seen
  // in this run: a progress bar that walks backwards reads as a glitch.
  let peakPercent = $state<number | null>(null)
  let peakRun = $state<number | null>(null)
  $effect(() => {
    if (startedAt !== peakRun) {
      peakRun = startedAt
      peakPercent = null
    }
    if (rawPercent !== null && (peakPercent === null || rawPercent > peakPercent)) {
      peakPercent = rawPercent
    }
  })
  const percent = $derived(running ? peakPercent : null)

  // Rates use ClickHouse's own elapsed while the query runs so they don't drift
  // with client lag, and the measured duration once it is done. Tiny reads get
  // no rate: dividing a handful of rows by a few milliseconds is noise.
  const RATE_MIN_ROWS = 1_000
  const rateWindow = $derived(running ? (progress?.elapsed ?? 0) : elapsed)
  const showRate = $derived(readRows !== null && readRows >= RATE_MIN_ROWS && rateWindow > 0)
  const rowsPerSec = $derived(showRate && readRows !== null ? readRows / rateWindow : null)
  const bytesPerSec = $derived(showRate && readBytes !== null ? readBytes / rateWindow : null)

  const detail = $derived.by(() => {
    if (running && !progress) return 'Waiting for the first progress report from ClickHouse'

    const lines: string[] = []
    if (running && progress) {
      lines.push(
        progress.total_rows
          ? `Read ${progress.read_rows.toLocaleString()} of ~${progress.total_rows.toLocaleString()} rows`
          : `Read ${progress.read_rows.toLocaleString()} rows (total unknown for this source)`,
      )
      lines.push(`${formatBytes(progress.read_bytes)} read`)
      if (progress.memory_usage > 0) lines.push(`${formatBytes(progress.memory_usage)} memory in use`)
      lines.push(`Server elapsed: ${progress.elapsed.toFixed(3)}s`)
      return lines.join('\n')
    }

    if (readRows !== null) lines.push(`Read ${readRows.toLocaleString()} rows`)
    if (readBytes !== null) lines.push(`${formatBytes(readBytes)} read`)
    lines.push(`Took ${elapsed.toFixed(3)}s`)
    return lines.join('\n')
  })
</script>

<div
  class="relative overflow-hidden border-b border-gray-200 dark:border-gray-800 bg-gray-100/70 dark:bg-gray-900/60 shrink-0"
  role="progressbar"
  aria-label={running ? 'Query progress' : 'Query statistics'}
  aria-valuemin={0}
  aria-valuemax={100}
  aria-valuenow={running ? (percent !== null ? Math.round(percent) : undefined) : 100}
  title={detail}
>
  <!-- Fill: a measured bar when the row estimate is known, a sweep only when a
       sample arrived without one. Nothing is drawn before the first sample, so
       the bar fills once and grows instead of restarting a moment in. A
       finished query gets no fill — the numbers are the result. -->
  {#if running && percent !== null}
    <div
      class="absolute inset-y-0 left-0 bg-ch-blue/15 dark:bg-ch-blue/20 transition-[width] duration-300 ease-linear"
      style="width: {percent}%"
    ></div>
  {:else if running && progress}
    <div class="absolute inset-y-0 left-0 w-1/4 bg-ch-blue/15 dark:bg-ch-blue/20 animate-query-sweep"></div>
  {/if}

  <div class="relative flex items-center justify-between gap-2 px-2 py-1 text-[11px] text-gray-600 dark:text-gray-300">
    <span class="flex items-center gap-1.5 shrink-0 tabular-nums">
      <span
        class="size-1.5 rounded-full {running ? 'bg-ch-blue animate-pulse' : 'bg-gray-400 dark:bg-gray-600'}"
        aria-hidden="true"
      ></span>
      {elapsed.toFixed(2)}s
    </span>

    {#if readRows !== null}
      <span class="flex items-center gap-1.5 min-w-0 justify-end tabular-nums">
        {#if percent !== null}
          <span class="shrink-0 font-medium text-ch-blue">{percent.toFixed(1)}%</span>
        {/if}
        <span class="truncate text-gray-500 dark:text-gray-400">
          read {formatCompactNumber(readRows)} {readRows === 1 ? 'row' : 'rows'}{readBytes !== null ? ` · ${formatBytes(readBytes)}` : ''}
        </span>
        {#if rowsPerSec !== null && bytesPerSec !== null}
          <span class="hidden lg:inline shrink-0 text-gray-400 dark:text-gray-500">
            {formatCompactNumber(rowsPerSec)} rows/s · {formatBytes(bytesPerSec)}/s
          </span>
        {/if}
      </span>
    {:else}
      <span class="truncate text-gray-400 dark:text-gray-500">{running ? 'Running…' : 'No read statistics reported'}</span>
    {/if}
  </div>
</div>
