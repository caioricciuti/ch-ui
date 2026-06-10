<script lang="ts">
  import { onMount } from 'svelte'
  import type { QueryHistoryEntry } from '../../api/history'
  import { fetchQueryHistory, deleteQueryHistoryEntry, clearQueryHistory } from '../../api/history'
  import { formatNumber, formatElapsed } from '../../utils/format'
  import { error as toastError, success as toastSuccess } from '../../stores/toast.svelte'
  import Spinner from '../common/Spinner.svelte'
  import { Search, X, ExternalLink, Replace, Copy, Trash2, History, CircleCheck, CircleX, CircleMinus } from 'lucide-svelte'

  interface Props {
    onopen: (sql: string) => void
    oninsert: (sql: string) => void
  }

  let { onopen, oninsert }: Props = $props()

  const PAGE_SIZE = 50

  let entries = $state<QueryHistoryEntry[]>([])
  let loading = $state(false)
  let loadError = $state<string | null>(null)
  let search = $state('')
  let statusFilter = $state<'' | 'success' | 'error'>('')
  let hasMore = $state(false)
  let confirmClear = $state(false)
  let searchTimer: ReturnType<typeof setTimeout> | null = null
  let loadSeq = 0

  async function load(reset = true) {
    const seq = ++loadSeq
    loading = true
    if (reset) loadError = null
    try {
      const offset = reset ? 0 : entries.length
      const batch = await fetchQueryHistory({
        search: search.trim(),
        status: statusFilter,
        limit: PAGE_SIZE,
        offset,
      })
      // A newer request superseded this one (fast typing, filter change).
      if (seq !== loadSeq) return
      if (reset) {
        entries = batch
      } else {
        // New runs landing between pages shift offsets — drop duplicates so
        // the keyed each-block never sees the same id twice.
        const seen = new Set(entries.map((e) => e.id))
        entries = [...entries, ...batch.filter((b) => !seen.has(b.id))]
      }
      hasMore = batch.length === PAGE_SIZE
    } catch (e: any) {
      if (seq !== loadSeq) return
      // Keep the loaded list visible when only a load-more fails.
      if (reset) loadError = e.message
      else toastError(e.message)
    } finally {
      if (seq === loadSeq) loading = false
    }
  }

  onMount(() => {
    void load()
    return () => {
      if (searchTimer) clearTimeout(searchTimer)
    }
  })

  function handleSearchInput() {
    if (searchTimer) clearTimeout(searchTimer)
    searchTimer = setTimeout(() => void load(), 300)
  }

  function setStatusFilter(value: '' | 'success' | 'error') {
    statusFilter = value
    void load()
  }

  async function handleDelete(id: string) {
    try {
      await deleteQueryHistoryEntry(id)
      entries = entries.filter((e) => e.id !== id)
    } catch (e: any) {
      toastError(e.message)
    }
  }

  async function handleClear() {
    if (!confirmClear) {
      confirmClear = true
      setTimeout(() => (confirmClear = false), 3000)
      return
    }
    confirmClear = false
    try {
      await clearQueryHistory()
      entries = []
      hasMore = false
      toastSuccess('Query history cleared')
    } catch (e: any) {
      toastError(e.message)
    }
  }

  async function handleCopy(sql: string) {
    try {
      await navigator.clipboard.writeText(sql)
      toastSuccess('SQL copied to clipboard')
    } catch {
      toastError('Failed to copy to clipboard')
    }
  }

  // SQLite timestamps are "YYYY-MM-DD HH:MM:SS" in UTC — normalize before parsing.
  function parseTime(value: string): number {
    const normalized = /^\d{4}-\d{2}-\d{2} /.test(value) ? value.replace(' ', 'T') + 'Z' : value
    const t = Date.parse(normalized)
    return Number.isFinite(t) ? t : 0
  }

  function formatRelative(value: string): string {
    const t = parseTime(value)
    if (!t) return 'unknown'
    const delta = Math.max(0, Date.now() - t)
    const minutes = Math.floor(delta / 60000)
    if (minutes < 1) return 'just now'
    if (minutes < 60) return `${minutes}m ago`
    const hours = Math.floor(minutes / 60)
    if (hours < 24) return `${hours}h ago`
    const days = Math.floor(hours / 24)
    if (days < 30) return `${days}d ago`
    const months = Math.floor(days / 30)
    if (months < 12) return `${months}mo ago`
    return `${Math.floor(months / 12)}y ago`
  }

  function formatAbsolute(value: string): string {
    const t = parseTime(value)
    return t ? new Date(t).toLocaleString() : value
  }

  const statusOptions: { value: '' | 'success' | 'error'; label: string }[] = [
    { value: '', label: 'All' },
    { value: 'success', label: 'Success' },
    { value: 'error', label: 'Errors' },
  ]
</script>

