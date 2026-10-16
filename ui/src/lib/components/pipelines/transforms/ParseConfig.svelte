<script lang="ts">
  import { PARSE_FORMATS, REGEX_EXAMPLE, readParse, regexGroupNames, type ParseConfig, type ParseFormat } from '../../../types/transforms'
  import { hintClass, inputClass, labelClass, monoInputClass, smallButtonClass } from './ui'

  interface Props {
    config: Record<string, unknown>
    /** Signal of the ingest source, if any: OTel logs suggest Body and LogAttributes. */
    signal?: string | null
    onchange: (config: Record<string, unknown>) => void
  }

  let { config, signal = null, onchange }: Props = $props()

  const c = $derived(readParse(config))
  const groups = $derived(c.format === 'regex' ? regexGroupNames(c.pattern) : [])
  const formatHint = $derived(PARSE_FORMATS.find((f) => f.value === c.format)?.hint ?? '')

  function set(patch: Partial<ParseConfig>) {
    onchange({ ...config, ...c, ...patch })
  }

  function setFormat(format: ParseFormat) {
    set(format === 'regex' && !c.pattern ? { format, pattern: REGEX_EXAMPLE } : { format })
  }
</script>

<div class="space-y-3">
  <div>
    <label for="parse-format" class={labelClass}>Format</label>
    <select id="parse-format" class={inputClass} value={c.format} onchange={(e) => setFormat(e.currentTarget.value as ParseFormat)}>
      {#each PARSE_FORMATS as f (f.value)}
        <option value={f.value}>{f.label}</option>
      {/each}
    </select>
    <p class={hintClass}>{formatHint}</p>
  </div>

  {#if c.format === 'regex'}
    <div>
      <label for="parse-pattern" class={labelClass}>Pattern</label>
      <textarea
        id="parse-pattern"
        rows={3}
        class="{monoInputClass} resize-y"
        value={c.pattern}
        placeholder={'(?P<level>\\w+) (?P<msg>.*)'}
        oninput={(e) => set({ pattern: e.currentTarget.value })}
      ></textarea>
      <p class={hintClass}>
        Each named group <code class="font-mono text-fg-3">(?P&lt;name&gt;...)</code> becomes a field. Go RE2: no lookarounds or backreferences.
      </p>
      {#if groups.length}
        <p class="mt-1 text-[11px] text-fg-3">Fields: <span class="font-mono text-fg-2">{groups.join(', ')}</span></p>
      {/if}
      {#if c.pattern !== REGEX_EXAMPLE}
        <button class="{smallButtonClass} mt-1.5" onclick={() => set({ pattern: REGEX_EXAMPLE })}>Use access log example</button>
      {/if}
    </div>
  {/if}

  <div>
    <label for="parse-field" class={labelClass}>Field</label>
    <input id="parse-field" class={monoInputClass} value={c.field} placeholder="Body" oninput={(e) => set({ field: e.currentTarget.value })} />
    <p class={hintClass}>The field holding the text to parse.</p>
  </div>

  <div>
    <label for="parse-target" class={labelClass}>Target</label>
    <input id="parse-target" class={monoInputClass} value={c.target} placeholder="record root" list="parse-target-options" oninput={(e) => set({ target: e.currentTarget.value })} />
    <datalist id="parse-target-options">
      <option value="LogAttributes"></option>
      <option value="ResourceAttributes"></option>
    </datalist>
    <p class={hintClass}>Where parsed fields go. Empty merges them into the record root.{signal === 'logs' ? ' OTel logs usually use LogAttributes.' : ''}</p>
  </div>

  <div>
    <label for="parse-prefix" class={labelClass}>Prefix</label>
    <input id="parse-prefix" class={monoInputClass} value={c.prefix} placeholder="none" oninput={(e) => set({ prefix: e.currentTarget.value })} />
    <p class={hintClass}>Optional, added to every parsed field name, e.g. http_.</p>
  </div>

  <div>
    <label for="parse-on-error" class={labelClass}>When a record does not parse</label>
    <select id="parse-on-error" class={inputClass} value={c.on_error} onchange={(e) => set({ on_error: e.currentTarget.value === 'drop' ? 'drop' : 'keep' })}>
      <option value="keep">Keep it unchanged</option>
      <option value="drop">Drop it</option>
    </select>
    <p class={hintClass}>Either way it counts as a parse error.</p>
  </div>

  <label class="ds-checkbox-label">
    <input type="checkbox" class="ds-checkbox" checked={c.remove_source} onchange={(e) => set({ remove_source: e.currentTarget.checked })} />
    Remove the source field after parsing
  </label>
</div>
