<script lang="ts">
  import { onMount } from 'svelte'
  import { queryLogs, fetchLogServices, fetchLogHistogram } from '../../api/telemetry'
  import type { LogEntry, LogHistogramBucket, SeverityLevel } from '../../types/telemetry'
  import SeverityFilter from './SeverityFilter.svelte'
  import LogRow from './LogRow.svelte'
  import VolumeHistogram from './VolumeHistogram.svelte'
  import Button from '../common/Button.svelte'
  import Spinner from '../common/Spinner.svelte'
  import { Search, ChevronDown, RefreshCw } from 'lucide-svelte'

  interface Props {
    database: string
    table: string
  }

  let { database, table }: Props = $props()

  let logs = $state<LogEntry[]>([])
  let loading = $state(false)
  let histogramBuckets = $state<LogHistogramBucket[]>([])
  let histogramLoading = $state(false)
  let services = $state<string[]>([])
  let hasMore = $state(false)

  // Filters
  let searchText = $state('')
  let selectedSeverity = $state<SeverityLevel[]>([])
  let selectedServices = $state<string[]>([])
  let timeRange = $state('1h')
  let serviceDropdownOpen = $state(false)
  let serviceDropdownEl = $state<HTMLDivElement>(undefined!)

  const TIME_RANGES = [
    { label: '15m', value: '15m' },
    { label: '1h', value: '1h' },
    { label: '6h', value: '6h' },
    { label: '24h', value: '24h' },
    { label: '3d', value: '3d' },
    { label: '7d', value: '7d' },
  ]

  function getTimeFromRange(range: string): { from: string; to: string } {
    const now = new Date()
    const to = now.toISOString()
    const ms: Record<string, number> = {
      '15m': 15 * 60 * 1000,
      '1h': 60 * 60 * 1000,
      '6h': 6 * 60 * 60 * 1000,
      '24h': 24 * 60 * 60 * 1000,
      '3d': 3 * 24 * 60 * 60 * 1000,
      '7d': 7 * 24 * 60 * 60 * 1000,
    }
    const from = new Date(now.getTime() - (ms[range] ?? ms['1h'])).toISOString()
    return { from, to }
  }

  async function loadLogs(append = false) {
    loading = true
    try {
      const { from, to } = getTimeFromRange(timeRange)
      const res = await queryLogs({
        database,
        table,
        timeFrom: from,
        timeTo: to,
        severity: selectedSeverity,
        services: selectedServices,
        search: searchText,
        limit: 100,
        offset: append ? logs.length : 0,
      })
      const newLogs = (res.data ?? []) as LogEntry[]
      if (append) {
        logs = [...logs, ...newLogs]
      } else {
        logs = newLogs
      }
      hasMore = newLogs.length === 100
    } catch {
      logs = append ? logs : []
    } finally {
      loading = false
    }
  }

  async function loadHistogram() {
    histogramLoading = true
    try {
      const { from, to } = getTimeFromRange(timeRange)
      const res = await fetchLogHistogram({
        database,
        table,
        timeFrom: from,
        timeTo: to,
        severity: selectedSeverity,
        services: selectedServices,
        search: searchText,
      })
      histogramBuckets = (res.data ?? []) as LogHistogramBucket[]
    } catch {
      histogramBuckets = []
    } finally {
      histogramLoading = false
    }
  }

  async function loadServices() {
    try {
      const res = await fetchLogServices(database, table)
      services = (res.data ?? []).map((d: Record<string, unknown>) => String(d.ServiceName ?? '')).filter(Boolean)
    } catch {
      services = []
    }
  }

  function refresh() {
    loadLogs()
    loadHistogram()
  }

  function handleSearch(e: KeyboardEvent) {
    if (e.key === 'Enter') refresh()
  }

  function handleSeverityChange(selected: SeverityLevel[]) {
    selectedSeverity = selected
    refresh()
  }

  function toggleService(svc: string) {
    if (selectedServices.includes(svc)) {
      selectedServices = selectedServices.filter(s => s !== svc)
    } else {
      selectedServices = [...selectedServices, svc]
    }
    refresh()
  }

  function handleTimeRangeChange(range: string) {
    timeRange = range
    refresh()
  }

  $effect(() => {
    if (serviceDropdownOpen) {
      const handler = (e: MouseEvent) => {
        if (serviceDropdownEl && !serviceDropdownEl.contains(e.target as Node)) {
          serviceDropdownOpen = false
        }
      }
      document.addEventListener('mousedown', handler)
      return () => document.removeEventListener('mousedown', handler)
    }
  })

  onMount(() => {
    loadServices()
    refresh()
  })
</script>

