<script lang="ts">
  import { onMount, untrack } from 'svelte'
  import type { ColumnMeta } from '../../types/query'
  import TableHeader from './TableHeader.svelte'
  import TableCell from './TableCell.svelte'
  import { SearchX } from 'lucide-svelte'

  const ROW_HEIGHT = 34
  const OVERSCAN = 5
  const ROW_NUMBER_WIDTH = 60
  const MIN_COL_WIDTH = 120
  const MAX_COL_WIDTH = 720
  const SAMPLE_ROWS = 160

  interface Props {
    meta: ColumnMeta[]
    data: unknown[][]
    totalRows?: number
    sortColumn?: string
    sortDir?: 'asc' | 'desc'
    onsort?: (column: string) => void
  }

  let { meta, data, totalRows, sortColumn = '', sortDir = 'asc', onsort }: Props = $props()

  let container: HTMLDivElement
  let scrollTop = $state(0)
  let viewportHeight = $state(600)
  let viewportWidth = $state(900)
  let widths = $state<number[]>([])
  let baseWidths = $state<number[]>([])
  let manualTouched = $state<boolean[]>([])
  let selectedRow = $state<number | null>(null)

  function estimateTextWidth(text: string): number {
    return Math.max(0, Math.ceil(text.length * 7.4))
  }

  function estimateValueWidth(value: unknown): number {
    if (value === null || value === undefined) return 34
    if (typeof value === 'number') return Math.max(60, estimateTextWidth(value.toLocaleString()) + 18)
    if (typeof value === 'boolean') return 58
    if (typeof value === 'string') {
      const str = value.length > 80 ? value.slice(0, 80) : value
      return Math.max(74, estimateTextWidth(str) + 18)
    }
    return Math.max(90, estimateTextWidth(JSON.stringify(value).slice(0, 80)) + 20)
  }

  function compactTypeLabel(type: string): string {
    const normalized = type
      .replace(/^Nullable\((.+)\)$/, '$1')
      .replace(/^LowCardinality\((.+)\)$/, '$1')
      .replace(/\s+/g, '')

    if (normalized.length <= 16) return normalized
    return `${normalized.slice(0, 15)}…`
  }

  function sampleRows(rows: unknown[][]): unknown[][] {
    if (rows.length <= SAMPLE_ROWS) return rows
    const sampled: unknown[][] = []
    const step = rows.length / SAMPLE_ROWS
    for (let i = 0; i < SAMPLE_ROWS; i++) {
      const idx = Math.floor(i * step)
      sampled.push(rows[idx] ?? rows[rows.length - 1])
    }
    return sampled
  }

  function buildBaseWidths(columns: ColumnMeta[], rows: unknown[][]): number[] {
    if (!columns.length) return []
    const sampled = sampleRows(rows)

    return columns.map((col, ci) => {
      const metricWidths: number[] = []
      const typeLabel = compactTypeLabel(col.type)
      const headerMinWidth = estimateTextWidth(col.name) + Math.min(estimateTextWidth(typeLabel), 124) + 62
      let width = Math.max(
        estimateTextWidth(col.name) + 40,
        estimateTextWidth(col.type) + 28,
        headerMinWidth,
      )

      for (let ri = 0; ri < sampled.length; ri++) {
        const valueWidth = estimateValueWidth(sampled[ri]?.[ci])
        metricWidths.push(valueWidth)
        width = Math.max(width, valueWidth)
      }

      if (metricWidths.length > 0) {
        const sorted = [...metricWidths].sort((a, b) => a - b)
        const p90 = sorted[Math.floor((sorted.length - 1) * 0.9)] ?? width
        width = Math.max(width, Math.round(p90))
      }

      if (/^Date(Time)?/.test(col.type)) width = Math.max(width, 140)
      if (/^(U?Int|Float|Decimal)/.test(col.type)) width = Math.max(width, 124)
      if (/UUID|FixedString/.test(col.type)) width = Math.max(width, 180)

      return Math.max(MIN_COL_WIDTH, Math.min(MAX_COL_WIDTH, Math.round(width)))
    })
  }

  function distributeToViewport(source: number[], touched: boolean[], viewport: number): number[] {
    if (!source.length) return []
    const available = Math.max(0, viewport - ROW_NUMBER_WIDTH)
    const total = source.reduce((sum, w) => sum + w, 0)
    if (available <= 0 || total >= available) return source

    const extra = available - total
    const autoIndices = source.map((_, i) => i).filter((i) => !touched[i])
    const targets = autoIndices.length > 0 ? autoIndices : source.map((_, i) => i)
    const targetSet = new Set(targets)
    const weightSum = targets.reduce((sum, i) => sum + Math.max(1, Math.sqrt(source[i])), 0)

    let consumed = 0
    const grown = source.map((w, i) => {
      if (!targetSet.has(i)) return w
      const gain = Math.floor((extra * Math.max(1, Math.sqrt(source[i]))) / weightSum)
      consumed += gain
      return w + gain
    })

    // Allocate remaining pixels so width matches viewport exactly.
    let remainder = extra - consumed
    for (let i = 0; i < grown.length && remainder > 0; i++) {
      if (targetSet.has(i)) {
        grown[i] += 1
        remainder--
      }
    }

    return grown
  }

  function syncViewport() {
    if (!container) return
    viewportHeight = container.clientHeight
    viewportWidth = container.clientWidth
  }

  // Initialize and keep widths in sync with new result sets.
  $effect(() => {
    const columns = meta
    const rows = data
    if (!columns.length) {
      widths = []
      baseWidths = []
      manualTouched = []
      return
    }

    const nextBase = buildBaseWidths(columns, rows)
    baseWidths = nextBase

    const currentWidths = untrack(() => widths)
    const currentTouched = untrack(() => manualTouched)

    if (currentWidths.length !== columns.length || currentTouched.length !== columns.length) {
      widths = [...nextBase]
      manualTouched = Array.from({ length: columns.length }, () => false)
      return
    }

    const nextWidths = currentWidths.map((w, i) => (currentTouched[i] ? w : nextBase[i]))
    const changed = nextWidths.some((w, i) => w !== currentWidths[i])
    if (changed) widths = nextWidths
  })

  // Keep viewport metrics fresh when result sets or panels change size.
  $effect(() => {
    meta
    data
    const raf = requestAnimationFrame(syncViewport)
    return () => cancelAnimationFrame(raf)
  })

  const rowCount = $derived(data.length)
  const totalHeight = $derived(rowCount * ROW_HEIGHT)
  const startIdx = $derived(Math.max(0, Math.floor(scrollTop / ROW_HEIGHT) - OVERSCAN))
  const endIdx = $derived(Math.min(rowCount, Math.ceil((scrollTop + viewportHeight) / ROW_HEIGHT) + OVERSCAN))
  const visibleRows = $derived(data.slice(startIdx, endIdx))
  const topPad = $derived(startIdx * ROW_HEIGHT)
  const effectiveWidths = $derived(distributeToViewport(widths, manualTouched, viewportWidth))
  const tableWidth = $derived(ROW_NUMBER_WIDTH + effectiveWidths.reduce((sum, w) => sum + w, 0))

  function handleScroll() {
    scrollTop = container.scrollTop
  }

  function handleResize(index: number, width: number) {
    widths = widths.map((w, i) => i === index ? Math.max(MIN_COL_WIDTH, Math.min(MAX_COL_WIDTH, Math.round(width))) : w)
    manualTouched = manualTouched.map((t, i) => i === index ? true : t)
  }

  function handleFitColumn(index: number) {
    if (!baseWidths[index]) return
    widths = widths.map((w, i) => i === index ? baseWidths[index] : w)
    manualTouched = manualTouched.map((t, i) => i === index ? false : t)
  }

  function handleFitAll() {
    widths = [...baseWidths]
    manualTouched = manualTouched.map(() => false)
  }

  onMount(() => {
    if (!container) return

    syncViewport()

    const observer = new ResizeObserver(() => syncViewport())
    observer.observe(container)
    window.addEventListener('resize', syncViewport)

    return () => {
      observer.disconnect()
      window.removeEventListener('resize', syncViewport)
    }
  })
