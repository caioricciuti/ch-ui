<script lang="ts">
  import { Plus, X } from 'lucide-svelte'
  import { iconButtonClass, monoInputClass, smallButtonClass } from './ui'

  // Rows of two text inputs (from/to, field/value), or one when `second` is unset.
  interface Props {
    rows: [string, string][]
    first: string
    second?: string
    /** Text between the two inputs, e.g. an arrow or "=". */
    joiner?: string
    addLabel: string
    onchange: (rows: [string, string][]) => void
  }

  let { rows, first, second, joiner = '', addLabel, onchange }: Props = $props()

  function set(i: number, col: 0 | 1, v: string) {
    onchange(rows.map((r, j) => (j === i ? (col === 0 ? [v, r[1]] : [r[0], v]) : r)))
  }
</script>

<div class="space-y-1">
  {#each rows as row, i (i)}
    <div class="flex items-center gap-1">
      <input class={monoInputClass} value={row[0]} placeholder={first} aria-label="{first} {i + 1}" oninput={(e) => set(i, 0, e.currentTarget.value)} />
      {#if second !== undefined}
        {#if joiner}<span class="shrink-0 text-[12px] text-fg-4">{joiner}</span>{/if}
        <input class={monoInputClass} value={row[1]} placeholder={second} aria-label="{second} {i + 1}" oninput={(e) => set(i, 1, e.currentTarget.value)} />
      {/if}
      <button class={iconButtonClass} aria-label="Remove row {i + 1}" title="Remove" onclick={() => onchange(rows.filter((_, j) => j !== i))}>
        <X size={13} />
      </button>
    </div>
  {/each}
  <button class={smallButtonClass} onclick={() => onchange([...rows, ['', '']])}>
    <Plus size={12} /> {addLabel}
  </button>
</div>
