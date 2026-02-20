<script lang="ts">
  import type { ColumnMeta, QueryPlanNode, QueryStats } from '../../types/query'
  import { getDisplayType } from '../../utils/ch-types'
  import { formatBytes, formatElapsed, formatNumber } from '../../utils/format'
  import Combobox from '../common/Combobox.svelte'
  import type { ComboboxOption } from '../common/Combobox.svelte'
  import Spinner from '../common/Spinner.svelte'
  import { Activity, Layers3, AreaChart, GitBranch, Gauge, RefreshCw, FlaskConical } from 'lucide-svelte'

  interface Props {
    meta: ColumnMeta[]
    data: unknown[][]
    stats?: QueryStats | null
    elapsedMs?: number
    running?: boolean
    streamRows?: number
    streamChunks?: number
    streamStartedAt?: number | null
    streamLastChunkAt?: number | null
    planNodes?: QueryPlanNode[]
    planLines?: string[]
    planSource?: string
    planLoading?: boolean
    planError?: string | null
    onLoadPlan?: () => void
    onSample?: (perShard: number) => void
    profile?: Record<string, unknown> | null
    profileAvailable?: boolean
    profileReason?: string | null
    profileLoading?: boolean
    profileError?: string | null
    samplingMode?: string | null
  }

  let {
    meta,
    data,
    stats = null,
    elapsedMs = 0,
    running = false,
    streamRows = 0,
    streamChunks = 0,
    streamStartedAt = null,
    streamLastChunkAt = null,
    planNodes = [],
    planLines = [],
    planSource = '',
    planLoading = false,
    planError = null,
    onLoadPlan,
    onSample,
    profile = null,
    profileAvailable = false,
    profileReason = null,
    profileLoading = false,
    profileError = null,
    samplingMode = null,
  }: Props = $props()

  const encoder = new TextEncoder()
  let histogramColumn = $state('')
  let histogramBins = $state(12)
  let samplePerShard = $state(25)

  function asNumber(v: unknown): number | null {
    if (v === null || v === undefined || v === '') return null
    const n = Number(v)
    return Number.isFinite(n) ? n : null
  }

  function estimateBytes(v: unknown): number {
    if (v === null || v === undefined) return 1
    if (typeof v === 'number' || typeof v === 'bigint') return 8
    if (typeof v === 'boolean') return 1
    if (typeof v === 'string') return encoder.encode(v).length
    try {
      return encoder.encode(JSON.stringify(v)).length
    } catch {
      return 16
    }
  }

  const elapsedSeconds = $derived(elapsedMs > 0 ? elapsedMs / 1000 : 0)
  const rowsPerSec = $derived(elapsedSeconds > 0 ? data.length / elapsedSeconds : 0)
  const numericColumns = $derived(meta.filter((col) => getDisplayType(col.type) === 'number'))
  const histogramColumnOptions = $derived.by<ComboboxOption[]>(() =>
    numericColumns.map((col) => ({ value: col.name, label: col.name, hint: col.type, keywords: `${col.name} ${col.type}` }))
  )

  $effect(() => {
    const exists = numericColumns.some((c) => c.name === histogramColumn)
    if (!exists) histogramColumn = numericColumns[0]?.name ?? ''
  })

  const columnMemory = $derived.by(() => {
    if (!meta.length || !data.length) return []
    const items = meta.map((col, ci) => {
      let bytes = 0
      for (let r = 0; r < data.length; r++) bytes += estimateBytes(data[r][ci])
      return {
        name: col.name,
        type: col.type,
        bytes,
        avgBytes: bytes / Math.max(data.length, 1),
      }
    })
    const total = items.reduce((sum, col) => sum + col.bytes, 0)
    return items
      .map((col) => ({
        ...col,
        pct: total > 0 ? (col.bytes / total) * 100 : 0,
      }))
      .sort((a, b) => b.bytes - a.bytes)
  })

  const totalEstimatedBytes = $derived(columnMemory.reduce((sum, c) => sum + c.bytes, 0))
  const throughputBytesPerSec = $derived(elapsedSeconds > 0 ? totalEstimatedBytes / elapsedSeconds : 0)

  const histogram = $derived.by(() => {
    if (!histogramColumn) return null
    const index = meta.findIndex((c) => c.name === histogramColumn)
    if (index === -1) return null

    const values: number[] = []
    for (let i = 0; i < data.length; i++) {
      const n = asNumber(data[i][index])
      if (n !== null) values.push(n)
    }
    if (!values.length) return null

    let min = Math.min(...values)
    let max = Math.max(...values)
    if (min === max) {
      min = min - 1
      max = max + 1
    }

    const bins = Math.max(6, Math.min(24, histogramBins))
    const counts = Array.from({ length: bins }, () => 0)
    const width = (max - min) / bins

    for (const v of values) {
      let idx = Math.floor((v - min) / width)
      if (idx < 0) idx = 0
      if (idx >= bins) idx = bins - 1
      counts[idx]++
    }

    const maxCount = Math.max(...counts, 1)
    return counts.map((count, i) => {
      const from = min + i * width
      const to = from + width
      return {
        count,
        from,
        to,
        widthPct: (count / maxCount) * 100,
      }
    })
  })

  function profileNumber(key: string): number {
    if (!profile) return 0
    const value = profile[key]
    const num = Number(value ?? 0)
    return Number.isFinite(num) ? num : 0
  }

  function handleSample() {
    if (!onSample) return
    const n = Math.max(1, Math.min(500, samplePerShard))
    samplePerShard = n
    onSample(n)
  }

  function splitPlanLabel(label: string): { title: string; detail: string } {
    const cleaned = (label ?? '').trim()
    if (!cleaned) return { title: 'Operation', detail: '' }

    const parenIdx = cleaned.indexOf('(')
    if (parenIdx > 0) {
      return {
        title: cleaned.slice(0, parenIdx).trim(),
        detail: cleaned.slice(parenIdx).trim(),
      }
    }

    const dashIdx = cleaned.indexOf(':')
    if (dashIdx > 0) {
      return {
        title: cleaned.slice(0, dashIdx).trim(),
        detail: cleaned.slice(dashIdx + 1).trim(),
      }
    }

    return { title: cleaned, detail: '' }
  }

  const planFlow = $derived.by(() => {
    if (planNodes.length > 0) {
      return planNodes.map((node, index) => {
        const parsed = splitPlanLabel(node.label)
        return {
          id: node.id || `node-${index}`,
          level: node.level ?? 0,
          index: index + 1,
          title: parsed.title,
          detail: parsed.detail,
          raw: node.label,
        }
      })
    }

    return planLines.map((line, index) => {
      const parsed = splitPlanLabel(line)
      return {
        id: `line-${index}`,
        level: 0,
        index: index + 1,
        title: parsed.title,
        detail: parsed.detail,
        raw: line,
      }
    })
  })
