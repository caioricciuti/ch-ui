<script lang="ts">
  import type { ColumnMeta } from '../../types/query'
  import { formatNumber, formatElapsed } from '../../utils/format'
  import {
    generateCSV,
    generateTSV,
    generateJSON,
    generateJSONCompact,
    generateJSONLines,
    generateMarkdown,
    generateSQLInsert,
    generateXML,
    copyToClipboard,
    downloadFile,
  } from '../../utils/export'
  import { success, error } from '../../stores/toast.svelte'
  import { Table2, BarChart3, Columns3, Sparkles, Copy, Download, ChevronUp, FileJson, FileText, Database, Hash, AlertTriangle, ListFilter, CloudCog } from 'lucide-svelte'
  import { getFormatNumbers, toggleFormatNumbers } from '../../stores/number-format.svelte'
  import { getMaxResultRows, setMaxResultRows } from '../../stores/query-limit.svelte'
  import { getResultFiltersEnabled, toggleResultFiltersEnabled } from '../../stores/result-filters.svelte'

  type Tab = 'data' | 'stats' | 'schema' | 'insights'
  type ExportFormat = 'csv' | 'tsv' | 'json' | 'jsoncompact' | 'jsonl' | 'markdown' | 'sql' | 'xml'

  interface Props {
    activeTab: Tab
    onTabChange: (tab: Tab) => void
    meta: ColumnMeta[]
    data: unknown[][]
    elapsedMs?: number
    streamRows?: number
    streamChunks?: number
    /** Unfiltered row count when client-side filters hide rows. */
    totalRows?: number | null
    /** Rows came from a server-side filtered/ordered re-query. */
    serverApplied?: boolean
  }

  let { activeTab, onTabChange, meta, data, elapsedMs = 0, streamRows = 0, streamChunks = 0, totalRows = null, serverApplied = false }: Props = $props()
  let exportMenuOpen = $state(false)
  let exportMenuRef = $state<HTMLDivElement | null>(null)
  let rowsMenuOpen = $state(false)
  let rowsMenuRef = $state<HTMLDivElement | null>(null)

  // Row limit presets. The trigger always derives from the store, so the
  // displayed value cannot drift from the real limit (a one-way `value=`
  // binding used to show 1,000 while 100,000 was in effect).
  const ROW_PRESETS = [1000, 10000, 50000, 100000, 500000]
  const WARN_ABOVE = 10000
  const fmtRows = (n: number) => (n >= 1000 ? `${n / 1000}K` : String(n))

  const rowCount = $derived(data.length)

  const tabs: { id: Tab; label: string; icon: typeof Table2 }[] = [
    { id: 'data', label: 'Data', icon: Table2 },
    { id: 'stats', label: 'Stats', icon: BarChart3 },
    { id: 'schema', label: 'Schema', icon: Columns3 },
    { id: 'insights', label: 'Insights', icon: Sparkles },
  ]

  const tabClass = (id: Tab) =>
    `flex items-center gap-1 px-2 py-1 text-xs font-medium transition-colors ${
      activeTab === id
        ? 'text-ch-orange border-b-2 border-ch-orange'
        : 'text-fg-3 hover:text-fg border-b-2 border-transparent'
    }`

  const formatOptions: { id: ExportFormat | 'parquet'; label: string; ext: string; mime: string; icon: typeof FileText; disabled?: boolean }[] = [
    { id: 'csv', label: 'CSV', ext: 'csv', mime: 'text/csv;charset=utf-8', icon: FileText },
    { id: 'tsv', label: 'TSV', ext: 'tsv', mime: 'text/tab-separated-values;charset=utf-8', icon: FileText },
    { id: 'json', label: 'JSON', ext: 'json', mime: 'application/json;charset=utf-8', icon: FileJson },
    { id: 'jsoncompact', label: 'JSONCompact', ext: 'json', mime: 'application/json;charset=utf-8', icon: FileJson },
    { id: 'jsonl', label: 'JSONLines', ext: 'jsonl', mime: 'application/x-ndjson;charset=utf-8', icon: FileJson },
    { id: 'markdown', label: 'Markdown', ext: 'md', mime: 'text/markdown;charset=utf-8', icon: FileText },
    { id: 'sql', label: 'SQL Insert', ext: 'sql', mime: 'application/sql;charset=utf-8', icon: Database },
    { id: 'xml', label: 'XML', ext: 'xml', mime: 'application/xml;charset=utf-8', icon: FileText },
    { id: 'parquet', label: 'Parquet (Soon)', ext: 'parquet', mime: 'application/octet-stream', icon: Database, disabled: true },
  ]

  function payloadFor(format: ExportFormat): string {
    switch (format) {
      case 'csv': return generateCSV(meta, data)
      case 'tsv': return generateTSV(meta, data)
      case 'json': return generateJSON(meta, data)
      case 'jsoncompact': return generateJSONCompact(meta, data)
      case 'jsonl': return generateJSONLines(meta, data)
      case 'markdown': return generateMarkdown(meta, data)
      case 'sql': return generateSQLInsert(meta, data)
      case 'xml': return generateXML(meta, data)
      default: return generateCSV(meta, data)
    }
  }

  async function handleCopy(format: ExportFormat | 'parquet') {
    if (format === 'parquet') return
    try {
      await copyToClipboard(payloadFor(format))
      success(`Copied ${formatNumber(rowCount)} rows as ${format.toUpperCase()}`)
      exportMenuOpen = false
    } catch {
      error('Failed to copy to clipboard')
    }
  }

  function handleDownload(format: ExportFormat | 'parquet') {
    if (format === 'parquet') return
    const opt = formatOptions.find((f) => f.id === format)
    if (!opt) return
    const filename = `query_results.${opt.ext}`
    downloadFile(payloadFor(format), filename, opt.mime)
    success(`Downloaded ${filename}`)
    exportMenuOpen = false
  }

  function closeMenus() {
    exportMenuOpen = false
    rowsMenuOpen = false
  }

  function handleWindowClick(e: MouseEvent) {
    const target = e.target as Node | null
    if (!target) return
    if (exportMenuRef?.contains(target) || rowsMenuRef?.contains(target)) return
    closeMenus()
  }

  function handleWindowKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') closeMenus()
  }
