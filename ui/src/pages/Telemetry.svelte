<script lang="ts">
  import { onMount } from 'svelte'
  import { fetchTelemetrySchema, getTelemetryConfig } from '../lib/api/telemetry'
  import type { TelemetryTable, TelemetryConfig } from '../lib/types/telemetry'
  import LogExplorer from '../lib/components/telemetry/LogExplorer.svelte'
  import SetupWizard from '../lib/components/telemetry/SetupWizard.svelte'
  import Spinner from '../lib/components/common/Spinner.svelte'
  import { Activity } from 'lucide-svelte'

  let loading = $state(true)
  let error = $state<string | null>(null)
  let tables = $state<TelemetryTable[]>([])
  let hasLogs = $state(false)
  let hasTraces = $state(false)
  let hasMetrics = $state(false)
  let logsDatabase = $state('default')
  let logsTable = $state('otel_logs')
  let needsSetup = $state(false)

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

  function handleConfigured(db: string, table: string) {
    logsDatabase = db
    logsTable = table
    needsSetup = false
    hasLogs = true
  }

  // A saved config takes priority over the otel_% auto-detect scan, so custom
  // table names survive a reload instead of resetting to default.otel_logs.
  async function loadSavedConfig(): Promise<boolean> {
    try {
      const res = await getTelemetryConfig()
      if (!res.config?.config_json) return false
      const cfg = JSON.parse(res.config.config_json) as Partial<TelemetryConfig>
      if (!cfg.logsDatabase || !cfg.logsTable) return false
      logsDatabase = cfg.logsDatabase
      logsTable = cfg.logsTable
      hasLogs = true
      return true
    } catch {
      return false
    }
  }

  onMount(async () => {
    if (await loadSavedConfig()) {
      loading = false
      return
    }
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
    {:else}
      <LogExplorer database={logsDatabase} table={logsTable} />
    {/if}
  </div>
</div>
