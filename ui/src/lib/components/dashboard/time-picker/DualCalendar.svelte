<script lang="ts">
  import { shiftMonth } from '../../../utils/calendar'
  import { ChevronLeft, ChevronRight } from 'lucide-svelte'
  import CalendarMonth from './CalendarMonth.svelte'

  interface Props {
    rangeStart: Date | null
    rangeEnd: Date | null
    hoverDate: Date | null
    onselect: (date: Date) => void
    onhover: (date: Date | null) => void
  }

  let { rangeStart, rangeEnd, hoverDate, onselect, onhover }: Props = $props()

  let leftYear = $state(new Date().getFullYear())
  let leftMonth = $state(new Date().getMonth() + 1)

  const right = $derived(shiftMonth(leftYear, leftMonth, 1))

  function prev() {
    const m = shiftMonth(leftYear, leftMonth, -1)
    leftYear = m.year
    leftMonth = m.month
  }

  function next() {
    const m = shiftMonth(leftYear, leftMonth, 1)
    leftYear = m.year
    leftMonth = m.month
  }
</script>

<div>
  <div class="flex items-center justify-between mb-3 px-1">
    <button
      type="button"
      class="ds-icon-btn"
      onclick={prev}
      title="Previous month"
    >
      <ChevronLeft size={16} />
    </button>
    <button
      type="button"
      class="ds-icon-btn"
      onclick={next}
      title="Next month"
    >
      <ChevronRight size={16} />
    </button>
  </div>

  <div class="flex gap-8">
    <CalendarMonth
      year={leftYear}
      month={leftMonth}
      {rangeStart}
      {rangeEnd}
      {hoverDate}
      {onselect}
      {onhover}
    />
    <CalendarMonth
      year={right.year}
      month={right.month}
      {rangeStart}
      {rangeEnd}
      {hoverDate}
      {onselect}
      {onhover}
    />
  </div>
</div>
