<script lang="ts">
  import type { ColumnFilter, FilterOperator } from '../../utils/result-filters'
  import { OPERATOR_LABELS, operatorsFor, isUnaryOperator, isValidFilterValue } from '../../utils/result-filters'
  import { Filter, X } from 'lucide-svelte'
  import Button from '../common/Button.svelte'

  interface Props {
    column: string
    chType: string
    current: ColumnFilter | null
    x: number
    y: number
    onapply: (filter: ColumnFilter) => void
    onclear: () => void
    onclose: () => void
  }

  let { column, chType, current, x, y, onapply, onclear, onclose }: Props = $props()

  const operators = $derived(operatorsFor(chType))
  let operator = $state<FilterOperator>('eq')
  let value = $state('')
  let panelEl = $state<HTMLDivElement | null>(null)
  let inputEl = $state<HTMLInputElement | null>(null)

  // Seed from the existing filter (or the first valid operator) once per open.
  $effect.pre(() => {
    column
    operator = current?.operator ?? operatorsFor(chType)[0]
    value = current?.value ?? ''
  })

  $effect(() => {
    inputEl?.focus()
  })

  const PANEL_WIDTH = 248
  const left = $derived(Math.max(8, Math.min(x, window.innerWidth - PANEL_WIDTH - 8)))
  const top = $derived(Math.min(y, window.innerHeight - 150))

  const valueInvalid = $derived(
    !isUnaryOperator(operator) && value !== '' && !isValidFilterValue(chType, operator, value)
  )
  const canApply = $derived(isUnaryOperator(operator) || (value !== '' && !valueInvalid))

  function apply() {
    if (!canApply) return
    onapply({ column, operator, value: isUnaryOperator(operator) ? '' : value })
  }

  function handleWindowClick(e: MouseEvent) {
    if (panelEl && !panelEl.contains(e.target as Node)) onclose()
  }

  function handleWindowKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') onclose()
  }
</script>

<svelte:window onmousedown={handleWindowClick} onkeydown={handleWindowKeydown} />

<div
  bind:this={panelEl}
  class="fixed z-50 rounded-md border border-edge bg-surface backdrop-blur-xl shadow-xl p-2.5"
  style="left:{left}px;top:{top}px;width:{PANEL_WIDTH}px"
>
  <div class="flex items-center justify-between mb-2">
    <div class="flex items-center gap-1.5 text-xs font-semibold text-fg-2 min-w-0">
      <Filter size={12} class="text-accent shrink-0" />
      <span class="truncate" title={column}>{column}</span>
    </div>
    <Button icon variant="ghost" size="sm" onclick={onclose} title="Close" aria-label="Close filter">
      <X size={13} />
    </Button>
  </div>

  <div class="flex flex-col gap-2">
    <select
      class="w-full px-2 py-1 text-xs rounded border border-edge bg-canvas text-fg-2 focus:outline-none focus:border-ch-orange"
      bind:value={operator}
    >
      {#each operators as op}
        <option value={op}>{OPERATOR_LABELS[op]}</option>
      {/each}
    </select>

    {#if !isUnaryOperator(operator)}
      <input
        bind:this={inputEl}
        class="w-full px-2 py-1 text-xs rounded border bg-canvas text-fg-2 focus:outline-none
          {valueInvalid
            ? 'border-danger focus:border-danger'
            : 'border-edge focus:border-ch-orange'}"
        placeholder="Value"
        bind:value
        spellcheck="false"
        onkeydown={(e) => { if (e.key === 'Enter') apply() }}
      />
      {#if valueInvalid}
        <p class="text-[11px] text-danger">This column is numeric — enter a number.</p>
      {/if}
    {/if}

    <div class="flex justify-end gap-1.5">
      {#if current}
        <button
          class="px-2 py-1 text-xs text-fg-3 hover:text-fg rounded hover:bg-hover"
          onclick={onclear}
        >Clear</button>
      {/if}
      <button
        class="px-2.5 py-1 text-xs bg-ch-orange text-white rounded hover:bg-ch-orange/80 disabled:opacity-50"
        onclick={apply}
        disabled={!canApply}
      >Apply</button>
    </div>
  </div>
</div>
