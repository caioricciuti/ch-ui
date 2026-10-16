<script lang="ts">
  import { readFilter } from '../../../types/transforms'
  import Tabs from '../../common/Tabs.svelte'
  import RulesEditor from './RulesEditor.svelte'
  import { hintClass, labelClass } from './ui'

  interface Props {
    config: Record<string, unknown>
    signal?: string | null
    onchange: (config: Record<string, unknown>) => void
  }

  let { config, signal = null, onchange }: Props = $props()

  const c = $derived(readFilter(config))
</script>

<div class="space-y-3">
  <div>
    <p class={labelClass}>Matching records are</p>
    <Tabs
      variant="segmented"
      items={[{ id: 'keep', label: 'Kept' }, { id: 'drop', label: 'Dropped' }]}
      value={c.mode}
      onchange={(id) => onchange({ ...config, ...c, mode: id === 'drop' ? 'drop' : 'keep' })}
    />
    <p class={hintClass}>{c.mode === 'keep' ? 'Only records that match pass; the rest are dropped.' : 'Records that match are dropped; the rest pass.'}</p>
  </div>

  <div>
    <p class={labelClass}>Rules</p>
    <RulesEditor
      rules={c.rules}
      match={c.match}
      fieldHint={signal === 'logs' ? 'SeverityText or LogAttributes.k' : 'field'}
      onchange={(rules, match) => onchange({ ...config, ...c, rules, match })}
    />
    <p class={hintClass}>Comparisons are numeric when both sides are numbers. "in list" takes a JSON array.</p>
  </div>
</div>
