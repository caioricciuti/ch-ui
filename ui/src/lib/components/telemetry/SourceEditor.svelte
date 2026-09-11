<script lang="ts">
  import { Check, X } from 'lucide-svelte'
  import Sheet from '../common/Sheet.svelte'
  import Button from '../common/Button.svelte'
  import Tabs from '../common/Tabs.svelte'
  import FormField from '../common/FormField.svelte'
  import Input from '../common/Input.svelte'
  import Select from '../common/Select.svelte'
  import Combobox from '../common/Combobox.svelte'
  import Badge from '../common/Badge.svelte'
  import HelpTip from '../common/HelpTip.svelte'
  import { listDatabases, listTables, listColumns } from '../../api/query'
  import type {
    TelemetrySource, SourceInput, SourceKind, LogsMapping, TracesMapping, MetricsTables,
  } from '../../types/telemetry'
  import { DEFAULT_LOGS_MAPPING, DEFAULT_TRACES_MAPPING, DEFAULT_METRICS_TABLES } from '../../types/telemetry'

  interface Props {
    open: boolean
    source: TelemetrySource | null
    others: TelemetrySource[]
    saving: boolean
    onsave: (input: SourceInput) => void
    onclose: () => void
  }
  let { open, source, others, saving, onsave, onclose }: Props = $props()

  let kind = $state<SourceKind>('logs')
  let name = $state('')
  let database = $state('default')
  let table = $state('')
  let logs = $state<LogsMapping>({ ...DEFAULT_LOGS_MAPPING })
  let traces = $state<TracesMapping>({ ...DEFAULT_TRACES_MAPPING })
  let tables = $state<MetricsTables>({ ...DEFAULT_METRICS_TABLES })
  let correlated = $state('')
  let enabled = $state(true)

  let databases = $state<string[]>([])
  let tableNames = $state<string[]>([])
  let columns = $state<Record<string, string>>({})
  let columnsFor = ''

  const kindTabs = [
    { id: 'logs', label: 'Logs' },
    { id: 'traces', label: 'Traces' },
    { id: 'metrics', label: 'Metrics' },
  ]

  // Reset the form each time the sheet opens.
  $effect(() => {
    if (!open) return
    const s = source
    kind = s?.kind ?? 'logs'
    name = s?.name ?? ''
    database = s?.database ?? 'default'
    table = s?.table ?? (kind === 'logs' ? 'otel_logs' : kind === 'traces' ? 'otel_traces' : '')
    logs = { ...DEFAULT_LOGS_MAPPING, ...(s?.logs ?? {}) }
    traces = { ...DEFAULT_TRACES_MAPPING, ...(s?.traces ?? {}) }
    tables = { ...DEFAULT_METRICS_TABLES, ...(s?.tables ?? {}) }
    correlated = s?.kind === 'logs' ? (s?.correlated_traces ?? '') : (s?.correlated_logs ?? '')
    enabled = s?.enabled ?? true
    columns = {}
    columnsFor = ''
    listDatabases().then((d) => (databases = d)).catch(() => (databases = []))
  })

  $effect(() => {
    if (!open || !database) return
    listTables(database).then((t) => (tableNames = t)).catch(() => (tableNames = []))
  })

  $effect(() => {
    if (!open || kind === 'metrics' || !database || !table) return
    const key = `${database}.${table}`
    if (columnsFor === key) return
    columnsFor = key
    listColumns(database, table)
      .then((cols) => (columns = Object.fromEntries(cols.map((c) => [c.name, c.type]))))
      .catch(() => (columns = {}))
  })

  function setKind(k: SourceKind) {
    kind = k
    if (!source) table = k === 'logs' ? 'otel_logs' : k === 'traces' ? 'otel_traces' : ''
    if (!name.trim() || name === 'Logs' || name === 'Traces' || name === 'Metrics') {
      name = k === 'logs' ? 'Logs' : k === 'traces' ? 'Traces' : 'Metrics'
    }
  }

  const logsFields: { key: keyof LogsMapping; label: string; required?: boolean }[] = [
    { key: 'timestamp', label: 'Timestamp column', required: true },
    { key: 'body', label: 'Log body', required: true },
    { key: 'severity', label: 'Severity text', required: true },
    { key: 'severity_number', label: 'Severity number' },
    { key: 'service', label: 'Service name', required: true },
    { key: 'trace_id', label: 'Trace id' },
    { key: 'span_id', label: 'Span id' },
    { key: 'log_attributes', label: 'Log attributes (Map)' },
    { key: 'resource_attributes', label: 'Resource attributes (Map)' },
    { key: 'scope_attributes', label: 'Scope attributes (Map)' },
    { key: 'scope_name', label: 'Scope name' },
    { key: 'event_name', label: 'Event name' },
  ]

  const tracesFields: { key: keyof TracesMapping; label: string; required?: boolean }[] = [
    { key: 'timestamp', label: 'Timestamp column', required: true },
    { key: 'trace_id', label: 'Trace id', required: true },
    { key: 'span_id', label: 'Span id', required: true },
    { key: 'parent_span_id', label: 'Parent span id', required: true },
    { key: 'span_name', label: 'Span name', required: true },
    { key: 'span_kind', label: 'Span kind' },
    { key: 'service', label: 'Service name', required: true },
    { key: 'duration', label: 'Duration', required: true },
    { key: 'status_code', label: 'Status code' },
    { key: 'status_message', label: 'Status message' },
    { key: 'span_attributes', label: 'Span attributes (Map)' },
    { key: 'resource_attributes', label: 'Resource attributes (Map)' },
    { key: 'events', label: 'Events (Nested prefix)' },
    { key: 'links', label: 'Links (Nested prefix)' },
  ]

  const metricsFields: { key: keyof MetricsTables; label: string }[] = [
    { key: 'gauge', label: 'Gauge table' },
    { key: 'sum', label: 'Sum table' },
    { key: 'histogram', label: 'Histogram table' },
    { key: 'exponential_histogram', label: 'Exponential histogram table' },
    { key: 'summary', label: 'Summary table' },
  ]

  const durationUnits = [
    { value: 'ns', label: 'nanoseconds (exporter default)' },
    { value: 'us', label: 'microseconds' },
    { value: 'ms', label: 'milliseconds' },
    { value: 's', label: 'seconds' },
  ]

  /** True when the mapped column is stored as a JSON type (exporter json mode). */
  function isJson(col: string): boolean {
    if (!col) return false
    const t = columns[col]
    return typeof t === 'string' && t.startsWith('JSON')
  }

  /** Nested columns appear as Events.Name etc; a prefix counts as present. */
  function present(col: string): boolean | null {
    if (!col) return null
    if (Object.keys(columns).length === 0) return null
    if (columns[col] !== undefined) return true
    return Object.keys(columns).some((c) => c.startsWith(col + '.'))
  }

  const correlationOptions = $derived([
    { value: '', label: 'None' },
    ...others
      .filter((o) => (kind === 'logs' ? o.kind === 'traces' : kind === 'traces' ? o.kind === 'logs' : false))
      // The name already carries (db.table) when the source was detected,
      // so appending it again read "Traces (default.otel_traces) (default.otel_traces)".
      .map((o) => ({ value: o.id, label: o.name })),
  ])

  const canSave = $derived(name.trim() !== '' && database.trim() !== '' && (kind === 'metrics' || table.trim() !== ''))

  function save() {
    const input: SourceInput = {
      kind, name: name.trim(), database: database.trim(), table: kind === 'metrics' ? '' : table.trim(), enabled,
      correlated_logs: kind === 'traces' ? correlated : '',
      correlated_traces: kind === 'logs' ? correlated : '',
    }
    if (kind === 'logs') input.logs = { ...logs }
    if (kind === 'traces') input.traces = { ...traces }
    if (kind === 'metrics') input.tables = { ...tables }
    onsave(input)
  }
