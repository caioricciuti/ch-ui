<script lang="ts">
  import { TIME_RANGES } from '../../utils/chart-transform'
  import {
    decodeAbsoluteDashboardRange,
    encodeAbsoluteDashboardRange,
    formatDashboardTimeRangeLabel,
  } from '../../utils/dashboard-time'
  import { Clock3, ChevronDown, CalendarClock, TimerReset, SlidersHorizontal } from 'lucide-svelte'

  interface Props {
    value: string
    onchange: (value: string) => void
  }

  type PickerTab = 'presets' | 'relative' | 'absolute'

  let { value, onchange }: Props = $props()

  let open = $state(false)
  let activeTab = $state<PickerTab>('presets')
  let relativeFrom = $state('now-1h')
  let relativeTo = $state('now')
  let customExpr = $state('')
  let absoluteFrom = $state('')
  let absoluteTo = $state('')
  let rootEl: HTMLDivElement | null = null

  const label = $derived(formatDashboardTimeRangeLabel(value))
  const quickRelativeExpressions = ['now-5m', 'now-15m', 'now-1h', 'now-6h', 'now-24h', 'now-7d']

  $effect(() => {
    const absolute = decodeAbsoluteDashboardRange(value)
    if (absolute) {
      absoluteFrom = toDateTimeLocal(absolute.from)
      absoluteTo = toDateTimeLocal(absolute.to)
      customExpr = ''
      activeTab = 'absolute'
      return
    }

    const trimmed = value.trim()
    if (trimmed.includes(' to ')) {
      const [from, to] = trimmed.split(/\s+to\s+/i)
      relativeFrom = from.trim() || 'now-1h'
      relativeTo = to.trim() || 'now'
      customExpr = trimmed
      activeTab = 'relative'
      return
    }

    if (trimmed) {
      relativeFrom = trimmed.startsWith('now-') ? trimmed : `now-${trimmed}`
      relativeTo = 'now'
      customExpr = trimmed
      activeTab = 'relative'
    }
  })

  function toDateTimeLocal(isoLike: string): string {
    const d = new Date(isoLike)
    if (Number.isNaN(d.getTime())) return ''
    const local = new Date(d.getTime() - d.getTimezoneOffset() * 60000)
    return local.toISOString().slice(0, 16)
  }

  function fromDateTimeLocal(localDateTime: string): string {
    if (!localDateTime) return ''
    const d = new Date(localDateTime)
    if (Number.isNaN(d.getTime())) return ''
    return d.toISOString()
  }

  function applyPreset(v: string) {
    onchange(v)
    activeTab = 'presets'
    open = false
  }

  function applyRelativeRange() {
    const from = relativeFrom.trim() || 'now-1h'
    const to = relativeTo.trim() || 'now'
    onchange(`${from} to ${to}`)
    open = false
  }

  function applyCustomExpr() {
    const raw = customExpr.trim()
    if (!raw) return
    onchange(raw)
    open = false
  }

  function applyAbsoluteRange() {
    const fromISO = fromDateTimeLocal(absoluteFrom)
    const toISO = fromDateTimeLocal(absoluteTo)
    if (!fromISO || !toISO) return
    onchange(encodeAbsoluteDashboardRange(fromISO, toISO))
    open = false
  }

  function applyQuickExpr(expr: string) {
    customExpr = expr
    onchange(expr)
    open = false
  }

  function close() {
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
</script>

<svelte:window onmousedown={onWindowMouseDown} onkeydown={onWindowKeyDown} />

<div class="relative" bind:this={rootEl}>
  <button
    class="inline-flex items-center gap-1.5 text-xs bg-transparent border border-gray-300 dark:border-gray-700 rounded px-2 py-1 text-gray-700 dark:text-gray-300 hover:border-ch-orange"
    onclick={() => open = !open}
    title="Select dashboard time range"
  >
    <Clock3 size={12} class="text-ch-orange" />
    <span class="max-w-[220px] truncate">{label}</span>
    <ChevronDown size={12} class="text-gray-400 {open ? 'rotate-180' : ''}" />
  </button>

  {#if open}
    <div class="absolute right-0 mt-2 z-[70] w-[500px] max-w-[95vw] max-h-[72vh] overflow-y-auto surface-card rounded-xl border border-gray-200 dark:border-gray-800 p-3 shadow-xl backdrop-blur-xl">
      <div class="mb-3 flex items-center justify-between">
        <div class="text-[11px] uppercase tracking-wide text-gray-500 dark:text-gray-400">
          Dashboard Time Range
        </div>
        <button class="ds-btn-ghost px-2 py-1 text-[11px]" onclick={close}>Close</button>
      </div>

      <div class="mb-3 ds-segment w-full">
        <button
          class="ds-segment-btn flex-1 {activeTab === 'presets' ? 'ds-segment-btn-active' : ''}"
          onclick={() => activeTab = 'presets'}
        >
          Presets
        </button>
        <button
          class="ds-segment-btn flex-1 {activeTab === 'relative' ? 'ds-segment-btn-active' : ''}"
          onclick={() => activeTab = 'relative'}
        >
          Relative
        </button>
        <button
          class="ds-segment-btn flex-1 {activeTab === 'absolute' ? 'ds-segment-btn-active' : ''}"
          onclick={() => activeTab = 'absolute'}
        >
          Absolute
        </button>
      </div>

      {#if activeTab === 'presets'}
        <div class="grid grid-cols-2 gap-2">
          {#each TIME_RANGES as range}
            <button
              class="text-left rounded border px-2 py-1.5 text-xs transition-colors
                {value === range.value
                  ? 'border-ch-orange text-ch-orange bg-orange-100/60 dark:bg-orange-900/20'
                  : 'border-gray-200 dark:border-gray-700 text-gray-600 dark:text-gray-300 hover:border-gray-300 dark:hover:border-gray-600'}"
              onclick={() => applyPreset(range.value)}
            >
              {range.label}
            </button>
          {/each}
        </div>

        <div class="mt-3 rounded-lg border border-gray-200 dark:border-gray-800 bg-gray-100/70 dark:bg-gray-900/60 p-2">
          <div class="flex items-center gap-2 text-[11px] text-gray-500 dark:text-gray-400">
            <SlidersHorizontal size={12} class="text-ch-orange" />
            Need exact timestamps? Switch to Relative or Absolute above.
          </div>
        </div>
      {:else if activeTab === 'relative'}
        <div class="space-y-2">
          <div class="flex items-center gap-2 text-[11px] uppercase tracking-wide text-gray-500 dark:text-gray-400">
            <TimerReset size={12} class="text-ch-orange" />
            Relative Expression
          </div>
          <div class="grid grid-cols-2 gap-2">
            <input
              class="ds-input-sm"
              bind:value={relativeFrom}
              placeholder="now-1h"
            />
            <input
              class="ds-input-sm"
              bind:value={relativeTo}
              placeholder="now"
            />
          </div>
          <div class="grid grid-cols-3 gap-2">
            {#each quickRelativeExpressions as expr}
              <button class="ds-btn-outline px-2 py-1" onclick={() => applyQuickExpr(expr)}>{expr}</button>
            {/each}
          </div>
          <input
            class="ds-input-sm"
            bind:value={customExpr}
            placeholder="now-5m or now-5m to now-1m"
          />
          <div class="flex justify-end gap-2">
            <button class="ds-btn-outline px-2 py-1" onclick={applyCustomExpr}>Use Expression</button>
            <button class="ds-btn-primary px-2 py-1" onclick={applyRelativeRange}>Apply Range</button>
          </div>
        </div>
      {:else}
        <div class="space-y-2">
          <div class="flex items-center gap-2 text-[11px] uppercase tracking-wide text-gray-500 dark:text-gray-400">
            <CalendarClock size={12} class="text-ch-orange" />
            Absolute Range
          </div>
          <div class="grid grid-cols-2 gap-2">
            <input
              type="datetime-local"
              class="ds-input-sm"
              bind:value={absoluteFrom}
            />
            <input
              type="datetime-local"
              class="ds-input-sm"
              bind:value={absoluteTo}
            />
          </div>
          <p class="text-[11px] text-gray-500 dark:text-gray-400">
            Uses your local browser timezone. Query variables still receive UTC ISO timestamps.
          </p>
          <div class="flex justify-end">
            <button class="ds-btn-primary px-2 py-1" onclick={applyAbsoluteRange}>Apply Absolute</button>
          </div>
        </div>
      {/if}
    </div>
  {/if}
</div>
