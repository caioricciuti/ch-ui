<script lang="ts">
  import { saveTelemetryConfig } from '../../api/telemetry'
  import { success as toastSuccess, error as toastError } from '../../stores/toast.svelte'
  import Button from '../common/Button.svelte'
  import { Activity, RefreshCw, Database } from 'lucide-svelte'

  interface Props {
    onretry: () => void
    onconfigured: () => void
  }

  let { onretry, onconfigured }: Props = $props()

  let logsDatabase = $state('default')
  let logsTable = $state('otel_logs')
  let tracesDatabase = $state('default')
  let tracesTable = $state('otel_traces')
  let metricsDatabase = $state('default')
  let metricsPrefix = $state('otel_metrics')
  let saving = $state(false)

  async function saveConfig() {
    saving = true
    try {
      await saveTelemetryConfig({
        logsDatabase,
        logsTable,
        tracesDatabase,
        tracesTable,
        metricsDatabase,
        metricsPrefix,
      })
      toastSuccess('Telemetry config saved')
      onconfigured()
    } catch (e: unknown) {
      toastError('Failed to save config: ' + (e instanceof Error ? e.message : String(e)))
    } finally {
      saving = false
    }
  }
</script>

<div class="flex items-center justify-center h-full">
  <div class="max-w-md w-full">
    <div class="text-center mb-8">
      <div class="inline-flex items-center justify-center w-14 h-14 rounded-2xl bg-ch-blue/10 text-ch-blue mb-4">
        <Activity size={28} />
      </div>
      <h2 class="text-lg font-semibold text-gray-800 dark:text-gray-200 mb-2">Configure Telemetry</h2>
      <p class="text-sm text-gray-500">
        No OpenTelemetry tables were auto-detected. Enter your table names below, or click Retry to scan again.
      </p>
    </div>

    <div class="space-y-4 mb-6">
      <div class="rounded-lg border border-gray-200 dark:border-gray-800 p-4">
        <div class="flex items-center gap-2 mb-3">
          <Database size={14} class="text-gray-500" />
          <h3 class="text-sm font-medium text-gray-700 dark:text-gray-300">Logs</h3>
        </div>
        <div class="grid grid-cols-2 gap-3">
          <div>
            <label class="block text-[10px] font-medium text-gray-500 uppercase mb-1">Database</label>
            <input
              type="text"
              bind:value={logsDatabase}
              class="w-full text-xs px-2.5 py-1.5 rounded-md border border-gray-300 dark:border-gray-700 bg-white dark:bg-gray-900 text-gray-800 dark:text-gray-200 focus:ring-2 focus:ring-ch-blue/40 focus:border-ch-blue outline-none"
            />
          </div>
          <div>
            <label class="block text-[10px] font-medium text-gray-500 uppercase mb-1">Table</label>
            <input
              type="text"
              bind:value={logsTable}
              class="w-full text-xs px-2.5 py-1.5 rounded-md border border-gray-300 dark:border-gray-700 bg-white dark:bg-gray-900 text-gray-800 dark:text-gray-200 focus:ring-2 focus:ring-ch-blue/40 focus:border-ch-blue outline-none"
            />
          </div>
        </div>
      </div>

      <div class="rounded-lg border border-gray-200 dark:border-gray-800 p-4 opacity-50">
        <div class="flex items-center gap-2 mb-1">
          <Database size={14} class="text-gray-500" />
          <h3 class="text-sm font-medium text-gray-700 dark:text-gray-300">Traces & Metrics</h3>
          <span class="text-[9px] font-medium text-gray-400 bg-gray-100 dark:bg-gray-800 px-1.5 py-0.5 rounded">Coming soon</span>
        </div>
      </div>
    </div>

    <div class="flex items-center justify-center gap-3">
      <Button variant="secondary" size="sm" onclick={onretry}>
        <RefreshCw size={13} /> Retry Detection
      </Button>
      <Button size="sm" loading={saving} onclick={saveConfig}>
        Save & Continue
      </Button>
    </div>
  </div>
</div>
