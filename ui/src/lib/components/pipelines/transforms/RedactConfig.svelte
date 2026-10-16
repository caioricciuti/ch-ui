<script lang="ts">
  import { DEFAULT_REPLACEMENT, REDACT_KINDS, readRedact, withCustomKind, type RedactConfig, type RedactKind } from '../../../types/transforms'
  import PairRows from './PairRows.svelte'
  import { hintClass, labelClass, monoInputClass } from './ui'

  interface Props {
    config: Record<string, unknown>
    onchange: (config: Record<string, unknown>) => void
  }

  let { config, onchange }: Props = $props()

  const c = $derived(readRedact(config))

  function set(patch: Partial<RedactConfig>) {
    onchange({ ...config, ...withCustomKind({ ...c, ...patch }) })
  }

  function toggleKind(kind: RedactKind, on: boolean) {
    const kinds = c.kinds.filter((k) => k !== kind)
    set({ kinds: on ? [...kinds, kind] : kinds })
  }
</script>

<div class="space-y-4">
  <div>
    <p class={labelClass}>Find and mask</p>
    <div class="space-y-1.5">
      {#each REDACT_KINDS as k (k.value)}
        <label class="ds-checkbox-label flex">
          <input type="checkbox" class="ds-checkbox" checked={c.kinds.includes(k.value)} onchange={(e) => toggleKind(k.value, e.currentTarget.checked)} />
          {k.label}
        </label>
      {/each}
    </div>
  </div>

  <div>
    <p class={labelClass}>Custom patterns</p>
    <PairRows
      rows={c.custom.map((p) => [p.name, p.pattern])}
      first="name"
      second="regex"
      addLabel="Add pattern"
      onchange={(rows) => set({ custom: rows.map(([name, pattern]) => ({ name, pattern })) })}
    />
    <p class={hintClass}>Go RE2 regex. The name fills {'{kind}'} in the replacement.</p>
  </div>

  <div>
    <p class={labelClass}>Fields</p>
    <PairRows rows={c.fields.map((f) => [f, ''])} first="field" addLabel="Limit to a field" onchange={(rows) => set({ fields: rows.map((r) => r[0]) })} />
    <p class={hintClass}>{c.fields.length ? 'Only these fields are scanned.' : 'Every string value is scanned, map values included.'}</p>
  </div>

  <div>
    <label for="redact-replacement" class={labelClass}>Replacement</label>
    <input id="redact-replacement" class={monoInputClass} value={c.replacement} placeholder={DEFAULT_REPLACEMENT} oninput={(e) => set({ replacement: e.currentTarget.value })} />
    <p class={hintClass}>{'{kind}'} becomes the kind found, e.g. [REDACTED:email].</p>
  </div>
</div>
