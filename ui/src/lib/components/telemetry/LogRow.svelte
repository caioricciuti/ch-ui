<script lang="ts">
  import { SEVERITY_COLORS, type SeverityLevel } from '../../types/telemetry'
  import type { LogEntry } from '../../types/telemetry'
  import { ChevronRight, ChevronDown } from 'lucide-svelte'

  interface Props {
    entry: LogEntry
  }

  let { entry }: Props = $props()

  let expanded = $state(false)

  const severityColor = $derived(SEVERITY_COLORS[entry.SeverityText as SeverityLevel] ?? '#6b7280')

  function formatTimestamp(ts: string): string {
    try {
      const d = new Date(ts)
      return d.toLocaleTimeString('en-US', { hour12: false, hour: '2-digit', minute: '2-digit', second: '2-digit', fractionalSecondDigits: 3 })
    } catch {
      return ts
    }
  }

  function formatDate(ts: string): string {
    try {
      const d = new Date(ts)
      return d.toLocaleDateString('en-US', { month: 'short', day: 'numeric' })
    } catch {
      return ''
    }
  }

  function parseAttributes(val: unknown): Record<string, string> {
    if (!val) return {}
    if (typeof val === 'object') return val as Record<string, string>
    if (typeof val === 'string') {
      try { return JSON.parse(val) } catch { return {} }
    }
    return {}
  }

  const resourceAttrs = $derived(parseAttributes(entry.ResourceAttributes))
  const logAttrs = $derived(parseAttributes(entry.LogAttributes))
  const hasAttrs = $derived(Object.keys(resourceAttrs).length > 0 || Object.keys(logAttrs).length > 0)
</script>

<button
  class="w-full text-left border-b border-gray-100 dark:border-gray-800/50 hover:bg-gray-50 dark:hover:bg-gray-800/30 transition-colors"
  onclick={() => expanded = !expanded}
>
  <div class="flex items-center gap-3 px-3 py-1.5 min-w-0">
    <span class="text-gray-400 shrink-0 w-3">
      {#if hasAttrs || entry.TraceId}
        {#if expanded}
          <ChevronDown size={12} />
        {:else}
          <ChevronRight size={12} />
        {/if}
      {/if}
    </span>

    <span class="text-[11px] font-mono text-gray-400 dark:text-gray-500 shrink-0 tabular-nums whitespace-nowrap">
      <span class="text-gray-300 dark:text-gray-600">{formatDate(entry.Timestamp)}</span>
      {formatTimestamp(entry.Timestamp)}
    </span>

    <span
      class="text-[10px] font-bold px-1.5 py-0.5 rounded shrink-0 leading-none"
      style="color: {severityColor}; background: {severityColor}18"
    >
      {entry.SeverityText || 'INFO'}
    </span>

    {#if entry.ServiceName}
      <span class="text-[10px] font-medium text-purple-500 dark:text-purple-400 bg-purple-50 dark:bg-purple-900/20 px-1.5 py-0.5 rounded shrink-0 leading-none">
        {entry.ServiceName}
      </span>
    {/if}

    <span class="text-xs font-mono text-gray-700 dark:text-gray-300 truncate min-w-0">
      {entry.Body}
    </span>
  </div>
</button>

{#if expanded}
  <div class="bg-gray-50 dark:bg-gray-900/60 border-b border-gray-200 dark:border-gray-800 px-6 py-3">
    {#if entry.Body}
      <div class="mb-3">
        <p class="text-[10px] font-semibold text-gray-500 uppercase tracking-wider mb-1">Message</p>
        <pre class="text-xs font-mono text-gray-800 dark:text-gray-200 whitespace-pre-wrap break-all bg-white dark:bg-gray-950 border border-gray-200 dark:border-gray-800 rounded-md p-2">{entry.Body}</pre>
      </div>
    {/if}

    {#if entry.TraceId}
      <div class="flex items-center gap-4 mb-3 text-xs">
        <span class="text-gray-500">TraceId:</span>
        <span class="font-mono text-ch-blue">{entry.TraceId}</span>
        {#if entry.SpanId}
          <span class="text-gray-500">SpanId:</span>
          <span class="font-mono text-gray-600 dark:text-gray-400">{entry.SpanId}</span>
        {/if}
      </div>
    {/if}

    {#if Object.keys(resourceAttrs).length > 0}
      <div class="mb-3">
        <p class="text-[10px] font-semibold text-gray-500 uppercase tracking-wider mb-1">Resource Attributes</p>
        <div class="grid grid-cols-[auto_1fr] gap-x-4 gap-y-0.5 text-xs">
          {#each Object.entries(resourceAttrs) as [key, value]}
            <span class="font-mono text-gray-500 truncate">{key}</span>
            <span class="font-mono text-gray-800 dark:text-gray-200 truncate">{value}</span>
          {/each}
        </div>
      </div>
    {/if}

    {#if Object.keys(logAttrs).length > 0}
      <div>
        <p class="text-[10px] font-semibold text-gray-500 uppercase tracking-wider mb-1">Log Attributes</p>
        <div class="grid grid-cols-[auto_1fr] gap-x-4 gap-y-0.5 text-xs">
          {#each Object.entries(logAttrs) as [key, value]}
            <span class="font-mono text-gray-500 truncate">{key}</span>
            <span class="font-mono text-gray-800 dark:text-gray-200 truncate">{value}</span>
          {/each}
        </div>
      </div>
    {/if}
  </div>
{/if}
