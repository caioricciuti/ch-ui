<script lang="ts">
  import type { InferredSchema, SchemaColumn } from '../../types/pipelines'
  import { createTableDDL } from '../../types/pipelines'
  import Button from '../common/Button.svelte'
  import { Sparkles, X } from 'lucide-svelte'

  interface Props {
    columns: SchemaColumn[]
    database: string
    table: string
    engine: string
    orderBy: string
    partitionBy: string
    /** Replaces the column list (empty: infer from the first record). */
    onColumns: (columns: SchemaColumn[]) => void
    /** Applies an inferred schema: columns plus suggested keys. */
    onApply: (schema: InferredSchema) => void
    /** Saves, samples the source and asks ClickHouse for types. */
    onInfer?: () => Promise<InferredSchema | null>
  }

  let { columns, database, table, engine, orderBy, partitionBy, onColumns, onApply, onInfer }: Props = $props()

  let inferring = $state(false)

  const ddl = $derived(columns.length ? createTableDDL({ database, table, columns, engine, orderBy, partitionBy }) : '')

  async function infer() {
    if (!onInfer || inferring) return
    inferring = true
    try {
      const schema = await onInfer()
      if (schema) onApply(schema)
    } finally {
      inferring = false
    }
  }

  function setType(index: number, type: string) {
    onColumns(columns.map((c, i) => (i === index ? { ...c, type } : c)))
  }

  function remove(index: number) {
    onColumns(columns.filter((_, i) => i !== index))
  }
</script>

<div class="space-y-2 border-t border-edge-subtle pt-3">
  <div class="flex items-center justify-between gap-2">
    <p class="text-[11px] font-medium uppercase tracking-wider text-fg-3">Columns</p>
    {#if onInfer}
      <Button size="sm" variant="outline" onclick={infer} loading={inferring} title="Save, read a sample from the source and let ClickHouse infer the types">
        <Sparkles size={12} /> Infer from source
      </Button>
    {/if}
  </div>

  {#if columns.length === 0}
    <p class="text-[12px] text-fg-3">
      No columns defined. The table is created from the first batch with basic types (String, Float64, UInt8).
      Infer from source to get real ClickHouse types.
    </p>
  {:else}
    <div class="space-y-1">
      {#each columns as col, i (col.name)}
        <div class="flex items-center gap-1">
          <span class="w-24 shrink-0 truncate font-mono text-[12px] text-fg-2" title={col.name}>{col.name}</span>
          <input
            type="text"
            value={col.type}
            aria-label="Type of {col.name}"
            oninput={(e) => setType(i, e.currentTarget.value)}
            class="min-w-0 flex-1 rounded-md border border-edge bg-surface px-2 py-1 font-mono text-[12px] text-fg hover:border-edge-strong focus:border-accent focus:outline-none"
          />
          <button
            class="shrink-0 rounded p-1 text-fg-4 hover:bg-hover hover:text-fg-2"
            title="Remove column"
            aria-label="Remove column {col.name}"
            onclick={() => remove(i)}
          >
            <X size={12} />
          </button>
        </div>
      {/each}
    </div>
    <div class="flex justify-end">
      <Button size="sm" variant="ghost" onclick={() => onColumns([])}>Clear columns</Button>
    </div>
    <div>
      <p class="mb-1 text-[11px] font-medium uppercase tracking-wider text-fg-3">DDL preview</p>
      <pre class="max-h-56 overflow-auto rounded-md border border-edge-subtle bg-surface px-2 py-1.5 font-mono text-[11px] leading-relaxed text-fg-2">{ddl}</pre>
    </div>
  {/if}
</div>
