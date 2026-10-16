<script lang="ts">
  import { TIMESTAMP_FORMATS, readMap, type MapConfig } from '../../../types/transforms'
  import PairRows from './PairRows.svelte'
  import { hintClass, inputClass, labelClass, monoInputClass } from './ui'

  interface Props {
    config: Record<string, unknown>
    signal?: string | null
    onchange: (config: Record<string, unknown>) => void
  }

  let { config, signal = null, onchange }: Props = $props()

  const c = $derived(readMap(config))
  const layoutMode = $derived(c.timestamp_format.startsWith('layout:'))

  function set(patch: Partial<MapConfig>) {
    onchange({ ...config, ...c, ...patch })
  }

  const toPairs = (rows: { from: string; to: string }[]): [string, string][] => rows.map((r) => [r.from, r.to])
  const fromPairs = (rows: [string, string][]) => rows.map(([from, to]) => ({ from, to }))
</script>

<div class="space-y-4">
  <p class="text-[12px] text-fg-3">Applied in this order: copy, rename, set, severity, timestamp, drop.</p>

  <div>
    <p class={labelClass}>Copy</p>
    <PairRows rows={toPairs(c.copy)} first="from" second="to" joiner="to" addLabel="Add copy" onchange={(rows) => set({ copy: fromPairs(rows) })} />
  </div>

  <div>
    <p class={labelClass}>Rename</p>
    <PairRows rows={toPairs(c.rename)} first="from" second="to" joiner="to" addLabel="Add rename" onchange={(rows) => set({ rename: fromPairs(rows) })} />
  </div>

  <div>
    <p class={labelClass}>Set</p>
    <PairRows
      rows={c.set.map((r) => [r.field, r.value])}
      first="field"
      second="value"
      joiner="="
      addLabel="Add field"
      onchange={(rows) => set({ set: rows.map(([field, value]) => ({ field, value })) })}
    />
    <p class={hintClass}>Values are fixed text, the same for every record.</p>
  </div>

  <div>
    <label for="map-severity" class={labelClass}>Severity from</label>
    <input id="map-severity" class={monoInputClass} value={c.severity_from} placeholder="level" oninput={(e) => set({ severity_from: e.currentTarget.value })} />
    <p class={hintClass}>
      Maps level words (debug, info, warn, error, fatal and their variants) to
      {signal === 'logs' ? 'SeverityText (TRACE, DEBUG, INFO, WARN, ERROR, FATAL) and SeverityNumber.' : 'a severity field.'}
    </p>
  </div>

  <div>
    <label for="map-ts" class={labelClass}>Timestamp from</label>
    <input id="map-ts" class={monoInputClass} value={c.timestamp_from} placeholder="ts" oninput={(e) => set({ timestamp_from: e.currentTarget.value })} />
    <p class={hintClass}>Parsed and written to {signal === 'logs' || signal === 'traces' ? 'Timestamp' : 'timestamp'}.</p>
    {#if c.timestamp_from}
      <select
        class="{inputClass} mt-1.5"
        aria-label="Timestamp format"
        value={layoutMode ? 'layout' : c.timestamp_format}
        onchange={(e) => set({ timestamp_format: e.currentTarget.value === 'layout' ? 'layout:2006-01-02 15:04:05' : e.currentTarget.value })}
      >
        {#each TIMESTAMP_FORMATS as f (f.value)}
          <option value={f.value}>{f.label}</option>
        {/each}
      </select>
      {#if layoutMode}
        <input
          class="{monoInputClass} mt-1.5"
          aria-label="Go time layout"
          value={c.timestamp_format.slice('layout:'.length)}
          placeholder="2006-01-02 15:04:05"
          oninput={(e) => set({ timestamp_format: `layout:${e.currentTarget.value}` })}
        />
        <p class={hintClass}>Go reference time: Mon Jan 2 15:04:05 MST 2006.</p>
      {/if}
    {/if}
  </div>

  <div>
    <p class={labelClass}>Drop</p>
    <PairRows rows={c.drop.map((d) => [d, ''])} first="field" addLabel="Add field to drop" onchange={(rows) => set({ drop: rows.map((r) => r[0]) })} />
  </div>
</div>