<div class="flex flex-col h-full min-h-0">
  <!-- Search bar -->
  <div class="px-4 pt-3 pb-2">
    <div class="relative">
      <Search size={14} class="absolute left-3 top-1/2 -translate-y-1/2 text-gray-400" />
      <input
        type="text"
        bind:value={searchText}
        onkeydown={handleSearch}
        placeholder="Search logs... (press Enter)"
        class="w-full pl-9 pr-3 py-2 text-sm bg-gray-50 dark:bg-gray-900 border border-gray-200 dark:border-gray-800 rounded-lg text-gray-800 dark:text-gray-200 placeholder-gray-400 focus:outline-none focus:ring-2 focus:ring-ch-blue/40 focus:border-ch-blue font-mono"
      />
    </div>
  </div>

  <!-- Filter bar -->
  <div class="flex items-center gap-3 px-4 py-2 border-b border-gray-200 dark:border-gray-800">
    <SeverityFilter selected={selectedSeverity} onchange={handleSeverityChange} />

    <!-- Service dropdown -->
    <div class="relative" bind:this={serviceDropdownEl}>
      <button
        class="flex items-center gap-1 px-2 py-1 text-[11px] rounded-md border border-gray-200 dark:border-gray-700 bg-white dark:bg-gray-900 text-gray-600 dark:text-gray-400 hover:bg-gray-50 dark:hover:bg-gray-800 transition-colors"
        onclick={() => serviceDropdownOpen = !serviceDropdownOpen}
      >
        Services
        {#if selectedServices.length > 0}
          <span class="text-[9px] bg-ch-blue text-white rounded-full px-1.5 leading-relaxed">{selectedServices.length}</span>
        {/if}
        <ChevronDown size={10} />
      </button>
      {#if serviceDropdownOpen}
        <div class="absolute top-full mt-1 left-0 z-50 w-52 max-h-60 overflow-y-auto bg-white dark:bg-gray-900 border border-gray-200 dark:border-gray-700 rounded-lg shadow-xl">
          {#if services.length === 0}
            <p class="text-xs text-gray-400 p-3">No services found</p>
          {:else}
            {#each services as svc}
              <button
                class="w-full flex items-center gap-2 px-3 py-1.5 text-xs text-left hover:bg-gray-50 dark:hover:bg-gray-800 transition-colors
                  {selectedServices.includes(svc) ? 'text-ch-blue font-medium' : 'text-gray-600 dark:text-gray-400'}"
                onclick={() => toggleService(svc)}
              >
                <span class="w-3 h-3 rounded border flex items-center justify-center shrink-0
                  {selectedServices.includes(svc) ? 'bg-ch-blue border-ch-blue' : 'border-gray-300 dark:border-gray-600'}">
                  {#if selectedServices.includes(svc)}
                    <svg viewBox="0 0 12 12" class="w-2 h-2 text-white"><path d="M2 6l3 3 5-5" stroke="currentColor" fill="none" stroke-width="2" /></svg>
                  {/if}
                </span>
                {svc}
              </button>
            {/each}
          {/if}
        </div>
      {/if}
    </div>

    <div class="flex-1"></div>

    <!-- Time range -->
    <div class="flex items-center gap-1">
      {#each TIME_RANGES as tr}
        <button
          class="px-2 py-0.5 text-[10px] font-medium rounded transition-colors
            {timeRange === tr.value
              ? 'bg-ch-blue text-white'
              : 'text-gray-500 hover:text-gray-700 dark:hover:text-gray-300 hover:bg-gray-100 dark:hover:bg-gray-800'}"
          onclick={() => handleTimeRangeChange(tr.value)}
        >
          {tr.label}
        </button>
      {/each}
    </div>

    <button
      class="p-1.5 rounded-md text-gray-400 hover:text-gray-600 dark:hover:text-gray-300 hover:bg-gray-100 dark:hover:bg-gray-800 transition-colors"
      onclick={refresh}
      title="Refresh"
    >
      <RefreshCw size={13} />
    </button>
  </div>

  <!-- Volume histogram -->
  <div class="px-4 py-2 border-b border-gray-200 dark:border-gray-800">
    <VolumeHistogram buckets={histogramBuckets} loading={histogramLoading} />
  </div>

  <!-- Log list -->
  <div class="flex-1 min-h-0 overflow-y-auto">
    {#if loading && logs.length === 0}
      <div class="flex items-center justify-center py-12">
        <Spinner size="sm" />
      </div>
    {:else if logs.length === 0}
      <div class="text-center py-12">
        <p class="text-sm text-gray-500 mb-1">No logs found</p>
        <p class="text-xs text-gray-400">Try adjusting your filters or time range</p>
      </div>
    {:else}
      {#each logs as entry (entry.Timestamp + entry.SpanId + entry.Body?.slice(0, 50))}
        <LogRow {entry} />
      {/each}

      {#if hasMore}
        <div class="flex justify-center py-4">
          <Button size="sm" variant="secondary" loading={loading} onclick={() => loadLogs(true)}>
            Load More
          </Button>
        </div>
      {/if}
    {/if}
  </div>
</div>
