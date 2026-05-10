<script lang="ts">
  import { onMount } from 'svelte'
  import { fetchTelemetrySchema } from '../lib/api/telemetry'
  import type { TelemetryTable } from '../lib/types/telemetry'
  import LogExplorer from '../lib/components/telemetry/LogExplorer.svelte'
  import SetupWizard from '../lib/components/telemetry/SetupWizard.svelte'
  import Spinner from '../lib/components/common/Spinner.svelte'
  import { Activity, FileText, GitBranch, BarChart3, Network } from 'lucide-svelte'

  type TelemetryTab = 'logs' | 'traces' | 'metrics' | 'services'

  let activeTab = $state<TelemetryTab>('logs')
  let loading = $state(true)
  let error = $state<string | null>(null)
  let tables = $state<TelemetryTable[]>([])
  let hasLogs = $state(false)
  let hasTraces = $state(false)
  let hasMetrics = $state(false)
  let logsDatabase = $state('default')
  let logsTable = $state('otel_logs')
  let needsSetup = $state(false)

  const tabs: { id: TelemetryTab; label: string; icon: typeof FileText; available: boolean }[] = $derived([
    { id: 'logs', label: 'Logs', icon: FileText, available: hasLogs },
    { id: 'traces', label: 'Traces', icon: GitBranch, available: hasTraces },
    { id: 'metrics', label: 'Metrics', icon: BarChart3, available: hasMetrics },
    { id: 'services', label: 'Services', icon: Network, available: hasTraces },
  ])

  async function detectSchema() {
    loading = true
    error = null
    needsSetup = false
    try {
      const res = await fetchTelemetrySchema()
      const rawTables = (res.tables ?? []) as TelemetryTable[]
      tables = rawTables

      hasLogs = rawTables.some(t => t.name === 'otel_logs')
      hasTraces = rawTables.some(t => t.name === 'otel_traces')
      hasMetrics = rawTables.some(t => t.name?.startsWith('otel_metrics'))

      if (hasLogs) {
        const logsT = rawTables.find(t => t.name === 'otel_logs')
        if (logsT) {
          logsDatabase = logsT.database
          logsTable = logsT.name
        }
      }

      if (!hasLogs && !hasTraces && !hasMetrics) {
        needsSetup = true
      }
    } catch (e: unknown) {
      error = e instanceof Error ? e.message : 'Failed to detect schema'
    } finally {
      loading = false
    }
  }

  function handleConfigured() {
    needsSetup = false
    hasLogs = true
  }

  onMount(() => {
    detectSchema()
  })
</script>

<div class="flex flex-col h-full min-h-0">
  <!-- Header -->
  <div class="flex items-center justify-between px-4 py-3 border-b border-gray-200 dark:border-gray-800 shrink-0">
    <div class="flex items-center gap-3">
      <div class="flex items-center gap-2">
        <Activity size={18} class="text-ch-blue" />
        <h1 class="text-base font-semibold text-gray-800 dark:text-gray-200">Telemetry</h1>
      </div>

      {#if !loading && !needsSetup && !error}
        <nav class="flex items-center gap-1 ml-4">
          {#each tabs as tab}
            <button
              class="flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium rounded-md transition-colors
                {activeTab === tab.id
                  ? 'bg-ch-blue/10 text-ch-blue'
                  : tab.available
                    ? 'text-gray-500 hover:text-gray-700 dark:hover:text-gray-300 hover:bg-gray-100 dark:hover:bg-gray-800'
                    : 'text-gray-300 dark:text-gray-700 cursor-not-allowed'}"
              onclick={() => { if (tab.available) activeTab = tab.id }}
              disabled={!tab.available}
            >
              <tab.icon size={13} />
              {tab.label}
              {#if !tab.available && tab.id !== 'logs'}
                <span class="text-[8px] font-medium text-gray-400 bg-gray-100 dark:bg-gray-800 px-1 py-0.5 rounded leading-none">Soon</span>
              {/if}
            </button>
          {/each}
        </nav>
      {/if}
    </div>

    {#if tables.length > 0}
      <div class="flex items-center gap-2 text-[10px] text-gray-400">
        <span>{tables.length} table{tables.length !== 1 ? 's' : ''} detected</span>
      </div>
    {/if}
  </div>

  <!-- Content -->
  <div class="flex-1 min-h-0 overflow-hidden">
    {#if loading}
      <div class="flex flex-col items-center justify-center h-full gap-3">
        <Spinner size="sm" />
        <p class="text-sm text-gray-500">Detecting OpenTelemetry tables...</p>
      </div>
    {:else if error}
      <div class="flex flex-col items-center justify-center h-full gap-3">
        <p class="text-sm text-red-500">{error}</p>
        <button
          class="text-xs text-ch-blue hover:underline"
          onclick={detectSchema}
        >Retry</button>
      </div>
    {:else if needsSetup}
      <SetupWizard onretry={detectSchema} onconfigured={handleConfigured} />
    {:else if activeTab === 'logs'}
      <LogExplorer database={logsDatabase} table={logsTable} />
    {:else}
      <div class="flex flex-col items-center justify-center h-full gap-2">
        <p class="text-sm text-gray-500">Coming soon</p>
        <p class="text-xs text-gray-400">This feature is under development</p>
      </div>
    {/if}
  </div>
</div>