</script>

<div class="flex-1 overflow-auto min-h-0 p-4 space-y-4">
  <div class="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-4 gap-3">
    <div class="surface-card rounded-xl p-3">
      <div class="text-[11px] uppercase tracking-wider text-gray-500 inline-flex items-center gap-1.5"><Activity size={13} />Runtime</div>
      <div class="mt-2 text-xl font-semibold text-gray-800 dark:text-gray-100">{elapsedMs > 0 ? formatElapsed(elapsedSeconds) : '\u2014'}</div>
      <div class="mt-1 text-xs text-gray-500">{formatNumber(data.length)} rows returned</div>
    </div>

    <div class="surface-card rounded-xl p-3">
      <div class="text-[11px] uppercase tracking-wider text-gray-500 inline-flex items-center gap-1.5"><Gauge size={13} />Inline Profiling</div>
      <div class="mt-2 text-sm font-semibold text-gray-800 dark:text-gray-100">{rowsPerSec > 0 ? formatNumber(Math.round(rowsPerSec)) : '0'} rows/s</div>
      <div class="mt-1 text-xs text-gray-500">{throughputBytesPerSec > 0 ? formatBytes(throughputBytesPerSec) : '0 B'}/s (estimated)</div>
    </div>

    <div class="surface-card rounded-xl p-3">
      <div class="text-[11px] uppercase tracking-wider text-gray-500 inline-flex items-center gap-1.5"><Layers3 size={13} />Streaming Viewer</div>
      <div class="mt-2 text-sm font-semibold text-gray-800 dark:text-gray-100">{formatNumber(streamRows || data.length)} rows, {formatNumber(streamChunks)} chunks</div>
      <div class="mt-1 text-xs text-gray-500">
        {#if running}
          Live ingest running...
        {:else if streamLastChunkAt}
          Last chunk at {new Date(streamLastChunkAt).toLocaleTimeString()}
        {:else}
          No stream events yet
        {/if}
      </div>
    </div>

    <div class="surface-card rounded-xl p-3">
      <div class="text-[11px] uppercase tracking-wider text-gray-500 inline-flex items-center gap-1.5"><FlaskConical size={13} />Sampling</div>
      <div class="mt-2 flex items-center gap-2">
        <input
          type="number"
          min="1"
          max="500"
          bind:value={samplePerShard}
          class="w-24 px-2 py-1 text-xs rounded-md bg-gray-100 dark:bg-gray-800 border border-gray-300 dark:border-gray-700"
        />
        <button
          class="px-2.5 py-1 text-xs rounded-md border border-gray-300 dark:border-gray-700 hover:bg-gray-200 dark:hover:bg-gray-800"
          onclick={handleSample}
          disabled={!onSample || running}
        >Sample / shard</button>
      </div>
      <div class="mt-1 text-xs text-gray-500">Mode: {samplingMode ?? 'none'}</div>
    </div>
  </div>

  <div class="grid grid-cols-1 xl:grid-cols-2 gap-4">
    <div class="surface-card rounded-xl p-3">
      <div class="flex items-center justify-between mb-3">
        <div class="text-[11px] uppercase tracking-wider text-gray-500 inline-flex items-center gap-1.5"><GitBranch size={13} />Query Plan Visualizer</div>
        <button
          class="px-2.5 py-1 text-xs rounded-md border border-gray-300 dark:border-gray-700 hover:bg-gray-200 dark:hover:bg-gray-800 inline-flex items-center gap-1.5"
          onclick={() => onLoadPlan?.()}
          disabled={!onLoadPlan || planLoading}
        >
          {#if planLoading}<Spinner size="sm" />{:else}<RefreshCw size={12} />{/if}
          Refresh Plan
        </button>
      </div>
      {#if planLoading}
        <div class="text-xs text-gray-500">Loading query plan...</div>
      {:else if planError}
        <div class="text-xs text-red-500">{planError}</div>
      {:else if planFlow.length > 0}
        <div class="text-[11px] text-gray-500 mb-2">Source: {planSource} · {formatNumber(planFlow.length)} stages</div>
        <div class="max-h-64 overflow-auto rounded-lg border border-gray-200 dark:border-gray-800 bg-gray-50/70 dark:bg-gray-900/60 p-2.5">
          <div class="space-y-2">
            {#each planFlow as node, i (node.id)}
              <div class="relative" style={`margin-left:${node.level * 14}px`}>
                {#if i < planFlow.length - 1}
                  <div class="absolute left-3 top-7 h-6 w-px bg-gray-300 dark:bg-gray-700"></div>
                {/if}
                <div class="flex items-start gap-2">
                  <div class="mt-0.5 w-6 h-6 rounded-full border border-gray-300 dark:border-gray-700 bg-gray-100 dark:bg-gray-800 text-[10px] font-semibold text-gray-600 dark:text-gray-300 flex items-center justify-center">
                    {node.index}
                  </div>
                  <div class="flex-1 min-w-0 rounded-md border border-gray-200 dark:border-gray-800 bg-white/60 dark:bg-gray-950/40 px-2.5 py-1.5">
                    <div class="text-xs font-semibold text-gray-700 dark:text-gray-200">{node.title}</div>
                    {#if node.detail}
                      <div class="text-[11px] text-gray-500 mt-0.5 break-words">{node.detail}</div>
                    {/if}
                  </div>
                </div>
              </div>
            {/each}
          </div>
        </div>
      {:else}
        <div class="text-xs text-gray-500">No plan loaded yet.</div>
      {/if}
    </div>

    <div class="surface-card rounded-xl p-3">
      <div class="text-[11px] uppercase tracking-wider text-gray-500 inline-flex items-center gap-1.5 mb-3"><Layers3 size={13} />Columnar Memory View</div>
      {#if columnMemory.length === 0}
        <div class="text-xs text-gray-500">Run a query with rows to estimate per-column memory.</div>
      {:else}
        <div class="space-y-2 max-h-64 overflow-auto pr-1">
          {#each columnMemory as col}
            <div>
              <div class="flex items-center justify-between text-xs">
                <span class="font-mono text-gray-700 dark:text-gray-300 truncate pr-3">{col.name}</span>
                <span class="text-gray-500">{formatBytes(col.bytes)} ({col.pct.toFixed(1)}%)</span>
              </div>
              <div class="mt-1 h-1.5 rounded bg-gray-200 dark:bg-gray-800 overflow-hidden">
                <div class="h-full bg-ch-orange/80" style={`width:${Math.max(3, col.pct)}%`}></div>
              </div>
              <div class="mt-0.5 text-[11px] text-gray-500">{col.type} · avg {formatBytes(col.avgBytes)}/row</div>
            </div>
          {/each}
        </div>
      {/if}
    </div>
  </div>

  <div class="grid grid-cols-1 xl:grid-cols-2 gap-4">
    <div class="surface-card rounded-xl p-3">
      <div class="flex items-center justify-between mb-3">
        <div class="text-[11px] uppercase tracking-wider text-gray-500 inline-flex items-center gap-1.5"><AreaChart size={13} />Histogram Per Column</div>
        <div class="flex items-center gap-2">
          <div class="w-56">
            <Combobox
              options={histogramColumnOptions}
              value={histogramColumn}
              onChange={(v) => histogramColumn = v}
              placeholder="Numeric column"
            />
          </div>
          <input type="range" min="6" max="24" step="1" bind:value={histogramBins} />
        </div>
      </div>

      {#if !histogram || histogram.length === 0}
        <div class="text-xs text-gray-500">No numeric values available for histogram.</div>
      {:else}
        <div class="space-y-1.5">
          {#each histogram as bin}
            <div class="grid grid-cols-[90px_1fr_42px] items-center gap-2 text-[11px]">
              <span class="text-gray-500 font-mono truncate">{bin.from.toFixed(2)}</span>
              <div class="h-3 rounded bg-gray-200 dark:bg-gray-800 overflow-hidden">
                <div class="h-full bg-ch-orange/80" style={`width:${Math.max(bin.widthPct, 2)}%`}></div>
              </div>
              <span class="text-right text-gray-600 dark:text-gray-400">{bin.count}</span>
            </div>
          {/each}
        </div>
      {/if}
    </div>

    <div class="surface-card rounded-xl p-3">
      <div class="text-[11px] uppercase tracking-wider text-gray-500 inline-flex items-center gap-1.5 mb-3"><Gauge size={13} />Inline Profile Events</div>
      {#if profileLoading}
        <div class="text-xs text-gray-500">Loading profile from system.query_log...</div>
      {:else if profileError}
        <div class="text-xs text-red-500">{profileError}</div>
      {:else if profileAvailable && profile}
        <div class="grid grid-cols-2 gap-2 text-xs">
          <div class="rounded-md bg-gray-100/70 dark:bg-gray-800/70 px-2 py-1.5">
            <div class="text-gray-500">Duration</div>
            <div class="font-semibold text-gray-800 dark:text-gray-100">{profileNumber('query_duration_ms')} ms</div>
          </div>
          <div class="rounded-md bg-gray-100/70 dark:bg-gray-800/70 px-2 py-1.5">
            <div class="text-gray-500">Memory</div>
            <div class="font-semibold text-gray-800 dark:text-gray-100">{formatBytes(profileNumber('memory_usage'))}</div>
          </div>
          <div class="rounded-md bg-gray-100/70 dark:bg-gray-800/70 px-2 py-1.5">
            <div class="text-gray-500">Read Rows</div>
            <div class="font-semibold text-gray-800 dark:text-gray-100">{formatNumber(profileNumber('read_rows'))}</div>
          </div>
          <div class="rounded-md bg-gray-100/70 dark:bg-gray-800/70 px-2 py-1.5">
            <div class="text-gray-500">Read Bytes</div>
            <div class="font-semibold text-gray-800 dark:text-gray-100">{formatBytes(profileNumber('read_bytes'))}</div>
          </div>
          <div class="rounded-md bg-gray-100/70 dark:bg-gray-800/70 px-2 py-1.5">
            <div class="text-gray-500">Result Rows</div>
            <div class="font-semibold text-gray-800 dark:text-gray-100">{formatNumber(profileNumber('result_rows'))}</div>
          </div>
          <div class="rounded-md bg-gray-100/70 dark:bg-gray-800/70 px-2 py-1.5">
            <div class="text-gray-500">Selected Marks</div>
            <div class="font-semibold text-gray-800 dark:text-gray-100">{formatNumber(profileNumber('selected_marks'))}</div>
          </div>
        </div>
      {:else}
        <div class="text-xs text-gray-500">{profileReason ?? 'No profile row available yet.'}</div>
      {/if}
    </div>
  </div>

  {#if stats}
    <div class="text-[11px] text-gray-500">
      Stream started: {streamStartedAt ? new Date(streamStartedAt).toLocaleTimeString() : '\u2014'}
      · rows_read: {formatNumber(Number(stats.rows_read ?? 0))}
      · bytes_read: {formatBytes(Number(stats.bytes_read ?? 0))}
    </div>
  {/if}
</div>
