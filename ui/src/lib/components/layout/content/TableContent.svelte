<script lang="ts">
  import { onMount, onDestroy } from 'svelte'
  import type { TableTab } from '../../../stores/tabs.svelte'
  import type { ColumnMeta } from '../../../types/query'
  import { fetchTableInfo, fetchTableSchema } from '../../../api/query'
  import { formatBytes, formatNumber } from '../../../utils/format'
  import { copyToClipboard } from '../../../utils/export'
  import { success } from '../../../stores/toast.svelte'
  import { getTheme } from '../../../stores/theme.svelte'
  import DataPreview from '../../explorer/DataPreview.svelte'
  import VirtualTable from '../../table/VirtualTable.svelte'
  import Spinner from '../../common/Spinner.svelte'
  import { Table2, Database, HardDrive, Rows3, Clock, Key, LayoutGrid, RefreshCw, Copy, Check, ChevronDown, Braces, FileSpreadsheet, FileText, Code2 } from 'lucide-svelte'
  import ContextMenu from '../../common/ContextMenu.svelte'
  import type { ContextMenuItem } from '../../common/ContextMenu.svelte'
  import { EditorView } from '@codemirror/view'
  import { EditorState, Compartment } from '@codemirror/state'
  import { HighlightStyle, syntaxHighlighting } from '@codemirror/language'
  import { sql, StandardSQL } from '@codemirror/lang-sql'
  import { tags as t } from '@lezer/highlight'

  interface Props {
    tab: TableTab
  }

  let { tab }: Props = $props()

  type SubTab = 'overview' | 'schema' | 'data'
  let activeSubTab = $state<SubTab>('overview')

  // Overview data
  let tableInfo = $state<Record<string, any>>({})
  let infoLoading = $state(true)
  let infoError = $state<string | null>(null)

  // Schema data
  let schemaMeta = $state<ColumnMeta[]>([])
  let schemaData = $state<unknown[][]>([])
  let schemaLoading = $state(false)
  let schemaError = $state<string | null>(null)
  let schemaLoaded = $state(false)

  // Copy button state
  let copied = $state(false)
  let copyTimeout: ReturnType<typeof setTimeout> | undefined

  // Schema copy menu state
  let schemaCopyMenuOpen = $state(false)
  let schemaCopyMenuX = $state(0)
  let schemaCopyMenuY = $state(0)

  // CodeMirror state
  let cmContainer = $state<HTMLDivElement>(undefined!)
  let cmView: EditorView | undefined
  const themeCompartment = new Compartment()
  let observer: MutationObserver | undefined

  const lightTheme = EditorView.theme({
    '&': { backgroundColor: '#ffffff' },
    '.cm-gutters': { backgroundColor: '#f4f4f5', borderRight: '1px solid #d4d4d8' },
    '.cm-activeLineGutter': { backgroundColor: '#ececef' },
    '.cm-activeLine': { backgroundColor: 'transparent' },
    '.cm-selectionBackground': { backgroundColor: '#ffedd5 !important' },
    '&.cm-focused .cm-selectionBackground': { backgroundColor: '#fed7aa !important' },
    '.cm-cursor': { borderLeftColor: '#1f2126' },
    '.cm-matchingBracket': { backgroundColor: '#ffedd5', outline: '1px solid #fb923c' },
  }, { dark: false })

  const darkTheme = EditorView.theme({
    '&': { backgroundColor: '#17181d', color: '#f3f4f6' },
    '.cm-gutters': { backgroundColor: '#1d1f25', borderRight: '1px solid #3f434c', color: '#a5a8b2' },
    '.cm-activeLine': { backgroundColor: 'rgba(249,115,22,0.1)' },
    '.cm-activeLineGutter': { backgroundColor: 'rgba(249,115,22,0.16)' },
    '.cm-selectionBackground': { backgroundColor: 'rgba(251,146,60,0.32) !important' },
    '.cm-matchingBracket': { backgroundColor: 'rgba(249,115,22,0.2)', outline: '1px solid rgba(251,146,60,0.9)' },
    '.cm-cursor': { borderLeftColor: '#f3f4f6' },
  }, { dark: true })

  const lightHighlight = HighlightStyle.define([
    { tag: [t.keyword, t.operatorKeyword, t.controlKeyword, t.definitionKeyword, t.moduleKeyword], color: '#c2410c', fontWeight: '600' },
    { tag: [t.function(t.variableName), t.function(t.propertyName)], color: '#ea580c' },
    { tag: [t.variableName, t.definition(t.variableName), t.definition(t.name), t.special(t.variableName)], color: '#27272a' },
    { tag: [t.propertyName], color: '#166534' },
    { tag: [t.typeName, t.className], color: '#374151', fontWeight: '500' },
    { tag: [t.string, t.special(t.string)], color: '#15803d' },
    { tag: [t.number, t.integer, t.float, t.atom], color: '#b45309' },
    { tag: [t.bool, t.null], color: '#a16207', fontWeight: '600' },
    { tag: [t.comment], color: '#71717a', fontStyle: 'italic' },
    { tag: [t.operator, t.punctuation, t.separator, t.bracket], color: '#52525b' },
    { tag: t.invalid, color: '#b91c1c', textDecoration: 'underline wavy' },
  ])

  const darkHighlight = HighlightStyle.define([
    { tag: [t.keyword, t.operatorKeyword, t.controlKeyword, t.definitionKeyword, t.moduleKeyword], color: '#fb923c', fontWeight: '600' },
    { tag: [t.function(t.variableName), t.function(t.propertyName)], color: '#fdba74' },
    { tag: [t.variableName, t.definition(t.variableName), t.definition(t.name), t.special(t.variableName)], color: '#f4f4f5' },
    { tag: [t.propertyName], color: '#86efac' },
    { tag: [t.typeName, t.className], color: '#d4d4d8', fontWeight: '500' },
    { tag: [t.string, t.special(t.string)], color: '#4ade80' },
    { tag: [t.number, t.integer, t.float, t.atom], color: '#fbbf24' },
    { tag: [t.bool, t.null], color: '#f59e0b', fontWeight: '600' },
    { tag: [t.comment], color: '#9ca3af', fontStyle: 'italic' },
    { tag: [t.operator, t.punctuation, t.separator, t.bracket], color: '#d4d4d8' },
    { tag: t.invalid, color: '#f87171', textDecoration: 'underline wavy' },
  ])

  function getThemeExtension() {
    return getTheme() === 'dark'
      ? [darkTheme, syntaxHighlighting(darkHighlight)]
      : [lightTheme, syntaxHighlighting(lightHighlight)]
  }

  function formatCreateTableSQL(raw: string): string {
    if (!raw) return raw
    let s = raw.trim()
    // Add newline before top-level keywords
    s = s.replace(/\s+(ENGINE\s*=)/gi, '\n$1')
    s = s.replace(/\s+(ORDER\s+BY)/gi, '\n$1')
    s = s.replace(/\s+(PARTITION\s+BY)/gi, '\n$1')
    s = s.replace(/\s+(PRIMARY\s+KEY)/gi, '\n$1')
    s = s.replace(/\s+(SETTINGS)/gi, '\n$1')
    s = s.replace(/\s+(TTL)/gi, '\n$1')
    // Opening paren on same line, but newline after it
    s = s.replace(/\(\s*`/g, '(\n  `')
    // Each column on its own line (comma followed by backtick)
    s = s.replace(/,\s*`/g, ',\n  `')
    // Closing paren on its own line
    s = s.replace(/\)\s*(ENGINE)/gi, '\n)\n$1')
    return s
  }

  function mountCodeMirror(sqlText: string) {
    if (!cmContainer) return
    cmView?.destroy()

    const state = EditorState.create({
      doc: formatCreateTableSQL(sqlText),
      extensions: [
        sql({ dialect: StandardSQL }),
        EditorState.readOnly.of(true),
        EditorView.editable.of(false),
        themeCompartment.of(getThemeExtension()),
        EditorView.theme({
          '&': { fontSize: '12px', maxHeight: '360px' },
          '.cm-scroller': { overflow: 'auto', fontFamily: 'var(--font-mono)' },
          '.cm-content': { padding: '0.75rem 0.875rem 1rem' },
          '.cm-gutters': { display: 'none' },
          '&.cm-focused': { outline: 'none' },
          '.cm-activeLine': { backgroundColor: 'transparent' },
        }),
        EditorView.lineWrapping,
      ],
    })

    cmView = new EditorView({ state, parent: cmContainer })

    observer?.disconnect()
    observer = new MutationObserver(() => {
      cmView?.dispatch({
        effects: themeCompartment.reconfigure(getThemeExtension()),
      })
    })
    observer.observe(document.documentElement, { attributes: true, attributeFilter: ['class'] })
  }

  onMount(() => {
    loadInfo()
  })

  onDestroy(() => {
    cmView?.destroy()
    observer?.disconnect()
    if (copyTimeout) clearTimeout(copyTimeout)
  })

  async function loadInfo() {
    infoLoading = true
    infoError = null
    try {
      tableInfo = await fetchTableInfo(tab.database, tab.table)
    } catch (e: any) {
      infoError = e.message
    } finally {
      infoLoading = false
    }
  }

  function handleRefresh() {
    schemaLoaded = false
    loadInfo()
  }

  async function handleCopy() {
    if (!tableInfo.create_table_query) return
    await copyToClipboard(tableInfo.create_table_query)
    success('Copied to clipboard')
    copied = true
    if (copyTimeout) clearTimeout(copyTimeout)
    copyTimeout = setTimeout(() => { copied = false }, 2000)
  }

  function openSchemaCopyMenu(e: MouseEvent) {
    const rect = (e.currentTarget as HTMLElement).getBoundingClientRect()
    schemaCopyMenuX = rect.left
    schemaCopyMenuY = rect.bottom + 4
    schemaCopyMenuOpen = true
  }

  function schemaRows(): Record<string, unknown>[] {
    return schemaData.map((row) => {
      const obj: Record<string, unknown> = {}
      schemaMeta.forEach((col, i) => { obj[col.name] = (row as unknown[])[i] ?? '' })
      return obj
    })
  }

  async function copySchemaAsJSON() {
    await copyToClipboard(JSON.stringify(schemaRows(), null, 2))
    success('Schema copied as JSON')
  }

  async function copySchemaAsTSV() {
    const header = schemaMeta.map((c) => c.name).join('\t')
    const rows = schemaData.map((row) => (row as unknown[]).map((v) => v ?? '').join('\t'))
    await copyToClipboard([header, ...rows].join('\n'))
    success('Schema copied as TSV')
  }

  async function copySchemaAsCSV() {
    const escape = (v: unknown) => {
      const s = String(v ?? '')
      return s.includes(',') || s.includes('"') || s.includes('\n') ? `"${s.replace(/"/g, '""')}"` : s
    }
    const header = schemaMeta.map((c) => escape(c.name)).join(',')
    const rows = schemaData.map((row) => (row as unknown[]).map((v) => escape(v)).join(','))
    await copyToClipboard([header, ...rows].join('\n'))
    success('Schema copied as CSV')
  }

  async function copySchemaAsSQL() {
    const cols = schemaRows()
      .map((r) => `  ${r.name} ${r.type}`)
      .join(',\n')
    const ddl = `CREATE TABLE ${tab.database}.${tab.table}\n(\n${cols}\n)`
    await copyToClipboard(ddl)
    success('Schema copied as SQL')
  }

  const schemaCopyItems: ContextMenuItem[] = [
    { id: 'json', label: 'Copy as JSON', icon: Braces, onSelect: copySchemaAsJSON },
    { id: 'tsv', label: 'Copy as TSV', icon: FileSpreadsheet, onSelect: copySchemaAsTSV },
    { id: 'csv', label: 'Copy as CSV', icon: FileText, onSelect: copySchemaAsCSV },
    { id: 'sql', label: 'Copy as SQL', icon: Code2, onSelect: copySchemaAsSQL },
  ]

  async function loadSchema() {
    if (schemaLoaded) return
    schemaLoading = true
    schemaError = null
    try {
      const res = await fetchTableSchema(tab.database, tab.table)
      schemaMeta = res.meta ?? []
      schemaData = (res.data ?? []).map((row: any) => {
        if (Array.isArray(row)) return row
        return res.meta.map((col: any) => row[col.name])
      })
      schemaLoaded = true
    } catch (e: any) {
      schemaError = e.message
    } finally {
      schemaLoading = false
    }
  }

  function switchTab(t: SubTab) {
    activeSubTab = t
    if (t === 'schema') loadSchema()
  }

  function formatDateTime(value: unknown): string {
    if (!value) return '—'
    const date = new Date(String(value))
    if (Number.isNaN(date.getTime())) return String(value)
    try {
      return new Intl.DateTimeFormat(undefined, {
        year: 'numeric',
        month: '2-digit',
        day: '2-digit',
        hour: '2-digit',
        minute: '2-digit',
        second: '2-digit',
      }).format(date)
    } catch {
      return date.toLocaleString()
    }
  }

  const subTabs: { id: SubTab; label: string }[] = [
    { id: 'overview', label: 'Overview' },
    { id: 'schema', label: 'Schema' },
    { id: 'data', label: 'Data Sample' },
  ]

  // Metric cards
  const metrics = $derived.by(() => {
    if (!tableInfo || Object.keys(tableInfo).length === 0) return []
    return [
      { label: 'Rows', value: formatNumber(Number(tableInfo.total_rows ?? 0)), icon: Rows3, color: 'text-ch-orange' },
      { label: 'Size', value: formatBytes(Number(tableInfo.total_bytes ?? 0)), icon: HardDrive, color: 'text-ch-green' },
      { label: 'Engine', value: tableInfo.engine ?? '—', icon: Database, color: 'text-ch-orange' },
      { label: 'Last Modified', value: formatDateTime(tableInfo.metadata_modification_time), icon: Clock, color: 'text-gray-500' },
    ]
  })

  // Whether any keys exist
  const hasKeys = $derived(
    !!(tableInfo.partition_key || tableInfo.sorting_key || tableInfo.primary_key || tableInfo.sampling_key)
  )

  // Mount CodeMirror when create_table_query becomes available and tab is overview
  $effect(() => {
    if (activeSubTab === 'overview' && tableInfo.create_table_query && cmContainer) {
      mountCodeMirror(tableInfo.create_table_query)
    }
  })
</script>

<div class="flex flex-col h-full">
  <!-- Header -->
  <div class="flex items-center gap-3 px-4 py-3 border-b border-gray-200 dark:border-gray-800 bg-gray-100/40 dark:bg-gray-900/45 shrink-0">
    <Table2 size={16} class="text-ch-orange shrink-0" />
    <div class="min-w-0">
      <span class="text-sm text-gray-500">{tab.database}.</span>
      <span class="text-sm font-medium text-gray-800 dark:text-gray-200 truncate">{tab.table}</span>
    </div>
    <button
      class="ml-1 p-1 rounded hover:bg-gray-200 dark:hover:bg-gray-800 transition-colors text-gray-400 hover:text-gray-600 dark:hover:text-gray-300"
      onclick={handleRefresh}
      title="Refresh table info"
    >
      <RefreshCw size={13} class={infoLoading ? 'animate-spin' : ''} />
    </button>
  </div>

  <!-- Sub-tab bar -->
  <div class="flex items-center gap-1 px-3 py-1.5 border-b border-gray-200 dark:border-gray-800 bg-white dark:bg-gray-950 shrink-0">
    {#each subTabs as st}
      <button
        class="px-3 py-1.5 text-xs rounded-md transition-colors whitespace-nowrap {activeSubTab === st.id
          ? 'bg-gray-200 dark:bg-gray-800 text-gray-800 dark:text-gray-200 font-medium'
          : 'text-gray-500 hover:text-gray-700 dark:hover:text-gray-300 hover:bg-gray-200/50 dark:hover:bg-gray-800/50'}"
        onclick={() => switchTab(st.id)}
      >{st.label}</button>
    {/each}
  </div>

  <!-- Sub-tab content -->
  <div class="flex-1 min-h-0 overflow-auto">
    {#if activeSubTab === 'overview'}
      {#if infoLoading}
        <div class="flex items-center justify-center py-12 gap-2">
          <Spinner size="sm" />
          <span class="text-sm text-gray-500">Loading table info...</span>
        </div>
      {:else if infoError}
        <div class="p-4">
          <div class="bg-red-100/20 dark:bg-red-900/20 border border-red-300/50 dark:border-red-800/50 rounded-lg p-3 text-sm text-red-700 dark:text-red-300">{infoError}</div>
        </div>
      {:else}
        <!-- Metric cards -->
        <div class="grid grid-cols-2 xl:grid-cols-4 gap-3 p-4">
          {#each metrics as m}
            {@const Icon = m.icon}
            <div class="bg-gray-50 dark:bg-gray-900 border border-gray-200 dark:border-gray-800 rounded-xl p-4">
              <div class="flex items-center gap-2 mb-2">
                <Icon size={14} class={m.color} />
                <span class="text-xs text-gray-500 uppercase tracking-wider">{m.label}</span>
              </div>
              <div class="text-xl font-semibold text-gray-800 dark:text-gray-200 truncate">{m.value}</div>
            </div>
          {/each}
        </div>

        <!-- Detail rows (keys) — only if any keys exist -->
        {#if hasKeys}
          <div class="px-4 pb-4">
            <div class="bg-gray-50 dark:bg-gray-900 border border-gray-200 dark:border-gray-800 rounded-xl p-4">
              <h3 class="text-xs text-gray-500 uppercase tracking-wider mb-3">Storage Keys</h3>
              <div class="grid grid-cols-[140px_1fr] gap-y-2 gap-x-3 text-sm">
              {#if tableInfo.partition_key}
                <span class="text-gray-500 inline-flex items-center gap-2"><LayoutGrid size={13} class="shrink-0" />Partition Key</span>
                <code class="text-xs text-gray-700 dark:text-gray-300 font-mono truncate">{tableInfo.partition_key}</code>
              {/if}
              {#if tableInfo.sorting_key}
                <span class="text-gray-500 inline-flex items-center gap-2"><Key size={13} class="shrink-0" />Sorting Key</span>
                <code class="text-xs text-gray-700 dark:text-gray-300 font-mono truncate">{tableInfo.sorting_key}</code>
              {/if}
              {#if tableInfo.primary_key}
                <span class="text-gray-500 inline-flex items-center gap-2"><Key size={13} class="text-ch-orange shrink-0" />Primary Key</span>
                <code class="text-xs text-gray-700 dark:text-gray-300 font-mono truncate">{tableInfo.primary_key}</code>
              {/if}
              {#if tableInfo.sampling_key}
                <span class="text-gray-500 inline-flex items-center gap-2"><Key size={13} class="shrink-0" />Sampling Key</span>
                <code class="text-xs text-gray-700 dark:text-gray-300 font-mono truncate">{tableInfo.sampling_key}</code>
              {/if}
              </div>
            </div>
          </div>
        {/if}

        <!-- CREATE TABLE statement -->
        {#if tableInfo.create_table_query}
          <div class="px-4 pb-5">
            <div class="mb-2.5 flex items-center justify-between">
              <h4 class="text-xs text-gray-500 uppercase tracking-wider">Create Table SQL</h4>
              <span class="text-[11px] text-gray-400">Syntax highlighted</span>
            </div>
            <div class="relative bg-gray-50/80 dark:bg-gray-900/80 border border-gray-200 dark:border-gray-800 rounded-xl p-2">
              <button
                class="absolute top-2 right-2 z-10 p-1.5 rounded-md bg-gray-200/80 dark:bg-gray-800/80 hover:bg-gray-300 dark:hover:bg-gray-700 transition-colors text-gray-500 hover:text-gray-700 dark:hover:text-gray-300"
                onclick={handleCopy}
                title="Copy CREATE TABLE"
              >
                {#if copied}
                  <Check size={13} class="text-green-500" />
                {:else}
                  <Copy size={13} />
                {/if}
              </button>
              <div class="rounded-lg overflow-hidden border border-gray-200 dark:border-gray-800" bind:this={cmContainer}></div>
            </div>
          </div>
        {/if}
      {/if}

    {:else if activeSubTab === 'schema'}
      {#if schemaLoading}
        <div class="flex items-center justify-center py-12 gap-2">
          <Spinner size="sm" />
          <span class="text-sm text-gray-500">Loading schema...</span>
        </div>
      {:else if schemaError}
        <div class="p-4">
          <div class="bg-red-100/20 dark:bg-red-900/20 border border-red-300/50 dark:border-red-800/50 rounded-lg p-3 text-sm text-red-700 dark:text-red-300">{schemaError}</div>
        </div>
      {:else if schemaMeta.length > 0}
        <div class="flex items-center justify-end px-3 py-1.5 border-b border-gray-200 dark:border-gray-800">
          <button
            class="inline-flex items-center gap-1.5 px-2.5 py-1 text-xs rounded-md transition-colors
              bg-gray-100 dark:bg-gray-800 text-gray-600 dark:text-gray-300 hover:bg-gray-200 dark:hover:bg-gray-700"
            onclick={openSchemaCopyMenu}
            title="Copy schema to clipboard"
          >
            <Copy size={12} />
            Copy Schema
            <ChevronDown size={11} class="text-gray-400 transition-transform {schemaCopyMenuOpen ? 'rotate-180' : ''}" />
          </button>
        </div>
        <ContextMenu
          open={schemaCopyMenuOpen}
          x={schemaCopyMenuX}
          y={schemaCopyMenuY}
          items={schemaCopyItems}
          onclose={() => schemaCopyMenuOpen = false}
        />
        <VirtualTable meta={schemaMeta} data={schemaData} />
      {:else}
        <div class="flex items-center justify-center py-12 text-gray-400 dark:text-gray-600 text-sm">No schema data</div>
      {/if}

    {:else if activeSubTab === 'data'}
      <DataPreview database={tab.database} table={tab.table} />
    {/if}
  </div>
</div>