</script>

<svelte:window onclick={handleWindowClick} onkeydown={handleWindowKeydown} />

<div class="flex items-center gap-1 px-2 py-0.5 border-t border-edge-subtle bg-surface shrink-0 h-9 select-none">
  <!-- Tabs -->
  <div class="flex items-center gap-0.5">
    {#each tabs as tab}
      <button class={tabClass(tab.id)} onclick={() => onTabChange(tab.id)}>
        <tab.icon size={13} />
        {tab.label}
      </button>
    {/each}
  </div>

  <!-- Divider -->
  <div class="w-px h-4 bg-edge mx-1"></div>

  <!-- Info chips -->
  <div class="flex items-center gap-3 text-xs text-fg-3 flex-1 min-w-0">
    {#if totalRows !== null && totalRows !== rowCount}
      <span>{formatNumber(rowCount)} of {formatNumber(totalRows)} rows</span>
    {:else}
      <span>{formatNumber(rowCount)} rows</span>
    {/if}
    {#if serverApplied}
      <span class="flex items-center gap-1 text-ch-orange" title="Filters/ordering were applied by re-running the query on the server">
        <CloudCog size={12} />
        server-filtered
      </span>
    {/if}
    {#if elapsedMs > 0}
      <span>{formatElapsed(elapsedMs / 1000)}</span>
    {/if}
    {#if streamChunks > 0}
      <span>{formatNumber(streamRows)} streamed</span>
      <span>{formatNumber(streamChunks)} chunks</span>
    {/if}
    <!-- Rows and bytes read live in the progress readout above the results, so
         they are not repeated here. -->
  </div>

  <!-- Result filters toggle -->
  <div class="w-px h-4 bg-edge mx-1"></div>
  <button
    class="flex items-center gap-1 px-1.5 py-1 text-xs rounded-md transition-colors
      {getResultFiltersEnabled()
        ? 'bg-surface-2 text-fg'
        : 'text-fg-3 hover:bg-hover hover:text-fg'}"
    onclick={toggleResultFiltersEnabled}
    title={getResultFiltersEnabled()
      ? 'Column sorting & filtering enabled — click headers to sort, hover for filters (click to disable)'
      : 'Column sorting & filtering disabled (click to enable)'}
  >
    <ListFilter size={12} />
    <span class="hidden sm:inline">Filters</span>
  </button>

  <!-- Number format toggle -->
  <div class="w-px h-4 bg-edge mx-1"></div>
  <button
    class="flex items-center gap-1 px-1.5 py-1 text-xs rounded-md transition-colors
      {getFormatNumbers()
        ? 'bg-surface-2 text-fg'
        : 'text-fg-3 hover:bg-hover hover:text-fg'}"
    onclick={toggleFormatNumbers}
    title={getFormatNumbers() ? 'Numbers formatted with separators (click to show raw)' : 'Numbers shown as raw values (click to format)'}
  >
    <Hash size={12} />
    <span class="hidden sm:inline">{getFormatNumbers() ? 'Format Numbers' : 'Raw Numbers'}</span>
  </button>

  <!-- Row limit: presets, with the current value always read from the store -->
  <div class="w-px h-4 bg-edge mx-1"></div>
  <div class="relative" bind:this={rowsMenuRef}>
    <button
      class="flex h-7 items-center gap-1.5 rounded-md px-2 text-xs transition-colors hover:bg-hover hover:text-fg {getMaxResultRows() > WARN_ABOVE ? 'text-warning' : 'text-fg-3'}"
      onclick={() => { rowsMenuOpen = !rowsMenuOpen; exportMenuOpen = false }}
      title={getMaxResultRows() > WARN_ABOVE ? 'Large limits load a lot of rows into the browser' : 'Maximum rows fetched per query'}
      aria-haspopup="menu"
      aria-expanded={rowsMenuOpen}
    >
      {#if getMaxResultRows() > WARN_ABOVE}<AlertTriangle size={12} />{/if}
      Rows: {fmtRows(getMaxResultRows())}
      <ChevronUp size={12} class="opacity-70" />
    </button>
    {#if rowsMenuOpen}
      <div class="surface-card absolute bottom-full right-0 z-20 mb-1 w-44 rounded-md p-1" role="menu">
        {#each ROW_PRESETS as n}
          <button
            class="flex w-full items-center justify-between rounded-sm px-2 py-1.5 text-left text-xs transition-colors {getMaxResultRows() === n ? 'bg-hover text-fg' : 'text-fg-2 hover:bg-hover hover:text-fg'}"
            role="menuitemradio"
            aria-checked={getMaxResultRows() === n}
            onclick={() => { setMaxResultRows(n); closeMenus() }}
          >
            <span class="tabular-nums">{fmtRows(n)}</span>
            {#if n > WARN_ABOVE}<span class="text-[10px] text-warning">heavy</span>{/if}
          </button>
        {/each}
      </div>
    {/if}
  </div>

  <!-- Export: one menu, copy or download in any format -->
  <div class="relative" bind:this={exportMenuRef}>
    <button
      class="flex h-7 items-center gap-1.5 rounded-md px-2 text-xs text-fg-3 transition-colors hover:bg-hover hover:text-fg"
      onclick={() => { exportMenuOpen = !exportMenuOpen; rowsMenuOpen = false }}
      aria-haspopup="menu"
      aria-expanded={exportMenuOpen}
    >
      <Download size={12} />
      Export
      <ChevronUp size={12} class="opacity-70" />
    </button>

    {#if exportMenuOpen}
      <div class="surface-card absolute bottom-full right-0 z-20 mb-1 w-80 rounded-md p-1" role="menu">
        <div class="grid grid-cols-2 gap-1">
          <div>
            <div class="flex items-center gap-1 px-2 py-1 text-[10px] font-medium uppercase tracking-wide text-fg-4"><Copy size={10} /> Copy</div>
            {#each formatOptions as option (option.id)}
              <button
                class="flex w-full items-center gap-2 rounded-sm px-2 py-1 text-left text-xs transition-colors {option.disabled ? 'cursor-not-allowed text-fg-4' : 'text-fg-2 hover:bg-hover hover:text-fg'}"
                onclick={() => !option.disabled && handleCopy(option.id)}
                disabled={option.disabled}
                role="menuitem"
              >
                <option.icon size={12} />
                {option.label}
              </button>
            {/each}
          </div>
          <div class="border-l border-edge-subtle pl-1">
            <div class="flex items-center gap-1 px-2 py-1 text-[10px] font-medium uppercase tracking-wide text-fg-4"><Download size={10} /> Download</div>
            {#each formatOptions as option (option.id)}
              <button
                class="flex w-full items-center gap-2 rounded-sm px-2 py-1 text-left text-xs transition-colors {option.disabled ? 'cursor-not-allowed text-fg-4' : 'text-fg-2 hover:bg-hover hover:text-fg'}"
                onclick={() => !option.disabled && handleDownload(option.id)}
                disabled={option.disabled}
                role="menuitem"
              >
                <option.icon size={12} />
                {option.label}
              </button>
            {/each}
          </div>
        </div>
      </div>
    {/if}
  </div>
</div>
