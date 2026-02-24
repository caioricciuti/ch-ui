<script lang="ts">
  import {
    decodeAbsoluteDashboardRange,
    encodeAbsoluteDashboardRange,
    formatDashboardTimeRangeLabel,
    resolveNamedPreset,
  } from '../../utils/dashboard-time'
  import { Clock3, ChevronDown } from 'lucide-svelte'
  import DualCalendar from './time-picker/DualCalendar.svelte'
  import TimeInput from './time-picker/TimeInput.svelte'
  import TimezoneSelect from './time-picker/TimezoneSelect.svelte'
  import PresetList from './time-picker/PresetList.svelte'

  interface Props {
    value: string
    onchange: (value: string) => void
  }

  let { value, onchange }: Props = $props()

  let open = $state(false)
  let rangeStart = $state<Date | null>(null)
  let rangeEnd = $state<Date | null>(null)
  let hoverDate = $state<Date | null>(null)
  let fromTime = $state('00:00:00')
  let toTime = $state('23:59:59')
  let timezone = $state('UTC')
  let rootEl: HTMLDivElement | null = null

  const label = $derived(formatDashboardTimeRangeLabel(value))

  // Seed calendar state when the picker opens
  $effect(() => {
    if (!open) return

    // Try to decode the current value into calendar state
    const absolute = decodeAbsoluteDashboardRange(value)
    if (absolute) {
      const from = new Date(absolute.from)
      const to = new Date(absolute.to)
      if (!isNaN(from.getTime()) && !isNaN(to.getTime())) {
        rangeStart = new Date(from.getFullYear(), from.getMonth(), from.getDate())
        rangeEnd = new Date(to.getFullYear(), to.getMonth(), to.getDate())
        fromTime = pad(from.getUTCHours()) + ':' + pad(from.getUTCMinutes()) + ':' + pad(from.getUTCSeconds())
        toTime = pad(to.getUTCHours()) + ':' + pad(to.getUTCMinutes()) + ':' + pad(to.getUTCSeconds())
        return
      }
    }

    // Named presets — resolve to get calendar dates
    if (value.startsWith('preset:')) {
      const resolved = resolveNamedPreset(value)
      if (resolved) {
        const from = new Date(resolved.from)
        const to = new Date(resolved.to)
        rangeStart = new Date(from.getFullYear(), from.getMonth(), from.getDate())
        rangeEnd = new Date(to.getFullYear(), to.getMonth(), to.getDate())
        fromTime = pad(from.getUTCHours()) + ':' + pad(from.getUTCMinutes()) + ':' + pad(from.getUTCSeconds())
        toTime = pad(to.getUTCHours()) + ':' + pad(to.getUTCMinutes()) + ':' + pad(to.getUTCSeconds())
        return
      }
    }

    // Relative shorthand — show current moment minus offset
    const now = new Date()
    rangeEnd = new Date(now.getFullYear(), now.getMonth(), now.getDate())
    rangeStart = null
    fromTime = '00:00:00'
    toTime = pad(now.getUTCHours()) + ':' + pad(now.getUTCMinutes()) + ':' + pad(now.getUTCSeconds())
  })

  function pad(n: number): string {
    return String(n).padStart(2, '0')
  }

  function handleDateSelect(date: Date) {
    if (!rangeStart || rangeEnd) {
      rangeStart = date
      rangeEnd = null
      hoverDate = null
    } else {
      if (date < rangeStart) {
        rangeEnd = rangeStart
        rangeStart = date
      } else {
        rangeEnd = date
      }
      hoverDate = null
    }
  }

  function handleDateHover(date: Date | null) {
    if (rangeStart && !rangeEnd) {
      hoverDate = date
    }
  }

  function applyCalendarRange() {
    if (!rangeStart || !rangeEnd) return
    const [fh, fm, fs] = fromTime.split(':').map(Number)
    const [th, tm, ts] = toTime.split(':').map(Number)

    let fromDate: Date
    let toDate: Date
    if (timezone === 'UTC') {
      fromDate = new Date(Date.UTC(rangeStart.getFullYear(), rangeStart.getMonth(), rangeStart.getDate(), fh, fm, fs))
      toDate = new Date(Date.UTC(rangeEnd.getFullYear(), rangeEnd.getMonth(), rangeEnd.getDate(), th, tm, ts))
    } else {
      fromDate = new Date(rangeStart.getFullYear(), rangeStart.getMonth(), rangeStart.getDate(), fh, fm, fs)
      toDate = new Date(rangeEnd.getFullYear(), rangeEnd.getMonth(), rangeEnd.getDate(), th, tm, ts)
    }

    onchange(encodeAbsoluteDashboardRange(fromDate.toISOString(), toDate.toISOString()))
    open = false
  }

  function applyPreset(v: string) {
    onchange(v)
    open = false
  }

  function cancel() {
    open = false
  }

  function onWindowMouseDown(event: MouseEvent) {
    if (!open || !rootEl) return
    if (!(event.target instanceof Node)) return
    if (!rootEl.contains(event.target)) {
      open = false
    }
  }

  function onWindowKeyDown(event: KeyboardEvent) {
    if (event.key === 'Escape') {
      open = false
    }
  }

  const rangeDescription = $derived.by(() => {
    if (!rangeStart) return ''
    const fmt = (d: Date) =>
      `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
    if (!rangeEnd) return fmt(rangeStart)
    return `${fmt(rangeStart)}  →  ${fmt(rangeEnd)}`
  })
</script>

<svelte:window onmousedown={onWindowMouseDown} onkeydown={onWindowKeyDown} />

<div class="relative" bind:this={rootEl}>
  <!-- Trigger button -->
  <button
    class="inline-flex items-center gap-1.5 text-xs bg-transparent border border-gray-300 dark:border-gray-700 rounded px-2 py-1 text-gray-700 dark:text-gray-300 hover:border-ch-orange transition-colors"
    onclick={() => open = !open}
    title="Select dashboard time range"
  >
    <Clock3 size={12} class="text-ch-orange" />
    <span class="max-w-[220px] truncate">{label}</span>
    <ChevronDown size={12} class="text-gray-400 transition-transform {open ? 'rotate-180' : ''}" />
  </button>

  <!-- Popover -->
  {#if open}
    <div
      class="absolute right-0 mt-2 z-[70] surface-card rounded-xl border border-gray-200 dark:border-gray-800 shadow-xl backdrop-blur-xl
        sm:w-[750px] max-w-[95vw] overflow-hidden"
    >
      <div class="relative">
        <!-- Left: Calendar + time inputs (defines the popover height) -->
        <div class="p-5 sm:pr-[195px] flex flex-col gap-3">
          <DualCalendar
            {rangeStart}
            {rangeEnd}
            {hoverDate}
            onselect={handleDateSelect}
            onhover={handleDateHover}
          />

          {#if rangeDescription}
            <div class="text-center text-xs text-gray-500 dark:text-gray-400 font-mono">
              {rangeDescription}
            </div>
          {/if}

          <div class="border-t border-gray-200 dark:border-gray-800 pt-3 flex flex-col gap-2.5">
            <TimeInput label="From" value={fromTime} onchange={(v) => fromTime = v} />
            <TimeInput label="To" value={toTime} onchange={(v) => toTime = v} />
            <TimezoneSelect {timezone} onchange={(v) => timezone = v} />
          </div>

          <div class="flex items-center justify-end gap-2 pt-1">
            <button class="ds-btn-outline px-3 py-1.5 text-xs" onclick={cancel}>Cancel</button>
            <button
              class="ds-btn-primary px-3 py-1.5 text-xs"
              onclick={applyCalendarRange}
              disabled={!rangeStart || !rangeEnd}
            >
              Apply
            </button>
          </div>
        </div>

        <!-- Right: Presets sidebar — absolutely positioned, scrolls within calendar height -->
        <div class="border-t sm:border-t-0 sm:border-l border-gray-200 dark:border-gray-800
          w-full max-h-[50vh]
          sm:absolute sm:top-0 sm:right-0 sm:bottom-0 sm:w-[185px] sm:max-h-none
          py-3 overflow-y-auto">
          <PresetList currentValue={value} onselect={applyPreset} />
        </div>
      </div>
    </div>
  {/if}
</div>
