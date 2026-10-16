<script lang="ts">
  import type { InferredSchema, PipelineTestResult, SinkCheck } from '../../types/pipelines'
  import Sheet from '../common/Sheet.svelte'
  import Button from '../common/Button.svelte'
  import Badge from '../common/Badge.svelte'
  import Spinner from '../common/Spinner.svelte'
  import EmptyState from '../common/EmptyState.svelte'
  import DataTable, { type DataColumn } from '../common/DataTable.svelte'
  import { AlertTriangle, FlaskConical, RefreshCw } from 'lucide-svelte'

  interface Props {
    open: boolean
    loading: boolean
    result: PipelineTestResult | null
    error: string
    onclose: () => void
    onretest: () => void
    /** Uses the inferred columns and keys for the sink's auto-created table. */
    onapplyschema: (schema: InferredSchema) => void
  }

  let { open, loading, result, error, onclose, onretest, onapplyschema }: Props = $props()

  /** Sample columns shown; the full record is on hover of each cell. */
  const MAX_COLUMNS = 16

  type SampleRow = Record<string, unknown> & { __row: number }
  type CheckRow = SinkCheck & Record<string, unknown>
  type ColumnRow = { name: string; type: string }

  const sampleKeys = $derived.by(() => {
    const keys: string[] = []
    for (const rec of result?.records ?? []) {
      for (const k of Object.keys(rec)) {
        if (!keys.includes(k)) keys.push(k)
        if (keys.length >= MAX_COLUMNS) return keys
      }
    }
    return keys
  })

  const sampleColumns = $derived<DataColumn<SampleRow>[]>(
    sampleKeys.map((k) => ({ key: k, label: k, mono: true, truncate: true, format: (v: unknown) => cellText(v) })),
  )
  const sampleRows = $derived<SampleRow[]>((result?.records ?? []).map((r, i) => ({ ...r, __row: i })))
  const checkRows = $derived<CheckRow[]>((result?.sink_checks ?? []).map((c) => ({ ...c })))
  const schemaRows = $derived<ColumnRow[]>((result?.schema?.columns ?? []).map((c) => ({ name: c.name, type: c.type })))

  const checkColumns: DataColumn<CheckRow>[] = [
    { key: 'status', label: 'Result', width: '90px', sortable: false },
    { key: 'name', label: 'Check', width: '150px', sortable: false },
    { key: 'detail', label: 'Detail', sortable: false },
  ]
  const schemaColumns: DataColumn<ColumnRow>[] = [
    { key: 'name', label: 'Column', mono: true },
    { key: 'type', label: 'Inferred type', mono: true },
  ]

  function cellText(v: unknown): string {
    if (v === null || v === undefined) return 'NULL'
    if (typeof v === 'object') return JSON.stringify(v)
    return String(v)
  }

  function tone(status: SinkCheck['status']): 'success' | 'warning' | 'danger' {
    return status === 'ok' ? 'success' : status === 'warn' ? 'warning' : 'danger'
  }
</script>

<Sheet {open} {onclose} size="xl" title="Test pipeline" description="Reads a sample from the saved source and checks the sink. Nothing is written and the source position does not move.">
  {#if loading}
    <div class="flex flex-col items-center gap-2 py-16 text-[13px] text-fg-3">
      <Spinner />
      Reading a sample (up to 10 seconds)
    </div>
  {:else if error}
    <EmptyState size="compact" icon={AlertTriangle} title="Test failed" description={error} />
  {:else if result}
    <div class="space-y-6">
      <section class="space-y-2">
        <div class="flex items-baseline justify-between gap-3">
          <h3 class="text-[13px] font-semibold text-fg">Sample records</h3>
          <span class="text-[12px] text-fg-3 tabular-nums">{result.records.length} of up to 20</span>
        </div>
        {#if result.source_error}
          <div class="flex items-start gap-2 rounded-md bg-danger-soft px-3 py-2 text-[13px] text-danger">
            <AlertTriangle size={14} class="mt-0.5 shrink-0" />
            <span class="min-w-0 break-words">{result.source_error}</span>
          </div>
        {:else}
          {#if result.note}
            <p class="text-[13px] text-fg-3">{result.note}</p>
          {/if}
          {#if result.records.length > 0}
            <DataTable columns={sampleColumns} rows={sampleRows} rowKey={(r) => r.__row} emptyTitle="No records" />
            {#if sampleKeys.length >= MAX_COLUMNS}
              <p class="text-[12px] text-fg-4">Showing the first {MAX_COLUMNS} fields.</p>
            {/if}
          {/if}
        {/if}
      </section>

      <section class="space-y-2">
        <h3 class="text-[13px] font-semibold text-fg">Sink checks</h3>
        <DataTable columns={checkColumns} rows={checkRows} rowKey={(r) => r.name} emptyTitle="No checks ran" emptyDescription="This sink cannot be checked.">
          {#snippet cell(row, col, value)}
            {#if col.key === 'status'}
              <Badge tone={tone(row.status)}>{value}</Badge>
            {:else if col.key === 'detail'}
              <span class="break-words text-[13px] text-fg-2">{value}</span>
            {:else}{value}{/if}
          {/snippet}
        </DataTable>
      </section>

      {#if result.schema || result.schema_error}
        <section class="space-y-2">
          <div class="flex items-baseline justify-between gap-3">
            <h3 class="text-[13px] font-semibold text-fg">Inferred schema</h3>
            {#if result.schema}
              <span class="text-[12px] text-fg-3">From DESC format(JSONEachRow, sample), run read-only as you</span>
            {/if}
          </div>
          {#if result.schema_error}
            <p class="text-[13px] text-warning">ClickHouse could not infer a schema: {result.schema_error}</p>
          {:else if result.schema}
            <DataTable columns={schemaColumns} rows={schemaRows} rowKey={(r) => r.name} />
            <p class="text-[13px] text-fg-3">
              Suggested ORDER BY <code class="font-mono text-fg-2">{result.schema.order_by}</code>{#if result.schema.partition_by}, PARTITION BY <code class="font-mono text-fg-2">{result.schema.partition_by}</code>{/if}.
            </p>
          {/if}
        </section>
      {/if}
    </div>
  {:else}
    <EmptyState size="compact" icon={FlaskConical} title="No test yet" description="Run a test to see sample records and sink checks." />
  {/if}

  {#snippet footer()}
    {#if result?.schema}
      <Button size="sm" variant="outline" onclick={() => result?.schema && onapplyschema(result.schema)} title="Turns on Create Table If Not Exists with these columns and keys; edit them in the sink panel">
        Create table with these columns
      </Button>
    {/if}
    <Button size="sm" variant="outline" onclick={onretest} disabled={loading}>
      <RefreshCw size={13} /> Test again
    </Button>
    <Button size="sm" onclick={onclose}>Close</Button>
  {/snippet}
</Sheet>