</script>

<Sheet
  {open}
  title={source ? `Edit ${source.name}` : 'New source'}
  description="Which table holds the signal and which columns play which role. Defaults match the OpenTelemetry ClickHouse exporter."
  size="lg"
  {onclose}
>
  <div class="space-y-5">
    <Tabs variant="segmented" size="sm" items={kindTabs} value={kind} onchange={(id) => setKind(id as SourceKind)} />

    <div class="grid grid-cols-1 gap-3 md:grid-cols-2">
      <FormField label="Name" for="src-name" required controlWidth="full">
        <Input id="src-name" bind:value={name} placeholder={kind === 'logs' ? 'Logs' : kind === 'traces' ? 'Traces' : 'Metrics'} />
      </FormField>
      <FormField label="Database" required controlWidth="full">
        <Combobox options={databases.map((d) => ({ value: d, label: d }))} value={database} placeholder="default" onChange={(v) => { database = v; table = kind === 'metrics' ? '' : table }} />
      </FormField>
      {#if kind !== 'metrics'}
        <FormField label="Table" required controlWidth="full" class="md:col-span-2">
          <Combobox options={tableNames.map((t) => ({ value: t, label: t }))} value={table} placeholder={kind === 'logs' ? 'otel_logs' : 'otel_traces'} onChange={(v) => (table = v)} />
        </FormField>
      {/if}
    </div>

    <div>
      <h3 class="mb-2 text-[13px] font-semibold text-fg">Column mapping</h3>
      {#if kind === 'logs'}
        <div class="grid grid-cols-1 gap-3 md:grid-cols-2">
          {#each logsFields as f (f.key)}
            {@const ok = present(logs[f.key])}
            <FormField label={f.label} required={f.required} controlWidth="full">
              <div class="flex items-center gap-2">
                <Input size="sm" mono class="flex-1" bind:value={logs[f.key]} placeholder={f.required ? '' : 'optional'} />
                {#if isJson(logs[f.key])}
                  <span class="inline-flex shrink-0 items-center gap-1"><Badge tone="info">JSON</Badge><HelpTip text="Exporter json mode; attribute access uses JSON paths" /></span>
                {/if}
                {#if ok === true}<Check size={14} class="shrink-0 text-success" />{:else if ok === false}<X size={14} class="shrink-0 text-danger" />{/if}
              </div>
            </FormField>
          {/each}
        </div>
      {:else if kind === 'traces'}
        <div class="grid grid-cols-1 gap-3 md:grid-cols-2">
          {#each tracesFields as f (f.key)}
            {@const ok = present(traces[f.key])}
            <FormField label={f.label} required={f.required} controlWidth="full">
              <div class="flex items-center gap-2">
                <Input size="sm" mono class="flex-1" bind:value={traces[f.key]} placeholder={f.required ? '' : 'optional'} />
                {#if isJson(traces[f.key])}
                  <span class="inline-flex shrink-0 items-center gap-1"><Badge tone="info">JSON</Badge><HelpTip text="Exporter json mode; attribute access uses JSON paths" /></span>
                {/if}
                {#if ok === true}<Check size={14} class="shrink-0 text-success" />{:else if ok === false}<X size={14} class="shrink-0 text-danger" />{/if}
              </div>
            </FormField>
          {/each}
          <FormField label="Duration unit" controlWidth="full" hint="How the duration column is stored. Searches use milliseconds.">
            <Select size="sm" options={durationUnits} bind:value={traces.duration_unit} />
          </FormField>
        </div>
      {:else}
        <div class="grid grid-cols-1 gap-3 md:grid-cols-2">
          {#each metricsFields as f (f.key)}
            <FormField label={f.label} controlWidth="full">
              <Input size="sm" mono bind:value={tables[f.key]} placeholder="leave empty if absent" />
            </FormField>
          {/each}
        </div>
      {/if}
    </div>

    {#if kind !== 'metrics'}
      <FormField label={kind === 'logs' ? 'Correlated traces source' : 'Correlated logs source'} controlWidth="full" hint="Lets the UI jump between a log line and its trace.">
        <Select size="sm" options={correlationOptions} bind:value={correlated} />
      </FormField>
    {/if}

    <label class="ds-checkbox-label">
      <input type="checkbox" class="ds-checkbox" bind:checked={enabled} />
      Enabled
    </label>
  </div>

  {#snippet footer()}
    <Button variant="ghost" size="sm" onclick={onclose}>Cancel</Button>
    <Button size="sm" loading={saving} disabled={!canSave} onclick={save}>{source ? 'Save changes' : 'Create source'}</Button>
  {/snippet}
</Sheet>
