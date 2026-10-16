<script lang="ts">
  import { Plus, X } from 'lucide-svelte'
  import { RULE_OPS, opNeedsValue, type MatchMode, type Rule, type RuleOp } from '../../../types/transforms'
  import Tabs from '../../common/Tabs.svelte'
  import { iconButtonClass, inputClass, monoInputClass, smallButtonClass } from './ui'

  interface Props {
    rules: Rule[]
    match: MatchMode
    onchange: (rules: Rule[], match: MatchMode) => void
    /** Placeholder for a new rule's field. */
    fieldHint?: string
  }

  let { rules, match, onchange, fieldHint = 'SeverityText' }: Props = $props()

  function update(i: number, patch: Partial<Rule>) {
    onchange(rules.map((r, j) => (j === i ? { ...r, ...patch } : r)), match)
  }

  function setOp(i: number, op: RuleOp) {
    const r = rules[i]
    // Switching to "in list" turns a plain value into a one-item list.
    let value = opNeedsValue(op) ? r.value : ''
    if (op === 'in' && value && !value.trim().startsWith('[')) value = JSON.stringify([value])
    update(i, { op, value })
  }

  function valuePlaceholder(op: RuleOp): string {
    if (op === 'in') return '["a","b"]'
    if (op === 'matches') return 'timeout|refused'
    if (op === 'gt' || op === 'gte' || op === 'lt' || op === 'lte') return '500'
    return 'value'
  }
</script>

<div class="space-y-2">
  {#if rules.length > 1}
    <div class="flex items-center gap-2 text-[12px] text-fg-3">
      Match
      <Tabs
        variant="segmented"
        size="sm"
        items={[{ id: 'all', label: 'all rules' }, { id: 'any', label: 'any rule' }]}
        value={match}
        onchange={(id) => onchange(rules, id === 'any' ? 'any' : 'all')}
      />
    </div>
  {/if}

  {#each rules as rule, i (i)}
    <div class="space-y-1 rounded-md border border-edge-subtle p-2">
      <div class="flex items-center gap-1">
        <input
          class={monoInputClass}
          value={rule.field}
          placeholder={fieldHint}
          aria-label="Rule {i + 1} field"
          oninput={(e) => update(i, { field: e.currentTarget.value })}
        />
        <button class={iconButtonClass} aria-label="Remove rule {i + 1}" title="Remove rule" onclick={() => onchange(rules.filter((_, j) => j !== i), match)}>
          <X size={13} />
        </button>
      </div>
      <div class="flex items-center gap-1">
        <select
          class="{inputClass} w-36 shrink-0"
          value={rule.op}
          aria-label="Rule {i + 1} operator"
          onchange={(e) => setOp(i, e.currentTarget.value as RuleOp)}
        >
          {#each RULE_OPS as op (op.value)}
            <option value={op.value}>{op.label}</option>
          {/each}
        </select>
        {#if opNeedsValue(rule.op)}
          <input
            class={monoInputClass}
            value={rule.value}
            placeholder={valuePlaceholder(rule.op)}
            aria-label="Rule {i + 1} value"
            oninput={(e) => update(i, { value: e.currentTarget.value })}
          />
        {/if}
      </div>
    </div>
  {/each}

  <button class={smallButtonClass} onclick={() => onchange([...rules, { field: '', op: 'eq', value: '' }], match)}>
    <Plus size={12} /> Add rule
  </button>
</div>
