<script lang="ts">
  import type { ColumnFilter, FilterOperator } from '../../utils/result-filters'
  import { OPERATOR_LABELS, operatorsFor, isUnaryOperator, isValidFilterValue } from '../../utils/result-filters'
  import { Filter, X } from 'lucide-svelte'

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
  class="fixed z-50 rounded-lg border border-gray-300 dark:border-gray-700 bg-gray-50/98 dark:bg-gray-900/98 backdrop-blur-xl shadow-xl p-2.5"
  style="left:{left}px;top:{top}px;width:{PANEL_WIDTH}px"
>
  <div class="flex items-center justify-between mb-2">
    <div class="flex items-center gap-1.5 text-xs font-semibold text-gray-700 dark:text-gray-200 min-w-0">
      <Filter size={12} class="text-ch-orange shrink-0" />
      <span class="truncate" title={column}>{column}</span>
    </div>
    <button class="ds-icon-btn" onclick={onclose} title="Close" aria-label="Close filter">
      <X size={13} />
    </button>
  </div>

  <div class="flex flex-col gap-2">
    <select
      class="w-full px-2 py-1 text-xs rounded border border-gray-300 dark:border-gray-700 bg-white dark:bg-gray-950 text-gray-700 dark:text-gray-200 focus:outline-none focus:border-ch-blue"
      bind:value={operator}
    >
      {#each operators as op}
        <option value={op}>{OPERATOR_LABELS[op]}</option>
      {/each}
    </select>

    {#if !isUnaryOperator(operator)}
      <input
        bind:this={inputEl}
        class="w-full px-2 py-1 text-xs rounded border bg-white dark:bg-gray-950 text-gray-700 dark:text-gray-200 focus:outline-none
          {valueInvalid
            ? 'border-red-400 dark:border-red-500 focus:border-red-400'
            : 'border-gray-300 dark:border-gray-700 focus:border-ch-blue'}"
        placeholder="Value"
        bind:value
        spellcheck="false"
        onkeydown={(e) => { if (e.key === 'Enter') apply() }}
      />
      {#if valueInvalid}
        <p class="text-[11px] text-red-500 dark:text-red-400">This column is numeric — enter a number.</p>
      {/if}
    {/if}

    <div class="flex justify-end gap-1.5">
      {#if current}
        <button
          class="px-2 py-1 text-xs text-gray-500 dark:text-gray-400 hover:text-gray-800 dark:hover:text-gray-200 rounded hover:bg-gray-200 dark:hover:bg-gray-800"
          onclick={onclear}
        >Clear</button>
      {/if}
      <button
        class="px-2.5 py-1 text-xs bg-ch-blue text-white rounded hover:bg-ch-blue/80 disabled:opacity-50"
        onclick={apply}
        disabled={!canApply}
      >Apply</button>
    </div>
  </div>
</div>