</script>

<div bind:this={container} class="relative flex-1 overflow-auto bg-white dark:bg-gray-950" onscroll={handleScroll}>
  <table class="text-sm border-collapse table-fixed" style="width:{tableWidth}px;min-width:{tableWidth}px">
    <colgroup>
      <col style="width:{ROW_NUMBER_WIDTH}px;min-width:{ROW_NUMBER_WIDTH}px;max-width:{ROW_NUMBER_WIDTH}px" />
      {#each effectiveWidths as width}
        <col style="width:{width}px;min-width:{width}px;max-width:{width}px" />
      {/each}
    </colgroup>
    <TableHeader
      columns={meta}
      widths={effectiveWidths}
      {sortColumn}
      {sortDir}
      {onsort}
      onresize={handleResize}
      onfitcolumn={handleFitColumn}
      onfitall={handleFitAll}
    />
    {#if rowCount > 0}
      <tbody>
        <!-- Spacer for virtual scroll -->
        <tr style="height:{topPad}px" aria-hidden="true"><td colspan={meta.length + 1}></td></tr>

        {#each visibleRows as row, vi (startIdx + vi)}
          {@const absIdx = startIdx + vi}
          <tr
            class="group h-[34px] border-b border-gray-100 dark:border-gray-900 hover:bg-orange-50/70 dark:hover:bg-ch-orange/8 cursor-default
              {absIdx % 2 === 1 ? 'bg-gray-50 dark:bg-gray-900' : 'bg-white dark:bg-gray-950'}
              {selectedRow === absIdx ? 'bg-orange-100 dark:bg-orange-950' : ''}"
            onclick={() => selectedRow = absIdx}
          >
            <td
              class="sticky left-0 z-[2] px-2.5 text-right text-xs font-semibold text-gray-700 dark:text-gray-200 border-r border-gray-200 dark:border-gray-800 tabular-nums select-none
                {absIdx % 2 === 1 ? 'bg-gray-50 dark:bg-gray-900' : 'bg-white dark:bg-gray-950'}
                {selectedRow === absIdx ? 'bg-orange-100 dark:bg-orange-950' : ''}
                group-hover:bg-orange-50 dark:group-hover:bg-orange-900/60"
              style="width:{ROW_NUMBER_WIDTH}px;max-width:{ROW_NUMBER_WIDTH}px;min-width:{ROW_NUMBER_WIDTH}px"
            >{absIdx + 1}</td>
            {#each meta as col, ci}
              <TableCell
                value={row[ci]}
                type={col.type}
                width={effectiveWidths[ci] ?? 120}
              />
            {/each}
          </tr>
        {/each}

        <!-- Bottom spacer -->
        <tr style="height:{Math.max(0, totalHeight - (endIdx * ROW_HEIGHT))}px" aria-hidden="true"><td colspan={meta.length + 1}></td></tr>
      </tbody>
    {/if}
  </table>

  {#if rowCount === 0 && meta.length > 0}
    <div class="absolute inset-x-0 top-[35px] bottom-0 grid place-items-center p-6 pointer-events-none">
      <div class="max-w-md rounded-xl border border-gray-200/80 dark:border-gray-800/80 bg-gray-50/90 dark:bg-gray-900/65 px-6 py-5 text-center shadow-lg">
        <div class="mx-auto mb-3 inline-flex h-10 w-10 items-center justify-center rounded-full bg-gray-100 dark:bg-gray-800 text-gray-500 dark:text-gray-300">
          <SearchX size={18} />
        </div>
        <p class="text-sm font-semibold text-gray-800 dark:text-gray-200">No rows returned</p>
        <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
          Your query executed successfully, but nothing matched the current filters.
        </p>
      </div>
    </div>
  {/if}
</div>