<div class="flex flex-col gap-3 h-full">
  <!-- Search + filters -->
  <div class="flex items-center gap-2 shrink-0">
    <div class="relative flex-1">
      <Search size={13} class="absolute left-2.5 top-1/2 -translate-y-1/2 text-gray-400" />
      <input
        class="w-full pl-8 pr-7 py-1.5 text-xs rounded-md border border-gray-300 dark:border-gray-700 bg-white dark:bg-gray-950 text-gray-700 dark:text-gray-200 focus:outline-none focus:border-ch-blue"
        placeholder="Search history..."
        bind:value={search}
        oninput={handleSearchInput}
        spellcheck="false"
      />
      {#if search}
        <button
          class="absolute right-2 top-1/2 -translate-y-1/2 text-gray-400 hover:text-gray-600 dark:hover:text-gray-300"
          onclick={() => { search = ''; void load() }}
          aria-label="Clear search"
        >
          <X size={13} />
        </button>
      {/if}
    </div>

    <div class="flex items-center rounded-md border border-gray-300 dark:border-gray-700 overflow-hidden">
      {#each statusOptions as opt}
        <button
          class="px-2 py-1.5 text-xs transition-colors
            {statusFilter === opt.value
              ? 'bg-ch-blue text-white'
              : 'text-gray-500 dark:text-gray-400 hover:bg-gray-100 dark:hover:bg-gray-800'}"
          onclick={() => setStatusFilter(opt.value)}
        >{opt.label}</button>
      {/each}
    </div>

    <button
      class="px-2 py-1.5 text-xs rounded-md border transition-colors
        {confirmClear
          ? 'border-red-400 text-red-500 bg-red-50 dark:bg-red-900/20'
          : 'border-gray-300 dark:border-gray-700 text-gray-500 dark:text-gray-400 hover:text-red-500 hover:border-red-300'}"
      onclick={handleClear}
      title="Delete all history entries for this connection"
    >{confirmClear ? 'Confirm?' : 'Clear all'}</button>
  </div>

  <!-- Entries -->
  {#if loading && entries.length === 0}
    <div class="flex items-center justify-center flex-1 text-gray-500">
      <Spinner size="sm" />
    </div>
  {:else if loadError}
    <div class="flex-1 p-3 text-sm text-red-600 dark:text-red-400">{loadError}</div>
  {:else if entries.length === 0}
    <div class="flex flex-col items-center justify-center flex-1 gap-2 text-gray-400 dark:text-gray-600">
      <History size={26} class="text-gray-300 dark:text-gray-700" />
      <p class="text-sm">No queries in history yet</p>
      <p class="text-xs text-gray-300 dark:text-gray-700">Queries you run are recorded automatically</p>
    </div>
  {:else}
    <div class="flex-1 overflow-y-auto -mx-1 px-1">
      <ul class="flex flex-col gap-1.5">
        {#each entries as entry (entry.id)}
          <li class="group rounded-lg border border-gray-200 dark:border-gray-800 bg-white dark:bg-gray-950/60 hover:border-ch-blue/40 transition-colors">
            <button
              class="w-full text-left px-3 pt-2 pb-1"
              onclick={() => onopen(entry.query_text)}
              title="Open in a new query tab"
            >
              <pre class="font-mono text-[11px] leading-4 text-gray-700 dark:text-gray-300 whitespace-pre-wrap break-words line-clamp-2">{entry.query_text}</pre>
              {#if entry.status === 'error' && entry.error_message}
                <p class="mt-1 text-[11px] text-red-500 dark:text-red-400 truncate" title={entry.error_message}>{entry.error_message}</p>
              {/if}
            </button>

            <div class="flex items-center gap-2.5 px-3 pb-2 text-[11px] text-gray-400 dark:text-gray-500">
              {#if entry.status === 'success'}
                <CircleCheck size={11} class="text-emerald-500 shrink-0" />
              {:else if entry.status === 'cancelled'}
                <CircleMinus size={11} class="text-gray-400 shrink-0" />
              {:else}
                <CircleX size={11} class="text-red-500 shrink-0" />
              {/if}
              <span title={formatAbsolute(entry.created_at)}>{formatRelative(entry.created_at)}</span>
              {#if entry.elapsed_ms !== null}
                <span>{formatElapsed(entry.elapsed_ms / 1000)}</span>
              {/if}
              {#if entry.status === 'success' && entry.rows_returned !== null}
                <span>{formatNumber(entry.rows_returned)} rows</span>
              {/if}

              <div class="flex-1"></div>

              <div class="flex items-center gap-0.5 opacity-0 group-hover:opacity-100 transition-opacity">
                <button class="p-1 rounded hover:bg-gray-100 dark:hover:bg-gray-800 hover:text-gray-700 dark:hover:text-gray-200" onclick={() => onopen(entry.query_text)} title="Open in new tab" aria-label="Open in new tab">
                  <ExternalLink size={12} />
                </button>
                <button class="p-1 rounded hover:bg-gray-100 dark:hover:bg-gray-800 hover:text-gray-700 dark:hover:text-gray-200" onclick={() => oninsert(entry.query_text)} title="Replace current editor contents (undo with Cmd/Ctrl+Z)" aria-label="Replace current editor contents">
                  <Replace size={12} />
                </button>
                <button class="p-1 rounded hover:bg-gray-100 dark:hover:bg-gray-800 hover:text-gray-700 dark:hover:text-gray-200" onclick={() => handleCopy(entry.query_text)} title="Copy SQL" aria-label="Copy SQL">
                  <Copy size={12} />
                </button>
                <button class="p-1 rounded hover:bg-red-50 dark:hover:bg-red-900/20 hover:text-red-500" onclick={() => handleDelete(entry.id)} title="Delete entry" aria-label="Delete entry">
                  <Trash2 size={12} />
                </button>
              </div>
            </div>
          </li>
        {/each}
      </ul>

      {#if hasMore}
        <div class="flex justify-center py-3">
          <button
            class="px-3 py-1.5 text-xs rounded-md border border-gray-300 dark:border-gray-700 text-gray-600 dark:text-gray-300 hover:bg-gray-100 dark:hover:bg-gray-800 disabled:opacity-50"
            onclick={() => void load(false)}
            disabled={loading}
          >{loading ? 'Loading...' : 'Load more'}</button>
        </div>
      {/if}
    </div>
  {/if}
</div>
