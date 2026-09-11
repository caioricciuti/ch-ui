<script lang="ts">
  import { buildMonthGrid, isSameDay, isInRange, isToday, monthName } from '../../../utils/calendar'

  interface Props {
    year: number
    month: number
    rangeStart: Date | null
    rangeEnd: Date | null
    hoverDate: Date | null
    onselect: (date: Date) => void
    onhover: (date: Date | null) => void
  }

  let { year, month, rangeStart, rangeEnd, hoverDate, onselect, onhover }: Props = $props()

  const WEEKDAYS = ['Su', 'Mo', 'Tu', 'We', 'Th', 'Fr', 'Sa']

  const grid = $derived(buildMonthGrid(year, month))
  const title = $derived(`${monthName(month)} ${year}`)

  function cellClass(date: Date): string {
    const base = 'h-8 w-8 flex items-center justify-center text-xs rounded-full cursor-pointer transition-colors'
    const parts = [base]

    const isStart = rangeStart && isSameDay(date, rangeStart)
    const isEnd = rangeEnd && isSameDay(date, rangeEnd)

    if (isStart || isEnd) {
      parts.push('bg-ch-orange text-white font-semibold')
    } else if (rangeStart && rangeEnd && isInRange(date, rangeStart, rangeEnd)) {
      parts.push('bg-accent-soft text-accent')
    } else if (rangeStart && !rangeEnd && hoverDate) {
      const hd = hoverDate.getTime() < rangeStart.getTime() ? hoverDate : hoverDate
      const previewStart = hd < rangeStart ? hd : rangeStart
      const previewEnd = hd < rangeStart ? rangeStart : hd
      if (isInRange(date, previewStart, previewEnd) && !isSameDay(date, rangeStart)) {
        parts.push('bg-accent/8 text-accent/70')
      } else if (!isToday(date)) {
        parts.push('text-fg-2 hover:bg-hover')
      }
    } else if (!isToday(date)) {
      parts.push('text-fg-2 hover:bg-hover')
    }

    if (isToday(date) && !isStart && !isEnd) {
      parts.push('font-bold ring-1 ring-ch-orange')
    }

    return parts.join(' ')
  }
</script>

<div class="select-none">
  <div class="text-center text-sm font-semibold text-fg mb-2">
    {title}
  </div>

  <div class="grid grid-cols-7 gap-0.5 mb-1">
    {#each WEEKDAYS as day}
      <div class="h-8 w-8 flex items-center justify-center text-[10px] font-medium text-fg-4">
        {day}
      </div>
    {/each}
  </div>

  {#each grid as week}
    <div class="grid grid-cols-7 gap-0.5">
      {#each week as cell}
        {#if cell}
          <button
            type="button"
            class={cellClass(cell)}
            onclick={() => onselect(cell)}
            onmouseenter={() => onhover(cell)}
            onmouseleave={() => onhover(null)}
          >
            {cell.getDate()}
          </button>
        {:else}
          <div class="h-8 w-8"></div>
        {/if}
      {/each}
    </div>
  {/each}
</div>
