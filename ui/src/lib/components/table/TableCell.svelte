<script lang="ts">
  import { getDisplayType } from '../../utils/ch-types'

  interface Props {
    value: unknown
    type: string
    width: number
  }

  let { value, type, width }: Props = $props()

  const displayType = $derived(getDisplayType(type))

  const rawValue = $derived.by(() => {
    if (value === null || value === undefined) return 'NULL'
    if (typeof value === 'string') return value
    if (typeof value === 'object') return JSON.stringify(value)
    return String(value)
  })

  const formatted = $derived.by(() => {
    if (value === null || value === undefined) return null
    if (displayType === 'number' && typeof value === 'number') {
      return value.toLocaleString()
    }
    if (displayType === 'json' && typeof value === 'object') {
      return JSON.stringify(value)
    }
    return String(value)
  })

  const isNull = $derived(value === null || value === undefined)
  const align = $derived(displayType === 'number' ? 'text-right' : 'text-left')
  const isUrl = $derived(displayType === 'string' && typeof value === 'string' && /^https?:\/\//i.test(value))

  async function handleCopyCell() {
    if (typeof navigator === 'undefined' || isNull) return
    try {
      await navigator.clipboard.writeText(rawValue)
    } catch {
      // Clipboard failures are non-fatal and should not interrupt navigation.
    }
  }
</script>

<td
  class="px-2.5 truncate border-r border-gray-200/60 dark:border-gray-800/60 {align}"
  style="width:{width}px;max-width:{width}px;min-width:{width}px"
  title={isNull ? 'NULL' : `${rawValue}\n\nDouble-click to copy`}
  ondblclick={handleCopyCell}
>
  {#if isNull}
    <span class="inline-flex items-center rounded-sm px-1 py-0.5 text-[10px] font-medium uppercase tracking-wide bg-gray-200/80 dark:bg-gray-800 text-gray-500 dark:text-gray-400">Null</span>
  {:else if displayType === 'bool'}
    <span class="inline-flex items-center rounded-sm px-1.5 py-0.5 text-[10px] font-semibold uppercase tracking-wide {value ? 'text-emerald-700 dark:text-emerald-300 bg-emerald-100/70 dark:bg-emerald-500/12' : 'text-rose-700 dark:text-rose-300 bg-rose-100/70 dark:bg-rose-500/12'}">
      {String(value)}
    </span>
  {:else if isUrl}
    <span class="font-mono text-[12px] text-orange-700 dark:text-orange-300">{formatted}</span>
  {:else if displayType === 'json'}
    <span class="font-mono text-xs text-gray-500 dark:text-gray-400">{formatted}</span>
  {:else if displayType === 'number' || displayType === 'date'}
    <span class="font-mono tabular-nums text-[12px] {displayType === 'number' ? 'text-gray-800 dark:text-gray-200' : 'text-gray-700 dark:text-gray-300'}">{formatted}</span>
  {:else}
    <span class="text-[12px] text-gray-700 dark:text-gray-300">{formatted}</span>
  {/if}
</td>
