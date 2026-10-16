<script lang="ts">
  import { SINK_PRESETS, WIDE_ROLES, wideEventsDDL, type SinkPreset } from '../../types/pipelines'
  import { usesOtelLayout } from '../../types/ingest'

  interface Props {
    config: Record<string, unknown>
    preset: SinkPreset
    /** Signal of the pipeline's ingest source, if any. */
    sourceSignal?: string | null
    onChange: (key: string, value: unknown) => void
  }

  let { config, preset, sourceSignal = null, onChange }: Props = $props()

  let ttlError = $state('')

  const hint = $derived(SINK_PRESETS.find((p) => p.value === preset)?.hint ?? '')
  const otel = $derived(preset === 'otel_logs' || preset === 'otel_traces')
  // An ingest logs or traces source sets the TTL of the OpenTelemetry tables.
  const sourceTTL = $derived(otel && usesOtelLayout(sourceSignal))
  const createTable = $derived(!!config['create_table'])
  const mapping = $derived.by(() => {
    const m = config['mapping']
    return m && typeof m === 'object' && !Array.isArray(m) ? (m as Record<string, unknown>) : {}
  })
  const ttlDays = $derived(typeof config['ttl_days'] === 'number' ? (config['ttl_days'] as number) : 0)
  const ddl = $derived(
    preset === 'wide_events'
      ? wideEventsDDL({
          database: String(config['database'] ?? 'default'),
          table: String(config['table'] ?? ''),
          engine: String(config['create_table_engine'] ?? 'MergeTree'),
          keepRaw: !!config['keep_raw'],
          ttlDays,
        })
      : '',
  )

  function mappingText(role: string): string {
    const v = mapping[role]
    if (Array.isArray(v)) return v.join(', ')
    return typeof v === 'string' ? v : ''
  }

  function setMapping(role: string, value: string) {
    const next: Record<string, unknown> = { ...mapping }
    if (value.trim()) next[role] = value
    else delete next[role]
    onChange('mapping', Object.keys(next).length ? next : null)
  }

  function setTTL(raw: string) {
    const n = Number(raw.trim())
    if (raw.trim() === '' || !Number.isInteger(n) || n < 0 || n > 36500) {
      ttlError = 'Enter a whole number from 0 to 36500'
      return
    }
    ttlError = ''
    onChange('ttl_days', n)
  }

  const labelClass = 'block text-[11px] font-medium text-fg-3 uppercase tracking-wider mb-1'
  const inputClass = 'w-full rounded-md border border-edge bg-surface px-2.5 py-1.5 text-[13px] text-fg hover:border-edge-strong focus:border-accent focus:outline-none'
</script>

{#snippet toggle(key: string, label: string, help: string)}
  <div>
    <p class={labelClass}>{label}</p>
    <div class="flex items-start gap-2">
      <button
        aria-label={label}
        aria-pressed={!!config[key]}
        class="relative mt-0.5 inline-flex h-5 w-9 shrink-0 items-center rounded-full transition-colors {config[key] ? 'bg-accent' : 'bg-edge'}"
        onclick={() => onChange(key, !config[key])}
      >
        <span class="inline-block h-3.5 w-3.5 transform rounded-full bg-surface shadow-sm transition-transform {config[key] ? 'translate-x-4' : 'translate-x-0.5'}"></span>
      </button>
      <p class="text-[11px] text-fg-4">{help}</p>
    </div>
  </div>
{/snippet}

<div class="space-y-3 border-t border-edge-subtle pt-3">
  <div>
    <label for="sink-table-layout" class={labelClass}>Table layout</label>
    <select id="sink-table-layout" value={preset} onchange={(e) => onChange('preset', e.currentTarget.value)} class={inputClass}>
      {#each SINK_PRESETS as p (p.value)}
        <option value={p.value}>{p.label}</option>
      {/each}
    </select>
    <p class="mt-1 text-[11px] text-fg-4">{hint}</p>
  </div>

  {#if preset === 'wide_events'}
    <div class="space-y-1">
      <p class={labelClass}>Field mapping</p>
      {#each WIDE_ROLES as r (r.role)}
        <div class="flex items-center gap-1">
          <label for="sink-map-{r.role}" class="w-20 shrink-0 text-[12px] text-fg-3">{r.label}</label>
          <input
            id="sink-map-{r.role}"
            type="text"
            value={mappingText(r.role)}
            placeholder={r.defaults.join(', ')}
            oninput={(e) => setMapping(r.role, e.currentTarget.value)}
            class="min-w-0 flex-1 rounded-md border border-edge bg-surface px-2 py-1 font-mono text-[12px] text-fg hover:border-edge-strong focus:border-accent focus:outline-none"
          />
        </div>
      {/each}
      <p class="text-[11px] text-fg-4">Field names to look for, comma-separated, first match wins. Dotted names also match nested fields. Empty uses the names shown. Every other field goes into attributes.</p>
    </div>
    {@render toggle('keep_raw', 'Keep raw record', 'Also store each original record in a raw column, compressed with ZSTD.')}
  {/if}

  {#if preset === 'wide_events' || (otel && !sourceTTL)}
    <div>
      <label for="sink-ttl-days" class={labelClass}>TTL (days)</label>
      <input
        id="sink-ttl-days"
        type="number"
        min="0"
        step="1"
        value={ttlDays}
        oninput={(e) => setTTL(e.currentTarget.value)}
        aria-invalid={ttlError ? 'true' : undefined}
        class="{inputClass} {ttlError ? 'border-danger' : ''}"
      />
      {#if ttlError}
        <p class="mt-1 text-[11px] text-danger">{ttlError}; keeping {ttlDays}</p>
      {:else}
        <p class="mt-1 text-[11px] text-fg-4">Applied when the sink creates the table. 0 keeps data forever.</p>
      {/if}
    </div>
  {/if}

  {#if preset === 'custom' || preset === 'wide_events'}
    {@render toggle(
      'add_new_columns',
      'Add columns for new fields',
      preset === 'custom'
        ? 'A field the table does not have yet gets a column (ALTER TABLE ADD COLUMN), typed from the batch. Existing columns are never changed. Off: unknown fields are dropped.'
        : 'Adds layout columns the table lacks, such as raw after Keep raw record is turned on. New fields always go into attributes.',
    )}
  {/if}

  {#if otel}
    <p class="rounded-md bg-info-soft px-2.5 py-2 text-[12px] text-info">
      The table is created in the OpenTelemetry {preset} layout that Telemetry reads{sourceTTL ? ", with the source's TTL" : ''}. Engine, ORDER BY and PARTITION BY are set by the layout.
    </p>
  {:else if preset === 'wide_events' && createTable}
    <div>
      <p class="mb-1 text-[11px] font-medium uppercase tracking-wider text-fg-3">DDL preview</p>
      <pre class="max-h-56 overflow-auto rounded-md border border-edge-subtle bg-surface px-2 py-1.5 font-mono text-[11px] leading-relaxed text-fg-2">{ddl}</pre>
      <p class="mt-1 text-[11px] text-fg-4">On ClickHouse before 25.3, attributes is Map(LowCardinality(String), String) with nested fields flattened to dotted keys.</p>
    </div>
  {/if}
</div>
