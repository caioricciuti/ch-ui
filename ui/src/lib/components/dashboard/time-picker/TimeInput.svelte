<script lang="ts">
  import { Clock3 } from 'lucide-svelte'

  interface Props {
    label: string
    value: string
    onchange: (value: string) => void
  }

  let { label, value, onchange }: Props = $props()

  let parts = $derived(value.split(':'))
  let h = $derived(parts[0] ?? '00')
  let m = $derived(parts[1] ?? '00')
  let s = $derived(parts[2] ?? '00')

  function update(field: 'h' | 'm' | 's', raw: string) {
    let n = parseInt(raw, 10)
    if (isNaN(n)) n = 0
    const max = field === 'h' ? 23 : 59
    n = Math.max(0, Math.min(max, n))
    const padded = String(n).padStart(2, '0')
    const nh = field === 'h' ? padded : h
    const nm = field === 'm' ? padded : m
    const ns = field === 's' ? padded : s
    onchange(`${nh}:${nm}:${ns}`)
  }

  function selectOnFocus(e: FocusEvent) {
    (e.target as HTMLInputElement).select()
  }
</script>

<div class="flex items-center gap-2">
  <Clock3 size={13} class="text-gray-400 shrink-0" />
  <span class="text-xs text-gray-500 dark:text-gray-400 w-10 shrink-0">{label}:</span>
  <div class="flex items-center gap-1">
    <input
      type="number"
      min="0"
      max="23"
      value={h}
      class="w-14 text-center ds-input-sm !px-1 tabular-nums"
      onchange={(e) => update('h', e.currentTarget.value)}
      onfocus={selectOnFocus}
    />
    <span class="text-xs text-gray-400">:</span>
    <input
      type="number"
      min="0"
      max="59"
      value={m}
      class="w-14 text-center ds-input-sm !px-1 tabular-nums"
      onchange={(e) => update('m', e.currentTarget.value)}
      onfocus={selectOnFocus}
    />
    <span class="text-xs text-gray-400">:</span>
    <input
      type="number"
      min="0"
      max="59"
      value={s}
      class="w-14 text-center ds-input-sm !px-1 tabular-nums"
      onchange={(e) => update('s', e.currentTarget.value)}
      onfocus={selectOnFocus}
    />
  </div>
</div>
