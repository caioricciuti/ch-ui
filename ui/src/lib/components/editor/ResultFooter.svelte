<script lang="ts">
  import type { ColumnMeta, QueryStats } from '../../types/query'
  import { formatNumber, formatElapsed, formatBytes } from '../../utils/format'
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
  import { Table2, BarChart3, Columns3, Sparkles, Copy, Download, ChevronUp, FileJson, FileText, Database } from 'lucide-svelte'

  type Tab = 'data' | 'stats' | 'schema' | 'insights'
  type ExportFormat = 'csv' | 'tsv' | 'json' | 'jsoncompact' | 'jsonl' | 'markdown' | 'sql' | 'xml'

  interface Props {
    activeTab: Tab
    onTabChange: (tab: Tab) => void
    meta: ColumnMeta[]
    data: unknown[][]
    stats?: QueryStats | null
    elapsedMs?: number
    streamRows?: number
    streamChunks?: number
  }

  let { activeTab, onTabChange, meta, data, stats = null, elapsedMs = 0, streamRows = 0, streamChunks = 0 }: Props = $props()
  let copyMenuOpen = $state(false)
  let downloadMenuOpen = $state(false)
  let copyMenuRef = $state<HTMLDivElement | null>(null)
  let downloadMenuRef = $state<HTMLDivElement | null>(null)

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
        ? 'text-ch-blue border-b-2 border-ch-blue'
        : 'text-gray-500 hover:text-gray-700 dark:hover:text-gray-300 border-b-2 border-transparent'
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
      copyMenuOpen = false
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
    downloadMenuOpen = false
  }

  function closeMenus() {
    copyMenuOpen = false
    downloadMenuOpen = false
  }

  function handleWindowClick(e: MouseEvent) {
    const target = e.target as Node | null
    if (!target) return
    if (copyMenuRef?.contains(target) || downloadMenuRef?.contains(target)) return
    closeMenus()
  }

  function handleWindowKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') closeMenus()
  }
</script>

<svelte:window onclick={handleWindowClick} onkeydown={handleWindowKeydown} />

<div class="flex items-center gap-1 px-2 py-0.5 border-t border-gray-200 dark:border-gray-800 bg-gray-100/50 dark:bg-gray-900/50 shrink-0 h-9 select-none">
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
  <div class="w-px h-4 bg-gray-300 dark:bg-gray-700 mx-1"></div>

  <!-- Info chips -->
  <div class="flex items-center gap-3 text-xs text-gray-500 flex-1 min-w-0">
    <span>{formatNumber(rowCount)} rows</span>
    {#if elapsedMs > 0}
      <span>{formatElapsed(elapsedMs / 1000)}</span>
    {/if}
    {#if streamChunks > 0}
      <span>{formatNumber(streamRows)} streamed</span>
      <span>{formatNumber(streamChunks)} chunks</span>
    {/if}
    {#if stats}
      {#if stats.rows_read}
        <span>{formatNumber(stats.rows_read)} read</span>
      {/if}
      {#if stats.bytes_read}
        <span>{formatBytes(stats.bytes_read)}</span>
      {/if}
    {/if}
  </div>

  <!-- Export buttons -->
  <div class="flex items-center gap-1">
    <div class="relative" bind:this={copyMenuRef}>
      <button
        class="flex items-center gap-1.5 px-2 py-1 text-xs border border-gray-300 dark:border-gray-700 rounded-md text-gray-600 dark:text-gray-300 hover:text-gray-900 dark:hover:text-gray-100 hover:bg-gray-200 dark:hover:bg-gray-800 transition-colors"
        onclick={() => {
          copyMenuOpen = !copyMenuOpen
          if (copyMenuOpen) downloadMenuOpen = false
        }}
      >
        <Copy size={12} />
        Copy
        <ChevronUp size={12} class="opacity-70" />
      </button>

      {#if copyMenuOpen}
        <div class="absolute right-0 bottom-full mb-1 w-44 rounded-lg border border-gray-300 dark:border-gray-700 bg-gray-50/98 dark:bg-gray-900/98 backdrop-blur-xl shadow-xl p-1 z-20">
          {#each formatOptions as option}
            <button
              class="w-full flex items-center gap-2 px-2 py-1.5 text-xs rounded-md text-left transition-colors
                {option.disabled
                  ? 'text-gray-400 dark:text-gray-600 cursor-not-allowed'
                  : 'text-gray-600 dark:text-gray-300 hover:text-gray-900 dark:hover:text-gray-100 hover:bg-gray-200 dark:hover:bg-gray-800'}"
              onclick={() => !option.disabled && handleCopy(option.id)}
              disabled={option.disabled}
            >
              <option.icon size={12} />
              {option.label}
            </button>
          {/each}
        </div>
      {/if}
    </div>

    <div class="relative" bind:this={downloadMenuRef}>
      <button
        class="flex items-center gap-1.5 px-2 py-1 text-xs border border-gray-300 dark:border-gray-700 rounded-md text-gray-600 dark:text-gray-300 hover:text-gray-900 dark:hover:text-gray-100 hover:bg-gray-200 dark:hover:bg-gray-800 transition-colors"
        onclick={() => {
          downloadMenuOpen = !downloadMenuOpen
          if (downloadMenuOpen) copyMenuOpen = false
        }}
      >
        <Download size={12} />
        Download
        <ChevronUp size={12} class="opacity-70" />
      </button>

      {#if downloadMenuOpen}
        <div class="absolute right-0 bottom-full mb-1 w-44 rounded-lg border border-gray-300 dark:border-gray-700 bg-gray-50/98 dark:bg-gray-900/98 backdrop-blur-xl shadow-xl p-1 z-20">
          {#each formatOptions as option}
            <button
              class="w-full flex items-center gap-2 px-2 py-1.5 text-xs rounded-md text-left transition-colors
                {option.disabled
                  ? 'text-gray-400 dark:text-gray-600 cursor-not-allowed'
                  : 'text-gray-600 dark:text-gray-300 hover:text-gray-900 dark:hover:text-gray-100 hover:bg-gray-200 dark:hover:bg-gray-800'}"
              onclick={() => !option.disabled && handleDownload(option.id)}
              disabled={option.disabled}
            >
              <option.icon size={12} />
              {option.label}
            </button>
          {/each}
        </div>
      {/if}
    </div>
  </div>
</div>
