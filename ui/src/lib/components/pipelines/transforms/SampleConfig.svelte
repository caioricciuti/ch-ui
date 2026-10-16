<script lang="ts">
  import { readSample } from '../../../types/transforms'
  import RulesEditor from './RulesEditor.svelte'
  import { hintClass, inputClass, labelClass, monoInputClass } from './ui'

  interface Props {
    config: Record<string, unknown>
    signal?: string | null
    onchange: (config: Record<string, unknown>) => void
  }

  let { config, signal = null, onchange }: Props = $props()

  const c = $derived(readSample(config))
  let percentError = $state('')

  function setPercent(raw: string) {
    const n = Number(raw.trim())
    if (raw.trim() === '' || !Number.isFinite(n) || n < 0 || n > 100) {
      percentError = 'Enter a number from 0 to 100'
      return
    }
    percentError = ''
    onchange({ ...config, ...c, keep_percent: n })
  }
</script>

<div class="space-y-3">
  <div>
    <label for="sample-percent" class={labelClass}>Keep percent</label>
    <input
      id="sample-percent"
      type="number"
      min={0}
      max={100}
      step="any"
      class="{inputClass} {percentError ? 'border-danger' : ''}"
      value={c.keep_percent}
      aria-invalid={percentError ? 'true' : undefined}
      oninput={(e) => setPercent(e.currentTarget.value)}
    />
    {#if percentError}
      <p class="mt-1 text-[11px] text-danger">{percentError}; keeping {c.keep_percent}</p>
    {:else}
      <p class={hintClass}>Share of records that pass, decided by a hash, so a replay keeps the same records.</p>
    {/if}
  </div>

  <div>
    <label for="sample-key" class={labelClass}>Key</label>
    <input id="sample-key" class={monoInputClass} value={c.key} placeholder="whole record" oninput={(e) => onchange({ ...config, ...c, key: e.currentTarget.value })} />
    <p class={hintClass}>Records with the same key value are kept or dropped together. TraceId keeps whole traces. Empty hashes the whole record.</p>
  </div>

  <div>
    <p class={labelClass}>Always keep</p>
    <RulesEditor
      rules={c.always_keep.rules}
      match={c.always_keep.match}
      fieldHint={signal === 'logs' ? 'SeverityText or LogAttributes.k' : 'field'}
      onchange={(rules, match) => onchange({ ...config, ...c, always_keep: { rules, match } })}
    />
    <p class={hintClass}>Records matching these rules always pass, e.g. errors.</p>
  </div>
</div>
