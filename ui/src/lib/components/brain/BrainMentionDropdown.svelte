<script lang="ts">
  import { getDatabases, loadTables } from '../../stores/schema.svelte'
  import { Database as DbIcon, Loader2 } from 'lucide-svelte'

  interface Props {
    query: string
    onSelect: (database: string, table: string) => void
    onDismiss: () => void
  }

  let { query, onSelect, onDismiss }: Props = $props()

  let selectedIndex = $state(0)
  let loadingDb = $state<string | null>(null)

  interface MentionOption {
    database: string
    table: string
    label: string
    loadTables?: boolean
  }

  const filteredOptions = $derived.by<MentionOption[]>(() => {
    const dbs = getDatabases()
    const q = query.toLowerCase()
    const results: MentionOption[] = []

    for (const db of dbs) {
      if (!db.tables || db.tables.length === 0) {
        // Database without tables loaded — show a "load" option
        const label = `${db.name}.* (load tables...)`
        if (!q || label.toLowerCase().includes(q) || db.name.toLowerCase().includes(q)) {
          results.push({ database: db.name, table: '', label, loadTables: true })
        }
        continue
      }
      for (const table of db.tables) {
        const label = `${db.name}.${table.name}`
        if (!q || label.toLowerCase().includes(q)) {
          results.push({ database: db.name, table: table.name, label })
        }
        if (results.length >= 50) break
      }
      if (results.length >= 50) break
    }
    return results
  })

  // Reset selected index when options change
  $effect(() => {
    filteredOptions // track
    selectedIndex = 0
  })

  async function handleSelect(opt: MentionOption) {
    if (opt.loadTables) {
      loadingDb = opt.database
      await loadTables(opt.database)
      loadingDb = null
      return
    }
    onSelect(opt.database, opt.table)
  }

  export function handleKeydown(e: KeyboardEvent): boolean {
    if (filteredOptions.length === 0) return false

    if (e.key === 'ArrowDown') {
      e.preventDefault()
      selectedIndex = (selectedIndex + 1) % filteredOptions.length
      return true
    }
    if (e.key === 'ArrowUp') {
      e.preventDefault()
      selectedIndex = (selectedIndex - 1 + filteredOptions.length) % filteredOptions.length
      return true
    }
    if (e.key === 'Enter') {
      e.preventDefault()
      const opt = filteredOptions[selectedIndex]
      if (opt) handleSelect(opt)
      return true
    }
    if (e.key === 'Escape') {
      e.preventDefault()
      onDismiss()
      return true
    }
    return false
  }
</script>

{#if filteredOptions.length > 0}
  <div class="absolute bottom-full left-0 right-0 mb-1 max-h-60 overflow-auto rounded-lg border border-gray-200 dark:border-gray-700 bg-white dark:bg-gray-900 shadow-lg z-50">
    {#each filteredOptions as opt, i (opt.label)}
      <button
        class="w-full text-left px-3 py-1.5 text-sm flex items-center gap-2 transition-colors
          {i === selectedIndex ? 'bg-ch-blue/10 text-ch-blue' : 'text-gray-700 dark:text-gray-300 hover:bg-gray-100 dark:hover:bg-gray-800'}"
        onmouseenter={() => selectedIndex = i}
        onclick={() => handleSelect(opt)}
      >
        {#if opt.loadTables && loadingDb === opt.database}
          <Loader2 size={14} class="animate-spin shrink-0" />
        {:else}
          <DbIcon size={14} class="shrink-0 opacity-50" />
        {/if}
        <span class="truncate">{opt.label}</span>
      </button>
    {/each}
  </div>
{/if}
