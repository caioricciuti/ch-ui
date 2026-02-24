/** Number of days in a given month (1-12). */
export function daysInMonth(year: number, month: number): number {
  return new Date(year, month, 0).getDate()
}

/** Day-of-week (0=Sun..6=Sat) for the 1st of a month (1-12). */
export function firstDayOfWeek(year: number, month: number): number {
  return new Date(year, month - 1, 1).getDay()
}

/**
 * Build a calendar grid for a given month.
 * Returns rows of 7 cells. Cells outside the month are null.
 */
export function buildMonthGrid(year: number, month: number): (Date | null)[][] {
  const total = daysInMonth(year, month)
  const startDay = firstDayOfWeek(year, month)
  const grid: (Date | null)[][] = []
  let day = 1

  for (let row = 0; row < 6; row++) {
    const week: (Date | null)[] = []
    for (let col = 0; col < 7; col++) {
      if (row === 0 && col < startDay) {
        week.push(null)
      } else if (day > total) {
        week.push(null)
      } else {
        week.push(new Date(year, month - 1, day))
        day++
      }
    }
    grid.push(week)
    if (day > total) break
  }
  return grid
}

/** Navigate months by delta, returns new { year, month }. */
export function shiftMonth(year: number, month: number, delta: number): { year: number; month: number } {
  const d = new Date(year, month - 1 + delta, 1)
  return { year: d.getFullYear(), month: d.getMonth() + 1 }
}

/** Check if two dates are the same calendar day. */
export function isSameDay(a: Date, b: Date): boolean {
  return (
    a.getFullYear() === b.getFullYear() &&
    a.getMonth() === b.getMonth() &&
    a.getDate() === b.getDate()
  )
}

/** Check if date falls within [from, to] inclusive (day-level). */
export function isInRange(date: Date, from: Date, to: Date): boolean {
  const t = date.getTime()
  const lo = new Date(from.getFullYear(), from.getMonth(), from.getDate()).getTime()
  const hi = new Date(to.getFullYear(), to.getMonth(), to.getDate(), 23, 59, 59, 999).getTime()
  return t >= lo && t <= hi
}

/** Check if a date is today. */
export function isToday(date: Date): boolean {
  return isSameDay(date, new Date())
}

/** Month name from month number (1-12). */
export function monthName(month: number): string {
  return new Date(2000, month - 1, 1).toLocaleString('en', { month: 'long' })
}
